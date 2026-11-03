"""Validate precompiled Metal kernels and their immutable compilation inputs."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import posixpath
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
KERNEL = 'runtime/metal/kernels.metal'
HEADER = 'runtime/native/host/metal/metal_types.h'
LIBRARY = 'runtime/metal/metal.metallib'
MANIFEST = 'runtime/metal/manifest.json'
KERNEL_SOURCES = {KERNEL, HEADER}
BRIDGE_SOURCES = {HEADER, 'runtime/native/host/metal/metal_bridge.h',
                  'runtime/native/host/metal/metal_bridge_darwin.m'}
MAX_SOURCE_BYTES = 256 * 1024
MAX_EXPANDED_BYTES = 512 * 1024
KERNEL_FLAGS = ['-std=metal3.1', '-mmacosx-version-min=27.0', '-fmetal-math-mode=safe',
                '-fmetal-math-fp32-functions=precise', '-ffp-contract=off']
IDENTITY = {'profile': 'metal-fp32', 'xcode': 'Xcode 27.1\nBuild version 27A9275',
            'sdk': '27.0', 'clang': 'Apple clang version 21.0.0 (clang-2100.3.34.2)',
            'metal_compiler': 'Apple metal version 32023.921 (metalfe-32023.921.6)',
            'kernel_linker': 'AIR-LLD 32023.921 (metalfe-32023.921.6) (compatible with legacy metallib linker)',
            'deployment_target': '27.0', 'kernel_flags': KERNEL_FLAGS}


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def checked_path(root: Path, name: str) -> Path:
    path = (root / name).resolve()
    if not root.is_relative_to(ROOT) or not path.is_relative_to(root):
        raise ValueError('Metal asset path must remain inside its Lo root')
    return path


def source_name(name: object) -> str:
    if not isinstance(name, str) or len(name.encode('utf-8')) > 4096:
        raise ValueError('Metal source path is invalid')
    path = PurePosixPath(name)
    if not name or path.is_absolute() or path.as_posix() != name or '..' in path.parts or '\\' in name or '\0' in name:
        raise ValueError('Metal source path must be a clean repository-relative path')
    return name


def expanded_sources(sources: dict[str, bytes], entry: str = KERNEL) -> bytes:
    """Expand the bounded two-file local graph; system headers remain pinned inputs."""
    if len(sources) != 2 or sum(map(len, sources.values())) > MAX_SOURCE_BYTES:
        raise ValueError('Metal kernel source graph exceeds its bounds')
    for name, data in sources.items():
        source_name(name)
        # Only the canonical fixed-kernel preprocessing subset is supported.
        # These constructs change logical lines/tokens before include recognition.
        if any(token in data for token in (b'\\', b'\r', b'/*', b'*/', b'%:', b'??', b'_Pragma', b'__pragma')):
            raise ValueError('Metal kernel has unsupported preprocessing syntax')
    visited: set[str] = set()

    def expand(name: str, active: set[str]) -> bytes:
        if name in active or name not in sources:
            raise ValueError('Metal kernel has a cyclic or unknown local include')
        visited.add(name)
        active = active | {name}
        output = bytearray()
        for line in sources[name].splitlines(keepends=True):
            if re.search(rb'\b__(?:FILE|LINE|BASE_FILE|INCLUDE_LEVEL)__\b', line):
                raise ValueError('Metal kernel depends on its source location')
            directive = line.removesuffix(b'\n')
            code = directive.split(b'//', 1)[0]
            if b'#' in code:
                if ((directive == b'#include <metal_stdlib>' and name == entry)
                        or (directive == b'#include <stdint.h>' and name != entry)):
                    output.extend(line)
                elif name != entry and directive in {
                    b'#ifndef LO_METAL_TYPES_H', b'#define LO_METAL_TYPES_H',
                    b'#ifdef __METAL_VERSION__', b'#else', b'#endif',
                }:
                    output.extend(line)
                else:
                    match = re.fullmatch(rb'#include "([^"\r\n]+)"', directive)
                    if match is None:
                        raise ValueError('Metal kernel has an unsupported preprocessing directive')
                    included = posixpath.normpath(posixpath.join(posixpath.dirname(name), match[1].decode('utf-8')))
                    source_name(included)
                    output.extend(expand(included, active))
            else:
                output.extend(line)
            if len(output) > MAX_EXPANDED_BYTES:
                raise ValueError('Metal expanded translation unit exceeds its bound')
        return bytes(output)

    result = expand(entry, set())
    if visited != set(sources):
        raise ValueError('Metal kernel graph contains unused sources')
    return result


def snapshot(sources: dict[str, bytes]) -> dict[str, object]:
    return {'entry': KERNEL, 'sources': {name: {'sha256': sha256(data), 'text': data.decode('utf-8')}
                                       for name, data in sorted(sources.items())},
            'translation_unit_sha256': sha256(expanded_sources(sources))}


def validate_assets(root: Path) -> tuple[bytes, dict[str, object]]:
    root = root.resolve()
    manifest = checked_path(root, MANIFEST)
    if manifest.stat().st_size > MAX_SOURCE_BYTES * 3:
        raise ValueError('Metal provenance manifest exceeds its bound')
    record = json.loads(manifest.read_text())
    if not isinstance(record, dict) or set(record) != {'identity', 'sources', 'translation_unit_sha256', 'compiled'}:
        raise ValueError('Metal provenance manifest has an incorrect field set')
    identity = record['identity']
    if not isinstance(identity, dict) or set(identity) != set(IDENTITY) | {'kernel_sha256'} or any(identity[key] != value for key, value in IDENTITY.items()):
        raise ValueError('Metal kernel tool or numeric identity differs from its pins')
    if not isinstance(record['sources'], dict) or set(record['sources']) != KERNEL_SOURCES:
        raise ValueError('Metal kernel source inventory differs')
    sources = {}
    for name in sorted(KERNEL_SOURCES):
        path = checked_path(root, name)
        if path.stat().st_size > MAX_SOURCE_BYTES:
            raise ValueError('Metal kernel source exceeds its bound')
        sources[name] = path.read_bytes()
        if sha256(sources[name]) != record['sources'][name]:
            raise ValueError(f'Metal kernel source identity differs: {name}')
    expanded = expanded_sources(sources)
    if sha256(expanded) != record['translation_unit_sha256']:
        raise ValueError('Metal current translation unit identity differs')
    compiled = record['compiled']
    if not isinstance(compiled, dict) or set(compiled) != {'entry', 'sources', 'translation_unit_sha256'} or compiled['entry'] != KERNEL:
        raise ValueError('Metal compiled provenance has an incorrect field set')
    captured = compiled['sources']
    if not isinstance(captured, dict) or len(captured) != 2:
        raise ValueError('Metal compiled source inventory differs')
    original = {}
    for name, item in captured.items():
        source_name(name)
        if not isinstance(item, dict) or set(item) != {'sha256', 'text'} or not isinstance(item['text'], str):
            raise ValueError('Metal compiled source snapshot has an incorrect field set')
        data = item['text'].encode('utf-8')
        if len(data) > MAX_SOURCE_BYTES or sha256(data) != item['sha256']:
            raise ValueError('Metal compiled source identity differs')
        original[name] = data
    compiled_expanded = expanded_sources(original)
    if sha256(compiled_expanded) != compiled['translation_unit_sha256'] or compiled_expanded != expanded:
        raise ValueError('Metal compiled translation unit differs from current sources')
    library = checked_path(root, LIBRARY).read_bytes()
    if sha256(library) != identity['kernel_sha256']:
        raise ValueError('Metal kernel library identity differs')
    return library, identity


def reproduce(root: Path) -> None:
    """Compile the current canonical graph with the declared original kernel tools."""
    def command(*args: str) -> str:
        result = subprocess.run(args, cwd=root, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=True)
        return result.stdout.strip()
    for args, expected, first_line in [
        (('xcodebuild', '-version'), IDENTITY['xcode'], False),
        (('xcrun', '--sdk', 'macosx', '--show-sdk-version'), IDENTITY['sdk'], False),
        (('xcrun', '--sdk', 'macosx', 'clang', '--version'), IDENTITY['clang'], True),
        (('xcrun', '--sdk', 'macosx', 'metal', '--version'), IDENTITY['metal_compiler'], True),
        (('xcrun', '--sdk', 'macosx', 'metallib', '--version'), IDENTITY['kernel_linker'], False),
    ]:
        actual = command(*args)
        if (actual.splitlines()[0] if first_line else actual) != expected:
            raise ValueError(f'Metal reproduction tool identity differs: {args[0]}')
    sources = {name: checked_path(root, name).read_bytes() for name in KERNEL_SOURCES}
    captured = snapshot(sources)
    work_root = checked_path(root, '_scratch/metal-kernel-reproduction')
    work_root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=work_root) as temporary:
        air, library = Path(temporary) / 'kernels.air', Path(temporary) / 'metal.metallib'
        command('xcrun', '--sdk', 'macosx', 'metal', *KERNEL_FLAGS, '-c', KERNEL, '-o', str(air))
        command('xcrun', '--sdk', 'macosx', 'metallib', str(air), '-o', str(library))
        if sources != {name: checked_path(root, name).read_bytes() for name in KERNEL_SOURCES}:
            raise ValueError('Metal kernel sources changed during reproduction')
        data = library.read_bytes()
    record = {'identity': dict(IDENTITY, kernel_sha256=sha256(data)),
              'sources': {name: sha256(data) for name, data in sorted(sources.items())},
              'translation_unit_sha256': captured['translation_unit_sha256'], 'compiled': captured}
    checked_path(root, LIBRARY).write_bytes(data)
    checked_path(root, MANIFEST).write_text(json.dumps(record, indent=2) + '\n')
    validate_assets(root)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('verify', 'reproduce'))
    parser.add_argument('--root', type=Path, default=ROOT)
    arguments = parser.parse_args()
    try:
        root = arguments.root.resolve()
        checked_path(root, MANIFEST)
        if arguments.action == 'reproduce':
            reproduce(root)
        library, identity = validate_assets(root)
        print(json.dumps({'kernel_sha256': sha256(library), 'profile': identity['profile']}))
        return 0
    except (ValueError, OSError, UnicodeError, subprocess.CalledProcessError) as error:
        print(f'Metal assets: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
