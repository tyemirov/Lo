"""Run a Linux ARM64 executable in the pinned software guest."""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from prepare import ROOT, local


def run(preparation: Path, executable: Path, arguments: list[str], stdin: bytes) -> int:
    if not executable.is_file():
        raise ValueError(f'missing guest executable: {executable}')
    if executable.read_bytes()[:4] != b'\x7fELF':
        raise ValueError(f'guest executable must be ELF: {executable}')
    encoded_arguments = b'\0'.join(os.fsencode(argument) for argument in [str(executable), *arguments]) + b'\0'
    if len(encoded_arguments) >= 1048576 or len(arguments) >= 4094:
        raise ValueError('guest arguments exceed the declared limit')
    emulation = local(preparation / 'emulation')
    with tempfile.TemporaryDirectory(prefix='guest-', dir=preparation) as temporary:
        work = local(Path(temporary))
        guest = local(work / 'root')
        (guest / 'dev').mkdir(parents=True)
        shutil.copy2(executable, guest / 'probe')
        shutil.copy2(local(emulation / 'guest-init'), guest / 'init')
        shutil.copytree(local(preparation / 'linux-sysroot/lib'), guest / 'lib')
        (guest / 'arguments').write_bytes(encoded_arguments)
        (guest / 'stdin').write_bytes(stdin)
        environment = dict(os.environ, CI='1', TMPDIR=str(work), DYLD_LIBRARY_PATH=str(emulation / 'lib'))
        result = subprocess.run(['/usr/bin/tar', '--format=newc', '-cf', str(work / 'root.cpio'),
                                 '-C', str(guest), '.'], cwd=ROOT, env=environment,
                                stdin=subprocess.DEVNULL, capture_output=True, check=False)
        if result.returncode:
            raise ValueError(f'guest archive failed: {result.stderr.decode(errors="replace")}')
        command = [str(local(emulation / 'qemu-system-aarch64')), '-machine', 'virt', '-accel', 'tcg',
                   '-cpu', 'cortex-a72', '-m', '256', '-display', 'none', '-monitor', 'none',
                   '-serial', 'stdio', '-no-reboot', '-nodefaults', '-nic', 'none',
                   '-kernel', str(local(preparation / 'vmlinuz-virt')), '-initrd', str(work / 'root.cpio'),
                   '-append', 'console=ttyAMA0 rdinit=/init panic=-1 loglevel=3']
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, timeout=120, check=False)
        if result.returncode:
            raise ValueError(f'software guest failed: {result.stderr.decode(errors="replace")}')
        marker = b'LO_NATIVE_RESULT:'
        if marker not in result.stdout:
            raise ValueError(f'guest result missing: {result.stdout[-4096:].decode(errors="replace")}')
        record = result.stdout.split(marker, 1)[1].splitlines()[0]
        status, stdout_hex, stderr_hex = record.split(b':')
        sys.stdout.buffer.write(bytes.fromhex(stdout_hex.decode()))
        sys.stderr.buffer.write(bytes.fromhex(stderr_hex.decode()))
        return int(status)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--preparation', required=True, type=Path)
    parser.add_argument('executable', type=Path)
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    arguments = parser.parse_args()
    try:
        return run(local(arguments.preparation), local(arguments.executable), arguments.arguments, sys.stdin.buffer.read())
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f'native guest: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
