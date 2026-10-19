"""Review-gated preparation of an owned, stopped debugger diagnostic wrapper."""
from __future__ import annotations

import hashlib
from contextlib import ExitStack
from collections.abc import Callable
import json
import os
from pathlib import Path
import re
import resource
import signal
import socket
import subprocess
import sys
import time

PREEXEC_REVIEWED_MANIFEST = "72d4e0db509f94a3097c29fdcbd5d73ab9b5c9ae09aa598ac6716c40d37eaa9b"
REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-debugger"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/debugger"
PREEXEC = REPOSITORY / "_scratch/adapter-implementation-x921/native-preexec"
PREEXEC_SOURCE = REPOSITORY / "experiments/process-preexec"
DEBUGSERVER = "/Applications/Xcode.app/Contents/SharedFrameworks/LLDB.framework/Resources/debugserver"
LLDB = "/usr/bin/lldb"
LSOF = "/usr/sbin/lsof"
OUTPUT_LIMIT = 64 << 10
TOTAL_TIMEOUT = 15
OPERATION_TIMEOUT = 10
PREEXEC_ARTIFACTS = {
    "target_source": PREEXEC_SOURCE / "target.c",
    "launcher_source": PREEXEC_SOURCE / "launcher.c",
    "harness": PREEXEC_SOURCE / "probe.py",
    "verification_test": PREEXEC_SOURCE / "test_probe.py",
    "build": PREEXEC_SOURCE / "probe.mk",
    "profile": PREEXEC / "candidate.sb",
    "header": PREEXEC / "fixture_paths.h",
    "target_binary": PREEXEC / "target",
    "launcher_binary": PREEXEC / "launcher",
}
ARTIFACTS = {
    "wrapper_source": SOURCE / "launcher.c",
    "harness": SOURCE / "probe.py",
    "verification_test": SOURCE / "test_probe.py",
    "build": SOURCE / "probe.mk",
    "controller": SOURCE / "controller.py",
    "configuration_template": SOURCE / "settings.lldb.in",
    "configuration": ROOT / "settings.lldb",
    "wrapper_binary": ROOT / "launcher",
    "preexec_manifest": PREEXEC / "build.json",
    **{f"preexec_{name}": path for name, path in PREEXEC_ARTIFACTS.items()},
}


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def verify_preexec() -> None:
    if digest(PREEXEC / "build.json") != PREEXEC_REVIEWED_MANIFEST:
        raise ValueError("immutable preexec manifest differs")
    expected = json.loads(inside(PREEXEC / "build.json").read_text())
    actual = {name: digest(path) for name, path in PREEXEC_ARTIFACTS.items()}
    if actual != expected:
        raise ValueError("immutable preexec artifacts differ")


def prepare() -> None:
    verify_preexec()
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    for name in ("home", "tmp", "cache", "index", "swift", "clang", "cas", "modules", "sessions"):
        inside(ROOT / name).mkdir(exist_ok=True)
    template = inside(SOURCE / "settings.lldb.in").read_text()
    escaped_root = json.dumps(str(inside(ROOT)), ensure_ascii=False)[1:-1]
    inside(ROOT / "settings.lldb").write_text(template.replace("@ROOT@", escaped_root))


def seal() -> None:
    verify_preexec()
    manifest = {name: digest(path) for name, path in ARTIFACTS.items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    verify_preexec()
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed debugger manifest differs")
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed debugger inputs or wrapper differ")
    return actual


def target_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (OUTPUT_LIMIT, OUTPUT_LIMIT))
    resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))


def tool_limits() -> None:
    target_limits()
    resource.setrlimit(resource.RLIMIT_CPU, (5, 5))


def bounded_file(path: Path) -> bytes:
    with inside(path).open("rb") as stream:
        data = stream.read(OUTPUT_LIMIT + 1)
    if len(data) > OUTPUT_LIMIT:
        raise ValueError("diagnostic output exceeded 64 KiB")
    return data


def run(expected: str) -> int:
    reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior debugger result before another run")
    controller_output = inside(ROOT / "controller.json")
    if controller_output.exists():
        controller_output.unlink()
    started = time.monotonic()
    operation_deadline = started + OPERATION_TIMEOUT
    final_deadline = started + TOTAL_TIMEOUT
    owned: list[subprocess.Popen[bytes]] = []
    report: dict[str, object] = {"qualified_process_backend": False, "scope": "Debugger capture only; diagnostic_completed does not establish SIGABRT reproduction or Process qualification.", "reviewed_manifest_sha256": expected, "timeout_seconds": TOTAL_TIMEOUT, "target_limits": {"core_dump_limit": 0, "output_limit_per_stream": OUTPUT_LIMIT, "file_descriptor_limit": 64, "cpu_limit_modified": False}, "tool_limits": {"core_dump_limit": 0, "output_limit_per_stream": OUTPUT_LIMIT, "file_descriptor_limit": 64, "cpu_limit_seconds": 5}}
    fixture_names = ("allowed-read", "denied-read", "allowed-write", "denied-write")
    fixture_expected = {"allowed-read": b"allowed-fixture", "denied-read": b"denied-fixture", "allowed-write": b"allowed-baseline", "denied-write": b"denied-baseline"}
    for name, content in fixture_expected.items():
        if inside(PREEXEC / name).read_bytes() != content:
            raise ValueError(f"immutable fixture input differs: {name}")
    report["fixture_inputs"] = {name: digest(PREEXEC / name) for name in fixture_names}
    environment = {"CI": "1", "CFFIXED_USER_HOME": str(ROOT / "home"), "TMPDIR": str(ROOT / "tmp"), "TMP": str(ROOT / "tmp"), "TEMP": str(ROOT / "tmp"), "XDG_CACHE_HOME": str(ROOT / "cache"), "CLANG_MODULE_CACHE_PATH": str(ROOT / "clang"), "PYTHONDONTWRITEBYTECODE": "1"}
    target_environment = {"CI": "1", "TMPDIR": str(PREEXEC), "LO_PREEXEC_SENTINEL": "explicit"}

    def remaining(maximum: float) -> float:
        duration = min(maximum, operation_deadline - time.monotonic())
        if duration <= 0:
            raise TimeoutError("debugger operation deadline exceeded")
        return duration

    with ExitStack() as resources:
        def spawn(arguments: list[str], stdout_path: Path, stderr_path: Path, cwd: Path, child_environment: dict[str, str], limits: Callable[[], None] = tool_limits) -> subprocess.Popen[bytes]:
            remaining(1)
            stdout = resources.enter_context(inside(stdout_path).open("wb"))
            stderr = resources.enter_context(inside(stderr_path).open("wb"))
            process = subprocess.Popen(arguments, cwd=inside(cwd), env=child_environment, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr, close_fds=True, start_new_session=True, preexec_fn=limits)
            owned.append(process)
            return process

        try:
            listener = resources.enter_context(socket.socket(socket.AF_INET, socket.SOCK_STREAM))
            listener.bind(("127.0.0.1", 0))
            listener.listen(1)
            listener.setblocking(False)
            target = spawn([str(ROOT / "launcher"), str(listener.getsockname()[1])], PREEXEC / "stdout", PREEXEC / "stderr", PREEXEC, target_environment, limits=target_limits)
            ready_deadline = time.monotonic() + remaining(3)
            stopped = False
            while time.monotonic() < ready_deadline:
                child_pid, status = os.waitpid(target.pid, os.WNOHANG | os.WUNTRACED)
                if child_pid == target.pid:
                    if os.WIFSTOPPED(status):
                        stopped = os.WSTOPSIG(status) == signal.SIGSTOP
                        break
                    target.returncode = os.waitstatus_to_exitcode(status)
                    raise ValueError("owned wrapper exited before debugger stop")
                time.sleep(0.02)
            if not stopped or b"LO_DEBUG_READY\n" not in bounded_file(PREEXEC / "stderr").splitlines(keepends=True):
                raise ValueError("owned wrapper did not confirm ready SIGSTOP")
            report["wrapper_pid"] = target.pid
            report["ready_stopped"] = True
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as reservation:
                reservation.bind(("127.0.0.1", 0))
                port = reservation.getsockname()[1]
            server = spawn([DEBUGSERVER, f"127.0.0.1:{port}", f"--attach={target.pid}", "--kill-on-error", f"--log-file={ROOT / 'debugserver.log'}"], ROOT / "debugserver.stdout", ROOT / "debugserver.stderr", ROOT, environment)
            listener_deadline = time.monotonic() + remaining(3)
            listener_verified = False
            while time.monotonic() < listener_deadline:
                if server.poll() is not None:
                    raise ValueError("owned debugserver exited before listener confirmation")
                check = spawn([LSOF, "-nP", "-a", "-p", str(server.pid), "-iTCP", "-sTCP:LISTEN", "-Fpn"], ROOT / "listener.stdout", ROOT / "listener.stderr", ROOT, environment)
                check.wait(timeout=remaining(1))
                lines = bounded_file(ROOT / "listener.stdout").decode("ascii", errors="strict").splitlines()
                pids = [line[1:] for line in lines if line.startswith("p")]
                names = [line[1:] for line in lines if line.startswith("n")]
                if names:
                    if check.returncode != 0 or pids != [str(server.pid)] or names != [f"127.0.0.1:{port}"]:
                        raise ValueError("debugserver listener identity or address differs")
                    listener_verified = True
                    break
                time.sleep(0.02)
            if not listener_verified:
                raise ValueError("owned debugserver listener deadline exceeded")
            report["listener_verified"] = True
            report["debugserver_pid"] = server.pid
            report["debugserver_port"] = port
            controller_call = "script import runpy; runpy.run_path(" + repr(str(SOURCE / "controller.py")) + ")[\"capture\"](lldb.debugger," + str(target.pid) + "," + str(port) + "," + repr(str(ROOT / "controller.json")) + ")"
            debugger = spawn([LLDB, "--no-lldbinit", "--batch", "--no-use-colors", "--source", str(ROOT / "settings.lldb"), "--one-line", "target create --no-dependents " + json.dumps(str(ROOT / "launcher")), "--one-line", controller_call, "--one-line", "quit"], ROOT / "lldb.stdout", ROOT / "lldb.stderr", ROOT, environment)
            debugger.wait(timeout=remaining(6))
            report["lldb_exit_code"] = debugger.returncode
            report["controller"] = json.loads(bounded_file(ROOT / "controller.json"))
            try:
                connection, _ = listener.accept()
                connection.close()
                report["target_loopback_connection_observed"] = True
            except BlockingIOError:
                report["target_loopback_connection_observed"] = False
        except (OSError, ValueError, TimeoutError, subprocess.TimeoutExpired) as error:
            report["error"] = str(error)[:512]
        finally:
            signal_errors = []
            for process in reversed(owned):
                if process.poll() is None:
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    except OSError as error:
                        signal_errors.append({"pid": process.pid, "errno": error.errno})
            cleanup_errors = []
            wait_errors = []
            for process in reversed(owned):
                try:
                    process.wait(timeout=max(0.01, final_deadline - time.monotonic()))
                except subprocess.TimeoutExpired:
                    cleanup_errors.append(process.pid)
                except OSError as error:
                    cleanup_errors.append(process.pid)
                    wait_errors.append({"pid": process.pid, "errno": error.errno})
            report["cleanup_signal_errors"] = signal_errors
            report["cleanup_wait_errors"] = wait_errors
            report["owned_children_joined"] = not cleanup_errors
            report["unjoined_owned_pids"] = cleanup_errors
            report["owned_exit_codes"] = [{"pid": process.pid, "exit_code": process.returncode} for process in owned]
    report["elapsed_seconds"] = round(time.monotonic() - started, 3)
    for name in ("stdout", "stderr"):
        report[f"target_{name}"] = bounded_file(PREEXEC / name).decode("utf-8", errors="replace")
    report["phase_markers"] = [line for line in str(report["target_stderr"]).splitlines() if line == "LO_DEBUG_READY" or re.fullmatch(r"LO_PHASE (?:launcher|target)\.[a-z-]+", line)]
    report["fixture_outputs"] = {name: digest(PREEXEC / name) for name in fixture_names}
    report["nonwriting_fixtures_unchanged"] = all(report["fixture_inputs"][name] == report["fixture_outputs"][name] for name in ("allowed-read", "denied-read", "denied-write"))
    try:
        report["post_run_artifacts"] = verify(expected)
    except (OSError, ValueError) as error:
        report["post_run_artifacts"] = None
        report["integrity_error"] = str(error)[:256]
    report["reviewed_artifacts_unchanged"] = report["post_run_artifacts"] == reviewed
    controller = report.get("controller")
    passed = isinstance(controller, dict) and "error" not in controller and controller.get("connected") is True and controller.get("continued") is True and controller.get("destroyed") is True and report.get("lldb_exit_code") == 0 and report["owned_children_joined"] and not report["cleanup_signal_errors"] and not report["cleanup_wait_errors"] and report["reviewed_artifacts_unchanged"] and report["nonwriting_fixtures_unchanged"] and "error" not in report
    report["diagnostic_completed"] = bool(passed)
    with inside(ROOT / "result.json").open("x") as destination:
        destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if passed else 1


def main() -> int:
    arguments = sys.argv[1:]
    if arguments and arguments[0] in ("verify", "run"):
        if len(arguments) != 2 or re.fullmatch(r"[0-9a-f]{64}", arguments[1]) is None:
            raise ValueError("reviewed manifest SHA256 is required")
    if Path.cwd().resolve() != inside(REPOSITORY):
        raise ValueError("run from the Lo repository")
    if arguments == ["prepare"]:
        prepare()
        return 0
    if arguments == ["seal"]:
        seal()
        return 0
    if len(arguments) == 2 and arguments[0] in ("verify", "run"):
        if arguments[0] == "run":
            return run(arguments[1])
        verify(arguments[1])
        print(json.dumps({"reviewed_build_verified": True}))
        return 0
    raise ValueError("expected prepare, seal, verify SHA256, or run SHA256")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(2)
