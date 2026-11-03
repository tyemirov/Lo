import argparse
from dataclasses import dataclass
import hashlib
import json
from pathlib import Path
import re
import sys
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = "github.com/MarcoPoloResearchLab/Lo/tests/integration"
IDENTITY_MARKER = "LO_SEMANTIC_IDENTITY="
COVERAGE_GATES = ("TestSemanticCoverageInventory", "TestSemanticCoverageRunIdentity")
TARGETS = ("darwin/arm64", "linux/arm64")
BOUNDARIES = {"compiler-command", "program-command", "native-abi"}
PROFILES = {"frontend", "software", "controlled-metal", "metal-artifact"}
TEST_IDENTITY = re.compile(r"Test[A-Za-z0-9_]+(?:/[^\n]+)?")
SOURCE_FOLDERS = ("cmd/lo", "internal", "tests/integration", "tools/semanticcoverage", "runtime",
                  "tools/metalbuild", "tools/nativeruntime", "tools/lodistribution", "tools/loqualification",
                  "testdata", "examples", "docs/examples")
SOURCE_SUFFIXES = {".go", ".py", ".c", ".h", ".m", ".mm", ".metal", ".lo", ".json", ".txt", ".rs", ".toml", ".lock"}
SOURCE_FILES = ("go.mod", "go.sum", "Makefile", "docs/evidence/training-reference-results.json")


def contained(raw: str) -> Path:
    path = Path(raw)
    resolved = (ROOT / path if not path.is_absolute() else path).resolve()
    if not resolved.is_relative_to(ROOT):
        raise ValueError(f"path outside repository: {raw}")
    return resolved


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_matrix(matrix: dict[str, Any]) -> None:
    if set(matrix) != {"operations", "contracts", "gaps"}:
        raise ValueError("invalid matrix fields; use direct native contracts")
    operations = {}
    for row in matrix["operations"]:
        if set(row) != {"operation", "capability", "errors", "activation_errors", "negative_applicability"}:
            raise ValueError("invalid operation metadata fields")
        name = row["operation"]
        if not isinstance(name, str) or not name or name in operations:
            raise ValueError(f"unknown or duplicate operation metadata: {name}")
        if not isinstance(row["capability"], str) or row["negative_applicability"] not in {
                "catalog_failure_or_activation", "verifier_and_callback", "verifier_only_typed_total",
                "verifier_only_canonical_state_constructor"}:
            raise ValueError(f"invalid operation metadata: {name}")
        for axis in ("errors", "activation_errors"):
            if not isinstance(row[axis], list) or any(not isinstance(item, str) or not item for item in row[axis]) or len(set(row[axis])) != len(row[axis]):
                raise ValueError(f"invalid operation errors: {name}")
        operations[name] = row
    seen = set()
    axes = {name: set() for name in operations}
    for row in matrix["contracts"]:
        if set(row) != {"id", "operation", "axis", "boundary", "targets", "profile", "tests"}:
            raise ValueError("invalid contract fields")
        identity = row["id"]
        if not isinstance(identity, str) or not identity or identity in seen:
            raise ValueError(f"empty or duplicate contract ID: {identity}")
        seen.add(identity)
        if row["operation"] and row["operation"] not in operations:
            raise ValueError(f"unknown operation in contract: {identity}: {row['operation']}")
        if row["axis"] not in {"positive", "negative", "runtime-negative", "contract"}:
            raise ValueError(f"invalid contract axis: {identity}")
        if bool(row["operation"]) != (row["axis"] != "contract"):
            raise ValueError(f"operation/axis mismatch: {identity}")
        if row["operation"]:
            axes[row["operation"]].add(row["axis"])
        targets, refs = row["targets"], row["tests"]
        if not isinstance(targets, list) or not targets or any(target not in TARGETS for target in targets) or len(set(targets)) != len(targets):
            raise ValueError(f"invalid or duplicate contract target: {identity}")
        if row["boundary"] not in BOUNDARIES or row["profile"] not in PROFILES:
            raise ValueError(f"invalid boundary/profile: {identity}")
        if (row["profile"] == "frontend") != (row["boundary"] == "compiler-command"):
            raise ValueError(f"compiler boundary/profile mismatch: {identity}")
        if row["profile"] == "controlled-metal" and row["boundary"] != "native-abi":
            raise ValueError(f"controlled driver boundary mismatch: {identity}")
        if row["profile"] == "metal-artifact" and targets != ["darwin/arm64"]:
            raise ValueError(f"Metal artifact target mismatch: {identity}")
        if not isinstance(refs, list) or not refs or any(not isinstance(ref, str) or not TEST_IDENTITY.fullmatch(ref) for ref in refs):
            raise ValueError(f"missing or invalid test identity: {identity}")
        if len(set(refs)) != len(refs):
            raise ValueError(f"duplicate test reference within contract: {identity}")
    if not seen:
        raise ValueError("matrix has no direct contracts")
    for name, row in operations.items():
        required = {"positive", "negative"}
        if row["errors"] or row["activation_errors"]:
            if row["negative_applicability"] != "verifier_only_canonical_state_constructor":
                required.add("runtime-negative")
        missing = required - axes[name]
        if missing:
            raise ValueError(f"operation {name} missing contract axes: {sorted(missing)}")
    gap_ids = set()
    for gap in matrix["gaps"]:
        if (set(gap) != {"ID", "Scope", "Reason", "Owner", "Acceptance"}
                or not all(isinstance(value, str) and value for value in gap.values())
                or not re.fullmatch(r"[A-Z][0-9]{3}", gap["Owner"])
                or gap["Acceptance"] not in {"required", "disclosed_limit", "downstream"}
                or (gap["Acceptance"] == "disclosed_limit" and gap["Owner"] != "I005")
                or (gap["Acceptance"] == "downstream" and gap["Owner"] == "I005")
                or gap["ID"] in gap_ids):
            raise ValueError(f"invalid gap classification: {gap}")
        gap_ids.add(gap["ID"])


def references(matrix: dict[str, Any]) -> dict[str, set[str]]:
    validate_matrix(matrix)
    result: dict[str, set[str]] = {}
    for row in matrix["contracts"]:
        for reference in row["tests"]:
            result.setdefault(reference, set()).add(row["boundary"])
    return result


@dataclass(frozen=True)
class Inputs:
    matrix: Path
    native: dict[str, Path]
    output: Path
    allow_partial: bool = False


@dataclass(frozen=True)
class Events:
    statuses: dict[str, str]
    identity: dict[str, str]


def verify_identity(identity: dict[str, str]) -> None:
    fields = {"implementation", "target", "abi", "profile", "compiler", "compiler_sha256", "source_sha256",
              "matrix_sha256", "toolchain", "toolchain_sha256", "runtime_sha256"}
    if set(identity) not in (fields, fields | {"metal_runtime_sha256"}) or not all(isinstance(value, str) and value for value in identity.values()):
        raise ValueError("invalid execution identity fields")
    if identity["implementation"] != "llvm-native" or identity["profile"] != "software" or identity["target"] not in TARGETS:
        raise ValueError("native implementation/target/profile identity mismatch")
    if digest(contained(identity["compiler"])) != identity["compiler_sha256"]:
        raise ValueError("compiler identity mismatch")
    path = contained(identity["toolchain"])
    if digest(path) != identity["toolchain_sha256"]:
        raise ValueError("toolchain identity mismatch")
    manifest = json.loads(path.read_text())
    if identity["abi"] != manifest["abi"]:
        raise ValueError("runtime ABI identity mismatch")
    runtime = manifest["targets"][identity["target"]]["runtime"]
    actual = digest(contained(str(path.parent / runtime["path"])))
    if actual != runtime["sha256"] or actual != identity["runtime_sha256"]:
        raise ValueError("runtime identity mismatch")
    if "metal_runtime_sha256" in identity:
        metal = manifest["metal"]["runtime"]
        if identity["target"] != "darwin/arm64" or digest(contained(str(path.parent / metal["path"]))) != metal["sha256"] or metal["sha256"] != identity["metal_runtime_sha256"]:
            raise ValueError("Metal runtime identity mismatch")


def read_events(path: Path, expected: dict[str, str]) -> Events:
    statuses: dict[str, str] = {}
    started = completed = False
    identities = []
    for line in path.read_text().splitlines():
        event = json.loads(line)
        if event.get("Package") != PACKAGE:
            continue
        action, test = event.get("Action"), event.get("Test")
        if action == "output" and test == "TestSemanticCoverageRunIdentity" and IDENTITY_MARKER in event.get("Output", ""):
            identities.append(json.loads(event["Output"].split(IDENTITY_MARKER, 1)[1]))
        if not test:
            if action == "start":
                if started:
                    raise ValueError(f"duplicate package run: {path}")
                started = True
            elif action in {"pass", "fail", "skip"}:
                if completed:
                    raise ValueError(f"duplicate package completion: {path}")
                if action != "pass":
                    raise ValueError(f"package did not pass: {path}")
                completed = True
        elif action in {"pass", "fail", "skip"}:
            if test in statuses:
                raise ValueError(f"duplicate test completion: {test}: {path}")
            statuses[test] = action
    if not started or not completed:
        raise ValueError(f"incomplete package execution: {path}")
    if len(identities) != 1 or any(identities[0].get(key) != value for key, value in expected.items()):
        raise ValueError(f"source/matrix/target identity mismatch: {path}")
    identity = identities[0]
    verify_identity(identity)
    for gate in COVERAGE_GATES:
        if statuses.get(gate) != "pass":
            raise ValueError(f"coverage gate did not pass: {gate}: {path}")
    return Events(statuses, identity)
def source_identity() -> tuple[dict[str, str], str]:
    sources = {}
    for folder in SOURCE_FOLDERS:
        for path in sorted((ROOT / folder).rglob("*")):
            if path.suffix not in SOURCE_SUFFIXES:
                continue
            resolved = contained(str(path))
            if resolved.is_file():
                sources[str(path.relative_to(ROOT))] = digest(resolved)
    for name in SOURCE_FILES:
        sources[name] = digest(contained(name))
    combined = "".join(f"{path}\0{sha}\n" for path, sha in sorted(sources.items()))
    return sources, hashlib.sha256(combined.encode()).hexdigest()



def report(inputs: Inputs) -> int:
    validate_output_paths((inputs.output,), consumed_inputs(inputs.matrix, inputs.native))
    inputs.output.unlink(missing_ok=True)
    matrix = json.loads(inputs.matrix.read_text())
    validate_matrix(matrix)
    sources, source_hash = source_identity()
    expected = {"source_sha256": source_hash, "matrix_sha256": digest(inputs.matrix)}
    lanes = {}
    for target, path in sorted(inputs.native.items()):
        if target not in TARGETS:
            raise ValueError(f"unsupported native target: {target}")
        lanes[target] = read_events(path, {**expected, "target": target})
    if not lanes:
        raise ValueError("report requires native execution logs")
    required_targets = set().union(*(set(row["targets"]) for row in matrix["contracts"]))
    missing_targets = sorted(required_targets - set(lanes))
    observations, failures = [], []
    for row in matrix["contracts"]:
        observation = {**row, "observations": {}}
        for target in row["targets"]:
            if target not in lanes:
                observation["observations"][target] = {"status": "not-run"}
                continue
            events = lanes[target]
            statuses = {reference: events.statuses.get(reference, "missing") for reference in row["tests"]}
            status = "pass" if all(value == "pass" for value in statuses.values()) else "missing"
            runtime = events.identity["runtime_sha256"]
            if row["profile"] == "metal-artifact":
                runtime = events.identity.get("metal_runtime_sha256", "")
                if not runtime:
                    status = "missing"
            observation["observations"][target] = {"status": status, "tests": statuses,
                                                    "runtime_sha256": runtime if row["profile"] != "frontend" else ""}
            if status != "pass":
                failures.append(f"{target}:{row['id']}:{statuses}")
        observations.append(observation)
    required_gaps = [gap for gap in matrix["gaps"] if gap["Acceptance"] == "required"]
    qualification_gaps = [gap for gap in matrix["gaps"] if gap["Acceptance"] == "required" or gap["Owner"] == "I011"]
    result = {"execution_status": "incomplete" if failures else "passed",
              "qualification_status": "passed" if not (failures or qualification_gaps or missing_targets) else "incomplete",
              "coverage_status": "incomplete" if required_gaps else "complete_with_limits" if matrix["gaps"] else "complete",
              "missing_targets": missing_targets, "qualification_gaps": qualification_gaps,
              "operation_count": len(matrix["operations"]), "contract_count": len(observations),
              "matrix_sha256": digest(inputs.matrix), "log_sha256": {target: digest(path) for target, path in inputs.native.items()},
              "source_sha256": sources, "execution_identity": {target: event.identity for target, event in lanes.items()},
              "coverage_gates": {target: {gate: event.statuses[gate] for gate in COVERAGE_GATES} for target, event in lanes.items()},
              "contracts": observations, "gaps": matrix["gaps"]}
    inputs.output.parent.mkdir(parents=True, exist_ok=True)
    inputs.output.write_text(json.dumps(result, indent=2) + "\n")
    if failures:
        print("unverified references: " + ", ".join(failures), file=sys.stderr)
    print(json.dumps({"execution_status": result["execution_status"], "qualification_status": result["qualification_status"],
                      "coverage_status": result["coverage_status"], "missing_targets": missing_targets,
                      "contracts": len(observations), "report": str(inputs.output.relative_to(ROOT))}))
    if failures:
        return 1
    if required_gaps or ((missing_targets or qualification_gaps) and not inputs.allow_partial):
        print("incomplete qualification: " + ", ".join([gap["ID"] for gap in qualification_gaps] + missing_targets), file=sys.stderr)
        return 2
    return 0


def native_logs(raw: list[str]) -> dict[str, Path]:
    result = {}
    for value in raw:
        target, separator, path = value.partition("=")
        if not separator or target not in TARGETS or target in result:
            raise ValueError(f"expected unique supported target=log: {value}")
        result[target] = contained(path)
    return result


def toolchain_inputs(toolchain: Path) -> set[Path]:
    paths = {toolchain}
    manifest = json.loads(toolchain.read_text())

    def collect(value: Any) -> None:
        if isinstance(value, dict):
            if isinstance(value.get("path"), str) and isinstance(value.get("sha256"), str):
                paths.add(contained(str(toolchain.parent / value["path"])))
            for item in value.values():
                collect(item)
        elif isinstance(value, list):
            for item in value:
                collect(item)

    collect(manifest)
    return paths


def consumed_inputs(matrix: Path, logs: dict[str, Path], compiler: Path | None = None,
                    toolchain: Path | None = None) -> set[Path]:
    paths = {matrix, *logs.values(), *(contained(name) for name in source_identity()[0])}
    if compiler is not None:
        paths.add(compiler)
    if toolchain is not None:
        paths.update(toolchain_inputs(toolchain))
    for log in logs.values():
        for line in log.read_text().splitlines():
            event = json.loads(line)
            if (event.get("Package") != PACKAGE or event.get("Action") != "output"
                    or event.get("Test") != "TestSemanticCoverageRunIdentity"
                    or IDENTITY_MARKER not in event.get("Output", "")):
                continue
            identity = json.loads(event["Output"].split(IDENTITY_MARKER, 1)[1])
            paths.add(contained(identity["compiler"]))
            paths.update(toolchain_inputs(contained(identity["toolchain"])))
    return paths


def validate_output_paths(outputs: tuple[Path, ...], inputs: set[Path]) -> None:
    protected = {contained(str(path)) for path in inputs}
    existing = set()
    for path in protected:
        if path.exists():
            identity = path.stat()
            existing.add((identity.st_dev, identity.st_ino))
    for raw in outputs:
        path = contained(str(raw))
        alias = False
        if path.exists():
            identity = path.stat()
            alias = (identity.st_dev, identity.st_ino) in existing
        if path in protected or alias:
            raise ValueError(f"generated output overlaps an input: {path}")
        if not path.is_relative_to(ROOT / "_scratch"):
            raise ValueError(f"generated output must stay inside repository _scratch: {path}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--matrix", default="testdata/semantic-migration/matrix.json")
    parser.add_argument("--native", action="append", default=[], metavar="TARGET=LOG")
    parser.add_argument("--allow-partial", action="store_true")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        inputs = Inputs(contained(args.matrix), native_logs(args.native), contained(args.output), args.allow_partial)
        return report(inputs)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"semantic coverage: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
