"""Public preparation CLI integration tests."""
from __future__ import annotations

import os
from pathlib import Path
import shlex
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
CLI = ROOT / 'tools/nativeruntime/prepare.py'
STATE = ROOT / '_scratch/i007-i008-20261020/preparation/test-missing'


def test_default_make_preparation_includes_linux_platform() -> None:
    result = subprocess.run(['make', '--no-print-directory', '-n', 'prepare-native-runtime'],
                            cwd=ROOT, env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    assert result.returncode == 0, result.stderr
    command, = [shlex.split(line) for line in result.stdout.splitlines()
                if 'tools/nativeruntime/prepare.py' in line]
    assert '--download' in command
    assert '--platform' in command


def invoke(*arguments: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, '-B', str(CLI), *arguments], cwd=ROOT,
                          env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                          capture_output=True, text=True, check=False)


def test_missing_archives_require_explicit_download() -> None:
    result = invoke('--state', str(STATE))
    assert result.returncode == 1
    assert 'requires --download' in result.stderr


def test_state_must_be_in_scratch() -> None:
    result = invoke('--state', str(ROOT / 'tools/invalid-state'))
    assert result.returncode == 1
    assert 'preparation state must be under Lo/_scratch' in result.stderr


def test_runtime_build_requires_prepared_compiler() -> None:
    result = subprocess.run([sys.executable, '-B', str(CLI.with_name('build.py')),
                             '--preparation', str(STATE), '--output', str(STATE / 'distribution')],
                            cwd=ROOT, env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    assert result.returncode == 1
    assert 'missing prepared Rust receipt' in result.stderr


def test_runtime_build_uses_prepared_host_linker() -> None:
    import json
    import pytest

    supplied = os.environ.get('LO_NATIVE_PREPARATION')
    if supplied is None:
        pytest.skip('LO_NATIVE_PREPARATION selects the prepared compiler inputs')
    state = (ROOT / supplied).resolve()
    assert state.is_relative_to(ROOT / '_scratch')
    result = subprocess.run([sys.executable, '-B', str(CLI.with_name('build.py')),
                             '--preparation', str(state), '--output', str(state / 'distribution')],
                            cwd=ROOT, env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    assert result.returncode == 0, result.stderr
    manifest = json.loads((state / 'distribution/toolchain.json').read_text())
    assert set(manifest['targets']) == {'darwin/arm64', 'linux/arm64'}
    receipt = json.loads((state / 'distribution/build-receipt.json').read_text())
    environment = receipt['cargo_environment']
    assert environment['CARGO_TARGET_AARCH64_APPLE_DARWIN_LINKER'] == str(state / 'host-linker/linker')
    assert environment['SDKROOT'] == str(state / 'host-linker/sdk')
    assert receipt['host_linker']['source']['path'] == 'tools/nativeruntime/host_linker.c'


def test_guest_runner_rejects_missing_executable() -> None:
    result = subprocess.run([sys.executable, '-B', str(CLI.with_name('run.py')),
                             '--preparation', str(STATE), str(STATE / 'missing')],
                            cwd=ROOT, env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    assert result.returncode == 1
    assert 'missing guest executable' in result.stderr


def test_software_guest_preserves_process_contract() -> None:
    import json
    import tempfile
    import pytest

    supplied = os.environ.get('LO_NATIVE_PREPARATION')
    if supplied is None:
        pytest.skip('LO_NATIVE_PREPARATION selects the prepared software guest')
    state = (ROOT / supplied).resolve()
    assert state.is_relative_to(ROOT / '_scratch')
    distribution = state / 'distribution'
    record = json.loads((distribution / 'toolchain.json').read_text())
    target = record['targets']['linux/arm64']
    with tempfile.TemporaryDirectory(prefix='process-contract-', dir=state) as temporary:
        work = Path(temporary)
        object_path = work / 'probe.o'
        executable = work / 'probe'
        compile_result = subprocess.run([str(distribution / record['compiler']['path']),
                                         '--target=aarch64-unknown-linux-gnu', '-c',
                                         str(CLI.with_name('guest_probe.c')), '-o', str(object_path)],
                                        cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, check=False)
        assert compile_result.returncode == 0, compile_result.stderr
        values = {'{object}': str(object_path), '{runtime}': str(distribution / target['runtime']['path']),
                  '{output}': str(executable),
                  **{'{input:' + name + '}': str(distribution / pin['path'])
                     for name, pin in target['inputs'].items()}}
        linked = subprocess.run([str(distribution / target['linker']['path']),
                                 *[values.get(arg, arg) for arg in target['link_args']]],
                                cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, check=False)
        assert linked.returncode == 0, linked.stderr
        result = subprocess.run([sys.executable, '-B', str(CLI.with_name('run.py')),
                                 '--preparation', str(state), str(executable), 'ARG'],
                                cwd=ROOT, input=b'stdin\n', capture_output=True, timeout=120, check=False)
        assert result.returncode == 9, result.stderr
        assert result.stdout == b'stdout\nARGstdin\n'
        assert result.stderr == b'stderr\n'


def test_native_import_scan_uses_native_archive_symbols() -> None:
    import json
    import pytest

    supplied = os.environ.get('LO_NATIVE_PREPARATION')
    if supplied is None:
        pytest.skip('LO_NATIVE_PREPARATION selects the native runtime archive')
    state = (ROOT / supplied).resolve()
    assert state.is_relative_to(ROOT / '_scratch')
    result = subprocess.run([sys.executable, '-B', str(CLI.with_name('discover_imports.py')),
                             '--nm', str(state / 'tools/bin/llvm-nm'), '--archive',
                             str(state / 'distribution/runtime/aarch64-apple-darwin/liblo_runtime.a')],
                            cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False)
    assert result.returncode == 0, result.stderr
    record = json.loads(result.stdout)
    assert {'__Unwind_Resume', '__tlv_bootstrap', '_bzero'} <= set(record['symbols'])
    assert set(record['program_symbols']) == {'_lo_entry', '_lo_program_flow_count', '_lo_program_flows',
                                               '_lo_program_metadata', '_lo_program_metadata_size'}


def test_corrupt_pinned_archive_is_rejected() -> None:
    import tempfile

    STATE.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='corrupt-archive-', dir=STATE.parent) as temporary:
        state = Path(temporary)
        (state / 'rustc.tar.xz').write_bytes(b'not the pinned compiler')
        result = invoke('--state', str(state), '--rust-only')
        assert result.returncode == 1
        assert 'archive does not match pin: rustc.tar.xz' in result.stderr


def test_preparation_reuses_frozen_compiler_files() -> None:
    import json
    import pytest

    supplied = os.environ.get('LO_NATIVE_PREPARATION')
    if supplied is None:
        pytest.skip('LO_NATIVE_PREPARATION selects the frozen compiler prefix')
    state = (ROOT / supplied).resolve()
    assert state.is_relative_to(ROOT / '_scratch')
    record = json.loads((state / 'rust.json').read_text())
    compiler = ROOT / record['rustc']['path']
    stdlib = next((ROOT / record['prefix'] / 'lib/rustlib/aarch64-apple-darwin/lib').glob('liballoc-*.rlib'))
    before = {path: path.stat().st_mtime_ns for path in (compiler, stdlib)}
    result = invoke('--state', str(state), '--rust-only')
    assert result.returncode == 0, result.stderr
    assert {path: path.stat().st_mtime_ns for path in before} == before


def test_runtime_build_rejects_undeclared_cargo_controls() -> None:
    wrapper = str(ROOT / '_scratch/i007-i008-20261020/core/undeclared-wrapper')
    controls = {'RUSTFLAGS': '-Copt-level=3', 'CARGO_ENCODED_RUSTFLAGS': '-Copt-level=3',
                'RUSTC_WRAPPER': wrapper, 'RUSTC_WORKSPACE_WRAPPER': wrapper,
                'CARGO_BUILD_RUSTFLAGS': '-Copt-level=3', 'CARGO_PROFILE_RELEASE_OPT_LEVEL': '3',
                'CARGO_PROFILE_RELEASE_PANIC': 'unwind', 'CARGO_PROFILE_RELEASE_LTO': 'true',
                'CARGO_PROFILE_RELEASE_BUILD_OVERRIDE_OPT_LEVEL': '3'}
    baseline = {name: value for name, value in os.environ.items()
                if name not in controls and not name.startswith('CARGO_PROFILE_')}
    for name, value in controls.items():
        environment = dict(baseline, CI='1')
        environment[name] = value
        result = subprocess.run([sys.executable, '-B', str(CLI.with_name('build.py')),
                                 '--preparation', str(STATE), '--output', str(STATE / 'distribution')],
                                cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        assert result.returncode == 1
        assert f'undeclared Cargo build control: {name}' in result.stderr
