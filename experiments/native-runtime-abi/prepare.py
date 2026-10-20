"""Prepare source-bound static ABI fixtures before offline qualification."""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import subprocess
import sys

from probe import ROOT, SOURCE, backend, runtime_command


def prepare(prefix_name: str, output_name: str) -> None:
    """Compile both archives and publish a receipt only after both succeed."""
    prefix = backend.relative_path(prefix_name)
    compiler = backend.relative_path((prefix / "bin/rustc").relative_to(ROOT).as_posix())
    source = backend.relative_path((SOURCE / "runtime.rs").relative_to(ROOT).as_posix())
    output = backend.relative_path(output_name, exists=False)
    if any(path.is_relative_to(output) or output.is_relative_to(path) for path in (prefix, SOURCE)):
        raise backend.InputError("runtime preparation output overlaps compiler or source inputs")
    manifest = backend.relative_path((output / "runtime-libraries.json").relative_to(ROOT).as_posix(), exists=False)
    artifacts: dict[str, tuple[Path, Path, Path]] = {}
    for target in backend.TARGETS:
        directory = backend.relative_path((output / target).relative_to(ROOT).as_posix(), exists=False)
        archive = backend.relative_path((directory / "liblo_abi_probe.a").relative_to(ROOT).as_posix(), exists=False)
        log = backend.relative_path((directory / "compile.json").relative_to(ROOT).as_posix(), exists=False)
        artifacts[target] = (directory, archive, log)
    output.mkdir(parents=True, exist_ok=True)
    manifest.unlink(missing_ok=True)
    env = backend.environment(output)
    source_digest = backend.sha256(source)
    compiler_digest = backend.sha256(compiler)
    prefix_digest = backend.tree_sha256(prefix)
    identity = subprocess.run([str(compiler), "-vV"], cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                              capture_output=True, text=True, check=True).stdout
    record = {
        "schema": 1,
        "runtime_source": {"path": str(source.relative_to(ROOT)), "sha256": source_digest},
        "compiler": {"path": str(compiler.relative_to(ROOT)), "sha256": compiler_digest,
                     "identity": identity, "sysroot": {"path": prefix_name, "sha256": prefix_digest}},
        "libraries": {},
    }
    for target in backend.TARGETS:
        directory, archive, log = artifacts[target]
        directory.mkdir(exist_ok=True)
        archive.unlink(missing_ok=True)
        command = runtime_command(compiler, prefix, target, archive)
        process = subprocess.run(command, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                                 capture_output=True, text=True)
        backend.write_json(log, {"command": command, "exit_code": process.returncode,
                                 "stdout": process.stdout, "stderr": process.stderr})
        if process.returncode:
            raise backend.InputError(f"{target} runtime preparation failed: {process.stderr}")
        archive = backend.relative_path(archive.relative_to(ROOT).as_posix())
        record["libraries"][target] = {"archive": {"path": str(archive.relative_to(ROOT)),
                                                  "sha256": backend.sha256(archive)}, "command": command}
    source = backend.relative_path(source.relative_to(ROOT).as_posix())
    compiler = backend.relative_path(compiler.relative_to(ROOT).as_posix())
    if record["runtime_source"]["sha256"] != backend.sha256(source):
        raise backend.InputError("runtime source changed during preparation")
    if compiler_digest != backend.sha256(compiler):
        raise backend.InputError("runtime compiler changed during preparation")
    if prefix_digest != backend.tree_sha256(prefix):
        raise backend.InputError("runtime compiler sysroot changed during preparation")
    backend.write_json(manifest, record)
    print(json.dumps({"prepared": str(manifest.relative_to(ROOT))}))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--rust-prefix", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        prepare(args.rust_prefix, args.output)
        return 0
    except (backend.InputError, OSError, subprocess.CalledProcessError) as error:
        print(f"runtime ABI preparation: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
