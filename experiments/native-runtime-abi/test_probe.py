"""Public CLI checks for source-bound runtime archive preparation."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import textwrap

import pytest

ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture
def workspace(tmp_path: Path) -> Path:
    assert tmp_path.resolve().is_relative_to(ROOT)
    for name in ("native-runtime-abi", "native-backends"):
        directory = tmp_path / "experiments" / name
        directory.mkdir(parents=True)
        shutil.copyfile(ROOT / "experiments" / name / "probe.py", directory / "probe.py")
    for name in ("prepare.py", "client.c", "expected.txt", "platform.c", "libSystem.tbd",
                 "Foundation.tbd", "Metal.tbd", "libobjc.tbd"):
        shutil.copyfile(ROOT / "experiments/native-runtime-abi" / name,
                        tmp_path / "experiments/native-runtime-abi" / name)
    for name in ("probe.ll", "runtime.c", "isolation_probe.c", "runner.rs", "guest_init.c",
                 "cranelift/Cargo.toml", "cranelift/Cargo.lock", "cranelift/src/main.rs"):
        target = tmp_path / "experiments/native-backends" / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / "experiments/native-backends" / name, target)
    source = tmp_path / "experiments/native-runtime-abi/runtime.rs"
    shutil.copyfile(ROOT / "experiments/native-runtime-abi/runtime.rs", source)
    archive = tmp_path / "runtime.a"
    archive.write_bytes(b"fixture archive")
    digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    compiler = tmp_path / "rust/bin/rustc"
    prefix = tmp_path / "rust"
    record = {"schema": 1, "runtime_source": {"path": str(source.relative_to(tmp_path)), "sha256": digest(source)},
              "compiler": {"path": "rust/bin/rustc", "sha256": "1" * 64, "identity": "rustc fixture",
                           "sysroot": {"path": "rust", "sha256": "2" * 64}}, "libraries": {}}
    for target in ("aarch64-apple-darwin", "aarch64-unknown-linux-gnu"):
        command = [str(compiler), "--edition=2021", "--crate-name", "lo_abi_probe", "--crate-type", "staticlib",
                   "--sysroot", str(prefix), "--target", target, "-C", "panic=abort", "-C", "opt-level=2",
                   "-C", "debuginfo=2", "--remap-path-prefix", f"{tmp_path}=/lo", str(source), "-o", str(archive)]
        record["libraries"][target] = {"archive": {"path": "runtime.a", "sha256": digest(archive)}, "command": command}
    (tmp_path / "libraries.json").write_text(json.dumps(record))
    (tmp_path / "_scratch").mkdir()
    return tmp_path


def invoke(workspace: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, "-B", str(workspace / "experiments/native-runtime-abi/probe.py"),
                           "--verify-only", "--runtime-libraries", "libraries.json"], cwd=workspace,
                          stdin=subprocess.DEVNULL, capture_output=True, text=True,
                          env={"PATH": "", "HOME": str(workspace), "TMPDIR": str(workspace)})


def test_archive_provenance_does_not_require_preparation_compiler(workspace: Path) -> None:
    process = invoke(workspace)
    assert process.returncode == 0, process.stderr
    assert json.loads(process.stdout) == {"runtime_preparation": "verified"}


@pytest.mark.parametrize("artifact", ["experiments/native-runtime-abi/runtime.rs", "runtime.a"])
def test_stale_preparation_is_rejected(workspace: Path, artifact: str) -> None:
    with (workspace / artifact).open("ab") as stream:
        stream.write(b"changed")
    process = invoke(workspace)
    assert process.returncode == 1
    assert "SHA-256 mismatch" in process.stderr


@pytest.mark.parametrize("change", ["panic", "target"])
def test_different_compile_contract_is_rejected(workspace: Path, change: str) -> None:
    path = workspace / "libraries.json"
    record = json.loads(path.read_text())
    command = record["libraries"]["aarch64-apple-darwin"]["command"]
    command[command.index("panic=abort" if change == "panic" else "aarch64-apple-darwin")] = "wrong"
    path.write_text(json.dumps(record))
    process = invoke(workspace)
    assert process.returncode == 1
    assert "preparation command mismatch" in process.stderr


def run_cli(workspace: Path, module: str, arguments: list[str], injection: str = "") -> subprocess.CompletedProcess[str]:
    script = (f"import sys\nsys.path.insert(0, {str(workspace / 'experiments/native-runtime-abi')!r})\n"
              f"import {module} as application\n" + textwrap.dedent(injection) +
              f"\nsys.argv = [{module!r}, *{arguments!r}]\nsys.exit(application.main())\n")
    return subprocess.run([sys.executable, "-B", "-c", script], cwd=workspace,
                          stdin=subprocess.DEVNULL, capture_output=True, text=True,
                          env={"PATH": "", "HOME": str(workspace), "TMPDIR": str(workspace)})


@pytest.fixture
def run_inputs(workspace: Path) -> Path:
    bundle = workspace / "inputs/bundle"
    bundle.mkdir(parents=True)
    tool = bundle / "fixture-tool"
    tool.write_text("declared fixture; not an executable\n")
    sysroot = workspace / "inputs/sysroot"
    sysroot.mkdir()
    (sysroot / "fixture").write_text("fixture sysroot\n")
    digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    def tree(path: Path) -> str:
        files = {str(p.relative_to(path)): digest(p) for p in sorted(path.rglob("*")) if p.is_file()}
        return hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    pin = {"path": "inputs/bundle/fixture-tool", "sha256": digest(tool), "version": "fixture"}
    bundle_pin = {"path": "inputs/bundle", "sha256": tree(bundle)}
    target = {"bundle": bundle_pin, "sysroot": {"path": "inputs/sysroot", "sha256": tree(sysroot)},
              **{name: pin for name in ("llvm", "cranelift", "cc", "linker")},
              "runner": dict(pin, contract="executable,sysroot")}
    manifest = {"isolation": {"kind": "sandbox-exec", "bundle": bundle_pin, "tool": pin,
                              "probe": dict(pin, contract="readonly-sentinel,state-directory")},
                "targets": {target_name: target for target_name in
                            ("aarch64-apple-darwin", "aarch64-unknown-linux-gnu")}}
    (workspace / "inputs.json").write_text(json.dumps(manifest))
    return workspace


@pytest.mark.parametrize("manifest", ["inputs", "libraries"])
def test_run_rejects_result_input_overlap_without_deleting_input(run_inputs: Path, manifest: str) -> None:
    state = run_inputs / "_scratch/native-runtime-abi"
    state.mkdir()
    result = state / "result.json"
    original = (run_inputs / f"{manifest}.json").read_bytes()
    result.write_bytes(original)
    args = ["--inputs", str(result.relative_to(run_inputs)) if manifest == "inputs" else "inputs.json",
            "--runtime-libraries", str(result.relative_to(run_inputs)) if manifest == "libraries" else "libraries.json"]
    process = run_cli(run_inputs, "probe", args)
    assert process.returncode == 1
    assert "overlap writable state" in process.stderr
    assert result.read_bytes() == original


@pytest.mark.parametrize("relative", ["readonly-sentinel", "execution/guard-write"])
def test_run_rejects_guard_symlink_before_write(run_inputs: Path, relative: str) -> None:
    state = run_inputs / "_scratch/native-runtime-abi"
    target = run_inputs / "protected-fixture"
    target.write_text("unchanged\n")
    link = state / relative
    link.parent.mkdir(parents=True)
    link.symlink_to(target)
    process = run_cli(run_inputs, "probe", ["--inputs", "inputs.json", "--runtime-libraries", "libraries.json"])
    assert process.returncode == 1
    assert "symlink" in process.stderr
    assert target.read_text() == "unchanged\n"


def test_run_validates_maintained_source_before_read(run_inputs: Path) -> None:
    source = run_inputs / "experiments/native-runtime-abi/client.c"
    protected = run_inputs / "protected-client"
    protected.write_text("fixture source\n")
    source.unlink()
    source.symlink_to(protected)
    process = run_cli(run_inputs, "probe", ["--inputs", "inputs.json", "--runtime-libraries", "libraries.json"])
    assert process.returncode == 1
    assert "symlink" in process.stderr
    assert not (run_inputs / "_scratch/native-runtime-abi/result.json").exists()


def test_platform_stages_cannot_reuse_stale_outputs(run_inputs: Path) -> None:
    injection = '''
        from pathlib import Path
        import json, struct
        def artifact(path, target, kind):
            if target == "aarch64-apple-darwin":
                data = struct.pack("<IIIIIIIIII", 0xfeedfacf, 0x0100000c, 0,
                                   1 if kind == "object" else 2, 1, 8, 0, 0, 0x19, 8)
            else:
                data = bytearray(128)
                data[:7] = b"\\x7fELF\\x02\\x01\\x01"
                struct.pack_into("<HHI", data, 16, 1 if kind == "object" else 2, 183, 1)
                struct.pack_into("<H", data, 52, 64)
                if kind == "object":
                    struct.pack_into("<Q", data, 40, 64)
                    struct.pack_into("<HH", data, 58, 64, 1)
                else:
                    struct.pack_into("<Q", data, 32, 64)
                    struct.pack_into("<HH", data, 54, 56, 1)
            path.write_bytes(data)
        directory = application.STATE / "execution/aarch64-apple-darwin"
        directory.mkdir(parents=True)
        artifact(directory / "platform.o", "aarch64-apple-darwin", "object")
        artifact(directory / "platform", "aarch64-apple-darwin", "executable")
        def execute(inputs, state, command, env):
            result = dict(exit_code=0, stdout="", stderr="")
            if len(command) == 3 and command[1].endswith("readonly-sentinel"):
                (state / "guard-write").unlink()
                result["stdout"] = json.dumps(application.backend.GUARD_RESULT)
            elif "-o" in command:
                output = Path(command[command.index("-o") + 1])
                if output.name not in ("platform", "platform.o"):
                    artifact(output, output.parent.name, "object" if output.suffix == ".o" else "executable")
            else:
                result["stdout"] = ("platform foundation 1 objc 1 metal 1 process 1\\n"
                                    if Path(command[1]).name == "platform"
                                    else (application.SOURCE / "expected.txt").read_text())
            return result
        application.backend.execute = execute
    '''
    process = run_cli(run_inputs, "probe", ["--inputs", "inputs.json", "--runtime-libraries", "libraries.json"], injection)
    assert process.returncode == 1
    assert "missing artifact" in process.stderr
    assert json.loads((run_inputs / "_scratch/native-runtime-abi/result.json").read_text())["qualification"] == "failed"


@pytest.fixture
def preparation_inputs(workspace: Path) -> Path:
    compiler = workspace / "rust/bin/rustc"
    compiler.parent.mkdir(parents=True)
    compiler.write_text("fixture compiler\n")
    return workspace


@pytest.mark.parametrize("output", ["rust", "experiments/native-runtime-abi"])
def test_prepare_rejects_output_input_overlap_before_mutation(preparation_inputs: Path, output: str) -> None:
    receipt = preparation_inputs / output / "runtime-libraries.json"
    receipt.write_text("preserve existing receipt\n")
    process = run_cli(preparation_inputs, "prepare", ["--rust-prefix", "rust", "--output", output])
    assert process.returncode == 1
    assert "overlap" in process.stderr
    assert receipt.read_text() == "preserve existing receipt\n"


@pytest.mark.parametrize("relative", ["aarch64-apple-darwin", "aarch64-apple-darwin/liblo_abi_probe.a",
                                      "runtime-libraries.json"])
def test_prepare_rejects_derived_symlink_before_mutation(preparation_inputs: Path, relative: str) -> None:
    output = preparation_inputs / "prepared"
    target = preparation_inputs / "protected"
    target.mkdir()
    (target / "liblo_abi_probe.a").write_text("preserve archive\n")
    link = output / relative
    link.parent.mkdir(parents=True, exist_ok=True)
    link.symlink_to(target if relative == "aarch64-apple-darwin" else target / "liblo_abi_probe.a")
    process = run_cli(preparation_inputs, "prepare", ["--rust-prefix", "rust", "--output", "prepared"])
    assert process.returncode == 1
    assert "symlink" in process.stderr
    assert link.is_symlink()
    assert (target / "liblo_abi_probe.a").read_text() == "preserve archive\n"


@pytest.mark.parametrize("changed", ["compiler", "sysroot"])
def test_prepare_rejects_changed_compiler_snapshot(preparation_inputs: Path, changed: str) -> None:
    injection = f'''
        from pathlib import Path
        import subprocess
        def compile_fixture(command, **kwargs):
            if command[-1] == "-vV":
                return subprocess.CompletedProcess(command, 0, "rustc fixture\\n", "")
            Path(command[-1]).write_bytes(b"fixture archive")
            target = application.ROOT / {('rust/bin/rustc' if changed == 'compiler' else 'rust/changed-stdlib')!r}
            target.write_text("changed compiler input\\n")
            return subprocess.CompletedProcess(command, 0, "", "")
        application.subprocess.run = compile_fixture
    '''
    process = run_cli(preparation_inputs, "prepare", ["--rust-prefix", "rust", "--output", "prepared"], injection)
    assert process.returncode == 1
    assert "changed during preparation" in process.stderr
    assert not (preparation_inputs / "prepared/runtime-libraries.json").exists()


@pytest.mark.parametrize("field,value", [("compiler", 123), ("sysroot", None)])
def test_malformed_provenance_hash_is_a_cli_input_error(workspace: Path, field: str, value: object) -> None:
    path = workspace / "libraries.json"
    record = json.loads(path.read_text())
    subject = record["compiler"] if field == "compiler" else record["compiler"]["sysroot"]
    subject["sha256"] = value
    path.write_text(json.dumps(record))
    process = invoke(workspace)
    assert process.returncode == 1
    assert "runtime compiler provenance is incomplete" in process.stderr
    assert "Traceback" not in process.stderr
