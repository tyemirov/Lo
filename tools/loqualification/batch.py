"""Run a declared Linux qualification batch in one isolated software guest."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/lodistribution'))
from prepare import local, verified  # noqa: E402

STREAM_LIMIT = 64 << 20
CASE_LIMIT = 4096
ARGUMENT_LIMIT = 1 << 20
GUEST_STATE = '/workspace/_scratch/qualification'
ENVIRONMENT = {'CI': '1', 'PATH': '', 'HOME': GUEST_STATE + '/home', 'TMPDIR': GUEST_STATE + '/tmp', 'TMP': GUEST_STATE + '/tmp',
               'TEMP': GUEST_STATE + '/tmp', 'LO_BUILD_ROOT': GUEST_STATE, 'XDG_CACHE_HOME': GUEST_STATE + '/cache',
               'LC_ALL': 'C', 'TZ': 'UTC', 'GOMAXPROCS': '2'}


@dataclass(frozen=True)
class Payload:
    source: Path
    path: str


@dataclass(frozen=True)
class Case:
    identifier: str
    argv: tuple[str, ...]
    cwd: str
    environment: tuple[str, ...]
    stdin: bytes
    timeout_seconds: int
    outputs: tuple[str, ...]


@dataclass(frozen=True)
class Result:
    identifier: str
    status: int
    stdout: bytes
    stderr: bytes
    elapsed_ns: int
    outputs: tuple[bytes | None, ...] = ()


def guest_path(raw: object) -> str:
    if not isinstance(raw, str) or not raw.startswith('/') or '\0' in raw:
        raise ValueError(f'invalid guest path: {raw!r}')
    path = PurePosixPath(raw)
    if '..' in path.parts or str(path) != raw or len(path.parts) < 2:
        raise ValueError(f'invalid guest path: {raw!r}')
    if path.parts[1] not in ('workspace', 'input', 'work', 'result', 'compiler'):
        raise ValueError(f'guest path is outside payload roots: {raw}')
    return raw


def strings(raw: object, subject: str) -> tuple[str, ...]:
    if not isinstance(raw, list) or not all(isinstance(value, str) and '\0' not in value for value in raw):
        raise ValueError(f'{subject} must contain strings without NUL')
    return tuple(raw)


def read_manifest(path: Path) -> tuple[tuple[Payload, ...], tuple[Case, ...]]:
    record = json.loads(path.read_text())
    if not isinstance(record, dict) or set(record) != {'files', 'cases'}:
        raise ValueError('batch manifest requires exactly files and cases')
    if not isinstance(record['files'], list) or not isinstance(record['cases'], list):
        raise ValueError('batch files and cases must be lists')
    if not 1 <= len(record['cases']) <= CASE_LIMIT:
        raise ValueError(f'batch requires 1..{CASE_LIMIT} cases')
    payloads = []
    destinations: list[PurePosixPath] = []
    for item in record['files']:
        if not isinstance(item, dict) or set(item) != {'source', 'path'} or not isinstance(item['source'], str):
            raise ValueError('batch payload requires exactly source and path')
        destination = guest_path(item['path'])
        target = PurePosixPath(destination)
        source = local(Path(item['source']))
        if not source.exists():
            raise ValueError(f'missing batch payload: {source}')
        if source.is_dir():
            for member in source.rglob('*'):
                local(member)
        destinations.append(target)
        payloads.append(Payload(source, destination))
    ordered_destinations = sorted(destinations, key=lambda path: path.parts)
    for previous, current in zip(ordered_destinations, ordered_destinations[1:]):
        if current == previous or current.is_relative_to(previous):
            raise ValueError(f'batch payload paths overlap: {current}')
    cases = []
    identifiers = set()
    fields = {'id', 'argv', 'cwd', 'environment', 'stdin', 'timeout_seconds', 'outputs'}
    for item in record['cases']:
        if not isinstance(item, dict) or set(item) != fields:
            raise ValueError(f'batch case requires exactly {sorted(fields)}')
        identifier = item['id']
        if not isinstance(identifier, str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,128}', identifier):
            raise ValueError(f'invalid batch case ID: {identifier!r}')
        if identifier in identifiers:
            raise ValueError(f'duplicate case ID: {identifier}')
        identifiers.add(identifier)
        argv = strings(item['argv'], 'case argv')
        if not argv or len(argv) >= 4094 or not argv[0].startswith('/'):
            raise ValueError(f'case {identifier} requires an absolute executable and bounded arguments')
        guest_path(argv[0])
        cwd = guest_path(item['cwd'])
        additions = item['environment']
        if not isinstance(additions, dict):
            raise ValueError(f'case {identifier} environment must be an object')
        environment = dict(ENVIRONMENT)
        for name, value in additions.items():
            if not isinstance(name, str) or not re.fullmatch(r'[A-Z_][A-Z0-9_]*', name) or name in ENVIRONMENT:
                raise ValueError(f'case {identifier} cannot override environment key: {name}')
            if not isinstance(value, str) or '\0' in value:
                raise ValueError(f'case {identifier} invalid environment value: {name}')
            environment[name] = value
        encoded_environment = tuple(f'{name}={value}' for name, value in sorted(environment.items()))
        if len(encoded_environment) >= 4094:
            raise ValueError(f'case {identifier} environment exceeds limit')
        for values in (argv, encoded_environment):
            if len(encode(values)) >= ARGUMENT_LIMIT:
                raise ValueError(f'case {identifier} arguments or environment exceed limit')
        if not isinstance(item['stdin'], str) or len(item['stdin'].encode()) > STREAM_LIMIT:
            raise ValueError(f'case {identifier} stdin exceeds limit')
        timeout = item['timeout_seconds']
        if type(timeout) is not int or not 1 <= timeout <= 7200:
            raise ValueError(f'case {identifier} timeout must be 1..7200 seconds')
        outputs = tuple(guest_path(value) for value in strings(item['outputs'], 'case outputs'))
        if len(outputs) > 32 or len(set(outputs)) != len(outputs):
            raise ValueError(f'case {identifier} outputs exceed limit or repeat')
        cases.append(Case(identifier, argv, cwd, encoded_environment, item['stdin'].encode(), timeout, outputs))
    return tuple(payloads), tuple(cases)


def encode(values: tuple[str, ...]) -> bytes:
    return b'\0'.join(os.fsencode(value) for value in values) + b'\0'


def receive(serial: Path, identifiers: tuple[str, ...],
            output_counts: tuple[int, ...] | None = None) -> tuple[Result, ...]:
    """Reject incomplete or repeated guest records before publishing batch results."""
    serial = local(serial)
    output_counts = output_counts or tuple(0 for _ in identifiers)
    streams: dict[tuple[int, str], bytearray] = {}
    lengths: dict[tuple[int, str], int] = {}
    output_presence: dict[tuple[int, int], bool] = {}
    results: dict[int, tuple[int, int]] = {}
    complete = False
    with serial.open('rb') as records:
        for raw in records:
            if not raw.endswith(b'\n'):
                raise ValueError(f'unterminated export record: {serial}')
            raw = raw[:-1]
            if not raw.startswith(b'LO_QUALIFICATION_'):
                raise ValueError(f'unexpected export record: {serial}')
            try:
                fields = raw.decode('ascii').split(':')
                kind = fields[0]
                if complete:
                    raise ValueError('record follows batch completion')
                if kind == 'LO_QUALIFICATION_COMPLETE':
                    if len(fields) != 2 or int(fields[1]) != len(identifiers):
                        raise ValueError('batch completion count differs')
                    complete = True
                    continue
                if kind == 'LO_QUALIFICATION_ERROR':
                    raise ValueError(f'guest protocol error: {fields[1:]}')
                index = int(fields[1])
                if not 0 <= index < len(identifiers):
                    raise ValueError('unknown case index')
                if kind == 'LO_QUALIFICATION_PROGRESS':
                    if len(fields) != 5 or int(fields[2]) < 0 or int(fields[3]) < 0:
                        raise ValueError('invalid progress record')
                    tail = bytes.fromhex(fields[4])
                    if len(tail) > 512 or len(tail) > int(fields[3]):
                        raise ValueError('progress tail exceeds its bound')
                    continue
                if kind in ('LO_QUALIFICATION_DATA', 'LO_QUALIFICATION_STREAM'):
                    if len(fields) != 4:
                        raise ValueError('invalid stream record')
                    stream = fields[2]
                    allowed = ('stdout', 'stderr', *(f'file{number}' for number in range(output_counts[index])))
                    if stream not in allowed:
                        raise ValueError('unknown case stream')
                    key = (index, stream)
                    if key in lengths:
                        raise ValueError('repeated stream or data after stream length')
                    if kind == 'LO_QUALIFICATION_DATA':
                        payload = bytes.fromhex(fields[3])
                        streams.setdefault(key, bytearray()).extend(payload)
                        if len(streams[key]) > STREAM_LIMIT:
                            raise ValueError('stream exceeds byte limit')
                    else:
                        lengths[key] = int(fields[3])
                elif kind == 'LO_QUALIFICATION_OUTPUT':
                    if len(fields) != 4:
                        raise ValueError('invalid output record')
                    number, present = int(fields[2]), int(fields[3])
                    key = (index, number)
                    if not 0 <= number < output_counts[index] or present not in (0, 1) or key in output_presence:
                        raise ValueError('invalid or repeated output presence')
                    output_presence[key] = bool(present)
                elif kind == 'LO_QUALIFICATION_RESULT':
                    if len(fields) != 4 or index in results:
                        raise ValueError('invalid or repeated case result')
                    status, elapsed = int(fields[2]), int(fields[3])
                    if not 0 <= status <= 255 or elapsed < 0:
                        raise ValueError('invalid result status or elapsed time')
                    results[index] = (status, elapsed)
                else:
                    raise ValueError('unknown qualification record')
            except (ValueError, UnicodeError, IndexError) as error:
                raise ValueError(f'invalid guest batch protocol in {serial}: {error}') from error
    if not complete:
        raise ValueError(f'guest batch completion missing: {serial}')
    exported = []
    for index, identifier in enumerate(identifiers):
        if index not in results:
            raise ValueError(f'missing case result: {identifier}')
        names = ['stdout', 'stderr']
        for number in range(output_counts[index]):
            key = (index, number)
            if key not in output_presence:
                raise ValueError(f'missing output presence: {identifier} {number}')
            if output_presence[key]:
                names.append(f'file{number}')
            elif (index, f'file{number}') in streams or (index, f'file{number}') in lengths:
                raise ValueError(f'absent output has data: {identifier} {number}')
        for name in names:
            key = (index, name)
            if key not in lengths or lengths[key] != len(streams.get(key, b'')):
                raise ValueError(f'guest stream byte count differs: {identifier} {name}')
        status, elapsed = results[index]
        outputs = tuple(bytes(streams.get((index, f'file{number}'), b'')) if output_presence[(index, number)] else None
                        for number in range(output_counts[index]))
        exported.append(Result(identifier, status, bytes(streams.get((index, 'stdout'), b'')),
                               bytes(streams.get((index, 'stderr'), b'')), elapsed, outputs))
    return tuple(exported)


def initializer(preparation: Path, work: Path, environment: dict[str, str]) -> Path:
    tools = json.loads(local(preparation / 'tools.json').read_text())['tools']
    clang, linker = (verified(tools[name]) for name in ('clang', 'ld.lld'))
    sysroot = local(preparation / 'linux-sysroot')
    obj, binary = work / 'init.o', work / 'init'
    commands = [[str(clang), '--target=aarch64-unknown-linux-gnu', '-O2', '-c',
                 str(ROOT / 'tools/loqualification/batch_init.c'), '-o', str(obj)],
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
            raise ValueError(f'qualification guest initializer failed: {result.stderr}')
    return binary


def run(preparation: Path, package: Path, manifest: Path, work: Path,
        package_path: str = '/compiler', accelerator: str = 'tcg') -> dict:
    if accelerator not in ('tcg', 'hvf'):
        raise ValueError('qualification accelerator must be tcg or hvf')
    cpu = 'host' if accelerator == 'hvf' else 'cortex-a72'
    preparation, package, manifest, work = map(local, (preparation, package, manifest, work))
    package_path = guest_path(package_path)
    payloads, cases = read_manifest(manifest)
    if not (package / 'bin/lo').is_file():
        raise ValueError(f'missing installed frontend: {package / "bin/lo"}')
    if work.exists():
        raise ValueError(f'guest work must be fresh: {work}')
    package_guest = PurePosixPath(package_path)
    if any(package_guest == PurePosixPath(item.path) or package_guest.is_relative_to(PurePosixPath(item.path))
           or PurePosixPath(item.path).is_relative_to(package_guest) for item in payloads):
        raise ValueError('guest package and payload paths overlap')
    work.mkdir(parents=True)
    guest = work / 'root'
    for name in ('dev', 'proc', 'tmp', 'home', 'cache', 'work', 'input', 'result', 'workspace', 'cases'):
        (guest / name).mkdir(parents=True, exist_ok=True)
    for name in ('home', 'tmp', 'cache'):
        (guest / GUEST_STATE.lstrip('/') / name).mkdir(parents=True, exist_ok=True)
    environment = {'CI': '1', 'PATH': '', 'HOME': str(work / 'host-home'), 'TMPDIR': str(work),
                   'DYLD_LIBRARY_PATH': str(preparation / 'emulation/lib')}
    (work / 'host-home').mkdir()
    os.link(initializer(preparation, work, environment), guest / 'init')
    libraries = local(preparation / 'linux-sysroot/lib')
    for member in libraries.rglob('*'):
        local(member)
    shutil.copytree(libraries, guest / 'lib', copy_function=os.link)
    for member in package.rglob('*'):
        local(member)
    destination = guest / package_path.lstrip('/')
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(package, destination, copy_function=os.link)
    for item in payloads:
        destination = guest / item.path.lstrip('/')
        destination.parent.mkdir(parents=True, exist_ok=True)
        if item.source.is_dir():
            shutil.copytree(item.source, destination, dirs_exist_ok=True, copy_function=shutil.copy2)
        else:
            shutil.copy2(item.source, destination)
    (guest / 'case-count').write_text(str(len(cases)))
    for index, case in enumerate(cases):
        destination = guest / 'cases' / str(index)
        destination.mkdir()
        for name, data in {'arguments': encode(case.argv), 'environment': encode(case.environment),
                           'cwd': case.cwd.encode() + b'\0', 'stdin': case.stdin,
                           'timeout': str(case.timeout_seconds).encode(), 'outputs': encode(case.outputs)}.items():
            (destination / name).write_bytes(data)
    archive = work / 'root.cpio'
    result = subprocess.run(['/usr/bin/tar', '--format=newc', '-cf', str(archive), '-C', str(guest), '.'],
                            cwd=ROOT, env=environment, stdin=subprocess.DEVNULL, capture_output=True, check=False)
    if result.returncode:
        raise ValueError(f'qualification guest archive failed: {result.stderr.decode(errors="replace")}')
    compressed = work / 'root.cpio.gz'
    with archive.open('rb') as original, gzip.open(compressed, 'wb', compresslevel=1) as destination:
        shutil.copyfileobj(original, destination)
    archive.unlink()
    export = work / 'export.log'
    command = [str(local(preparation / 'emulation/qemu-system-aarch64')), '-machine', 'virt',
               '-accel', accelerator, '-cpu', cpu, '-m', '8192', '-smp', '2', '-display', 'none',
               '-monitor', 'none', '-serial', 'stdio', '-no-reboot', '-nodefaults', '-nic', 'none',
               '-device', 'virtio-serial-pci,id=lo-transport', '-chardev', f'file,id=lo-export,path={export}',
               '-device', 'virtserialport,bus=lo-transport.0,nr=1,chardev=lo-export,name=lo.export',
               '-kernel', str(local(preparation / 'vmlinuz-virt')), '-initrd', str(compressed),
               '-append', 'console=ttyAMA0 rdinit=/init panic=-1 loglevel=3']
    serial, errors = work / 'serial.log', work / 'qemu.stderr'
    with serial.open('wb') as stdout, errors.open('wb') as stderr:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                stdout=stdout, stderr=stderr, timeout=120 + sum(case.timeout_seconds for case in cases),
                                check=False)
    if result.returncode:
        raise ValueError(f'qualification software guest failed: {errors.read_text(errors="replace")}')
    if not export.is_file():
        raise ValueError(f'qualification export channel missing: {export}')
    exported = receive(export, tuple(case.identifier for case in cases), tuple(len(case.outputs) for case in cases))
    records = []
    for index, (case, result) in enumerate(zip(cases, exported)):
        files = []
        for name, data in [('stdout', result.stdout), ('stderr', result.stderr),
                           *((f'file{number}', data) for number, data in enumerate(result.outputs))]:
            output = work / f'{index}.{name}'
            if data is not None:
                output.write_bytes(data)
                files.append({'name': name, 'path': str(output.relative_to(ROOT)), 'bytes': len(data),
                              'sha256': hashlib.sha256(data).hexdigest()})
        records.append({'id': result.identifier, 'status': result.status, 'elapsed_ns': result.elapsed_ns,
                        'argv': case.argv, 'cwd': case.cwd, 'streams': files,
                        'output_paths': case.outputs})
    digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    report = {'target': 'linux/arm64', 'guest_memory_mib': 8192, 'accelerator': accelerator, 'cpu': cpu,
              'environment': f'QEMU {accelerator.upper()}; no external network interface; loopback enabled; empty PATH; declared payloads',
              'package': str(package.relative_to(ROOT)), 'package_path': package_path,
              'compiler_sha256': digest(package / 'bin/lo'),
              'manifest_sha256': digest(package / 'toolchain.json'), 'batch_sha256': digest(manifest),
              'cases': records, 'serial': str(serial.relative_to(ROOT)), 'export': str(export.relative_to(ROOT))}
    preliminary = package / 'preliminary.json'
    if preliminary.is_file():
        report['preliminary'] = json.loads(preliminary.read_text())
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('preparation', 'package', 'manifest', 'work'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--package-path', default='/compiler')
    parser.add_argument('--accelerator', choices=('tcg', 'hvf'), default='tcg')
    args = parser.parse_args()
    try:
        report = run(args.preparation, args.package, args.manifest, args.work, args.package_path, args.accelerator)
        print(json.dumps(report))
        return 0 if all(case['status'] == 0 for case in report['cases']) else 1
    except (ValueError, OSError, subprocess.SubprocessError, KeyError, TypeError) as error:
        print(f'qualification batch: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
