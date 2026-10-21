"""Discover native Darwin system imports from a Lo runtime archive."""
from __future__ import annotations

import argparse
import ctypes
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

from prepare import ROOT, digest, local

FRAMEWORKS = {'Security': '/System/Library/Frameworks/Security.framework/Versions/A/Security',
              'CoreFoundation': '/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation',
              'Foundation': '/System/Library/Frameworks/Foundation.framework/Versions/C/Foundation',
              'Metal': '/System/Library/Frameworks/Metal.framework/Versions/A/Metal'}
LIBRARIES = {'objc': '/usr/lib/libobjc.A.dylib'}

class DynamicSymbol(ctypes.Structure):
    _fields_ = [('image', ctypes.c_char_p), ('base', ctypes.c_void_p),
                ('symbol', ctypes.c_char_p), ('address', ctypes.c_void_p)]

BUILD_AND_FIXTURE_SYMBOLS = frozenset({
    '__Unwind_DeleteException', '__Unwind_RaiseException', '_alarm', '_mkfifo',
})

PROGRAM_SYMBOLS = frozenset({'_lo_entry', '_lo_program_flow_count', '_lo_program_flows',
                             '_lo_program_metadata', '_lo_program_metadata_size'})


def discover(nm: Path, archive: Path) -> dict[str, object]:
    def symbols(selection: str) -> set[str]:
        command = [str(nm), '--no-llvm-bc', selection, '--extern-only', '-j', str(archive)]
        result = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True,
                                text=True, check=True)
        return {line for line in result.stdout.splitlines()
                if re.fullmatch(r'[A-Za-z_$][A-Za-z0-9_$.]*', line)}
    defined = symbols('--defined-only')
    undefined = symbols('--undefined-only') - defined
    program = undefined & PROGRAM_SYMBOLS
    if program != PROGRAM_SYMBOLS:
        raise ValueError(f'runtime program imports differ from contract: {sorted(program)}')
    system = (undefined - PROGRAM_SYMBOLS) | BUILD_AND_FIXTURE_SYMBOLS
    libraries = {name: ctypes.CDLL(path) for name, path in (FRAMEWORKS | LIBRARIES).items()}
    library = ctypes.CDLL(None)
    locate = library.dladdr
    locate.argtypes = [ctypes.c_void_p, ctypes.POINTER(DynamicSymbol)]
    locate.restype = ctypes.c_int
    frameworks: dict[str, list[str]] = {name: [] for name in FRAMEWORKS}
    dynamic_libraries: dict[str, list[str]] = {name: [] for name in LIBRARIES}
    for symbol in sorted(system):
        name = symbol[1:] if symbol.startswith('_') else symbol
        resolved = None
        for candidate in (library, *libraries.values()):
            try:
                resolved = getattr(candidate, name)
                break
            except AttributeError:
                continue
        if resolved is None:
            raise ValueError(f'native runtime import is absent from the host system: {symbol}')
        location = DynamicSymbol()
        if not locate(ctypes.cast(resolved, ctypes.c_void_p), ctypes.byref(location)):
            raise ValueError(f'native runtime import has no image: {symbol}')
        image = location.image.decode()
        for framework in frameworks:
            if '/' + framework + '.framework/' in image:
                frameworks[framework].append(symbol)
                break
        for name, path in LIBRARIES.items():
            if image == path or name == 'objc' and image == '/usr/lib/objc/libobjcMsgSend.dylib':
                dynamic_libraries[name].append(symbol)
                break
    return {'method': 'LLVM native archive externals (--no-llvm-bc for both undefined and defined scans), verified by host dynamic symbol lookup',
            'rust': '1.85.1', 'llvm': '21.1.8', 'archive_sha256': digest(archive),
            'metal': {'_lo_metal_open', '_lo_native_metal_library'} <= defined,
            'symbols': sorted(system - {symbol for values in (*frameworks.values(), *dynamic_libraries.values()) for symbol in values}),
            'framework_symbols': frameworks, 'library_symbols': dynamic_libraries, 'program_symbols': sorted(program)}


def write_description(directory: Path, record: dict[str, object]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    def replace(name: str, text: str) -> None:
        with tempfile.NamedTemporaryFile(dir=directory, delete=False) as output:
            temporary = Path(output.name)
            output.write(text.encode())
        try:
            temporary.replace(directory / name)
        finally:
            temporary.unlink(missing_ok=True)

    symbols = record['symbols']
    if not isinstance(symbols, list):
        raise ValueError('system symbols must be a list')
    replace('libSystem.tbd',
        '--- !tapi-tbd\ntbd-version: 4\ntargets: [ arm64-macos ]\n'
        'install-name: /usr/lib/libSystem.B.dylib\ncurrent-version: 1\ncompatibility-version: 1\n'
        'exports:\n  - targets: [ arm64-macos ]\n    symbols:\n'
        + ''.join('      - ' + str(symbol) + '\n' for symbol in symbols) + '...\n')
    for name, symbols in record['framework_symbols'].items():
        if name in ('Metal', 'Foundation') and not symbols:
            continue
        replace(name + '.tbd',
            '--- !tapi-tbd\ntbd-version: 4\ntargets: [ arm64-macos ]\n'
            + 'install-name: ' + FRAMEWORKS[name] + '\ncurrent-version: 1\ncompatibility-version: 1\n'
            + 'exports:\n  - targets: [ arm64-macos ]\n    symbols:\n'
            + ''.join('      - ' + symbol + '\n' for symbol in symbols) + '...\n')
    for name, symbols in record['library_symbols'].items():
        if not symbols:
            continue
        replace('lib' + name + '.tbd',
                '--- !tapi-tbd\ntbd-version: 4\ntargets: [ arm64-macos ]\n'
                + 'install-name: ' + LIBRARIES[name] + '\ncurrent-version: 1\ncompatibility-version: 1\n'
                + 'exports:\n  - targets: [ arm64-macos ]\n    symbols:\n'
                + ''.join('      - ' + symbol + '\n' for symbol in symbols) + '...\n')
    replace('symbols.json', json.dumps(record, indent=2) + '\n')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nm', required=True, type=Path)
    parser.add_argument('--archive', required=True, type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--metal', action='store_true')
    arguments = parser.parse_args()
    try:
        record = discover(local(arguments.nm), local(arguments.archive))
        if arguments.metal and not record['metal']:
            raise ValueError('Metal import descriptions require the bridge and embedded kernel definitions')
        if arguments.output is not None:
            write_description(local(arguments.output / 'metal' if arguments.metal else arguments.output), record)
        print(json.dumps(record))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f'native import discovery: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
