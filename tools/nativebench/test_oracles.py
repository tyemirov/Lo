"""Independent numerical checks through the public native measurement command."""

from __future__ import annotations
import json
import os
from pathlib import Path
import subprocess
import sys
import pytest
from oracles import training_reference, verify_output

ROOT = Path(__file__).resolve().parents[2]


def test_public_dense_native_measurement_has_independent_expectations(tmp_path: Path):
    manifest = os.environ.get("LO_LLVM_TOOLCHAIN")
    if manifest is None:
        pytest.skip("LO_LLVM_TOOLCHAIN selects the native fixture runtime")
    compiler = tmp_path / "compiler"
    result = subprocess.run(
        ["go", "build", "-o", str(compiler), "./cmd/lo"],
        cwd=ROOT,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, result.stderr
    work = tmp_path / "measurement"
    result = subprocess.run(
        [
            sys.executable,
            "-B",
            str(ROOT / "scripts/qualify_native.py"),
            "--binary",
            str(compiler),
            "--toolchain",
            manifest,
            "--workdir",
            str(work),
            "--lane",
            "execution",
            "--case",
            "training_dense",
        ],
        cwd=ROOT,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
        timeout=90,
    )
    assert result.returncode == 0, result.stderr
    report = json.loads((work / "report.json").read_text())
    record = report["execution"][0]
    assert report["measurement_complete"] and not report["qualification_complete"]
    assert record["verified"] and len(record["samples"]) == 10
    assert (
        record["native_allocation_volume"]["bytes"] > 0
        and record["native_allocation_volume"]["allocations"] > 0
    )
    assert "interpreter" not in record["binaries"]
    assert (work / "execution/training_dense/oracle.json").is_file()


@pytest.mark.parametrize("kind", ["training_mvp", "training_dense"])
def test_training_oracle_rejects_perturbed_parameters_or_shapes(kind):
    reference = training_reference(kind)
    fields = {
        name: {"type": "Float", "value": str(reference[name])}
        for name in ("initial_loss", "loss")
    }
    parameters = {
        name: {
            "type": "Tensor<FP32>",
            "value": {
                "dtype": "fp32",
                "device": "cpu",
                "shape": list(map(str, shape)),
                "values": list(map(str, values)),
            },
        }
        for name, (shape, values) in reference["parameters"].items()
    }
    fields["parameters"] = {"type": "Record", "value": parameters}
    envelope = {
        "result": {
            "type": "Record!",
            "status": "ok",
            "value": {"type": "Record", "value": fields},
        }
    }
    verify_output(json.dumps(envelope), {"kind": kind})
    parameters["w1"]["value"]["values"][0] = "NaN"
    with pytest.raises(ValueError):
        verify_output(json.dumps(envelope), {"kind": kind})
    parameters["w1"]["value"]["values"][0] = "1000000"
    with pytest.raises(ValueError):
        verify_output(json.dumps(envelope), {"kind": kind})
    parameters["w1"]["value"]["shape"] = ["999"]
    with pytest.raises(ValueError):
        verify_output(json.dumps(envelope), {"kind": kind})
