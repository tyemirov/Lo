"""Public whole-lane report admission; synthetic observations are not timing evidence."""

from __future__ import annotations
import copy
import json
from pathlib import Path
import subprocess
import sys
import pytest
from qualification import LANES, expected_inventory, completion
from identity import (
    CACHE,
    ORDER,
    TIMER_CONTRACT,
    LIMITS,
    LOAD_POLICY,
    HARNESS,
    ORACLE_SOURCES,
)
from test_qualification import valid_report

ROOT = Path(__file__).resolve().parents[2]
CLI = ROOT / "tools/nativebench/merge.py"
SHA = "1" * 64


def lane_report(lane: str) -> dict:
    names = expected_inventory((lane,), ())[lane]
    report = valid_report(lane, names[0])
    report[lane] = [
        record for name in names for record in valid_report(lane, name)[lane]
    ]
    report["expected_inventory"] = {lane: names}
    report.update(
        order=ORDER,
        cache=CACHE,
        max_steps=10000000,
        target="darwin/arm64",
        lanes=[lane],
        cases=[],
    )
    report["merge_identity"] = {
        "target": "darwin/arm64",
        "environment": {
            "system": "fixture",
            "machine": "arm64",
            "processor": "fixture",
            "execution": "native",
        },
        "accelerator": "none",
        "package": {
            name + "_sha256": SHA
            for name in ("compiler", "manifest", "runtime", "inventory", "provenance")
        },
        "source": {"semantic_source_sha256": SHA},
        "producer": {
            "kind": "darwin-native",
            "tools": {"python": "controlled"},
            "source_identity": {"scripts/qualify_native.py": SHA},
        },
        "oracle": {name: SHA for name in ORACLE_SOURCES},
        "harness": {name: SHA for name in HARNESS},
        "evaluator": {"tools/nativebench/qualification.py": SHA},
        "measurement": {
            "sample_plan": dict(report["sample_plan"]),
            "warmup": 3,
            "order": ORDER,
            "cache": CACHE,
            "max_steps": 10000000,
            "concurrency": 1,
            "limits": dict(LIMITS),
            "timer_contract": dict(TIMER_CONTRACT),
            "load_policy": LOAD_POLICY,
        },
    }
    completion(report, 0)
    return report


def reports(work: Path) -> list[Path]:
    paths = []
    for lane in LANES:
        path = work / (lane + ".json")
        path.write_text(json.dumps(lane_report(lane)))
        paths.append(path)
    return paths


def invoke(paths, output):
    return subprocess.run(
        [
            sys.executable,
            "-B",
            str(CLI),
            *[item for path in paths for item in ("--report", str(path))],
            "--output",
            str(output),
        ],
        cwd=ROOT,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
    )


def test_public_merge_keeps_all_native_samples(tmp_path):
    paths = reports(tmp_path)
    output = tmp_path / "merged.json"
    result = invoke(paths, output)
    assert result.returncode == 0, result.stderr
    report = json.loads(output.read_text())
    assert report["qualification_complete"] and len(report["input_reports"]) == 4
    assert len(report["execution"][0]["samples"]) == 10
    assert "reference" not in report["merge_identity"]
    assert invoke(paths, output).returncode == 2


@pytest.mark.parametrize(
    "problem",
    [
        "missing",
        "duplicate",
        "paired",
        "old-identity",
        "wrong-runtime",
        "unknown-identity",
        "missing-case",
        "short-samples",
        "incomplete",
        "preliminary",
        "diagnostic",
        "duplicate-json",
    ],
)
def test_public_merge_rejects_invalid_or_old_reports(tmp_path, problem):
    paths = reports(tmp_path)
    record = json.loads(paths[0].read_text())
    if problem == "missing":
        paths.pop()
    elif problem == "duplicate":
        paths.append(paths[0])
    elif problem == "paired":
        record["execution"][0]["samples"][0] = {
            "sample": 0,
            "results": {"native": 40, "interpreter": 100},
        }
    elif problem == "old-identity":
        record["merge_identity"]["reference"] = {"go": "removed"}
    elif problem == "wrong-runtime":
        record["merge_identity"]["package"]["runtime_sha256"] = "2" * 64
    elif problem == "unknown-identity":
        record["merge_identity"]["other"] = True
    elif problem == "missing-case":
        record["execution"].pop()
    elif problem == "short-samples":
        record["execution"][0]["samples"].pop()
    elif problem == "incomplete":
        record["measurement_complete"] = False
    elif problem == "preliminary":
        record["preliminary"] = {}
    elif problem == "diagnostic":
        record["cases"] = ["startup"]
    if problem == "duplicate-json":
        paths[0].write_text(
            '{"measurement_complete":true,"measurement_complete":false}'
        )
    else:
        paths[0].write_text(json.dumps(record))
    output = tmp_path / "merged.json"
    result = invoke(paths, output)
    assert result.returncode == 2, result.stderr
    assert not output.exists()


def test_native_absolute_resource_failure_preserves_measurement(tmp_path):
    paths = reports(tmp_path)
    record = json.loads(paths[-1].read_text())
    record["build"][0]["artifact_bytes"] += 1
    paths[-1].write_text(json.dumps(record))
    output = tmp_path / "merged.json"
    result = invoke(paths, output)
    assert result.returncode == 1, result.stderr
    report = json.loads(output.read_text())
    assert report["measurement_complete"] and not report["qualification_complete"]
    assert report["gate_failures"] == ["build:resource-limits"]
