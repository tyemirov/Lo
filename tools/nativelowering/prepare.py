"""Prepare the selected pinned LLVM tools; network is an explicit preparation step."""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
SELECTED = ('clang-21', 'llc', 'opt', 'lld', 'llvm-dwarfdump', 'llvm-nm')
ALIASES = {'clang': 'clang-21', 'ld64.lld': 'lld'}
# Selected member digests from the SHA256-verified official archive.
TOOL_SHA256 = {'clang': '8501bb9d3deb0250f23422a759b94651c89479937a0af29a7c7f0b98eaf3ca5a', 'clang-21': '8501bb9d3deb0250f23422a759b94651c89479937a0af29a7c7f0b98eaf3ca5a', 'ld64.lld': '0c4381c70e4229b9988e120710de371c031cedd1d6da8f415a9aa7bac21a3620', 'llc': '07ba92b98ccf4318ef42477ab16f2e0eea9845a8517f5a3d5e707ca05ec58a4e', 'lld': '0c4381c70e4229b9988e120710de371c031cedd1d6da8f415a9aa7bac21a3620', 'llvm-dwarfdump': '36b66049cc0995fdd7623235b260ecccd0239d87cc719488f7658f36a18fbc9b', 'llvm-nm': 'a011020cab7ca848ebb10948c636cde63a83cf81e81156285f96f577ea70e426', 'opt': '66e9c199539e80e7cdeb1a6dc370e5278a6e90ad3b18e984d1af746b1ede35c7'}

def digest(path: Path) -> str:
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def local(value: str) -> Path:
    path = Path(value)
    if not path.is_absolute():
        path = ROOT / path
    resolved = path.resolve()
    if not resolved.is_relative_to(ROOT) or resolved == ROOT:
        raise ValueError('state must be a directory strictly inside Lo')
    for component in (path, *path.parents):
        if component == ROOT:
            break
        if component.is_symlink():
            raise ValueError(f'symlink path is forbidden: {component}')
    return resolved

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state', required=True)
    parser.add_argument('--download', action='store_true')
    args = parser.parse_args()
    state = local(args.state)
    if not state.is_relative_to(ROOT / '_scratch'):
        raise ValueError('preparation state must be under Lo/_scratch')
    pin = json.loads((ROOT / 'experiments/native-backends/preparation.lock.json').read_text())['third_party_artifacts'][0]
    state.mkdir(parents=True, exist_ok=True)
    archive = local(str(state / pin['archive']))
    if not archive.exists():
        if not args.download:
            raise ValueError(f'missing pinned archive: {archive}; preparation requires --download')
        temporary = local(str(archive) + '.download')
        request = urllib.request.Request(pin['url'], headers={'User-Agent': 'Lo-native-lowering-preparation'})
        with urllib.request.urlopen(request, timeout=120) as response, temporary.open('wb') as output:
            shutil.copyfileobj(response, output)
        if temporary.stat().st_size != pin['bytes'] or digest(temporary) != pin['sha256']:
            raise ValueError('download does not match pinned LLVM archive')
        temporary.replace(archive)
    if archive.stat().st_size != pin['bytes'] or digest(archive) != pin['sha256']:
        raise ValueError('LLVM archive does not match repository pin')
    binary = local(str(state / 'tools/bin'))
    binary.mkdir(parents=True, exist_ok=True)
    found: set[str] = set()
    with tarfile.open(archive, mode='r|xz') as source:
        for member in source:
            member_path = Path(member.name)
            if member_path.parent.name != 'bin' or member_path.name not in SELECTED or not member.isfile():
                continue
            target = local(str(binary / member_path.name))
            # Standard extraction preserves the published runnable artifact.
            member.name = member_path.name
            source.extract(member, path=binary, filter='data')
            found.add(member_path.name)
    if found != set(SELECTED):
        raise ValueError(f'archive missing selected tools: {set(SELECTED) - found}')
    for alias, original in ALIASES.items():
        target = local(str(binary / alias))
        target.unlink(missing_ok=True)
        os.link(binary / original, target)
    for name, expected in TOOL_SHA256.items():
        if digest(binary / name) != expected:
            raise ValueError(f'published LLVM member digest mismatch: {name}')
    receipt = {'version': '21.1.8', 'archive': pin, 'tools': {
        name: {'path': str((binary / name).relative_to(ROOT)), 'sha256': digest(binary / name)}
        for name in sorted((*SELECTED, *ALIASES))}}
    local(str(state / 'tools.json')).write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps({'status': 'ok', 'tools': str((state / 'tools.json').relative_to(ROOT))}))
    return 0

if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, tarfile.TarError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
