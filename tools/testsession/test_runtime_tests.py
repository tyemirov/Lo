"""Exercise early failures of the Rust unit-test session command."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name("runtime_tests.py")


@pytest.mark.parametrize("receipt", [None, {}])
def test_runtime_session_rejects_missing_or_invalid_inputs_before_work(tmp_path: Path, receipt: dict | None) -> None:
    assert tmp_path.resolve().is_relative_to(ROOT / "_scratch")
    preparation = tmp_path / "preparation"
    preparation.mkdir()
    if receipt is not None:
        (preparation / "rust.json").write_text(json.dumps(receipt))
    work = tmp_path / "work"
    result = subprocess.run(
        [sys.executable, "-B", str(SCRIPT), "--preparation", str(preparation), "--work", str(work)],
        cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False,
    )
    assert result.returncode == 1
    assert "runtime tests:" in result.stderr
    assert not work.exists()
    assert not result.stdout
