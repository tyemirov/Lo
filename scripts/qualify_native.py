"""Measure native execution, process latency, HTTP, and build resources."""

from __future__ import annotations

import argparse
from dataclasses import dataclass, replace
import hashlib
import http.client
import ipaddress
from urllib.parse import urlsplit
import json
import os
from pathlib import Path
import platform
import re
import signal
import shutil
import selectors
import subprocess
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools/nativebench"))
from qualification import (
    SAMPLE_PLAN,
    LANES,
    completion,
    expected_inventory,
)  # noqa: E402
from identity import (
    CACHE,
    DiagnosticCompiler,
    capture_provenance,
    merge_identity,
    verify_provenance,
)  # noqa: E402

WARMUP = 3
STEPS = "10000000"
from oracles import verify_output
from identity import ORDER

PRODUCER_SOURCES = ("scripts/qualify_native.py", "tools/nativecorpus/main.go")


@dataclass(frozen=True)
class Config:
    root: Path
    work: Path
    binary: Path
    samples: int
    case: str | None = None
    toolchain: Path | None = None
    provenance: dict | None = None


def verify_inputs(config: Config) -> None:
    if config.provenance is not None:
        verify_provenance(config.root, config.provenance)


def capture(command: list[str], cwd: Path) -> str:
    return subprocess.check_output(
        command, cwd=cwd, text=True, stdin=subprocess.DEVNULL, timeout=180
    ).strip()


def processor_identity(root: Path) -> str:
    if platform.system() == "Darwin":
        return capture(["sysctl", "-n", "machdep.cpu.brand_string"], root)
    fields = dict(
        line.split(":", 1)
        for line in Path("/proc/cpuinfo").read_text().split("\n\n", 1)[0].splitlines()
        if ":" in line
    )
    fields = {key.strip(): value.strip() for key, value in fields.items()}
    return ", ".join(
        key + "=" + fields[key]
        for key in ("CPU implementer", "CPU architecture", "CPU part", "CPU revision")
    )


def measured(command: list[str], cwd: Path) -> tuple[int, str]:
    start = time.perf_counter_ns()
    result = subprocess.run(
        command,
        cwd=cwd,
        text=True,
        capture_output=True,
        stdin=subprocess.DEVNULL,
        timeout=180,
    )
    elapsed = time.perf_counter_ns() - start
    if result.returncode or result.stderr:
        raise RuntimeError(f"{command}: exit {result.returncode}: {result.stderr}")
    return elapsed, result.stdout.strip()


def benchmark_jobs(root: Path) -> list[dict]:
    jobs = []
    for row in capture(["ps", "-axo", "pid=,comm=,args="], root).splitlines():
        fields = row.split(None, 2)
        if len(fields) != 3:
            continue
        pid, executable, arguments = fields
        name = Path(executable).name
        if int(pid) == os.getpid():
            continue
        if (
            name
            in (
                "native-benchmark",
                "native-allocation",
                "native-allocations",
                "performance-runner",
            )
            or (name.endswith(".test") and "-test.bench" in arguments)
            or (
                name.startswith("python")
                and any(
                    token in arguments.split()
                    for token in (
                        "scripts/qualify_native.py",
                        "tools/loqualification/performance.py",
                    )
                )
            )
        ):
            jobs.append({"pid": int(pid), "command": arguments})
    return jobs


def ready(server: subprocess.Popen, timeout: float = 10) -> dict:
    if server.stdout is None:
        raise ValueError("HTTP server stdout is unavailable")
    deadline = time.monotonic() + timeout
    data = bytearray()
    with selectors.DefaultSelector() as selector:
        selector.register(server.stdout, selectors.EVENT_READ)
        while b"\n" not in data:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not selector.select(remaining):
                raise TimeoutError(
                    "HTTP server did not report readiness within ten seconds"
                )
            block = os.read(server.stdout.fileno(), 4096)
            if not block or server.poll() is not None:
                raise RuntimeError("HTTP server stopped before readiness")
            data.extend(block)
            if len(data) > 65536:
                raise ValueError("HTTP readiness exceeds 64 KiB")
    envelope = json.loads(data.split(b"\n", 1)[0])
    if (
        not isinstance(envelope, dict)
        or envelope.get("command") != "serve"
        or envelope.get("status") != "ready"
    ):
        raise ValueError("HTTP readiness requires a serve ready envelope")
    if set(envelope) - {"command", "status", "servers", "result"}:
        raise ValueError("HTTP readiness has unexpected fields")
    servers = envelope.get("servers")
    if (
        not isinstance(servers, list)
        or len(servers) != 1
        or not isinstance(servers[0], dict)
    ):
        raise ValueError("HTTP readiness requires one server")
    address = servers[0].get("address")
    if not isinstance(address, str):
        raise ValueError("HTTP readiness requires a server address")
    try:
        parsed = urlsplit("http://" + address)
        valid = (
            parsed.username is None
            and parsed.password is None
            and not parsed.path
            and not parsed.query
            and not parsed.fragment
            and parsed.hostname is not None
            and ipaddress.ip_address(parsed.hostname).is_loopback
            and parsed.port is not None
            and 0 < parsed.port <= 65535
        )
    except ValueError as error:
        raise ValueError("HTTP readiness requires a valid loopback address") from error
    if not valid:
        raise ValueError("HTTP readiness requires a valid loopback address")
    return envelope


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def native_fixture(config: Config, path: Path, *, allocations: bool = False) -> Path:
    manifest_path = config.toolchain or (config.binary.parent.parent / "toolchain.json")
    manifest = json.loads(manifest_path.read_text())
    host = {"Darwin": "darwin/arm64", "Linux": "linux/arm64"}[platform.system()]
    if manifest["host"] != host or manifest["llvm"] != "21.1.8":
        raise ValueError("benchmark requires a matching installed LLVM distribution")
    target = manifest["targets"][host]

    def resolve(record: dict) -> str:
        candidate = (manifest_path.parent / record["path"]).resolve()
        if not candidate.is_relative_to(manifest_path.parent):
            raise ValueError("benchmark input leaves the installed distribution")
        if hashlib.sha256(candidate.read_bytes()).hexdigest() != record["sha256"]:
            raise ValueError(f"benchmark input identity mismatch: {candidate}")
        return str(candidate)

    def tool(record: dict) -> list[str]:
        binary = resolve(record)
        if host == "linux/arm64":
            libraries = [
                Path(resolve(item)).parent
                for item in manifest["host_libraries"].values()
            ]
            if len(set(libraries)) != 1:
                raise ValueError("benchmark requires one host library directory")
            return [
                "/lib/ld-linux-aarch64.so.1",
                "--inhibit-cache",
                "--library-path",
                str(libraries[0]),
                "--argv0",
                binary,
                binary,
            ]
        return [binary]

    module, obj = path / "program.ll", path / "program.o"
    harness = path / ("allocation.o" if allocations else "harness.o")
    artifact = path / ("native-allocations" if allocations else "native-benchmark")
    if not allocations:
        subprocess.run(
            [
                str(config.binary),
                "build",
                str(path / "source.lo"),
                "--emit",
                "llvm",
                "--output",
                str(module),
            ],
            cwd=config.root,
            stdin=subprocess.DEVNULL,
            check=True,
            capture_output=True,
        )
    flags = [
        "-I" + str(config.root / "runtime/native/include"),
        "-target",
        target["triple"],
        "-O2",
        "-g",
        "-fPIC",
        "-ffp-contract=off",
    ]
    if host == "darwin/arm64":
        flags.append("-mmacosx-version-min=27.0")
    compile_inputs = [
        (path / ("allocation.c" if allocations else "benchmark.c"), harness)
    ]
    if not allocations:
        compile_inputs.insert(0, (module, obj))
    for source, output in compile_inputs:
        subprocess.run(
            tool(manifest["compiler"])
            + flags
            + ["-nostdinc", "-c", str(source), "-o", str(output)],
            cwd=config.root,
            stdin=subprocess.DEVNULL,
            check=True,
            capture_output=True,
        )
    arguments = []
    for arg in target["link_args"]:
        if arg == "{object}":
            arguments.extend([str(obj), str(harness)])
        elif arg == "{runtime}":
            arguments.append(resolve(target["runtime"]))
        elif arg == "{output}":
            arguments.append(str(artifact))
        elif arg.startswith("{input:"):
            arguments.append(resolve(target["inputs"][arg[7:-1]]))
        else:
            arguments.append(arg)
    subprocess.run(
        tool(target["linker"]) + arguments,
        cwd=config.root,
        stdin=subprocess.DEVNULL,
        check=True,
        capture_output=True,
    )
    return artifact


def execution(config: Config) -> list[dict]:
    directory = config.work / "execution"
    packages = capture(
        ["go", "run", "./tools/nativebench", "--output", str(directory)], config.root
    ).splitlines()
    records = []
    for package in packages:
        path = Path(package)
        if config.case is not None and path.name != config.case:
            continue
        oracle = json.loads((path / "oracle.json").read_text())
        native = native_fixture(config, path)
        metrics = native_fixture(config, path, allocations=True)
        allocation_volume = json.loads(capture([str(metrics)], config.root))
        artifact = path / "program"
        build(config, path / "source.lo", artifact)
        _, actual = measured(
            [str(artifact), "run", "--max-steps", STEPS, "--format", "json"],
            config.root,
        )
        verify_output(actual, oracle)
        verify_output(capture([str(native), "--verify"], config.root), oracle)
        verify_inputs(config)
        raw = []
        for index in range(config.samples + WARMUP):
            timing = json.loads(capture([str(native)], config.root))
            if index >= WARMUP:
                raw.append({"sample": index - WARMUP, **timing})
        records.append(
            {
                "name": path.name,
                "source_sha256": digest(path / "source.lo"),
                "oracle_sha256": digest(path / "oracle.json"),
                "verified": True,
                "samples": raw,
                "binaries": {
                    name: digest(file)
                    for name, file in [
                        ("native", native),
                        ("allocation", metrics),
                        ("artifact", artifact),
                    ]
                },
                "generated_sources": {
                    name: digest(path / name)
                    for name in ("oracle.json", "benchmark.c", "allocation.c")
                },
                "native_allocation_volume": allocation_volume,
            }
        )
        (config.work / "execution-partial.json").write_text(
            json.dumps(records, indent=2) + "\n"
        )
    if not records:
        raise ValueError(f"unknown execution case: {config.case}")
    return records


def build_command(config: Config, source: Path, output: Path) -> list[str]:
    command = [str(config.binary), "build", str(source), "--output", str(output)]
    if config.toolchain is not None:
        command.extend(["--toolchain", str(config.toolchain)])
    return command


def build(config: Config, source: Path, output: Path) -> None:
    measured(build_command(config, source, output), config.work)


def process(config: Config) -> list[dict]:
    corpus = json.loads(capture(["go", "run", "./tools/nativecorpus"], config.root))
    corpus.append(
        {
            "name": "file",
            "source": 'flow F() -> Int! { text=UTF8.String.new(File.read(File.Path.new("input.txt"))); effect=File.Write.new(File.Path.new("output.txt"),Bytes.new(text)); commit effect; return String.length(text) }; return F()',
            "expected": "30000",
        }
    )
    payload = "abc" * 10000
    records = []
    for case in corpus:
        if config.case is not None and case["name"] != config.case:
            continue
        source = config.work / (case["name"] + ".lo")
        source.write_text(case["source"])
        artifact = config.work / case["name"]
        build(config, source, artifact)
        directory = config.work / (case["name"] + "_process")
        directory.mkdir()
        command = [str(artifact), "run", "--max-steps", STEPS]
        if case["name"] == "file":
            (directory / "input.txt").write_text(payload)
            command.extend(
                ["--read-root", str(directory), "--write-root", str(directory)]
            )
        verify_inputs(config)
        samples = []
        for index in range(config.samples + WARMUP):
            elapsed, result = measured(command, directory)
            verify_output(result, {"kind": "exact", "expected": case["expected"]})
            if (
                case["name"] == "file"
                and (directory / "output.txt").read_text() != payload
            ):
                raise ValueError("file result differs")
            if index >= WARMUP:
                samples.append({"sample": index - WARMUP, "wall_ns": elapsed})
        records.append(
            {
                "name": case["name"],
                "source_sha256": digest(source),
                "verified": True,
                "samples": samples,
            }
        )
    return records


def http_bench(config: Config) -> list[dict]:
    sources = {
        "echo": (config.root / "examples/http_echo.lo").read_text(),
        "computed": "flow H(r: HTTP.Request) -> HTTP.Response { n=String.length(r.path); return HTTP.Response.Text.new(200,Int.to_string(n)) }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(400,e.kind) }; server=HTTP.Server.new(0,H,E); commit server",
    }
    records = []
    for name, text in sources.items():
        if config.case is not None and config.case != name:
            continue
        source = config.work / ("http_" + name + ".lo")
        source.write_text(text)
        artifact = config.work / ("http_" + name)
        build(config, source, artifact)
        verify_inputs(config)
        command = [str(artifact), "serve", "--listen", "0", "--format", "json"]
        server = subprocess.Popen(
            command,
            cwd=config.work,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            stdin=subprocess.DEVNULL,
        )
        client = None
        samples = []
        try:
            readiness = ready(server)
            client = http.client.HTTPConnection(
                readiness["servers"][0]["address"], timeout=5
            )
            for index in range(config.samples + WARMUP):
                if server.poll() is not None:
                    raise ValueError("HTTP server stopped during measurement")
                times = []
                start = time.perf_counter_ns()
                for _ in range(200):
                    before = time.perf_counter_ns()
                    client.request(
                        "POST", "/benchmark", body=b"fixed varied payload 123"
                    )
                    response = client.getresponse()
                    body = response.read()
                    if response.status != 200 or body != (
                        b"fixed varied payload 123" if name == "echo" else b"10"
                    ):
                        raise ValueError("HTTP response differs")
                    times.append(time.perf_counter_ns() - before)
                elapsed = time.perf_counter_ns() - start
                if server.poll() is not None:
                    raise ValueError("HTTP server stopped during measurement")
                if index >= WARMUP:
                    samples.append(
                        {
                            "sample": index - WARMUP,
                            "samples_ns": times,
                            "elapsed_ns": elapsed,
                            "p95_ns": sorted(times)[189],
                            "requests_per_second": 200e9 / elapsed,
                        }
                    )
        finally:
            if client is not None:
                client.close()
            if server.poll() is None:
                server.send_signal(signal.SIGTERM)
            try:
                output, errors = server.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                server.kill()
                server.communicate(timeout=5)
                raise
            if server.returncode or errors:
                raise ValueError(f"HTTP shutdown: {server.returncode} {errors}")
            stopped = json.loads(output)
            if (
                not isinstance(stopped, dict)
                or set(stopped) != {"command", "status"}
                or stopped.get("command") != "serve"
                or stopped.get("status") != "stopped"
            ):
                raise ValueError("HTTP stopped event missing")
        records.append(
            {
                "name": name,
                "source_sha256": digest(source),
                "verified": True,
                "samples": samples,
                "requests_per_sample": 200,
                "concurrency": 1,
                "ready_verified": True,
                "stopped_verified": True,
            }
        )
    return records


def tree_memory(pid: int) -> int:
    rows = capture(["ps", "-axo", "pid=,ppid=,rss="], Path.cwd()).splitlines()
    processes = [tuple(map(int, row.split())) for row in rows]
    children = {pid}
    while True:
        expanded = children | {
            child for child, parent, _ in processes if parent in children
        }
        if expanded == children:
            break
        children = expanded
    return sum(rss * 1024 for child, _, rss in processes if child in children)


def builds(config: Config) -> list[dict]:
    corpus = json.loads(capture(["go", "run", "./tools/nativecorpus"], config.root))
    for name, path in [
        ("training_mvp", "examples/training_cpu.lo"),
        ("training_dense", "tests/benchmark/testdata/training_dense.lo"),
    ]:
        corpus.append({"name": name, "source": (config.root / path).read_text()})
    env = dict(os.environ, PATH="")
    records = []
    for case in corpus:
        if config.case is not None and case["name"] != config.case:
            continue
        source = config.work / ("build_" + case["name"] + ".lo")
        source.write_text(case["source"])
        artifact = config.work / ("build_" + case["name"])
        verify_inputs(config)
        for sample in range(config.samples + WARMUP + 1):
            start, peak = time.perf_counter_ns(), 0
            with (config.work / "build-output.txt").open("w+") as output:
                child = subprocess.Popen(
                    build_command(config, source, artifact),
                    cwd=config.work,
                    env=env,
                    stdout=output,
                    stderr=subprocess.STDOUT,
                    stdin=subprocess.DEVNULL,
                )
                while child.poll() is None:
                    peak = max(peak, tree_memory(child.pid))
                    time.sleep(0.01)
                elapsed = time.perf_counter_ns() - start
                if child.returncode:
                    output.seek(0)
                    raise RuntimeError(output.read())
            phase = (
                "first-build"
                if sample == 0
                else "warmup" if sample <= WARMUP else "measurement"
            )
            index = (
                0
                if sample == 0
                else sample - 1 if sample <= WARMUP else sample - WARMUP - 1
            )
            records.append(
                {
                    "name": case["name"],
                    "source_sha256": digest(source),
                    "phase": phase,
                    "sample": index,
                    "elapsed_ns": elapsed,
                    "peak_process_tree_rss_bytes": peak,
                    "rss_method": "sampled process-tree RSS at ten millisecond intervals",
                    "artifact_bytes": artifact.stat().st_size,
                    "diagnostic_bytes": (config.work / "build-output.txt")
                    .stat()
                    .st_size,
                }
            )
            (config.work / "build-partial.json").write_text(
                json.dumps(records, indent=2) + "\n"
            )
            print(
                f"build {case['name']} {sample}: {elapsed/1e9:.2f}s {peak/(1<<20):.0f} MiB",
                file=sys.stderr,
                flush=True,
            )
    return records


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--workdir", type=Path, required=True)
    parser.add_argument("--toolchain", type=Path)
    parser.add_argument("--case")
    parser.add_argument(
        "--lane",
        choices=["execution", "process", "http", "build", "all"],
        default="all",
    )
    args = parser.parse_args()
    root, work = Path(__file__).resolve().parents[1], args.workdir.resolve()
    if not work.is_relative_to(root):
        parser.error(
            "--workdir must be inside the repository for generated benchmark packages"
        )
    work.mkdir(parents=True, exist_ok=False)
    config = Config(
        root,
        work,
        args.binary.resolve(),
        SAMPLE_PLAN["execution"],
        args.case,
        args.toolchain.resolve() if args.toolchain else None,
    )
    manifest_path = config.toolchain or config.binary.parent.parent / "toolchain.json"
    diagnostic = None
    go_path = shutil.which("go")
    if go_path is None:
        raise ValueError("benchmark preparation requires the declared Go producer")
    try:
        frozen = capture_provenance(
            root,
            config.binary,
            manifest_path,
            {"Darwin": "darwin/arm64", "Linux": "linux/arm64"}[platform.system()],
            PRODUCER_SOURCES,
            {"python": Path(sys.executable), "go": Path(go_path)},
        )
        config = replace(config, provenance=frozen)
    except DiagnosticCompiler as error:
        diagnostic = str(error)
    report = {
        "system": platform.platform(),
        "machine": platform.machine(),
        "processor": processor_identity(root),
        "producer_go": capture(["go", "version"], root),
        "binary_sha256": hashlib.sha256(config.binary.read_bytes()).hexdigest(),
        "sample_plan": dict(SAMPLE_PLAN),
        "warmup": WARMUP,
        "max_steps": int(STEPS),
        "concurrency": 1,
        "cache": CACHE,
        "order": ORDER,
        "scope": "diagnostic selection" if args.case else "full fixed corpus",
    }
    manifest_path = config.toolchain or config.binary.parent.parent / "toolchain.json"
    report["toolchain_sha256"] = hashlib.sha256(manifest_path.read_bytes()).hexdigest()
    report["toolchain"] = json.loads(manifest_path.read_text())
    lanes = LANES if args.lane == "all" else (args.lane,)
    report["expected_inventory"] = expected_inventory(
        lanes, (args.case,) if args.case else ()
    )
    report["complete_contract"] = args.case is None and args.lane == "all"
    report["lanes"] = list(lanes)
    report["cases"] = [args.case] if args.case else []
    before = {
        "other_benchmark_jobs": benchmark_jobs(root),
        "load_average_1_5_15": list(os.getloadavg()),
    }
    report["benchmark_context"] = {"before": before}
    report["other_benchmark_jobs"] = list(before["other_benchmark_jobs"])
    report["benchmark_job_observation"] = (
        "Informational host process and load snapshots before and after sequential measurement; no load admission."
    )
    report["measurement_source_identity"] = {
        str(path.relative_to(root)): digest(path)
        for path in (
            Path(__file__).resolve(),
            root / "tools/nativebench/qualification.py",
        )
    }
    target = {"Darwin": "darwin/arm64", "Linux": "linux/arm64"}[platform.system()]
    report["target"] = target
    try:
        if diagnostic is not None:
            raise DiagnosticCompiler(diagnostic)
        report["merge_identity"] = merge_identity(
            root=root,
            compiler=config.binary,
            manifest=manifest_path,
            target=target,
            environment={
                "system": report["system"],
                "machine": report["machine"],
                "processor": report["processor"],
                "execution": "native",
            },
            accelerator="none",
            producer_kind=(
                "darwin-native" if target == "darwin/arm64" else "linux-native"
            ),
            producer_tools={
                "python": platform.python_version(),
                "go_version": report["producer_go"],
                **config.provenance["tools"],
            },
            producer_sources=PRODUCER_SOURCES,
        )
        report["measurement_source_identity"] = {
            **report["merge_identity"]["producer"]["source_identity"],
            **report["merge_identity"]["evaluator"],
        }
    except DiagnosticCompiler as error:
        report["complete_contract"] = False
        report["scope"] = "diagnostic compiler"
        report["merge_ineligible"] = str(error)
    try:
        for lane, measure in (
            ("execution", execution),
            ("process", process),
            ("http", http_bench),
            ("build", builds),
        ):
            if lane in report["expected_inventory"]:
                report[lane] = measure(replace(config, samples=SAMPLE_PLAN[lane]))
                verify_inputs(config)
                (work / "partial.json").write_text(json.dumps(report, indent=2) + "\n")
        verify_inputs(config)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        report["error"] = str(error)
        completion(report, 1)
        (work / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        raise
    after = {
        "other_benchmark_jobs": benchmark_jobs(root),
        "load_average_1_5_15": list(os.getloadavg()),
    }
    report["benchmark_context"]["after"] = after
    report["other_benchmark_jobs"].extend(after["other_benchmark_jobs"])
    report["measurement_complete"] = True
    completion(report, 0)
    report["gate"] = "pass" if report["gates_passed"] else "fail"
    (work / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    if report["gate"] != "pass":
        raise SystemExit(1)


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as error:
        print(
            error.stderr.decode() if isinstance(error.stderr, bytes) else error.stderr,
            file=sys.stderr,
        )
        raise
