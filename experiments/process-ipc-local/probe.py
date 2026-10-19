"""Reviewed local-canary checks for selected-slot normalization, not inheritance."""
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
SOURCE = REPOSITORY / "experiments/process-ipc-local"
REFERENCE = REPOSITORY / "experiments/process-ipc/normalization.c"
OUTPUT = REPOSITORY / "_scratch/adapter-implementation-x921/ipc-local-native"
MANIFEST = OUTPUT / "build.json"
VARIANTS = ["local-control", "normalized-control", "normalized-confined"]
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
    return {"variants": [{"name": VARIANTS[0], "execute_target": False, "confine": False, "normalization": "cleanup_after_positive_measurement"}, {"name": VARIANTS[1], "execute_target": True, "confine": False, "normalization": "measured_before_exec"}, {"name": VARIANTS[2], "execute_target": True, "confine": True, "normalization": "measured_before_exec"}], "channels": CHANNELS, "inherited_prestate": "inventory_without_send_then_generic_normalization", "local_canary": "same_process_receive_plus_send_held_live", "positive_control": "each_slot_getter_matches_known_local_endpoint_and_send_receive_succeeds", "duplicate_aliases": True, "post_normalization_send_check": "query_through_still_live_receive_name", "receive_destroyed_before_exec": True, "normalizer": "byte_identical_reviewed_generic_routine", "unknown_endpoint_messages": False, "parent_inheritance_proven": False, "named_ipc_denial_proven": False, "global_registration": False, "exception_triggering": False, "qualified_process_backend": False, "automatic_retry": False, "invocations": 3, "limits": {"execution_seconds": 5, "cleanup_seconds": 2, "output_bytes_per_stream": CAPTURE_LIMIT, "file_descriptors": 64}, "native_run_requires": ["exact_source_and_binary_manifest", "independent_reviewed_sha256", "one_use_token"]}


def inside(path: Path) -> Path:
    path = path.absolute()
    path.relative_to(REPOSITORY)
    if path.resolve() != path:
        raise ValueError("task path is not canonical inside Lo")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def save(path: Path, content: str) -> None:
    with inside(path).open("x", encoding="utf-8") as stream:
        stream.write(content)


def normalizer_body(path: Path) -> bytes:
    encoded = inside(path).read_bytes()
    start = encoded.index(b"int lo_normalize_inherited_rights(void) {")
    return encoded[start:encoded.index(b"\n}\n", start) + 3]


def prepare() -> None:
    if OUTPUT.exists():
        raise ValueError("local IPC directory exists; no replacement or retry")
    inside(OUTPUT.parent)
    OUTPUT.mkdir()
    profile = "\n".join(["(version 1)", "(deny default (with no-log))", f"(allow process-exec (literal {json.dumps(str(OUTPUT / 'target'))}))", '(allow file-read* (subpath "/usr/lib") (subpath "/System/Library"))', f"(allow file-read* (literal {json.dumps(str(OUTPUT / 'target'))}))", f"(allow file-read-metadata (literal {json.dumps(str(OUTPUT))}))", "(deny network* (with no-log))", "(deny process-fork (with no-log))", '(allow file-read* file-test-existence (literal "/"))', ""])
    header = "".join(f"#define {key} {json.dumps(value)}\n" for key, value in {"LO_ROOT": str(OUTPUT), "LO_LAUNCHER": str(OUTPUT / "launcher"), "LO_TARGET": str(OUTPUT / "target"), "LO_PROFILE": profile}.items())
    save(OUTPUT / "candidate.sb", profile)
    save(OUTPUT / "fixture_paths.h", header)


def source_artifacts() -> list[Path]:
    return sorted(path for path in SOURCE.iterdir() if path.suffix in {".c", ".h", ".py", ".mk"}) + [REFERENCE] + [OUTPUT / name for name in ("controller", "launcher", "target", "candidate.sb", "fixture_paths.h")]


def seal() -> None:
    if normalizer_body(REFERENCE) != normalizer_body(SOURCE / "normalization.c"):
        raise ValueError("generic normalizer body differs from reviewed source")
    artifacts = {str(path.relative_to(REPOSITORY)): digest(path) for path in source_artifacts()}
    save(MANIFEST, json.dumps({"schema": "process-ipc-local-v1", "plan": plan(), "artifacts": artifacts}, indent=2, sort_keys=True) + "\n")
    print(digest(MANIFEST))


def verified(reviewed: str) -> dict:
    if digest(MANIFEST) != reviewed:
        raise ValueError("reviewed manifest digest differs")
    manifest = json.loads(MANIFEST.read_text())
    if manifest.get("schema") != "process-ipc-local-v1" or manifest.get("plan") != plan():
        raise ValueError("finite local IPC contract differs")
    if set(manifest["artifacts"]) != {str(path.relative_to(REPOSITORY)) for path in source_artifacts()}:
        raise ValueError("manifest does not cover exact source/binary selection")
    for name, expected in manifest["artifacts"].items():
        if digest(REPOSITORY / name) != expected:
            raise ValueError("sealed source, profile, or binary differs")
    if normalizer_body(REFERENCE) != normalizer_body(SOURCE / "normalization.c"):
        raise ValueError("generic normalizer changed")
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
    if any(not isinstance(record, dict) for record in reports):
        raise ValueError("invalid local IPC observation")
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
                controllers = [record for record in reports if record.get("kind") == "controller"]
                cleanup = len(controllers) == 1 and controllers[0].get("cleanup_complete") is True and controllers[0].get("child_joined") is True
            except ValueError:
                cleanup = False
            if cleanup or emergency:
                break
        needs_emergency = (cancel_at is not None and now - cancel_at > 2 and not exited) or (exited and not selector.get_map() and not cleanup) or (exited_at is not None and now - exited_at > 2 and bool(selector.get_map()))
        if needs_emergency and not emergency:
            emergency = True
            # All fixed fixture children inherit this owned group. The leader
            # remains unreaped until every possible group signal is finished.
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
    return {"variant": variant, "exit_code": code, "elapsed_seconds": time.monotonic() - started, "controller_joined": True, "cleanup_complete": cleanup, "emergency_group_signal": emergency, "cancel_requested": cancel_at is not None, "output_saturated": saturated, "stderr_empty": not captured["stderr"], "reports": reports}


def run_variant(cancellation: Cancellation, variant: str) -> dict:
    read_control, write_control = os.pipe()
    environment = {"CI": "1", "TMPDIR": str(OUTPUT), "LO_IPC_LOCAL_CONTROL_FD": str(read_control)}
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


def measured(result: dict) -> bool:
    if result.get("exit_code") != 0 or result.get("cleanup_complete") is not True or result.get("emergency_group_signal") or result.get("cancel_requested") or result.get("output_saturated") or result.get("stderr_empty") is not True:
        return False
    variant = result["variant"]
    records = result["reports"]
    if any(record.get("variant") != variant for record in records):
        return False
    inventories = [record for record in records if record.get("kind") == "inventory"]
    phases = [record.get("phase") for record in inventories]
    expected = ["inherited-prestate-no-send", "inherited-normalized", "local-positive-control", "local-normalized"] + ([] if variant == VARIANTS[0] else ["post-exec"])
    if phases != expected:
        return False
    for record in inventories:
        if record.get("api_status") != [0] * 4 or record.get("runtime_valid") != [True] * 3 or record.get("copy_cleanup_complete") is not True or record.get("requirements_met") is not True:
            return False
        if record["phase"] == "local-positive-control":
            if record.get("matches_known_local") != [True] * 4 or record.get("local_deliveries") != [True] * 4 or record["nonempty_slots"][0] != 3:
                return False
        elif record.get("local_deliveries") != [False] * 4:
            return False
        if record["phase"] in {"inherited-normalized", "local-normalized", "post-exec"} and (record.get("nonempty_slots") != [0] * 4 or record.get("bootstrap_cache_empty") is not True):
            return False
    fixtures = [record for record in records if record.get("kind") == "local_fixture"]
    controllers = [record for record in records if record.get("kind") == "controller"]
    if len(fixtures) != 1 or len(controllers) != 1 or len(records) != len(inventories) + 2:
        return False
    if not all(fixtures[0].get(key) is True for key in ("inherited_normalized", "setup_complete", "positive_controls", "normalization_complete", "send_removed_through_live_receive", "receive_destroyed", "cleanup_complete")):
        return False
    controller = controllers[0]
    return controller.get("child_created") is True and controller.get("child_joined") is True and controller.get("exited") is True and controller.get("exit_code") == 0 and controller.get("signal") == 0 and controller.get("cancelled") is False and controller.get("timed_out") is False and controller.get("cleanup_deadline_exceeded") is False


def run(reviewed: str) -> None:
    cancellation = Cancellation()
    def cancel(_number: int, _frame: object) -> None:
        cancellation.requested = True
    signal.signal(signal.SIGINT, cancel)
    signal.signal(signal.SIGTERM, cancel)
    manifest = verified(reviewed)
    token = OUTPUT / "run-token"
    if token.exists() or (OUTPUT / "run-token.consumed").exists() or (OUTPUT / "result.json").exists():
        raise ValueError("local IPC lane already authorized or attempted; no retry")
    save(token, reviewed + "\n")
    token.rename(OUTPUT / "run-token.consumed")
    results = []
    failed = False
    for variant in VARIANTS:
        result = run_variant(cancellation, variant)
        result["measurement_passed"] = measured(result)
        results.append(result)
        if not result["measurement_passed"]:
            failed = True
            break
    unchanged = verified(reviewed) == manifest
    save(OUTPUT / "result.json", json.dumps({"reviewed_manifest_sha256": reviewed, "runs": results, "selected_slot_normalization_measured": len(results) == 3 and not failed, "artifacts_unchanged": unchanged, "parent_inheritance_proven": False, "named_ipc_denial_proven": False, "qualified_process_backend": False}, indent=2, sort_keys=True) + "\n")
    if failed:
        raise ValueError("local IPC lane failed; no retry or next invocation")


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
