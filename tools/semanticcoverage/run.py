import argparse
import json
import os
from pathlib import Path
import re
import platform
import subprocess
import sys

import report

TEST_IDENTITY = re.compile(r"Test[A-Za-z0-9_]+(?:/[^\n]+)?")


def select_tests(matrix: Path) -> str:
    data = json.loads(matrix.read_text())
    references = []
    for row in data["operations"]:
        for axis in ("positive", "negative", "runtime_negative"):
            references.extend(row[axis])
    for section in ("cli", "semantics", "profiles"):
        for row in data[section]:
            references.extend(row["tests"])
    if not references:
        raise ValueError("matrix has no test references")
    for native_refs in data["native_tests"].values():
        references.extend(native_refs)
    tests = set(report.COVERAGE_GATES)
    for reference in references:
        if not isinstance(reference, str) or not TEST_IDENTITY.fullmatch(reference):
            raise ValueError(f"invalid test identity: {reference}")
        tests.add(reference.split("/", 1)[0])
    return "^(" + "|".join(sorted(tests)) + ")$"


def run(matrix: Path, output_root: Path, pattern: str, compiler: Path,
        toolchain: Path, additional: dict[str, Path], allow_partial: bool) -> int:
    for name in ("HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH"):
        raw = os.environ.get(name)
        if not raw or not Path(raw).is_absolute():
            raise ValueError(f"{name} must declare a repository-local directory; use make test-semantic-migration")
        report.contained(raw)
    host = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system(), "unsupported")
    target = host + "/" + {"arm64": "arm64", "aarch64": "arm64"}.get(platform.machine(), "unsupported")
    if target not in report.TARGETS:
        raise ValueError(f"unsupported semantic execution target: {target}")
    output_root.mkdir(parents=True, exist_ok=True)
    logs = [report.contained(str(output_root / f"semantic-{lane}.jsonl"))
            for lane in ("interpreter", "llvm-" + target.replace("/", "-"))]
    output = report.contained(str(output_root / "semantic-coverage.json"))
    if matrix in (*logs, output) or matrix in additional.values():
        raise ValueError("semantic migration output overlaps matrix")
    if not compiler.is_file() or not toolchain.is_file():
        raise ValueError("run requires --compiler and --toolchain installed distribution inputs")
    if target in additional:
        raise ValueError(f"additional LLVM log duplicates current host target: {target}")
    output.unlink(missing_ok=True)
    command = ["go", "test", "-json", "-count=1", "-timeout=120m", "-run", pattern, "./tests/integration"]
    for lane, path in zip(("interpreter", "llvm"), logs):
        environment = dict(os.environ)
        environment["LO_SEMANTIC_MATRIX"] = str(matrix)
        environment["LO_TEST_COMPILER"] = str(compiler)
        environment["LO_LLVM_TOOLCHAIN"] = str(toolchain)
        environment["LO_TEST_TARGET"] = target
        environment["LO_TEST_BACKEND"] = lane
        print(f"semantic migration: {lane}:{target}", flush=True)
        with path.open("wb") as stream:
            result = subprocess.run(command, cwd=report.ROOT, env=environment,
                                    stdin=subprocess.DEVNULL, stdout=stream)
        if result.returncode:
            print(f"semantic migration: {lane}:{target} failed; see {path.relative_to(report.ROOT)}", file=sys.stderr)
            return 1
    return report.report(report.Inputs(matrix, logs[0], {**additional, target: logs[1]}, output, allow_partial))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("select", "run"))
    parser.add_argument("--matrix", default="testdata/semantic-migration/matrix.json")
    parser.add_argument("--output-root", default="_scratch/build-state")
    parser.add_argument("--compiler", default=os.environ.get("LO_TEST_COMPILER", ""))
    parser.add_argument("--toolchain", default=os.environ.get("LO_LLVM_TOOLCHAIN", ""))
    parser.add_argument("--llvm-log", action="append", default=[], metavar="TARGET=LOG")
    parser.add_argument("--allow-partial", action="store_true")
    args = parser.parse_args()
    try:
        matrix = report.contained(args.matrix)
        pattern = select_tests(matrix)
        if args.action == "select":
            print(pattern)
            return 0
        return run(matrix, report.contained(args.output_root), pattern,
                   report.contained(args.compiler), report.contained(args.toolchain),
                   report.llvm_logs(args.llvm_log), args.allow_partial)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"semantic migration: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
