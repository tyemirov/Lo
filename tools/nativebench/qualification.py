"""Validate native measurements and explicit resource bounds."""

from __future__ import annotations

import math
import random
import statistics
from typing import Any

WARMUP = 3
BUILD_SECONDS = 180
BUILD_BYTES = 64 << 20
BUILD_RSS = 2 << 30
DIAGNOSTIC_BYTES = 1 << 20
ALLOCATION_METHOD = "successful allocator requests during fresh session execution and checked cleanup; admission excluded"
ALLOCATION_ACCOUNTING = {
    "native": "successful allocator requests during fresh native session execution and checked cleanup; admission excluded; separate untimed process"
}


def interval(values: list[float]) -> list[float]:
    if not values:
        raise ValueError("confidence interval requires observations")
    for value in values:
        number(value, "confidence observation")
    rng = random.Random(1001)
    estimates = sorted(
        statistics.median(rng.choices(values, k=len(values))) for _ in range(2000)
    )
    return [estimates[49], estimates[1949]]


def absolute_summary(values: list[float]) -> dict[str, Any]:
    for value in values:
        number(value, "native observation")
    return {
        "median": statistics.median(values),
        "p95": sorted(values)[math.ceil(len(values) * 0.95) - 1],
        "median_bootstrap_95_percent": interval(values),
    }


LANES = ("execution", "process", "http", "build")
SAMPLE_PLAN = {"execution": 10, "process": 10, "http": 100, "build": 10}


def validate_sample_plan(value: Any) -> None:
    if (
        not isinstance(value, dict)
        or set(value) != set(SAMPLE_PLAN)
        or any(
            type(value[lane]) is not int or value[lane] != count
            for lane, count in SAMPLE_PLAN.items()
        )
    ):
        raise ValueError(
            "sample_plan requires execution=10, process=10, http=100, build=10"
        )


CALLBACK_CASES = tuple(
    f"{operation}_{size}"
    for size in (1000, 10000)
    for operation in ("map", "filter", "fold")
)
CORPUS_CASES = (
    "startup",
    "list_sum",
    *(
        f"{operation}_{size}"
        for size in (1000, 10000)
        for operation in ("map", "filter", "fold", "records", "bigint")
    ),
    "unicode",
)
FULL_INVENTORY = {
    "execution": (*CORPUS_CASES, "training_mvp", "training_dense"),
    "process": (*CORPUS_CASES, "file"),
    "http": ("echo", "computed"),
    "build": (*CORPUS_CASES, "training_mvp", "training_dense"),
}


def expected_inventory(
    lanes: tuple[str, ...], cases: tuple[str, ...]
) -> dict[str, list[str]]:
    if len(set(lanes)) != len(lanes) or len(set(cases)) != len(cases):
        raise ValueError("duplicate performance lane or case selection")
    if not lanes or set(lanes) - set(LANES):
        raise ValueError("unknown or empty performance lane selection")
    available = set().union(*(set(FULL_INVENTORY[lane]) for lane in lanes))
    if set(cases) - available:
        raise ValueError("performance cases do not belong to selected lanes")
    inventory = {
        lane: [name for name in FULL_INVENTORY[lane] if not cases or name in cases]
        for lane in lanes
    }
    return {lane: names for lane, names in inventory.items() if names}


def number(value: Any, subject: str, *, zero: bool = False) -> float:
    diagnostic = (
        f'{subject} requires a finite {"nonnegative" if zero else "positive"} number'
    )
    if type(value) not in (int, float):
        raise ValueError(diagnostic)
    try:
        converted = float(value)
    except OverflowError as error:
        raise ValueError(diagnostic) from error
    if not math.isfinite(converted) or (converted < 0 if zero else converted <= 0):
        raise ValueError(diagnostic)
    return converted


def sample_inventory(samples: Any, count: int, subject: str) -> None:
    if not isinstance(samples, list) or len(samples) != count:
        raise ValueError(f"{subject} requires exactly {count} samples")
    indices = [sample.get("sample") for sample in samples]
    if any(type(index) is not int for index in indices) or indices != list(
        range(count)
    ):
        raise ValueError(
            f"{subject} requires unique sample indices in measurement order"
        )


def native_batch_timing(timing: Any) -> None:
    """Admit the actual calibrated C batch, excluding calibration iterations."""
    if not isinstance(timing, dict):
        raise ValueError("native batch requires a timing object")
    for field in ("ns", "total_ns", "iterations", "batch_size"):
        if type(timing.get(field)) is not int or timing[field] <= 0:
            raise ValueError(f"native batch {field} requires a positive integer")
    total, iterations, batch = (
        timing["total_ns"],
        timing["iterations"],
        timing["batch_size"],
    )
    if batch not in (1, 2, 4, 8, 16, 32, 64) or iterations % batch:
        raise ValueError("native batch requires a calibrated size and complete batches")
    if iterations > 1000000 + batch - 1:
        raise ValueError("native batch exceeds its iteration stop envelope")
    if total < 200000000 and iterations < 1000000:
        raise ValueError("native batch did not reach either stopping condition")
    if timing["ns"] != total // iterations:
        raise ValueError("native batch ns differs from total_ns divided by iterations")


def validate_measurements(report: dict[str, Any]) -> None:
    removed = {
        "reference",
        "reference_go",
        "reference_source_identity",
        "callback_speedup",
        "native_over_interpreter",
        "interpreter",
    }
    if removed & set(report):
        raise ValueError("obsolete paired runtime report fields")
    if report.get("order", "sequential native samples") != "sequential native samples":
        raise ValueError("obsolete paired sample order")
    if report.get("concurrency") != 1 or type(report.get("concurrency")) is not int:
        raise ValueError("qualification requires concurrency one")
    if not isinstance(report.get("other_benchmark_jobs"), list) or not report.get(
        "benchmark_job_observation"
    ):
        raise ValueError("qualification requires informational process observations")
    validate_sample_plan(report.get("sample_plan"))
    if report.get("warmup") != WARMUP:
        raise ValueError("qualification requires three warmups")
    inventory = report.get("expected_inventory")
    if not isinstance(inventory, dict) or not inventory or set(inventory) - set(LANES):
        raise ValueError("qualification requires expected lane inventory")
    if report.get("complete_contract") and inventory != expected_inventory(LANES, ()):
        raise ValueError("full qualification requires exact full inventory")
    if {lane for lane in LANES if lane in report} != set(inventory):
        raise ValueError("measured lanes differ from inventory")
    for lane, names in inventory.items():
        if (
            not isinstance(names, list)
            or not names
            or len(set(names)) != len(names)
            or set(names) - set(FULL_INVENTORY[lane])
        ):
            raise ValueError("invalid case inventory")
        records = report[lane]
        if not isinstance(records, list):
            raise ValueError("invalid lane records")
        if lane == "build":
            if set(record["name"] for record in records) != set(names):
                raise ValueError("build case inventory differs")
            for name in names:
                actual = [
                    (r["phase"], r["sample"]) for r in records if r["name"] == name
                ]
                expected = [
                    (phase, index)
                    for phase, count in [
                        ("first-build", 1),
                        ("warmup", WARMUP),
                        ("measurement", SAMPLE_PLAN[lane]),
                    ]
                    for index in range(count)
                ]
                if sorted(actual) != sorted(expected):
                    raise ValueError("invalid build phase samples")
            for record in records:
                for key in (
                    "elapsed_ns",
                    "peak_process_tree_rss_bytes",
                    "artifact_bytes",
                ):
                    number(record[key], key)
                number(record["diagnostic_bytes"], "diagnostic bytes", zero=True)
            continue
        if len(records) != len(names) or {r["name"] for r in records} != set(names):
            raise ValueError("case inventory differs")
        for record in records:
            if record.get("verified") is not True:
                raise ValueError("independent verification missing")
            if {
                "samples_ns",
                "native_over_interpreter",
                "callback_speedup",
                "both_servers_alive",
            } & set(record):
                raise ValueError("obsolete paired case schema")
            samples = record.get("samples")
            sample_inventory(samples, SAMPLE_PLAN[lane], lane + ":" + record["name"])
            for sample in samples:
                if lane == "execution":
                    if set(sample) != {
                        "sample",
                        "ns",
                        "total_ns",
                        "iterations",
                        "batch_size",
                    }:
                        raise ValueError("execution requires direct native timing")
                    native_batch_timing(sample)
                elif lane == "process":
                    if set(sample) != {"sample", "wall_ns"}:
                        raise ValueError("process requires direct native wall time")
                    number(sample["wall_ns"], "process wall time")
                else:
                    if set(sample) != {
                        "sample",
                        "samples_ns",
                        "elapsed_ns",
                        "p95_ns",
                        "requests_per_second",
                    }:
                        raise ValueError("HTTP requires direct native batch")
                    times = sample["samples_ns"]
                    if not isinstance(times, list) or len(times) != 200:
                        raise ValueError("HTTP requires 200 requests")
                    for value in times:
                        number(value, "HTTP request time")
                    elapsed = number(sample["elapsed_ns"], "HTTP elapsed")
                    if sample["p95_ns"] != sorted(times)[189] or not math.isclose(
                        number(sample["requests_per_second"], "HTTP throughput"),
                        200e9 / elapsed,
                        rel_tol=1e-12,
                    ):
                        raise ValueError("HTTP derived metrics differ")
            if lane == "execution":
                allocation = record.get("native_allocation_volume")
                if (
                    not isinstance(allocation, dict)
                    or set(allocation) != {"bytes", "allocations", "method"}
                    or allocation.get("method") != ALLOCATION_METHOD
                ):
                    raise ValueError("native allocation volume missing")
                for key in ("bytes", "allocations"):
                    number(allocation[key], "native allocation volume", zero=True)
            if lane == "http" and (
                record.get("ready_verified") is not True
                or record.get("stopped_verified") is not True
            ):
                raise ValueError("HTTP lifecycle verification missing")


def evaluate(report: dict[str, Any]) -> list[str]:
    validate_measurements(report)
    failures = []
    for record in report.get("execution", []):
        record["timing_ns"] = absolute_summary([s["ns"] for s in record["samples"]])
    if "execution" in report:
        report["allocation_accounting"] = dict(ALLOCATION_ACCOUNTING)
    for record in report.get("process", []):
        record["wall_ns"] = absolute_summary([s["wall_ns"] for s in record["samples"]])
    for record in report.get("http", []):
        record["p95_ns"] = absolute_summary([s["p95_ns"] for s in record["samples"]])
        record["requests_per_second"] = absolute_summary(
            [s["requests_per_second"] for s in record["samples"]]
        )
    for record in report.get("build", []):
        if (
            record["elapsed_ns"] > BUILD_SECONDS * 1e9
            or record["peak_process_tree_rss_bytes"] > BUILD_RSS
            or record["artifact_bytes"] > BUILD_BYTES
            or record["diagnostic_bytes"] > DIAGNOSTIC_BYTES
        ):
            failures.append("build:resource-limits")
    return sorted(set(failures))


def completion(report: dict[str, Any], status: int) -> None:
    report["measurement_complete"] = False
    report["gates_passed"] = False
    report["qualification_complete"] = False
    if status:
        report["measurement_error"] = report.get(
            "error", "native measurement process failed"
        )
        return
    try:
        report["gate_failures"] = evaluate(report)
    except (ValueError, KeyError, TypeError) as error:
        report["measurement_error"] = str(error)
        return
    report["measurement_complete"] = True
    report["gates_passed"] = not report["gate_failures"]
    report["qualification_complete"] = bool(
        report["gates_passed"]
        and report.get("complete_contract")
        and "preliminary" not in report
    )
