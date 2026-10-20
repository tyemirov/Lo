"""Assemble pinned Linux platform files and the software emulator."""
from __future__ import annotations

import copy
import io
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import subprocess
import tarfile

from prepare import ROOT, archive, digest, local, tree_digest


def unpack_tar(source: tarfile.TarFile, destination: Path, libraries_only: bool = False) -> None:
    members = {posixpath.normpath(member.name).removeprefix('./'): member for member in source}
    def resolve(name: str, visited: frozenset[str] = frozenset()) -> tarfile.TarInfo | Path:
        if name in visited:
            raise ValueError(f'archive link cycle: {name}')
        if name not in members:
            existing = local(destination / name)
            if existing.is_file():
                return existing
            raise ValueError(f"archive link target absent from pinned platform inputs: {name}")
        member = members[name]
        if member.issym() or member.islnk():
            target = member.linkname.lstrip('/') if member.linkname.startswith('/') else posixpath.normpath(posixpath.join(posixpath.dirname(name), member.linkname)) if member.issym() else posixpath.normpath(member.linkname).removeprefix('./')
            return resolve(target, visited | {name})
        if not member.isfile():
            raise ValueError(f'archive link does not target a file: {name}')
        return member
    for name, member in members.items():
        if member.isdir():
            continue
        path = PurePosixPath(name)
        if path.is_absolute() or '..' in path.parts:
            raise ValueError(f'unsafe archive member: {name}')
        if not libraries_only and path.parts[0] != 'lib' and tuple(path.parts[:2]) not in (('usr', 'lib'), ('usr', 'include')):
            continue
        if libraries_only:
            if '/lib/' in '/' + name and '.dylib' in path.name:
                target = local(destination / 'lib' / path.name)
            elif path.name == 'qemu-system-aarch64':
                target = local(destination / path.name)
            else:
                continue
        else:
            target = local(destination / path)
        canonical = resolve(name)
        target.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(canonical, Path):
            target.unlink(missing_ok=True)
            os.link(canonical, target)
            continue
        selected = copy.copy(canonical)
        selected.name = target.name
        source.extract(selected, path=target.parent, filter='data')


def deb_tar(path: Path) -> bytes:
    with path.open('rb') as source:
        if source.read(8) != b'!<arch>\n':
            raise ValueError(f'invalid Debian archive: {path}')
        while header := source.read(60):
            if len(header) != 60:
                raise ValueError(f'truncated Debian archive: {path}')
            size = int(header[48:58])
            payload = source.read(size)
            if size % 2:
                source.read(1)
            if header[:16].decode().strip().rstrip('/').startswith('data.tar.'):
                return payload
    raise ValueError(f'Debian data archive missing: {path}')


def platform_identity(state: Path, pins: list[dict[str, object]]) -> dict[str, object]:
    return {'archives': pins,
            'sysroot_sha256': tree_digest(local(state / 'linux-sysroot')),
            'emulation_sha256': tree_digest(local(state / 'emulation')),
            'guest_source_sha256': digest(ROOT / 'tools/nativeruntime/guest_init.c')}


def verify_platform(state: Path) -> None:
    receipt = local(state / 'platform.json')
    if not receipt.is_file():
        raise ValueError(f'missing prepared Linux platform receipt: {receipt}')
    lock = json.loads(Path(__file__).with_name('platform.lock.json').read_text())
    if json.loads(receipt.read_text()) != platform_identity(state, lock['artifacts']):
        raise ValueError('prepared Linux platform differs from receipt; select a fresh preparation state')


def prepare_platform(state: Path, download: bool) -> Path:
    lock = json.loads(Path(__file__).with_name('platform.lock.json').read_text())
    sysroot = local(state / 'linux-sysroot')
    receipt = local(state / 'platform.json')
    if receipt.exists():
        verify_platform(state)
        return sysroot
    if sysroot.exists():
        raise ValueError('Linux sysroot has no receipt; select a fresh preparation state')
    sysroot.mkdir()
    for pin in lock['artifacts']:
        path = archive(state, pin, download)
        if path.suffix == '.deb':
            with tarfile.open(fileobj=io.BytesIO(deb_tar(path))) as source:
                unpack_tar(source, sysroot)
        elif path.suffix == '.tbz2':
            with tarfile.open(path) as source:
                unpack_tar(source, local(state / 'emulation'), libraries_only=True)
    loader = local(sysroot / 'lib/ld-linux-aarch64.so.1')
    loader.unlink(missing_ok=True)
    os.link(local(sysroot / 'lib/aarch64-linux-gnu/ld-linux-aarch64.so.1'), loader)
    emulation = local(state / 'emulation')
    tools = local(state / 'tools/bin')
    guest_object = local(state / 'guest-init.o')
    commands = [
        [str(tools / 'clang'), '--target=aarch64-unknown-linux-gnu', '-c',
         str(ROOT / 'tools/nativeruntime/guest_init.c'), '-o', str(guest_object)],
        [str(tools / 'ld.lld'), '-m', 'aarch64elf', '-dynamic-linker', '/lib/ld-linux-aarch64.so.1',
         str(sysroot / 'usr/lib/aarch64-linux-gnu/crt1.o'), str(sysroot / 'usr/lib/aarch64-linux-gnu/crti.o'),
         str(guest_object), str(sysroot / 'lib/aarch64-linux-gnu/libc.so.6'),
         str(sysroot / 'usr/lib/aarch64-linux-gnu/libc_nonshared.a'),
         str(sysroot / 'usr/lib/aarch64-linux-gnu/crtn.o'), '-o', str(emulation / 'guest-init')]]
    environment = dict(os.environ, CI='1', TMPDIR=str(state))
    for command in commands:
        result = subprocess.run(command, env=environment, cwd=ROOT, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        if result.returncode:
            raise ValueError(f'guest initializer preparation failed: {result.stderr}')
    receipt.write_text(json.dumps(platform_identity(state, lock['artifacts']), indent=2) + '\n')
    return sysroot
