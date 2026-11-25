"""Build all current compiler packages once for an isolated test session."""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from tools.testsession.shared_runtime import SESSION_VARIABLE, contained  # noqa: E402

PACKAGE = ROOT / "tools/lodistribution/package.py"
VARIANTS = (("darwin/arm64", "software"), ("linux/arm64", "software"), ("darwin/arm64", "metal"))


class PreparationError(RuntimeError):
    """Report one failed public producer with its original command evidence."""


@dataclass(frozen=True)
class Inputs:
    prepared: Path
    runtime: Path
    work: Path

    @classmethod
    def validate(cls, prepared: Path, runtime: Path, work: Path) -> Inputs:
        supplied = os.environ.get(SESSION_VARIABLE)
        if not supplied:
            raise ValueError(f"compiler package preparation requires {SESSION_VARIABLE}")
        session = contained(Path(supplied))
        prepared, runtime, work = map(contained, (prepared, runtime, work))
        for path in (runtime, work):
            if not path.is_relative_to(session) or path == session:
                raise ValueError("runtime and package work must belong to their test session")
        if work == runtime or work.is_relative_to(runtime) or runtime.is_relative_to(work):
            raise ValueError("package work must not overlap the consumed runtime")
        if work.exists():
            raise ValueError(f"compiler package work must be fresh: {work}")
        if not work.parent.is_dir():
            raise ValueError("compiler package work requires an existing session parent")
        for path in (prepared, runtime / "toolchain.json", runtime / "build-receipt.json"):
            if not path.is_file():
                raise ValueError(f"compiler package input is missing: {path}")
            if not isinstance(json.loads(path.read_text()), dict):
                raise ValueError(f"compiler package input must be a JSON object: {path}")
        return cls(prepared, runtime, work)


def construct(inputs: Inputs) -> dict[str, object]:
    go = shutil.which("go")
    if go is None:
        raise ValueError("compiler package preparation requires the Go producer")
    inputs.work.mkdir()
    try:
        prepared = inputs.work / "prepared.json"
        shutil.copyfile(inputs.prepared, prepared)
        packages = inputs.work / "packages"
        packages.mkdir()

        def build(profile: tuple[str, str]) -> tuple[str, str]:
            host, variant = profile
            name = "lo-" + host.replace("/", "-") + "-" + variant
            output = packages / name
            command = [sys.executable, "-B", str(PACKAGE), "--prepared", str(prepared),
                       "--runtime", str(inputs.runtime), "--host", host, "--variant", variant,
                       "--go", str(Path(go).resolve()), "--output", str(output)]
            result = subprocess.run(command, cwd=ROOT, env=dict(os.environ, CI="1", PYTHONDONTWRITEBYTECODE="1"),
                                    stdin=subprocess.DEVNULL, capture_output=True, check=False)
            if result.returncode:
                raise PreparationError(f"package {name}: command={command!r} status={result.returncode} "
                                       f"stdout={result.stdout.decode(errors='replace')[-65536:]!r} "
                                       f"stderr={result.stderr.decode(errors='replace')[-65536:]!r}")
            receipt = json.loads(result.stdout)
            if not isinstance(receipt, dict) or receipt.get("package") != str(output.relative_to(ROOT)) or receipt.get("host") != host or receipt.get("variant") != variant:
                raise PreparationError(f"package {name}: public producer returned a different package identity")
            for path in (output / "toolchain.json", output / "inventory.json", output.with_suffix(".tar"), output.with_suffix(".receipt.json")):
                if not path.is_file():
                    raise PreparationError(f"package {name}: public producer did not complete {path}")
            return name, str(output)

        workers = min(len(VARIANTS), os.cpu_count() or 1)
        with ThreadPoolExecutor(max_workers=workers) as producers:
            outputs = dict(producers.map(build, VARIANTS))
        return {"status": "pass", "packages": outputs}
    except BaseException as error:
        try:
            shutil.rmtree(inputs.work)
        except OSError as cleanup:
            raise PreparationError(f"{error}; remove owned package work {inputs.work}: {cleanup}") from error
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prepared", type=Path, required=True)
    parser.add_argument("--runtime", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(construct(Inputs.validate(args.prepared, args.runtime, args.work))))
    except (OSError, ValueError, PreparationError, subprocess.SubprocessError) as error:
        print(f"session compiler packages: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
