"""Review-gated remaining resource cases using the immutable original harness."""
from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys
from types import ModuleType

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-resources-tail"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources-tail"
ORIGINAL_ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources"
ORIGINAL_SOURCE = REPOSITORY / "experiments/process-resources/probe.py"
ORIGINAL_SHA = "8c160bb31491d18b2da5daf5a4a1b633ac5ed198e880446c6ff1def0f44e7354"
ORIGINAL_RESULT_SHA = "1d6960bc2dab9422a49e9504e36ce5034fa3f283ecf1ab654ba846613edeb3b2"
CASE_NAMES = ("wall", "stdout-exact", "stdout-overflow", "stderr-exact", "stderr-overflow", "dual-pressure", "fd", "limits")
PRIOR_NAMES = ("control-fork", "control-vfork", "control-spawn", "fork", "vfork", "spawn", "cpu-soft", "cpu-hard")
ARTIFACTS = {"harness": SOURCE / "probe.py", "test": SOURCE / "test_probe.py", "build": SOURCE / "probe.mk", "original_manifest": ORIGINAL_ROOT / "build.json", "original_result": ORIGINAL_ROOT / "result.json", "original_harness": ORIGINAL_SOURCE}


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def load_original() -> tuple[ModuleType, dict[str, object]]:
    if digest(ORIGINAL_ROOT / "build.json") != ORIGINAL_SHA:
        raise ValueError("reviewed original manifest differs")
    manifest = json.loads(inside(ORIGINAL_ROOT / "build.json").read_text())
    source_bytes = inside(ORIGINAL_SOURCE).read_bytes()
    if hashlib.sha256(source_bytes).hexdigest() != manifest["harness"]:
        raise ValueError("reviewed original harness differs")
    if digest(ORIGINAL_ROOT / "result.json") != ORIGINAL_RESULT_SHA:
        raise ValueError("reviewed original result differs")
    prior = json.loads(inside(ORIGINAL_ROOT / "result.json").read_text())
    results = prior["cases"]
    if tuple(result["case"] for result in results) != PRIOR_NAMES or not all(result["passed"] for result in results[:7]):
        raise ValueError("original case history differs")
    failed = results[7]
    if failed["passed"] or not failed["timeout"] or not failed["host_kill"] or failed["cpu_seconds"] != 4.995 or failed["exit_code"] != -9 or not failed["stop_suite"] or prior["suite_passed"]:
        raise ValueError("original failed CPU assumption differs")
    if prior["reviewed_manifest_sha256"] != ORIGINAL_SHA or prior["post_run_artifacts"] != manifest or prior["fixture_inputs"] != prior["fixture_outputs"]:
        raise ValueError("original integrity evidence differs")
    for result in results:
        if not result["artifacts_unchanged"] or not result["fixtures_unchanged"] or not result["owned_child_joined"] or result["cleanup_errors"] or not all(stream["eof"] and stream["closed"] for stream in result["streams"].values()):
            raise ValueError("original cleanup or integrity evidence differs")
        for stream, evidence in result["streams"].items():
            if digest(ORIGINAL_ROOT / "cases" / f"{result['case']}.{stream}") != evidence["sha256"]:
                raise ValueError("original captured output differs")
    name = "lo_reviewed_resource_probe"
    specification = importlib.util.spec_from_file_location(name, inside(ORIGINAL_SOURCE))
    if specification is None:
        raise ValueError("original module specification is unavailable")
    module = importlib.util.module_from_spec(specification)
    sys.modules[name] = module
    # Execute only the already verified bytes; do not create a bytecode cache.
    exec(compile(source_bytes, str(ORIGINAL_SOURCE), "exec"), module.__dict__)
    if module.ROOT != ORIGINAL_ROOT:
        raise ValueError("original capture root differs")
    module.verify(ORIGINAL_SHA)
    return module, prior


def prepare() -> None:
    load_original()
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior tail result before preparation")
    inside(ROOT).mkdir(parents=True, exist_ok=True)


def seal() -> None:
    load_original()
    inside(ROOT / "build.json").write_text(json.dumps({name: digest(path) for name, path in ARTIFACTS.items()}, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> tuple[ModuleType, dict[str, object], dict[str, str]]:
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed tail manifest differs")
    actual = {name: digest(path) for name, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed tail artifacts differ")
    module, prior = load_original()
    return module, prior, actual


def fixture_hashes(module: ModuleType) -> dict[str, str]:
    return {name: digest(ORIGINAL_ROOT / name) for name in module.FIXTURES}


def run(expected: str) -> int:
    module, prior, reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve the prior tail result before another suite")
    available = {case.name: case for case in module.cases()}
    if any(name not in available for name in CASE_NAMES):
        raise ValueError("original remaining cases differ")
    for name in CASE_NAMES:
        for stream in ("stdout", "stderr"):
            if inside(ORIGINAL_ROOT / "cases" / f"{name}.{stream}").exists():
                raise ValueError("preserve existing tail case captures")
    if any(inside(ORIGINAL_ROOT / name).read_bytes() != content for name, content in module.FIXTURES.items()):
        raise ValueError("original fixture baseline differs")
    before = fixture_hashes(module)
    report = {
        "scope": "Only the remaining eight finite resource cases, using unchanged original helpers, profile and limits. The prior failed CPU-hard assumption remains failed. This is not full Process qualification.",
        "qualified_process_backend": False, "reviewed_manifest_sha256": expected,
        "original_manifest_sha256": ORIGINAL_SHA, "original_result_sha256": ORIGINAL_RESULT_SHA,
        "original_suite_passed": False, "prior_cpu_hard": prior["cases"][7],
        "limits": prior["limits"], "startup_limit": prior["startup_limit"], "cleanup_scope": prior["cleanup_scope"],
        "fixture_inputs": before, "cases": [],
    }
    for name in CASE_NAMES:
        result = module.run_case(available[name])
        try:
            _, _, current = verify(expected)
            result["artifacts_unchanged"] = current == reviewed
            result["fixtures_unchanged"] = fixture_hashes(module) == before
        except (OSError, ValueError) as error:
            result["integrity_error"] = str(error)[:256]
            result["artifacts_unchanged"] = False
            result["fixtures_unchanged"] = False
        report["cases"].append(result)
        if result["stop_suite"] or not result["artifacts_unchanged"] or not result["fixtures_unchanged"]:
            break
    try:
        _, _, post_run = verify(expected)
        report["post_run_artifacts"] = post_run
        report["fixture_outputs"] = fixture_hashes(module)
    except (OSError, ValueError) as error:
        report["integrity_error"] = str(error)[:256]
    passed = len(report["cases"]) == len(CASE_NAMES) and all(result["passed"] and result["artifacts_unchanged"] and result["fixtures_unchanged"] for result in report["cases"]) and report.get("post_run_artifacts") == reviewed and report.get("fixture_outputs") == before
    report["tail_cases_passed"] = bool(passed)
    with inside(ROOT / "result.json").open("x") as destination:
        destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if passed else 1


def main() -> int:
    arguments = sys.argv[1:]
    if arguments and arguments[0] in ("verify", "run") and (len(arguments) != 2 or re.fullmatch(r"[0-9a-f]{64}", arguments[1]) is None):
        raise ValueError("reviewed manifest SHA256 is required")
    if Path.cwd().resolve() != inside(REPOSITORY):
        raise ValueError("run from the Lo repository")
    if arguments == ["prepare"]:
        prepare()
    elif arguments == ["seal"]:
        seal()
    elif len(arguments) == 2 and arguments[0] == "verify":
        verify(arguments[1])
        print(json.dumps({"reviewed_build_verified": True}))
    elif len(arguments) == 2 and arguments[0] == "run":
        return run(arguments[1])
    else:
        raise ValueError("expected prepare, seal, verify SHA256, or run SHA256")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(2)
