"""Read-only source and measured-schedule gates for the final CI command."""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name("preflight.py")


def invoke(*arguments: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, "-B", str(SCRIPT), *arguments], cwd=ROOT,
                          env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"),
                          stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False)


def sources(tmp_path: Path) -> Path:
    root = tmp_path / "source"
    root.mkdir()
    (root / "go.mod").write_text("module fixture\n")
    (root / "internal").mkdir()
    (root / "internal/value.go").write_text("package fixture\n")
    (root / "runtime/vendor/input").mkdir(parents=True)
    (root / "runtime/vendor/input/source.rs").write_text("fn main() {}\n")
    return root


def fingerprint(root: Path) -> str:
    result = invoke("fingerprint", "--root", str(root))
    assert result.returncode == 0, result.stderr
    return json.loads(result.stdout)["source_sha256"]


def inputs(tmp_path: Path) -> tuple[Path, Path, Path]:
    root = sources(tmp_path)
    manifest = tmp_path / "manifest.json"
    manifest.write_text(json.dumps({
        "prepare": [{"id": "prepare", "argv": ["must-not-execute"], "release": ["{work}/owned-intermediate"]}],
        "groups": [{"id": "guest-a", "resource": "guest", "argv": ["must-not-execute"]},
                   {"id": "guest-b", "resource": "guest", "argv": ["must-not-execute"]},
                   {"id": "host-a", "resource": "host", "argv": ["must-not-execute"]},
                   {"id": "host-b", "argv": ["must-not-execute"]}],
        "finalize": [{"id": "finalize", "argv": ["must-not-execute"]}],
    }))
    report = tmp_path / "report.json"
    report.write_text(json.dumps({"status": "pass", "source_sha256": fingerprint(root),
                                  "workers": 3, "build_slots": 2, "guest_workers": 1, "max_bytes": 7 << 30,
                                  "group_seconds": {"prepare": 2, "guest-a": 10, "guest-b": 10,
                                                    "host-a": 15, "host-b": 5, "finalize": 3}}))
    return root, manifest, report


def estimate(root: Path, manifest: Path, *reports: Path, limit: str = "31") -> subprocess.CompletedProcess[str]:
    arguments = ["estimate", "--root", str(root), "--manifest", str(manifest),
                 "--workers", "3", "--build-slots", "2", "--guest-workers", "1",
                 "--limit", limit, "--margin", "5"]
    for report in reports:
        arguments.extend(["--report", str(report)])
    return invoke(*arguments)


def test_source_fingerprint_includes_vendor_and_excludes_task_state(tmp_path: Path) -> None:
    root = sources(tmp_path)
    first = fingerprint(root)
    (root / "_scratch").mkdir()
    (root / "_scratch/state.json").write_text("changed output")
    (root / "bin").mkdir()
    (root / "bin/lo").write_bytes(b"generated compiler")
    assert fingerprint(root) == first
    (root / "docs/examples").mkdir(parents=True)
    (root / "docs/examples/program.lo").write_text("return 1\n")
    (root / "docs/evidence").mkdir()
    (root / "docs/evidence/training-reference-results.json").write_text("{}")
    corpus = fingerprint(root)
    assert corpus != first
    (root / "docs/evidence/archive").mkdir()
    (root / "docs/evidence/archive/payload.json").write_text("ignored historical output")
    assert fingerprint(root) == corpus
    (root / "runtime/vendor/input/source.rs").write_text("fn main() { changed(); }\n")
    assert fingerprint(root) != corpus


def test_estimate_schedules_bounded_guests_and_counts_all_phases(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    descriptor = json.loads(manifest.read_text())
    descriptor["source_sha256"] = fingerprint(root)
    manifest.write_text(json.dumps(descriptor))
    result = estimate(root, manifest, report)
    assert result.returncode == 0, result.stdout + result.stderr
    record = json.loads(result.stdout)
    assert record["expected_seconds"] == 30
    assert record["measured_groups"] == 6
    assert record["unmeasured"] == []
    assert estimate(root, manifest, report, limit="30").returncode == 1


def test_estimate_default_resources_match_runner_cpu_bounds(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    cpus = os.cpu_count() or 1
    data = json.loads(report.read_text())
    data.update(workers=min(16, cpus), build_slots=min(8, cpus), guest_workers=min(4, max(1, cpus // 4)))
    report.write_text(json.dumps(data))
    result = invoke("estimate", "--root", str(root), "--manifest", str(manifest),
                    "--report", str(report), "--limit", "100", "--margin", "5")
    assert result.returncode == 0, result.stdout + result.stderr


@pytest.mark.parametrize("problem", ["failed", "stale", "workers", "build_slots", "guest_workers", "missing", "bool_duration", "negative_duration", "missing_fingerprint", "huge_duration", "bool_resource"])
def test_estimate_rejects_unusable_measurements(tmp_path: Path, problem: str) -> None:
    root, manifest, report = inputs(tmp_path)
    data = json.loads(report.read_text())
    if problem == "failed":
        data["status"] = "fail"
    elif problem == "stale":
        (root / "internal/value.go").write_text("package changed\n")
    elif problem in {"workers", "build_slots", "guest_workers"}:
        data[problem] += 1
    elif problem == "missing":
        del data["group_seconds"]["finalize"]
    elif problem == "bool_duration":
        data["group_seconds"]["prepare"] = True
    elif problem == "negative_duration":
        data["group_seconds"]["prepare"] = -1
    elif problem == "huge_duration":
        data["group_seconds"]["prepare"] = 10 ** 1000
    elif problem == "bool_resource":
        data["workers"] = True
    else:
        del data["source_sha256"]
    report.write_text(json.dumps(data))
    result = estimate(root, manifest, report)
    assert result.returncode == 1, result.stdout
    record = json.loads(result.stdout)
    assert record["status"] == "fail"
    assert record["error"]
    if problem == "missing":
        assert record["unmeasured"] == ["finalize"]


def test_estimate_uses_worst_duration_and_preserves_complete_coverage(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    data = json.loads(report.read_text())
    data["group_seconds"] = {"guest-a": 30}
    second = tmp_path / "second.json"
    second.write_text(json.dumps(data))
    result = estimate(root, manifest, report, second, limit="51")
    assert result.returncode == 0, result.stdout
    assert json.loads(result.stdout)["expected_seconds"] == 50


def test_estimate_rejects_duplicate_manifest_ids(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    data = json.loads(manifest.read_text())
    data["finalize"][0]["id"] = "prepare"
    manifest.write_text(json.dumps(data))
    assert estimate(root, manifest, report).returncode == 1


def test_estimate_rejects_a_stale_described_manifest(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    descriptor = json.loads(manifest.read_text())
    descriptor["source_sha256"] = "0" * 64
    manifest.write_text(json.dumps(descriptor))
    result = estimate(root, manifest, report)
    assert result.returncode == 1
    assert "manifest source is stale" in json.loads(result.stdout)["error"]


@pytest.mark.parametrize("field,value", [("resource", []), ("resource", "unknown"), ("argv", "command"), ("expected", True), ("environment", {"NAME": False}),
                                       ("release", "path"), ("release", [True]), ("release", [""]), ("release", ["bad\0path"])])
def test_estimate_rejects_malformed_manifest_fields(tmp_path: Path, field: str, value: object) -> None:
    root, manifest, report = inputs(tmp_path)
    data = json.loads(manifest.read_text())
    data["groups"][0][field] = value
    manifest.write_text(json.dumps(data))
    result = estimate(root, manifest, report)
    assert result.returncode == 1
    assert json.loads(result.stdout)["error"]


def test_source_fingerprint_rejects_a_source_link_outside_its_root(tmp_path: Path) -> None:
    root = sources(tmp_path)
    outside = tmp_path / "outside.go"
    outside.write_text("package outside\n")
    (root / "internal/escape.go").symlink_to(outside)
    result = invoke("fingerprint", "--root", str(root))
    assert result.returncode == 1
    assert "leaves its fingerprint root" in json.loads(result.stdout)["error"]


def test_estimate_rejects_duplicate_json_fields(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    raw = report.read_text()
    report.write_text(raw[:-1] + ',"status":"pass"}')
    result = estimate(root, manifest, report)
    assert result.returncode == 1
    assert "duplicate field" in json.loads(result.stdout)["error"]


def test_estimate_waits_for_storage_release_dependencies_before_scheduling(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    descriptor = json.loads(manifest.read_text())
    descriptor["groups"] = [
        {"id": "python", "argv": ["must-not-execute"], "after": ["acceptance"]},
        {"id": "linux", "resource": "guest", "argv": ["must-not-execute"], "after": ["acceptance"]},
        {"id": "acceptance", "resource": "guest", "argv": ["must-not-execute"]},
        {"id": "independent", "argv": ["must-not-execute"]},
    ]
    manifest.write_text(json.dumps(descriptor))
    measurement = json.loads(report.read_text())
    measurement["group_seconds"] = {"prepare": 2, "finalize": 3, "python": 103, "linux": 50,
                                    "acceptance": 41, "independent": 80}
    report.write_text(json.dumps(measurement))
    result = estimate(root, manifest, report, limit="155")
    assert result.returncode == 0, result.stdout + result.stderr
    record = json.loads(result.stdout)
    assert record["groups_seconds"] == 144
    assert record["expected_seconds"] == 154
    assert record["measured_groups"] == 6
    assert estimate(root, manifest, report, limit="154").returncode == 1


@pytest.mark.parametrize("problem", ["unknown", "self", "cycle", "prepare", "finalize", "duplicate",
                                     "scalar", "nonstring", "empty", "nul"])
def test_estimate_rejects_invalid_group_dependencies(tmp_path: Path, problem: str) -> None:
    root, manifest, report = inputs(tmp_path)
    descriptor = json.loads(manifest.read_text())
    first, second = descriptor["groups"][:2]
    if problem in {"prepare", "finalize"}:
        descriptor[problem][0]["after"] = [first["id"]]
    elif problem == "unknown": first["after"] = ["absent"]
    elif problem == "self": first["after"] = [first["id"]]
    elif problem == "cycle":
        first["after"] = [second["id"]]
        second["after"] = [first["id"]]
    elif problem == "duplicate": first["after"] = [second["id"], second["id"]]
    elif problem == "scalar": first["after"] = second["id"]
    elif problem == "nonstring": first["after"] = [True]
    elif problem == "empty": first["after"] = [""]
    else: first["after"] = ["bad\0id"]
    manifest.write_text(json.dumps(descriptor))
    result = estimate(root, manifest, report)
    assert result.returncode == 1
    assert json.loads(result.stdout)["error"]


@pytest.mark.parametrize("budget", [None, True, 0, -1, (10 << 30) + 1, 10 ** 1000])
def test_estimate_rejects_missing_invalid_or_looser_storage_budget(tmp_path: Path, budget: object) -> None:
    root, manifest, report = inputs(tmp_path)
    data = json.loads(report.read_text())
    if budget is None:
        data.pop("max_bytes", None)
    else:
        data["max_bytes"] = budget
    report.write_text(json.dumps(data))
    result = estimate(root, manifest, report)
    assert result.returncode == 1
    assert json.loads(result.stdout)["error"]


@pytest.mark.parametrize("budget", [7 << 30, 10 << 30])
def test_estimate_accepts_equal_or_stricter_storage_measurement(tmp_path: Path, budget: int) -> None:
    root, manifest, report = inputs(tmp_path)
    data = json.loads(report.read_text())
    data["max_bytes"] = budget
    report.write_text(json.dumps(data))
    result = estimate(root, manifest, report)
    assert result.returncode == 0, result.stdout


def test_estimate_serializes_fixed_host_http_without_occupying_blocked_workers(tmp_path: Path) -> None:
    root, manifest, report = inputs(tmp_path)
    descriptor = json.loads(manifest.read_text())
    descriptor["groups"] = [{"id": "http-a", "resource": "host-http", "argv": ["must-not-execute"]},
                             {"id": "http-b", "resource": "host-http", "argv": ["must-not-execute"]},
                             {"id": "host", "argv": ["must-not-execute"]}]
    manifest.write_text(json.dumps(descriptor))
    data = json.loads(report.read_text())
    data["group_seconds"] = {"prepare": 2, "finalize": 3, "http-a": 10, "http-b": 10, "host": 25}
    data["workers"] = 2
    report.write_text(json.dumps(data))
    result = invoke("estimate", "--root", str(root), "--manifest", str(manifest), "--report", str(report),
                    "--workers", "2", "--build-slots", "2", "--guest-workers", "1", "--limit", "36", "--margin", "5")
    assert result.returncode == 0, result.stdout
    assert json.loads(result.stdout)["groups_seconds"] == 25
    assert json.loads(result.stdout)["expected_seconds"] == 35
