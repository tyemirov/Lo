"""Completion-based stop candidate over unchanged finite native fixtures."""
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
SOURCE = REPOSITORY / "experiments/process-stop-completion"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-stop-completion"
PREVIOUS_ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources-controller"
PREVIOUS_SOURCE = REPOSITORY / "experiments/process-resources-controller/probe.py"
PREVIOUS_SHA = "68909a6ffb6e17ed319eed9a9670f776bc60301c5072f9f93b75b7a81fcac04a"
PREVIOUS_RESULT_SHA = "2cd93f9ccf4a8d8b7297363645d532b58e1048c815a66e59b2809bd51e796882"
CASE_NAMES = ("stdout-overflow", "stderr-exact", "stderr-overflow", "dual-pressure", "fd", "limits")
ARTIFACTS = {"harness": SOURCE / "probe.py", "test": SOURCE / "test_probe.py", "build": SOURCE / "probe.mk", "previous_manifest": PREVIOUS_ROOT / "build.json", "previous_result": PREVIOUS_ROOT / "result.json", "previous_harness": PREVIOUS_SOURCE}


class OwnedChild(Protocol):
    pid: int
    returncode: int | None

    def poll(self) -> int | None: ...
    def wait(self, timeout: float) -> int: ...


class CompletionController:
    """Own one absolute cleanup deadline and distinguish evidence from failure."""

    def __init__(self, child: OwnedChild, signal_group: Callable[[int, int], None], cleanup_timeout: float, clock: Callable[[], float] = time.monotonic) -> None:
        if cleanup_timeout <= 0:
            raise ValueError("cleanup timeout must be positive")
        self.child, self.signal_group = child, signal_group
        self.cleanup_timeout, self.clock = cleanup_timeout, clock
        self.deadline: float | None = None
        self.stop_reason: str | None = None
        self.signal_errors: list[dict[str, int | None]] = []
        self.poll_errors: list[dict[str, int | None]] = []
        self.wait_errors: list[dict[str, object]] = []
        self.deadline_failed = False
        self.host_kill = False
        self.terminal_confirmed = False

    def begin_cleanup(self) -> float:
        if self.deadline is None:
            self.deadline = self.clock() + self.cleanup_timeout
        return self.deadline

    def expired(self) -> bool:
        if self.deadline is not None and self.clock() >= self.deadline:
            self.deadline_failed = True
        return self.deadline_failed

    def poll(self) -> int | None:
        try:
            status = self.child.poll()
        except OSError as error:
            self.poll_errors.append({"errno": error.errno})
            return None
        if status is not None:
            self.terminal_confirmed = True
        return status

    def request_stop(self, reason: str) -> None:
        self.begin_cleanup()
        if self.stop_reason is not None:
            return
        self.stop_reason = reason
        if self.expired() or self.poll() is not None or self.poll_errors:
            return
        try:
            self.signal_group(self.child.pid, signal.SIGKILL)
            self.host_kill = True
        except OSError as error:
            self.signal_errors.append({"errno": error.errno})
            self.poll()  # Evidence now; completion by the deadline decides stop.

    def finish(self, streams: dict[str, dict[str, bool]], descendants_complete: bool) -> dict[str, object]:
        deadline = self.begin_cleanup()
        status = self.poll()
        if not self.expired() and status is None and not self.poll_errors:
            remaining = deadline - self.clock()
            if remaining > 0:
                try:
                    self.child.wait(timeout=remaining)
                except subprocess.TimeoutExpired:
                    self.wait_errors.append({"timeout": True})
                except OSError as error:
                    self.wait_errors.append({"errno": error.errno})
                self.poll()
        self.expired()
        cleanup_complete = self.terminal_confirmed and len(streams) == 2 and all(stream["eof"] and stream["closed"] for stream in streams.values()) and descendants_complete
        failed = self.deadline_failed or bool(self.poll_errors or self.wait_errors) or not cleanup_complete
        return {"stop_reason": self.stop_reason, "signal_errors": self.signal_errors, "poll_errors": self.poll_errors, "wait_errors": self.wait_errors, "stop_failed": bool(failed), "deadline_failed": self.deadline_failed, "deadline": deadline, "host_kill": self.host_kill, "terminal_confirmed": self.terminal_confirmed, "descendants_complete": descendants_complete, "cleanup_complete": bool(cleanup_complete)}


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def reference() -> tuple[ModuleType, dict[str, object]]:
    if digest(PREVIOUS_ROOT / "build.json") != PREVIOUS_SHA or digest(PREVIOUS_ROOT / "result.json") != PREVIOUS_RESULT_SHA:
        raise ValueError("reviewed previous stop history differs")
    manifest = json.loads(inside(PREVIOUS_ROOT / "build.json").read_text())
    code = inside(PREVIOUS_SOURCE).read_bytes()
    if hashlib.sha256(code).hexdigest() != manifest["harness"]:
        raise ValueError("reviewed previous harness differs")
    specification = importlib.util.spec_from_file_location("lo_reviewed_resource_controller", inside(PREVIOUS_SOURCE))
    if specification is None:
        raise ValueError("previous module specification is unavailable")
    module = importlib.util.module_from_spec(specification)
    sys.modules[specification.name] = module
    exec(compile(code, str(PREVIOUS_SOURCE), "exec"), module.__dict__)
    original, _, actual = module.verify(PREVIOUS_SHA)
    history = json.loads(inside(PREVIOUS_ROOT / "result.json").read_text())
    previous = history["cases"]
    if len(previous) != 1 or previous[0]["case"] != "stdout-overflow" or previous[0]["passed"] or not previous[0]["controller"]["stop_failed"] or not previous[0]["cleanup_complete"] or history["candidate_cases_passed"]:
        raise ValueError("previous failed completion history differs")
    if history["post_run_artifacts"] != actual or history["fixture_inputs"] != history["fixture_outputs"]:
        raise ValueError("previous integrity history differs")
    for case in previous:
        if not case["artifacts_unchanged"] or not case["fixtures_unchanged"]:
            raise ValueError("previous integrity evidence differs")
        for name, evidence in case["streams"].items():
            if not evidence["eof"] or not evidence["closed"] or digest(PREVIOUS_ROOT / "cases" / f"{case['case']}.{name}") != evidence["sha256"]:
                raise ValueError("previous captured evidence differs")
    return original, history


def prepare() -> None:
    reference()
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior stop-completion result before preparation")
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "cases").mkdir(exist_ok=True)


def seal() -> None:
    reference()
    inside(ROOT / "build.json").write_text(json.dumps({name: digest(path) for name, path in ARTIFACTS.items()}, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> tuple[ModuleType, dict[str, object], dict[str, str]]:
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed stop-completion manifest differs")
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed stop-completion artifacts differ")
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
    process = controller = None
    reader = writer = None
    start = time.monotonic()
    deadline = start + original.EXECUTION_TIMEOUT
    usage_before = resource.getrusage(resource.RUSAGE_CHILDREN)

    def close_fd(descriptor: int) -> None:
        try:
            os.close(descriptor)
        except OSError as error:
            report["cleanup_errors"].append({"operation": "close-control", "errno": error.errno})

    def stop(reason: str) -> None:
        if controller is not None:
            controller.request_stop(reason)

    try:
        reader, writer = os.pipe()
        if not 3 <= reader < original.FD_LIMIT:
            raise ValueError("control reader exceeds inherited descriptor range")
        process = subprocess.Popen([str(original.ROOT / "launcher"), str(reader), "values"], cwd=inside(original.ROOT), env={"CI": "1", "TMPDIR": str(original.ROOT)}, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True, pass_fds=(reader,), start_new_session=True, preexec_fn=original.child_limits)
        controller = CompletionController(process, os.killpg, cleanup_timeout=original.CLEANUP_TIMEOUT)
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
            if controller.deadline is None and now >= deadline:
                report["timeout"] = True
                stop("timeout")
            if controller.expired():
                report["cleanup_errors"].append({"operation": "drain-or-wait", "timeout": True})
                break
            end = controller.deadline if controller.deadline is not None else deadline
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
                    try:
                        key.fileobj.close()
                    except OSError as error:
                        report["cleanup_errors"].append({"operation": "close-" + stream_name, "errno": error.errno})
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
        if controller is not None:
            controller.begin_cleanup()
            if controller.poll() is None:
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
            # These six sealed target branches contain no process-creation path;
            # the pinned original history also records creation denials. This
            # source-based fixture fact is not a general OS descendant query.
            report["controller"] = controller.finish(ownership, descendants_complete=True)
        else:
            report["controller"] = {"cleanup_complete": not report["cleanup_errors"], "stop_failed": False, "deadline_failed": False, "terminal_confirmed": False, "signal_errors": [], "poll_errors": [], "wait_errors": [], "host_kill": False, "stop_reason": None}
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
    healthy = report["cleanup_complete"] and not state["stop_failed"] and "transport_error" not in report and "control_error_errno" not in report
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
    # A passed overflow case confirms a host-limit outcome and completed cleanup;
    # it never changes that activation failure into successful process output.
    report["primary_trigger"] = "output_limit" if report["output_limit"] else "timeout" if report["timeout"] else "transport" if "transport_error" in report or "control_error_errno" in report else None
    report["passed"] = bool(passed)
    unexpected_signal = isinstance(report["exit_code"], int) and report["exit_code"] < 0 and not (report["output_limit"] and state["host_kill"] and report["exit_code"] == -signal.SIGKILL)
    report["stop_suite"] = bool(not healthy or unexpected_signal)
    return report


def run(expected: str) -> int:
    original, history, reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior completion result before another suite")
    for name in CASE_NAMES:
        for stream in ("stdout", "stderr"):
            if inside(ROOT / "cases" / f"{name}.{stream}").exists():
                raise ValueError("preserve prior completion captures")
    if any(inside(original.ROOT / name).read_bytes() != content for name, content in original.FIXTURES.items()):
        raise ValueError("original fixture baseline differs")
    before = {name: digest(original.ROOT / name) for name in original.FIXTURES}
    report = {"scope": "Completion-based stop candidate and six fixed resource cases only. Original CPU-hard, tail, and immediate-poll-controller failures remain unchanged; no full native Process qualification.", "descendant_scope": "Completion is established only from the fixed reviewed no-creation target branches and pinned prior creation-denial observations. No general OS descendant query or descendant-enforcement claim.", "qualified_process_backend": False, "reviewed_manifest_sha256": expected, "previous_manifest_sha256": PREVIOUS_SHA, "previous_result_sha256": PREVIOUS_RESULT_SHA, "original_manifest_sha256": history["original_manifest_sha256"], "original_result_sha256": history["original_result_sha256"], "tail_manifest_sha256": history["tail_manifest_sha256"], "tail_result_sha256": history["tail_result_sha256"], "limits": history["limits"], "startup_limit": history["startup_limit"], "fixture_inputs": before, "cases": []}
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
