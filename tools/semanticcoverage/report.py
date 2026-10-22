import argparse
from dataclasses import dataclass
import hashlib
import json
import re
from pathlib import Path
import sys
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = "github.com/MarcoPoloResearchLab/Lo/tests/integration"
IDENTITY_MARKER = "LO_SEMANTIC_IDENTITY="
COVERAGE_GATES = ("TestSemanticCoverageInventory", "TestSemanticCoverageRunIdentity")
TARGETS = ("darwin/arm64", "linux/arm64")
SOURCE_FOLDERS = ("cmd/lo", "internal", "tests/integration", "tools/semanticcoverage",
                  "runtime", "tools/metalbuild", "tools/nativeruntime", "tools/lodistribution", "tools/loqualification",
                  "testdata", "examples", "docs/examples")
SOURCE_SUFFIXES = {".go", ".py", ".c", ".h", ".m", ".mm", ".metal", ".lo", ".json", ".txt", ".rs", ".toml", ".lock"}
SOURCE_FILES = ("go.mod", "go.sum", "Makefile", "docs/evidence/training-reference-results.json")


def contained(raw: str) -> Path:
    path = Path(raw)
    if not path.is_absolute():
        path = ROOT / path
    resolved = path.resolve()
    if not resolved.is_relative_to(ROOT):
        raise ValueError(f"path outside repository: {raw}")
    return resolved


@dataclass(frozen=True)
class Inputs:
    matrix: Path
    interpreter: Path
    llvm: dict[str, Path]
    output: Path
    allow_partial: bool = False


@dataclass(frozen=True)
class Events:
    statuses: dict[str, str]
    identity: dict[str, str]


def read_events(path: Path, expected: dict[str, str]) -> Events:
    statuses: dict[str, str] = {}
    completed = False
    started = False
    identities = []
    for line in path.read_text().splitlines():
        event = json.loads(line)
        if event.get("Package") != PACKAGE:
            continue
        action = event.get("Action")
        test = event.get("Test")
        if action == "output" and test == "TestSemanticCoverageRunIdentity" and IDENTITY_MARKER in event.get("Output", ""):
            identities.append(json.loads(event["Output"].split(IDENTITY_MARKER, 1)[1]))
        if not test:
            if action == "start":
                if started:
                    raise ValueError(f"duplicate package run: {path}")
                started = True
            elif action in {"pass", "fail", "skip"}:
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
        raise ValueError(f"backend/source/matrix identity mismatch: {path}")
    identity = identities[0]
    verify_identity(identity)
    for gate in COVERAGE_GATES:
        if statuses.get(gate) != "pass":
            raise ValueError(f"coverage gate did not pass: {gate}: {path}")
    return Events(statuses, identity)


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_identity(identity: dict[str, str]) -> None:
    fields = {"backend", "implementation", "target", "compiler", "compiler_sha256", "source_sha256", "matrix_sha256"}
    backend = identity.get("backend")
    if backend == "llvm":
        fields |= {"toolchain", "toolchain_sha256", "runtime_sha256"}
    if set(identity) != fields or not all(isinstance(value, str) and value for value in identity.values()):
        raise ValueError("invalid execution identity fields")
    if identity["target"] not in TARGETS:
        raise ValueError("unsupported execution identity target")
    implementation = {"interpreter": "go-interpreter", "llvm": "llvm-native"}
    if identity["implementation"] != implementation.get(backend):
        raise ValueError("backend implementation identity mismatch")
    if digest(contained(identity["compiler"])) != identity["compiler_sha256"]:
        raise ValueError("compiler identity mismatch")
    if backend == "llvm":
        path = contained(identity["toolchain"])
        if digest(path) != identity["toolchain_sha256"]:
            raise ValueError("toolchain identity mismatch")
        manifest = json.loads(path.read_text())
        runtime = manifest["targets"][identity["target"]]["runtime"]
        actual = digest(contained(str(path.parent / runtime["path"])))
        if actual != runtime["sha256"] or actual != identity["runtime_sha256"]:
            raise ValueError("runtime identity mismatch")


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


def references(matrix: dict[str, Any]) -> dict[str, set[str]]:
    result: dict[str, set[str]] = {}
    for row in matrix["operations"]:
        for axis in ("positive", "negative", "runtime_negative"):
            for ref in row[axis]:
                result.setdefault(ref, set()).add(row["boundary"])
    for section in ("cli", "semantics", "profiles"):
        for row in matrix[section]:
            for ref in row["tests"]:
                result.setdefault(ref, set()).add(row["boundary"])
    if not result:
        raise ValueError("matrix has no test references")
    return result


def report(inputs: Inputs) -> int:
    matrix = json.loads(inputs.matrix.read_text())
    required_gaps = []
    for gap in matrix["gaps"]:
        if (set(gap) != {"ID", "Scope", "Reason", "Owner", "Acceptance"}
                or not all(isinstance(value, str) and value for value in gap.values())
                or not re.fullmatch(r"[A-Z][0-9]{3}", gap["Owner"])
                or gap["Acceptance"] not in {"required", "disclosed_limit", "downstream"}
                or (gap["Acceptance"] == "downstream" and gap["Owner"] == "I005")
                or (gap["Acceptance"] == "disclosed_limit" and gap["Owner"] != "I005")):
            raise ValueError(f"invalid gap classification: {gap}")
        if gap["Acceptance"] == "required":
            required_gaps.append(gap)
    refs = references(matrix)
    native_tests = matrix["native_tests"]
    if set(native_tests) != set(refs) or any(not isinstance(items,list) or any(not isinstance(item,str) or not re.fullmatch(r"Test[A-Za-z0-9_]+(?:/[^\n]+)?",item) for item in items) for items in native_tests.values()):
        raise ValueError("native mapping must declare every original reference exactly once")
    sources, source_hash = source_identity()
    identity = {"source_sha256": source_hash, "matrix_sha256": digest(inputs.matrix)}
    lanes = {"interpreter": read_events(inputs.interpreter, {**identity, "backend": "interpreter"})}
    for target, path in sorted(inputs.llvm.items()):
        if target not in TARGETS:
            raise ValueError(f"unsupported LLVM lane target: {target}")
        lanes["llvm:" + target] = read_events(path, {**identity, "backend": "llvm", "target": target})
    missing_targets = [target for target in TARGETS if target not in inputs.llvm]
    observations = []
    failures = []
    for ref, boundaries in sorted(refs.items()):
        observation = {"test": ref, "boundaries": sorted(boundaries)}
        for lane, events in lanes.items():
            required = [ref] if lane == "interpreter" else native_tests[ref]
            statuses = {item: events.statuses.get(item,"missing") for item in required}
            observation[lane] = "pass" if required and all(value == "pass" for value in statuses.values()) else "missing"
            if lane != "interpreter":
                observation.setdefault("native_tests", {})[lane] = statuses
            if observation[lane] != "pass":
                failures.append(f"{lane}:{ref}:{observation[lane]}")
        observations.append(observation)
    qualification_gaps = [gap for gap in matrix["gaps"] if gap["Acceptance"] == "required" or gap["Owner"] == "I011"]
    qualification_complete = not failures and not qualification_gaps and not missing_targets
    log_paths = {"interpreter": inputs.interpreter,
                 **{"llvm:" + target: path for target, path in inputs.llvm.items()}}
    result = {
        "execution_status": "incomplete" if failures else "passed",
        "qualification_status": "passed" if qualification_complete else "incomplete",
        "coverage_status": "incomplete" if required_gaps else ("complete_with_limits" if matrix["gaps"] else "complete"),
        "missing_targets": missing_targets,
        "qualification_gaps": qualification_gaps,
        "operation_count": len(matrix["operations"]),
        "cli_contract_count": len(matrix["cli"]),
        "matrix_sha256": digest(inputs.matrix),
        "log_sha256": {lane: digest(path) for lane, path in log_paths.items()},
        "source_sha256": sources,
        "execution_identity": {lane: events.identity for lane, events in lanes.items()},
        "coverage_gates": {lane: {gate: events.statuses[gate] for gate in COVERAGE_GATES}
                           for lane, events in lanes.items()},
        "references": observations,
        "gaps": matrix["gaps"],
    }
    inputs.output.parent.mkdir(parents=True, exist_ok=True)
    inputs.output.write_text(json.dumps(result, indent=2) + "\n")
    if failures:
        print("unverified references: " + ", ".join(failures), file=sys.stderr)
    print(json.dumps({"execution_status": result["execution_status"],
                      "qualification_status": result["qualification_status"],
                      "coverage_status": result["coverage_status"],
                      "missing_targets": missing_targets,
                      "references": len(observations), "report": str(inputs.output.relative_to(ROOT))}))
    if failures:
        return 1
    if required_gaps or ((missing_targets or qualification_gaps) and not inputs.allow_partial):
        print("incomplete qualification: " + ", ".join([gap["ID"] for gap in qualification_gaps] + missing_targets), file=sys.stderr)
        return 2
    return 0


def llvm_logs(raw: list[str]) -> dict[str, Path]:
    result = {}
    for value in raw:
        target, separator, path = value.partition("=")
        if not separator or target not in TARGETS or target in result:
            raise ValueError(f"expected unique supported target=log: {value}")
        result[target] = contained(path)
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--matrix", default="testdata/semantic-migration/matrix.json")
    parser.add_argument("--interpreter", required=True)
    parser.add_argument("--llvm", action="append", default=[], metavar="TARGET=LOG")
    parser.add_argument("--allow-partial", action="store_true")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        inputs = Inputs(contained(args.matrix), contained(args.interpreter), llvm_logs(args.llvm),
                        contained(args.output), args.allow_partial)
        if inputs.output in (inputs.matrix, inputs.interpreter, *inputs.llvm.values()):
            raise ValueError("report output overlaps an input")
        inputs.output.unlink(missing_ok=True)
        return report(inputs)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"semantic coverage: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
