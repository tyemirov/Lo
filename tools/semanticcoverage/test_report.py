import hashlib
import json
import os
import platform
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/semanticcoverage/report.py"
TARGETS = ("darwin/arm64", "linux/arm64")


class ReportCLI(unittest.TestCase):
    def setUp(self):
        base = ROOT / "_scratch/build-state/tmp"
        base.mkdir(parents=True, exist_ok=True)
        self.workspace = tempfile.TemporaryDirectory(prefix="native-report-", dir=base)
        self.addCleanup(self.workspace.cleanup)
        self.directory = Path(self.workspace.name)
        self.script = self.directory / "tools/semanticcoverage/report.py"
        self.script.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT, self.script)
        for name in ("cmd/lo/main.go", "runtime/native/src/lib.rs", "runtime/native/Cargo.toml",
                     "runtime/native/Cargo.lock", "go.mod", "go.sum", "Makefile",
                     "docs/evidence/training-reference-results.json"):
            item = self.directory / name
            item.parent.mkdir(parents=True, exist_ok=True)
            item.write_text("fixture\n")
        self.matrix = self.directory / "matrix.json"
        self.data = {
            "operations": [{"operation": "Math.add", "capability": "", "errors": [],
                            "activation_errors": [], "negative_applicability": "verifier_only_typed_total"}],
            "contracts": [self.contract("add", "program-command", "TestExample/add", "positive"),
                          self.contract("reject", "compiler-command", "TestExample/reject", "negative")],
            "gaps": [],
        }
        self.compiler = self.directory / "lo"
        self.compiler.write_bytes(b"compiler fixture")
        self.runtime = self.directory / "runtime.a"
        self.runtime.write_bytes(b"native runtime fixture")
        self.manifest = self.directory / "toolchain.json"
        self.manifest.write_text(json.dumps({"abi": "lo-native-3", "host": TARGETS[0], "targets": {
            target: {"runtime": {"path": "runtime.a", "sha256": self.digest(self.runtime)}} for target in TARGETS}}))
        self.logs = {target: self.directory / (target.replace("/", "-") + ".jsonl") for target in TARGETS}
        self.write_matrix()

    @staticmethod
    def contract(identity, boundary, test, axis="contract", operation="Math.add"):
        return {"id": identity, "operation": operation, "axis": axis, "boundary": boundary,
                "targets": list(TARGETS), "profile": "frontend" if boundary == "compiler-command" else "software",
                "tests": [test]}

    @staticmethod
    def digest(path):
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def write_matrix(self):
        self.matrix.write_text(json.dumps(self.data))
        for target, path in self.logs.items():
            self.events(path, target)

    def events(self, path, target, statuses=None, gates=None):
        sys.path.insert(0, str(ROOT / "tools/semanticcoverage"))
        import report
        previous = report.ROOT
        report.ROOT = self.directory
        try:
            _, source = report.source_identity()
        finally:
            report.ROOT = previous
        identity = {"implementation": "llvm-native", "target": target, "abi": "lo-native-3", "profile": "software",
                    "compiler": "lo", "compiler_sha256": self.digest(self.compiler), "source_sha256": source,
                    "matrix_sha256": self.digest(self.matrix), "toolchain": "toolchain.json",
                    "toolchain_sha256": self.digest(self.manifest), "runtime_sha256": self.digest(self.runtime)}
        package = report.PACKAGE
        tests = {"TestExample/add": "pass", "TestExample/reject": "pass", **(statuses or {})}
        tests.update({gate: "pass" for gate in report.COVERAGE_GATES})
        tests.update(gates or {})
        events = [{"Action": "start", "Package": package},
                  {"Action": "output", "Package": package, "Test": "TestSemanticCoverageRunIdentity",
                   "Output": report.IDENTITY_MARKER + json.dumps(identity) + "\n"}]
        events.extend({"Action": action, "Package": package, "Test": name} for name, action in tests.items() if action != "missing")
        events.append({"Action": "pass", "Package": package})
        path.write_text("".join(json.dumps(event) + "\n" for event in events))

    def run_report(self, output=None, targets=TARGETS, extra=()):
        command = [sys.executable, "-B", str(self.script), "--matrix", str(self.matrix)]
        for target in targets:
            command += ["--native", target + "=" + str(self.logs[target])]
        command += ["--output", str(output or self.directory / "_scratch" / "report.json"), *extra]
        return subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True)

    def test_native_direct_contracts(self):
        result = self.run_report()
        self.assertEqual(result.returncode, 0, result.stderr)
        record = json.loads((self.directory / "_scratch" / "report.json").read_text())
        self.assertEqual(record["execution_status"], "passed")
        self.assertEqual(len(record["contracts"]), 2)
        self.assertEqual(set(record["execution_identity"]), set(TARGETS))
        self.assertNotIn("native_tests", record)

    def test_missing_and_skipped_references_fail(self):
        for status in ("missing", "skip", "fail"):
            self.events(self.logs[TARGETS[0]], TARGETS[0], {"TestExample/add": status})
            result = self.run_report()
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertIn("TestExample/add", result.stderr)

    def test_many_contracts_can_share_a_reference(self):
        self.data["contracts"].append(self.contract("shared", "program-command", "TestExample/add", operation=""))
        self.write_matrix()
        self.assertEqual(self.run_report().returncode, 0)

    def test_one_contract_can_require_several_references(self):
        self.data["contracts"][0]["tests"].append("TestExample/reject")
        self.write_matrix()
        self.events(self.logs[TARGETS[0]], TARGETS[0], {"TestExample/reject": "skip"})
        self.assertEqual(self.run_report().returncode, 1)

    def test_duplicate_contract_ids_are_rejected(self):
        self.data["contracts"].append(dict(self.data["contracts"][0]))
        self.write_matrix()
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("duplicate contract", result.stderr)

    def test_duplicate_references_within_contract_are_rejected(self):
        self.data["contracts"][0]["tests"] *= 2
        self.write_matrix()
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("duplicate test reference", result.stderr)

    def test_unknown_operation_reference_is_rejected(self):
        self.data["contracts"][0]["operation"] = "Unknown.operation"
        self.write_matrix()
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("unknown operation", result.stderr)

    def test_operation_axes_are_required(self):
        self.data["contracts"].pop()
        self.write_matrix()
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("negative", result.stderr)

    def test_old_matrix_shapes_are_rejected(self):
        for field in ("native_tests", "cli", "semantics", "profiles"):
            self.data[field] = {}
            self.write_matrix()
            result = self.run_report()
            self.assertEqual(result.returncode, 1)
            self.assertIn("matrix fields", result.stderr)
            del self.data[field]

    def test_old_interpreter_cli_and_logs_are_rejected(self):
        result = self.run_report(extra=("--interpreter", str(self.logs[TARGETS[0]])))
        self.assertNotEqual(result.returncode, 0)
        events = [json.loads(line) for line in self.logs[TARGETS[0]].read_text().splitlines()]
        identity = json.loads(events[1]["Output"].split("=", 1)[1])
        identity["backend"] = "interpreter"
        identity["implementation"] = "go-interpreter"
        events[1]["Output"] = "LO_SEMANTIC_IDENTITY=" + json.dumps(identity)
        self.logs[TARGETS[0]].write_text("".join(json.dumps(event) + "\n" for event in events))
        result = self.run_report()
        self.assertEqual(result.returncode, 1)
        self.assertIn("identity", result.stderr)

    def test_invalid_boundary_target_profile_and_test_are_rejected(self):
        for field, value in (("boundary", "evaluator-api"), ("targets", ["unknown/target"]),
                             ("targets", [TARGETS[0], TARGETS[0]]), ("profile", "physical-device"), ("tests", [".*"])):
            original = self.data["contracts"][0][field]
            self.data["contracts"][0][field] = value
            self.write_matrix()
            self.assertEqual(self.run_report().returncode, 1)
            self.data["contracts"][0][field] = original

    def test_target_specific_contract_does_not_require_other_target(self):
        self.data["contracts"][0]["targets"] = [TARGETS[0]]
        self.write_matrix()
        self.events(self.logs[TARGETS[1]], TARGETS[1], {"TestExample/add": "skip"})
        self.assertEqual(self.run_report().returncode, 0)

    def test_missing_target_requires_explicit_partial_report(self):
        result = self.run_report(targets=(TARGETS[0],))
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(self.run_report(targets=(TARGETS[0],), extra=("--allow-partial",)).returncode, 0)

    def test_duplicate_package_and_test_completion_are_rejected(self):
        path = self.logs[TARGETS[0]]
        original = path.read_text()
        for event in ({"Action": "pass", "Package": "github.com/MarcoPoloResearchLab/Lo/tests/integration"},
                      {"Action": "pass", "Package": "github.com/MarcoPoloResearchLab/Lo/tests/integration", "Test": "TestExample/add"}):
            path.write_text(original + json.dumps(event) + "\n")
            result = self.run_report()
            self.assertEqual(result.returncode, 1)
            self.assertIn("duplicate", result.stderr)

    def test_truncated_and_failed_package_runs_are_rejected(self):
        path = self.logs[TARGETS[0]]
        original = path.read_text()
        path.write_text(original.splitlines()[0] + "\n")
        self.assertEqual(self.run_report().returncode, 1)
        path.write_text(original.replace('"Action": "pass", "Package": "github.com/MarcoPoloResearchLab/Lo/tests/integration"}', '"Action": "fail", "Package": "github.com/MarcoPoloResearchLab/Lo/tests/integration"}'))
        self.assertEqual(self.run_report().returncode, 1)

    def test_inventory_and_identity_gates_must_complete(self):
        for gate in ("TestSemanticCoverageInventory", "TestSemanticCoverageRunIdentity"):
            for status in ("missing", "skip"):
                self.events(self.logs[TARGETS[0]], TARGETS[0], gates={gate: status})
                result = self.run_report()
                self.assertEqual(result.returncode, 1)
                self.assertIn("coverage gate", result.stderr)

    def test_changed_compiler_runtime_matrix_and_source_invalidate_evidence(self):
        for path in (self.compiler, self.runtime, self.matrix, self.directory / "cmd/lo/main.go", self.directory / "runtime/native/src/lib.rs"):
            original = path.read_bytes()
            path.write_bytes(original + b" ")
            result = self.run_report()
            self.assertEqual(result.returncode, 1)
            self.assertFalse((self.directory / "_scratch" / "report.json").exists())
            path.write_bytes(original)

    def test_output_cannot_overlap_inputs_or_leave_repository(self):
        for path in (self.matrix, self.logs[TARGETS[0]], ROOT.parent / "forbidden-report.json"):
            result = self.run_report(output=path)
            self.assertEqual(result.returncode, 1)
            self.assertIn("overlap" if path != ROOT.parent / "forbidden-report.json" else "outside repository", result.stderr)

    def test_output_cannot_replace_compiler_toolchain_or_runtime(self):
        for path in (self.compiler, self.manifest, self.runtime, self.directory / "cmd/lo/main.go", self.directory / "runtime/native/src/lib.rs"):
            before = path.read_bytes()
            result = self.run_report(output=path)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertEqual(path.read_bytes(), before)
            self.assertIn("overlap", result.stderr)

    def test_required_and_downstream_gaps_are_classified(self):
        for owner, acceptance, expected in (("I005", "required", 2), ("I011", "downstream", 2),
                                             ("I005", "disclosed_limit", 0), ("F017", "downstream", 0)):
            self.data["gaps"] = [{"ID": "gap", "Scope": "test", "Reason": "recorded", "Owner": owner, "Acceptance": acceptance}]
            self.write_matrix()
            self.assertEqual(self.run_report().returncode, expected)
        self.data["gaps"] = [{"ID": "gap", "Scope": "test"}]
        self.write_matrix()
        self.assertEqual(self.run_report().returncode, 1)

    def test_selector_uses_direct_contract_union_and_gates(self):
        result = subprocess.run([sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"), "select", "--matrix", str(self.matrix)],
                                cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "^(TestExample|TestSemanticCoverageInventory|TestSemanticCoverageRunIdentity)$")

    def test_required_metal_artifact_needs_metal_runtime_identity(self):
        data = json.loads(self.manifest.read_text())
        data["metal"] = {"runtime": {"path": "runtime.a", "sha256": self.digest(self.runtime)}}
        self.manifest.write_text(json.dumps(data))
        contract = self.contract("metal-artifact", "program-command", "TestExample/add", operation="")
        contract.update(targets=[TARGETS[0]], profile="metal-artifact")
        self.data["contracts"].append(contract)
        self.write_matrix()
        result = self.run_report()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("metal-artifact", result.stderr)
        path = self.logs[TARGETS[0]]
        events = [json.loads(line) for line in path.read_text().splitlines()]
        identity = json.loads(events[1]["Output"].split("=", 1)[1])
        identity["metal_runtime_sha256"] = self.digest(self.runtime)
        events[1]["Output"] = "LO_SEMANTIC_IDENTITY=" + json.dumps(identity)
        path.write_text("".join(json.dumps(event) + "\n" for event in events))
        result = self.run_report()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_direct_report_api_preserves_source_and_consumed_inputs(self):
        sys.path.insert(0, str(ROOT / "tools/semanticcoverage"))
        import report
        previous = report.ROOT
        report.ROOT = self.directory
        try:
            for path in (self.compiler, self.manifest, self.runtime, self.directory / "cmd/lo/main.go"):
                before = path.read_bytes()
                with self.assertRaisesRegex(ValueError, "overlap"):
                    report.report(report.Inputs(self.matrix, self.logs, path))
                self.assertEqual(path.read_bytes(), before)
        finally:
            report.ROOT = previous

    def test_generated_report_must_stay_in_repository_scratch(self):
        path = self.directory / "unexpected-report.json"
        result = self.run_report(output=path)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("_scratch", result.stderr)
        self.assertFalse(path.exists())

    def runner_matrix(self):
        manifest = json.loads(self.manifest.read_text())
        manifest["metal"] = {"runtime": {"path": "runtime.a", "sha256": self.digest(self.runtime)}}
        self.manifest.write_text(json.dumps(manifest))
        data = json.loads((ROOT / "testdata/semantic-migration/matrix.json").read_text())
        for row in data["contracts"]:
            row["tests"] = ["TestSemanticCoverageInventory"]
        data["gaps"] = []
        path = self.directory / "selected-matrix.json"
        path.write_text(json.dumps(data))
        compiler = self.directory / "frontend"
        built = subprocess.run(["go", "build", "-o", str(compiler), "./cmd/lo"], cwd=ROOT,
                               stdin=subprocess.DEVNULL, text=True, capture_output=True)
        self.assertEqual(built.returncode, 0, built.stderr)
        return path, compiler

    def invoke_runner(self, matrix, compiler):
        output = self.directory / "selected-run"
        result = subprocess.run([sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"), "run",
                                 "--matrix", str(matrix), "--compiler", str(compiler), "--toolchain", str(self.manifest),
                                 "--output-root", str(output), "--allow-partial"], cwd=ROOT,
                                stdin=subprocess.DEVNULL, text=True, capture_output=True)
        return result, output

    def test_runner_executes_selected_matrix_once_with_native_identity(self):
        matrix, compiler = self.runner_matrix()
        result, output = self.invoke_runner(matrix, compiler)
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        logs = list(output.glob("*.jsonl"))
        self.assertEqual(len(logs), 1)
        self.assertTrue(logs[0].name.startswith("semantic-native-"))
        record = json.loads((output / "semantic-coverage.json").read_text())
        self.assertEqual(len(record["contracts"]), len(json.loads(matrix.read_text())["contracts"]))
        self.assertEqual(len(record["execution_identity"]), 1)
        self.assertEqual(next(iter(record["execution_identity"].values()))["implementation"], "llvm-native")

    def test_runner_rejects_missing_catalog_operation(self):
        matrix, compiler = self.runner_matrix()
        data = json.loads(matrix.read_text())
        removed = data["operations"].pop()["operation"]
        data["contracts"] = [row for row in data["contracts"] if row["operation"] != removed]
        matrix.write_text(json.dumps(data))
        result, output = self.invoke_runner(matrix, compiler)
        self.assertEqual(result.returncode, 1, result.stderr + result.stdout)
        log = next(output.glob("*.jsonl")).read_text()
        self.assertIn("missing operation " + removed, log)
        self.assertFalse((output / "semantic-coverage.json").exists())

    def test_selector_limits_execution_to_applicable_target(self):
        sys.path.insert(0, str(ROOT / "tools/semanticcoverage"))
        import run
        self.data["contracts"][0]["targets"] = [TARGETS[0]]
        self.data["contracts"][0]["tests"] = ["TestDarwinOnly"]
        self.write_matrix()
        self.assertNotIn("TestDarwinOnly", run.select_tests(self.matrix, TARGETS[1]))
        self.assertIn("TestDarwinOnly", run.select_tests(self.matrix, TARGETS[0]))
        with self.assertRaises(ValueError):
            run.select_tests(self.matrix, "other/target")

    def test_runner_preserves_every_consumed_input_before_execution(self):
        host = {"Darwin": "darwin", "Linux": "linux"}[platform.system()] + "/arm64"
        other = next(target for target in TARGETS if target != host)
        names = ("semantic-coverage.json", "semantic-native-" + host.replace("/", "-") + ".jsonl")
        for kind in ("compiler", "compiler-hardlink", "toolchain", "additional-log", "runtime", "identity-compiler", "identity-runtime"):
            for name in names:
                with self.subTest(kind=kind, generated=name):
                    case = self.directory / (kind + "-" + name)
                    case.mkdir()
                    generated = case / name
                    compiler, toolchain = self.compiler, self.manifest
                    log = case / "other.jsonl"
                    self.events(log, other)
                    initial = [json.loads(line) for line in log.read_text().splitlines()]
                    identity = json.loads(initial[1]["Output"].split("=", 1)[1])
                    identity.update(compiler=str(self.compiler), toolchain=str(self.manifest))
                    initial[1]["Output"] = "LO_SEMANTIC_IDENTITY=" + json.dumps(identity)
                    log.write_text("".join(json.dumps(event) + "\n" for event in initial))
                    if kind == "compiler":
                        generated.write_bytes(self.compiler.read_bytes())
                        compiler = generated
                    elif kind == "compiler-hardlink":
                        os.link(self.compiler, generated)
                    elif kind == "toolchain":
                        data = json.loads(self.manifest.read_text())
                        for target in data["targets"].values():
                            target["runtime"]["path"] = str(self.runtime)
                        generated.write_text(json.dumps(data))
                        toolchain = generated
                    elif kind == "additional-log":
                        generated.write_bytes(log.read_bytes())
                        log = generated
                    elif kind == "runtime":
                        generated.write_bytes(self.runtime.read_bytes())
                        data = json.loads(self.manifest.read_text())
                        for target in data["targets"].values():
                            target["runtime"]["path"] = str(generated)
                        toolchain = case / "toolchain.json"
                        toolchain.write_text(json.dumps(data))
                    else:
                        generated.write_bytes(self.compiler.read_bytes() if kind == "identity-compiler" else self.runtime.read_bytes())
                        events = [json.loads(line) for line in log.read_text().splitlines()]
                        identity = json.loads(events[1]["Output"].split("=", 1)[1])
                        if kind == "identity-compiler":
                            identity["compiler"] = str(generated)
                        else:
                            data = json.loads(self.manifest.read_text())
                            for target in data["targets"].values():
                                target["runtime"]["path"] = str(generated)
                            manifest = case / "identity-toolchain.json"
                            manifest.write_text(json.dumps(data))
                            identity["toolchain"] = str(manifest)
                        events[1]["Output"] = "LO_SEMANTIC_IDENTITY=" + json.dumps(identity)
                        log.write_text("".join(json.dumps(event) + "\n" for event in events))
                    consumed = {self.matrix, compiler, toolchain, log, generated}
                    before = {path: path.read_bytes() for path in consumed}
                    result = subprocess.run([sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"), "run",
                                             "--matrix", str(self.matrix), "--compiler", str(compiler), "--toolchain", str(toolchain),
                                             "--native", other + "=" + str(log), "--output-root", str(case)],
                                            cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn("overlap", result.stderr)
                    for path, contents in before.items():
                        self.assertTrue(path.exists(), str(path))
                        self.assertEqual(path.read_bytes(), contents, str(path))
                    self.assertNotIn("semantic execution:", result.stdout)

    def test_runner_cannot_overwrite_matrix(self):
        for name in ("semantic-coverage.json", "semantic-native-darwin-arm64.jsonl"):
            path = self.directory / name
            path.write_text(self.matrix.read_text())
            before = path.read_bytes()
            result = subprocess.run([sys.executable, "-B", str(ROOT / "tools/semanticcoverage/run.py"), "run", "--matrix", str(path),
                                     "--output-root", str(self.directory), "--compiler", str(self.compiler), "--toolchain", str(self.manifest)], cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True)
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual(result.returncode, 1)
            self.assertIn("overlaps an input", result.stderr)


if __name__ == "__main__":
    unittest.main()
