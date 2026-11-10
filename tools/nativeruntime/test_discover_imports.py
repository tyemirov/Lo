"""Generated Darwin imports must link the native acceptance fixtures."""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
CLI = ROOT / 'tools/nativeruntime/discover_imports.py'
FIXTURES = (
    ('f031-json-accounting.c', 0),
    ('f033-descendants.c', 13),
    ('f034-tree.c', 32),
)


def run(command: list[str], work: Path) -> str:
    result = subprocess.run(command, cwd=work, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False, timeout=120)
    assert result.returncode == 0, f'{command!r}\n{result.stdout}\n{result.stderr}'
    return result.stdout


def test_native_fixture_imports_survive_discovery(tmp_path: Path) -> None:
    supplied = os.environ.get('LO_NATIVE_PREPARATION')
    if supplied is None or sys.platform != 'darwin':
        pytest.skip('Darwin native preparation supplies the real import discovery inputs')
    state = (ROOT / supplied).resolve()
    assert state.is_relative_to(ROOT / '_scratch')
    assert tmp_path.resolve().is_relative_to(ROOT / '_scratch')
    distribution = state / 'distribution'
    manifest = json.loads((distribution / 'toolchain.json').read_text())
    compiler = str(distribution / manifest['compiler']['path'])
    nm = str(state / 'tools/bin/llvm-nm')
    targets = [('software', manifest['targets']['darwin/arm64'])]
    if 'metal' in manifest:
        targets.append(('metal', manifest['metal']))

    objects: list[tuple[Path, list[str]]] = []
    for name, case in FIXTURES:
        source = ROOT / 'testdata/native-runtime' / name
        object_path = tmp_path / (name + '.o')
        run([compiler, '-target', 'aarch64-apple-darwin', '-mmacosx-version-min=27.0',
             '-O0', '-nostdinc', '-fno-stack-protector', '-DLO_CASE=' + str(case),
             '-I', str(ROOT / 'runtime/native/include'), '-c', str(source),
             '-o', str(object_path)], tmp_path)
        imports = run([nm, '--undefined-only', '--extern-only', '-j',
                       str(object_path)], tmp_path).splitlines()
        assert imports and all(re.fullmatch(r'_[A-Za-z0-9_$.]+', item) for item in imports)
        # Runtime and generated Lo entry points are supplied by the program link.
        # All system imports must resolve through the freshly generated stubs.
        allowed = [argument for symbol in imports if symbol.startswith('_lo_')
                   for argument in ('-U', symbol)]
        objects.append((object_path, allowed))

    for profile, target in targets:
        output = tmp_path / profile
        arguments = [sys.executable, '-B', str(CLI), '--nm', nm,
                     '--archive', str(distribution / target['runtime']['path']),
                     '--output', str(output)]
        if profile == 'metal':
            arguments.append('--metal')
        record = json.loads(run(arguments, tmp_path))
        descriptions = output / 'metal' if profile == 'metal' else output
        serialized = json.loads((descriptions / 'symbols.json').read_text())
        assert serialized == record
        system = set(re.findall(r'^      - (.+)$',
                                (descriptions / 'libSystem.tbd').read_text(), re.MULTILINE))
        assert system == set(record['symbols'])
        stubs = [str(path) for path in sorted(descriptions.glob('*.tbd'))]
        for object_path, allowed in objects:
            run([str(distribution / target['linker']['path']), '-dylib', '-arch', 'arm64',
                 '-platform_version', 'macos', '27.0', '27.0', '-undefined', 'error',
                 '-o', str(descriptions / (object_path.stem + '.dylib')), str(object_path),
                 *allowed, *stubs], tmp_path)
