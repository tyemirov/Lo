"""Check loader diagnostic review rejection without launching the fixture."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

import pytest

REPOSITORY = Path(__file__).resolve().parents[2]


@pytest.mark.parametrize("arguments", [["run"], ["verify"], ["run", "unreviewed"], ["verify", "unreviewed"]])
def test_review_required_before_access(arguments: list[str]) -> None:
    result = subprocess.run(
        [sys.executable, "-B", str(REPOSITORY / "experiments/process-loader/probe.py"), *arguments],
        cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True,
        timeout=5, check=False,
    )
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}
