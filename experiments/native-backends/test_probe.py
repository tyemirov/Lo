import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from collections.abc import Iterator
from typing import Any

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'experiments/native-backends/probe.py'
STATE = ROOT / '_scratch/build-state/tmp'
TARGETS = ['aarch64-apple-darwin', 'aarch64-unknown-linux-gnu']


@pytest.fixture
def workspace() -> Iterator[Path]:
    assert STATE.absolute() == STATE.resolve() and STATE.resolve().is_relative_to(ROOT)
    STATE.mkdir(parents=True, exist_ok=True)
    path = Path(tempfile.mkdtemp(dir=STATE))
    source = path / 'experiments/native-backends'
    source.mkdir(parents=True)
    for name in ['probe.py', 'probe.ll', 'runtime.c', 'isolation_probe.c', 'runner.rs', 'guest_init.c']:
        shutil.copyfile(SCRIPT.parent / name, source / name)
    shutil.copytree(SCRIPT.parent / 'cranelift', source / 'cranelift')
    yield path
    shutil.rmtree(path)


def invoke(workspace: Path, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, '-B', str(workspace / 'experiments/native-backends/probe.py'), *args],
                          stdin=subprocess.DEVNULL, capture_output=True, text=True, cwd=workspace,
                          env={'PATH': '', 'PYTHONDONTWRITEBYTECODE': '1', 'HOME': str(workspace),
                               'TMPDIR': str(workspace), 'TMP': str(workspace), 'TEMP': str(workspace)})


def test_check_does_not_requalify_historical_evidence(workspace: Path) -> None:
    result = invoke(workspace, 'check')
    assert result.returncode == 0, result.stderr
    report = json.loads(result.stdout)
    assert report['qualification'] == 'not-run'
    assert report['required_inputs'] == ['isolation', *TARGETS]


def test_run_requires_declared_local_inputs(workspace: Path) -> None:
    result = invoke(workspace, 'run')
    assert result.returncode != 0
    assert '--inputs' in result.stderr


def test_bootstrap_removed(workspace: Path) -> None:
    result = invoke(workspace, 'bootstrap')
    assert result.returncode == 2
    assert 'invalid choice' in result.stderr


def test_prepare_rejects_absolute_input_path(workspace: Path) -> None:
    result = invoke(workspace, 'prepare', '--inputs', str(workspace / 'inputs.json'))
    assert result.returncode == 1
    assert 'repository-relative' in result.stderr


def test_prepare_rejects_symlink_before_read(workspace: Path) -> None:
    (workspace / 'link.json').symlink_to(workspace / 'missing.json')
    result = invoke(workspace, 'prepare', '--inputs', 'link.json')
    assert result.returncode == 1
    assert 'symlink' in result.stderr


def test_prepare_rejects_parent_traversal(workspace: Path) -> None:
    result = invoke(workspace, 'prepare', '--inputs', '../inputs.json')
    assert result.returncode == 1
    assert 'repository-relative' in result.stderr


def test_prepare_rejects_missing_pins(workspace: Path) -> None:
    (workspace / 'inputs.json').write_text('{}')
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'isolation' in result.stderr


def tree_digest(path: Path) -> str:
    entries = {p.relative_to(path).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
               for p in sorted(path.rglob('*')) if p.is_file()}
    return hashlib.sha256(json.dumps(entries, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


@pytest.fixture
def declared_inputs(workspace: Path) -> dict[str, Any]:
    bundle = workspace / 'inputs/toolchain'
    bundle.mkdir(parents=True)
    tool = bundle / 'fixture-tool'
    tool.write_text('fixture input only; never executed\n')
    sysroot = workspace / 'inputs/sysroot'
    sysroot.mkdir()
    (sysroot / 'fixture.h').write_text('/* fixture input only */\n')
    tool_pin = dict(path='inputs/toolchain/fixture-tool', sha256=hashlib.sha256(tool.read_bytes()).hexdigest(), version='fixture-only')
    bundle_pin = dict(path='inputs/toolchain', sha256=tree_digest(bundle))
    sysroot_pin = dict(path='inputs/sysroot', sha256=tree_digest(sysroot))
    target = dict(bundle=bundle_pin, sysroot=sysroot_pin,
                  **{name: tool_pin for name in ['llvm', 'cranelift', 'cc', 'linker']},
                  runner=dict(tool_pin, contract='executable,sysroot'))
    manifest = dict(isolation=dict(kind='sandbox-exec', bundle=bundle_pin, tool=tool_pin,
                                  probe=dict(tool_pin, contract='readonly-sentinel,state-directory')),
                    targets={triple: target for triple in TARGETS})
    (workspace / 'inputs.json').write_text(json.dumps(manifest))
    return manifest


def test_prepare_pins_fixtures_without_backend_qualification(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 0, result.stderr
    assert json.loads(result.stdout)['qualification'] == 'not-run'
    receipt = json.loads((workspace / '_scratch/native-backend-probe/prepared.json').read_text())
    assert receipt['manifest_sha256'] == hashlib.sha256((workspace / 'inputs.json').read_bytes()).hexdigest()
    assert set(receipt['targets']) == set(TARGETS)
    assert json.loads((workspace / '_scratch/native-backend-probe/result.json').read_text())['qualification'] != 'passed'


def test_run_rejects_stale_receipt_before_tool_execution(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    assert invoke(workspace, 'prepare', '--inputs', 'inputs.json').returncode == 0
    receipt = workspace / '_scratch/native-backend-probe/prepared.json'
    receipt.write_text('{}')
    result = invoke(workspace, 'run', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'stale preparation' in result.stderr
    assert json.loads((workspace / '_scratch/native-backend-probe/result.json').read_text())['qualification'] != 'passed'


def test_prepare_rejects_tampered_bundle(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    (workspace / 'inputs/toolchain/fixture-tool').write_text('tampered\n')
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'SHA-256 mismatch' in result.stderr


def test_prepare_rejects_symlink_in_bundle(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    (workspace / 'inputs/toolchain/link').symlink_to(workspace / 'inputs/sysroot')
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'symlink' in result.stderr


def test_prepare_rejects_tool_outside_bundle(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    tool = workspace / 'other-tool'
    tool.write_text('fixture input only\n')
    declared_inputs['targets'][TARGETS[0]]['llvm'] = dict(path='other-tool', sha256=hashlib.sha256(tool.read_bytes()).hexdigest(), version='fixture-only')
    (workspace / 'inputs.json').write_text(json.dumps(declared_inputs))
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'inside its pinned bundle' in result.stderr


def test_prepare_rejects_symlinked_state(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    (workspace / '_scratch').mkdir()
    (workspace / '_scratch/native-backend-probe').symlink_to(workspace / 'inputs/sysroot')
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'symlink' in result.stderr


def test_inspect_artifact_rejects_empty_output(workspace: Path) -> None:
    (workspace / 'empty.o').write_bytes(b'')
    result = invoke(workspace, 'inspect-artifact', '--path', 'empty.o', '--target', TARGETS[0], '--kind', 'object')
    assert result.returncode == 1
    assert 'empty artifact' in result.stderr


def test_inspect_artifact_rejects_wrong_target_format(workspace: Path) -> None:
    (workspace / 'wrong.o').write_bytes(b'not an arm64 object')
    result = invoke(workspace, 'inspect-artifact', '--path', 'wrong.o', '--target', TARGETS[1], '--kind', 'object')
    assert result.returncode == 1
    assert 'ELF' in result.stderr


def test_exit_zero_without_output_cannot_pass_run(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    import importlib.util
    spec = importlib.util.spec_from_file_location('contained_probe', workspace / 'experiments/native-backends/probe.py')
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    inputs = module.Inputs.read('inputs.json')
    module.prepare(inputs)
    execution = workspace / '_scratch/native-backend-probe/execution'
    execution.mkdir()
    (execution / f'llvm-{TARGETS[0]}.o').write_bytes(b'stale object')
    calls = []
    def exit_zero(*args: Any) -> dict[str, Any]:
        calls.append(args)
        if len(calls) == 1:
            (execution / 'guard-write').unlink()
        return dict(exit_code=0, stdout=json.dumps(module.GUARD_RESULT) if len(calls) == 1 else '23 0\n0 7\n', stderr='')
    with pytest.raises(module.InputError, match='missing artifact'):
        module.run(inputs, execute_command=exit_zero)


def test_failed_isolation_probe_blocks_every_backend(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    import importlib.util
    spec = importlib.util.spec_from_file_location('blocked_probe', workspace / 'experiments/native-backends/probe.py')
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    inputs = module.Inputs.read('inputs.json')
    module.prepare(inputs)
    result_path = workspace / '_scratch/native-backend-probe/result.json'
    result_path.write_text(json.dumps({'qualification': 'passed', 'cells': ['stale-success']}))
    calls = []
    def failed_probe(*args: Any) -> dict[str, Any]:
        calls.append(args)
        return dict(exit_code=1, stdout='', stderr='fixture denial probe failure')
    with pytest.raises(module.InputError, match='backend execution blocked'):
        module.run(inputs, execute_command=failed_probe)
    assert len(calls) == 1
    report = json.loads(result_path.read_text())
    assert report['qualification'] == 'failed'
    assert report['cells'] == []
    assert 'backend execution blocked' in report['error']
    assert json.loads((workspace / '_scratch/native-backend-probe/result.json').read_text())['qualification'] != 'passed'


def test_missing_isolation_probe_is_rejected_before_preparation(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    del declared_inputs['isolation']['probe']
    (workspace / 'inputs.json').write_text(json.dumps(declared_inputs))
    result = invoke(workspace, 'prepare', '--inputs', 'inputs.json')
    assert result.returncode == 1
    assert 'probe' in result.stderr
    assert not (workspace / '_scratch/native-backend-probe/prepared.json').exists()


def test_inspect_artifact_rejects_wrong_macho_cpu(workspace: Path) -> None:
    import struct
    data = struct.pack('<IIIIIIIIII', 0xfeedfacf, 0x01000007, 0, 1, 1, 8, 0, 0, 0x19, 8)
    (workspace / 'wrong-cpu.o').write_bytes(data)
    result = invoke(workspace, 'inspect-artifact', '--path', 'wrong-cpu.o', '--target', TARGETS[0], '--kind', 'object')
    assert result.returncode == 1
    assert 'incorrect arm64 target' in result.stderr


def test_inspect_artifact_rejects_wrong_elf_cpu(workspace: Path) -> None:
    import struct
    data = bytearray(128)
    data[:7] = b'\x7fELF\x02\x01\x01'
    struct.pack_into('<HHI', data, 16, 1, 62, 1)
    struct.pack_into('<Q', data, 40, 64)
    struct.pack_into('<HHHHHH', data, 52, 64, 0, 0, 64, 1, 0)
    (workspace / 'wrong-cpu.o').write_bytes(data)
    result = invoke(workspace, 'inspect-artifact', '--path', 'wrong-cpu.o', '--target', TARGETS[1], '--kind', 'object')
    assert result.returncode == 1
    assert 'incorrect arm64 target' in result.stderr


def test_nonzero_backend_stage_persists_failed_result(workspace: Path, declared_inputs: dict[str, Any]) -> None:
    import importlib.util
    spec = importlib.util.spec_from_file_location('nonzero_probe', workspace / 'experiments/native-backends/probe.py')
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    inputs = module.Inputs.read('inputs.json')
    module.prepare(inputs)
    calls = []
    def nonzero_stage(*args: Any) -> dict[str, Any]:
        calls.append(args)
        if len(calls) == 1:
            (workspace / '_scratch/native-backend-probe/execution/guard-write').unlink()
            return dict(exit_code=0, stdout=json.dumps(module.GUARD_RESULT), stderr='')
        return dict(exit_code=17, stdout='', stderr='injected emitter failure')
    assert module.run(inputs, execute_command=nonzero_stage) == 1
    report = json.loads((workspace / '_scratch/native-backend-probe/result.json').read_text())
    assert report['qualification'] == 'failed'
    assert report['cells'][0]['passed'] is False
    assert report['cells'][0]['stages']['object']['exit_code'] == 17
