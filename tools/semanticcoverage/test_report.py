import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from typing import Any
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/semanticcoverage/report.py"


class ReportCLI(unittest.TestCase):
    def setUp(self):
        base = ROOT / "_scratch/build-state/tmp"
        base.mkdir(parents=True, exist_ok=True)
        self.workspace = tempfile.TemporaryDirectory(prefix="semantic-report-", dir=base)
        self.addCleanup(self.workspace.cleanup)
        self.directory = Path(self.workspace.name)
        self.script = self.directory / "tools/semanticcoverage/report.py"
        self.script.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT, self.script)
        for name in ("cmd/lo/main.go", "runtime/native/src/lib.rs", "runtime/native/Cargo.toml", "runtime/native/Cargo.lock",
                     "go.mod", "go.sum", "Makefile", "docs/evidence/training-reference-results.json"):
            item = self.directory / name
            item.parent.mkdir(parents=True, exist_ok=True)
            item.write_text("fixture\n")
        self.matrix = self.directory / "matrix.json"
        self.matrix.write_text(json.dumps({
            "operations": [{"operation": "Math.add", "positive": ["TestExample/add"],
                            "negative": ["TestExample/reject"], "runtime_negative": [],
                            "boundary": "cli_interpreter_and_native"}],
            "cli": [], "semantics": [], "profiles": [], "gaps": [],
            "native_tests": {"TestExample/add": ["TestExample/add"],"TestExample/reject": ["TestExample/reject"]}
        }))
        self.compiler = self.directory / "lo"
        self.compiler.write_bytes(b"installed compiler fixture")
        self.runtime = self.directory / "runtime.a"
        self.runtime.write_bytes(b"native runtime fixture")
        self.manifest = self.directory / "toolchain.json"
        self.manifest.write_text(json.dumps({"host": "darwin/arm64", "targets": {
            target: {"runtime": {"path": "runtime.a", "sha256": hashlib.sha256(self.runtime.read_bytes()).hexdigest()}}
            for target in ("darwin/arm64", "linux/arm64")}}))
        self.logs = [self.directory / "interpreter.jsonl", self.directory / "llvm-darwin.jsonl", self.directory / "llvm-linux.jsonl"]
        for path in self.logs:
            self.events(path, {"TestExample/add": "pass", "TestExample/reject": "pass"})

    def events(self, path: Path, tests: dict[str, str], gates: dict[str, str] | None = None,
               root: Path | None = None) -> None:
        root = root or self.directory
        package = "github.com/MarcoPoloResearchLab/Lo/tests/integration"
        sys.path.insert(0, str(ROOT / "tools/semanticcoverage"))
        import report as reporter
        original_root = reporter.ROOT
        reporter.ROOT = root
        try:
            _, source_hash = reporter.source_identity()
        finally:
            reporter.ROOT = original_root
        backend = "interpreter" if path.name.startswith("interpreter") else "llvm"
        target = "linux/arm64" if path.name.endswith("linux.jsonl") else "darwin/arm64"
        identity = {"backend": backend, "implementation": "go-interpreter" if backend == "interpreter" else "llvm-native",
                    "target": target, "compiler": str(self.compiler.relative_to(root)),
                    "compiler_sha256": hashlib.sha256(self.compiler.read_bytes()).hexdigest(),
                    "source_sha256": source_hash,
                    "matrix_sha256": hashlib.sha256(self.matrix.read_bytes()).hexdigest()}
        if backend == "llvm":
            identity.update(toolchain=str(self.manifest.relative_to(root)),
                            toolchain_sha256=hashlib.sha256(self.manifest.read_bytes()).hexdigest(),
                            runtime_sha256=hashlib.sha256(self.runtime.read_bytes()).hexdigest())
        events = [{"Action": "start", "Package": package},
                  {"Action": "output", "Package": package, "Test": "TestSemanticCoverageRunIdentity",
                   "Output": "LO_SEMANTIC_IDENTITY=" + json.dumps(identity) + "\n"}]
        gate_statuses = {"TestSemanticCoverageRunIdentity": "pass", "TestSemanticCoverageInventory": "pass"}
        if gates is not None:
            gate_statuses.update(gates)
        events += [{"Action": action, "Package": package, "Test": test}
                   for test, action in {**tests, **gate_statuses}.items() if action != "missing"]
        events.append({"Action": "pass", "Package": package})
        path.write_text("".join(json.dumps(event) + "\n" for event in events))

    def run_report(self, output: Path | None = None, script: Path | None = None) -> subprocess.CompletedProcess[str]:
        target = output or self.directory / "report.json"
        command = [sys.executable, "-B", str(script or self.script), "--matrix", str(self.matrix),
                   "--interpreter", str(self.logs[0]), "--llvm", "darwin/arm64=" + str(self.logs[1]),
                   "--llvm", "linux/arm64=" + str(self.logs[2]),
                   "--output", str(target)]
        return subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                              text=True, capture_output=True,
                              env={**os.environ, "CI": "1", "PYTHONDONTWRITEBYTECODE": "1"})

    def test_both_lanes_require_every_reference(self):
        result = self.run_report()
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads((self.directory / "report.json").read_text())
        self.assertEqual(report["execution_status"], "passed")
        self.assertEqual(len(report["references"]), 2)
        self.assertEqual(report["references"][0]["llvm:darwin/arm64"], "pass")

    def test_missing_or_skipped_subtest_is_not_coverage(self):
        for status in ["missing", "skip"]:
            self.events(self.logs[1], {"TestExample/add": "pass", "TestExample/reject": status})
            result = self.run_report()
            self.assertEqual(result.returncode, 1, result.stderr)
            report = json.loads((self.directory / "report.json").read_text())
            self.assertEqual(report["execution_status"], "incomplete")
            self.assertIn("TestExample/reject", result.stderr)

    def test_missing_target_is_not_standalone_qualification(self):
        self.logs[2].unlink()
        result = subprocess.run(
            [sys.executable, "-B", str(self.script), "--matrix", str(self.matrix),
             "--interpreter", str(self.logs[0]), "--llvm", "darwin/arm64=" + str(self.logs[1]),
             "--output", str(self.directory / "report.json")], cwd=ROOT,
            stdin=subprocess.DEVNULL, text=True, capture_output=True)
        self.assertEqual(result.returncode, 2, result.stderr)
        record = json.loads((self.directory / "report.json").read_text())
        self.assertEqual(record["qualification_status"], "incomplete")
        self.assertEqual(record["missing_targets"], ["linux/arm64"])

    def test_installed_runtime_mutation_invalidates_evidence(self):
        self.runtime.write_bytes(b"changed runtime")
        result = self.run_report()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("runtime identity mismatch", result.stderr)

    def test_installed_compiler_mutation_invalidates_evidence(self):
        self.compiler.write_bytes(b"changed compiler")
        result = self.run_report()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("compiler identity mismatch", result.stderr)

    def test_output_outside_repository_is_rejected(self):
        result = self.run_report(ROOT.parent / "forbidden-semantic-report.json")
        self.assertEqual(result.returncode, 1)
        self.assertIn("outside repository", result.stderr)

    def test_truncated_package_run_is_rejected(self):
        self.logs[1].write_text(self.logs[1].read_text().splitlines()[0] + "\n")
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("package", result.stderr)

    def test_interpreter_log_cannot_count_as_native(self):
        output = self.directory / "report.json"
        output.write_text('{"execution_status":"passed"}')
        self.logs[1].write_text(self.logs[0].read_text())
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("identity mismatch", result.stderr)
        self.assertFalse(output.exists())

    def test_changed_matrix_is_rejected(self):
        self.matrix.write_text(self.matrix.read_text() + "\n")
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("identity mismatch", result.stderr)

    def test_cli_source_changes_invalidate_evidence(self) -> None:
        script = self.directory / "tools/semanticcoverage/report.py"
        script.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(SCRIPT, script)
        for name in ("cmd/lo/main.go", "go.mod", "go.sum", "Makefile",
                     "docs/evidence/training-reference-results.json"):
            path = self.directory / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture\n")
        for name in ("compiler", "runtime", "manifest"):
            original = getattr(self, name)
            destination = self.directory / original.name
            if destination != original:
                shutil.copyfile(original, destination)
            setattr(self, name, destination)
        for path in self.logs:
            self.events(path, {"TestExample/add": "pass", "TestExample/reject": "pass"},
                        root=self.directory)
        result = self.run_report(script=script)
        self.assertEqual(result.returncode, 0, result.stderr)
        cli = self.directory / "cmd/lo/main.go"
        report = json.loads((self.directory / "report.json").read_text())
        self.assertEqual(report["source_sha256"]["cmd/lo/main.go"],
                         hashlib.sha256(cli.read_bytes()).hexdigest())
        cli.write_text("changed CLI\n")
        result = self.run_report(script=script)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("identity mismatch", result.stderr)
        self.assertFalse((self.directory / "report.json").exists())

    def test_rust_runtime_source_changes_invalidate_evidence(self):
        runtime = self.directory / "runtime/native/src/lib.rs"
        runtime.write_text("changed runtime source\n")
        result = self.run_report()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("identity mismatch", result.stderr)

    def test_i011_gap_prevents_qualification(self):
        matrix = json.loads(self.matrix.read_text())
        matrix["gaps"] = [{"ID": "native-injected-device-runtime", "Scope": "device", "Reason": "pending",
                           "Owner": "I011", "Acceptance": "downstream"}]
        self.matrix.write_text(json.dumps(matrix))
        for path in self.logs:
            self.events(path, {"TestExample/add": "pass", "TestExample/reject": "pass"})
        result = self.run_report()
        self.assertEqual(result.returncode, 2, result.stderr)
        record = json.loads((self.directory / "report.json").read_text())
        self.assertEqual(record["qualification_status"], "incomplete")
        self.assertEqual(record["qualification_gaps"], matrix["gaps"])

    def test_recorded_gap_blocks_qualification(self):
        matrix = json.loads(self.matrix.read_text())
        matrix["gaps"] = [{"ID": "missing-profile", "Scope": "test", "Reason": "absent",
                           "Owner": "I005", "Acceptance": "required"}]
        self.matrix.write_text(json.dumps(matrix))
        for path in self.logs:
            self.events(path, {"TestExample/add": "pass", "TestExample/reject": "pass"})
        result = self.run_report()
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("missing-profile", result.stderr)
        report = json.loads((self.directory / "report.json").read_text())
        self.assertEqual(report["execution_status"], "passed")
        self.assertEqual(report["coverage_status"], "incomplete")


    def test_disclosed_limits_do_not_require_downstream_implementation(self):
        matrix = json.loads(self.matrix.read_text())
        matrix["gaps"] = [
            {"ID": "offline-distribution", "Scope": "compiler", "Reason": "not_yet_implemented",
             "Owner": "F017", "Acceptance": "downstream"},
            {"ID": "error-kind-exhaustiveness", "Scope": "errors", "Reason": "coverage_limit_recorded",
             "Owner": "I005", "Acceptance": "disclosed_limit"},
        ]
        self.matrix.write_text(json.dumps(matrix))
        for path in self.logs:
            self.events(path, {"TestExample/add": "pass", "TestExample/reject": "pass"})
        result = self.run_report()
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads((self.directory / "report.json").read_text())
        self.assertEqual(report["coverage_status"], "complete_with_limits")
        self.assertEqual(report["gaps"], matrix["gaps"])

    def test_unclassified_gap_is_rejected(self):
        matrix = json.loads(self.matrix.read_text())
        matrix["gaps"] = [{"ID": "unknown", "Scope": "test", "Reason": "absent"}]
        self.matrix.write_text(json.dumps(matrix))
        result = self.run_report()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("gap classification", result.stderr)


    def test_selector_keeps_every_matrix_axis_and_identity_checks(self):
        matrix = json.loads(self.matrix.read_text())
        matrix["operations"][0]["runtime_negative"] = ["TestRuntime/failure"]
        for section in ("cli", "semantics", "profiles"):
            matrix[section] = [{"tests": ["Test" + section.title() + "/case"]}]
        matrix["native_tests"] = {"TestExample/add":["TestExample/add"],"TestExample/reject":["TestExample/reject"]}
        self.matrix.write_text(json.dumps(matrix))
        result = subprocess.run(
            [sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"),
             "select", "--matrix", str(self.matrix)], cwd=ROOT,
            stdin=subprocess.DEVNULL, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(),
                         "^(TestCli|TestExample|TestProfiles|TestRuntime|TestSemanticCoverageInventory|TestSemanticCoverageRunIdentity|TestSemantics)$")

    def test_selector_rejects_invalid_test_identity(self):
        matrix = json.loads(self.matrix.read_text())
        matrix["operations"][0]["positive"] = [".*"]
        matrix["native_tests"] = {"TestExample/add":["TestExample/add"],"TestExample/reject":["TestExample/reject"]}
        self.matrix.write_text(json.dumps(matrix))
        result = subprocess.run(
            [sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"),
             "select", "--matrix", str(self.matrix)], cwd=ROOT,
            stdin=subprocess.DEVNULL, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("test identity", result.stderr)


    def test_inventory_and_identity_gates_must_complete(self):
        for gate in ("TestSemanticCoverageInventory", "TestSemanticCoverageRunIdentity"):
            for status in ("missing", "skip"):
                with self.subTest(gate=gate, status=status):
                    self.events(self.logs[1], {"TestExample/add": "pass", "TestExample/reject": "pass"},
                                gates={gate: status})
                    result = self.run_report()
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn("coverage gate did not pass", result.stderr)
                    self.assertIn(gate, result.stderr)

    def test_runner_cannot_overwrite_its_matrix(self):
        contents = self.matrix.read_text()
        for name in ("semantic-coverage.json", "semantic-interpreter.jsonl", "semantic-llvm-darwin-arm64.jsonl"):
            with self.subTest(name=name):
                matrix = self.directory / name
                matrix.write_text(contents)
                result = subprocess.run(
                    [sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"),
                     "run", "--matrix", str(matrix), "--output-root", str(self.directory)],
                    cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True)
                self.assertTrue(matrix.exists(), "runner deleted its input matrix")
                self.assertEqual(matrix.read_text(), contents, "runner replaced its input matrix")
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("overlaps matrix", result.stderr)

    def alternate_matrix(self) -> dict[str, Any]:
        matrix = json.loads((ROOT / "testdata/semantic-migration/matrix.json").read_text())
        for row in matrix["operations"]:
            for axis in ("positive", "negative", "runtime_negative"):
                row[axis] = ["TestSemanticCoverageInventory"]
        for section in ("cli", "semantics", "profiles"):
            for row in matrix[section]:
                row["tests"] = ["TestSemanticCoverageInventory"]
        matrix["native_tests"] = {"TestSemanticCoverageInventory":["TestSemanticCoverageInventory"]}
        return matrix

    def run_alternate_matrix(self, matrix: dict[str, Any]) -> subprocess.CompletedProcess[str]:
        for name in ("LO_TEST_COMPILER", "LO_LLVM_TOOLCHAIN"):
            if not os.environ.get(name):
                raise ValueError(f"alternate matrix test requires explicit {name}")
        self.matrix.write_text(json.dumps(matrix))
        return subprocess.run(
            [sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"),
             "run", "--matrix", str(self.matrix.relative_to(ROOT)),
             "--output-root", str(self.directory), "--allow-partial",
             "--compiler", os.environ["LO_TEST_COMPILER"],
             "--toolchain", os.environ["LO_LLVM_TOOLCHAIN"]],
            cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True,
            env={**os.environ, "LO_SEMANTIC_MATRIX": "missing-inherited-matrix.json"})

    def test_runner_uses_selected_matrix_in_both_lanes(self) -> None:
        result = self.run_alternate_matrix(self.alternate_matrix())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        report = json.loads((self.directory / "semantic-coverage.json").read_text())
        self.assertEqual(report["matrix_sha256"], hashlib.sha256(self.matrix.read_bytes()).hexdigest())
        self.assertEqual(report["source_sha256"]["cmd/lo/main.go"],
                         hashlib.sha256((ROOT / "cmd/lo/main.go").read_bytes()).hexdigest())
        for lane in ("interpreter", "llvm:darwin/arm64"):
            self.assertEqual(set(report["coverage_gates"][lane].values()), {"pass"})

    def test_runner_validates_selected_matrix_inventory(self) -> None:
        matrix = self.alternate_matrix()
        removed = matrix["operations"].pop()["operation"]
        result = self.run_alternate_matrix(matrix)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        log = (self.directory / "semantic-interpreter.jsonl").read_text()
        self.assertIn("missing operation " + removed, log)
        self.assertFalse((self.directory / "semantic-coverage.json").exists())


if __name__ == "__main__":
    unittest.main()
