import argparse
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

import report


def select_tests(matrix: Path, target: str | None = None) -> str:
    data = json.loads(matrix.read_text())
    references = report.references(data)
    if target is not None:
        if target not in report.TARGETS:
            raise ValueError(f"unsupported semantic execution target: {target}")
        references = {reference for row in data["contracts"] if target in row["targets"] for reference in row["tests"]}
    tests = set(report.COVERAGE_GATES)
    tests.update(reference.split("/", 1)[0] for reference in references)
    return "^(" + "|".join(sorted(tests)) + ")$"


def run(matrix: Path, output_root: Path, compiler: Path,
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
    log = report.contained(str(output_root / ("semantic-native-" + target.replace("/", "-") + ".jsonl")))
    output = report.contained(str(output_root / "semantic-coverage.json"))
    if not compiler.is_file() or not toolchain.is_file():
        raise ValueError("run requires --compiler and --toolchain installed distribution inputs")
    if target in additional:
        raise ValueError(f"additional native log duplicates current host target: {target}")
    report.validate_output_paths((log, output), report.consumed_inputs(matrix, additional, compiler, toolchain))
    output_root.mkdir(parents=True, exist_ok=True)
    output.unlink(missing_ok=True)
    pattern = select_tests(matrix, target)
    command = ["go", "test", "-json", "-count=1", "-timeout=120m", "-run", pattern, "./tests/integration"]
    environment = dict(os.environ)
    environment["LO_SEMANTIC_MATRIX"] = str(matrix)
    environment["LO_TEST_COMPILER"] = str(compiler)
    environment["LO_LLVM_TOOLCHAIN"] = str(toolchain)
    environment["LO_TEST_TARGET"] = target
    print(f"semantic execution: native:{target}", flush=True)
    with log.open("wb") as stream:
        result = subprocess.run(command, cwd=report.ROOT, env=environment,
                                stdin=subprocess.DEVNULL, stdout=stream)
    if result.returncode:
        print(f"semantic execution: native:{target} failed; see {log.relative_to(report.ROOT)}", file=sys.stderr)
        return 1
    return report.report(report.Inputs(matrix, {**additional, target: log}, output, allow_partial))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("select", "run"))
    parser.add_argument("--matrix", default="testdata/semantic-migration/matrix.json")
    parser.add_argument("--output-root", default="_scratch/build-state")
    parser.add_argument("--compiler", default=os.environ.get("LO_TEST_COMPILER", ""))
    parser.add_argument("--toolchain", default=os.environ.get("LO_LLVM_TOOLCHAIN", ""))
    parser.add_argument("--native", action="append", default=[], metavar="TARGET=LOG")
    parser.add_argument("--allow-partial", action="store_true")
    args = parser.parse_args()
    try:
        matrix = report.contained(args.matrix)
        pattern = select_tests(matrix)
        if args.action == "select":
            print(pattern)
            return 0
        return run(matrix, report.contained(args.output_root),
                   report.contained(args.compiler), report.contained(args.toolchain),
                   report.native_logs(args.native), args.allow_partial)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"semantic migration: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
