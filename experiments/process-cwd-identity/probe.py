"""Observe directory identity after exec under the reviewed root-read profile."""
from __future__ import annotations

import errno
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import time
from types import ModuleType

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-cwd-identity"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-cwd-identity"
REFERENCE = REPOSITORY / "_scratch/adapter-implementation-x921/native-root-bootstrap"
REFERENCE_MANIFEST = "c6256f479b3d610d5c07089d8cb76f295f517ca74440eb82ae7940664442f734"
PREEXEC = REPOSITORY / "_scratch/adapter-implementation-x921/native-preexec"
PREEXEC_SOURCE = REPOSITORY / "experiments/process-preexec/probe.py"
PREEXEC_REVIEWED_MANIFEST = "72d4e0db509f94a3097c29fdcbd5d73ab9b5c9ae09aa598ac6716c40d37eaa9b"
SUPPORT_EVIDENCE = REPOSITORY / "_scratch/adapter-implementation-x921/loader/dyld-support-rules.txt"
BOOTSTRAP_RULE = '(allow file-read* file-test-existence (literal "/"))'
CLEANUP_TIMEOUT = 2


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def preexec_probe() -> ModuleType:
    """Load only the original source bytes bound by the pinned manifest."""
    if digest(PREEXEC / "build.json") != PREEXEC_REVIEWED_MANIFEST:
        raise ValueError("immutable preexec manifest differs")
    manifest = json.loads(inside(PREEXEC / "build.json").read_text())
    source = inside(PREEXEC_SOURCE).read_bytes()
    if hashlib.sha256(source).hexdigest() != manifest.get("harness"):
        raise ValueError("immutable preexec harness differs")
    module = ModuleType("lo_reviewed_preexec")
    module.__file__ = str(PREEXEC_SOURCE)
    exec(compile(source, str(PREEXEC_SOURCE), "exec"), module.__dict__)
    module.verify(PREEXEC_REVIEWED_MANIFEST)
    return module


def artifacts(preexec: ModuleType) -> dict[str, Path]:
    return {
        "harness": SOURCE / "probe.py", "verification_test": SOURCE / "test_probe.py",
        "build": SOURCE / "probe.mk", "support_evidence": SUPPORT_EVIDENCE,
        "mechanism_manifest": REPOSITORY / "_scratch/adapter-implementation-x921/native-cwd/build.json",
        "mechanism_result": REPOSITORY / "_scratch/adapter-implementation-x921/native-cwd/result.json",
        "reference_manifest": REFERENCE / "build.json",
        "reference_profile": REFERENCE / "candidate.sb",
        "reference_header": REFERENCE / "fixture_paths.h",
        "target_source": SOURCE / "target.c", "target_binary": ROOT / "target",
        "expected_directory": ROOT / "expected-directory.json",
        "profile": ROOT / "candidate.sb", "header": ROOT / "fixture_paths.h",
        "launcher_binary": ROOT / "launcher", "preexec_manifest": PREEXEC / "build.json",
        **{f"preexec_{name}": path for name, path in preexec.ARTIFACTS.items()},
    }


def directory_identity() -> dict[str, int]:
    information = inside(PREEXEC).stat()
    return {"device": information.st_dev, "inode": information.st_ino}


def selected_inputs() -> tuple[str, str, dict[str, int]]:
    if digest(REFERENCE / "build.json") != REFERENCE_MANIFEST:
        raise ValueError("reviewed root-read manifest differs")
    manifest = json.loads(inside(REFERENCE / "build.json").read_text())
    for name, path in {"profile": REFERENCE / "candidate.sb", "header": REFERENCE / "fixture_paths.h"}.items():
        if digest(path) != manifest[name]:
            raise ValueError("reviewed root-read input differs")
    original_profile = inside(REFERENCE / "candidate.sb").read_text()
    original_target, target = str(PREEXEC / "target"), str(ROOT / "target")
    if original_profile.count(original_target) != 2:
        raise ValueError("expected two exact executable path references")
    profile = original_profile.replace(original_target, target)
    lines = inside(REFERENCE / "fixture_paths.h").read_text().splitlines(keepends=True)
    for macro, replacement in {"LO_PROFILE": profile, "LO_TARGET": target}.items():
        definitions = [index for index, line in enumerate(lines) if line.startswith("#define " + macro + " ")]
        if len(definitions) != 1:
            raise ValueError("expected one selected header definition")
        lines[definitions[0]] = "#define " + macro + " " + json.dumps(replacement) + "\n"
    identity = directory_identity()
    for macro, field in {"LO_EXPECTED_DEVICE": "device", "LO_EXPECTED_INODE": "inode"}.items():
        lines.append(f"#define {macro} {identity[field]}ULL\n")
    return profile, "".join(lines), identity


def verify_selection() -> None:
    profile, header, identity = selected_inputs()
    if inside(ROOT / "candidate.sb").read_text() != profile or inside(ROOT / "fixture_paths.h").read_text() != header:
        raise ValueError("directory identity comparison profile or header differs")
    if json.loads(inside(ROOT / "expected-directory.json").read_text()) != identity:
        raise ValueError("selected directory identity differs")


def prepare() -> None:
    preexec_probe()
    profile, header, identity = selected_inputs()
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "candidate.sb").write_text(profile)
    inside(ROOT / "fixture_paths.h").write_text(header)
    inside(ROOT / "expected-directory.json").write_text(json.dumps(identity, sort_keys=True) + "\n")


def seal() -> None:
    preexec = preexec_probe()
    verify_selection()
    manifest = {name: digest(path) for name, path in artifacts(preexec).items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> tuple[ModuleType, dict[str, str]]:
    preexec = preexec_probe()
    verify_selection()
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed directory identity manifest differs")
    actual = {name: digest(path) for name, path in artifacts(preexec).items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed comparison inputs or original artifacts differ")
    return preexec, actual


def read_output(path: Path, limit: int) -> bytes:
    with inside(path).open("rb") as stream:
        return stream.read(limit + 1)


def run(expected: str) -> int:
    preexec, reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior directory identity result before another run")
    for name, content in preexec.FIXTURES.items():
        if preexec.inside(PREEXEC / name).read_bytes() != content:
            raise ValueError(f"fixture baseline differs: {name}")
    report: dict[str, object] = {
        "scope": "Directory identity after exec for the fixed trusted fixture. Only device and inode metadata are observed. The root-read profile changes only its exact target path; this does not qualify arbitrary Process execution.",
        "cleanup_scope": "Direct owned fixture/group while the child remains unreaped; the immutable target reaps its immediate-exit fork child. No untrusted descendant guarantee.",
        "qualified_process_backend": False, "reviewed_manifest_sha256": expected,
        "profile_change": "exact target path only", "expected_directory_identity": directory_identity(), "execution_timeout_seconds": preexec.TIMEOUT_SECONDS,
        "cleanup_timeout_seconds": CLEANUP_TIMEOUT, "core_dump_limit": 0,
        "file_descriptor_limit": preexec.FD_LIMIT, "output_limit_per_stream": preexec.OUTPUT_LIMIT,
        "fixture_inputs": {name: digest(PREEXEC / name) for name in preexec.FIXTURES},
        "timeout": False, "cleanup_signal_errors": [], "cleanup_wait_errors": [],
    }
    environment = {"CI": "1", "TMPDIR": str(PREEXEC), "LO_PREEXEC_SENTINEL": "explicit"}
    process = None
    started = time.monotonic()
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.setblocking(False)
        with preexec.inside(PREEXEC / "stdout").open("wb") as stdout, preexec.inside(PREEXEC / "stderr").open("wb") as stderr:
            try:
                process = subprocess.Popen([str(ROOT / "launcher"), str(listener.getsockname()[1])], cwd=PREEXEC,
                                           env=environment, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr,
                                           close_fds=True, start_new_session=True, preexec_fn=preexec.child_limits)
                process.wait(timeout=preexec.TIMEOUT_SECONDS)
            except subprocess.TimeoutExpired:
                report["timeout"] = True
            except OSError as error:
                report["launch_error_errno"] = error.errno
            finally:
                if process is not None:
                    if process.poll() is None:
                        try:
                            os.killpg(process.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                        except OSError as error:
                            report["cleanup_signal_errors"].append({"pid": process.pid, "errno": error.errno})
                    try:
                        process.wait(timeout=CLEANUP_TIMEOUT)
                    except subprocess.TimeoutExpired:
                        report["cleanup_wait_errors"].append({"pid": process.pid, "timeout": True})
                    except OSError as error:
                        report["cleanup_wait_errors"].append({"pid": process.pid, "errno": error.errno})
                    report["exit_code"] = process.returncode
                report["owned_child_joined"] = process is None or process.returncode is not None
        try:
            connection, _ = listener.accept()
            connection.close()
            report["loopback_connection_observed"] = True
        except BlockingIOError:
            report["loopback_connection_observed"] = False
    report["elapsed_seconds"] = round(time.monotonic() - started, 3)
    stdout_bytes = read_output(PREEXEC / "stdout", preexec.OUTPUT_LIMIT)
    stderr_bytes = read_output(PREEXEC / "stderr", preexec.OUTPUT_LIMIT)
    report["output_saturated"] = len(stdout_bytes) >= preexec.OUTPUT_LIMIT or len(stderr_bytes) >= preexec.OUTPUT_LIMIT or report.get("exit_code") == -signal.SIGXFSZ
    report["stdout"] = stdout_bytes[:preexec.OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["stderr"] = stderr_bytes[:preexec.OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["phase_markers"] = [line.removeprefix("LO_PHASE ") for line in str(report["stderr"]).splitlines() if re.fullmatch(r"LO_PHASE (?:launcher|target)\.[a-z-]+", line)]
    try:
        observations = json.loads(stdout_bytes) if stdout_bytes and not report["output_saturated"] else None
    except (ValueError, UnicodeDecodeError):
        observations = None
    report["observations"] = observations
    report["fixture_outputs"] = {name: digest(PREEXEC / name) for name in preexec.FIXTURES}
    report["fixture_effects_expected"] = preexec.inside(PREEXEC / "allowed-write").read_bytes() == b"probe-write" and all(preexec.inside(PREEXEC / name).read_bytes() == preexec.FIXTURES[name] for name in ("allowed-read", "denied-read", "denied-write"))
    try:
        _, post_run = verify(expected)
    except (OSError, ValueError) as error:
        post_run = None
        report["integrity_error"] = str(error)[:256]
    report["post_run_artifacts"] = post_run
    report["reviewed_artifacts_unchanged"] = post_run == reviewed
    expected_observations = {
        "target_started": True, "arguments_exact": True, "environment_exact": True,
        "directory_identity": {"present": True, "errno": 0, **directory_identity()}, "standard_fds_open": True, "open_fds_below_limit": 3,
        "fd_limit": preexec.FD_LIMIT, "allowed_read": [0, 0], "allowed_write": [0, 0],
        "denied_read": [-1, errno.EPERM], "denied_write": [-1, errno.EPERM],
        "network": [-1, errno.EPERM], "fork": [-1, errno.EPERM],
    }
    comparable = dict(observations) if isinstance(observations, dict) else {}
    cwd_observation = comparable.pop("getcwd", None)
    cwd_report_valid = isinstance(cwd_observation, dict) and set(cwd_observation) == {"present", "exact", "errno"} and type(cwd_observation["present"]) is bool and type(cwd_observation["exact"]) is bool and type(cwd_observation["errno"]) is int
    passed = cwd_report_valid and comparable == expected_observations and report.get("exit_code") == 0 and not report["output_saturated"] and not report["timeout"] and "launch_error_errno" not in report and report["owned_child_joined"] and not report["cleanup_signal_errors"] and not report["cleanup_wait_errors"] and not report["loopback_connection_observed"] and report["reviewed_artifacts_unchanged"] and report["fixture_effects_expected"]
    report["comparison_passed"] = bool(passed)
    report["conclusion"] = "selected-fixture-comparison-passed" if passed else "inconclusive" if observations is None or report["output_saturated"] else "selected-fixture-comparison-failed"
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
