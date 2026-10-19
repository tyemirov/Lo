"""Review-gated finite resource measurements with fixed trusted native helpers."""
from __future__ import annotations

from dataclasses import dataclass
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import selectors
import signal
import struct
import subprocess
import sys
import time

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-resources"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources"
REFERENCE = REPOSITORY / "_scratch/adapter-implementation-x921/native-environment"
REFERENCE_SHA = "4aa83a61e9fcabd98ccf0769a0459718f0881495cbc46c9bd0bb450319fbceab"
LAUNCHER = REPOSITORY / "experiments/process-environment/launcher.c"
OUTPUT_LIMIT = 65536
FD_LIMIT = 64
EXECUTION_TIMEOUT = 5.0
CLEANUP_TIMEOUT = 2.0
WALL_TIMEOUT = 0.5
PHASES = ("launcher.entered", "launcher.environment-exact", "launcher.before-sandbox-init", "launcher.after-sandbox-init", "launcher.before-environment-eof", "launcher.environment-received", "launcher.control-closed", "launcher.before-execve")
PREFIX = b"".join(("LO_PHASE " + phase + "\n").encode() for phase in PHASES) + b"LO_RESOURCE target-entered\n"
FIXTURES = {"allowed-read": b"allowed-fixture", "denied-read": b"denied-fixture", "allowed-write": b"allowed-baseline", "denied-write": b"denied-baseline"}
ARTIFACTS = {"harness": SOURCE / "probe.py", "test": SOURCE / "test_probe.py", "build": SOURCE / "probe.mk", "launcher_source": LAUNCHER, "target_source": SOURCE / "target.c", "launcher_binary": ROOT / "launcher", "target_binary": ROOT / "target", "profile": ROOT / "candidate.sb", "header": ROOT / "fixture_paths.h", "reference_manifest": REFERENCE / "build.json", "reference_profile": REFERENCE / "candidate.sb"}


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def selection() -> tuple[str, str]:
    if digest(REFERENCE / "build.json") != REFERENCE_SHA:
        raise ValueError("reviewed reference manifest differs")
    manifest = json.loads(inside(REFERENCE / "build.json").read_text())
    if digest(REFERENCE / "candidate.sb") != manifest["profile"] or digest(LAUNCHER) != manifest["launcher_source"]:
        raise ValueError("reviewed reference profile or launcher differs")
    profile = inside(REFERENCE / "candidate.sb").read_text()
    library_rule = "(allow file-read* (literal " + json.dumps(str(REFERENCE / "constructor.dylib")) + "))\n"
    if profile.count(library_rule) != 1:
        raise ValueError("reference constructor grant is not exact")
    profile = profile.replace(library_rule, "").replace(str(REFERENCE), str(ROOT))
    header = "\n".join(f"#define {name} {json.dumps(content)}" for name, content in {"LO_PROFILE": profile, "LO_TARGET": str(ROOT / "target"), "LO_ROOT": str(ROOT)}.items()) + "\n"
    header += f"#define LO_FD_LIMIT {FD_LIMIT}\n#define LO_OUTPUT_LIMIT {OUTPUT_LIMIT}\n#define LO_CAPTURE_PREFIX_BYTES {len(PREFIX)}\n"
    return profile, header


def prepare() -> None:
    profile, header = selection()
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior resource results before preparation")
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "cases").mkdir(exist_ok=True)
    inside(ROOT / "candidate.sb").write_text(profile)
    inside(ROOT / "fixture_paths.h").write_text(header)
    for name, content in FIXTURES.items():
        inside(ROOT / name).write_bytes(content)


def selected() -> None:
    profile, header = selection()
    if inside(ROOT / "candidate.sb").read_text() != profile or inside(ROOT / "fixture_paths.h").read_text() != header:
        raise ValueError("resource profile or header differs")


def seal() -> None:
    selected()
    inside(ROOT / "build.json").write_text(json.dumps({name: digest(path) for name, path in ARTIFACTS.items()}, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    selected()
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed resource manifest differs")
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed resource artifacts differ")
    return actual


@dataclass(frozen=True)
class Case:
    name: str
    operation: str
    control: bool = False


def cases() -> tuple[Case, ...]:
    return tuple(Case("control-" + api, api, True) for api in ("fork", "vfork", "spawn")) + tuple(Case(api, api) for api in ("fork", "vfork", "spawn")) + tuple(Case(name, name) for name in ("cpu-soft", "cpu-hard", "wall", "stdout-exact", "stdout-overflow", "stderr-exact", "stderr-overflow", "dual-pressure", "fd", "limits"))


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (OUTPUT_LIMIT, OUTPUT_LIMIT))
    resource.setrlimit(resource.RLIMIT_NOFILE, (FD_LIMIT, FD_LIMIT))
    resource.setrlimit(resource.RLIMIT_CPU, (1, 2))


def control_frame(operation: str) -> bytes:
    entry = ("LO_RESOURCE_CASE=" + operation).encode()
    body = struct.pack(">I", len(entry)) + entry
    return b"LOE1" + struct.pack(">II", len(body), 1) + body


def run_case(case: Case) -> dict[str, object]:
    report: dict[str, object] = {"case": case.name, "unconfined_control": case.control, "timeout": False, "output_limit": False, "host_kill": False, "cleanup_errors": []}
    captured = {"stdout": bytearray(), "stderr": bytearray()}
    counts = {"stdout": 0, "stderr": 0}
    stream_eof = {"stdout": False, "stderr": False}
    streams = {}
    selector = selectors.DefaultSelector()
    process = None
    reader = None
    writer = None
    start = time.monotonic()
    deadline = start + (WALL_TIMEOUT if case.operation == "wall" else EXECUTION_TIMEOUT)
    cleanup_deadline = None
    usage_before = resource.getrusage(resource.RUSAGE_CHILDREN)

    def close_fd(descriptor: int) -> None:
        try:
            os.close(descriptor)
        except OSError as error:
            report["cleanup_errors"].append({"operation": "close-control", "errno": error.errno})

    def stop() -> None:
        nonlocal cleanup_deadline
        if cleanup_deadline is None:
            cleanup_deadline = time.monotonic() + CLEANUP_TIMEOUT
        if process is not None and process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
                report["host_kill"] = True
            except ProcessLookupError:
                pass
            except OSError as error:
                report["cleanup_errors"].append({"operation": "kill-owned-group", "errno": error.errno})

    try:
        if case.control:
            argv = [str(ROOT / "target"), "values", "fixed-argument"]
            environment = {"LO_RESOURCE_CASE": case.operation}
            inherited = ()
        else:
            reader, writer = os.pipe()
            if not 3 <= reader < FD_LIMIT:
                raise ValueError("control reader exceeds inherited descriptor range")
            argv = [str(ROOT / "launcher"), str(reader), "values"]
            environment = {"CI": "1", "TMPDIR": str(ROOT)}
            inherited = (reader,)
        # Popen is synchronous: the absolute deadline includes it, but cannot
        # interrupt process creation. This suite does not qualify that boundary.
        process = subprocess.Popen(argv, cwd=inside(ROOT), env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True, pass_fds=inherited, start_new_session=True, preexec_fn=child_limits)
        streams = {"stdout": process.stdout, "stderr": process.stderr}
        if reader is not None:
            close_fd(reader)
            reader = None
        for name, stream in streams.items():
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, name)
        payload = control_frame(case.operation)
        sent = 0
        if writer is not None:
            os.set_blocking(writer, False)
            selector.register(writer, selectors.EVENT_WRITE, "control")
        while selector.get_map() or process.poll() is None:
            now = time.monotonic()
            if cleanup_deadline is None and now >= deadline:
                report["timeout"] = True
                stop()
            if cleanup_deadline is not None and now >= cleanup_deadline:
                report["cleanup_errors"].append({"operation": "drain-or-wait", "timeout": True})
                break
            end = cleanup_deadline if cleanup_deadline is not None else deadline
            events = selector.select(min(0.05, max(0, end - now)))
            for key, _ in events:
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
                        stop()
                    continue
                name = key.data
                allowance = OUTPUT_LIMIT - len(captured[name])
                try:
                    # Detect the first excess byte precisely, then drain in
                    # bounded chunks without retaining additional payload.
                    data = os.read(key.fd, 4096 if report["output_limit"] else min(4096, allowance + 1))
                except (BlockingIOError, InterruptedError):
                    continue
                if not data:
                    stream_eof[name] = True
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                counts[name] += len(data)
                captured[name].extend(data[:allowance])
                if counts[name] > OUTPUT_LIMIT and not report["output_limit"]:
                    report["output_limit"] = True
                    stop()
        report["control_bytes_written"] = sent if not case.control else 0
    except Exception as error:
        report["transport_error"] = str(error)[:256]
        stop()
    finally:
        if process is not None and process.poll() is None:
            stop()
        for descriptor in (reader, writer):
            if descriptor is not None:
                close_fd(descriptor)
        try:
            selector.close()
        except OSError as error:
            report["cleanup_errors"].append({"operation": "close-selector", "errno": error.errno})
        for name, stream in streams.items():
            try:
                stream.close()
            except OSError as error:
                report["cleanup_errors"].append({"operation": "close-" + name, "errno": error.errno})
        if process is not None:
            remaining = max(0.001, (cleanup_deadline or (time.monotonic() + CLEANUP_TIMEOUT)) - time.monotonic())
            try:
                process.wait(timeout=remaining)
            except (OSError, subprocess.TimeoutExpired) as error:
                report["cleanup_errors"].append({"operation": "wait", "error": str(error)[:128]})
        report["owned_child_joined"] = process is None or process.returncode is not None
        report["exit_code"] = process.returncode if process is not None else None
    usage_after = resource.getrusage(resource.RUSAGE_CHILDREN)
    report["cpu_seconds"] = usage_after.ru_utime + usage_after.ru_stime - usage_before.ru_utime - usage_before.ru_stime
    report["elapsed_seconds"] = time.monotonic() - start
    report["streams"] = {}
    for name, data in captured.items():
        with inside(ROOT / "cases" / f"{case.name}.{name}").open("xb") as destination:
            destination.write(data)
        report["streams"][name] = {"observed_bytes": counts[name], "captured_bytes": len(data), "eof": stream_eof[name], "closed": name in streams and streams[name].closed, "sha256": hashlib.sha256(data).hexdigest(), "preview": bytes(data[:512]).decode("utf-8", errors="replace")}
    stdout = bytes(captured["stdout"])
    stderr = bytes(captured["stderr"])
    try:
        observation = json.loads(stdout) if stdout else None
    except (ValueError, UnicodeDecodeError):
        observation = None
    report["observations"] = observation
    report["phase_markers"] = [line for line in stderr.decode("utf-8", errors="replace").splitlines() if line.startswith(("LO_PHASE ", "LO_RESOURCE "))][:20]
    healthy = report["owned_child_joined"] and all(item["eof"] and item["closed"] for item in report["streams"].values()) and not report["cleanup_errors"] and "transport_error" not in report and "control_error_errno" not in report
    expected_prefix = b"LO_RESOURCE target-entered\n" if case.control else PREFIX
    ready = stderr.startswith(expected_prefix)
    ordinary = healthy and ready and report["exit_code"] == 0 and not report["timeout"] and not report["output_limit"] and not report["host_kill"]
    if case.operation in ("fork", "vfork", "spawn"):
        passed = ordinary and observation == {"api": case.operation, "error": 0 if case.control else errno.EPERM, "child_created": case.control, "child_reaped": case.control}
    elif case.operation == "cpu-soft":
        passed = ordinary and observation == {"cpu_soft_observed": True} and b"LO_RESOURCE cpu-soft\n" in stderr
    elif case.operation == "cpu-hard":
        passed = healthy and ready and b"LO_RESOURCE cpu-hard-ready\n" in stderr and report["exit_code"] == -signal.SIGKILL and report["cpu_seconds"] >= 1 and not report["host_kill"] and not report["timeout"] and not report["output_limit"]
    elif case.operation == "wall":
        passed = healthy and ready and b"LO_RESOURCE wall-ready\n" in stderr and report["timeout"] and report["host_kill"] and report["exit_code"] == -signal.SIGKILL and not report["output_limit"]
    elif case.operation == "stdout-exact":
        passed = ordinary and stdout == b"o" * OUTPUT_LIMIT and stderr == expected_prefix
    elif case.operation == "stderr-exact":
        passed = ordinary and stdout == b"" and stderr == expected_prefix + b"e" * (OUTPUT_LIMIT - len(expected_prefix))
    elif case.operation in ("stdout-overflow", "stderr-overflow", "dual-pressure"):
        if case.operation == "stdout-overflow":
            payload_exact = stdout == b"o" * OUTPUT_LIMIT and stderr == expected_prefix and counts["stdout"] > OUTPUT_LIMIT
        elif case.operation == "stderr-overflow":
            payload_exact = not stdout and stderr == expected_prefix + b"e" * (OUTPUT_LIMIT - len(expected_prefix)) and counts["stderr"] > OUTPUT_LIMIT
        else:
            payload_exact = bool(stdout) and stdout == b"o" * len(stdout) and stderr == expected_prefix + b"e" * (len(stderr) - len(expected_prefix)) and any(count > OUTPUT_LIMIT for count in counts.values())
        expected_exit = report["exit_code"] == 0 or report["exit_code"] == -signal.SIGKILL and report["host_kill"]
        passed = healthy and ready and payload_exact and expected_exit and report["output_limit"] and not report["timeout"]
    elif case.operation == "fd":
        passed = ordinary and observation == {"error": errno.EMFILE, "duplicates": 61, "closed": True}
    else:
        passed = ordinary and observation == {"limits_exact": True, "hard_raises_denied": True}
    report["passed"] = bool(passed)
    unexpected_signal = isinstance(report["exit_code"], int) and report["exit_code"] < 0 and not (case.operation == "cpu-hard" and passed or case.operation == "wall" and passed or report["output_limit"] and report["host_kill"] and report["exit_code"] == -signal.SIGKILL)
    report["stop_suite"] = bool(not healthy or unexpected_signal or case.control and not passed)
    return report


def run(expected: str) -> int:
    reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior resource result before another suite")
    for case in cases():
        for stream in ("stdout", "stderr"):
            if inside(ROOT / "cases" / f"{case.name}.{stream}").exists():
                raise ValueError("preserve prior resource case captures")
    before = {name: digest(ROOT / name) for name in FIXTURES}
    if any(inside(ROOT / name).read_bytes() != content for name, content in FIXTURES.items()):
        raise ValueError("resource fixture baseline differs")
    report = {"scope": "Finite fixed-helper resource and process-creation measurements only; no full native Process qualification or standard-host activation.", "startup_limit": "Synchronous Popen cannot be independently interrupted; the absolute deadline starts before it.", "cleanup_scope": "Owned primary/group while unreaped; reviewed creation controls immediately exit and reap their child.", "qualified_process_backend": False, "reviewed_manifest_sha256": expected, "reference_manifest_sha256": REFERENCE_SHA, "limits": {"core": [0, 0], "fsize": [OUTPUT_LIMIT, OUTPUT_LIMIT], "nofile": [FD_LIMIT, FD_LIMIT], "cpu": [1, 2], "execution_seconds": EXECUTION_TIMEOUT, "wall_seconds": WALL_TIMEOUT, "cleanup_seconds": CLEANUP_TIMEOUT, "capture_bytes_per_stream": OUTPUT_LIMIT}, "fixture_inputs": before, "cases": []}
    for case in cases():
        result = run_case(case)
        try:
            result["artifacts_unchanged"] = verify(expected) == reviewed
            result["fixtures_unchanged"] = {name: digest(ROOT / name) for name in FIXTURES} == before
        except (OSError, ValueError) as error:
            result["integrity_error"] = str(error)[:256]
            result["artifacts_unchanged"] = False
            result["fixtures_unchanged"] = False
        report["cases"].append(result)
        if result["stop_suite"] or not result["artifacts_unchanged"] or not result["fixtures_unchanged"]:
            break
    try:
        report["post_run_artifacts"] = verify(expected)
        report["fixture_outputs"] = {name: digest(ROOT / name) for name in FIXTURES}
    except (OSError, ValueError) as error:
        report["integrity_error"] = str(error)[:256]
    report["suite_passed"] = len(report["cases"]) == len(cases()) and all(result["passed"] and result["artifacts_unchanged"] and result["fixtures_unchanged"] for result in report["cases"]) and report.get("post_run_artifacts") == reviewed and report.get("fixture_outputs") == before
    with inside(ROOT / "result.json").open("x") as destination:
        destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if report["suite_passed"] else 1


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
