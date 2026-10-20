"""Link and execute the P005 runtime ABI fixture with declared offline inputs."""
from __future__ import annotations

import argparse
import importlib.util
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "experiments/native-runtime-abi"
STATE = ROOT / "_scratch/native-runtime-abi"
spec = importlib.util.spec_from_file_location("backend_probe", ROOT / "experiments/native-backends/probe.py")
backend = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = backend
spec.loader.exec_module(backend)


def runtime_command(compiler: Path, prefix: Path, target: str, archive: Path) -> list[str]:
    """Return the single supported fixture preparation recipe."""
    return [str(compiler), "--edition=2021", "--crate-name", "lo_abi_probe", "--crate-type", "staticlib",
            "--sysroot", str(prefix), "--target", target, "-C", "panic=abort", "-C", "opt-level=2",
            "-C", "debuginfo=2", "--remap-path-prefix", f"{ROOT}=/lo", str(SOURCE / "runtime.rs"),
            "-o", str(archive)]


def read_libraries(path: Path) -> tuple[dict, dict[str, backend.Pinned]]:
    """Reject archives prepared from another source or compile recipe."""
    record = backend.fields(json.loads(path.read_text()),
                            {"schema", "runtime_source", "compiler", "libraries"}, "runtime preparation")
    if record["schema"] != 1:
        raise backend.InputError("runtime preparation schema must be 1")
    source = backend.Pinned.read(record["runtime_source"], "runtime source")
    if source.path != SOURCE / "runtime.rs":
        raise backend.InputError("runtime preparation must identify the maintained runtime source")
    compiler = backend.fields(record["compiler"], {"path", "sha256", "identity", "sysroot"}, "compiler")
    # These are preparation provenance. Offline linking needs only the resulting archives.
    compiler_path = backend.relative_path(compiler["path"], exists=False)
    prefix_record = backend.fields(compiler["sysroot"], {"path", "sha256"}, "compiler sysroot")
    prefix = backend.relative_path(prefix_record["path"], exists=False)
    if (not isinstance(compiler["identity"], str) or not compiler["identity"].startswith("rustc ")
            or not isinstance(compiler["sha256"], str)
            or not backend.HASH_PATTERN.fullmatch(compiler["sha256"])
            or not isinstance(prefix_record["sha256"], str)
            or not backend.HASH_PATTERN.fullmatch(prefix_record["sha256"])):
        raise backend.InputError("runtime compiler provenance is incomplete")
    libraries = {}
    for target, entry in backend.fields(record["libraries"], set(backend.TARGETS), "libraries").items():
        entry = backend.fields(entry, {"archive", "command"}, target)
        archive = backend.Pinned.read(entry["archive"], target)
        if entry["command"] != runtime_command(compiler_path, prefix, target, archive.path):
            raise backend.InputError(f"{target} runtime preparation command mismatch")
        libraries[target] = archive
    return record, libraries


def run(manifest: str, libraries_manifest: str) -> int:
    """Validate pins before execution and preserve every isolated command result."""
    result_path = backend.relative_path((STATE / "result.json").relative_to(ROOT).as_posix(), exists=False)
    inputs = backend.Inputs.read(manifest)
    library_path = backend.relative_path(libraries_manifest)
    preparation, libraries = read_libraries(library_path)
    input_paths = [inputs.manifest, library_path, inputs.isolation_bundle.path]
    input_paths += [library.path for library in libraries.values()]
    input_paths += [path for target in inputs.targets for path in (target.bundle.path, target.sysroot.path)]
    if any(path.is_relative_to(STATE) or STATE.is_relative_to(path) for path in input_paths):
        raise backend.InputError("runtime ABI inputs overlap writable state")
    source_files: dict[str, Path] = {}
    for path in sorted(SOURCE.iterdir()):
        checked = backend.relative_path(path.relative_to(ROOT).as_posix())
        if checked.is_file():
            source_files[checked.name] = checked
    expected = backend.relative_path((SOURCE / "expected.txt").relative_to(ROOT).as_posix()).read_text()
    sentinel = backend.relative_path((STATE / "readonly-sentinel").relative_to(ROOT).as_posix(), exists=False)
    writable = backend.relative_path((STATE / "execution/guard-write").relative_to(ROOT).as_posix(), exists=False)
    result_path.unlink(missing_ok=True)
    backend.relative_path(STATE.relative_to(ROOT).as_posix(), exists=False)
    STATE.mkdir(exist_ok=True)
    execution = STATE / "execution"
    backend.relative_path(execution.relative_to(ROOT).as_posix(), exists=False)
    execution.mkdir(exist_ok=True)
    report = {
        "qualification": "not-run",
        "inputs": inputs.identity(),
        "runtime_manifest_sha256": backend.sha256(library_path),
        "runtime_preparation": preparation,
        "runtime_libraries": {target: library.digest for target, library in libraries.items()},
        "source_sha256": {name: backend.sha256(path) for name, path in source_files.items()},
        "cells": [],
    }
    backend.write_json(result_path, report)
    try:
        env = backend.environment(execution)
        sentinel.write_text("ABI isolation sentinel\n")
        sentinel_hash = backend.sha256(sentinel)
        writable.write_text("guard state\n")
        guard = backend.execute(inputs, execution, [str(inputs.probe.pin.path), str(sentinel), str(execution)], env)
        report["isolation_probe"] = guard
        if (guard["exit_code"] != 0 or backend.sha256(sentinel) != sentinel_hash
                or writable.exists() or json.loads(guard["stdout"]) != backend.GUARD_RESULT):
            raise backend.InputError("runtime ABI isolation probe failed")
        for target in inputs.targets:
            output = execution / target.triple
            backend.relative_path(output.relative_to(ROOT).as_posix(), exists=False)
            output.mkdir(exist_ok=True)
            caller, executable = output / "client.o", output / "client"
            for artifact in (caller, executable):
                backend.relative_path(artifact.relative_to(ROOT).as_posix(), exists=False)
                artifact.unlink(missing_ok=True)
            tool = target.tools
            compile_args = [str(tool["cc"].pin.path), f"--target={target.triple}", "-nostdinc", "-ffreestanding",
                            "-Wall", "-Wextra", "-Werror", "-g", "-c", str(SOURCE / "client.c"), "-o", str(caller)]
            if target.triple == backend.TARGETS[0]:
                compile_args += ["-mmacosx-version-min=27.0"]
                link = [str(tool["linker"].pin.path), "-arch", "arm64", "-platform_version", "macos", "27.0", "27.0",
                        "-syslibroot", str(target.sysroot.path), "-dead_strip", "-o", str(executable), str(caller),
                        str(libraries[target.triple].path), str(SOURCE / "libSystem.tbd")]
            else:
                library = target.sysroot.path / "usr/lib/aarch64-linux-gnu"
                dynamic = target.sysroot.path / "lib/aarch64-linux-gnu"
                link = [str(tool["linker"].pin.path), "-m", "aarch64elf", f"--sysroot={target.sysroot.path}",
                        "-pie", "--gc-sections", "--dynamic-linker", "/lib/ld-linux-aarch64.so.1", "-o", str(executable),
                        str(library / "Scrt1.o"), str(library / "crti.o"), str(caller), str(libraries[target.triple].path),
                        f"-L{library}", f"-L{dynamic}", "-lc", str(dynamic / "libgcc_s.so.1"), str(library / "crtn.o")]
            cell = {"target": target.triple, "passed": False, "stages": {}}
            report["cells"].append(cell)
            for name, command in [("caller", compile_args), ("link", link),
                                  ("run", [str(tool["runner"].pin.path), str(executable), str(target.sysroot.path)])]:
                result = backend.execute(inputs, execution, command, env)
                cell["stages"][name] = result
                if result["exit_code"] != 0:
                    raise backend.InputError(f"{target.triple} {name} failed: {result['stderr']}")
                if name != "run":
                    result["artifact"] = backend.inspect_artifact(caller if name == "caller" else executable,
                                                                  target.triple, "object" if name == "caller" else "executable")
                elif result["stdout"] != expected:
                    raise backend.InputError(f"{target.triple} runtime ABI result mismatch")
            if target.triple == backend.TARGETS[0]:
                platform_object, platform_executable = output / "platform.o", output / "platform"
                for artifact in (platform_object, platform_executable):
                    backend.relative_path(artifact.relative_to(ROOT).as_posix(), exists=False)
                    artifact.unlink(missing_ok=True)
                platform_compile = [str(tool["cc"].pin.path), f"--target={target.triple}", "-nostdinc",
                                    "-ffreestanding", "-Wall", "-Wextra", "-Werror", "-mmacosx-version-min=27.0",
                                    "-c", str(SOURCE / "platform.c"), "-o", str(platform_object)]
                platform_link = [str(tool["linker"].pin.path), "-arch", "arm64", "-platform_version",
                                 "macos", "27.0", "27.0", "-syslibroot", str(target.sysroot.path),
                                 "-o", str(platform_executable), str(platform_object)]
                platform_link += [str(SOURCE / name) for name in ("libSystem.tbd", "Foundation.tbd", "Metal.tbd", "libobjc.tbd")]
                for name, command in [("platform_compile", platform_compile), ("platform_link", platform_link),
                                      ("platform_run", [str(tool["runner"].pin.path), str(platform_executable), str(target.sysroot.path)])]:
                    result = backend.execute(inputs, execution, command, env)
                    cell["stages"][name] = result
                    if result["exit_code"] != 0:
                        raise backend.InputError(f"{target.triple} {name} failed: {result['stderr']}")
                    if name == "platform_run":
                        if result["stdout"] != "platform foundation 1 objc 1 metal 1 process 1\n":
                            raise backend.InputError("platform import result mismatch")
                    else:
                        result["artifact"] = backend.inspect_artifact(platform_object if name == "platform_compile" else platform_executable,
                                                                      target.triple, "object" if name == "platform_compile" else "executable")
            cell["passed"] = True
        report["qualification"] = "passed"
        return 0
    except (backend.InputError, OSError, ValueError) as error:
        report["qualification"] = "failed"
        report["error"] = str(error)
        raise
    finally:
        backend.write_json(result_path, report)
        print(json.dumps({"qualification": report["qualification"], "result": str(result_path.relative_to(ROOT))}))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--inputs")
    parser.add_argument("--runtime-libraries", required=True)
    parser.add_argument("--verify-only", action="store_true")
    args = parser.parse_args()
    try:
        if args.verify_only:
            read_libraries(backend.relative_path(args.runtime_libraries))
            print(json.dumps({"runtime_preparation": "verified"}))
            return 0
        if not args.inputs:
            raise backend.InputError("--inputs is required for offline execution")
        return run(args.inputs, args.runtime_libraries)
    except (backend.InputError, OSError, ValueError) as error:
        print(f"runtime ABI probe: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
