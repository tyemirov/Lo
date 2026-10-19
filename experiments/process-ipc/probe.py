"""Finite, reviewed inherited-rights measurements for one disposable Darwin fixture."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import resource
import select
import selectors
import signal
import subprocess
import sys
import time


REPOSITORY = Path(__file__).resolve().parents[2]
SOURCE = REPOSITORY / "experiments/process-ipc"
OUTPUT = REPOSITORY / "_scratch/adapter-implementation-x921/ipc-native"
MANIFEST = OUTPUT / "build.json"
VARIANTS = ["inheritance-control", "normalized-control", "normalized-confined"]
CHANNELS = ["registered_application_ports", "bootstrap_special_port", "task_exception_ports", "initial_thread_exception_ports"]
CAPTURE_LIMIT = 65536


@dataclass
class Cancellation:
    requested: bool = False


@dataclass
class Ownership:
    control: int
    joined: bool = False


def plan() -> dict:
    return {"variants": [{"name": name, "normalize": index != 0, "confine": index == 2, "exec_target": True} for index, name in enumerate(VARIANTS)], "channels": CHANNELS, "per_channel_control_required": True, "absent_or_unsupported_control": "inconclusive", "self_runtime_rights": "inventoried_separately", "bootstrap_clearing": "authority_removal_only", "canaries": "parent_owned_only", "global_registration": False, "exception_triggering": False, "cleanup": {"send_rights": True, "receive_rights": True, "port_arrays": True, "owned_processes": True}, "limits": {"execution_seconds": 5, "cleanup_seconds": 2, "output_bytes_per_stream": CAPTURE_LIMIT, "file_descriptors": 64}, "invocations": 3, "automatic_retry": False, "qualified_process_backend": False, "native_run_requires": ["exact_source_and_binary_manifest", "independent_reviewed_sha256", "one_use_token"], "standard_process_activation": "unavailable"}


def inside(path: Path) -> Path:
    path = path.absolute()
    path.relative_to(REPOSITORY)
    if path.resolve() != path:
        raise ValueError("task path is not canonical inside Lo")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def save(path: Path, value: str) -> None:
    with inside(path).open("x", encoding="utf-8") as stream:
        stream.write(value)


def prepare() -> None:
    if OUTPUT.exists():
        raise ValueError("IPC task directory exists; no replacement or retry")
    inside(OUTPUT.parent)
    OUTPUT.mkdir()
    profile = "\n".join(["(version 1)", "(deny default (with no-log))", f"(allow process-exec (literal {json.dumps(str(OUTPUT / 'target'))}))", '(allow file-read* (subpath "/usr/lib") (subpath "/System/Library"))', f"(allow file-read* (literal {json.dumps(str(OUTPUT / 'target'))}))", f"(allow file-read-metadata (literal {json.dumps(str(OUTPUT))}))", "(deny network* (with no-log))", "(deny process-fork (with no-log))", '(allow file-read* file-test-existence (literal "/"))', ""])
    header = "".join(f"#define {name} {json.dumps(value)}\n" for name, value in {"LO_ROOT": str(OUTPUT), "LO_TARGET": str(OUTPUT / "target"), "LO_LAUNCHER": str(OUTPUT / "launcher"), "LO_PROFILE": profile}.items())
    save(OUTPUT / "candidate.sb", profile)
    save(OUTPUT / "fixture_paths.h", header)


def seal() -> None:
    sources = sorted(path for path in SOURCE.iterdir() if path.suffix in {".c", ".h", ".py", ".mk"})
    artifacts = sources + [OUTPUT / name for name in ("controller", "launcher", "target", "candidate.sb", "fixture_paths.h")]
    manifest = {"schema": "process-ipc-v1", "plan": plan(), "artifacts": {str(path.relative_to(REPOSITORY)): digest(path) for path in artifacts}}
    save(MANIFEST, json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(digest(MANIFEST))


def verified(reviewed: str) -> dict:
    if digest(MANIFEST) != reviewed:
        raise ValueError("reviewed manifest digest differs")
    manifest = json.loads(MANIFEST.read_text())
    if manifest.get("schema") != "process-ipc-v1" or manifest.get("plan") != plan():
        raise ValueError("finite IPC contract differs")
    required = {str(path.relative_to(REPOSITORY)) for path in SOURCE.iterdir() if path.suffix in {".c", ".h", ".py", ".mk"}}
    required.update(str((OUTPUT / name).relative_to(REPOSITORY)) for name in ("controller", "launcher", "target", "candidate.sb", "fixture_paths.h"))
    if set(manifest["artifacts"]) != required:
        raise ValueError("manifest does not cover exact source/binary selection")
    for name, expected in manifest["artifacts"].items():
        if digest(REPOSITORY / name) != expected:
            raise ValueError("sealed source, profile, or binary differs")
    return manifest


def retain(reason: str) -> None:
    try:
        save(OUTPUT / "ownership-retained.json", json.dumps({"reason": reason, "ownership_retained": True, "retry_allowed": False}) + "\n")
    except BaseException:
        pass
    try:
        print(reason + "; ownership retained, no next invocation", file=sys.stderr, flush=True)
    except BaseException:
        pass
    while True:
        try:
            time.sleep(1)
        except BaseException:
            pass


def limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def parse_reports(encoded: bytes) -> list[dict]:
    reports = [json.loads(line) for line in encoded.splitlines() if line]
    if any(not isinstance(report, dict) for report in reports):
        raise ValueError("invalid IPC observation record")
    return reports


def owned_variant(process: subprocess.Popen, read_control: int, ownership: Ownership, cancellation: Cancellation, variant: str) -> dict:
    os.close(read_control)
    watcher = select.kqueue()
    watcher.control([select.kevent(process.pid, filter=select.KQ_FILTER_PROC, flags=select.KQ_EV_ADD | select.KQ_EV_ENABLE | select.KQ_EV_ONESHOT, fflags=select.KQ_NOTE_EXIT)], 0, 0)
    selector = selectors.DefaultSelector()
    captured = {"stdout": bytearray(), "stderr": bytearray()}
    for name, stream in (("stdout", process.stdout), ("stderr", process.stderr)):
        if stream is None:
            raise ValueError("controller pipe absent")
        os.set_blocking(stream.fileno(), False)
        selector.register(stream, selectors.EVENT_READ, name)
    if cancellation.requested:
        os.close(ownership.control)
        ownership.control = -1
    elif os.write(ownership.control, b"A") != 1:
        raise ValueError("controller admission byte not published")
    started = time.monotonic()
    exited = False
    exited_at = None
    cancel_at = None
    saturated = False
    emergency = False
    cleanup = False
    reports = []
    while True:
        for event in watcher.control(None, 1, 0):
            if event.ident == process.pid and event.fflags & select.KQ_NOTE_EXIT:
                exited = True
                exited_at = time.monotonic()
        now = time.monotonic()
        if cancel_at is None and (cancellation.requested or saturated or now - started > 8):
            cancel_at = now
            if ownership.control >= 0:
                try:
                    os.write(ownership.control, b"C")
                except BrokenPipeError:
                    pass
                os.close(ownership.control)
                ownership.control = -1
        for key, _ in selector.select(0.01):
            data = os.read(key.fileobj.fileno(), 8192)
            if not data:
                selector.unregister(key.fileobj)
                key.fileobj.close()
                continue
            destination = captured[key.data]
            remaining = CAPTURE_LIMIT - len(destination)
            destination.extend(data[:remaining])
            saturated = saturated or len(data) > remaining
        if exited and not selector.get_map():
            try:
                reports = parse_reports(bytes(captured["stdout"]))
                controllers = [report for report in reports if report.get("kind") == "controller"]
                cleanup = len(controllers) == 1 and controllers[0].get("cleanup_complete") is True and controllers[0].get("child_joined") is True
            except ValueError:
                cleanup = False
            if cleanup or emergency:
                break
        needs_emergency = (cancel_at is not None and now - cancel_at > 2 and not exited) or (exited and not selector.get_map() and not cleanup) or (exited_at is not None and now - exited_at > 2 and bool(selector.get_map()))
        if needs_emergency and not emergency:
            emergency = True
            # Keep the group leader unreaped until every possible group signal
            # is finished. The fixed controller forks exactly one fixed child;
            # launcher/target cannot fork or change groups in any variant.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except OSError:
                retain("emergency group signal failed")
    if ownership.control >= 0:
        os.close(ownership.control)
        ownership.control = -1
    code = process.wait()
    ownership.joined = True
    watcher.close()
    selector.close()
    for name, content in captured.items():
        save(OUTPUT / f"{variant}-{name}.txt", bytes(content).decode("utf-8", errors="replace"))
    return {"variant": variant, "exit_code": code, "elapsed_seconds": time.monotonic() - started, "controller_joined": True, "cleanup_complete": cleanup, "emergency_group_signal": emergency, "cancel_requested": cancel_at is not None, "output_saturated": saturated, "reports": reports}


def run_variant(cancellation: Cancellation, variant: str) -> dict:
    read_control, write_control = os.pipe()
    environment = {"CI": "1", "TMPDIR": str(OUTPUT), "LO_IPC_CONTROL_FD": str(read_control)}
    try:
        if cancellation.requested:
            raise ValueError("cancelled before controller creation")
        process = subprocess.Popen([str(OUTPUT / "controller"), variant], cwd=OUTPUT, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, pass_fds=(read_control,), start_new_session=True, preexec_fn=limits)
    except BaseException:
        os.close(read_control)
        os.close(write_control)
        raise
    ownership = Ownership(write_control)
    try:
        return owned_variant(process, read_control, ownership, cancellation, variant)
    except BaseException:
        if not ownership.joined:
            if ownership.control >= 0:
                try:
                    os.write(ownership.control, b"C")
                except OSError:
                    pass
            retain("unexpected controller failure; cleanup unverified")
        raise


def conclusions(results: list[dict]) -> dict:
    by_variant = {result["variant"]: result for result in results}
    statuses = {}
    for index, channel in enumerate(CHANNELS):
        control = by_variant.get("inheritance-control", {})
        control_snapshots = [record for record in control.get("reports", []) if record.get("kind") == "snapshot" and record.get("phase") == "post-exec"]
        controllers = [record for record in control.get("reports", []) if record.get("kind") == "controller"]
        observed = successful(control) and len(control_snapshots) == 1 and len(controllers) == 1 and control_snapshots[0]["matches_reference"][index] is True and control_snapshots[0]["canary_message_status"][index] == 0 and controllers[0]["canary_messages"][index] == 1
        removed = True
        for name in VARIANTS[1:]:
            result = by_variant.get(name, {})
            snapshots = [record for record in result.get("reports", []) if record.get("kind") == "snapshot"]
            selected = {record["phase"]: record for record in snapshots}
            before = selected.get("pre-normalization")
            after = selected.get("post-exec")
            removed = removed and successful(result) and before is not None and after is not None and before["matches_reference"][index] is True and after["api_status"][index] == 0 and after["nonempty_slots"][index] == 0 and all(record.get("copy_cleanup_complete") is True and record.get("self_task_valid") is True and record.get("self_thread_valid") is True for record in snapshots)
            if index == 1:
                removed = removed and after is not None and after["bootstrap_cache_nonempty"] is False
        statuses[channel] = "control-observed-normalized-slots-empty" if observed and removed else "inconclusive"
    return statuses


def successful(result: dict) -> bool:
    return result.get("exit_code") == 0 and result.get("cleanup_complete") is True and result.get("emergency_group_signal") is False and result.get("cancel_requested") is False and result.get("output_saturated") is False


def run(reviewed: str) -> None:
    cancellation = Cancellation()
    def request_cancel(_number: int, _frame: object) -> None:
        cancellation.requested = True
    signal.signal(signal.SIGINT, request_cancel)
    signal.signal(signal.SIGTERM, request_cancel)
    manifest = verified(reviewed)
    token = OUTPUT / "run-token"
    if token.exists() or (OUTPUT / "run-token.consumed").exists() or (OUTPUT / "result.json").exists():
        raise ValueError("IPC lane already authorized or attempted; no retry")
    save(token, reviewed + "\n")
    token.rename(OUTPUT / "run-token.consumed")
    results = []
    failed = False
    for variant in VARIANTS:
        result = run_variant(cancellation, variant)
        results.append(result)
        failed = result["exit_code"] != 0 or not result["cleanup_complete"] or result["emergency_group_signal"] or result["cancel_requested"] or result["output_saturated"]
        if failed:
            break
    unchanged = verified(reviewed) == manifest
    evidence = {"reviewed_manifest_sha256": reviewed, "runs": results, "channel_conclusions": conclusions(results), "artifacts_unchanged": unchanged, "qualified_process_backend": False, "scope": "current fixed fixture; bootstrap slot/cache removal is not named IPC denial"}
    save(OUTPUT / "result.json", json.dumps(evidence, indent=2, sort_keys=True) + "\n")
    if failed:
        raise ValueError("IPC lane failed; no retry or next invocation")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("plan", "prepare", "seal", "verify", "run"))
    parser.add_argument("reviewed", nargs="?", default="")
    arguments = parser.parse_args()
    if arguments.command in {"verify", "run"} and (len(arguments.reviewed) != 64 or any(character not in "0123456789abcdef" for character in arguments.reviewed)):
        print(json.dumps({"error": "reviewed manifest SHA256 is required"}), file=sys.stderr)
        return 2
    if arguments.command == "plan":
        print(json.dumps(plan()))
    elif arguments.command == "prepare":
        prepare()
    elif arguments.command == "seal":
        seal()
    elif arguments.command == "verify":
        verified(arguments.reviewed)
    else:
        run(arguments.reviewed)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        raise SystemExit(1) from error
