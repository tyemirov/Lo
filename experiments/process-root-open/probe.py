"""Measure opening the runtime root handle under the original reviewed profile."""
from __future__ import annotations

import errno
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from types import ModuleType

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-root-open"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-root-open"
PREEXEC = REPOSITORY / "_scratch/adapter-implementation-x921/native-preexec"
PREEXEC_SOURCE = REPOSITORY / "experiments/process-preexec/probe.py"
PREEXEC_REVIEWED_MANIFEST = "72d4e0db509f94a3097c29fdcbd5d73ab9b5c9ae09aa598ac6716c40d37eaa9b"
SUPPORT_EVIDENCE = REPOSITORY / "_scratch/adapter-implementation-x921/loader/dyld-support-rules.txt"
CLEANUP_TIMEOUT = 2


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def preexec_probe() -> ModuleType:
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
        "source": SOURCE / "fixture.c", "harness": SOURCE / "probe.py",
        "verification_test": SOURCE / "test_probe.py", "build": SOURCE / "probe.mk",
        "binary": ROOT / "fixture", "header": ROOT / "fixture_paths.h",
        "support_evidence": SUPPORT_EVIDENCE, "preexec_manifest": PREEXEC / "build.json",
        **{f"preexec_{name}": path for name, path in preexec.ARTIFACTS.items()},
    }


def verify_header() -> None:
    if inside(ROOT / "fixture_paths.h").read_bytes() != inside(PREEXEC / "fixture_paths.h").read_bytes():
        raise ValueError("copied header differs from the original reviewed header")


def prepare() -> None:
    preexec_probe()
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "fixture_paths.h").write_bytes(inside(PREEXEC / "fixture_paths.h").read_bytes())


def seal() -> None:
    preexec = preexec_probe()
    verify_header()
    manifest = {name: digest(path) for name, path in artifacts(preexec).items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> tuple[ModuleType, dict[str, str]]:
    preexec = preexec_probe()
    verify_header()
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed root-open manifest differs")
    actual = {name: digest(path) for name, path in artifacts(preexec).items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed root-open inputs or original artifacts differ")
    return preexec, actual


def read_output(path: Path, limit: int) -> bytes:
    with inside(path).open("rb") as stream:
        return stream.read(limit + 1)


def completed_open(item: object) -> bool:
    if not isinstance(item, dict):
        return False
    opened = item.get("open")
    return item.get("attempted") is True and isinstance(opened, list) and len(opened) == 2 and isinstance(opened[0], int) and opened[0] >= 0 and opened[1] == 0 and item.get("close_attempted") is True and item.get("close") == [0, 0]


def denied_open(item: object) -> bool:
    return isinstance(item, dict) and item.get("attempted") is True and item.get("open") in ([-1, errno.EPERM], [-1, errno.EACCES]) and item.get("close_attempted") is False


def run(expected: str) -> int:
    preexec, reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior root-open result before another run")
    for name, content in preexec.FIXTURES.items():
        if preexec.inside(PREEXEC / name).read_bytes() != content:
            raise ValueError(f"fixture baseline differs: {name}")
    report: dict[str, object] = {
        "scope": "Opening and closing the installed-runtime root handle only, without contents access or enumeration. Permission errno is a mechanism observation, not a cause claim or native qualification.",
        "cleanup_scope": "One directly owned fixture/group while the child remains unreaped; this fixture creates no descendants.",
        "qualified_process_backend": False, "reviewed_manifest_sha256": expected,
        "execution_timeout_seconds": preexec.TIMEOUT_SECONDS, "cleanup_timeout_seconds": CLEANUP_TIMEOUT,
        "core_dump_limit": 0, "file_descriptor_limit": preexec.FD_LIMIT, "output_limit_per_stream": preexec.OUTPUT_LIMIT,
        "fixture_inputs": {name: digest(PREEXEC / name) for name in preexec.FIXTURES},
        "timeout": False, "cleanup_signal_errors": [], "cleanup_wait_errors": [],
    }
    process = None
    started = time.monotonic()
    environment = {"CI": "1", "TMPDIR": str(PREEXEC), "LO_PREEXEC_SENTINEL": "explicit"}
    with preexec.inside(PREEXEC / "stdout").open("wb") as stdout, preexec.inside(PREEXEC / "stderr").open("wb") as stderr:
        try:
            process = subprocess.Popen([str(ROOT / "fixture")], cwd=PREEXEC, env=environment,
                                       stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr,
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
    report["elapsed_seconds"] = round(time.monotonic() - started, 3)
    stdout_bytes = read_output(PREEXEC / "stdout", preexec.OUTPUT_LIMIT)
    stderr_bytes = read_output(PREEXEC / "stderr", preexec.OUTPUT_LIMIT)
    report["output_saturated"] = len(stdout_bytes) >= preexec.OUTPUT_LIMIT or len(stderr_bytes) >= preexec.OUTPUT_LIMIT or report.get("exit_code") == -signal.SIGXFSZ
    report["stdout"] = stdout_bytes[:preexec.OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["stderr"] = stderr_bytes[:preexec.OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["phase_markers"] = [line.removeprefix("LO_ROOT_PHASE ") for line in str(report["stderr"]).splitlines() if re.fullmatch(r"LO_ROOT_PHASE [a-z-]+", line)]
    try:
        observations = json.loads(stdout_bytes) if stdout_bytes and not report["output_saturated"] else None
    except (ValueError, UnicodeDecodeError):
        observations = None
    report["observations"] = observations
    baseline = isinstance(observations, dict) and completed_open(observations.get("baseline"))
    installed = isinstance(observations, dict) and observations.get("sandbox_installed") is True
    confined = observations.get("confined") if isinstance(observations, dict) else None
    report["baseline_established"] = baseline
    report["permission_denial_observed"] = denied_open(confined)
    report["fixture_outputs"] = {name: digest(PREEXEC / name) for name in preexec.FIXTURES}
    report["fixtures_unchanged"] = report["fixture_inputs"] == report["fixture_outputs"]
    try:
        _, post_run = verify(expected)
    except (OSError, ValueError) as error:
        post_run = None
        report["integrity_error"] = str(error)[:256]
    report["post_run_artifacts"] = post_run
    report["reviewed_artifacts_unchanged"] = post_run == reviewed
    measured = baseline and installed and (completed_open(confined) or denied_open(confined)) and report.get("exit_code") == 0 and not report["timeout"] and not report["output_saturated"] and "launch_error_errno" not in report and report["owned_child_joined"] and not report["cleanup_signal_errors"] and not report["cleanup_wait_errors"] and report["fixtures_unchanged"] and report["reviewed_artifacts_unchanged"]
    report["diagnostic_completed"] = bool(measured)
    report["conclusion"] = "root-open-denial-observed" if measured and denied_open(confined) else "root-open-allowed-observed" if measured else "inconclusive"
    with inside(ROOT / "result.json").open("x") as destination:
        destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if measured else 1


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
