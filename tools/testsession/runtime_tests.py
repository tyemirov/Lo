"""Compile and execute the complete prepared Rust unit-test inventory."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import time
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/nativeruntime"))

import build as runtime_build
from prepare import LOCK, RUST_COMPONENTS, digest, local, tree_digest
from process_sdk import SDK

from inventory import original_rows, rust_cases

TAIL_BYTES = 64 << 10


@dataclass(frozen=True)
class Prepared:
    compiler: Path
    cargo: Path
    rust_identity: dict[str, Any]
    tools: dict[str, dict[str, str]]


def prepared_inputs(preparation: Path) -> Prepared:
    preparation = local(preparation)
    rust = json.loads(local(preparation / "rust.json").read_text())
    fields = {"version", "prefix", "archives", "prefix_sha256", "rustc", "cargo"}
    if not isinstance(rust, dict) or set(rust) != fields:
        raise ValueError("prepared Rust receipt has an invalid field set")
    lock = json.loads(LOCK.read_text())
    expected = [item for item in lock["artifacts"] if item["archive"] in RUST_COMPONENTS]
    if rust["version"] != "1.85.1" or rust["archives"] != expected:
        raise ValueError("prepared Rust version or archive identity differs from repository pins")
    prefix = local(Path(rust["prefix"]))
    if rust["prefix_sha256"] != lock["rust_prefix_sha256"] or tree_digest(prefix) != lock["rust_prefix_sha256"]:
        raise ValueError("prepared Rust sysroot identity differs from repository pins")
    selected = {}
    for name in ("rustc", "cargo"):
        path = local(Path(rust[name]["path"]))
        if digest(path) != rust[name]["sha256"]:
            raise ValueError(f"prepared {name} identity mismatch")
        selected[name] = path
    tools = json.loads(local(preparation / "tools.json").read_text())
    if (tools.get("version") != "21.1.8" or tools.get("archive") != lock["artifacts"][0]
            or {name: item["sha256"] for name, item in tools["tools"].items()} != lock["llvm_tools"]):
        raise ValueError("prepared native C tool identities differ from repository pins")
    return Prepared(selected["rustc"], selected["cargo"], rust, tools["tools"])


def storage_bytes(path: Path) -> int:
    if not path.exists():
        return 0
    return sum(item.stat().st_size for item in path.rglob("*") if item.is_file())


def emit(action: str, name: str) -> None:
    print("LO_TEST_EVENT:" + json.dumps({"Action": action, "Test": name}), flush=True)


def cargo_executable(messages: Path) -> Path:
    executables = set()
    for line in messages.read_text().splitlines():
        record = json.loads(line)
        if (record.get("reason") == "compiler-artifact" and record.get("target", {}).get("name") == "lo_runtime"
                and record.get("profile", {}).get("test") is True and record.get("executable")):
            executables.add(local(Path(record["executable"])))
    if len(executables) != 1:
        raise ValueError(f"Rust unit compilation requires one executable, got {len(executables)}")
    return executables.pop()


def command_failure(operation: str, status: int, stdout: Path, stderr: Path) -> ValueError:
    diagnostics = stderr.read_bytes()[-TAIL_BYTES:].decode(errors="replace")
    if operation == "compile Rust unit tests":
        for line in stdout.read_text().splitlines():
            record = json.loads(line)
            if record.get("reason") == "compiler-message" and record.get("message", {}).get("level") == "error":
                diagnostics += "\n" + record["message"].get("rendered", "")[-TAIL_BYTES:]
    else:
        diagnostics += "\n" + stdout.read_bytes()[-TAIL_BYTES:].decode(errors="replace")
    return ValueError(f"{operation}: exit status {status}\n{diagnostics}")


def listed_tests(executable: Path, environment: dict[str, str], work: Path) -> tuple[str, ...]:
    result = subprocess.run([str(executable), "--list", "--format=terse"], cwd=ROOT, env=environment,
                            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30, check=False)
    (work / "test-list.stdout").write_text(result.stdout)
    (work / "test-list.stderr").write_text(result.stderr)
    if result.returncode:
        raise ValueError(f"list Rust unit tests: exit status {result.returncode}: {result.stderr}")
    names = tuple(line.removesuffix(": test") for line in result.stdout.splitlines() if line.endswith(": test"))
    if not names or len(names) != len(set(names)):
        raise ValueError("Rust executable returned an empty or duplicate test inventory")
    original = json.loads((ROOT / "testdata/testsession/inventory.json").read_text())
    required = {(row["file"], row["name"]) for row in original_rows(original, "rust")}
    required.update((case.file, case.name) for case in rust_cases(ROOT))
    for file, name in sorted(required):
        matches = [actual for actual in names if actual == name or actual.endswith("::" + name)]
        if len(matches) != 1:
            raise ValueError(f"original/current Rust test missing or ambiguous in executable: {file}/{name}")
    return names


def run(preparation: Path, work: Path) -> None:
    started = time.monotonic()
    inputs = prepared_inputs(preparation)
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise ValueError("the prepared Rust producer currently selects the Darwin ARM64 host")
    work = local(work)
    if not work.is_relative_to(ROOT / "_scratch"):
        raise ValueError("Rust test work must stay inside Lo/_scratch")
    work.mkdir(parents=True)
    sdk = SDK.discover(ROOT, work)
    sdk_identity = sdk.identity()
    target = local(ROOT / "_scratch/build-state/runtime-test-target")
    before = storage_bytes(target)
    # Rust libtest links the complete OS SDK. The producer's minimal runtime
    # stubs omit libtest symbols and current SDK TAPI requires the SDK linker.
    environment = runtime_build.cargo_environment(work, inputs.compiler, sdk.compiler, sdk.root)
    environment.update(runtime_build.native_c_environment(work, inputs.tools))
    environment.update({"HOME": str(work / "home"), "TMP": str(work), "TEMP": str(work),
                        "CARGO_TARGET_DIR": str(target), "CARGO_BUILD_JOBS": "2",
                        "CLANG_MODULE_CACHE_PATH": str(work / "clang-cache")})
    (work / "home").mkdir()
    command = [str(inputs.cargo), "test", "--no-run", "--frozen", "--offline", "--lib", "--message-format=json",
               "--target", "aarch64-apple-darwin", "--manifest-path", str(ROOT / "runtime/native/Cargo.toml"),
               "--config", str(ROOT / "runtime/native/.cargo/config.toml")]
    stdout, stderr = work / "cargo.jsonl", work / "cargo.stderr"
    compile_started = time.monotonic()
    with stdout.open("wb") as output, stderr.open("wb") as errors:
        built = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                               stdout=output, stderr=errors, timeout=180, check=False)
    compiled_seconds = time.monotonic() - compile_started
    if built.returncode:
        raise command_failure("compile Rust unit tests", built.returncode, stdout, stderr)
    executable = cargo_executable(stdout)
    names = listed_tests(executable, environment, work)
    for name in names:
        emit("collect", name)
    for name in names:
        emit("run", name)
    stdout, stderr = work / "tests.stdout", work / "tests.stderr"
    with stdout.open("wb") as output, stderr.open("wb") as errors:
        executed = subprocess.run([str(executable), "--test-threads=2", "--format=pretty"],
                                  cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                  stdout=output, stderr=errors, timeout=120, check=False)
    results = {}
    for line in stdout.read_text().splitlines():
        match = re.fullmatch(r"test (\S+) \.\.\. (ok|FAILED|ignored)", line)
        if match is None:
            continue
        name, status = match.groups()
        if name not in names or name in results:
            raise ValueError(f"Rust executable returned an unknown or duplicate result: {name}")
        action = {"ok": "pass", "FAILED": "fail", "ignored": "skip"}[status]
        results[name] = action
        emit(action, name)
    if executed.returncode:
        raise command_failure("execute Rust unit tests", executed.returncode, stdout, stderr)
    missing = set(names) - results.keys()
    if missing or any(status != "pass" for status in results.values()):
        raise ValueError(f"Rust unit inventory did not pass completely: missing={sorted(missing)}, results={results}")
    if sdk.identity() != sdk_identity:
        raise ValueError("Rust unit SDK identity changed during execution")
    report = {"tests": list(names), "rust": inputs.rust_identity, "sdk": sdk_identity,
              "executable": str(executable.relative_to(ROOT)), "command": command,
              "compile_seconds": compiled_seconds, "elapsed_seconds": time.monotonic() - started,
              "cache": {"path": str(target.relative_to(ROOT)), "bytes_before": before,
                        "bytes_after": storage_bytes(target)}}
    (work / "report.json").write_text(json.dumps(report, indent=2) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preparation", required=True, type=Path)
    parser.add_argument("--work", required=True, type=Path)
    arguments = parser.parse_args()
    try:
        run(arguments.preparation, arguments.work)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f"runtime tests: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
