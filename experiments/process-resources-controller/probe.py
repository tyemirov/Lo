"""Sealed stop-controller correction using unchanged finite native fixtures."""
from __future__ import annotations

import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import resource
import selectors
import signal
import subprocess
import sys
import time
from types import ModuleType
from typing import Callable, Protocol

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-resources-controller"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources-controller"
TAIL_ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources-tail"
TAIL_SOURCE = REPOSITORY / "experiments/process-resources-tail/probe.py"
TAIL_SHA = "b685686be2a722eade4f8511f77e615bef72089272bbd2e563b8e7451ff9be75"
TAIL_RESULT_SHA = "30fcb7d270cb5b191eb2a5920d4c604c58f0f1c18bea0363dfff16c705bd0a6e"
CASE_NAMES = ("stdout-overflow", "stderr-exact", "stderr-overflow", "dual-pressure", "fd", "limits")
ARTIFACTS = {"harness": SOURCE / "probe.py", "test": SOURCE / "test_probe.py", "build": SOURCE / "probe.mk", "tail_manifest": TAIL_ROOT / "build.json", "tail_result": TAIL_ROOT / "result.json", "tail_harness": TAIL_SOURCE}


class OwnedChild(Protocol):
    pid: int
    returncode: int | None

    def poll(self) -> int | None: ...
    def wait(self, timeout: float) -> int: ...


class OwnedChildController:
    """Keep stop authority failure separate from confirmed resource cleanup."""

    def __init__(self, child: OwnedChild, signal_group: Callable[[int, int], None]) -> None:
        self.child = child
        self.signal_group = signal_group
        self.stop_reason: str | None = None
        self.signal_errors: list[dict[str, int | None]] = []
        self.poll_errors: list[dict[str, int | None]] = []
        self.wait_errors: list[dict[str, object]] = []
        self.stop_failed = False
        self.host_kill = False
        self.terminal_confirmed = False

    def poll(self) -> int | None:
        try:
            status = self.child.poll()
        except OSError as error:
            self.poll_errors.append({"errno": error.errno})
            self.stop_failed = True
            return None
        if status is not None:
            self.terminal_confirmed = True
        return status

    def request_stop(self, reason: str) -> None:
        if self.stop_reason is not None:
            return
        self.stop_reason = reason
        if self.poll() is not None:
            return
        if self.poll_errors:
            return  # Unknown terminal state must not grant numeric-PID authority.
        try:
            self.signal_group(self.child.pid, signal.SIGKILL)
            self.host_kill = True
        except OSError as error:
            self.signal_errors.append({"errno": error.errno})
            # This immediate observation decides the stop outcome permanently.
            # A later exit may complete cleanup, but cannot repair failed stop.
            if self.poll() is None:
                self.stop_failed = True

    def finish(self, streams: dict[str, dict[str, bool]], timeout: float) -> dict[str, object]:
        if timeout <= 0:
            raise ValueError("cleanup timeout must be positive")
        try:
            self.child.wait(timeout=timeout)
            self.terminal_confirmed = self.child.returncode is not None
        except subprocess.TimeoutExpired:
            self.wait_errors.append({"timeout": True})
            self.poll()
        except OSError as error:
            self.wait_errors.append({"errno": error.errno})
            self.poll()
        return {"stop_reason": self.stop_reason, "signal_errors": self.signal_errors, "poll_errors": self.poll_errors, "wait_errors": self.wait_errors, "stop_failed": self.stop_failed, "host_kill": self.host_kill, "terminal_confirmed": self.terminal_confirmed, "cleanup_complete": self.terminal_confirmed and len(streams) == 2 and all(stream["eof"] and stream["closed"] for stream in streams.values())}


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def reference() -> tuple[ModuleType, dict[str, object]]:
    if digest(TAIL_ROOT / "build.json") != TAIL_SHA or digest(TAIL_ROOT / "result.json") != TAIL_RESULT_SHA:
        raise ValueError("reviewed tail history differs")
    manifest = json.loads(inside(TAIL_ROOT / "build.json").read_text())
    code = inside(TAIL_SOURCE).read_bytes()
    if hashlib.sha256(code).hexdigest() != manifest["harness"]:
        raise ValueError("reviewed tail harness differs")
    specification = importlib.util.spec_from_file_location("lo_reviewed_resources_tail", inside(TAIL_SOURCE))
    if specification is None:
        raise ValueError("tail module specification is unavailable")
    module = importlib.util.module_from_spec(specification)
    sys.modules[specification.name] = module
    exec(compile(code, str(TAIL_SOURCE), "exec"), module.__dict__)
    original, _, actual = module.verify(TAIL_SHA)
    history = json.loads(inside(TAIL_ROOT / "result.json").read_text())
    previous = history["cases"]
    if tuple(case["case"] for case in previous) != ("wall", "stdout-exact", "stdout-overflow") or not all(case["passed"] for case in previous[:2]) or previous[2]["passed"] or history["tail_cases_passed"]:
        raise ValueError("tail failed history differs")
    if history["post_run_artifacts"] != actual or history["fixture_inputs"] != history["fixture_outputs"]:
        raise ValueError("tail integrity history differs")
    for case in previous:
        if not case["owned_child_joined"] or not case["artifacts_unchanged"] or not case["fixtures_unchanged"]:
            raise ValueError("tail ownership or integrity differs")
        for name, evidence in case["streams"].items():
            if not evidence["eof"] or not evidence["closed"] or digest(original.ROOT / "cases" / f"{case['case']}.{name}") != evidence["sha256"]:
                raise ValueError("tail captured evidence differs")
    return original, history


def prepare() -> None:
    reference()
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior controller result before preparation")
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "cases").mkdir(exist_ok=True)


def seal() -> None:
    reference()
    inside(ROOT / "build.json").write_text(json.dumps({name: digest(path) for name, path in ARTIFACTS.items()}, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> tuple[ModuleType, dict[str, object], dict[str, str]]:
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed controller manifest differs")
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed controller artifacts differ")
    original, history = reference()
    return original, history, actual


def run_case(original: ModuleType, name: str) -> dict[str, object]:
    limit = original.OUTPUT_LIMIT
    report: dict[str, object] = {"case": name, "timeout": False, "output_limit": False, "cleanup_errors": []}
    captured = {stream: bytearray() for stream in ("stdout", "stderr")}
    counts = {stream: 0 for stream in captured}
    eof = {stream: False for stream in captured}
    streams = {}
    selector = selectors.DefaultSelector()
    process = None
    controller = None
    reader = writer = None
    start = time.monotonic()
    deadline = start + original.EXECUTION_TIMEOUT
    cleanup_deadline = None
    usage_before = resource.getrusage(resource.RUSAGE_CHILDREN)

    def close_fd(descriptor: int) -> None:
        try:
            os.close(descriptor)
        except OSError as error:
            report["cleanup_errors"].append({"operation": "close-control", "errno": error.errno})

    def stop(reason: str) -> None:
        nonlocal cleanup_deadline
        if cleanup_deadline is None:
            cleanup_deadline = time.monotonic() + original.CLEANUP_TIMEOUT
        if controller is not None:
            controller.request_stop(reason)

    try:
        reader, writer = os.pipe()
        if not 3 <= reader < original.FD_LIMIT:
            raise ValueError("control reader exceeds inherited descriptor range")
        process = subprocess.Popen([str(original.ROOT / "launcher"), str(reader), "values"], cwd=inside(original.ROOT), env={"CI": "1", "TMPDIR": str(original.ROOT)}, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True, pass_fds=(reader,), start_new_session=True, preexec_fn=original.child_limits)
        controller = OwnedChildController(process, os.killpg)
        streams = {"stdout": process.stdout, "stderr": process.stderr}
        close_fd(reader)
        reader = None
        for stream_name, stream in streams.items():
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, stream_name)
        payload = original.control_frame(name)
        sent = 0
        os.set_blocking(writer, False)
        selector.register(writer, selectors.EVENT_WRITE, "control")
        while selector.get_map() or controller.poll() is None:
            now = time.monotonic()
            if cleanup_deadline is None and now >= deadline:
                report["timeout"] = True
                stop("timeout")
            if cleanup_deadline is not None and now >= cleanup_deadline:
                report["cleanup_errors"].append({"operation": "drain-or-wait", "timeout": True})
                break
            end = cleanup_deadline if cleanup_deadline is not None else deadline
            for key, _ in selector.select(min(0.05, max(0, end - now))):
                if key.data == "control":
                    try:
                        count = os.write(writer, payload[sent:sent + 7])
                        if count <= 0:
                            raise OSError(errno.EIO, "control writer made no progress")
                        sent += count
                        if sent == len(payload):
                            selector.unregister(writer)
                            close_fd(writer)
                            writer = None
                    except (BlockingIOError, InterruptedError):
                        continue
                    except OSError as error:
                        report["control_error_errno"] = error.errno
                        selector.unregister(writer)
                        close_fd(writer)
                        writer = None
                        stop("transport")
                    continue
                stream_name = key.data
                allowance = limit - len(captured[stream_name])
                try:
                    data = os.read(key.fd, 4096 if report["output_limit"] else min(4096, allowance + 1))
                except (BlockingIOError, InterruptedError):
                    continue
                if not data:
                    eof[stream_name] = True
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                counts[stream_name] += len(data)
                captured[stream_name].extend(data[:allowance])
                if counts[stream_name] > limit and not report["output_limit"]:
                    report["output_limit"] = True
                    stop("output_limit")
        report["control_bytes_written"] = sent
    except Exception as error:
        report["transport_error"] = str(error)[:256]
        stop("transport")
    finally:
        if controller is not None and controller.poll() is None:
            stop("cleanup")
        for descriptor in (reader, writer):
            if descriptor is not None:
                close_fd(descriptor)
        try:
            selector.close()
        except OSError as error:
            report["cleanup_errors"].append({"operation": "close-selector", "errno": error.errno})
        for stream_name, stream in streams.items():
            try:
                stream.close()
            except OSError as error:
                report["cleanup_errors"].append({"operation": "close-" + stream_name, "errno": error.errno})
        ownership = {stream_name: {"eof": eof[stream_name], "closed": stream_name in streams and streams[stream_name].closed} for stream_name in captured}
        if controller is not None:
            remaining = max(0.001, (cleanup_deadline or (time.monotonic() + original.CLEANUP_TIMEOUT)) - time.monotonic())
            report["controller"] = controller.finish(ownership, timeout=remaining)
        else:
            report["controller"] = {"cleanup_complete": not report["cleanup_errors"], "stop_failed": False, "terminal_confirmed": False, "signal_errors": [], "poll_errors": [], "wait_errors": [], "host_kill": False, "stop_reason": None}
        report["exit_code"] = process.returncode if process is not None else None
    usage_after = resource.getrusage(resource.RUSAGE_CHILDREN)
    report["cpu_seconds"] = usage_after.ru_utime + usage_after.ru_stime - usage_before.ru_utime - usage_before.ru_stime
    report["elapsed_seconds"] = time.monotonic() - start
    report["streams"] = {}
    for stream_name, data in captured.items():
        with inside(ROOT / "cases" / f"{name}.{stream_name}").open("xb") as destination:
            destination.write(data)
        report["streams"][stream_name] = {"observed_bytes": counts[stream_name], "captured_bytes": len(data), **ownership[stream_name], "sha256": hashlib.sha256(data).hexdigest(), "preview": bytes(data[:512]).decode("utf-8", errors="replace")}
    stdout, stderr = bytes(captured["stdout"]), bytes(captured["stderr"])
    try:
        observation = json.loads(stdout) if stdout else None
    except (ValueError, UnicodeDecodeError):
        observation = None
    report["observations"] = observation
    state = report["controller"]
    report["cleanup_complete"] = bool(state["cleanup_complete"] and not report["cleanup_errors"])
    healthy = report["cleanup_complete"] and not state["stop_failed"] and not state["poll_errors"] and not state["wait_errors"] and "transport_error" not in report and "control_error_errno" not in report
    ready = stderr.startswith(original.PREFIX)
    ordinary = healthy and ready and report["exit_code"] == 0 and not report["timeout"] and not report["output_limit"] and not state["host_kill"]
    if name == "stderr-exact":
        passed = ordinary and not stdout and stderr == original.PREFIX + b"e" * (limit - len(original.PREFIX))
    elif name == "fd":
        passed = ordinary and observation == {"error": errno.EMFILE, "duplicates": 61, "closed": True}
    elif name == "limits":
        passed = ordinary and observation == {"limits_exact": True, "hard_raises_denied": True}
    else:
        if name == "stdout-overflow":
            payload_exact = stdout == b"o" * limit and stderr == original.PREFIX and counts["stdout"] > limit
        elif name == "stderr-overflow":
            payload_exact = not stdout and stderr == original.PREFIX + b"e" * (limit - len(original.PREFIX)) and counts["stderr"] > limit
        else:
            payload_exact = bool(stdout) and stdout == b"o" * len(stdout) and stderr == original.PREFIX + b"e" * (len(stderr) - len(original.PREFIX)) and any(count > limit for count in counts.values())
        expected_exit = report["exit_code"] == 0 or report["exit_code"] == -signal.SIGKILL and state["host_kill"]
        passed = healthy and ready and payload_exact and expected_exit and report["output_limit"] and not report["timeout"]
    report["passed"] = bool(passed)
    unexpected_signal = isinstance(report["exit_code"], int) and report["exit_code"] < 0 and not (report["output_limit"] and state["host_kill"] and report["exit_code"] == -signal.SIGKILL)
    report["stop_suite"] = bool(not healthy or unexpected_signal)
    return report


def run(expected: str) -> int:
    original, history, reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior controller result before another suite")
    for name in CASE_NAMES:
        for stream in ("stdout", "stderr"):
            if inside(ROOT / "cases" / f"{name}.{stream}").exists():
                raise ValueError("preserve prior controller captures")
    if any(inside(original.ROOT / name).read_bytes() != content for name, content in original.FIXTURES.items()):
        raise ValueError("original fixture baseline differs")
    before = {name: digest(original.ROOT / name) for name in original.FIXTURES}
    report = {"scope": "New stop-controller candidate and six fixed resource cases only. Original CPU-hard and tail failures remain unchanged; no complete native Process qualification.", "qualified_process_backend": False, "reviewed_manifest_sha256": expected, "tail_manifest_sha256": TAIL_SHA, "tail_result_sha256": TAIL_RESULT_SHA, "original_manifest_sha256": history["original_manifest_sha256"], "original_result_sha256": history["original_result_sha256"], "limits": history["limits"], "startup_limit": history["startup_limit"], "cleanup_scope": history["cleanup_scope"], "fixture_inputs": before, "cases": []}
    for name in CASE_NAMES:
        result = run_case(original, name)
        try:
            _, _, actual = verify(expected)
            result["artifacts_unchanged"] = actual == reviewed
            result["fixtures_unchanged"] = {name: digest(original.ROOT / name) for name in original.FIXTURES} == before
        except (OSError, ValueError) as error:
            result["integrity_error"] = str(error)[:256]
            result["artifacts_unchanged"] = False
            result["fixtures_unchanged"] = False
        report["cases"].append(result)
        if result["stop_suite"] or not result["artifacts_unchanged"] or not result["fixtures_unchanged"]:
            break
    try:
        _, _, report["post_run_artifacts"] = verify(expected)
        report["fixture_outputs"] = {name: digest(original.ROOT / name) for name in original.FIXTURES}
    except (OSError, ValueError) as error:
        report["integrity_error"] = str(error)[:256]
    passed = len(report["cases"]) == len(CASE_NAMES) and all(result["passed"] and result["artifacts_unchanged"] and result["fixtures_unchanged"] for result in report["cases"]) and report.get("post_run_artifacts") == reviewed and report.get("fixture_outputs") == before
    report["candidate_cases_passed"] = bool(passed)
    with inside(ROOT / "result.json").open("x") as destination:
        destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if passed else 1


def main() -> int:
    arguments = sys.argv[1:]
    if arguments and arguments[0] in ("verify", "run") and (len(arguments) != 2 or re.fullmatch(r"[0-9a-f]{64}", arguments[1]) is None):
        raise ValueError("reviewed manifest SHA256 is required")
    if Path.cwd().resolve() != inside(REPOSITORY):
        raise ValueError("run from the Lo repository")
    if arguments == ["prepare"]:
        prepare()
    elif arguments == ["seal"]:
        seal()
    elif len(arguments) == 2 and arguments[0] == "verify":
        verify(arguments[1])
        print(json.dumps({"reviewed_build_verified": True}))
    elif len(arguments) == 2 and arguments[0] == "run":
        return run(arguments[1])
    else:
        raise ValueError("expected prepare, seal, verify SHA256, or run SHA256")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(2)
