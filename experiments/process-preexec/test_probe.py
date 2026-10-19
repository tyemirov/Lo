"""Verify the review gate through the probe CLI without launching a child."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

REPOSITORY = Path(__file__).resolve().parents[2]
HARNESS = REPOSITORY / "experiments/process-preexec/probe.py"


def test_verify_rejects_unreviewed_manifest() -> None:
    result = subprocess.run(
        [sys.executable, "-B", str(HARNESS), "verify", "unreviewed"],
        cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True,
        timeout=5, check=False,
    )
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}
