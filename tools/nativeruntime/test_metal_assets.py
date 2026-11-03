"""Precompiled kernel admission through the public Metal asset CLI."""
from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import pytest

ROOT = Path(__file__).resolve().parents[2]
CLI = ROOT / 'tools/nativeruntime/metal_assets.py'


@pytest.fixture
def assets(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    assert tmp_path.resolve().is_relative_to(ROOT / '_scratch')
    monkeypatch.syspath_prepend(str(CLI.parent))
    spec = importlib.util.spec_from_file_location('lo_kernel_assets', CLI)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    body = b'kernel void controlled() {}\n'
    header = b'typedef uint LoMetalWord;\n'
    current = {module.KERNEL: b'#include <metal_stdlib>\n#include "../native/host/metal/metal_types.h"\n' + body,
               module.HEADER: header}
    original = {module.KERNEL: b'#include <metal_stdlib>\n#include "../../captured/types.h"\n' + body,
                'captured/types.h': header}
    for name, data in current.items():
        path = tmp_path / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    library = tmp_path / module.LIBRARY
    library.write_bytes(b'controlled precompiled kernel')
    record = {'identity': dict(module.IDENTITY, kernel_sha256=module.sha256(library.read_bytes())),
              'sources': {name: module.sha256(data) for name, data in current.items()},
              'translation_unit_sha256': module.sha256(module.expanded_sources(current)),
              'compiled': module.snapshot(original)}
    manifest = tmp_path / module.MANIFEST
    manifest.write_text(json.dumps(record))
    return module, tmp_path, manifest


def invoke(root: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, '-B', str(CLI), 'verify', '--root', str(root)],
                          cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True)


def test_precompiled_kernel_admits_without_go_or_xcode_producer(assets) -> None:
    module, directory, _ = assets
    result = invoke(directory)
    assert result.returncode == 0, result.stderr
    assert json.loads(result.stdout) == {'kernel_sha256': module.sha256((directory / module.LIBRARY).read_bytes()),
                                         'profile': 'metal-fp32'}
    # An obsolete Go interpreter receipt cannot become a current asset dependency.
    receipt = directory / 'bin/lo-metal-build.json'
    receipt.parent.mkdir()
    receipt.write_text('invalid obsolete producer receipt')
    result = invoke(directory)
    assert result.returncode == 0, result.stderr
    assert not (directory / 'internal').exists()


@pytest.mark.parametrize('subject', ['kernel', 'source', 'header', 'compiled-text', 'compiled-hash',
                                    'current-tu', 'compiled-tu', 'flags', 'tool', 'source-set', 'old-shape'])
def test_precompiled_kernel_rejects_drift(assets, subject: str) -> None:
    module, directory, manifest = assets
    record = json.loads(manifest.read_text())
    if subject in {'kernel', 'source', 'header'}:
        name = {'kernel': module.LIBRARY, 'source': module.KERNEL, 'header': module.HEADER}[subject]
        (directory / name).write_bytes(b'changed')
    elif subject == 'compiled-text':
        record['compiled']['sources'][module.KERNEL]['text'] += '// changed\n'
    elif subject == 'compiled-hash':
        record['compiled']['sources'][module.KERNEL]['sha256'] = '0' * 64
    elif subject == 'current-tu':
        record['translation_unit_sha256'] = '0' * 64
    elif subject == 'compiled-tu':
        record['compiled']['translation_unit_sha256'] = '0' * 64
    elif subject == 'flags':
        record['identity']['kernel_flags'] = ['-ffast-math']
    elif subject == 'tool':
        record['identity']['xcode'] = 'unrecorded compiler'
    elif subject == 'source-set':
        record['sources']['internal/device/metal_darwin.go'] = '0' * 64
    else:
        record = {'identity': record['identity'], 'files': record['sources']}
    manifest.write_text(json.dumps(record))
    result = invoke(directory)
    assert result.returncode == 1
    assert 'Metal ' in result.stderr
    assert not result.stdout


@pytest.mark.parametrize('subject', ['absolute-path', 'cycle', 'unknown-include', 'unused-source', 'oversized', 'extra-key'])
def test_precompiled_kernel_rejects_invalid_provenance_graph(assets, subject: str) -> None:
    module, directory, manifest = assets
    record = json.loads(manifest.read_text())
    compiled = record['compiled']
    if subject == 'absolute-path':
        compiled['sources']['/outside.h'] = compiled['sources'].pop('captured/types.h')
    elif subject == 'unused-source':
        compiled['sources']['unused.h'] = compiled['sources'].pop('captured/types.h')
    elif subject == 'extra-key':
        compiled['producer'] = 'obsolete Go build'
    else:
        text = {'cycle': '#include "../../runtime/metal/kernels.metal"\n',
                'unknown-include': '#include "unknown.h"\n',
                'oversized': 'x' * (module.MAX_SOURCE_BYTES + 1)}[subject]
        compiled['sources']['captured/types.h'] = {'text': text, 'sha256': hashlib.sha256(text.encode()).hexdigest()}
    manifest.write_text(json.dumps(record))
    result = invoke(directory)
    assert result.returncode == 1, result.stdout
    assert 'Metal ' in result.stderr


def test_current_source_rehash_cannot_claim_old_compilation(assets) -> None:
    module, directory, manifest = assets
    record = json.loads(manifest.read_text())
    path = directory / module.KERNEL
    path.write_bytes(path.read_bytes() + b'kernel void uncompiled() {}\n')
    current = {name: (directory / name).read_bytes() for name in module.KERNEL_SOURCES}
    record['sources'][module.KERNEL] = module.sha256(path.read_bytes())
    record['translation_unit_sha256'] = module.sha256(module.expanded_sources(current))
    manifest.write_text(json.dumps(record))
    result = invoke(directory)
    assert result.returncode == 1
    assert 'compiled translation unit differs' in result.stderr


def test_native_bridge_receipt_has_no_go_source_dependency(assets, monkeypatch: pytest.MonkeyPatch) -> None:
    module, directory, _ = assets
    for name in module.BRIDGE_SOURCES - module.KERNEL_SOURCES:
        path = directory / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('controlled current bridge source')
    spec = importlib.util.spec_from_file_location('lo_native_asset_build', ROOT / 'tools/nativeruntime/build.py')
    build = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(build)
    monkeypatch.setattr(build, 'ROOT', directory)
    library, identity, provenance = build.metal_assets()
    assert library == (directory / module.LIBRARY).read_bytes()
    assert identity['profile'] == 'metal-fp32'
    assert set(provenance) == {'manifest', 'sources'}
    assert set(provenance['sources']) == module.KERNEL_SOURCES | module.BRIDGE_SOURCES | {module.LIBRARY}
    assert not any(name.endswith('.go') for name in provenance['sources'])


@pytest.mark.parametrize('directive', [
    b'#\\\ninclude "extra.h"\n',
    b'#inc\\\nlude "extra.h"\n',
    b'/* prefix */#include "extra.h"\n',
    b'#/**/include "extra.h"\n',
    b'# /* gap */ include "extra.h"\n',
    b'%:include "extra.h"\n',
    b'%\\ \n:include "extra.h"\n',
    b'%\\\t\n:include "extra.h"\n',
    b'??=include "extra.h"\n',
    b'#pragma clang module import extra\n',
    b'_Pragma("clang module import extra")\n',
])
def test_public_cli_rejects_obfuscated_untracked_dependencies(assets, directive: bytes) -> None:
    module, directory, manifest = assets
    record = json.loads(manifest.read_text())
    current = {name: (directory / name).read_bytes() for name in module.KERNEL_SOURCES}
    # The old physical-line scanner retained these hidden directives as ordinary
    # bytes. Bind both snapshots to that former expansion to expose its acceptance.
    former_expanded = module.expanded_sources(current) + directive
    kernel = directory / module.KERNEL
    kernel.write_bytes(current[module.KERNEL] + directive)
    (kernel.parent / 'extra.h').write_text('untracked third compiler input\n')
    record['sources'][module.KERNEL] = module.sha256(kernel.read_bytes())
    record['translation_unit_sha256'] = module.sha256(former_expanded)
    compiled = record['compiled']
    captured = compiled['sources'][module.KERNEL]
    captured['text'] += directive.decode()
    captured['sha256'] = module.sha256(captured['text'].encode())
    compiled['translation_unit_sha256'] = module.sha256(former_expanded)
    manifest.write_text(json.dumps(record))
    result = invoke(directory)
    assert result.returncode == 1, 'untracked preprocessor input accepted: ' + repr(directive)
    assert 'Metal ' in result.stderr
    assert not result.stdout
