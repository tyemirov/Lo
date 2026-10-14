"""Measure paired native execution, process latency, HTTP, and build resources."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import platform
import random
import re
import shutil
import signal
import statistics
import subprocess
import sys
import time

from benchmark_native import measured

WARMUP = 3
STEPS = "10000000"
BACKENDS = ("interpreter", "native")


@dataclass(frozen=True)
class Config:
    root: Path
    work: Path
    binary: Path
    samples: int


def capture(command: list[str], cwd: Path) -> str:
    return subprocess.check_output(command, cwd=cwd, text=True).strip()


def processor_identity(root: Path) -> str:
    if platform.system() == "Darwin":
        return capture(["sysctl", "-n", "machdep.cpu.brand_string"], root)
    fields = dict(line.split(":", 1) for line in Path("/proc/cpuinfo").read_text().split("\n\n", 1)[0].splitlines() if ":" in line)
    fields = {key.strip(): value.strip() for key, value in fields.items()}
    return ", ".join(key+"="+fields[key] for key in ("CPU implementer", "CPU architecture", "CPU part", "CPU revision"))


def interval(values: list[float]) -> list[float]:
    rng = random.Random(1001)
    estimates = sorted(statistics.median(rng.choices(values, k=len(values))) for _ in range(2000))
    return [estimates[49], estimates[1949]]


def comparison(pairs: list[dict[str, float]]) -> dict:
    ratios = [pair["native"] / pair["interpreter"] for pair in pairs]
    bounds = interval(ratios)
    return {"native_over_interpreter": statistics.median(ratios), "bootstrap_95_percent": bounds,
            "gate": "pass" if bounds[1] <= 1.1 else "fail" if bounds[0] > 1.1 else "inconclusive"}


def order(sample: int) -> tuple[str, str]:
    return BACKENDS if sample % 2 == 0 else ("native", "interpreter")


def execution(config: Config) -> list[dict]:
    directory = config.work / "execution"
    packages = capture(["go", "run", "./tools/nativebench", "--output", str(directory.relative_to(config.root))], config.root).splitlines()
    binaries: list[tuple[Path, Path]] = []
    for package in packages:
        path = config.root / package
        binary = path / "benchmark.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), package], cwd=config.root, check=True)
        binaries.append((path, binary))
    records = []
    pattern = re.compile(r"BenchmarkExecution/(Interpreter|Native)\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op")
    for path, binary in binaries:
        pairs, raw = [], []
        for index in range(config.samples + WARMUP):
            env = dict(os.environ, GOMAXPROCS="1", LO_BENCH_FIRST=order(index)[0])
            output = subprocess.check_output([str(binary), "-test.run=^$", "-test.bench=BenchmarkExecution", "-test.benchmem", "-test.benchtime=200ms"], cwd=config.root, env=env, text=True)
            results = {name.lower(): {"ns": float(ns), "bytes": int(size), "allocations": int(allocations)} for name, ns, size, allocations in pattern.findall(output)}
            if set(results) != set(BACKENDS):
                raise RuntimeError(f"incomplete benchmark {path.name}: {output}")
            if index >= WARMUP:
                pairs.append({name: results[name]["ns"] for name in BACKENDS})
                raw.append({"order": order(index), "results": results})
        records.append({"name": path.name, "source_sha256": hashlib.sha256((path/"source.lo").read_bytes()).hexdigest(), "samples": raw, **comparison(pairs)})
        print(f"execution {path.name}: {records[-1]['native_over_interpreter']:.3f}", file=sys.stderr, flush=True)
    return records


def build(config: Config, source: Path, output: Path) -> None:
    measured([str(config.binary), "build", str(source), "--output", str(output)], config.work)


def process(config: Config) -> list[dict]:
    corpus = json.loads(capture(["go", "run", "./tools/nativecorpus"], config.root))
    corpus.append({"name": "file", "source": 'flow F() -> Int! { text=UTF8.String.new(File.read(File.Path.new("input.txt"))); effect=File.write(File.Path.new("output.txt"),Bytes.new(text)); commit effect; return String.length(text) }; return F()', "expected": "30000"})
    payload = "abc" * 10000
    records = []
    for case in corpus:
        source = config.work / (case["name"] + ".lo")
        source.write_text(case["source"])
        artifact = config.work / case["name"]
        build(config, source, artifact)
        commands = {"interpreter": [str(config.binary), "run", str(source)], "native": [str(artifact), "run"]}
        directories = {}
        for backend in BACKENDS:
            directory = config.work / (case["name"] + "_" + backend)
            directory.mkdir()
            directories[backend] = directory
            commands[backend].extend(["--max-steps", STEPS])
            if case["name"] == "file":
                (directory / "input.txt").write_text(payload)
                commands[backend].extend(["--read-root", str(directory), "--write-root", str(directory)])
        samples = []
        for index in range(config.samples + WARMUP):
            pair = {}
            for backend in order(index):
                elapsed, result = measured(commands[backend], directories[backend])
                if result != case["expected"]:
                    raise RuntimeError(f"{case['name']} {backend}: {result}")
                if case["name"] == "file" and (directories[backend] / "output.txt").read_text() != payload:
                    raise RuntimeError("file result differs")
                pair[backend] = elapsed
            if index >= WARMUP:
                samples.append(pair)
        records.append({"name": case["name"], "source_sha256": hashlib.sha256(case["source"].encode()).hexdigest(), "samples_ns": samples, **comparison(samples)})
    return records


def http_bench(config: Config) -> list[dict]:
    sources = {"echo": (config.root / "examples/http_echo.lo").read_text(), "computed": 'flow H(r: HTTP.Request) -> HTTP.Response { n=String.length(r.path); return HTTP.Response.Text.new(200,Int.to_string(n)) }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(400,e.kind) }; server=HTTP.Server.new(0,H,E); commit server'}
    records = []
    for name, text in sources.items():
        source = config.work / ("http_"+name+".lo")
        source.write_text(text)
        artifact = config.work / ("http_"+name)
        build(config, source, artifact)
        commands = {"interpreter": [str(config.binary), "serve", str(source)], "native": [str(artifact), "serve"]}
        servers, clients = {}, {}
        samples = []
        try:
            for backend in BACKENDS:
                servers[backend] = subprocess.Popen(commands[backend] + ["--listen", "0", "--format", "json"], cwd=config.work, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                readiness = json.loads(servers[backend].stdout.readline())
                clients[backend] = http.client.HTTPConnection(readiness["servers"][0]["address"], timeout=5)
            for index in range(config.samples + WARMUP):
                pair = {}
                for backend in order(index):
                    times = []
                    start = time.perf_counter_ns()
                    for _ in range(200):
                        before = time.perf_counter_ns()
                        clients[backend].request("POST", "/benchmark", body=b"fixed varied payload 123")
                        response = clients[backend].getresponse()
                        body = response.read()
                        if response.status != 200 or body != (b"fixed varied payload 123" if name == "echo" else b"10"):
                            raise RuntimeError(f"HTTP response {response.status}: {body!r}")
                        times.append(time.perf_counter_ns() - before)
                    pair[backend] = {"samples_ns": times, "elapsed_ns": time.perf_counter_ns()-start, "p95_ns": sorted(times)[189]}
                if index >= WARMUP:
                    samples.append(pair)
        finally:
            for client in clients.values():
                client.close()
            for server in servers.values():
                server.send_signal(signal.SIGTERM)
                output, errors = server.communicate(timeout=10)
                if server.returncode or errors:
                    raise RuntimeError(f"HTTP shutdown: {server.returncode} {output} {errors}")
        records.append({"name": name, "source_sha256": hashlib.sha256(text.encode()).hexdigest(), "samples": samples,
                        "p95": comparison([{key: sample[key]["p95_ns"] for key in BACKENDS} for sample in samples]),
                        "reciprocal_throughput": comparison([{key: sample[key]["elapsed_ns"] for key in BACKENDS} for sample in samples]), "concurrency": 1, "requests_per_sample": 200})
    return records


def tree_memory(pid: int) -> int:
    rows = capture(["ps", "-axo", "pid=,ppid=,rss="], Path.cwd()).splitlines()
    processes = [tuple(map(int, row.split())) for row in rows]
    children = {pid}
    while True:
        expanded = children | {child for child, parent, _ in processes if parent in children}
        if expanded == children:
            break
        children = expanded
    return sum(rss * 1024 for child, _, rss in processes if child in children)


def builds(config: Config) -> list[dict]:
    corpus = json.loads(capture(["go", "run", "./tools/nativecorpus"], config.root))
    for name, path in [("training_mvp", "examples/training_cpu.lo"), ("training_dense", "tests/benchmark/testdata/training_dense.lo")]:
        corpus.append({"name": name, "source": (config.root / path).read_text()})
    tool = config.work / "tool"
    tool.mkdir()
    subprocess.run(["go", "build", "-o", str(tool/"go"), "./tools/nativecache"], cwd=config.root, check=True)
    actual_go = shutil.which("go")
    if actual_go is None:
        raise RuntimeError("Go toolchain is absent")
    env = dict(os.environ, PATH=str(tool)+os.pathsep+os.environ["PATH"], LO_BENCH_GO=actual_go)
    records = []
    for case in corpus:
        env["LO_BENCH_CACHE"] = str(config.work / "caches" / case["name"])
        source = config.work / ("build_"+case["name"]+".lo")
        source.write_text(case["source"])
        artifact = config.work / ("build_"+case["name"])
        for sample in range(config.samples + WARMUP + 1):
            start, peak = time.perf_counter_ns(), 0
            with (config.work / "build-output.txt").open("w+") as output:
                child = subprocess.Popen([str(config.binary), "build", str(source), "--output", str(artifact)], cwd=config.work, env=env, stdout=output, stderr=subprocess.STDOUT)
                while child.poll() is None:
                    peak = max(peak, tree_memory(child.pid))
                    time.sleep(.01)
                elapsed = time.perf_counter_ns()-start
                if child.returncode:
                    output.seek(0)
                    raise RuntimeError(output.read())
            if sample == 0 or sample > WARMUP:
                records.append({"name": case["name"], "source_sha256": hashlib.sha256(case["source"].encode()).hexdigest(), "cache": "empty" if sample == 0 else "populated", "sample": max(0, sample-WARMUP-1), "elapsed_ns": elapsed, "peak_process_tree_rss_bytes": peak, "artifact_bytes": len(artifact.read_bytes())})
            print(f"build {case['name']} {sample}: {elapsed/1e9:.2f}s {peak/(1<<20):.0f} MiB", file=sys.stderr, flush=True)
    return records


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--workdir", type=Path, required=True)
    parser.add_argument("--samples", type=int, default=10)
    parser.add_argument("--lane", choices=["execution", "process", "build", "all"], default="all")
    args = parser.parse_args()
    if args.samples < 10:
        parser.error("at least ten samples are required")
    root, work = Path(__file__).resolve().parents[1], args.workdir.resolve()
    if not work.is_relative_to(root):
        parser.error("--workdir must be inside the repository for generated benchmark packages")
    work.mkdir(parents=True, exist_ok=False)
    config = Config(root, work, args.binary.resolve(), args.samples)
    report = {"system": platform.platform(), "machine": platform.machine(), "processor": processor_identity(root), "go": capture(["go", "version"], root), "binary_sha256": hashlib.sha256(config.binary.read_bytes()).hexdigest(), "samples": args.samples, "warmup": WARMUP, "max_steps": int(STEPS), "concurrency": 1, "cache": "warm except explicit empty build cache", "order": "alternating paired samples"}
    if args.lane in ("execution", "all"):
        report["execution"] = execution(config)
        callback = [record for record in report["execution"] if record["name"].startswith(("map_", "filter_", "fold_"))]
        ratios = [record["native_over_interpreter"] for record in callback]
        report["callback_speedup"] = 1 / math.exp(statistics.mean(map(math.log, ratios)))
        rng = random.Random(1001)
        estimates = []
        for _ in range(2000):
            selected = []
            for record in callback:
                values = [sample["results"]["interpreter"]["ns"]/sample["results"]["native"]["ns"] for sample in record["samples"]]
                selected.append(statistics.median(rng.choices(values, k=len(values))))
            estimates.append(math.exp(statistics.mean(map(math.log, selected))))
        estimates.sort()
        report["callback_bootstrap_95_percent"] = [estimates[49], estimates[1949]]
        report["callback_gate"] = "pass" if estimates[49] >= 2 else "fail" if estimates[1949] < 2 else "inconclusive"
    if args.lane in ("process", "all"):
        report["process"] = process(config)
        report["http"] = http_bench(config)
    if args.lane in ("build", "all"):
        report["build"] = builds(config)
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
