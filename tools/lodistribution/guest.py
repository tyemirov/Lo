"""Compile with an installed Linux package in a network-free QEMU TCG guest."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

from prepare import ROOT, local, verified

GUEST_PACKAGE = '/relocated compiler package'
TARGETS = ('linux/arm64', 'darwin/arm64')


@dataclass(frozen=True)
class Export:
    status: int
    stdout: bytes
    stderr: bytes
    artifact: bytes
    entries: tuple[str, ...]


def receive(export: Path) -> Export:
    """Validate the guest byte-count envelope before publishing an artifact."""
    export = local(export)
    streams = {name: bytearray() for name in ('stdout', 'stderr', 'artifact')}
    entries: list[str] = []
    status = None
    artifact_bytes = None
    with export.open('rb') as records:
        for record in records:
            if status is not None:
                raise ValueError(f'guest export record follows terminal result: {export}')
            if not record.endswith(b'\n'):
                raise ValueError(f'guest export record lacks its final newline: {export}')
            record = record[:-1]
            fields = record.split(b':')
            kind = fields[0]
            if kind == b'LO_DISTRIBUTION_DATA':
                if len(fields) != 3:
                    raise ValueError(f'guest artifact export field count is invalid: {export}')
                _, name, payload = fields
                if len(payload) % 2 or any(byte not in b'0123456789abcdef' for byte in payload):
                    raise ValueError(f'guest artifact export hex is invalid: {export}')
                try:
                    streams[name.decode()].extend(bytes.fromhex(payload.decode()))
                except (KeyError, ValueError, UnicodeError) as error:
                    raise ValueError(f'guest artifact export record is invalid: {export}') from error
            elif kind in (b'LO_DISTRIBUTION_ARTIFACT', b'LO_DISTRIBUTION_RESULT'):
                if len(fields) != 2 or not fields[1].isdigit():
                    raise ValueError(f'guest export scalar record is invalid: {export}')
                value = int(fields[1])
                if kind == b'LO_DISTRIBUTION_ARTIFACT':
                    if artifact_bytes is not None:
                        raise ValueError(f'guest artifact byte count repeated: {export}')
                    artifact_bytes = value
                else:
                    if value > 255:
                        raise ValueError(f'guest result status is outside its byte range: {export}')
                    status = value
            elif kind == b'LO_DISTRIBUTION_ENTRY':
                if len(fields) < 2:
                    raise ValueError(f'guest export entry field is missing: {export}')
                entry = record.split(b':', 1)[1]
                if not entry:
                    raise ValueError(f'guest export entry is empty: {export}')
                entries.append(entry.decode())
            else:
                raise ValueError(f'unknown guest export record: {export}')
    if status is None:
        raise ValueError(f'distribution guest result missing; inspect {export}')
    if artifact_bytes is None or artifact_bytes != len(streams['artifact']):
        raise ValueError(f'guest artifact export byte count missing or differs: {export}')
    if status == 0 and artifact_bytes == 0:
        raise ValueError(f'successful guest artifact export is empty: {export}')
    return Export(status, bytes(streams['stdout']), bytes(streams['stderr']),
                  bytes(streams['artifact']), tuple(entries))


def initializer(preparation: Path, work: Path, environment: dict[str, str]) -> Path:
    tools = json.loads(local(preparation / 'tools.json').read_text())['tools']
    clang, linker = (verified(tools[name]) for name in ('clang', 'ld.lld'))
    sysroot = local(preparation / 'linux-sysroot')
    obj, binary = work / 'init.o', work / 'init'
    commands = [[str(clang), '--target=aarch64-unknown-linux-gnu', '-O2', '-c',
                 str(ROOT / 'tools/lodistribution/guest_init.c'), '-o', str(obj)],
                [str(linker), '-m', 'aarch64elf', '-dynamic-linker', '/lib/ld-linux-aarch64.so.1',
                 str(sysroot / 'usr/lib/aarch64-linux-gnu/crt1.o'),
                 str(sysroot / 'usr/lib/aarch64-linux-gnu/crti.o'), str(obj),
                 str(sysroot / 'lib/aarch64-linux-gnu/libc.so.6'),
                 str(sysroot / 'usr/lib/aarch64-linux-gnu/libc_nonshared.a'),
                 str(sysroot / 'usr/lib/aarch64-linux-gnu/crtn.o'), '-o', str(binary)]]
    for command in commands:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        if result.returncode:
            raise ValueError(f'distribution guest initializer failed: {result.stderr}')
    return binary


def compile(preparation: Path, package: Path, source: Path, target: str, output: Path,
            work: Path, extra_args: list[str] | None = None, *,
            initial_output: bytes | None = None) -> dict:
    preparation, package, source, output, work = map(local, (preparation, package, source, output, work))
    if not (package / 'bin/lo').is_file():
        raise ValueError(f'missing installed frontend: {package / "bin/lo"}')
    if not source.is_file():
        raise ValueError(f'missing guest source: {source}')
    if target not in TARGETS:
        raise ValueError(f'unsupported guest target: {target}')
    if work.exists():
        raise ValueError(f'guest work must be fresh: {work}')
    command = [GUEST_PACKAGE + '/bin/lo', 'build', '--target', target,
               '--output', '/result/program', '--format', 'json', *(extra_args or []), '/input/source.lo']
    if any('\0' in argument for argument in command):
        raise ValueError('guest arguments contain NUL')
    encoded = b'\0'.join(os.fsencode(argument) for argument in command) + b'\0'
    if len(encoded) >= 1048576 or len(command) >= 4094:
        raise ValueError('guest arguments exceed the declared limit')
    work.mkdir(parents=True)
    guest = work / 'root'
    for name in ('dev', 'proc', 'tmp', 'home', 'cache', 'unrelated', 'input', 'result'):
        (guest / name).mkdir(parents=True, exist_ok=True)
    environment = {'CI': '1', 'PATH': '', 'HOME': str(work / 'host-home'), 'TMPDIR': str(work),
                   'DYLD_LIBRARY_PATH': str(preparation / 'emulation/lib')}
    (work / 'host-home').mkdir()
    os.link(initializer(preparation, work, environment), guest / 'init')
    libraries = local(preparation / 'linux-sysroot/lib')
    for path in libraries.rglob('*'):
        local(path)
    shutil.copytree(libraries, guest / 'lib', copy_function=os.link)
    for path in package.rglob('*'):
        local(path)
    shutil.copytree(package, guest / GUEST_PACKAGE.lstrip('/'), copy_function=os.link)
    shutil.copyfile(source, guest / 'input/source.lo')
    (guest / 'arguments').write_bytes(encoded)
    if initial_output is not None:
        (guest / 'result/program').write_bytes(initial_output)
    archive = work / 'root.cpio'
    result = subprocess.run(['/usr/bin/tar', '--format=newc', '-cf', str(archive), '-C', str(guest), '.'],
                            cwd=ROOT, env=environment, stdin=subprocess.DEVNULL, capture_output=True, check=False)
    if result.returncode:
        raise ValueError(f'distribution guest archive failed: {result.stderr.decode(errors="replace")}')
    compressed = work / 'root.cpio.gz'
    with archive.open('rb') as original, gzip.open(compressed, 'wb', compresslevel=1) as destination:
        shutil.copyfileobj(original, destination)
    archive.unlink()
    export = work / 'export.log'
    command = [str(local(preparation / 'emulation/qemu-system-aarch64')), '-machine', 'virt',
               '-accel', 'tcg', '-cpu', 'cortex-a72', '-m', '4096', '-smp', '2', '-display', 'none',
               '-monitor', 'none', '-serial', 'stdio', '-no-reboot', '-nodefaults', '-nic', 'none',
               '-device', 'virtio-serial-pci,id=lo-transport',
               '-chardev', 'file,id=lo-export,path=' + str(export),
               '-device', 'virtserialport,bus=lo-transport.0,nr=1,chardev=lo-export,name=lo.export',
               '-kernel', str(local(preparation / 'vmlinuz-virt')), '-initrd', str(compressed),
               '-append', 'console=ttyAMA0 rdinit=/init panic=-1 loglevel=3']
    serial, errors = work / 'serial.log', work / 'qemu.stderr'
    with serial.open('wb') as stdout, errors.open('wb') as stderr:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                stdout=stdout, stderr=stderr, timeout=240, check=False)
    if result.returncode:
        raise ValueError(f'distribution software guest failed: {errors.read_text(errors="replace")}')
    if not export.is_file() or export.stat().st_size == 0:
        raise ValueError(f'distribution guest export transport missing or empty; inspect {serial}')
    exported = receive(export)
    stdout_text = exported.stdout.decode(errors='replace')
    receipt = json.loads(stdout_text) if exported.status == 0 else None
    if exported.artifact:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_bytes(exported.artifact)
    report = {'status': exported.status, 'stdout': stdout_text, 'stderr': exported.stderr.decode(errors='replace'),
              'receipt': receipt, 'artifact': str(output) if exported.artifact else None,
              'artifact_bytes': len(exported.artifact),
              'artifact_sha256': hashlib.sha256(exported.artifact).hexdigest() if exported.artifact else None,
              'entries': exported.entries,
              'environment': 'Linux arm64 QEMU TCG; no network interface; empty PATH and caches',
              'serial': str(serial), 'export': str(export)}
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('preparation', 'package', 'source', 'output', 'work'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--target', choices=TARGETS, required=True)
    parser.add_argument('--extra-arg', action='append', default=[])
    args = parser.parse_args()
    try:
        report = compile(args.preparation, args.package, args.source, args.target, args.output,
                         args.work, args.extra_arg)
        print(json.dumps(report))
        return report['status']
    except (ValueError, OSError, subprocess.SubprocessError, KeyError) as error:
        print(f'distribution guest: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
