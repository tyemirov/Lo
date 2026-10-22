"""Filesystem contracts for source-bound benchmark producer identities."""
from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
IDENTITY = ROOT / 'tools/nativebench/identity.py'


def load_identity():
    spec = importlib.util.spec_from_file_location('lo_benchmark_identity', IDENTITY)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


@pytest.fixture
def installed(tmp_path: Path) -> Path:
    assert tmp_path.resolve().is_relative_to(ROOT / '_scratch')
    package = tmp_path / 'installed'
    for name in ('bin/lo', 'runtime/liblo.a', 'inventory.json', 'provenance.json'):
        path = package / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('controlled ' + name)
    runtime = package / 'runtime/liblo.a'
    (package / 'toolchain.json').write_text(json.dumps({'host': 'darwin/arm64', 'targets': {
        'darwin/arm64': {'runtime': {'path': 'runtime/liblo.a', 'sha256': sha(runtime)}}}}))
    return package


def arguments(package: Path) -> dict:
    return {'root': ROOT, 'compiler': package / 'bin/lo', 'manifest': package / 'toolchain.json',
            'target': 'darwin/arm64',
            'environment': {'system': 'controlled system', 'machine': 'arm64',
                            'processor': 'controlled processor', 'execution': 'native'},
            'accelerator': 'none', 'producer_kind': 'darwin-native',
            'producer_tools': {'python': 'controlled Python'},
            'producer_sources': ('scripts/qualify_native.py', 'tools/nativebench/identity.py'),
            'reference_go': 'controlled Go compiler', 'samples': 10}


def test_identity_binds_actual_package_and_all_source_boundaries(installed: Path) -> None:
    identity = load_identity().merge_identity(**arguments(installed))
    assert set(identity) == {'target', 'environment', 'accelerator', 'package', 'source',
                             'producer', 'reference', 'harness', 'evaluator', 'measurement'}
    for field, path in {'compiler': 'bin/lo', 'manifest': 'toolchain.json', 'runtime': 'runtime/liblo.a',
                        'inventory': 'inventory.json', 'provenance': 'provenance.json'}.items():
        assert identity['package'][field + '_sha256'] == sha(installed / path)
    assert len(identity['source']['semantic_source_sha256']) == 64
    assert identity['producer']['source_identity']['tools/nativebench/identity.py'] == sha(IDENTITY)
    assert 'internal/eval/eval.go' in identity['reference']['source_identity']
    assert set(identity['harness']) == {'tools/nativebench/main.go', 'tools/nativebench/harness.c.txt',
                                       'tools/nativebench/allocation.c.txt'}
    assert identity['evaluator'] == {'tools/nativebench/qualification.py': sha(ROOT / 'tools/nativebench/qualification.py')}
    assert identity['measurement']['samples'] == 10
    assert identity['measurement']['limits']['build_seconds'] == 180
    assert identity['measurement']['timer_contract']['batch_calibration'] == (
        'untimed doubling of batch size from 1 through 64 until at least 1 ms; then at least 200 ms or 1000000 iterations; elapsed divided by completed iterations; no subtraction')


@pytest.mark.parametrize('subject', ['compiler', 'manifest', 'runtime', 'inventory', 'provenance'])
def test_identity_changes_with_each_installed_input(installed: Path, subject: str) -> None:
    module = load_identity()
    before = module.merge_identity(**arguments(installed))
    path = installed / {'compiler': 'bin/lo', 'manifest': 'toolchain.json', 'runtime': 'runtime/liblo.a',
                        'inventory': 'inventory.json', 'provenance': 'provenance.json'}[subject]
    if subject == 'manifest':
        record = json.loads(path.read_text())
        record['changed'] = True
        path.write_text(json.dumps(record))
    elif subject == 'runtime':
        path.write_text('changed runtime')
        manifest = installed / 'toolchain.json'
        record = json.loads(manifest.read_text())
        record['targets']['darwin/arm64']['runtime']['sha256'] = sha(path)
        manifest.write_text(json.dumps(record))
    else:
        path.write_text('changed input')
    after = module.merge_identity(**arguments(installed))
    assert after['package'][subject + '_sha256'] != before['package'][subject + '_sha256']


def test_identity_rejects_missing_package_identity_instead_of_inference(installed: Path) -> None:
    (installed / 'inventory.json').unlink()
    with pytest.raises(ValueError, match='inventory'):
        load_identity().merge_identity(**arguments(installed))


def test_identity_marks_development_toolchain_unavailable_for_merge(installed: Path) -> None:
    args = arguments(installed)
    args['compiler'] = ROOT / 'bin/lo'
    module = load_identity()
    with pytest.raises(module.DiagnosticCompiler, match='installed'):
        module.merge_identity(**args)


def test_identity_rejects_runtime_digest_mismatch(installed: Path) -> None:
    (installed / 'runtime/liblo.a').write_text('corrupt')
    with pytest.raises(ValueError, match='runtime'):
        load_identity().merge_identity(**arguments(installed))


def test_identity_rejects_target_environment_disagreement(installed: Path) -> None:
    args = arguments(installed)
    args['environment'] = {**args['environment'], 'machine': 'x86_64'}
    with pytest.raises(ValueError, match='environment'):
        load_identity().merge_identity(**args)


def test_identity_rejects_sources_changed_during_measurement(installed: Path) -> None:
    module = load_identity()
    identity = module.merge_identity(**arguments(installed))
    witness = installed / 'measurement-source.txt'
    witness.write_text('before')
    name = witness.relative_to(ROOT).as_posix()
    identity['producer']['source_identity'][name] = sha(witness)
    witness.write_text('after')
    with pytest.raises(ValueError, match='source changed'):
        module.verify_source_identity(ROOT, identity)


def test_identity_rejects_preliminary_installed_package(installed: Path) -> None:
    (installed / 'preliminary.json').write_text('{"reason":"unqualified runtime"}')
    with pytest.raises(ValueError, match='preliminary'):
        load_identity().merge_identity(**arguments(installed))


@pytest.mark.parametrize('subject', ['compiler', 'manifest', 'runtime', 'inventory', 'provenance', 'tool'])
def test_frozen_provenance_rejects_input_changes(installed: Path, subject: str) -> None:
    module = load_identity()
    tool = installed / 'producer-tool'
    tool.write_text('original producer tool')
    snapshot = module.capture_provenance(
        ROOT, installed / 'bin/lo', installed / 'toolchain.json', 'darwin/arm64',
        ('scripts/qualify_native.py',), {'go': tool})
    path = installed / {'compiler': 'bin/lo', 'manifest': 'toolchain.json', 'runtime': 'runtime/liblo.a',
                        'inventory': 'inventory.json', 'provenance': 'provenance.json', 'tool': 'producer-tool'}[subject]
    path.write_text('changed input during preparation or measurement')
    with pytest.raises(ValueError, match='changed'):
        module.verify_provenance(ROOT, snapshot)


def test_frozen_provenance_rejects_new_preliminary_marker(installed: Path) -> None:
    module = load_identity()
    snapshot = module.capture_provenance(
        ROOT, installed / 'bin/lo', installed / 'toolchain.json', 'darwin/arm64',
        ('scripts/qualify_native.py',), {})
    (installed / 'preliminary.json').write_text('{}')
    with pytest.raises(ValueError, match='preliminary'):
        module.verify_provenance(ROOT, snapshot)


def test_native_public_producer_rejects_preliminary_before_preparation(installed: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.syspath_prepend(str(ROOT / 'scripts'))
    import qualify_native
    (installed / 'preliminary.json').write_text('{}')
    monkeypatch.setattr(sys, 'argv', ['qualify_native.py', '--binary', str(installed / 'bin/lo'),
                                    '--workdir', str(installed.parent / 'native-run'), '--lane', 'process'])
    def unexpected_capture(*args, **kwargs):
        raise AssertionError('producer invoked a tool before preliminary admission')
    monkeypatch.setattr(qualify_native, 'capture', unexpected_capture)
    with pytest.raises(ValueError, match='preliminary'):
        qualify_native.main()


def test_linux_public_preparation_freezes_inputs_before_generators(installed: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.syspath_prepend(str(ROOT / 'tools/loqualification'))
    spec = importlib.util.spec_from_file_location('lo_performance_provenance_test', ROOT / 'tools/loqualification/performance.py')
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    preparation = installed.parent / 'preparation'
    preparation.mkdir()
    (preparation / 'tools.json').write_text(json.dumps({'tools': {'clang': {}, 'ld.lld': {}}}))
    monkeypatch.setattr(module, 'verified', lambda record: installed / 'bin/lo')
    invoked = []
    def freeze(*args, **kwargs):
        invoked.append('freeze')
        assert not (installed.parent / 'linux-run').exists()
        raise ValueError('controlled frozen input rejection')
    def execute(*args, **kwargs):
        invoked.append('execute')
        raise AssertionError('generator ran before frozen admission')
    monkeypatch.setattr(module, 'capture_provenance', freeze)
    monkeypatch.setattr(module, 'execute', execute)
    inputs = module.Inputs(preparation, installed, installed / 'bin/lo', installed.parent / 'linux-run',
                           'go', 10, ('process',), ('startup',))
    with pytest.raises(ValueError, match='controlled frozen input rejection'):
        module.prepare(inputs)
    assert invoked == ['freeze']
