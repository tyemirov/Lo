"""Verify ownership of the precompiled semantic-test executable."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name("run.py")


@pytest.mark.parametrize("hardlink", [False, True])
def test_semantic_runner_preserves_precompiled_executor_before_output(hardlink: bool, tmp_path: Path) -> None:
    assert tmp_path.resolve().is_relative_to(ROOT / "_scratch")
    host = {"Darwin": "darwin", "Linux": "linux"}[platform.system()] + "/arm64"
    output = tmp_path / "run"
    output.mkdir()
    log = output / ("semantic-native-" + host.replace("/", "-") + ".jsonl")
    executor = tmp_path / "integration.test" if hardlink else log
    original = b"precompiled test executable fixture"
    executor.write_bytes(original)
    if hardlink:
        os.link(executor, log)
    compiler = tmp_path / "frontend"
    compiler.write_bytes(b"frontend fixture")
    runtime = tmp_path / "runtime.a"
    runtime.write_bytes(b"runtime fixture")
    manifest = tmp_path / "toolchain.json"
    manifest.write_text(json.dumps({"abi": "lo-native-3", "host": host, "targets": {
        host: {"runtime": {"path": "runtime.a", "sha256": hashlib.sha256(runtime.read_bytes()).hexdigest()}}
    }}))
    matrix = json.loads((ROOT / "testdata/semantic-migration/matrix.json").read_text())
    for row in matrix["contracts"]:
        row["tests"] = ["TestSemanticCoverageInventory"]
    matrix["gaps"] = []
    selected = tmp_path / "matrix.json"
    selected.write_text(json.dumps(matrix))
    result = subprocess.run(
        [sys.executable, "-B", str(SCRIPT), "run", "--matrix", str(selected),
         "--output-root", str(output), "--compiler", str(compiler), "--toolchain", str(manifest),
         "--allow-partial"], cwd=ROOT,
        env=dict(os.environ, LO_TEST_INTEGRATION_BINARY=str(executor)),
        stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False,
    )
    assert result.returncode == 1, result.stdout + result.stderr
    assert "overlaps an input" in result.stderr
    assert executor.read_bytes() == original
