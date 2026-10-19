"""Prepare and seal the explicit, single-use Darwin startup qualification lane.

No command grants native qualification or changes the standard Process gate.
The run command requires the independently reviewed manifest digest. An outer
deadline retains the controller instead of guessing which historical PIDs live.
"""

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
SOURCE = REPOSITORY / "experiments/process-startup"
OUTPUT = REPOSITORY / "_scratch/adapter-implementation-x921/startup-native"
MANIFEST = OUTPUT / "manifest.json"
CASES = ["normal", "immediate-zero", "immediate-nonzero", "launcher-input-rejection", "eof-without-permit", "cancel-before-permit", "cancel-after-permit", "rapid-exit"]
OUTPUT_LIMIT = 64 << 10


@dataclass
class ControllerOwnership:
    control_write: int
    joined: bool = False


@dataclass
class ControllerCancellation:
    requested: bool = False


def inside(path: Path) -> Path:
    """Reject task endpoints that resolve outside this primary checkout."""
    path = path.absolute()
    path.relative_to(REPOSITORY)
    if path.resolve() != path:
        raise ValueError(f"noncanonical task path: {path}")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def save(path: Path, content: str) -> None:
    with inside(path).open("x", encoding="utf-8") as output:
        output.write(content)


def prepare() -> None:
    if OUTPUT.exists():
        raise ValueError("native task directory exists; no implicit replacement or retry")
    inside(OUTPUT.parent)
    OUTPUT.mkdir()
    profile = "\n".join([
        "(version 1)",
        "(deny default (with no-log))",
        f"(allow process-exec (literal {json.dumps(str(OUTPUT / 'target'))}))",
        '(allow file-read* (subpath "/usr/lib") (subpath "/System/Library"))',
        f"(allow file-read* (literal {json.dumps(str(OUTPUT / 'target'))}))",
        f"(allow file-read-metadata (literal {json.dumps(str(OUTPUT))}))",
        "(deny network* (with no-log))",
        "(deny process-fork (with no-log))",
        '(allow file-read* file-test-existence (literal "/"))',
        "",
    ])
    identity = OUTPUT.stat()
    header = "".join(f"#define {name} {json.dumps(value)}\n" for name, value in {"LO_PROFILE": profile, "LO_ROOT": str(OUTPUT), "LO_TARGET": str(OUTPUT / "target")}.items())
    header += f"#define LO_ROOT_DEVICE {identity.st_dev}ULL\n#define LO_ROOT_INODE {identity.st_ino}ULL\n"
    save(OUTPUT / "candidate.sb", profile)
    save(OUTPUT / "fixture_paths.h", header)


def seal() -> None:
    sources = sorted(SOURCE.glob("*.go")) + [SOURCE / "native_probe.mk", SOURCE / "native_probe.py"] + sorted((SOURCE / "native").glob("*.c"))
    artifacts = sources + [REPOSITORY / "go.mod", REPOSITORY / "go.sum", OUTPUT / "candidate.sb", OUTPUT / "fixture_paths.h", OUTPUT / "launcher", OUTPUT / "target", OUTPUT / "native-tests"]
    selected = {str(path.relative_to(REPOSITORY)): {"path": str(path.relative_to(REPOSITORY)), "sha256": digest(path)} for path in artifacts}
    manifest = {"launcher": str(OUTPUT / "launcher"), "target": str(OUTPUT / "target"), "directory": str(OUTPUT), "run_token": str(OUTPUT / "run-token"), "result": str(OUTPUT / "result.json"), "cases": CASES, "artifacts": selected}
    save(MANIFEST, json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(digest(MANIFEST))


def verified(reviewed: str) -> dict:
    if len(reviewed) != 64 or any(character not in "0123456789abcdef" for character in reviewed) or digest(MANIFEST) != reviewed:
        raise ValueError("exact reviewed manifest SHA-256 required")
    manifest = json.loads(MANIFEST.read_text())
    if manifest["cases"] != CASES:
        raise ValueError("finite lane changed")
    for artifact in manifest["artifacts"].values():
        if digest(REPOSITORY / artifact["path"]) != artifact["sha256"]:
            raise ValueError("sealed source or binary changed")
    return manifest


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def run(reviewed: str) -> None:
    cancellation = ControllerCancellation()
    def cancel_controller(_signum: int, _frame: object) -> None:
        cancellation.requested = True
    signal.signal(signal.SIGINT, cancel_controller)
    signal.signal(signal.SIGTERM, cancel_controller)
    manifest = verified(reviewed)
    token = inside(Path(manifest["run_token"]))
    if token.exists() or Path(str(token) + ".consumed").exists() or inside(Path(manifest["result"])).exists():
        raise ValueError("native lane already authorized or attempted; no retry")
    save(token, reviewed + "\n")
    environment = dict(os.environ)
    environment.update({"CI": "1", "LO_PROCESS_STARTUP_NATIVE_MANIFEST": str(MANIFEST), "LO_PROCESS_STARTUP_NATIVE_REVIEWED_SHA256": reviewed})
    control_read, control_write = os.pipe()
    environment["LO_PROCESS_STARTUP_NATIVE_CONTROL_FD"] = str(control_read)
    command = [str(OUTPUT / "native-tests"), "-test.run=^TestNativeProcessStartupReviewedLane$", "-test.v", "-test.count=1", "-test.timeout=0"]
    try:
        if cancellation.requested:
            raise ValueError("native controller cancelled before creation")
        process = subprocess.Popen(command, cwd=SOURCE, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True, pass_fds=(control_read,), preexec_fn=child_limits)
    except BaseException:
        os.close(control_read)
        os.close(control_write)
        raise
    ownership = ControllerOwnership(control_write)
    try:
        owned_controller(process, control_read, ownership, cancellation, manifest, reviewed)
    except BaseException:
        if not ownership.joined:
            if ownership.control_write >= 0:
                try:
                    os.write(ownership.control_write, b"C")
                except OSError:
                    pass
            retain_controller("unexpected controller failure; cleanup unverified")
        raise


def owned_controller(process: subprocess.Popen, control_read: int, ownership: ControllerOwnership, cancellation: ControllerCancellation, manifest: dict, reviewed: str) -> None:
    """Retain the one owned identity until all possible group signals end."""
    os.close(control_read)
    control_write = ownership.control_write
    # NOTE_EXIT observes without reaping. The controller cannot admit fixtures
    # until this witness is installed and the explicit A byte is received.
    watcher = None
    try:
        watcher = select.kqueue()
        watcher.control([select.kevent(process.pid, filter=select.KQ_FILTER_PROC, flags=select.KQ_EV_ADD | select.KQ_EV_ENABLE | select.KQ_EV_ONESHOT, fflags=select.KQ_NOTE_EXIT)], 0, 0)
        selector = selectors.DefaultSelector()
        streams = {"stdout": bytearray(), "stderr": bytearray()}
        for name, stream in (("stdout", process.stdout), ("stderr", process.stderr)):
            if stream is None:
                raise ValueError("controller stream absent")
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, name)
        if cancellation.requested:
            raise ValueError("native controller cancelled before fixture admission")
        if os.write(control_write, b"A") != 1:
            raise ValueError("controller admission byte not published")
    except (OSError, ValueError):
        ownership.control_write = -1
        os.close(control_write)
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except OSError:
            retain_controller("cannot stop controller after witness registration failure")
        process.wait()
        ownership.joined = True
        if watcher is not None:
            watcher.close()
        raise ValueError("controller exit witness registration failed; no fixture admitted")
    started = time.monotonic()
    deadline_exceeded = False
    cleanup_deadline = None
    leader_exited = False
    emergency = False
    cleanup_complete = False
    cleanup_checked = False
    saturated = False
    while selector.get_map() or not leader_exited:
        for event in watcher.control(None, 1, 0):
            if event.ident == process.pid and event.fflags & select.KQ_NOTE_EXIT:
                leader_exited = True
        now = time.monotonic()
        if not deadline_exceeded and (now - started > 75 or cancellation.requested or saturated):
            deadline_exceeded = True
            cleanup_deadline = now + 5
            try:
                os.write(control_write, b"C")
            except BrokenPipeError:
                pass
            ownership.control_write = -1
            os.close(control_write)
            control_write = -1
        if leader_exited and not cleanup_checked:
            cleanup_checked = True
            result = inside(Path(manifest["result"]))
            try:
                encoded = result.read_bytes()
                if len(encoded) <= OUTPUT_LIMIT:
                    cleanup_complete = json.loads(encoded).get("cleanup_complete") is True
            except (OSError, ValueError):
                cleanup_complete = False
        needs_emergency = (cleanup_deadline is not None and now >= cleanup_deadline and not leader_exited) or (cleanup_checked and not cleanup_complete)
        if needs_emergency and not emergency:
            emergency = True
            # The session leader is still an owned, unreaped identity here.
            # All fixed fixture children inherit this group; none can fork or
            # change groups. This is a failed lane, never successful cleanup.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except OSError:
                retain_controller("emergency group signal failed; ownership unresolved")
        for key, _ in selector.select(0.1):
            data = os.read(key.fileobj.fileno(), 8192)
            if not data:
                selector.unregister(key.fileobj)
                key.fileobj.close()
                continue
            selected = streams[key.data]
            remaining = OUTPUT_LIMIT - len(selected)
            selected.extend(data[:remaining])
            saturated = saturated or len(data) > remaining
    if control_write >= 0:
        ownership.control_write = -1
        os.close(control_write)
    # No PID or group signal is permitted after this sole controller reap.
    code = process.wait()
    ownership.joined = True
    watcher.close()
    selector.close()
    for name, content in streams.items():
        save(OUTPUT / (name + ".txt"), content.decode("utf-8", errors="replace"))
    verified(reviewed)
    save(OUTPUT / "controller.json", json.dumps({"reviewed_manifest_sha256": reviewed, "exit_code": code, "elapsed_seconds": time.monotonic() - started, "output_saturated": saturated, "deadline_exceeded": deadline_exceeded, "emergency_group_signal": emergency, "explicit_cleanup_complete": cleanup_complete, "controller_joined": True, "qualified_process_backend": False}, indent=2) + "\n")
    if code != 0 or saturated or deadline_exceeded or emergency or not cleanup_complete:
        raise ValueError("native lane failed; inspect bounded controller/result evidence; no retry")


def retain_controller(reason: str) -> None:
    """Keep this ownership controller alive when no safe release is established."""
    try:
        save(OUTPUT / "controller-retained.json", json.dumps({"reason": reason, "ownership_retained": True, "retry_allowed": False}) + "\n")
    except BaseException:
        pass
    try:
        print(reason + "; retaining harness, no further run permitted", file=sys.stderr, flush=True)
    except BaseException:
        pass
    while True:
        try:
            time.sleep(1)
        except BaseException:
            pass


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "seal", "verify", "run"))
    parser.add_argument("--reviewed-sha256", default="")
    arguments = parser.parse_args()
    if arguments.command == "prepare":
        prepare()
    elif arguments.command == "seal":
        seal()
    elif arguments.command == "verify":
        verified(arguments.reviewed_sha256)
    else:
        run(arguments.reviewed_sha256)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError) as error:
        print(f"native startup probe: {error}", file=sys.stderr)
        raise SystemExit(1) from error
