"""Prepare or run the reviewed, repository-contained native mechanism fixture."""
from __future__ import annotations

import hashlib
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
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-profile"
SOURCE = REPOSITORY / "experiments/process-profile/fixture.c"
OUTPUT_LIMIT = 64 << 10
TIMEOUT_SECONDS = 5
FIXTURES = {
    "allowed-read": b"allowed-fixture",
    "denied-read": b"denied-fixture",
    "allowed-write": b"allowed-baseline",
    "denied-write": b"denied-baseline",
}
ARTIFACTS = {
    "source": SOURCE,
    "launcher": REPOSITORY / "experiments/process-profile/probe.py",
    "build": REPOSITORY / "experiments/process-profile/probe.mk",
    "profile": ROOT / "candidate.sb",
    "header": ROOT / "fixture_paths.h",
    "executable": ROOT / "fixture",
}


def inside(path: Path) -> Path:
    """Reject traversal and symbolic paths before task access."""
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def prepare() -> None:
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    profile = "\n".join([
        "(version 1)",
        "(deny default (with no-log))",
        f"(allow file-read* (literal {json.dumps(str(ROOT / 'allowed-read'))}))",
        f"(allow file-read-metadata (subpath {json.dumps(str(ROOT))}))",
        "(allow file-write* " + " ".join(f"(literal {json.dumps(str(ROOT / name))})" for name in ("allowed-write", "stdout", "stderr")) + ")",
        "(deny network* (with no-log))",
        "(deny process-fork (with no-log))",
        "",
    ])
    inside(ROOT / "candidate.sb").write_text(profile)
    definitions = {"LO_PROFILE": profile}
    definitions.update({"LO_" + name.upper().replace("-", "_"): str(ROOT / name) for name in FIXTURES})
    header = "\n".join(f"#define {name} {json.dumps(content)}" for name, content in definitions.items()) + "\n"
    inside(ROOT / "fixture_paths.h").write_text(header)
    for name, content in FIXTURES.items():
        inside(ROOT / name).write_bytes(content)


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (OUTPUT_LIMIT, OUTPUT_LIMIT))


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def seal() -> None:
    """Bind the exact build inputs and artifact for independent review."""
    manifest = {name: digest(path) for name, path in ARTIFACTS.items()}
    inside(ROOT / "build.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    """Require the reviewed manifest and every sealed input before launch."""
    if re.fullmatch(r"[0-9a-f]{64}", expected) is None or digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed build manifest differs")
    manifest: dict[str, str] = json.loads(inside(ROOT / "build.json").read_text())
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if actual != manifest:
        raise ValueError("reviewed build inputs or artifact differ")
    return actual


def run(expected: str) -> int:
    reviewed = verify(expected)
    helper = inside(ROOT / "fixture")
    if not helper.is_file():
        raise ValueError("missing built native fixture")
    for name, fixture_expected in FIXTURES.items():
        if inside(ROOT / name).read_bytes() != fixture_expected:
            raise ValueError(f"fixture input changed: {name}")
    report: dict[str, object] = {
        "reviewed_manifest_sha256": expected,
        "reviewed_artifacts": reviewed,
        "qualified_process_backend": False,
        "scope": "Trusted self-confining fixture; individual mechanisms only.",
        "api_status": "Installed SDK declares sandbox_init deprecated and no longer supported; build succeeded.",
        "output_limit": OUTPUT_LIMIT,
        "timeout_seconds": TIMEOUT_SECONDS,
    }
    environment = {"CI": "1", "TMPDIR": str(ROOT), "TMP": str(ROOT), "TEMP": str(ROOT)}
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.settimeout(0.1)
        with inside(ROOT / "stdout").open("wb") as stdout, inside(ROOT / "stderr").open("wb") as stderr:
            process = subprocess.Popen([str(helper), str(listener.getsockname()[1])], cwd=ROOT,
                                       env=environment, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr,
                                       close_fds=True, start_new_session=True, preexec_fn=child_limits)
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
    if len(stdout_bytes) > OUTPUT_LIMIT or len(stderr_bytes) > OUTPUT_LIMIT:
        raise ValueError("fixture exceeded the output limit")
    report["stdout"] = stdout_bytes.decode("utf-8", errors="replace")
    report["stderr"] = stderr_bytes.decode("utf-8", errors="replace")
    report["allowed_write_changed"] = inside(ROOT / "allowed-write").read_bytes() == b"probe-write"
    report["denied_write_unchanged"] = inside(ROOT / "denied-write").read_bytes() == FIXTURES["denied-write"]
    try:
        report["observations"] = json.loads(stdout_bytes)
    except (ValueError, UnicodeDecodeError):
        report["observations"] = None
    report["post_run_artifacts"] = {name: digest(path) for name, path in ARTIFACTS.items()}
    report["reviewed_artifacts_unchanged"] = report["post_run_artifacts"] == reviewed and digest(ROOT / "build.json") == expected
    passed = report["exit_code"] == 0 and report["observations"] is not None and report["allowed_write_changed"] and report["denied_write_unchanged"] and not report["loopback_connection_observed"] and report["reviewed_artifacts_unchanged"]
    report["mechanism_probe_passed"] = bool(passed)
    inside(ROOT / "result.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return 0 if passed else 1


def main() -> int:
    if Path.cwd().resolve() != inside(REPOSITORY) or len(sys.argv) < 2:
        raise ValueError("run from the Lo repository with prepare, seal, verify, or run")
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
    sys.exit(main())
