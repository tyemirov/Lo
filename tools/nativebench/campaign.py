"""Run eight complete timing lanes serially and retain every valid gate outcome."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
import re
from pathlib import Path
import signal
import subprocess
import sys
import time
from types import FrameType
from typing import Any, Callable

from identity import LOAD_POLICY, file_identity, package_provenance, source_provenance
from merge import admit, local, merge
from qualification import SAMPLE_PLAN, LANES, WARMUP, completion

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_PACKAGES = ROOT / '_scratch/compiler-distribution/packages'
LOCK = ROOT / '_scratch/nativebench-campaign.lock'
PRODUCERS = {
    'darwin': ('scripts/qualify_native.py', 'tools/nativecorpus/main.go'),
    'linux': ('tools/loqualification/performance.py', 'tools/loqualification/performance-runner/main.go',
              'tools/loqualification/batch.py', 'tools/loqualification/batch_init.c',
              'tools/loqualification/suite.py', 'tools/lodistribution/prepare.py',
              'tools/nativecorpus/main.go'),
}


def stamp() -> dict[str, Any]:
    return {'utc': datetime.now(timezone.utc).isoformat(), 'monotonic_ns': time.monotonic_ns(),
            'load_average': list(os.getloadavg())}


def save(path: Path, value: dict[str, Any]) -> None:
    temporary = path.with_name('.' + path.name + '.tmp')
    temporary.write_text(json.dumps(value, indent=2, sort_keys=True, allow_nan=False) + '\n')
    temporary.replace(path)


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def outcome(report: dict[str, Any]) -> str:
    failures = report['gate_failures']
    if not failures:
        return 'pass'
    return 'inconclusive' if all(item.endswith(':inconclusive') for item in failures) else 'fail'


@dataclass(frozen=True)
class CleanupResult:
    remaining_owned_members: tuple[int, ...] | None
    direct_child_status: int | None
    signal_observations: tuple[str, ...] = ()


class CleanupError(ValueError):
    def __init__(self, message: str, result: CleanupResult):
        super().__init__(message)
        self.result = result


def owned_members(group: int) -> tuple[int, ...]:
    """Observe PID/PGID membership; an invalid inventory never means absence."""
    command = ['ps', '-axo', 'pid=,pgid=']
    result = subprocess.run(command, cwd=ROOT, env=dict(os.environ, CI='1', LC_ALL='C'),
                            stdin=subprocess.DEVNULL, capture_output=True, text=True,
                            encoding='ascii', errors='strict', timeout=2, check=False)
    if result.returncode:
        raise ValueError(f'process inventory status {result.returncode}: {result.stderr[:512]}')
    if not result.stdout or len(result.stdout) > 1 << 20:
        raise ValueError('process inventory is empty or exceeds the size limit')
    seen: set[int] = set()
    members: list[int] = []
    for line in result.stdout.splitlines():
        match = re.fullmatch(r'\s*([0-9]+)\s+([0-9]+)\s*', line)
        if match is None:
            raise ValueError('process inventory requires PID and PGID integer columns')
        pid, pgid = (int(value) for value in match.groups())
        if pid in seen:
            raise ValueError(f'process inventory contains duplicate PID {pid}')
        seen.add(pid)
        if pgid == group:
            members.append(pid)
    return tuple(sorted(members))


def stop(process: subprocess.Popen[Any]) -> CleanupResult:
    """Bound observation, signals, group quiescence, and direct-child reaping."""
    errors: list[str] = []
    observations: list[str] = []

    def observe() -> tuple[int, ...] | None:
        try:
            return owned_members(process.pid)
        except (OSError, ValueError, UnicodeError, subprocess.SubprocessError) as error:
            errors.append(f'owned-group observation failed: {error}')
            return None

    def send(number: signal.Signals, members: tuple[int, ...]) -> tuple[int, ...] | None:
        try:
            os.killpg(process.pid, number)
        except ProcessLookupError as error:
            observed = observe()
            confirmed = observed == ()
            observations.append(f'{number.name} for owned group {process.pid}: ESRCH; ' +
                                ('confirmed empty' if confirmed else 'not confirmed empty'))
            if not confirmed:
                errors.append(f'{number.name} disappearance not confirmed empty: {error}')
            return observed
        except OSError as error:
            errors.append(f'{number.name} failed for owned group {process.pid}: {error}')
        return members

    def quiesce(members: tuple[int, ...], seconds: float) -> tuple[int, ...] | None:
        deadline = time.monotonic() + seconds
        while members and time.monotonic() < deadline:
            process.poll()
            time.sleep(.01)
            observed = observe()
            if observed is None:
                return None
            members = observed
        return members

    members = observe()
    if members:
        members = send(signal.SIGTERM, members)
        if members:
            members = quiesce(members, 5)
        if members:
            members = send(signal.SIGKILL, members)
            if members:
                members = quiesce(members, 2)
    if members:
        errors.append(f'owned group did not become empty: {list(members)}')
    try:
        status = process.wait(timeout=2)
    except (OSError, subprocess.SubprocessError) as error:
        errors.append(f'direct-child reap failed: {error}')
        status = process.returncode
    result = CleanupResult(members, status, tuple(observations))
    if errors:
        raise CleanupError('; '.join(errors), result)
    return result


def snapshot(args: argparse.Namespace) -> dict[str, Any]:
    inputs: dict[str, str] = {}
    packages: dict[str, dict[str, str]] = {}
    for target in PRODUCERS:
        package = getattr(args, target + '_package')
        identity, files = package_provenance(ROOT, package / 'bin/lo', package / 'toolchain.json', target + '/arm64')
        packages[target] = identity
        inputs.update(files)
    preparation = args.preparation
    names = ('tools.json', 'emulation/qemu-system-aarch64', 'vmlinuz-virt',
             'emulation/lib', 'linux-sysroot/lib',
             *(f'linux-sysroot/usr/lib/aarch64-linux-gnu/{name}' for name in ('crt1.o', 'crti.o', 'crtn.o', 'libc_nonshared.a')))
    for path in [args.frontend, *(preparation / name for name in names)]:
        path = local(path)
        if path.is_dir():
            for child in path.rglob('*'):
                local(child)
        inputs[str(path)] = file_identity(path)
    return {'inputs': inputs, 'packages': packages,
            'source_identity': {target: source_provenance(ROOT, names) for target, names in PRODUCERS.items()},
            'campaign_sources': {str(path.relative_to(ROOT)): sha(path) for path in
                                 (Path(__file__), ROOT / 'Makefile', ROOT / 'tools/nativebench/merge.py')}}


def command(args: argparse.Namespace, target: str, lane: str, work: Path) -> list[str]:
    return ['make', '--no-print-directory',
            'benchmark-standalone' if target == 'darwin' else 'benchmark-native-linux',
            f'NATIVE_BENCH_LANE={lane}', f'NATIVE_BENCH_WORK={work}',
            f'NATIVE_BENCH_COMPILER={args.darwin_package / "bin/lo"}',
            f'COMPILER_DISTRIBUTION_OUTPUT={args.linux_package.parent}',
            f'NATIVE_LINUX_PACKAGE={args.linux_package}',
            f'NATIVE_RUNTIME_STATE={args.preparation}', f'SEMANTIC_COMPILER={args.frontend}',
            f'LINUX_ACCELERATOR={args.accelerator}', 'NATIVE_BENCH_ARGS=']


def run(args: argparse.Namespace, popen: Callable[..., subprocess.Popen[Any]]) -> int:
    work = args.work
    if work.exists():
        raise ValueError(f'campaign output must be fresh: {work}')
    work.mkdir(parents=True)
    report: dict[str, Any] = {'state': 'preparing', 'measurement_complete': False,
                              'qualification_outcome': None, 'subprocesses': [], 'merges': [], 'start': stamp()}
    report_path = work / 'report.json'
    save(report_path, report)
    process: subprocess.Popen[Any] | None = None
    active: dict[str, Any] | None = None
    try:
        frozen = snapshot(args)
        config = {'sample_plan': dict(SAMPLE_PLAN), 'warmup': WARMUP, 'concurrency': 1,
                  'load_policy': LOAD_POLICY,
                  'accelerator': args.accelerator,
                  'paths': {name: str(getattr(args, name)) for name in
                            ('darwin_package', 'linux_package', 'preparation', 'frontend', 'work')},
                  'lanes': list(LANES), **frozen}
        save(work / 'config.json', config)
        report.update(state='measuring', config_sha256=sha(work / 'config.json'))
        save(report_path, report)
        lane_paths: dict[str, list[Path]] = {target: [] for target in PRODUCERS}
        for target in PRODUCERS:
            for lane in LANES:
                lane_work = work / f'{target}-{lane}'
                stdout = work / f'{target}-{lane}.stdout'
                stderr = work / f'{target}-{lane}.stderr'
                argv = command(args, target, lane, lane_work)
                active = {'target': target, 'lane': lane, 'command': argv, 'start': stamp(), 'status': None,
                          'stdout': str(stdout.relative_to(ROOT)), 'stderr': str(stderr.relative_to(ROOT)),
                          'report': str((lane_work / 'report.json').relative_to(ROOT)),
                          'report_sha256': None, 'stdout_sha256': None, 'stderr_sha256': None,
                          'measurement_complete': False, 'qualification_outcome': None}
                report['remaining_owned_members'] = None
                report['subprocesses'].append(active)
                save(report_path, report)
                with stdout.open('wb') as out, stderr.open('wb') as err:
                    process = popen(argv, cwd=ROOT, env=dict(os.environ, CI='1'),
                                    stdin=subprocess.DEVNULL, stdout=out, stderr=err, start_new_session=True)
                    active['pid'] = process.pid
                    save(report_path, report)
                    status = process.wait()
                    active['status'] = status
                try:
                    cleaned = stop(process)
                    report.setdefault('cleanup_signal_observations', []).extend(cleaned.signal_observations)
                    report['remaining_owned_members'] = list(cleaned.remaining_owned_members)
                except CleanupError as error:
                    report['cleanup_error'] = str(error)
                    report.setdefault('cleanup_signal_observations', []).extend(error.result.signal_observations)
                    report['remaining_owned_members'] = (None if error.result.remaining_owned_members is None
                                                         else list(error.result.remaining_owned_members))
                    active['status'] = error.result.direct_child_status
                    raise
                finally:
                    process = None
                active.update(status=status, finish=stamp(), stdout_sha256=sha(stdout), stderr_sha256=sha(stderr))
                path = lane_work / 'report.json'
                if path.is_file():
                    active['report_sha256'] = sha(path)
                    active['report'] = str(path.relative_to(ROOT))
                save(report_path, report)
                if status not in (0, 2):
                    raise ValueError(f'{target} {lane}: subprocess status {status}')
                measured, _ = admit(path)
                if measured['merge_identity']['target'] != target + '/arm64' or measured.get('lanes') != [lane]:
                    raise ValueError(f'{target} {lane}: unexpected target or lane')
                identity = measured['merge_identity']
                if identity['accelerator'] != ('none' if target == 'darwin' else args.accelerator):
                    raise ValueError(f'{target} {lane}: accelerator differs from campaign configuration')
                declared = frozen['source_identity'][target]
                if (identity['package'] != frozen['packages'][target]
                        or any(identity[key] != declared[key] for key in ('source', 'harness', 'evaluator'))
                        or identity['producer']['source_identity'] != declared['producer']['source_identity']
                        or identity['reference']['source_identity'] != declared['reference']['source_identity']):
                    raise ValueError(f'{target} {lane}: report differs from predeclared provenance')
                if identity['measurement']['sample_plan'] != SAMPLE_PLAN or identity['measurement']['warmup'] != 3:
                    raise ValueError(f'{target} {lane}: sample contract differs')
                completion(measured, 0)
                if not measured['measurement_complete']:
                    raise ValueError(f'{target} {lane}: {measured.get("measurement_error")}')
                if status != (0 if measured['gates_passed'] else 2):
                    raise ValueError(f'{target} {lane}: subprocess status disagrees with recomputed gates')
                if snapshot(args) != frozen or sha(work / 'config.json') != report['config_sha256']:
                    raise ValueError('campaign inputs or source changed during execution')
                active['measurement_complete'] = True
                active['qualification_outcome'] = outcome(measured)
                active['gate_failures'] = measured['gate_failures']
                lane_paths[target].append(path)
                save(report_path, report)
        for target, paths in lane_paths.items():
            merged = merge(paths)
            output = work / (target + '-merged.json')
            save(output, merged)
            report['merges'].append({'target': target, 'report': str(output.relative_to(ROOT)),
                                     'report_sha256': sha(output), 'measurement_complete': True,
                                     'qualification_outcome': outcome(merged), 'gate_failures': merged['gate_failures']})
            save(report_path, report)
        if snapshot(args) != frozen:
            raise ValueError('campaign inputs or source changed during execution')
        outcomes = [record['qualification_outcome'] for record in report['merges']]
        result = 'fail' if 'fail' in outcomes else 'inconclusive' if 'inconclusive' in outcomes else 'pass'
        report.update(state='complete', measurement_complete=True, qualification_outcome=result)
        return 0 if result == 'pass' else 1
    except KeyboardInterrupt:
        report.update(state='interrupted', error='campaign interrupted; measurements are incomplete')
        return 130
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        report.update(state='invalid', error=str(error))
        return 2
    finally:
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        finalization_errors: list[str] = []
        try:
            if process is not None:
                try:
                    cleaned = stop(process)
                    report.setdefault('cleanup_signal_observations', []).extend(cleaned.signal_observations)
                    report['remaining_owned_members'] = list(cleaned.remaining_owned_members)
                except CleanupError as error:
                    finalization_errors.append(str(error))
                    report.setdefault('cleanup_signal_observations', []).extend(error.result.signal_observations)
                    report['remaining_owned_members'] = (None if error.result.remaining_owned_members is None
                                                         else list(error.result.remaining_owned_members))
                except Exception as error:
                    finalization_errors.append(f'owned cleanup failed: {error}')
                    report['remaining_owned_members'] = None
            if active is not None:
                if process is not None:
                    active['status'] = process.returncode
                try:
                    active['finish'] = stamp()
                except Exception as error:
                    finalization_errors.append(f'child finish context failed: {error}')
                for stream in ('stdout', 'stderr'):
                    try:
                        active[stream + '_sha256'] = sha(ROOT / active[stream])
                    except Exception as error:
                        finalization_errors.append(f'{stream} artifact hashing failed: {error}')
                try:
                    path = work / f'{active["target"]}-{active["lane"]}' / 'report.json'
                    if path.is_file():
                        active['report'] = str(path.relative_to(ROOT))
                        active['report_sha256'] = sha(path)
                except Exception as error:
                    finalization_errors.append(f'child report hashing failed: {error}')
            try:
                report['finish'] = stamp()
            except Exception as error:
                finalization_errors.append(f'campaign finish context failed: {error}')
        finally:
            invalid_finalization = bool(finalization_errors) and report['state'] == 'complete'
            if finalization_errors:
                report['cleanup_error'] = '; '.join(filter(None, [report.get('cleanup_error'), *finalization_errors]))
            if invalid_finalization:
                report.update(state='invalid', measurement_complete=False, qualification_outcome=None,
                              error='campaign finalization failed')
            save(report_path, report)
            if invalid_finalization:
                return 2


def main(argv: list[str] | None = None, *, popen: Callable[..., subprocess.Popen[Any]] = subprocess.Popen) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', required=True, type=Path)
    parser.add_argument('--darwin-package', type=Path, default=DEFAULT_PACKAGES / 'lo-darwin-arm64-software')
    parser.add_argument('--linux-package', type=Path, default=DEFAULT_PACKAGES / 'lo-linux-arm64-software')
    parser.add_argument('--preparation', type=Path, default=ROOT / '_scratch/native-runtime')
    parser.add_argument('--frontend', type=Path, default=DEFAULT_PACKAGES / 'lo-darwin-arm64-software/bin/lo')
    parser.add_argument('--accelerator', choices=('tcg', 'hvf'), default='tcg')
    args = parser.parse_args(argv)
    safe_work: Path | None = None
    try:
        safe_work = local(args.work)
        for name in ('work', 'darwin_package', 'linux_package', 'preparation', 'frontend'):
            setattr(args, name, local(getattr(args, name)))
        lock_path = local(LOCK)
        lock_path.parent.mkdir(parents=True, exist_ok=True)
        with lock_path.open('a') as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise ValueError('another owned benchmark campaign holds the repository lock') from error
            previous = {number: signal.getsignal(number) for number in (signal.SIGINT, signal.SIGTERM)}
            def interrupt(_number: int, _frame: FrameType | None) -> None:
                raise KeyboardInterrupt
            signal.signal(signal.SIGTERM, interrupt)
            try:
                status = run(args, popen)
            finally:
                for number, handler in previous.items():
                    signal.signal(number, handler)
            if status:
                print(f'Native benchmark campaign: {args.work / "report.json"}', file=sys.stderr)
            return status
    except (OSError, ValueError) as error:
        if safe_work is not None and not safe_work.exists():
            safe_work.mkdir(parents=True)
            save(safe_work / 'report.json', {'state': 'invalid', 'measurement_complete': False,
                 'qualification_outcome': None, 'subprocesses': [], 'merges': [],
                 'error': str(error), 'start': stamp(), 'finish': stamp()})
        print(f'Native benchmark campaign: {error}', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
