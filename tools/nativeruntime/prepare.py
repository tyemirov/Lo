"""Prepare the pinned native compiler inputs inside Lo."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
LOCK = Path(__file__).with_name('preparation.lock.json')
RUST_COMPONENTS = {'rustfmt.tar.xz': 'rustfmt-preview', 'rustc.tar.xz': 'rustc', 'cargo.tar.xz': 'cargo',
                   'rust-std.tar.xz': 'rust-std-aarch64-apple-darwin',
                   'rust-std-linux.tar.xz': 'rust-std-aarch64-unknown-linux-gnu'}
LLVM_TOOLS = ('clang-21', 'llc', 'opt', 'lld', 'llvm-dwarfdump', 'llvm-nm',
              'llvm-ar', 'llvm-readobj', 'llvm-objdump')
LLVM_ALIASES = {'clang': 'clang-21', 'ld64.lld': 'lld', 'ld.lld': 'lld'}


def local(path: Path) -> Path:
    """Validate repository containment before filesystem access."""
    if not path.is_absolute():
        path = ROOT / path
    resolved = path.resolve()
    if not resolved.is_relative_to(ROOT) or resolved == ROOT:
        raise ValueError('path must be strictly inside Lo')
    for part in (path, *path.parents):
        if part == ROOT:
            break
        if part.is_symlink():
            raise ValueError(f'symlink path is forbidden: {part}')
    return resolved


def digest(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def tree_digest(root: Path) -> str:
    hasher = hashlib.sha256()
    for path in sorted(root.rglob('*')):
        if path.is_file():
            path = local(path)
            hasher.update(path.relative_to(root).as_posix().encode() + b'\0')
            hasher.update(bytes.fromhex(digest(path)))
    return hasher.hexdigest()


def pin_file(path: Path) -> dict[str, str]:
    return {'path': str(path.relative_to(ROOT)), 'sha256': digest(path)}


def archive(state: Path, pin: dict[str, object], download: bool) -> Path:
    path = local(state / str(pin['archive']))
    if not path.exists():
        if not download:
            raise ValueError(f'missing pinned archive {path}; preparation requires --download')
        temporary = local(path.with_suffix(path.suffix + '.download'))
        request = urllib.request.Request(str(pin['url']), headers={'User-Agent': 'Lo-native-runtime-preparation'})
        with urllib.request.urlopen(request, timeout=120) as response, temporary.open('wb') as output:
            shutil.copyfileobj(response, output)
        if temporary.stat().st_size != pin['bytes'] or digest(temporary) != pin['sha256']:
            raise ValueError(f'download does not match pin: {path.name}')
        temporary.replace(path)
    if path.stat().st_size != pin['bytes'] or digest(path) != pin['sha256']:
        raise ValueError(f'archive does not match pin: {path.name}')
    return path


def extract_rust(source: Path, prefix: Path, component: str) -> None:
    with tarfile.open(source, 'r|xz') as members:
        for member in members:
            parts = Path(member.name).parts
            if len(parts) < 3 or parts[1] != component or not member.isfile():
                continue
            relative = Path(*parts[2:])
            if relative.parts[0] not in ('bin', 'lib', 'share'):
                continue
            local(prefix / relative)
            member.name = relative.as_posix()
            members.extract(member, path=prefix, filter='data')


def prepare(state: Path, download: bool, rust_only: bool) -> dict[str, str]:
    if not state.is_relative_to(ROOT / '_scratch'):
        raise ValueError('preparation state must be under Lo/_scratch')
    state.mkdir(parents=True, exist_ok=True)
    lock = json.loads(LOCK.read_text())
    pins = lock['artifacts']
    selected = [pin for pin in pins if not rust_only or pin['archive'] in RUST_COMPONENTS]
    archives = {pin['archive']: archive(state, pin, download) for pin in selected}
    prefix = local(state / 'rust-prefix')
    rust_receipt = local(state / 'rust.json')
    rust_pins = [pin for pin in pins if pin['archive'] in RUST_COMPONENTS]
    if rust_receipt.exists():
        previous = json.loads(rust_receipt.read_text())
        if previous['archives'] != rust_pins or previous['prefix_sha256'] != tree_digest(prefix):
            raise ValueError('prepared Rust prefix differs from receipt; select a fresh preparation state')
    else:
        if prefix.exists():
            raise ValueError('Rust prefix has no receipt; select a fresh preparation state')
        prefix.mkdir()
        for name, component in RUST_COMPONENTS.items():
            extract_rust(archives[name], prefix, component)
    if tree_digest(prefix) != lock['rust_prefix_sha256']:
        raise ValueError('prepared Rust prefix differs from the repository pin')
    environment = dict(os.environ, CI='1', CARGO_HOME=str(local(state / 'cargo-home')),
                       RUSTUP_HOME=str(local(state / 'rustup-home')), TMPDIR=str(state))
    identities = {}
    for tool in ('rustc', 'cargo'):
        executable = local(prefix / 'bin' / tool)
        result = subprocess.run([str(executable), '--version'], env=environment, cwd=ROOT,
                                stdin=subprocess.DEVNULL, capture_output=True, text=True, check=True)
        if not result.stdout.startswith(tool + ' 1.85.1 '):
            raise ValueError(f'unexpected {tool} identity: {result.stdout}')
        identities[tool] = dict(pin_file(executable), identity=result.stdout.strip())
    rust_manifest = {'version': '1.85.1', 'prefix': str(prefix.relative_to(ROOT)),
                     'archives': [pin for pin in pins if pin['archive'] in RUST_COMPONENTS],
                     'prefix_sha256': tree_digest(prefix), **identities}
    (state / 'rust.json').write_text(json.dumps(rust_manifest, indent=2) + '\n')
    if not rust_only:
        binary = local(state / 'tools/bin')
        binary.mkdir(parents=True, exist_ok=True)
        tools_receipt = local(state / 'tools.json')
        if tools_receipt.exists():
            previous = json.loads(tools_receipt.read_text())
            if previous['archive'] != pins[0] or set(previous['tools']) != set((*LLVM_TOOLS, *LLVM_ALIASES)):
                raise ValueError('prepared LLVM receipt differs from pins; select a fresh preparation state')
            for name, pin in previous['tools'].items():
                if digest(local(binary / name)) != pin['sha256']:
                    raise ValueError('prepared LLVM tool differs from receipt; select a fresh preparation state')
        else:
            found = set()
            with tarfile.open(archives['llvm.tar.xz'], 'r|xz') as members:
                for member in members:
                    path = Path(member.name)
                    if path.parent.name != 'bin' or path.name not in LLVM_TOOLS or not member.isfile():
                        continue
                    member.name = path.name
                    members.extract(member, path=binary, filter='data')
                    found.add(path.name)
            if found != set(LLVM_TOOLS):
                raise ValueError(f'LLVM archive lacks tools: {set(LLVM_TOOLS) - found}')
            for alias, original in LLVM_ALIASES.items():
                target = local(binary / alias)
                target.unlink(missing_ok=True)
                os.link(binary / original, target)
        if {name: digest(local(binary / name)) for name in (*LLVM_TOOLS, *LLVM_ALIASES)} != lock['llvm_tools']:
            raise ValueError('prepared LLVM tool differs from the repository pin')
        record = {'version': '21.1.8', 'archive': pins[0],
                  'tools': {name: pin_file(binary / name) for name in sorted((*LLVM_TOOLS, *LLVM_ALIASES))}}
        (state / 'tools.json').write_text(json.dumps(record, indent=2) + '\n')
    return {'rust': str((state / 'rust.json').relative_to(ROOT)),
            **({} if rust_only else {'tools': str((state / 'tools.json').relative_to(ROOT))})}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state', required=True, type=Path)
    parser.add_argument('--download', action='store_true')
    parser.add_argument('--rust-only', action='store_true')
    parser.add_argument('--platform', action='store_true')
    arguments = parser.parse_args()
    try:
        state = local(arguments.state)
        result = prepare(state, arguments.download, arguments.rust_only)
        if arguments.platform:
            from platforms import prepare_platform
            result['linux_sysroot'] = str(prepare_platform(state, arguments.download).relative_to(ROOT))
        print(json.dumps(result))
        return 0
    except (ValueError, OSError, KeyError, tarfile.TarError, subprocess.CalledProcessError) as error:
        print(f'native preparation: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
