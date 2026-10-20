"""Repository-contained native backend probe CLI."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import resource
import signal
import struct
import subprocess
import sys
import time
from typing import Any, Callable

ROOT = Path(__file__).absolute().parents[2]
SOURCE = ROOT / 'experiments/native-backends'
STATE_NAME = '_scratch/native-backend-probe'
TARGETS = ('aarch64-apple-darwin', 'aarch64-unknown-linux-gnu')
BACKENDS = ('llvm', 'cranelift')
TOOLS = ('llvm', 'cranelift', 'cc', 'linker', 'runner')
EXPECTED = '23 0\n0 7\nruntime_calls 1\n'
GUARD_CONTRACT = 'readonly-sentinel,state-directory'
RUNNER_CONTRACT = 'executable,sysroot'
GUARD_RESULT = {'readonly_write': 'denied', 'network': 'denied', 'descendant': 'denied', 'writable_state': 'allowed'}
HASH_PATTERN = re.compile(r'[0-9a-f]{64}')


class InputError(Exception):
    """Invalid external input."""


def relative_path(value: Any, *, exists: bool = True) -> Path:
    if not isinstance(value, str):
        raise InputError('path must be a repository-relative string')
    raw = PurePosixPath(value)
    if not value or raw.is_absolute() or any(p in ('', '.', '..') for p in value.split('/')) or '\\' in value:
        raise InputError(f'path must be repository-relative without traversal: {value!r}')
    path = ROOT
    for part in raw.parts:
        path = path / part
        if path.is_symlink():
            raise InputError(f'symlink is prohibited: {value}')
    if not path.resolve().is_relative_to(ROOT):
        raise InputError(f'path crosses repository boundary: {value}')
    if exists and not path.exists():
        raise InputError(f'missing repository input: {value}')
    return path


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree_sha256(path: Path) -> str:
    entries = {}
    for child in sorted(path.rglob('*')):
        relative_path(child.relative_to(ROOT).as_posix())
        if child.is_file():
            entries[child.relative_to(path).as_posix()] = sha256(child)
        elif not child.is_dir():
            raise InputError(f'unsupported bundle entry: {child.relative_to(ROOT)}')
    return hashlib.sha256(json.dumps(entries, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def fields(value: Any, required: set[str], subject: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != required:
        raise InputError(f'{subject} requires exactly: {", ".join(sorted(required))}')
    return value


@dataclass(frozen=True)
class Pinned:
    path: Path
    digest: str

    @classmethod
    def read(cls, value: Any, subject: str, directory: bool = False) -> Pinned:
        record = fields(value, {'path', 'sha256'}, subject)
        path = relative_path(record['path'])
        if not isinstance(record['sha256'], str) or not HASH_PATTERN.fullmatch(record['sha256']):
            raise InputError(f'{subject} requires a pinned SHA-256')
        if directory and not path.is_dir() or not directory and not path.is_file():
            raise InputError(f'{subject} has incorrect path type')
        actual = tree_sha256(path) if directory else sha256(path)
        if actual != record['sha256']:
            raise InputError(f'{subject} SHA-256 mismatch: {record["path"]}')
        state = relative_path(STATE_NAME, exists=False)
        if path.is_relative_to(state) or state.is_relative_to(path):
            raise InputError(f'{subject} overlaps writable experiment state')
        return cls(path, actual)


@dataclass(frozen=True)
class Tool:
    pin: Pinned
    version: str

    @classmethod
    def read(cls, value: Any, subject: str, bundle: Pinned) -> Tool:
        record = fields(value, {'path', 'sha256', 'version'}, subject)
        if not isinstance(record['version'], str) or not record['version'].strip():
            raise InputError(f'{subject} requires an explicit version')
        pin = Pinned.read({k: record[k] for k in ('path', 'sha256')}, subject)
        if not pin.path.is_relative_to(bundle.path):
            raise InputError(f'{subject} must be inside its pinned bundle')
        return cls(pin, record['version'])


@dataclass(frozen=True)
class Target:
    triple: str
    bundle: Pinned
    sysroot: Pinned
    tools: dict[str, Tool]


@dataclass(frozen=True)
class Inputs:
    manifest: Path
    isolation_kind: str
    isolation_bundle: Pinned
    isolation: Tool
    targets: tuple[Target, ...]
    probe: Tool

    @classmethod
    def read(cls, name: str) -> Inputs:
        path = relative_path(name)
        if path.is_relative_to(relative_path(STATE_NAME, exists=False)):
            raise InputError('manifest overlaps writable experiment state')
        value = fields(json.loads(path.read_text()), {'isolation', 'targets'}, 'inputs: isolation and targets')
        isolation = fields(value['isolation'], {'kind', 'bundle', 'tool', 'probe'}, 'isolation')
        if isolation['kind'] not in ('sandbox-exec', 'bwrap'):
            raise InputError('isolation kind must be sandbox-exec or bwrap')
        bundle = Pinned.read(isolation['bundle'], 'isolation bundle', directory=True)
        tool = Tool.read(isolation['tool'], 'isolation tool', bundle)
        probe_record = fields(isolation['probe'], {'path', 'sha256', 'version', 'contract'}, 'isolation probe')
        if probe_record['contract'] != GUARD_CONTRACT:
            raise InputError(f'isolation probe contract must be {GUARD_CONTRACT}')
        probe = Tool.read({k: probe_record[k] for k in ('path', 'sha256', 'version')}, 'isolation probe', bundle)
        targets = fields(value['targets'], set(TARGETS), 'targets')
        parsed = []
        for triple in TARGETS:
            record = fields(targets[triple], {'bundle', 'sysroot', *TOOLS}, triple)
            target_bundle = Pinned.read(record['bundle'], f'{triple} bundle', directory=True)
            sysroot = Pinned.read(record['sysroot'], f'{triple} sysroot', directory=True)
            runner = fields(record['runner'], {'path', 'sha256', 'version', 'contract'}, f'{triple} runner')
            if runner['contract'] != RUNNER_CONTRACT:
                raise InputError(f'{triple} runner contract must be {RUNNER_CONTRACT}')
            tools = {key: Tool.read({k: record[key][k] for k in ('path', 'sha256', 'version')} if key == 'runner' else record[key], f'{triple} {key}', target_bundle) for key in TOOLS}
            parsed.append(Target(triple, target_bundle, sysroot, tools))
        return cls(path, isolation['kind'], bundle, tool, tuple(parsed), probe)

    def identity(self) -> dict[str, Any]:
        return {'manifest_sha256': sha256(self.manifest), 'source_sha256': source_hashes(),
                'isolation_bundle_sha256': self.isolation_bundle.digest,
                'targets': {t.triple: {'bundle_sha256': t.bundle.digest, 'sysroot_sha256': t.sysroot.digest}
                            for t in self.targets}}


def source_hashes() -> dict[str, str]:
    names = ('probe.py', 'probe.ll', 'runtime.c', 'isolation_probe.c', 'runner.rs', 'guest_init.c',
             'cranelift/Cargo.toml', 'cranelift/Cargo.lock', 'cranelift/src/main.rs')
    return {name: sha256(relative_path(f'experiments/native-backends/{name}')) for name in names}


def state_directory() -> Path:
    path = relative_path(STATE_NAME, exists=False)
    if path.exists() and any(path.iterdir()):
        for child in path.rglob('*'):
            relative_path(child.relative_to(ROOT).as_posix())
    path.mkdir(parents=True, exist_ok=True)
    return path


def write_json(path: Path, value: Any) -> None:
    relative_path(path.relative_to(ROOT).as_posix(), exists=False)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def prepare(inputs: Inputs) -> None:
    state = state_directory()
    write_json(state / 'result.json', {'qualification': 'not-run', 'inputs': inputs.identity(), 'cells': []})
    write_json(state / 'prepared.json', inputs.identity())
    print(json.dumps({'qualification': 'not-run', 'prepared': True, 'receipt': f'{STATE_NAME}/prepared.json'}))


def environment(state: Path) -> dict[str, str]:
    for name in ('home', 'cache', 'tmp', 'work'):
        relative_path((state / name).relative_to(ROOT).as_posix(), exists=False).mkdir(exist_ok=True)
    return {'CI': '1', 'HOME': str(state / 'home'), 'PATH': '', 'TMPDIR': str(state / 'tmp'),
            'TMP': str(state / 'tmp'), 'TEMP': str(state / 'tmp'), 'XDG_CACHE_HOME': str(state / 'cache'),
            'XDG_CONFIG_HOME': str(state / 'home'), 'XDG_DATA_HOME': str(state / 'home'),
            'CARGO_HOME': str(state / 'cache'), 'RUSTUP_HOME': str(state / 'cache'),
            'CLANG_MODULE_CACHE_PATH': str(state / 'cache'), 'SOURCE_DATE_EPOCH': '0',
            'PYTHONDONTWRITEBYTECODE': '1', 'LANG': 'C', 'LC_ALL': 'C'}


def isolated(inputs: Inputs, state: Path, args: list[str], env: dict[str, str]) -> list[str]:
    if inputs.isolation_kind == 'sandbox-exec':
        if sys.platform != 'darwin':
            raise InputError('sandbox-exec requires a Darwin host')
        profile = '\n'.join(['(version 1)', '(deny default (with no-log))', '(allow process-fork process-exec)',
                              '(allow sysctl-read)',
                              f'(allow file-read* (subpath {json.dumps(str(ROOT))}))',
                              '(allow file-read* (subpath "/usr/lib") (subpath "/System/Library"))',
                              f'(allow file-write* (subpath {json.dumps(str(state))}))',
                              '(allow file-read-metadata file-test-existence ' +
                              ' '.join(f'(literal {json.dumps(str(path))})' for path in ROOT.parents) + ')',
                              '(allow file-read-data (literal "/"))',
                              '(deny network* (with no-log))'])
        path = state.parent / 'isolation.sb'
        relative_path(path.relative_to(ROOT).as_posix(), exists=False)
        path.write_text(profile + '\n')
        return [str(inputs.isolation.pin.path), '-f', str(path), *args]
    if sys.platform != 'linux':
        raise InputError('bwrap requires a Linux host')
    command = [str(inputs.isolation.pin.path), '--unshare-all', '--die-with-parent', '--new-session',
               '--cap-drop', 'ALL', '--ro-bind', str(ROOT), str(ROOT), '--bind', str(state), str(state),
               '--chdir', str(state / 'work'), '--clearenv']
    for key, value in sorted(env.items()):
        command.extend(['--setenv', key, value])
    return [*command, '--', *args]


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def execute(inputs: Inputs, state: Path, args: list[str], env: dict[str, str]) -> dict[str, Any]:
    relative_path((state / 'work').relative_to(ROOT).as_posix())
    for child in state.rglob('*'):
        relative_path(child.relative_to(ROOT).as_posix())
    command = isolated(inputs, state, args, env)
    started = time.monotonic()
    process = subprocess.Popen(command, cwd=state / 'work', env=env, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True, preexec_fn=child_limits)
    try:
        stdout, stderr = process.communicate(timeout=120)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.communicate()
        raise InputError('isolated command timed out; process group stopped') from None
    return {'command': command, 'exit_code': process.returncode, 'stdout': stdout,
            'stderr': stderr, 'seconds': round(time.monotonic() - started, 6)}


def inspect_artifact(path: Path, target: str, kind: str) -> dict[str, Any]:
    relative_path(path.relative_to(ROOT).as_posix(), exists=False)
    if not path.is_file():
        raise InputError(f'missing artifact: {path.relative_to(ROOT)}')
    data = path.read_bytes()
    if not data:
        raise InputError(f'empty artifact: {path.relative_to(ROOT)}')
    if target == TARGETS[0]:
        if len(data) < 32 or data[:4] != bytes.fromhex('cffaedfe'):
            raise InputError('artifact requires little-endian 64-bit Mach-O')
        cpu, _, filetype, count, size = struct.unpack_from('<IIIII', data, 4)
        if cpu != 0x0100000c or filetype != (1 if kind == 'object' else 2):
            raise InputError('Mach-O artifact has incorrect arm64 target or file type')
        if count == 0 or size == 0 or 32 + size > len(data):
            raise InputError('Mach-O artifact has invalid load-command bounds')
        offset = 32
        for _ in range(count):
            if offset + 8 > 32 + size:
                raise InputError('Mach-O artifact has truncated load commands')
            command_size = struct.unpack_from('<I', data, offset + 4)[0]
            if command_size < 8 or command_size % 8 or offset + command_size > 32 + size:
                raise InputError('Mach-O artifact has invalid load-command size')
            offset += command_size
        if offset != 32 + size:
            raise InputError('Mach-O artifact has incorrect load-command count')
        file_format = 'Mach-O'
    else:
        if len(data) < 64 or data[:7] != b'\x7fELF\x02\x01\x01':
            raise InputError('artifact requires little-endian 64-bit ELF')
        if struct.unpack_from('<I', data, 20)[0] != 1 or struct.unpack_from('<H', data, 52)[0] != 64:
            raise InputError('ELF artifact has incorrect header version or size')
        filetype, machine = struct.unpack_from('<HH', data, 16)
        if machine != 183 or filetype not in ((1,) if kind == 'object' else (2, 3)):
            raise InputError('ELF artifact has incorrect arm64 target or file type')
        if kind == 'object':
            offset = struct.unpack_from('<Q', data, 40)[0]
            entry_size, count = struct.unpack_from('<HH', data, 58)
            minimum = 64
        else:
            offset = struct.unpack_from('<Q', data, 32)[0]
            entry_size, count = struct.unpack_from('<HH', data, 54)
            minimum = 56
        if offset < 64 or entry_size < minimum or count == 0 or offset + entry_size * count > len(data):
            raise InputError('ELF artifact has invalid table bounds')
        file_format = 'ELF'
    return {'sha256': hashlib.sha256(data).hexdigest(), 'bytes': len(data), 'format': file_format, 'target': target, 'kind': kind}


def run(inputs: Inputs, execute_command: Callable[[Inputs, Path, list[str], dict[str, str]], dict[str, Any]] = execute) -> int:
    parent = state_directory()
    report: dict[str, Any] = {'qualification': 'not-run', 'inputs': inputs.identity(),
                              'expected_stdout': EXPECTED, 'historical_evidence_reused': False, 'cells': []}
    write_json(parent / 'result.json', report)
    try:
        receipt = relative_path(f'{STATE_NAME}/prepared.json')
        if json.loads(receipt.read_text()) != inputs.identity():
            raise InputError('stale preparation: run prepare with current pinned inputs')
        return run_prepared(inputs, parent, report, execute_command)
    except (InputError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        report['qualification'] = 'failed'
        report['error'] = str(error)
        write_json(parent / 'result.json', report)
        raise


def run_prepared(inputs: Inputs, parent: Path, report: dict[str, Any],
                 execute_command: Callable[[Inputs, Path, list[str], dict[str, str]], dict[str, Any]]) -> int:
    state = relative_path(f'{STATE_NAME}/execution', exists=False)
    state.mkdir(exist_ok=True)
    env = environment(state)
    sentinel = relative_path(f'{STATE_NAME}/readonly-sentinel', exists=False)
    sentinel.write_text('isolation sentinel\n')
    sentinel_hash = sha256(sentinel)
    writable = relative_path(f'{STATE_NAME}/execution/guard-write', exists=False)
    writable.write_text('guard state\n')
    report['isolation_probe'] = {'status': 'running'}
    guard = execute_command(inputs, state, [str(inputs.probe.pin.path), str(sentinel), str(state)], env)
    report['isolation_probe'] = guard
    relative_path(sentinel.relative_to(ROOT).as_posix())
    if guard['exit_code'] != 0 or sha256(sentinel) != sentinel_hash or writable.exists():
        raise InputError('isolation denial probe failed; backend execution blocked')
    try:
        guard_result = json.loads(guard['stdout'])
    except ValueError:
        raise InputError('isolation denial probe returned invalid JSON; backend execution blocked') from None
    if guard_result != GUARD_RESULT:
        raise InputError('isolation denial probe did not establish containment; backend execution blocked')
    for target in inputs.targets:
        for backend in BACKENDS:
            stem = f'{backend}-{target.triple}'
            object_path = state / f'{stem}.o'
            runtime = state / f'{stem}-runtime.o'
            executable = state / stem
            for artifact in (object_path, runtime, executable):
                relative_path(artifact.relative_to(ROOT).as_posix(), exists=False)
                if artifact.exists():
                    artifact.unlink()
            tool = target.tools
            compile_flags = [f'--target={target.triple}', f'--sysroot={target.sysroot.path}', '-g']
            if target.triple == TARGETS[0]:
                compile_flags.append('-mmacosx-version-min=27.0')
                link = [str(tool['linker'].pin.path), '-arch', 'arm64', '-platform_version',
                        'macos', '27.0', '27.0', '-syslibroot', str(target.sysroot.path),
                        '-lSystem', str(runtime), str(object_path), '-o', str(executable)]
            else:
                library = target.sysroot.path / 'usr/lib/aarch64-linux-gnu'
                link = [str(tool['linker'].pin.path), '-m', 'aarch64elf',
                        f'--sysroot={target.sysroot.path}', '-dynamic-linker', '/lib/ld-linux-aarch64.so.1',
                        str(library / 'crt1.o'), str(library / 'crti.o'), str(runtime), str(object_path),
                        f'-L{library}', f'-L{target.sysroot.path / "lib/aarch64-linux-gnu"}', '-lc',
                        str(library / 'crtn.o'), '-o', str(executable)]
            emitter = ([str(tool['llvm'].pin.path), f'-mtriple={target.triple}', '-filetype=obj',
                        str(SOURCE / 'probe.ll'), '-o', str(object_path)] if backend == 'llvm' else
                       [str(tool['cranelift'].pin.path), target.triple, str(object_path)])
            stages = [('object', emitter),
                      ('runtime_object', [str(tool['cc'].pin.path), *compile_flags, '-c',
                                          str(SOURCE / 'runtime.c'), '-o', str(runtime)]),
                      ('link', link),
                      ('run', [str(tool['runner'].pin.path), str(executable), str(target.sysroot.path)])]
            cell: dict[str, Any] = {'target': target.triple, 'backend': backend, 'passed': False, 'stages': {}}
            report['cells'].append(cell)
            for name, args in stages:
                cell['stages'][name] = {'status': 'running', 'command': args}
                result = execute_command(inputs, state, args, env)
                cell['stages'][name] = result
                if result['exit_code'] != 0:
                    break
                if name != 'run':
                    artifact = {'object': object_path, 'runtime_object': runtime, 'link': executable}[name]
                    result['artifact'] = inspect_artifact(artifact, target.triple, 'executable' if name == 'link' else 'object')
                if name == 'run':
                    cell['passed'] = result['stdout'] == EXPECTED
            if not cell['passed']:
                report['qualification'] = 'failed'
                write_json(parent / 'result.json', report)
                print(json.dumps({'qualification': 'failed', 'result': f'{STATE_NAME}/result.json'}))
                return 1
    report['qualification'] = 'passed'
    write_json(parent / 'result.json', report)
    print(json.dumps({'qualification': 'passed', 'cells': 4, 'result': f'{STATE_NAME}/result.json'}))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest='action', required=True)
    commands.add_parser('check')
    inspector = commands.add_parser('inspect-artifact')
    inspector.add_argument('--path', required=True)
    inspector.add_argument('--target', required=True, choices=TARGETS)
    inspector.add_argument('--kind', required=True, choices=('object', 'executable'))
    for action in ('prepare', 'run'):
        commands.add_parser(action).add_argument('--inputs', required=True)
    args = parser.parse_args()
    if args.action == 'inspect-artifact':
        print(json.dumps(inspect_artifact(relative_path(args.path), args.target, args.kind)))
        return 0
    if args.action == 'check':
        source_hashes()
        print(json.dumps({'qualification': 'not-run', 'required_inputs': ['isolation', *TARGETS],
                          'expected_stdout': EXPECTED, 'backends': BACKENDS}))
        return 0
    inputs = Inputs.read(args.inputs)
    if args.action == 'prepare':
        prepare(inputs)
        return 0
    return run(inputs)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (InputError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        sys.stderr.write(f'native backend probe: {error}\n')
        sys.exit(1)
