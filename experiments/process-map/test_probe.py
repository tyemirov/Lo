"""Exercise the mapping probe review gate without launching the helper."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

REPOSITORY = Path(__file__).resolve().parents[2]


def test_verify_requires_reviewed_manifest() -> None:
    result = subprocess.run(
        [sys.executable, "-B", str(REPOSITORY / "experiments/process-map/probe.py"), "verify", "unreviewed"],
        cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True,
        timeout=5, check=False,
    )
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}
