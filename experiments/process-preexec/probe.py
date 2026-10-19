"""Prepare and verify an independently reviewed pre-exec inheritance probe."""
from __future__ import annotations

import hashlib
import errno
import json
import os
from pathlib import Path
import re
import resource
import signal
import socket
import subprocess
import sys

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-preexec"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-preexec"
OUTPUT_LIMIT = 64 << 10
TIMEOUT_SECONDS = 5
FD_LIMIT = 64
FIXTURES = {
    "allowed-read": b"allowed-fixture",
    "denied-read": b"denied-fixture",
    "allowed-write": b"allowed-baseline",
    "denied-write": b"denied-baseline",
}
ARTIFACTS = {
    "target_source": SOURCE / "target.c",
    "launcher_source": SOURCE / "launcher.c",
    "harness": SOURCE / "probe.py",
    "verification_test": SOURCE / "test_probe.py",
    "build": SOURCE / "probe.mk",
    "profile": ROOT / "candidate.sb",
    "header": ROOT / "fixture_paths.h",
    "target_binary": ROOT / "target",
    "launcher_binary": ROOT / "launcher",
}


def inside(path: Path) -> Path:
    """Require a direct path inside the repository before accessing it."""
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def prepare() -> None:
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    profile = "\n".join([
        "(version 1)",
        "(deny default (with no-log))",
        f"(allow process-exec (literal {json.dumps(str(ROOT / 'target'))}))",
        "(allow file-read* (subpath \"/usr/lib\") (subpath \"/System/Library\"))",
        "(allow file-read* " + " ".join(
            f"(literal {json.dumps(str(ROOT / name))})"
            for name in ("target", "allowed-read")
        ) + ")",
        f"(allow file-read-metadata (subpath {json.dumps(str(ROOT))}))",
        "(allow file-write* " + " ".join(
            f"(literal {json.dumps(str(ROOT / name))})"
            for name in ("allowed-write", "stdout", "stderr")
        ) + ")",
        "(deny network* (with no-log))",
        "(deny process-fork (with no-log))",
        "",
    ])
    inside(ROOT / "candidate.sb").write_text(profile)
    definitions = {"LO_PROFILE": profile, "LO_TARGET": str(ROOT / "target"), "LO_ROOT": str(ROOT)}
    definitions.update({"LO_" + name.upper().replace("-", "_"): str(ROOT / name) for name in FIXTURES})
    inside(ROOT / "fixture_paths.h").write_text(f"#define LO_FD_LIMIT {FD_LIMIT}\n" + "\n".join(
        f"#define {name} {json.dumps(content)}" for name, content in definitions.items()
    ) + "\n")
    for name, content in FIXTURES.items():
        inside(ROOT / name).write_bytes(content)


def seal() -> None:
    manifest = {name: digest(path) for name, path in ARTIFACTS.items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    """Reject an absent approval before reading build artifacts."""
    if re.fullmatch(r"[0-9a-f]{64}", expected) is None:
        raise ValueError("reviewed manifest SHA256 is required")
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed build manifest differs")
    manifest = json.loads(inside(ROOT / "build.json").read_text())
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if manifest != actual:
        raise ValueError("reviewed inputs or binaries differ")
    return actual


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (OUTPUT_LIMIT, OUTPUT_LIMIT))
    resource.setrlimit(resource.RLIMIT_NOFILE, (FD_LIMIT, FD_LIMIT))


def run(expected: str) -> int:
    reviewed = verify(expected)
    for name, content in FIXTURES.items():
        if inside(ROOT / name).read_bytes() != content:
            raise ValueError(f"fixture input changed: {name}")
    report: dict[str, object] = {
        "reviewed_manifest_sha256": expected,
        "reviewed_artifacts": reviewed,
        "qualified_process_backend": False,
        "scope": "Trusted fixed target under launcher-installed pre-exec confinement.",
        "core_dump_limit": 0,
        "output_limit_per_stream": OUTPUT_LIMIT,
        "file_descriptor_limit": FD_LIMIT,
        "timeout_seconds": TIMEOUT_SECONDS,
        "timeout": False,
        "runtime_read_allowances": ["/usr/lib", "/System/Library"],
    }
    environment = {"CI": "1", "TMPDIR": str(ROOT), "LO_PREEXEC_SENTINEL": "explicit"}
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.settimeout(0.1)
        with inside(ROOT / "stdout").open("wb") as stdout, inside(ROOT / "stderr").open("wb") as stderr:
            process = subprocess.Popen(
                [str(inside(ROOT / "launcher")), str(listener.getsockname()[1])],
                cwd=inside(ROOT), env=environment, stdin=subprocess.DEVNULL,
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
        try:
            connection, _ = listener.accept()
            connection.close()
            report["loopback_connection_observed"] = True
        except TimeoutError:
            report["loopback_connection_observed"] = False
    stdout_bytes = inside(ROOT / "stdout").read_bytes()
    stderr_bytes = inside(ROOT / "stderr").read_bytes()
    report["output_within_limit"] = len(stdout_bytes) <= OUTPUT_LIMIT and len(stderr_bytes) <= OUTPUT_LIMIT
    report["stdout"] = stdout_bytes[:OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["stderr"] = stderr_bytes[:OUTPUT_LIMIT].decode("utf-8", errors="replace")
    report["phase_markers"] = [
        line.removeprefix("LO_PHASE ")
        for line in str(report["stderr"]).splitlines()
        if re.fullmatch(r"LO_PHASE (?:launcher|target)\.[a-z-]+", line)
    ]
    try:
        observations = json.loads(stdout_bytes) if report["output_within_limit"] else None
    except (ValueError, UnicodeDecodeError):
        observations = None
    report["observations"] = observations
    report["allowed_write_changed"] = inside(ROOT / "allowed-write").read_bytes() == b"probe-write"
    report["denied_write_unchanged"] = inside(ROOT / "denied-write").read_bytes() == FIXTURES["denied-write"]
    report["read_fixtures_unchanged"] = all(inside(ROOT / name).read_bytes() == FIXTURES[name] for name in ("allowed-read", "denied-read"))
    try:
        post_run = verify(expected)
    except (OSError, ValueError):
        post_run = None
    report["post_run_artifacts"] = post_run
    report["reviewed_artifacts_unchanged"] = post_run == reviewed
    expected_observations = {
        "target_started": True, "arguments_exact": True,
        "environment_exact": True, "directory_exact": True,
        "standard_fds_open": True, "open_fds_below_limit": 3,
        "fd_limit": FD_LIMIT, "allowed_read": [0, 0],
        "denied_read": [-1, errno.EPERM], "allowed_write": [0, 0],
        "denied_write": [-1, errno.EPERM], "network": [-1, errno.EPERM], "fork": [-1, errno.EPERM],
    }
    passed = (
        report["exit_code"] == 0 and not report["timeout"]
        and report["output_within_limit"] and observations == expected_observations
        and report["allowed_write_changed"] and report["denied_write_unchanged"]
        and report["read_fixtures_unchanged"] and not report["loopback_connection_observed"]
        and report["reviewed_artifacts_unchanged"]
    )
    report["mechanism_probe_passed"] = bool(passed)
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
