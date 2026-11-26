"""Build test fixtures on the host and run the Linux semantic corpus offline."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

from batch import GUEST_STATE, ROOT, local, run, verified

sys.path.insert(0, str(ROOT / "tools/semanticcoverage"))
from run import select_tests as test_pattern  # noqa: E402
from report import source_identity  # noqa: E402

PACKAGE = "github.com/MarcoPoloResearchLab/Lo/tests/integration"
SHARED_INPUTS = {
    "LO_TEST_LINUX_INTEGRATION_BINARY": "integration.test",
    "LO_TEST_LINUX_FILEPROBE": "fileprobe",
    "LO_TEST_LINUX_COMPILER": "lo-development",
}
FIXTURE_FOLDERS = ("testdata", "examples", "docs/examples", "runtime/metal")
DIAGNOSTIC_BYTES = 8192


def bounded(text: str) -> str:
    return text.encode(errors="replace")[-DIAGNOSTIC_BYTES:].decode(errors="replace")


def case_failure(work: Path, index: int, case: dict, events: Path | None = None) -> str:
    details = [f'Linux semantic guest case {case["id"]} failed: status={case["status"]}']
    if events is not None:
        outputs: dict[str, str] = {}
        with events.open() as records:
            for line in records:
                event = json.loads(line)
                name = event.get("Test")
                if name and event["Action"] == "output":
                    outputs[name] = bounded(outputs.get(name, "") + event["Output"])
                elif name and event["Action"] == "fail":
                    details.append(f'first failing test: {name}\n{outputs.get(name, "")}')
                    break
    for name in ("stdout", "stderr"):
        with (work / "guest" / f"{index}.{name}").open("rb") as source:
            source.seek(0, os.SEEK_END)
            source.seek(max(0, source.tell() - DIAGNOSTIC_BYTES))
            text = source.read(DIAGNOSTIC_BYTES).decode(errors="replace")
        details.append(f'{name} tail:\n{text}')
    return "\n".join(details)


def source_payloads(matrix: Path) -> list[dict[str, str]]:
    names = set(source_identity()[0])
    names.add(matrix.relative_to(ROOT).as_posix())
    for folder in FIXTURE_FOLDERS:
        for path in (ROOT / folder).rglob("*"):
            path = local(path)
            if path.is_file():
                names.add(path.relative_to(ROOT).as_posix())
    return [{"source": str(local(ROOT / name)), "path": "/workspace/" + name}
            for name in sorted(names)]


def shared_inputs() -> dict[str, Path]:
    selected = {name: os.environ.get(name, "") for name in SHARED_INPUTS}
    if not any(selected.values()):
        return {}
    if not all(selected.values()):
        raise ValueError("select all three precompiled Linux inputs")
    result = {}
    for name, raw in selected.items():
        path = local(Path(raw))
        with path.open("rb") as source:
            header = source.read(20)
        if len(header) != 20 or header[:6] != b"\x7fELF\x02\x01" or header[18:20] != b"\xb7\x00":
            raise ValueError(f"precompiled Linux input must be ARM64 ELF: {path}")
        result[SHARED_INPUTS[name]] = path
    return result


def validate_events(path: Path, expected: tuple[str, ...], required: tuple[str, ...]) -> dict[str, list[str]]:
    runs: set[str] = set()
    completed: dict[str, str] = {}
    package_started = package_completed = False
    for line in path.read_text().splitlines():
        event = json.loads(line)
        if event.get("Package") != PACKAGE:
            raise ValueError("Linux semantic events name an unexpected package")
        action, name = event.get("Action"), event.get("Test")
        if name and action in {"run", "pass", "fail", "skip"} and (not package_started or package_completed):
            raise ValueError("Linux semantic case event is outside package execution")
        if not name:
            if action == "start":
                if package_started:
                    raise ValueError("duplicate Linux semantic package start")
                package_started = True
            elif action in {"pass", "fail", "skip"}:
                if package_completed or action != "pass":
                    raise ValueError("Linux semantic package did not complete successfully exactly once")
                package_completed = True
        elif action == "run":
            if name in runs:
                raise ValueError(f"duplicate Linux semantic test start: {name}")
            runs.add(name)
        elif action in {"pass", "fail", "skip"}:
            if name in completed or name not in runs:
                raise ValueError(f"duplicate or unmatched Linux semantic completion: {name}")
            if action != "pass":
                raise ValueError(f"Linux semantic case did not pass: {name}: {action}")
            completed[name] = action
    actual = {name for name in runs if "/" not in name}
    if not package_started or not package_completed or runs != set(completed):
        raise ValueError("Linux semantic execution has missing package or case completion")
    if actual != set(expected) or len(expected) != len(set(expected)):
        raise ValueError(f"Linux semantic selected inventory differs: missing={sorted(set(expected)-actual)}, unexpected={sorted(actual-set(expected))}")
    missing = set(required) - set(completed)
    if missing:
        raise ValueError(f"Linux semantic referenced cases are missing: {sorted(missing)}")
    return {"selected": sorted(expected), "required": sorted(set(required)), "executed": sorted(completed)}


def environment() -> dict[str, str]:
    result = dict(
        os.environ,
        CI="1",
        CGO_ENABLED="0",
        GOOS="linux",
        GOARCH="arm64",
        GOTOOLCHAIN="local",
        GOENV="off",
        GOWORK="off",
        GOTELEMETRY="off",
        GOPROXY="off",
        GOSUMDB="off",
        GOFLAGS="",
        GOEXPERIMENT="",
    )
    result.pop("GOCACHEPROG", None)
    for name in ("HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH"):
        value = result.get(name)
        if not value or not Path(value).is_absolute():
            raise ValueError(f"{name} must declare a repository-local directory")
        local(Path(value))
    return result


def execute(command: list[str], env: dict[str, str], output: Path) -> None:
    result = subprocess.run(
        command,
        cwd=ROOT,
        env=env,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        check=False,
    )
    output.write_bytes(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f"host fixture command failed: {command}; inspect {output}")


def control_fixture(
    preparation: Path, package: Path, output: Path, env: dict[str, str], log: Path
) -> None:
    tools = json.loads(local(preparation / "tools.json").read_text())["tools"]
    compiler, linker = (verified(tools[name]) for name in ("clang", "ld.lld"))
    manifest = json.loads((package / "toolchain.json").read_text())
    selected = manifest["targets"]["linux/arm64"]

    def resolve(identity: dict[str, str]) -> Path:
        path = local(package / identity["path"])
        if (
            not path.is_relative_to(package)
            or hashlib.sha256(path.read_bytes()).hexdigest() != identity["sha256"]
        ):
            raise ValueError(f"controlled fixture input identity differs: {path}")
        return path

    obj = output.with_suffix(".o")
    execute(
        [
            str(compiler),
            "--target=" + selected["triple"],
            "-O0",
            "-nostdinc",
            "-fno-stack-protector",
            "-c",
            str(ROOT / "testdata/native_toolchain/control.c"),
            "-o",
            str(obj),
        ],
        env,
        log,
    )
    arguments = []
    for item in selected["link_args"]:
        if item == "{object}":
            arguments.append(str(obj))
        elif item == "{output}":
            arguments.append(str(output))
        elif item == "{runtime}":
            continue
        elif item.startswith("{input:") and item.endswith("}"):
            arguments.append(str(resolve(selected["inputs"][item[7:-1]])))
        else:
            arguments.append(item)
    execute([str(linker), *arguments], env, log.with_name("control-link.log"))


def qualify(
    preparation: Path,
    package: Path,
    matrix: Path,
    work: Path,
    go: str,
    pattern: str | None = None,
    timeout: int = 7200,
    accelerator: str = "tcg",
) -> dict:
    started = time.perf_counter()
    preparation, package, matrix, work = map(
        local, (preparation, package, matrix, work)
    )
    if work.exists():
        raise ValueError(f"qualification suite work must be fresh: {work}")
    env = environment()
    shared = shared_inputs()
    work.mkdir(parents=True)
    inputs = work / "inputs"
    inputs.mkdir()
    for name, arguments, log_name in (
        ("integration.test", ["test", "-c", "./tests/integration"], "test-build.log"),
        ("fileprobe", ["build", "./testdata/fileprobe"], "probe-build.log"),
        ("lo-development", ["build", "./cmd/lo"], "development-build.log"),
    ):
        destination = inputs / name
        if shared:
            os.link(shared[name], destination)
        else:
            execute([go, *arguments[:-1], "-o", str(destination), arguments[-1]], env, work / log_name)
    test_binary, probe, development = (inputs / name for name in ("integration.test", "fileprobe", "lo-development"))
    controlled = inputs / "native-toolchain"
    control_fixture(preparation, package, controlled, env, work / "control-build.log")
    identities = {
        name: {
            "path": path.name,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        }
        for name, path in (
            ("fileprobe", probe),
            ("development-compiler", development),
            ("native-toolchain", controlled),
        )
    }
    (inputs / "fixtures.json").write_text(json.dumps(identities, indent=2) + "\n")
    payloads = source_payloads(matrix)
    guest_inputs = "/workspace/" + inputs.relative_to(ROOT).as_posix()
    payloads.append({"source": str(inputs), "path": guest_inputs})
    guest_package = "/workspace/" + package.relative_to(ROOT).as_posix()
    guest_manifest = guest_package + "/toolchain.json"
    selected_pattern = pattern or test_pattern(matrix, "linux/arm64")
    case = {
        "id": "semantic-linux-arm64",
        "argv": [
            guest_inputs + "/integration.test",
            "-test.v=test2json",
            "-test.count=1",
            "-test.failfast",
            "-test.timeout=" + str(timeout) + "s",
            "-test.run=" + selected_pattern,
        ],
        "cwd": "/workspace/tests/integration",
        "environment": {
            "LO_TEST_COMPILER": guest_package + "/bin/lo",
            "LO_LLVM_TOOLCHAIN": guest_manifest,
            "LO_SEMANTIC_MATRIX": "/workspace/" + matrix.relative_to(ROOT).as_posix(),
            "LO_TEST_PREBUILT_FIXTURES": guest_inputs,
            "LO_TEST_SESSION_LOCAL": "1",
            "LO_TEST_SESSION_DIRECTORY": GUEST_STATE,
        },
        "stdin": "",
        "timeout_seconds": timeout,
        "outputs": [],
    }
    listing_case = {**case, "id": "semantic-linux-inventory",
                    "argv": [guest_inputs + "/integration.test", "-test.list=" + selected_pattern.split("/", 1)[0]],
                    "environment": {name: value for name, value in case["environment"].items()
                                    if name not in {"LO_TEST_SESSION_LOCAL", "LO_TEST_SESSION_DIRECTORY"}}}
    manifest = work / "batch.json"
    manifest.write_text(
        json.dumps({"files": payloads, "cases": [listing_case, case]}, indent=2) + "\n"
    )
    host_preparation_seconds = time.perf_counter() - started
    report = run(
        preparation, package, manifest, work / "guest", guest_package, accelerator
    )
    if report["cases"][0]["status"] != 0:
        raise ValueError(case_failure(work, 0, report["cases"][0]))
    expected = tuple(line for line in (work / "guest/0.stdout").read_text().splitlines()
                     if re.fullmatch(r"(?:Test|Fuzz)[A-Za-z0-9_]+", line))
    if not expected:
        raise ValueError("Linux semantic inventory selected no tests")
    stdout = work / "guest/1.stdout"
    events = work / "semantic-linux-arm64.jsonl"
    with stdout.open("rb") as source, events.open("wb") as output:
        result = subprocess.run(
            [go, "tool", "test2json", "-p", PACKAGE, "-t"],
            cwd=ROOT,
            env=env,
            stdin=source,
            stdout=output,
            stderr=subprocess.PIPE,
            check=False,
        )
    if result.returncode:
        failed = case_failure(work, 1, report["cases"][1]) if report["cases"][1]["status"] else ""
        raise ValueError(
            f'Linux semantic event conversion failed: {bounded(result.stderr.decode(errors="replace"))}\n{failed}'
        )
    report["events"] = str(events.relative_to(ROOT))
    if report["cases"][1]["status"] != 0:
        raise ValueError(case_failure(work, 1, report["cases"][1], events))
    matrix_data = json.loads(matrix.read_text())
    references = {reference for row in matrix_data["contracts"] if "linux/arm64" in row["targets"]
                  for reference in row["tests"] if reference.split("/", 1)[0] in expected}
    if pattern is not None:
        components = pattern.split("/")
        references = {reference for reference in references
                      if all(index < len(reference.split("/")) and re.search(part, reference.split("/")[index])
                             for index, part in enumerate(components))}
    report["inventory"] = validate_events(events, expected, tuple(sorted(references)))
    report["test_binary_sha256"] = hashlib.sha256(test_binary.read_bytes()).hexdigest()
    report["fixture_identities"] = identities
    report["pattern"] = selected_pattern
    report["suite_host_preparation_seconds"] = host_preparation_seconds
    report["suite_wall_seconds"] = time.perf_counter() - started
    (work / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("preparation", "package", "matrix", "work"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--go", default="go")
    parser.add_argument("--pattern")
    parser.add_argument("--timeout", type=int, default=7200)
    parser.add_argument("--accelerator", choices=("tcg", "hvf"), default="tcg")
    args = parser.parse_args()
    try:
        if not 1 <= args.timeout <= 7200:
            raise ValueError("suite timeout must be 1..7200 seconds")
        report = qualify(
            args.preparation,
            args.package,
            args.matrix,
            args.work,
            args.go,
            args.pattern,
            args.timeout,
            args.accelerator,
        )
        print(json.dumps(report))
        return 0 if all(case["status"] == 0 for case in report["cases"]) else 1
    except (ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(f"Linux qualification suite: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
