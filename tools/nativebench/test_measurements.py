"""Check public child-process measurement boundaries."""

from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import qualify_native


def test_measured_child_has_closed_stdin(tmp_path: Path) -> None:
    elapsed, result = qualify_native.measured(
        [sys.executable, "-B", "-c", "import sys;print(len(sys.stdin.read()))"],
        tmp_path,
    )
    assert elapsed > 0
    assert result == "0"


def test_http_partial_readiness_has_deadline(tmp_path: Path) -> None:
    child = subprocess.Popen(
        [
            sys.executable,
            "-B",
            "-c",
            'import sys,time;sys.stdout.write("{");sys.stdout.flush();time.sleep(30)',
        ],
        cwd=tmp_path,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        with pytest.raises(TimeoutError, match="readiness"):
            qualify_native.ready(child, timeout=0.1)
    finally:
        child.kill()
        child.communicate(timeout=5)


def test_execution_producer_emits_direct_native_samples(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    import json
    import qualification

    package = tmp_path / "startup"
    package.mkdir()
    for name in (
        "source.lo",
        "benchmark.c",
        "allocation.c",
        "native-benchmark",
        "native-allocations",
        "program",
    ):
        (package / name).write_text("controlled " + name)
    (package / "oracle.json").write_text(json.dumps({"kind": "exact", "expected": "7"}))

    def capture(argv, root):
        if argv[:2] == ["go", "run"]:
            return str(package)
        if argv[-1] == "--verify":
            return "7"
        if argv[0] == str(package / "native-allocations"):
            return json.dumps(
                {
                    "bytes": 16,
                    "allocations": 1,
                    "method": qualification.ALLOCATION_METHOD,
                }
            )
        return json.dumps(
            {"ns": 200, "total_ns": 200000000, "iterations": 1000000, "batch_size": 64}
        )

    monkeypatch.setattr(qualify_native, "capture", capture)
    monkeypatch.setattr(
        qualify_native,
        "native_fixture",
        lambda config, path, allocations=False: path
        / ("native-allocations" if allocations else "native-benchmark"),
    )
    monkeypatch.setattr(qualify_native, "build", lambda *a: None)
    monkeypatch.setattr(qualify_native, "measured", lambda *a: (1, "7"))
    records = qualify_native.execution(
        qualify_native.Config(ROOT, tmp_path, package / "program", 10, "startup")
    )
    report = {
        "execution": records,
        "sample_plan": dict(qualification.SAMPLE_PLAN),
        "warmup": 3,
        "concurrency": 1,
        "other_benchmark_jobs": [],
        "benchmark_job_observation": "controlled producer",
        "expected_inventory": {"execution": ["startup"]},
        "complete_contract": False,
    }
    qualification.completion(report, 0)
    assert report["measurement_complete"], report.get("measurement_error")
    assert all(
        set(sample) == {"sample", "ns", "total_ns", "iterations", "batch_size"}
        for sample in records[0]["samples"]
    )
    assert "interpreter" not in records[0]["binaries"]


@pytest.mark.parametrize("gate_passed", [False, True])
def test_public_producer_records_load_without_admission(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, gate_passed: bool
) -> None:
    import json

    binary = tmp_path / "package/bin/lo"
    binary.parent.mkdir(parents=True)
    binary.write_text("controlled installed compiler")
    (binary.parent.parent / "toolchain.json").write_text("{}")
    observations = [
        [{"pid": 123, "command": "owned-background-before"}],
        [{"pid": 456, "command": "owned-background-after"}],
    ]
    observed = iter(observations)
    monkeypatch.setattr(qualify_native, "benchmark_jobs", lambda root: next(observed))
    monkeypatch.setattr(qualify_native.os, "getloadavg", lambda: (8.0, 7.0, 6.0))
    monkeypatch.setattr(
        qualify_native, "capture_provenance", lambda *a, **k: {"tools": {}}
    )
    monkeypatch.setattr(qualify_native, "verify_provenance", lambda *a, **k: None)
    monkeypatch.setattr(
        qualify_native,
        "merge_identity",
        lambda **k: {
            "oracle": {},
            "producer": {"source_identity": {}},
            "evaluator": {},
        },
    )
    monkeypatch.setattr(qualify_native, "capture", lambda *a: "go version controlled")
    monkeypatch.setattr(
        qualify_native, "processor_identity", lambda *a: "controlled arm64"
    )
    called = []
    monkeypatch.setattr(
        qualify_native, "process", lambda config: called.append("measured") or []
    )

    def finish(report, status):
        report["measurement_complete"] = True
        report["gates_passed"] = gate_passed

    monkeypatch.setattr(qualify_native, "completion", finish)
    work = tmp_path / "measure"
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "qualify_native",
            "--binary",
            str(binary),
            "--workdir",
            str(work),
            "--lane",
            "process",
            "--case",
            "startup",
        ],
    )
    if gate_passed:
        qualify_native.main()
    else:
        with pytest.raises(SystemExit) as error:
            qualify_native.main()
        assert error.value.code == 1
    report = json.loads((work / "report.json").read_text())
    assert called == ["measured"]
    assert report["measurement_complete"] is True
    assert report["other_benchmark_jobs"] == observations[0] + observations[1]
    assert (
        report["benchmark_context"]["before"]["other_benchmark_jobs"] == observations[0]
    )
    assert (
        report["benchmark_context"]["after"]["other_benchmark_jobs"] == observations[1]
    )
    assert report["benchmark_context"]["before"]["load_average_1_5_15"] == [
        8.0,
        7.0,
        6.0,
    ]


def test_process_producer_runs_only_built_artifact(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    import json

    monkeypatch.setattr(
        qualify_native,
        "capture",
        lambda *a: json.dumps(
            [{"name": "startup", "source": "return 7", "expected": "7"}]
        ),
    )
    monkeypatch.setattr(qualify_native, "build", lambda *a: None)
    observed = []
    monkeypatch.setattr(
        qualify_native,
        "measured",
        lambda argv, root: observed.append(argv) or (10, "7"),
    )
    records = qualify_native.process(
        qualify_native.Config(
            ROOT, tmp_path, Path("/controlled/compiler"), 10, "startup"
        )
    )
    assert len(observed) == 13 and all(
        argv[:2] == [str(tmp_path / "startup"), "run"] for argv in observed
    )
    assert all(set(sample) == {"sample", "wall_ns"} for sample in records[0]["samples"])


@pytest.mark.parametrize(
    "stopped_event,valid",
    [
        ({"command": "serve", "status": "stopped"}, True),
        ({"command": "run", "status": "stopped"}, False),
        ({"status": "stopped"}, False),
        ({"command": "serve", "status": "ready"}, False),
        ({"command": "serve"}, False),
        ([], False),
    ],
)
def test_http_producer_uses_one_service_and_checks_stopped(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, stopped_event: object, valid: bool
) -> None:
    from itertools import count

    clock = count()
    observed = []
    monkeypatch.setattr(qualify_native.time, "perf_counter_ns", lambda: next(clock))

    class Server:
        returncode = None

        def __init__(self, argv, **kwargs):
            observed.append(argv)

        def poll(self):
            return self.returncode

        def send_signal(self, signal):
            pass

        def communicate(self, timeout):
            self.returncode = 0
            return __import__("json").dumps(stopped_event), ""

    class Client:
        def __init__(self, *args, **kwargs):
            pass

        def request(self, *args, **kwargs):
            pass

        def getresponse(self):
            return type("Response", (), {"status": 200, "read": lambda self: b"10"})()

        def close(self):
            pass

    monkeypatch.setattr(qualify_native, "build", lambda *a: None)
    monkeypatch.setattr(qualify_native.subprocess, "Popen", Server)
    monkeypatch.setattr(
        qualify_native, "ready", lambda server: {"servers": [{"address": "owned"}]}
    )
    monkeypatch.setattr(qualify_native.http.client, "HTTPConnection", Client)
    if not valid:
        with pytest.raises(ValueError, match="stopped"):
            qualify_native.http_bench(
                qualify_native.Config(
                    ROOT, tmp_path, Path("/controlled/compiler"), 10, "computed"
                )
            )
        return
    records = qualify_native.http_bench(
        qualify_native.Config(
            ROOT, tmp_path, Path("/controlled/compiler"), 10, "computed"
        )
    )
    assert len(observed) == 1 and observed[0][:2] == [
        str(tmp_path / "http_computed"),
        "serve",
    ]
    assert records[0]["ready_verified"] and records[0]["stopped_verified"]
    for sample in records[0]["samples"]:
        assert sample["samples_ns"] == [1] * 200
        assert sample["elapsed_ns"] == 401
        assert sample["requests_per_second"] == 200e9 / 401


@pytest.mark.parametrize(
    "mutation",
    [
        "wrong-command",
        "missing-command",
        "wrong-status",
        "missing-status",
        "missing-servers",
        "empty-servers",
        "bad-address",
    ],
)
def test_public_readiness_boundary_rejects_invalid_envelope(
    tmp_path: Path, mutation: str
) -> None:
    import json

    envelope = {
        "command": "serve",
        "status": "ready",
        "servers": [{"address": "127.0.0.1:12345"}],
    }
    if mutation == "wrong-command":
        envelope["command"] = "run"
    elif mutation == "missing-command":
        envelope.pop("command")
    elif mutation == "wrong-status":
        envelope["status"] = "stopped"
    elif mutation == "missing-status":
        envelope.pop("status")
    elif mutation == "missing-servers":
        envelope.pop("servers")
    elif mutation == "empty-servers":
        envelope["servers"] = []
    else:
        envelope["servers"] = [{"address": "external.invalid:80"}]
    child = subprocess.Popen(
        [
            sys.executable,
            "-B",
            "-c",
            "import sys,time;print(sys.argv[1],flush=True);time.sleep(30)",
            json.dumps(envelope),
        ],
        cwd=tmp_path,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        with pytest.raises(ValueError, match="readiness"):
            qualify_native.ready(child, timeout=1)
    finally:
        child.kill()
        child.communicate(timeout=5)
