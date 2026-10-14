"""Measure complete CPU training runs through the Lo CLI."""

import argparse
import hashlib
import json
import math
import os
import platform
import random
import statistics
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Workload:
    name: str
    source: Path
    steps: int


def capture(command: list[str]) -> str:
    return subprocess.run(command, check=True, text=True, capture_output=True).stdout.strip()


def interval(samples: list[float]) -> list[float]:
    rng = random.Random(5006007)
    medians = sorted(statistics.median(rng.choices(samples, k=len(samples))) for _ in range(2000))
    return [medians[49], medians[1949]]


def measure(binary: Path, workload: Workload, count: int) -> dict[str, object]:
    command = [str(binary.resolve()), "run", str(workload.source), "--format", "json", "--max-steps", "10000000"]
    samples: list[float] = []
    digest = ""
    initial_loss = final_loss = 0.0
    for sample in range(count + 2):
        start = time.perf_counter_ns()
        completed = subprocess.run(command, check=True, capture_output=True, timeout=120)
        elapsed = (time.perf_counter_ns() - start) / 1_000_000
        if completed.stderr:
            raise RuntimeError(f"{workload.name}: unexpected standard error")
        actual_digest = hashlib.sha256(completed.stdout).hexdigest()
        if digest and actual_digest != digest:
            raise RuntimeError(f"{workload.name}: training result changed between samples")
        digest = actual_digest
        result = json.loads(completed.stdout)["result"]["value"]["value"]
        initial_loss = float(result["initial_loss"]["value"])
        final_loss = float(result["loss"]["value"])
        if not math.isfinite(final_loss) or not final_loss < initial_loss:
            raise RuntimeError(f"{workload.name}: training did not reduce finite loss")
        if sample >= 2:
            samples.append(elapsed)
    ordered = sorted(samples)
    return {
        "name": workload.name,
        "source": str(workload.source),
        "source_sha256": hashlib.sha256(workload.source.read_bytes()).hexdigest(),
        "updates": workload.steps,
        "max_steps": 10_000_000,
        "warmup_runs": 2,
        "samples_ms": samples,
        "median_ms": statistics.median(samples),
        "median_bootstrap_95_percent_ms": interval(samples),
        "p95_ms": ordered[math.ceil(0.95 * len(ordered)) - 1],
        "result_sha256": digest,
        "initial_loss": initial_loss,
        "final_loss": final_loss,
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--samples", type=int, default=10)
    args = parser.parse_args()
    if args.samples < 10:
        parser.error("use at least ten samples")
    if os.environ.get("GOMAXPROCS") != "1":
        parser.error("set GOMAXPROCS=1 for the single-worker baseline")
    system = platform.system()
    if system == "Darwin":
        processor = capture(["sysctl", "-n", "machdep.cpu.brand_string"])
    elif system == "Linux":
        processor = Path("/proc/cpuinfo").read_text()
    else:
        parser.error("this benchmark qualifies macOS and Linux hosts")
    workloads = [
        Workload("MVP200", Path("examples/training_cpu.lo"), 200),
        Workload("Dense20", Path("tests/benchmark/testdata/training_dense.lo"), 20),
    ]
    report = {
        "platform": platform.platform(),
        "architecture": platform.machine(),
        "processor": processor,
        "go_version": capture(["go", "version"]),
        "gomaxprocs": 1,
        "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
        "binary_bytes": len(args.binary.read_bytes()),
        "clock": "perf_counter_ns",
        "cache_policy": "two warmup processes per workload; OS caches remain warm",
        "concurrency": 1,
        "workloads": [measure(args.binary, workload, args.samples) for workload in workloads],
    }
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
