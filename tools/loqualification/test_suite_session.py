"""Complete event accounting and shared inputs for Linux semantic sessions."""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess

import pytest

import suite


def events(path: Path, records: list[tuple[str, str | None]]) -> Path:
    path.write_text("".join(json.dumps({"Package": suite.PACKAGE, "Action": action,
                                      **({"Test": name} if name else {})}) + "\n"
                            for action, name in records))
    return path


def test_linux_events_require_all_selected_tests_and_referenced_subtests(tmp_path: Path) -> None:
    source = events(tmp_path / "events.jsonl", [("start", None), ("run", "TestOne"),
                    ("run", "TestOne/first"), ("pass", "TestOne/first"), ("pass", "TestOne"), ("pass", None)])
    result = suite.validate_events(source, ("TestOne",), ("TestOne/first",))
    assert result["executed"] == ["TestOne", "TestOne/first"]


@pytest.mark.parametrize("problem", ["missing_top", "missing_reference", "duplicate", "unfinished", "skip", "package_failed", "unexpected", "late_case"])
def test_linux_events_reject_incomplete_or_duplicate_execution(tmp_path: Path, problem: str) -> None:
    records: list[tuple[str, str | None]] = [("start", None), ("run", "TestOne"),
                                           ("run", "TestOne/first"), ("pass", "TestOne/first"),
                                           ("pass", "TestOne"), ("pass", None)]
    expected = ("TestOne", "TestTwo") if problem == "missing_top" else ("TestOne",)
    required = ("TestOne/missing",) if problem == "missing_reference" else ("TestOne/first",)
    if problem == "duplicate":
        records.insert(-1, ("pass", "TestOne/first"))
    elif problem == "unfinished":
        records.insert(-1, ("run", "TestOne/unfinished"))
    elif problem == "skip":
        records[3] = ("skip", "TestOne/first")
    elif problem == "package_failed":
        records[-1] = ("fail", None)
    elif problem == "unexpected":
        records[-1:-1] = [("run", "TestOther"), ("pass", "TestOther")]
    elif problem == "late_case":
        records.extend([("run", "TestOne/late"), ("pass", "TestOne/late")])
    with pytest.raises(ValueError):
        suite.validate_events(events(tmp_path / "events.jsonl", records), expected, required)


def test_linux_shared_inputs_reject_partial_selection(monkeypatch: pytest.MonkeyPatch) -> None:
    for name in suite.SHARED_INPUTS:
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("LO_TEST_LINUX_INTEGRATION_BINARY", "partial")
    with pytest.raises(ValueError, match="select all three"):
        suite.shared_inputs()


def test_linux_qualification_reuses_inputs_and_validates_guest_case_inventory(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    root = tmp_path / "fixtures"
    root.mkdir()
    monkeypatch.setattr(suite, "ROOT", root)
    required = ("internal/example.go", "testdata/opaque-fixture.bin", "examples/opaque-fixture.dat",
                "docs/examples/opaque-fixture.dat", "runtime/metal/metal.metallib")
    archival = "docs/evidence/archived-large-payload.json"
    for name in (*required, archival):
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("fixture")
    header = bytearray(20)
    header[:6] = b"\x7fELF\x02\x01"
    header[18:20] = b"\xb7\x00"
    for name, filename in suite.SHARED_INPUTS.items():
        path = root / filename
        path.write_bytes(header)
        monkeypatch.setenv(name, str(path))
    package, preparation = root / "package", root / "preparation"
    package.mkdir()
    preparation.mkdir()
    matrix = root / "matrix.json"
    matrix.write_text(json.dumps({"contracts": [{"targets": ["linux/arm64"], "tests": ["TestOne/first"]}]}))
    environment = dict(os.environ, GOTOOLCHAIN="local", GOENV="off", GOWORK="off", GOTELEMETRY="off")
    for name in ("HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH"):
        path = root / name.lower()
        path.mkdir()
        environment[name] = str(path)
    monkeypatch.setattr(suite, "environment", lambda: environment)
    monkeypatch.setattr(suite, "source_identity", lambda: ({required[0]: "identity"}, ""))
    def unnecessary_build(*_args: object) -> None:
        raise AssertionError("shared Linux binaries must not be rebuilt")
    monkeypatch.setattr(suite, "execute", unnecessary_build)
    def control(_preparation: Path, _package: Path, output: Path, _env: dict[str, str], _log: Path) -> None:
        output.write_bytes(header)
    monkeypatch.setattr(suite, "control_fixture", control)
    original_run = subprocess.run
    def command(argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
        if argv[0] == "git":
            raise AssertionError("guest payloads must use declared test inputs")
        return original_run(argv, **kwargs)
    monkeypatch.setattr(suite.subprocess, "run", command)
    def guest(_preparation: Path, _package: Path, manifest: Path, work: Path,
              _guest_package: str, _accelerator: str) -> dict[str, object]:
        record = json.loads(manifest.read_text())
        names = {item["path"] for item in record["files"]}
        assert {"/workspace/" + name for name in required} <= names
        assert "/workspace/matrix.json" in names
        assert "/workspace/" + archival not in names
        inventory, execution = record["cases"]
        assert inventory["id"] == "semantic-linux-inventory"
        assert execution["environment"]["LO_TEST_SESSION_LOCAL"] == "1"
        assert execution["environment"]["LO_TEST_SESSION_DIRECTORY"] == suite.GUEST_STATE
        work.mkdir()
        (work / "0.stdout").write_text("TestOne\n")
        (work / "1.stdout").write_text("=== RUN   TestOne\n=== RUN   TestOne/first\n"
                                     "--- PASS: TestOne/first (0.00s)\n--- PASS: TestOne (0.00s)\nPASS\n")
        return {"cases": [{"status": 0}, {"status": 0}]}
    monkeypatch.setattr(suite, "run", guest)
    result = suite.qualify(preparation, package, matrix, root / "work", "go", "^TestOne$")
    assert result["inventory"]["executed"] == ["TestOne", "TestOne/first"]
    assert result["inventory"]["required"] == ["TestOne/first"]


@pytest.mark.parametrize("failure", ["test", "startup", "inventory"])
def test_linux_qualification_exposes_bounded_failure_before_workspace_cleanup(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, failure: str
) -> None:
    inputs = {}
    for name in suite.SHARED_INPUTS.values():
        path = tmp_path / name
        path.write_bytes(b"controlled precompiled fixture")
        inputs[name] = path
    monkeypatch.setattr(suite, "shared_inputs", lambda: inputs)
    environment = dict(os.environ, GOTOOLCHAIN="local", GOENV="off", GOWORK="off", GOTELEMETRY="off")
    monkeypatch.setattr(suite, "environment", lambda: environment)
    monkeypatch.setattr(suite, "source_payloads", lambda _matrix: [])
    monkeypatch.setattr(suite, "control_fixture", lambda _prep, _pkg, output, _env, _log: output.write_bytes(b"control"))
    matrix = tmp_path / "matrix.json"
    matrix.write_text(json.dumps({"contracts": []}))
    def guest(_preparation: Path, _package: Path, manifest: Path, work: Path,
              _guest_package: str, _accelerator: str) -> dict[str, object]:
        work.mkdir()
        (work / "0.stdout").write_text("TestBroken\n" if failure != "inventory" else "inventory stdout detail\n")
        (work / "0.stderr").write_text("inventory stderr sentinel\n" if failure == "inventory" else "")
        if failure == "test":
            text = ("old noise " * 20000 + "\n=== RUN   TestBroken\n=== RUN   TestBroken/child\n"
                    "    fixture.c:17: first genuine assertion sentinel\n"
                    "--- FAIL: TestBroken/child (0.01s)\n--- FAIL: TestBroken (0.01s)\nFAIL\n")
        else:
            text = ""
        (work / "1.stdout").write_text(text)
        (work / "1.stderr").write_text("startup runtime identity sentinel\n" if failure == "startup" else "native stderr sentinel\n")
        return {"cases": [{"id": "semantic-linux-inventory", "status": 2 if failure == "inventory" else 0},
                          {"id": "semantic-linux-arm64", "status": 1}]}
    monkeypatch.setattr(suite, "run", guest)
    with pytest.raises(ValueError) as caught:
        suite.qualify(tmp_path, tmp_path, matrix, tmp_path / "work", "go", "^TestBroken$")
    diagnostic = str(caught.value)
    if failure == "test":
        assert "TestBroken/child" in diagnostic
        assert "first genuine assertion sentinel" in diagnostic
        assert "native stderr sentinel" in diagnostic
    elif failure == "startup":
        assert "startup runtime identity sentinel" in diagnostic
    else:
        assert "semantic-linux-inventory" in diagnostic
        assert "inventory stdout detail" in diagnostic and "inventory stderr sentinel" in diagnostic
    assert "status=" in diagnostic
    assert len(diagnostic.encode()) < 32768
    argv = json.loads((tmp_path / "work/batch.json").read_text())["cases"][1]["argv"]
    assert "-test.failfast" in argv
