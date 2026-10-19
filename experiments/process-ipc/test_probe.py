"""Public finite-matrix and review gates; these tests launch no native fixture."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

import pytest


REPOSITORY = Path(__file__).resolve().parents[2]
HARNESS = REPOSITORY / "experiments/process-ipc/probe.py"


def invoke(arguments: list[str]) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run([sys.executable, "-B", str(HARNESS), *arguments], cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True, timeout=5, check=False)


@pytest.mark.parametrize("arguments", [["run"], ["verify"], ["run", "unreviewed"], ["verify", "unreviewed"]])
def test_review_required_before_native_access(arguments: list[str]) -> None:
    result = invoke(arguments)
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}


def test_plan_has_exact_three_variants_and_channel_controls() -> None:
    result = invoke(["plan"])
    assert result.returncode == 0
    assert result.stderr == b""
    plan = json.loads(result.stdout)
    assert plan["variants"] == [
        {"name": "inheritance-control", "normalize": False, "confine": False, "exec_target": True},
        {"name": "normalized-control", "normalize": True, "confine": False, "exec_target": True},
        {"name": "normalized-confined", "normalize": True, "confine": True, "exec_target": True},
    ]
    assert plan["channels"] == ["registered_application_ports", "bootstrap_special_port", "task_exception_ports", "initial_thread_exception_ports"]
    assert plan["per_channel_control_required"] is True
    assert plan["absent_or_unsupported_control"] == "inconclusive"
    assert plan["self_runtime_rights"] == "inventoried_separately"
    assert plan["bootstrap_clearing"] == "authority_removal_only"
    assert plan["canaries"] == "parent_owned_only"
    assert plan["global_registration"] is False
    assert plan["exception_triggering"] is False
    assert plan["cleanup"] == {"send_rights": True, "receive_rights": True, "port_arrays": True, "owned_processes": True}
    assert plan["limits"] == {"execution_seconds": 5, "cleanup_seconds": 2, "output_bytes_per_stream": 65536, "file_descriptors": 64}
    assert plan["invocations"] == 3
    assert plan["automatic_retry"] is False
    assert plan["qualified_process_backend"] is False


def test_preparation_is_not_native_execution() -> None:
    result = invoke(["plan"])
    assert result.returncode == 0
    plan = json.loads(result.stdout)
    assert plan["native_run_requires"] == ["exact_source_and_binary_manifest", "independent_reviewed_sha256", "one_use_token"]
    assert plan["standard_process_activation"] == "unavailable"
