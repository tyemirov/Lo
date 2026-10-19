"""Review-gated environment transport qualification for fixed trusted helpers."""
from __future__ import annotations

from dataclasses import dataclass
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import select
import signal
import struct
import subprocess
import sys
import time

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-environment"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-environment"
REFERENCE = REPOSITORY / "_scratch/adapter-implementation-x921/native-cwd-identity"
REFERENCE_MANIFEST = "495ca0fc1d3fd53db5318e70b72e217e37b334c0f457ed7b3c63ef8bcdab7ad0"
PREEXEC = REPOSITORY / "_scratch/adapter-implementation-x921/native-preexec"
MAX_BODY = 4096
MAX_COUNT = 16
MAX_ENTRY = 1024
OUTPUT_LIMIT = 64 << 10
FD_LIMIT = 64
EXECUTION_TIMEOUT = 5.0
HOLD_TIMEOUT = 0.5
CLEANUP_TIMEOUT = 2.0
WRITE_CHUNK = 7
FIXTURES = {"allowed-read": b"allowed-fixture", "denied-read": b"denied-fixture", "allowed-write": b"allowed-baseline", "denied-write": b"denied-baseline"}
ARTIFACTS = {
    "harness": SOURCE / "probe.py", "verification_test": SOURCE / "test_probe.py",
    "build": SOURCE / "probe.mk", "launcher_source": SOURCE / "launcher.c",
    "target_source": SOURCE / "target.c", "constructor_source": SOURCE / "constructor.c",
    "launcher_binary": ROOT / "launcher", "target_binary": ROOT / "target",
    "constructor_binary": ROOT / "constructor.dylib", "profile": ROOT / "candidate.sb",
    "header": ROOT / "fixture_paths.h", "expected_directory": ROOT / "expected-directory.json",
    "reference_manifest": REFERENCE / "build.json", "reference_profile": REFERENCE / "candidate.sb",
}
INITIAL_PHASES = ("launcher.entered", "launcher.environment-exact", "launcher.before-sandbox-init", "launcher.after-sandbox-init")
VALID_PHASES = INITIAL_PHASES + ("launcher.before-environment-eof", "launcher.environment-received", "launcher.control-closed", "launcher.before-execve")
TARGET_PHASES = ("target.entered", "target.complete")


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def reference_profile() -> str:
    if digest(REFERENCE / "build.json") != REFERENCE_MANIFEST:
        raise ValueError("reviewed reference manifest differs")
    manifest = json.loads(inside(REFERENCE / "build.json").read_text())
    if digest(REFERENCE / "candidate.sb") != manifest["profile"]:
        raise ValueError("reviewed reference profile differs")
    return inside(REFERENCE / "candidate.sb").read_text()


def directory_identity() -> dict[str, int]:
    information = inside(ROOT).stat()
    return {"device": information.st_dev, "inode": information.st_ino}


def selected_inputs() -> tuple[str, str, dict[str, int]]:
    original = reference_profile()
    original_target = str(REFERENCE / "target")
    if original.count(original_target) != 2:
        raise ValueError("reference must have two exact target path grants")
    profile = original.replace(original_target, str(ROOT / "target")).replace(str(PREEXEC), str(ROOT))
    profile += "(allow file-read* (literal " + json.dumps(str(ROOT / "constructor.dylib")) + "))\n"
    identity = directory_identity()
    definitions = {"LO_PROFILE": profile, "LO_TARGET": str(ROOT / "target"), "LO_ROOT": str(ROOT), "LO_LIBRARY": str(ROOT / "constructor.dylib"), "LO_DENIED_WRITE": str(ROOT / "denied-write")}
    header = "\n".join(f"#define {name} {json.dumps(content)}" for name, content in definitions.items()) + "\n"
    header += f"#define LO_FD_LIMIT {FD_LIMIT}\n#define LO_EXPECTED_DEVICE {identity['device']}ULL\n#define LO_EXPECTED_INODE {identity['inode']}ULL\n"
    return profile, header, identity


def prepare() -> None:
    reference_profile()
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior environment result before preparation")
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "cases").mkdir(exist_ok=True)
    profile, header, identity = selected_inputs()
    inside(ROOT / "candidate.sb").write_text(profile)
    inside(ROOT / "fixture_paths.h").write_text(header)
    inside(ROOT / "expected-directory.json").write_text(json.dumps(identity, sort_keys=True) + "\n")
    for name, content in FIXTURES.items():
        inside(ROOT / name).write_bytes(content)


def verify_selection() -> None:
    profile, header, identity = selected_inputs()
    if inside(ROOT / "candidate.sb").read_text() != profile or inside(ROOT / "fixture_paths.h").read_text() != header:
        raise ValueError("environment profile or header differs from selected derivation")
    if json.loads(inside(ROOT / "expected-directory.json").read_text()) != identity:
        raise ValueError("environment directory identity differs")


def seal() -> None:
    verify_selection()
    manifest = {name: digest(path) for name, path in ARTIFACTS.items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    verify_selection()
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed environment manifest differs")
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed environment artifacts differ")
    return actual


def encoded_entries(entries: tuple[bytes, ...]) -> bytes:
    return b"".join(struct.pack(">I", len(entry)) + entry for entry in entries)


def frame(entries: tuple[bytes, ...]) -> bytes:
    body = encoded_entries(entries)
    return b"LOE1" + struct.pack(">II", len(body), len(entries)) + body


@dataclass(frozen=True)
class Case:
    name: str
    mode: str
    payload: bytes
    expectation: str
    hold_eof: bool = False


def cases() -> tuple[Case, ...]:
    loaded = tuple(entry.encode("utf-8") for entry in ("CI=1", f"TMPDIR={ROOT}", "LO_TARGET_SENTINEL=explicit", "LO_EMPTY=", f"DYLD_INSERT_LIBRARIES={ROOT / 'constructor.dylib'}"))
    values = tuple(entry.encode("utf-8") for entry in ("A0=after", "A=first=second", "UNICODE=λ\nline", "EMPTY="))
    complete = frame((b"A=value",))
    mismatched_body = encoded_entries((b"A=one", b"B=two"))
    return (
        Case("loaded", "loaded", frame(loaded), "loaded"),
        Case("empty", "empty", frame(()), "valid"),
        Case("values", "values", frame(values), "valid"),
        Case("truncated-header", "invalid", b"LOE1\x00\x00", "invalid"),
        Case("truncated-body", "invalid", complete[:-1], "invalid"),
        Case("oversize-body", "invalid", b"LOE1" + struct.pack(">II", MAX_BODY + 1, 0), "invalid"),
        Case("oversize-count", "invalid", b"LOE1" + struct.pack(">II", 0, MAX_COUNT + 1), "invalid"),
        Case("oversize-entry", "invalid", frame((b"A=" + b"x" * (MAX_ENTRY - 1),)), "invalid"),
        Case("missing-equals", "invalid", frame((b"NAME",)), "invalid"),
        Case("empty-name", "invalid", frame((b"=value",)), "invalid"),
        Case("nul", "invalid", frame((b"A=one\x00two",)), "invalid"),
        Case("invalid-utf8", "invalid", frame((b"A=\xff",)), "invalid"),
        Case("duplicate", "invalid", frame((b"A=one", b"A=two")), "invalid"),
        Case("trailing", "invalid", complete + b"x", "invalid"),
        Case("count-mismatch", "invalid", b"LOE1" + struct.pack(">II", len(mismatched_body), 1) + mismatched_body, "invalid"),
        Case("zero-length", "invalid", frame((b"",)), "invalid"),
        Case("hold-open-eof", "invalid", frame(()), "timeout", hold_eof=True),
    )


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (OUTPUT_LIMIT, OUTPUT_LIMIT))
    resource.setrlimit(resource.RLIMIT_NOFILE, (FD_LIMIT, FD_LIMIT))


def bounded_output(path: Path) -> bytes:
    with inside(path).open("rb") as stream:
        return stream.read(OUTPUT_LIMIT + 1)


def ordered(markers: list[str], required: tuple[str, ...]) -> bool:
    position = 0
    for marker in markers:
        if position < len(required) and marker == required[position]:
            position += 1
    return position == len(required)


def run_case(case: Case) -> dict[str, object]:
    # A setup failure must never inherit another case's captured output.
    inside(ROOT / "stdout").write_bytes(b"")
    inside(ROOT / "stderr").write_bytes(b"")
    timeout = HOLD_TIMEOUT if case.hold_eof else EXECUTION_TIMEOUT
    started = time.monotonic()
    deadline = started + timeout
    process = None
    read_fd = None
    write_fd = None
    report: dict[str, object] = {"case": case.name, "mode": case.mode, "payload_bytes": len(case.payload), "timeout_seconds": timeout, "timeout": False, "pipe_close_errors": [], "cleanup_signal_errors": [], "cleanup_wait_errors": []}

    def remaining() -> float:
        duration = deadline - time.monotonic()
        if duration <= 0:
            raise TimeoutError("environment transport deadline exceeded")
        return duration

    def close_pipe(descriptor: int) -> None:
        try:
            os.close(descriptor)
        except OSError as error:
            report["pipe_close_errors"].append({"fd": descriptor, "errno": error.errno})
            raise

    try:
        read_fd, write_fd = os.pipe()
        if not 3 <= read_fd < FD_LIMIT:
            raise ValueError("control pipe descriptor exceeds the declared inherited range")
        os.set_blocking(write_fd, False)
        with inside(ROOT / "stdout").open("wb") as stdout, inside(ROOT / "stderr").open("wb") as stderr:
            process = subprocess.Popen([str(ROOT / "launcher"), str(read_fd), case.mode], cwd=ROOT,
                                       env={"CI": "1", "TMPDIR": str(ROOT)}, stdin=subprocess.DEVNULL,
                                       stdout=stdout, stderr=stderr, close_fds=True, pass_fds=(read_fd,),
                                       start_new_session=True, preexec_fn=child_limits)
            parent_reader = read_fd
            read_fd = None
            close_pipe(parent_reader)
            sent = 0
            while sent < len(case.payload):
                remaining()
                try:
                    count = os.write(write_fd, case.payload[sent:sent + WRITE_CHUNK])
                    if count <= 0:
                        raise OSError(errno.EIO, "control pipe made no progress")
                    sent += count
                except (BlockingIOError, InterruptedError):
                    select.select([], [write_fd], [], remaining())
                except BrokenPipeError as error:
                    report["write_error_errno"] = error.errno
                    break
            report["payload_bytes_written"] = sent
            if not case.hold_eof:
                parent_writer = write_fd
                write_fd = None
                close_pipe(parent_writer)
            process.wait(timeout=remaining())
    except (TimeoutError, subprocess.TimeoutExpired):
        report["timeout"] = True
    except (OSError, ValueError) as error:
        report["transport_error"] = str(error)[:256]
    finally:
        if process is not None:
            if process.poll() is None:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                except OSError as error:
                    report["cleanup_signal_errors"].append({"pid": process.pid, "errno": error.errno})
        for descriptor in (read_fd, write_fd):
            if descriptor is not None:
                try:
                    close_pipe(descriptor)
                except OSError:
                    pass
        if process is not None:
            try:
                process.wait(timeout=CLEANUP_TIMEOUT)
            except subprocess.TimeoutExpired:
                report["cleanup_wait_errors"].append({"pid": process.pid, "timeout": True})
            except OSError as error:
                report["cleanup_wait_errors"].append({"pid": process.pid, "errno": error.errno})
            report["exit_code"] = process.returncode
        report["owned_child_joined"] = process is None or process.returncode is not None
    report["elapsed_seconds"] = round(time.monotonic() - started, 3)
    stdout_bytes = bounded_output(ROOT / "stdout")
    stderr_bytes = bounded_output(ROOT / "stderr")
    report["output_saturated"] = len(stdout_bytes) >= OUTPUT_LIMIT or len(stderr_bytes) >= OUTPUT_LIMIT or report.get("exit_code") == -signal.SIGXFSZ
    for stream, data in (("stdout", stdout_bytes), ("stderr", stderr_bytes)):
        captured = data[:OUTPUT_LIMIT]
        with inside(ROOT / "cases" / f"{case.name}.{stream}").open("xb") as destination:
            destination.write(captured)
        report[stream] = captured.decode("utf-8", errors="replace")
    lines = str(report["stderr"]).splitlines()
    markers = [line.removeprefix("LO_PHASE ") for line in lines if re.fullmatch(r"LO_PHASE (?:launcher|constructor|target)\.[a-z-]+", line)]
    report["phase_markers"] = markers
    constructor_lines = [line for line in lines if line.startswith("LO_CONSTRUCTOR ")]
    try:
        observations = json.loads(stdout_bytes) if stdout_bytes and not report["output_saturated"] else None
    except (ValueError, UnicodeDecodeError):
        observations = None
    report["observations"] = observations
    cleanup_ok = report["owned_child_joined"] and not report["pipe_close_errors"] and not report["cleanup_signal_errors"] and not report["cleanup_wait_errors"]
    no_target = not any(marker.startswith("target.") or marker.startswith("constructor.") for marker in markers) and not constructor_lines
    healthy = cleanup_ok and not report["output_saturated"] and "transport_error" not in report
    expected_output = {"target_started": True, "environment_exact": True, "arguments_exact": True, "directory_exact": True, "open_fds": 3}
    if case.expectation in ("valid", "loaded"):
        required = VALID_PHASES + (("constructor.entered",) if case.expectation == "loaded" else ()) + TARGET_PHASES
        constructor_ok = not constructor_lines and "constructor.entered" not in markers
        if case.expectation == "loaded":
            denial = "LO_CONSTRUCTOR denied_write_result=-1 errno=1"
            required_lines = tuple("LO_PHASE " + marker for marker in VALID_PHASES) + ("LO_PHASE constructor.entered", denial) + tuple("LO_PHASE " + marker for marker in TARGET_PHASES)
            constructor_ok = constructor_lines == [denial] and markers.count("constructor.entered") == 1 and ordered(lines, required_lines)
        passed = healthy and not report["timeout"] and "write_error_errno" not in report and report.get("exit_code") == 0 and observations == expected_output and ordered(markers, required) and constructor_ok
    elif case.expectation == "invalid":
        expected_write = "write_error_errno" not in report or report["write_error_errno"] == errno.EPIPE
        passed = healthy and expected_write and not report["timeout"] and report.get("exit_code") == 65 and ordered(markers, INITIAL_PHASES + ("launcher.invalid-environment",)) and "launcher.before-execve" not in markers and no_target and not stdout_bytes
    else:
        passed = healthy and report["timeout"] and report.get("exit_code") == -signal.SIGKILL and report.get("payload_bytes_written") == len(case.payload) and "write_error_errno" not in report and ordered(markers, INITIAL_PHASES + ("launcher.before-environment-eof",)) and "launcher.environment-received" not in markers and "launcher.before-execve" not in markers and "launcher.control-closed" not in markers and no_target and not stdout_bytes
    report["passed"] = bool(passed)
    missing_constructor = case.expectation == "loaded" and ("constructor.entered" not in markers or not constructor_lines)
    report["conclusion"] = "selected-case-passed" if passed else "inconclusive" if report["output_saturated"] or missing_constructor or not stdout_bytes and not stderr_bytes else "selected-case-failed"
    return report


def run(expected: str) -> int:
    reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior environment result before another suite")
    suite = cases()
    for case in suite:
        for stream in ("stdout", "stderr"):
            if inside(ROOT / "cases" / f"{case.name}.{stream}").exists():
                raise ValueError("preserve prior environment case outputs before another suite")
    for name, content in FIXTURES.items():
        if inside(ROOT / name).read_bytes() != content:
            raise ValueError(f"fixture baseline differs: {name}")
    report: dict[str, object] = {
        "scope": "Fixed trusted environment-transport and constructor-denial cases only; this does not qualify arbitrary Process execution or enable the standard host.",
        "cleanup_scope": "Owned primary/group while unreaped; the reviewed helper sources create no descendants.",
        "deadline_scope": "A monotonic deadline starts before synchronous Popen and bounds transfer and wait after it returns. Popen creation is not independently interruptible. This probe does not qualify bounded native Start.",
        "qualified_process_backend": False, "reviewed_manifest_sha256": expected,
        "reference_manifest_sha256": REFERENCE_MANIFEST, "execution_timeout_seconds": EXECUTION_TIMEOUT,
        "hold_open_timeout_seconds": HOLD_TIMEOUT, "cleanup_timeout_seconds": CLEANUP_TIMEOUT,
        "core_dump_limit": 0, "file_descriptor_limit": FD_LIMIT, "output_limit_per_stream": OUTPUT_LIMIT,
        "protocol_limits": {"body_bytes": MAX_BODY, "entries": MAX_COUNT, "entry_bytes": MAX_ENTRY, "write_chunk_bytes": WRITE_CHUNK},
        "fixture_inputs": {name: digest(ROOT / name) for name in FIXTURES},
    }
    results = []
    for case in suite:
        case_result = run_case(case)
        try:
            case_result["fixtures_unchanged"] = all(inside(ROOT / name).read_bytes() == content for name, content in FIXTURES.items())
            case_result["artifacts_unchanged"] = verify(expected) == reviewed
        except (OSError, ValueError) as error:
            case_result["integrity_error"] = str(error)[:256]
            case_result["fixtures_unchanged"] = False
            case_result["artifacts_unchanged"] = False
        results.append(case_result)
        exit_code = case_result.get("exit_code")
        if isinstance(exit_code, int) and exit_code < 0 and not (case.expectation == "timeout" and case_result["passed"]):
            report["stopped_after_unexpected_signal"] = case.name
            break
        if not case_result["owned_child_joined"] or case_result["pipe_close_errors"] or case_result["cleanup_signal_errors"] or case_result["cleanup_wait_errors"] or not case_result["fixtures_unchanged"] or not case_result["artifacts_unchanged"]:
            break
    report["cases"] = results
    try:
        report["fixture_outputs"] = {name: digest(ROOT / name) for name in FIXTURES}
        report["fixtures_unchanged"] = report["fixture_inputs"] == report["fixture_outputs"]
    except (OSError, ValueError) as error:
        report["fixture_integrity_error"] = str(error)[:256]
        report["fixtures_unchanged"] = False
    try:
        post_run = verify(expected)
    except (OSError, ValueError) as error:
        post_run = None
        report["integrity_error"] = str(error)[:256]
    report["post_run_artifacts"] = post_run
    report["reviewed_artifacts_unchanged"] = post_run == reviewed
    passed = len(results) == len(suite) and all(case["passed"] and case["fixtures_unchanged"] and case["artifacts_unchanged"] for case in results) and report["fixtures_unchanged"] and report["reviewed_artifacts_unchanged"]
    report["suite_passed"] = bool(passed)
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
    if len(arguments) == 2 and arguments[0] == "verify":
        verify(arguments[1])
        print(json.dumps({"reviewed_build_verified": True}))
        return 0
    if len(arguments) == 2 and arguments[0] == "run":
        return run(arguments[1])
    raise ValueError("expected prepare, seal, verify SHA256, or run SHA256")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(2)
