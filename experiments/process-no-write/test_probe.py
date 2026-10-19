"""Public review gate and finite matrix contract without native execution."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

import pytest

REPOSITORY = Path(__file__).resolve().parents[2]
SOURCE = REPOSITORY / "experiments/process-no-write/probe.py"


def invoke(arguments: list[str]) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run([sys.executable, "-B", str(SOURCE), *arguments], cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True, timeout=5, check=False)


@pytest.mark.parametrize("arguments", [["run"], ["verify"], ["run", "unreviewed"], ["verify", "unreviewed"]])
def test_review_required_before_access(arguments: list[str]) -> None:
    result = invoke(arguments)
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}


def test_public_plan_is_finite_and_does_not_claim_qualification() -> None:
    result = invoke(["plan"])
    assert result.returncode == 0
    assert result.stderr == b""
    plan = json.loads(result.stdout)
    assert plan["groups"] == ["write", "namespace", "unix", "descriptors"]
    assert plan["invocations"] == [f"{authority}-{group}" for group in plan["groups"] for authority in ("control", "confined")]
    assert plan["qualified_process_backend"] is False
    assert plan["shared_mmap_after_denied_readwrite_open"] == "unreachable"
    assert plan["descriptor_canary_high"] == 97
    assert plan["limits"] == {"execution_seconds": 5, "cleanup_seconds": 2, "output_bytes_per_stream": 65536, "file_descriptors": 64}
