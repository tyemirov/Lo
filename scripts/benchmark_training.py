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
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools/nativebench"))
from oracles import verify_output

ROOT = Path(__file__).resolve().parents[1]


@dataclass(frozen=True)
class Workload:
    name: str
    source: Path
    steps: int
    oracle: str


def capture(command: list[str]) -> str:
    return subprocess.run(
        command, check=True, text=True, capture_output=True
    ).stdout.strip()


def interval(samples: list[float]) -> list[float]:
    rng = random.Random(5006007)
    medians = sorted(
        statistics.median(rng.choices(samples, k=len(samples))) for _ in range(2000)
    )
    return [medians[49], medians[1949]]


def measure(binary: Path, workload: Workload, count: int) -> dict[str, object]:
    command = [
        str(binary.resolve()),
        "run",
        "--format",
        "json",
        "--max-steps",
        "10000000",
    ]
    samples: list[float] = []
    digest = ""
    initial_loss = final_loss = 0.0
    for sample in range(count + 2):
        start = time.perf_counter_ns()
        completed = subprocess.run(
            command,
            check=True,
            capture_output=True,
            timeout=120,
            stdin=subprocess.DEVNULL,
        )
        elapsed = (time.perf_counter_ns() - start) / 1_000_000
        if completed.stderr:
            raise RuntimeError(f"{workload.name}: unexpected standard error")
        verify_output(completed.stdout.decode(), {"kind": workload.oracle})
        actual_digest = hashlib.sha256(completed.stdout).hexdigest()
        if digest and actual_digest != digest:
            raise RuntimeError(
                f"{workload.name}: training result changed between samples"
            )
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
    parser.add_argument("--toolchain", type=Path, required=True)
    parser.add_argument(
        "--workdir", type=Path, default=ROOT / "_scratch/native-training"
    )
    args = parser.parse_args()
    if args.samples < 10:
        parser.error("use at least ten samples")
    system = platform.system()
    if system == "Darwin":
        processor = capture(["sysctl", "-n", "machdep.cpu.brand_string"])
    elif system == "Linux":
        processor = Path("/proc/cpuinfo").read_text()
    else:
        parser.error("this benchmark qualifies macOS and Linux hosts")
    workloads = [
        Workload("MVP200", ROOT / "examples/training_cpu.lo", 200, "training_mvp"),
        Workload(
            "Dense20",
            ROOT / "tests/benchmark/testdata/training_dense.lo",
            20,
            "training_dense",
        ),
    ]
    work = args.workdir.resolve()
    if not work.is_relative_to(ROOT / "_scratch"):
        parser.error("workdir must remain inside Lo/_scratch")
    work.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix="training-", dir=work))
    observations = []
    for workload in workloads:
        artifact = run / workload.name
        subprocess.run(
            [
                str(args.binary.resolve()),
                "build",
                str(workload.source),
                "--toolchain",
                str(args.toolchain.resolve()),
                "--output",
                str(artifact),
            ],
            cwd=ROOT,
            stdin=subprocess.DEVNULL,
            check=True,
            capture_output=True,
            timeout=180,
        )
        result = measure(artifact, workload, args.samples)
        result["artifact_sha256"] = hashlib.sha256(artifact.read_bytes()).hexdigest()
        result["artifact_bytes"] = artifact.stat().st_size
        observations.append(result)
    report = {
        "platform": platform.platform(),
        "architecture": platform.machine(),
        "processor": processor,
        "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
        "binary_bytes": len(args.binary.read_bytes()),
        "clock": "perf_counter_ns",
        "cache_policy": "two warmup processes per workload; OS caches remain warm",
        "concurrency": 1,
        "workloads": observations,
        "toolchain_sha256": hashlib.sha256(args.toolchain.read_bytes()).hexdigest(),
        "work": str(run),
    }
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
