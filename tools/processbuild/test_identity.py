"""Exercise the build and verification CLI without launching its output."""

from pathlib import Path
import hashlib
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/processbuild/identity.py"


def invoke(action: str, launcher: Path, identity: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, "-B", str(SCRIPT), action, str(launcher), str(identity)],
        cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False,
    )


def test_controlled_build_and_read_only_verification(tmp_path: Path) -> None:
    launcher = tmp_path / "launcher"
    identity = tmp_path / "identity.go"
    launcher.write_bytes(b"untrusted replacement")
    identity.write_text("untrusted pin")
    built = invoke("build", launcher, identity)
    assert built.returncode == 0, built.stderr
    original = launcher.read_bytes()
    pin = identity.read_bytes()
    assert original != b"untrusted replacement"
    assert hashlib.sha256(original).hexdigest().encode() in pin
    assert invoke("verify", launcher, identity).returncode == 0
    launcher.write_bytes(b"newer replacement")
    rejected = invoke("verify", launcher, identity)
    assert rejected.returncode != 0
    assert "identity mismatch" in rejected.stderr
    assert identity.read_bytes() == pin
    assert launcher.read_bytes() == b"newer replacement"
    rebuilt = invoke("build", launcher, identity)
    assert rebuilt.returncode == 0, rebuilt.stderr
    assert launcher.read_bytes() != b"newer replacement"
    assert invoke("verify", launcher, identity).returncode == 0
