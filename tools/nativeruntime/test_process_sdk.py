"""Validate SDK evidence without access to the producer's tool installation."""
import copy
import json
import os
from pathlib import Path

import pytest

from process_sdk import SOURCES, compiler_flags, sha256, validate_receipt


def sample_receipt() -> tuple[dict, dict[str, str]]:
    hashes = {source: 'a' * 64 for source in SOURCES.values()}
    hashes['runtime/native/include/lo_process_ownership.h'] = 'b' * 64
    receipt = {
        'compiler': {'path': '/producer/clang', 'sha256': 'c' * 64, 'version': 'selected compiler'},
        'sdk': {'path': '/producer/SDK', 'version': '27.0', 'settings_sha256': 'd' * 64},
        'objects': {name: {
            'source': {'path': source, 'sha256': hashes[source]},
            'headers': {'runtime/native/include/lo_process_ownership.h': 'b' * 64,
                        '/producer/SDK/usr/include/stdint.h': 'e' * 64},
            'flags': compiler_flags('/producer/SDK'), 'object_sha256': 'f' * 64, 'deployment': '27.0',
        } for name, source in SOURCES.items()},
    }
    return receipt, hashes


def test_process_sdk_receipt_is_independent_of_producer_paths() -> None:
    receipt, hashes = sample_receipt()
    opened = []

    def read_local(name: str) -> str:
        opened.append(name)
        return hashes[name]

    validate_receipt(receipt, read_local)
    assert set(opened) == set(hashes)
    assert all(not Path(name).is_absolute() for name in opened)


@pytest.mark.parametrize('change', [
    'target', 'sysroot', 'modules', 'extra-flag', 'compiler-version', 'sdk-version',
    'local-header', 'header-parent', 'header-outside-runtime', 'external-header-hash',
    'missing-object', 'deployment', 'source', 'extra-receipt-field', 'producer-nul', 'header-nul',
])
def test_process_sdk_receipt_rejects_changed_contract(change: str) -> None:
    original, hashes = sample_receipt()
    receipt = copy.deepcopy(original)
    item = receipt['objects']['process_es']
    if change == 'target':
        item['flags'][1] = 'x86_64-apple-darwin'
    elif change == 'sysroot':
        item['flags'][item['flags'].index('-isysroot') + 1] = '/different/SDK'
    elif change == 'modules':
        item['flags'].remove('-fno-modules')
    elif change == 'extra-flag':
        item['flags'].append('-DUNDECLARED=1')
    elif change == 'compiler-version':
        receipt['compiler']['version'] = 27
    elif change == 'sdk-version':
        receipt['sdk']['version'] = ['27.0']
    elif change == 'local-header':
        item['headers']['runtime/native/include/lo_process_ownership.h'] = '0' * 64
    elif change == 'header-parent':
        item['headers']['runtime/native/../../outside.h'] = '0' * 64
    elif change == 'header-outside-runtime':
        item['headers']['elsewhere/header.h'] = '0' * 64
    elif change == 'external-header-hash':
        item['headers']['/producer/SDK/usr/include/stdint.h'] = 123
    elif change == 'missing-object':
        del receipt['objects']['execution_token']
    elif change == 'deployment':
        item['deployment'] = '28.0'
    elif change == 'source':
        item['source']['sha256'] = '0' * 64
    elif change == 'extra-receipt-field':
        receipt['extra'] = True
    elif change == 'producer-nul':
        receipt['compiler']['path'] = '/producer/clang\0'
    elif change == 'header-nul':
        item['headers']['/producer/header\0'] = '0' * 64
    with pytest.raises(ValueError):
        validate_receipt(receipt, hashes.__getitem__)


def test_prepared_process_sdk_receipt() -> None:
    selected = os.environ.get('LO_NATIVE_PREPARATION')
    if selected is None:
        pytest.skip('LO_NATIVE_PREPARATION selects the prepared runtime')
    root = Path(__file__).resolve().parents[2]
    state = (root / selected).resolve()
    assert state.is_relative_to(root / '_scratch')
    receipt = json.loads((state / 'distribution/build-receipt.json').read_text())
    assert 'process_sdk' in receipt, 'runtime build omitted Process SDK evidence'
    validate_receipt(receipt['process_sdk'], lambda name: sha256(root / name))
