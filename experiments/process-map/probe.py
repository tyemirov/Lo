"""Prepare or run an exact reviewed file-mapping mechanism probe."""
from __future__ import annotations

import errno
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import signal
import subprocess
import sys

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-map"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-map"
MAPPING_LENGTH = 27
OUTPUT_LIMIT = 64 << 10
FD_LIMIT = 64
TIMEOUT_SECONDS = 5
ARTIFACTS = {
    "source": SOURCE / "fixture.c",
    "image_source": SOURCE / "image.c",
    "harness": SOURCE / "probe.py",
    "verification_test": SOURCE / "test_probe.py",
    "build": SOURCE / "probe.mk",
    "profile": ROOT / "candidate.sb",
    "header": ROOT / "fixture_paths.h",
    "fixture": ROOT / "mapping-fixture",
    "binary": ROOT / "fixture",
}


def inside(path: Path) -> Path:
    """Require direct repository paths before task file access."""
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def prepare() -> None:
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    profile = "\n".join([
        "(version 1)", "(deny default (with no-log))",
        f"(allow file-read* (literal {json.dumps(str(ROOT / 'mapping-fixture'))}))",
        "(allow file-write* " + " ".join(
            f"(literal {json.dumps(str(ROOT / name))})" for name in ("stdout", "stderr")
        ) + ")", "",
    ])
    inside(ROOT / "candidate.sb").write_text(profile)
    definitions = {
        "LO_PROFILE": profile,
        "LO_FIXTURE": str(ROOT / "mapping-fixture"),
    }
    inside(ROOT / "fixture_paths.h").write_text(
        f"#define LO_FIXTURE_SIZE {MAPPING_LENGTH}\n" + "\n".join(
            f"#define {name} {json.dumps(content)}" for name, content in definitions.items()
        ) + "\n"
    )


def seal() -> None:
    if len(inside(ROOT / "mapping-fixture").read_bytes()) < MAPPING_LENGTH:
        raise ValueError("mapped artifact is shorter than the mapping length")
    manifest = {name: digest(path) for name, path in ARTIFACTS.items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    if re.fullmatch(r"[0-9a-f]{64}", expected) is None:
        raise ValueError("reviewed manifest SHA256 is required")
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed build manifest differs")
    manifest = json.loads(inside(ROOT / "build.json").read_text())
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if manifest != actual:
        raise ValueError("reviewed inputs, fixture, or binary differ")
    return actual


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (OUTPUT_LIMIT, OUTPUT_LIMIT))
    resource.setrlimit(resource.RLIMIT_NOFILE, (FD_LIMIT, FD_LIMIT))


def completed_mapping(item: object) -> bool:
    if not isinstance(item, dict):
        return False
    opened = item.get("open")
    return (
        item.get("attempted") is True and isinstance(opened, list)
        and len(opened) == 2 and isinstance(opened[0], int) and opened[0] >= 0
        and opened[1] == 0 and item.get("map_attempted") is True
        and item.get("mmap") == [0, 0] and item.get("unmap_attempted") is True
        and item.get("munmap") == [0, 0] and item.get("close_attempted") is True
        and item.get("close") == [0, 0]
    )


def denied_mapping(item: object) -> bool:
    if not isinstance(item, dict):
        return False
    opened = item.get("open")
    return (
        item.get("attempted") is True and isinstance(opened, list)
        and len(opened) == 2 and isinstance(opened[0], int) and opened[0] >= 0
        and opened[1] == 0 and item.get("map_attempted") is True
        and item.get("mmap") == [-1, errno.EPERM]
        and item.get("unmap_attempted") is False
        and item.get("close_attempted") is True and item.get("close") == [0, 0]
    )


def run(expected: str) -> int:
    reviewed = verify(expected)
    fixture_size = len(inside(ROOT / "mapping-fixture").read_bytes())
    if fixture_size < MAPPING_LENGTH:
        raise ValueError("mapped artifact is shorter than the mapping length")
    report: dict[str, object] = {
        "reviewed_manifest_sha256": expected, "reviewed_artifacts": reviewed,
        "qualified_process_backend": False,
        "core_dump_limit": 0, "output_limit_per_stream": OUTPUT_LIMIT,
        "file_descriptor_limit": FD_LIMIT, "timeout_seconds": TIMEOUT_SECONDS,
        "timeout": False, "mapped_bytes_accessed": False,
        "mapping_length": MAPPING_LENGTH, "fixture_size": fixture_size,
    }
    with inside(ROOT / "stdout").open("wb") as stdout, inside(ROOT / "stderr").open("wb") as stderr:
        process = subprocess.Popen(
            [str(inside(ROOT / "fixture"))], cwd=inside(ROOT),
            env={"CI": "1", "TMPDIR": str(ROOT)}, stdin=subprocess.DEVNULL,
            stdout=stdout, stderr=stderr, close_fds=True,
            start_new_session=True, preexec_fn=child_limits,
        )
        try:
            process.wait(timeout=TIMEOUT_SECONDS)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            report["timeout"] = True
        report["exit_code"] = process.returncode
    stdout_bytes = inside(ROOT / "stdout").read_bytes()
    stderr_bytes = inside(ROOT / "stderr").read_bytes()
    report["output_within_limit"] = len(stdout_bytes) <= OUTPUT_LIMIT and len(stderr_bytes) <= OUTPUT_LIMIT
    report["stdout"] = stdout_bytes[:OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["stderr"] = stderr_bytes[:OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["phase_markers"] = [line.removeprefix("LO_MAP_PHASE ") for line in str(report["stderr"]).splitlines() if re.fullmatch(r"LO_MAP_PHASE [a-z-]+", line)]
    try:
        observations = json.loads(stdout_bytes) if report["output_within_limit"] else None
    except (ValueError, UnicodeDecodeError):
        observations = None
    report["observations"] = observations
    report["fixture_unchanged"] = digest(ROOT / "mapping-fixture") == reviewed["fixture"]
    try:
        post_run = verify(expected)
    except (OSError, ValueError):
        post_run = None
    report["post_run_artifacts"] = post_run
    report["reviewed_artifacts_unchanged"] = post_run == reviewed
    baseline = isinstance(observations, dict) and completed_mapping(observations.get("baseline_read")) and completed_mapping(observations.get("baseline_executable"))
    installed = isinstance(observations, dict) and observations.get("sandbox_installed") is True
    read_allowed = isinstance(observations, dict) and completed_mapping(observations.get("confined_read"))
    exec_denied = isinstance(observations, dict) and denied_mapping(observations.get("confined_executable"))
    passed = (
        report["exit_code"] == 0 and not report["timeout"] and report["output_within_limit"]
        and baseline and installed and read_allowed and exec_denied
        and report["fixture_unchanged"] and report["reviewed_artifacts_unchanged"]
    )
    report["baseline_mapping_established"] = baseline
    report["mechanism_probe_passed"] = bool(passed)
    report["conclusion"] = "denial-observed" if passed else "inconclusive" if not baseline or not installed else "selected-denial-not-established"
    inside(ROOT / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if passed else 1


def main() -> int:
    if Path.cwd().resolve() != inside(REPOSITORY):
        raise ValueError("run from the Lo repository")
    if sys.argv[1:] == ["prepare"]:
        prepare()
        return 0
    if sys.argv[1:] == ["seal"]:
        seal()
        return 0
    if len(sys.argv) == 3 and sys.argv[1] == "verify":
        verify(sys.argv[2])
        print(json.dumps({"reviewed_build_verified": True}))
        return 0
    if len(sys.argv) == 3 and sys.argv[1] == "run":
        return run(sys.argv[2])
    raise ValueError("expected prepare, seal, verify SHA256, or run SHA256")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(2)
