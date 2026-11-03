"""Bind benchmark reports to installed inputs and measurement source files."""

from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path
import sys
from typing import Any

from qualification import (
    BUILD_BYTES,
    BUILD_RSS,
    BUILD_SECONDS,
    DIAGNOSTIC_BYTES,
    SAMPLE_PLAN,
    WARMUP,
)

CACHE = "warm filesystem; fresh private tool snapshots"
ORDER = "sequential native samples"
LOAD_POLICY = "record external load; do not gate or discard samples"
LIMITS = {
    "build_seconds": BUILD_SECONDS,
    "artifact_bytes": BUILD_BYTES,
    "diagnostic_bytes": DIAGNOSTIC_BYTES,
    "peak_process_tree_rss_bytes": BUILD_RSS,
}
TIMER_CONTRACT = {
    "clock": "monotonic elapsed time",
    "checked_execution": "admission excluded; fresh session, execution and complete cleanup included",
    "batch_calibration": "untimed doubling of batch size from 1 through 64 until at least 1 ms; then at least 200 ms or 1000000 iterations; elapsed divided by completed iterations; no subtraction",
}
ORACLE_SOURCES = (
    "tests/corpus/corpus.go",
    "docs/evidence/training-reference-results.json",
    "tools/nativebench/oracles.py",
    "tests/benchmark/testdata/training_dense.lo",
    "examples/training_cpu.lo",
)
HARNESS = (
    "tools/nativebench/main.go",
    "tools/nativebench/harness.c.txt",
    "tools/nativebench/allocation.c.txt",
    "runtime/native/include/lo_runtime.h",
    "runtime/native/include/lo_context_options.h",
)


class DiagnosticCompiler(ValueError):
    """A development compiler cannot supply installed-package qualification evidence."""


def contained(root: Path, path: Path) -> Path:
    resolved = path.resolve()
    if not resolved.is_relative_to(root):
        raise ValueError(f"benchmark identity input leaves Lo: {path}")
    return resolved


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sources(root: Path, names: tuple[str, ...]) -> dict[str, str]:
    return {name: digest(contained(root, root / name)) for name in sorted(set(names))}


def semantic_source(root: Path) -> str:
    path = contained(root, root / "tools/semanticcoverage/report.py")
    spec = importlib.util.spec_from_file_location("lo_benchmark_semantic_source", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module.source_identity()[1]


def merge_identity(
    *,
    root: Path,
    compiler: Path,
    manifest: Path,
    target: str,
    environment: dict[str, str],
    accelerator: str,
    producer_kind: str,
    producer_tools: dict[str, str],
    producer_sources: tuple[str, ...],
) -> dict[str, Any]:
    """Read the complete canonical identity before admitting a whole-lane merge."""
    root = root.resolve()
    compiler, manifest = contained(root, compiler), contained(root, manifest)
    package = manifest.parent
    if compiler != package / "bin/lo":
        raise DiagnosticCompiler(
            "merge requires a compiler from its installed distribution"
        )
    validate_environment(target, environment, accelerator, producer_kind)
    if not producer_tools:
        raise ValueError("benchmark identity requires producer tool inputs")
    if not all(isinstance(value, str) and value for value in producer_tools.values()):
        raise ValueError("benchmark producer tool identities must be nonempty strings")
    package_identity, _ = package_provenance(root, compiler, manifest, target)
    provenance = source_provenance(root, producer_sources)
    return {
        "target": target,
        "environment": dict(environment),
        "accelerator": accelerator,
        "package": package_identity,
        "source": provenance["source"],
        "producer": {
            "kind": producer_kind,
            "tools": dict(producer_tools),
            "source_identity": provenance["producer"]["source_identity"],
        },
        "oracle": provenance["oracle"],
        "harness": provenance["harness"],
        "evaluator": provenance["evaluator"],
        "measurement": {
            "sample_plan": dict(SAMPLE_PLAN),
            "warmup": WARMUP,
            "order": ORDER,
            "cache": CACHE,
            "max_steps": 10000000,
            "concurrency": 1,
            "limits": dict(LIMITS),
            "timer_contract": dict(TIMER_CONTRACT),
            "load_policy": LOAD_POLICY,
        },
    }


def source_provenance(root: Path, producer_sources: tuple[str, ...]) -> dict[str, Any]:
    """Capture the complete source inputs before measurements start."""
    return {
        "source": {"semantic_source_sha256": semantic_source(root)},
        "producer": {
            "source_identity": sources(
                root, (*producer_sources, "tools/nativebench/identity.py")
            )
        },
        "oracle": sources(root, ORACLE_SOURCES),
        "harness": sources(root, HARNESS),
        "evaluator": sources(root, ("tools/nativebench/qualification.py",)),
    }


def verify_source_identity(root: Path, identity: dict[str, Any]) -> None:
    """Reject source edits between preparation and completed measurements."""
    if semantic_source(root) != identity["source"]["semantic_source_sha256"]:
        raise ValueError("semantic source changed during measurement")
    inventories = [
        identity["producer"]["source_identity"],
        identity["oracle"],
        identity["harness"],
        identity["evaluator"],
    ]
    for inventory in inventories:
        for name, expected in inventory.items():
            if digest(contained(root, root / name)) != expected:
                raise ValueError(f"benchmark source changed during measurement: {name}")


def validate_environment(
    target: str, environment: dict[str, str], accelerator: str, producer_kind: str
) -> None:
    """Validate the actual environment against the producer execution route."""
    if target not in ("darwin/arm64", "linux/arm64"):
        raise ValueError(f"unsupported benchmark identity target: {target}")
    if (
        set(environment) != {"system", "machine", "processor", "execution"}
        or not all(isinstance(value, str) and value for value in environment.values())
        or environment["machine"] not in ("arm64", "aarch64")
    ):
        raise ValueError(
            "benchmark environment must identify the actual arm64 execution environment"
        )
    if producer_kind not in ("darwin-native", "linux-native", "linux-guest"):
        raise ValueError("benchmark identity requires an explicit measurement producer")
    expected = {
        "darwin-native": ("darwin/arm64", "native", "none"),
        "linux-native": ("linux/arm64", "native", "none"),
        "linux-guest": ("linux/arm64", "qemu-guest", accelerator),
    }[producer_kind]
    if (
        (target, environment["execution"], accelerator) != expected
        or producer_kind == "linux-guest"
        and accelerator not in ("tcg", "hvf")
    ):
        raise ValueError("benchmark target, environment, and accelerator disagree")


def package_provenance(
    root: Path, compiler: Path, manifest: Path, target: str
) -> tuple[dict[str, str], dict[str, str]]:
    """Admit installed package inputs and freeze every declared tool and link input."""
    compiler, manifest = contained(root, compiler), contained(root, manifest)
    package = manifest.parent
    if compiler != package / "bin/lo":
        raise DiagnosticCompiler(
            "merge requires a compiler from its installed distribution"
        )
    if (package / "preliminary.json").exists():
        raise ValueError(
            "preliminary compiler package cannot produce qualification evidence"
        )
    selected = json.loads(manifest.read_text())
    if selected["host"] != target:
        raise ValueError("benchmark package host differs from its execution target")
    runtime = selected["targets"][target]["runtime"]
    archive = contained(root, package / runtime["path"])
    if not archive.is_relative_to(package) or digest(archive) != runtime["sha256"]:
        raise ValueError("benchmark runtime identity differs from installed manifest")
    package_files = {
        "compiler": compiler,
        "manifest": manifest,
        "runtime": archive,
        "inventory": package / "inventory.json",
        "provenance": package / "provenance.json",
    }
    package_identity = {}
    for name, path in package_files.items():
        path = contained(root, path)
        if not path.is_file():
            raise ValueError(f"missing installed package {name}: {path}")
        package_identity[name + "_sha256"] = digest(path)
    frozen = {
        str(path.resolve()): file_identity(path) for path in package_files.values()
    }

    def visit(value: Any) -> None:
        if isinstance(value, dict):
            if set(value) == {"path", "sha256"}:
                path = contained(root, package / value["path"])
                if not path.is_relative_to(package) or digest(path) != value["sha256"]:
                    raise ValueError(f"installed input identity differs: {path}")
                frozen[str(path)] = file_identity(path)
            else:
                for child in value.values():
                    visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)

    visit(selected)
    return package_identity, frozen


def file_identity(path: Path) -> str:
    """Hash a binary input or the complete content of an input directory."""
    if path.is_file():
        return digest(path)
    if not path.is_dir():
        raise ValueError(f"missing producer input: {path}")
    inventory = {
        entry.relative_to(path).as_posix(): digest(entry)
        for entry in sorted(path.rglob("*"))
        if entry.is_file()
    }
    return hashlib.sha256(
        json.dumps(inventory, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


def capture_provenance(
    root: Path,
    compiler: Path,
    manifest: Path,
    target: str,
    producer_sources: tuple[str, ...],
    tool_paths: dict[str, Path],
) -> dict[str, Any]:
    """Freeze package, source, and actual producer tools before preparation."""
    package, files = package_provenance(root, compiler, manifest, target)
    tools = {}
    for name, path in sorted(tool_paths.items()):
        path = path.resolve()
        tools[name + "_sha256"] = file_identity(path)
        files[str(path)] = tools[name + "_sha256"]
    return {
        "package": package,
        "files": files,
        "tools": tools,
        "package_path": str(manifest.resolve().parent),
        "sources": source_provenance(root, producer_sources),
    }


def verify_provenance(root: Path, snapshot: dict[str, Any]) -> None:
    """Reject changed admission inputs before any completed qualification report."""
    if (Path(snapshot["package_path"]) / "preliminary.json").exists():
        raise ValueError(
            "preliminary compiler package cannot produce qualification evidence"
        )
    for name, expected in snapshot["files"].items():
        if file_identity(Path(name)) != expected:
            raise ValueError(
                f"benchmark input changed during preparation or measurement: {name}"
            )
    verify_source_identity(root, snapshot["sources"])
