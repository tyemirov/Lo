import argparse
from dataclasses import dataclass
import hashlib
import json
import re
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = "github.com/MarcoPoloResearchLab/Lo/tests/integration"
IDENTITY_MARKER = "LO_SEMANTIC_IDENTITY="
COVERAGE_GATES = ("TestSemanticCoverageInventory", "TestSemanticCoverageRunIdentity")


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
    native: Path
    output: Path


def read_events(path: Path, expected: dict[str, str]) -> dict[str, str]:
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
    if identities != [expected]:
        raise ValueError(f"backend/source/matrix identity mismatch: {path}")
    for gate in COVERAGE_GATES:
        if statuses.get(gate) != "pass":
            raise ValueError(f"coverage gate did not pass: {gate}: {path}")
    return statuses


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_identity() -> tuple[dict[str, str], str]:
    sources = {}
    for folder in ("internal", "tests/integration", "tools/semanticcoverage", "testdata", "examples", "docs/examples"):
        for path in sorted((ROOT / folder).rglob("*")):
            if path.suffix not in {".go", ".py", ".c", ".h", ".m", ".mm", ".metal", ".lo", ".json", ".txt"}:
                continue
            resolved = contained(str(path))
            if resolved.is_file():
                sources[str(path.relative_to(ROOT))] = digest(resolved)
    for name in ("go.mod", "go.sum", "Makefile", "docs/evidence/training-reference-results.json"):
        sources[name] = digest(contained(name))
    combined = "".join(f"{path}\0{sha}\n" for path, sha in sorted(sources.items()))
    return sources, hashlib.sha256(combined.encode()).hexdigest()


def report(inputs: Inputs) -> int:
    matrix = json.loads(inputs.matrix.read_text())
    required_gaps = []
    for gap in matrix["gaps"]:
        if (set(gap) != {"ID", "Scope", "Reason", "Owner", "Acceptance"}
                or not all(isinstance(value, str) and value for value in gap.values())
                or not re.fullmatch(r"[A-Z][0-9]{3}", gap["Owner"])
                or gap["Acceptance"] not in {"required", "disclosed_limit", "downstream"}
                or (gap["Acceptance"] == "downstream" and gap["Owner"] == "I005")
                or (gap["Acceptance"] != "downstream" and gap["Owner"] != "I005")):
            raise ValueError(f"invalid gap classification: {gap}")
        if gap["Acceptance"] == "required":
            required_gaps.append(gap)
    references: dict[str, set[str]] = {}
    for row in matrix["operations"]:
        for axis in ("positive", "negative", "runtime_negative"):
            for ref in row[axis]:
                references.setdefault(ref, set()).add(row["boundary"])
    for section in ("cli", "semantics", "profiles"):
        for row in matrix[section]:
            for ref in row["tests"]:
                references.setdefault(ref, set()).add(row["boundary"])
    if not references:
        raise ValueError("matrix has no test references")
    sources, source_hash = source_identity()
    identity = {"source_sha256": source_hash, "matrix_sha256": digest(inputs.matrix)}
    interpreter = read_events(inputs.interpreter, {**identity, "backend": "interpreter"})
    native = read_events(inputs.native, {**identity, "backend": "native"})
    observations = []
    failures = []
    for ref, boundaries in sorted(references.items()):
        observation = {"test": ref, "boundaries": sorted(boundaries),
                       "interpreter": interpreter.get(ref, "missing"),
                       "native": native.get(ref, "missing")}
        observations.append(observation)
        for lane in ("interpreter", "native"):
            if observation[lane] != "pass":
                failures.append(f"{lane}:{ref}:{observation[lane]}")
    result = {
        "execution_status": "incomplete" if failures else "passed",
        "coverage_status": "incomplete" if required_gaps else ("complete_with_limits" if matrix["gaps"] else "complete"),
        "operation_count": len(matrix["operations"]),
        "cli_contract_count": len(matrix["cli"]),
        "matrix_sha256": digest(inputs.matrix),
        "log_sha256": {"interpreter": digest(inputs.interpreter), "native": digest(inputs.native)},
        "source_sha256": sources,
        "execution_identity": identity,
        "coverage_gates": {lane: {gate: events[gate] for gate in COVERAGE_GATES}
                           for lane, events in (("interpreter", interpreter), ("native", native))},
        "references": observations,
        "gaps": matrix["gaps"],
    }
    inputs.output.parent.mkdir(parents=True, exist_ok=True)
    inputs.output.write_text(json.dumps(result, indent=2) + "\n")
    if failures:
        print("unverified references: " + ", ".join(failures), file=sys.stderr)
    print(json.dumps({"execution_status": result["execution_status"],
                      "coverage_status": result["coverage_status"],
                      "references": len(observations), "report": str(inputs.output.relative_to(ROOT))}))
    if failures:
        return 1
    if required_gaps:
        print("open required coverage gaps: " + ", ".join(gap["ID"] for gap in required_gaps), file=sys.stderr)
        return 2
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--matrix", default="testdata/semantic-migration/matrix.json")
    parser.add_argument("--interpreter", required=True)
    parser.add_argument("--native", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        inputs = Inputs(*(contained(value) for value in
                          (args.matrix, args.interpreter, args.native, args.output)))
        if inputs.output in (inputs.matrix, inputs.interpreter, inputs.native):
            raise ValueError("report output overlaps an input")
        inputs.output.unlink(missing_ok=True)
        return report(inputs)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"semantic coverage: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
