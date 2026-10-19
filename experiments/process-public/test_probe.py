"""Pure gate checks. These commands never prepare or launch a native fixture."""
import json
from pathlib import Path
import subprocess
import sys

import pytest

HARNESS = Path(__file__).with_name("probe.py")


def invoke(*arguments: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, "-B", str(HARNESS), *arguments], stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False)


def test_finite_plan() -> None:
    result = invoke("plan")
    assert result.returncode == 0
    plan = json.loads(result.stdout)
    assert plan["backends"] == ["interpreter", "native"]
    assert len(plan["cases"]) == len(set(plan["cases"])) == 12
    assert plan["qualified_invocations"] == 24
    assert plan["gate_red"] == {"backend": "interpreter", "case": "normal", "expected_failure": "runtime.process-unavailable", "attempts": 1}
    assert plan["standard_process_flags"] == ["--process-allow", "--process-launcher", "--process-read-root"]
    assert plan["automatic_retry"] is False
    assert plan["trap_fixture"] is False
    assert plan["wall_timeout_code"] == "runtime.cancelled"
    assert plan["file_authority_inherited_from_process"] is False


@pytest.mark.parametrize("arguments", [("run",), ("verify",), ("run", "gate-red", "unreviewed"), ("verify", "qualified", "unreviewed")])
def test_review_required_before_any_preparation_or_launch(arguments: tuple[str, ...]) -> None:
    result = invoke(*arguments)
    assert result.returncode == 2
    assert "reviewed manifest SHA256 is required" in result.stderr
