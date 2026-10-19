"""Public contract for local-canary normalization; no native fixture execution."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

import pytest


REPOSITORY = Path(__file__).resolve().parents[2]
SOURCE = REPOSITORY / "experiments/process-ipc-local"


def invoke(arguments: list[str]) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run([sys.executable, "-B", str(SOURCE / "probe.py"), *arguments], cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True, timeout=5, check=False)


@pytest.mark.parametrize("arguments", [["run"], ["verify"], ["run", "unreviewed"], ["verify", "unreviewed"]])
def test_review_required_before_native_access(arguments: list[str]) -> None:
    result = invoke(arguments)
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}


def test_plan_uses_known_local_receive_identity_and_exact_three_variants() -> None:
    result = invoke(["plan"])
    assert result.returncode == 0
    assert result.stderr == b""
    plan = json.loads(result.stdout)
    assert plan["variants"] == [
        {"name": "local-control", "execute_target": False, "confine": False, "normalization": "cleanup_after_positive_measurement"},
        {"name": "normalized-control", "execute_target": True, "confine": False, "normalization": "measured_before_exec"},
        {"name": "normalized-confined", "execute_target": True, "confine": True, "normalization": "measured_before_exec"},
    ]
    assert plan["channels"] == ["registered_application_ports", "bootstrap_special_port", "task_exception_ports", "initial_thread_exception_ports"]
    assert plan["inherited_prestate"] == "inventory_without_send_then_generic_normalization"
    assert plan["local_canary"] == "same_process_receive_plus_send_held_live"
    assert plan["positive_control"] == "each_slot_getter_matches_known_local_endpoint_and_send_receive_succeeds"
    assert plan["duplicate_aliases"] is True
    assert plan["post_normalization_send_check"] == "query_through_still_live_receive_name"
    assert plan["receive_destroyed_before_exec"] is True
    assert plan["normalizer"] == "byte_identical_reviewed_generic_routine"
    assert plan["unknown_endpoint_messages"] is False
    assert plan["parent_inheritance_proven"] is False
    assert plan["named_ipc_denial_proven"] is False
    assert plan["global_registration"] is False
    assert plan["exception_triggering"] is False
    assert plan["qualified_process_backend"] is False
    assert plan["automatic_retry"] is False
    assert plan["invocations"] == 3
    assert plan["limits"] == {"execution_seconds": 5, "cleanup_seconds": 2, "output_bytes_per_stream": 65536, "file_descriptors": 64}
    assert plan["native_run_requires"] == ["exact_source_and_binary_manifest", "independent_reviewed_sha256", "one_use_token"]


def test_generic_normalizer_is_an_exact_copy_of_reviewed_function() -> None:
    original = (REPOSITORY / "experiments/process-ipc/normalization.c").read_bytes()
    copied = (SOURCE / "normalization.c").read_bytes()
    marker = b"int lo_normalize_inherited_rights(void) {"
    def function(source: bytes) -> bytes:
        start = source.index(marker)
        return source[start:source.index(b"\n}\n", start) + 3]
    assert function(copied) == function(original)
