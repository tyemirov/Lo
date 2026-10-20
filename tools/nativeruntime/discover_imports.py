"""Discover native Darwin system imports from a Lo runtime archive."""
from __future__ import annotations

import argparse
import ctypes
import json
from pathlib import Path
import re
import subprocess
import sys

from prepare import ROOT, digest, local

PROGRAM_SYMBOLS = frozenset({'_lo_entry', '_lo_program_flow_count', '_lo_program_flows',
                             '_lo_program_metadata', '_lo_program_metadata_size'})


def discover(nm: Path, archive: Path) -> dict[str, object]:
    def symbols(selection: str) -> set[str]:
        command = [str(nm), '--no-llvm-bc', selection, '--extern-only', '-j', str(archive)]
        result = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True,
                                text=True, check=True)
        return {line for line in result.stdout.splitlines()
                if re.fullmatch(r'[A-Za-z_$][A-Za-z0-9_$.]*', line)}
    undefined = symbols('--undefined-only') - symbols('--defined-only')
    program = undefined & PROGRAM_SYMBOLS
    if program != PROGRAM_SYMBOLS:
        raise ValueError(f'runtime program imports differ from contract: {sorted(program)}')
    system = undefined - PROGRAM_SYMBOLS
    library = ctypes.CDLL(None)
    for symbol in sorted(system):
        try:
            getattr(library, symbol[1:] if symbol.startswith('_') else symbol)
        except AttributeError as error:
            raise ValueError(f'native runtime import is absent from the host system: {symbol}') from error
    return {'method': 'LLVM native archive externals (--no-llvm-bc for both undefined and defined scans), verified by host dynamic symbol lookup',
            'rust': '1.85.1', 'llvm': '21.1.8', 'archive_sha256': digest(archive),
            'symbols': sorted(system), 'program_symbols': sorted(program)}


def write_description(directory: Path, record: dict[str, object]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    symbols = record['symbols']
    if not isinstance(symbols, list):
        raise ValueError('system symbols must be a list')
    (directory / 'libSystem.tbd').write_text(
        '--- !tapi-tbd\ntbd-version: 4\ntargets: [ arm64-macos ]\n'
        'install-name: /usr/lib/libSystem.B.dylib\ncurrent-version: 1\ncompatibility-version: 1\n'
        'exports:\n  - targets: [ arm64-macos ]\n    symbols:\n'
        + ''.join('      - ' + str(symbol) + '\n' for symbol in symbols) + '...\n')
    (directory / 'symbols.json').write_text(json.dumps(record, indent=2) + '\n')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nm', required=True, type=Path)
    parser.add_argument('--archive', required=True, type=Path)
    parser.add_argument('--output', type=Path)
    arguments = parser.parse_args()
    try:
        record = discover(local(arguments.nm), local(arguments.archive))
        if arguments.output is not None:
            write_description(local(arguments.output), record)
        print(json.dumps(record))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f'native import discovery: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
