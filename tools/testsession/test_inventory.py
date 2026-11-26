"""Exercise original-case preservation through the inventory command."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name("inventory.py")


@pytest.fixture
def inventory_fixture(tmp_path: Path) -> tuple[Path, Path, Path]:
    assert tmp_path.resolve().is_relative_to(ROOT / "_scratch")
    python = tmp_path / "tools/fixture/test_cases.py"
    rust = tmp_path / "runtime/native/src/value.rs"
    python.parent.mkdir(parents=True)
    rust.parent.mkdir(parents=True)
    python.write_text("@pytest.mark.parametrize('value', [1, 2])\ndef test_case(value):\n    pass\n")
    rust.write_text('const EXAMPLE: &str = r#"#[test] fn false_case() {}"#;\n'
                    '// #[test] fn comment_case() {}\n#[test]\nfn kept() {}\n')
    baseline = tmp_path / "inventory.json"
    baseline.write_text(json.dumps({
        "python": [{"file": "tools/fixture/test_cases.py", "name": "test_case",
                    "parameters": ["pytest.mark.parametrize('value', [1, 2])"]}],
        "rust": [{"file": "runtime/native/src/value.rs", "name": "kept"}],
    }))
    return python, rust, baseline


def invoke(fixture: tuple[Path, Path, Path]) -> subprocess.CompletedProcess[str]:
    baseline = fixture[2]
    return subprocess.run(
        [sys.executable, "-B", str(SCRIPT), "--root", str(baseline.parent), "--baseline", str(baseline)],
        cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False,
    )


def test_inventory_preserves_original_cases_and_ignores_rust_literal_decoys(inventory_fixture) -> None:
    result = invoke(inventory_fixture)
    assert result.returncode == 0, result.stderr
    data = json.loads(result.stdout)
    assert data["python"] == [{"file": "tools/fixture/test_cases.py", "name": "test_case",
                               "parameters": ["pytest.mark.parametrize('value', [1, 2])"]}]
    assert data["rust"] == [{"file": "runtime/native/src/value.rs", "name": "kept"}]


@pytest.mark.parametrize("mutation", ["python", "parameters", "rust"])
def test_inventory_rejects_missing_original_cases_and_parameters(inventory_fixture, mutation: str) -> None:
    python, rust, _ = inventory_fixture
    if mutation == "python":
        python.write_text("def renamed_case(value):\n    pass\n")
    elif mutation == "parameters":
        python.write_text("@pytest.mark.parametrize('value', [1])\ndef test_case(value):\n    pass\n")
    else:
        rust.write_text("fn kept() {}\n")
    result = invoke(inventory_fixture)
    assert result.returncode == 1
    assert "original" in result.stderr and mutation in result.stderr
    assert not result.stdout
