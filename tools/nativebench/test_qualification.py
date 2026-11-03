"""Native sample validity, independent checks and explicit build resource bounds."""

from __future__ import annotations
import copy
import pytest
import qualification as performance


def valid_report(lane: str = "process", name: str = "startup") -> dict:
    report = {
        "sample_plan": dict(performance.SAMPLE_PLAN),
        "warmup": 3,
        "measurement_complete": True,
        "expected_inventory": {lane: [name]},
        "complete_contract": False,
        "concurrency": 1,
        "other_benchmark_jobs": [],
        "benchmark_job_observation": "controlled process census",
    }
    if lane == "build":
        report[lane] = [
            {
                "name": name,
                "phase": phase,
                "sample": index,
                "elapsed_ns": 180_000_000_000,
                "peak_process_tree_rss_bytes": 2 << 30,
                "artifact_bytes": 64 << 20,
                "diagnostic_bytes": 1 << 20,
            }
            for phase, count in [("first-build", 1), ("warmup", 3), ("measurement", 10)]
            for index in range(count)
        ]
    else:
        if lane == "execution":
            sample = {
                "ns": 40,
                "total_ns": 40000000,
                "iterations": 1000000,
                "batch_size": 64,
            }
        elif lane == "http":
            sample = {
                "samples_ns": [1] * 200,
                "elapsed_ns": 300,
                "p95_ns": 1,
                "requests_per_second": 200e9 / 300,
            }
        else:
            sample = {"wall_ns": 40}
        record = {
            "name": name,
            "verified": True,
            "samples": [
                {"sample": index, **copy.deepcopy(sample)}
                for index in range(performance.SAMPLE_PLAN[lane])
            ],
        }
        if lane == "execution":
            record["native_allocation_volume"] = {
                "bytes": 12,
                "allocations": 1,
                "method": performance.ALLOCATION_METHOD,
            }
        if lane == "http":
            record.update(ready_verified=True, stopped_verified=True)
        report[lane] = [record]
    return report


@pytest.mark.parametrize(
    "lane,name",
    [
        ("execution", "startup"),
        ("process", "startup"),
        ("http", "echo"),
        ("build", "startup"),
    ],
)
def test_native_absolute_measurements_and_boundaries(lane, name):
    report = valid_report(lane, name)
    assert performance.evaluate(report) == []
    assert not {"callback_speedup", "native_over_interpreter"} & set(report)
    performance.completion(report, 0)
    assert report["measurement_complete"] and report["gates_passed"]
    assert not report["qualification_complete"]


@pytest.mark.parametrize(
    "mutation",
    [
        "missing",
        "duplicate",
        "short",
        "duplicate-index",
        "nan",
        "zero",
        "unverified",
        "paired",
        "wrong-plan",
    ],
)
def test_rejects_incomplete_or_removed_schema(mutation):
    report = valid_report()
    record = report["process"][0]
    if mutation == "missing":
        report["process"] = []
    elif mutation == "duplicate":
        report["process"].append(record)
    elif mutation == "short":
        record["samples"].pop()
    elif mutation == "duplicate-index":
        record["samples"][1]["sample"] = 0
    elif mutation == "nan":
        record["samples"][0]["wall_ns"] = float("nan")
    elif mutation == "zero":
        record["samples"][0]["wall_ns"] = 0
    elif mutation == "unverified":
        record["verified"] = False
    elif mutation == "paired":
        record["samples"][0] = {"sample": 0, "native": 40, "interpreter": 100}
    else:
        report["sample_plan"] = {"process": 10}
    with pytest.raises(ValueError):
        performance.evaluate(report)


@pytest.mark.parametrize(
    "mutation",
    [
        "old-result",
        "old-field",
        "short-requests",
        "wrong-throughput",
        "wrong-p95",
        "missing-stop",
    ],
)
def test_rejects_http_or_legacy_evidence(mutation):
    report = valid_report("http", "echo")
    record = report["http"][0]
    sample = record["samples"][0]
    if mutation == "old-result":
        record["samples_ns"] = record.pop("samples")
    elif mutation == "old-field":
        report["reference_go"] = "old interpreter"
    elif mutation == "short-requests":
        sample["samples_ns"].pop()
    elif mutation == "wrong-throughput":
        sample["requests_per_second"] = 1
    elif mutation == "wrong-p95":
        sample["p95_ns"] = 2
    else:
        record["stopped_verified"] = False
    with pytest.raises(ValueError):
        performance.evaluate(report)


@pytest.mark.parametrize(
    "mutation", ["batch", "iterations", "ns", "allocation", "verified"]
)
def test_execution_calibration_and_allocation_contract(mutation):
    report = valid_report("execution")
    record = report["execution"][0]
    if mutation == "allocation":
        record.pop("native_allocation_volume")
    elif mutation == "verified":
        record["verified"] = False
    else:
        record["samples"][0][
            {"batch": "batch_size", "iterations": "iterations", "ns": "ns"}[mutation]
        ] = 3
    with pytest.raises(ValueError):
        performance.evaluate(report)


def test_build_resources_remain_gates_without_runtime_ratios():
    report = valid_report("build")
    report["build"][0]["artifact_bytes"] += 1
    assert performance.evaluate(report) == ["build:resource-limits"]
    report["build"].pop()
    with pytest.raises(ValueError):
        performance.evaluate(report)
    report = valid_report()
    report["complete_contract"] = True
    with pytest.raises(ValueError, match="full"):
        performance.evaluate(report)
    assert len(performance.expected_inventory(performance.LANES, ())["execution"]) == 15
    with pytest.raises(ValueError, match="duplicate"):
        performance.expected_inventory(("process",), ("startup", "startup"))
