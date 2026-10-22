"""Canonical Metal asset admission through the native producer boundary."""
from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path
import pytest

ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture
def assets(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    assert tmp_path.resolve().is_relative_to(ROOT / '_scratch')
    monkeypatch.syspath_prepend(str(ROOT / 'tools/nativeruntime'))
    spec = importlib.util.spec_from_file_location('lo_metal_asset_build', ROOT / 'tools/nativeruntime/build.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    monkeypatch.setattr(module, 'ROOT', tmp_path)
    files = {}
    for name in module.METAL_FILES:
        path = tmp_path / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(('controlled asset ' + name).encode())
        files[name] = hashlib.sha256(path.read_bytes()).hexdigest()
    identity = {'profile': 'metal-fp32', 'xcode': 'controlled xcode', 'sdk': '27.0',
                'clang': 'controlled clang', 'metal_compiler': 'controlled metal',
                'kernel_linker': 'controlled linker', 'deployment_target': '27.0',
                'kernel_flags': [], 'kernel_sha256': files[module.METAL_LIBRARY]}
    manifest = tmp_path / module.METAL_MANIFEST
    manifest.write_text(json.dumps({'identity': identity, 'files': files}))
    source_digests = dict(files)
    source_digests[module.METAL_MANIFEST] = hashlib.sha256(manifest.read_bytes()).hexdigest()
    receipt = tmp_path / 'bin/lo-metal-build.json'
    receipt.parent.mkdir(parents=True)
    receipt.write_text(json.dumps({'metallib_sha256': files[module.METAL_LIBRARY],
                                   'source_sha256': source_digests}))
    return module, tmp_path, receipt


def test_metal_assets_admit_direct_kernel_without_source_archive(assets) -> None:
    module, directory, _ = assets
    library, identity, provenance = module.metal_assets()
    assert library == (directory / module.METAL_LIBRARY).read_bytes()
    assert identity['kernel_sha256'] == hashlib.sha256(library).hexdigest()
    assert set(provenance) == {'manifest', 'producer', 'sources'}
    assert set(provenance['sources']) == module.METAL_FILES | {module.METAL_MANIFEST}
    assert not list(directory.rglob('*.zip'))
    assert not (directory / 'internal/runtimebundle').exists()


@pytest.mark.parametrize('subject', ['kernel', 'source', 'manifest', 'receipt'])
def test_metal_assets_reject_changed_producer_inputs(assets, subject: str) -> None:
    module, directory, receipt = assets
    if subject == 'receipt':
        record = json.loads(receipt.read_text())
        record['metallib_sha256'] = '0' * 64
        receipt.write_text(json.dumps(record))
    else:
        name = {'kernel': module.METAL_LIBRARY, 'source': 'runtime/metal/kernels.metal',
                'manifest': module.METAL_MANIFEST}[subject]
        path = directory / name
        if subject == 'manifest':
            record = json.loads(path.read_text())
            record['identity']['xcode'] = 'changed'
            path.write_text(json.dumps(record))
        else:
            path.write_bytes(b'changed')
    with pytest.raises(ValueError, match='identity differs'):
        module.metal_assets()


@pytest.mark.parametrize('obsolete', ['archive', 'go'])
def test_metal_assets_reject_obsolete_archive_inventory(assets, obsolete: str) -> None:
    module, directory, _ = assets
    manifest = directory / module.METAL_MANIFEST
    record = json.loads(manifest.read_text())
    if obsolete == 'archive':
        record['files']['internal/runtimebundle/metal.zip'] = '0' * 64
    else:
        record['identity']['go'] = 'obsolete source generator'
    manifest.write_text(json.dumps(record))
    with pytest.raises(ValueError, match='incorrect identity or source set'):
        module.metal_assets()
