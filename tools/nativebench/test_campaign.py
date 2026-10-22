"""Public campaign decisions with injected measurement processes and real merges.

Synthetic reports prove orchestration contracts, not benchmark performance.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

import pytest

from campaign import owned_members
from qualification import LANES, completion
from identity import package_provenance, source_provenance, digest
from test_merge import lane_report

ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture
def campaign_inputs(tmp_path: Path) -> tuple[Path, list[str], dict[str, str]]:
    tmp_path.resolve().relative_to(ROOT)
    for name in ('darwin', 'linux', 'preparation'):
        (tmp_path / name).mkdir()
        (tmp_path / name / 'input').write_text(name)
    for target in ('darwin', 'linux'):
        package = tmp_path / target
        for name in ('bin/lo', 'runtime/liblo.a', 'inventory.json', 'provenance.json'):
            path = package / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('controlled ' + name)
        (package / 'toolchain.json').write_text(json.dumps({'host': target + '/arm64', 'targets': {
            target + '/arm64': {'runtime': {'path': 'runtime/liblo.a', 'sha256': digest(package / 'runtime/liblo.a')}}}}))
    for name in ('tools.json', 'emulation/qemu-system-aarch64', 'vmlinuz-virt',
                 'emulation/lib/library', 'linux-sysroot/lib/library',
                 *(f'linux-sysroot/usr/lib/aarch64-linux-gnu/{name}' for name in ('crt1.o', 'crti.o', 'crtn.o', 'libc_nonshared.a'))):
        path = tmp_path / 'preparation' / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(name)
    frontend = tmp_path / 'frontend'
    frontend.write_text('frontend')
    for target in ('darwin', 'linux'):
        for lane in LANES:
            record = lane_report(lane)
            producers = {
                'darwin': ('scripts/qualify_native.py', 'tools/nativecorpus/main.go'),
                'linux': ('tools/loqualification/performance.py', 'tools/loqualification/performance-runner/main.go',
                          'tools/loqualification/batch.py', 'tools/loqualification/batch_init.c',
                          'tools/loqualification/suite.py', 'tools/lodistribution/prepare.py', 'tools/nativecorpus/main.go')}
            provenance = source_provenance(ROOT, producers[target])
            identity = record['merge_identity']
            for name in ('source', 'harness', 'evaluator'):
                identity[name] = provenance[name]
            identity['producer']['source_identity'] = provenance['producer']['source_identity']
            identity['reference']['source_identity'] = provenance['reference']['source_identity']
            package = tmp_path / target
            identity['package'], _ = package_provenance(ROOT, package / 'bin/lo', package / 'toolchain.json', target + '/arm64')
            if target == 'linux':
                identity = record['merge_identity']
                identity['target'] = record['target'] = 'linux/arm64'
                identity['environment'].update(system='Linux fixture', execution='qemu-guest')
                identity['accelerator'] = 'tcg'
                identity['producer']['kind'] = 'linux-guest'
            record['other_benchmark_jobs'] = ['synthetic external load; retained, not an admission gate']
            completion(record, 0)
            (tmp_path / f'{target}-{lane}.json').write_text(json.dumps(record))
    producer = tmp_path / 'producer.py'
    producer.write_text('''import json, os, pathlib, subprocess, sys, time
args = sys.argv[1:]
values = dict(arg.split('=', 1) for arg in args if '=' in arg)
work = pathlib.Path(values['NATIVE_BENCH_WORK']); work.mkdir(parents=True)
target = 'linux' if 'benchmark-native-linux' in args else 'darwin'
lane = values['NATIVE_BENCH_LANE']
assert os.environ['CI'] == '1'
assert sys.stdin.read() == ''
root = pathlib.Path(os.environ['CAMPAIGN_FIXTURES'])
with (root / 'calls').open('a') as f: f.write(target + ':' + lane + '\\n')
if os.environ.get('CAMPAIGN_MODE') == 'wait':
    if os.environ.get('CAMPAIGN_DESCENDANT') == '1':
        subprocess.Popen([sys.executable, '-B', str(root / 'descendant.py')], stdin=subprocess.DEVNULL)
    (root / 'child-pid').write_text(str(os.getpid()))
    time.sleep(30)
record = json.loads((root / (target + '-' + lane + '.json')).read_text())
mode = os.environ.get('CAMPAIGN_MODE')
if mode == 'bad': record['measurement_complete'] = False
if mode == 'foreign': record['merge_identity']['package']['compiler_sha256'] = 'f' * 64
if mode == 'changed-input': (root / 'frontend').write_text('mutated input')
if mode == 'missing': raise SystemExit(7)
(work / 'report.json').write_text('{' if mode == 'malformed' else json.dumps(record))
raise SystemExit(1 if mode == 'lie' else 0 if record['gates_passed'] else 1)
''')
    makefile = tmp_path / 'fixture.mk'
    makefile.write_text('benchmark-standalone benchmark-native-linux:\n\t' + sys.executable +
                        ' -B ' + str(producer) + ' $@ "NATIVE_BENCH_WORK=$(NATIVE_BENCH_WORK)" "NATIVE_BENCH_LANE=$(NATIVE_BENCH_LANE)"\n')
    wrapper = tmp_path / 'entry.py'
    wrapper.write_text('''import pathlib, subprocess, sys
sys.path.insert(0, str(pathlib.Path.cwd() / 'tools/nativebench'))
import campaign
producer = pathlib.Path(__file__).with_name('producer.py')
def spawn(command, **kwargs):
    return subprocess.Popen(['make', '--no-print-directory', '-f', str(producer.with_name('fixture.mk')), *command[2:]], **kwargs)
raise SystemExit(campaign.main(sys.argv[1:], popen=spawn))
''')
    args = [sys.executable, '-B', str(wrapper), '--work', str(tmp_path / 'output'),
            '--darwin-package', str(tmp_path / 'darwin'), '--linux-package', str(tmp_path / 'linux'),
            '--preparation', str(tmp_path / 'preparation'), '--frontend', str(frontend)]
    return tmp_path, args, dict(os.environ, CI='1', CAMPAIGN_FIXTURES=str(tmp_path))


def invoke(inputs: tuple[Path, list[str], dict[str, str]]) -> subprocess.CompletedProcess[str]:
    _, args, env = inputs
    return subprocess.run(args, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                          capture_output=True, text=True, check=False)


def test_campaign_runs_eight_serial_lanes_and_both_strict_merges(campaign_inputs) -> None:
    work, _, _ = campaign_inputs
    result = invoke(campaign_inputs)
    assert result.returncode == 0, result.stderr
    report = json.loads((work / 'output/report.json').read_text())
    assert report['measurement_complete'] is True
    assert report['qualification_outcome'] == 'pass'
    assert len(report['subprocesses']) == 8
    assert len(report['merges']) == 2
    assert (work / 'calls').read_text().splitlines() == [f'{target}:{lane}' for target in ('darwin', 'linux') for lane in LANES]
    for run in report['subprocesses']:
        assert run['status'] == 0 and len(run['report_sha256']) == 64
        assert len(run['stdout_sha256']) == len(run['stderr_sha256']) == 64
    config = json.loads((work / 'output/config.json').read_text())
    assert config['samples'] == 10 and config['warmup'] == 3
    assert config['inputs'] and config['source_identity']
    assert invoke(campaign_inputs).returncode != 0


def test_campaign_keeps_valid_failed_gates_and_completes_both_targets(campaign_inputs) -> None:
    work, _, _ = campaign_inputs
    path = work / 'darwin-process.json'
    record = json.loads(path.read_text())
    for case in record['process']:
        for pair in case['samples_ns']:
            pair['native'] = 200
    completion(record, 0)
    path.write_text(json.dumps(record))
    result = invoke(campaign_inputs)
    assert result.returncode == 1, result.stderr
    report = json.loads((work / 'output/report.json').read_text())
    assert report['measurement_complete'] is True
    assert report['qualification_outcome'] == 'fail'
    assert len(report['subprocesses']) == 8 and len(report['merges']) == 2
    assert report['subprocesses'][1]['status'] == 2


@pytest.mark.parametrize('mode', ['bad', 'foreign', 'changed-input', 'missing', 'malformed', 'lie'])
def test_campaign_stops_invalid_report_and_keeps_explicit_failure(campaign_inputs, mode) -> None:
    work, _, env = campaign_inputs
    env['CAMPAIGN_MODE'] = mode
    result = invoke(campaign_inputs)
    assert result.returncode == 2, result.stderr
    report = json.loads((work / 'output/report.json').read_text())
    assert report['measurement_complete'] is False
    assert report['state'] == 'invalid'
    assert len(report['subprocesses']) == 1 and not report['merges']
    assert report['error']


def wait_for(path: Path) -> None:
    deadline = time.monotonic() + 10
    while not path.exists():
        if time.monotonic() > deadline:
            pytest.fail(f'missing owned process receipt: {path}')
        time.sleep(.01)


@pytest.mark.parametrize('descendant', [False, True])
def test_campaign_lock_and_sigint_only_stop_owned_children(campaign_inputs, descendant) -> None:
    work, args, env = campaign_inputs
    env['CAMPAIGN_MODE'] = 'wait'
    if descendant:
        env['CAMPAIGN_DESCENDANT'] = '1'
        (work / 'descendant.py').write_text(
            "import os, pathlib, signal, time\nsignal.signal(signal.SIGTERM, signal.SIG_IGN)\n"
            "pathlib.Path(__file__).with_name('descendant-pid').write_text(str(os.getpid()))\ntime.sleep(30)\n")
    unrelated = subprocess.Popen([sys.executable, '-B', '-c', 'import time; time.sleep(30)'], cwd=ROOT,
                                 stdin=subprocess.DEVNULL)
    first = subprocess.Popen(args, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        wait_for(work / 'child-pid')
        if descendant:
            wait_for(work / 'descendant-pid')
        second = args.copy()
        second[second.index('--work') + 1] = str(work / 'other-output')
        blocked = subprocess.run(second, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                                 capture_output=True, text=True)
        assert blocked.returncode == 2 and 'lock' in blocked.stderr.lower()
        first.send_signal(signal.SIGINT)
        _, stderr = first.communicate(timeout=10)
        assert first.returncode == 130, stderr
        report = json.loads((work / 'output/report.json').read_text())
        assert report['state'] == 'interrupted' and not report['measurement_complete']
        child = int((work / 'child-pid').read_text())
        with pytest.raises(ProcessLookupError):
            os.kill(child, 0)
        if descendant:
            pid = int((work / 'descendant-pid').read_text())
            deadline = time.monotonic() + 3
            while True:
                try:
                    os.kill(pid, 0)
                except ProcessLookupError:
                    break
                if time.monotonic() > deadline:
                    pytest.fail('owned TERM-resistant descendant survived interruption')
                time.sleep(.01)
        assert unrelated.poll() is None
    finally:
        if first.poll() is None:
            first.kill(); first.wait()
        if descendant and (work / 'descendant-pid').exists():
            try:
                os.kill(int((work / 'descendant-pid').read_text()), signal.SIGKILL)
            except ProcessLookupError:
                pass
        unrelated.terminate(); unrelated.wait()


def test_real_make_failed_recipe_returns_two_for_complete_failed_lane(campaign_inputs) -> None:
    work, _, env = campaign_inputs
    path = work / 'darwin-process.json'
    record = json.loads(path.read_text())
    for case in record['process']:
        for pair in case['samples_ns']:
            pair['native'] = 200
    completion(record, 0)
    path.write_text(json.dumps(record))
    result = subprocess.run(['make', '--no-print-directory', '-f', str(work / 'fixture.mk'),
                             'benchmark-standalone', 'NATIVE_BENCH_LANE=process',
                             f'NATIVE_BENCH_WORK={work / "make-status-proof"}'], cwd=ROOT, env=env,
                            stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode == 2
    assert json.loads((work / 'make-status-proof/report.json').read_text())['measurement_complete'] is True
    result = invoke(campaign_inputs)
    assert result.returncode == 1, result.stderr
    report = json.loads((work / 'output/report.json').read_text())
    assert report['measurement_complete'] is True and len(report['subprocesses']) == 8


def test_campaign_keeps_inconclusive_distinct_from_failure(campaign_inputs) -> None:
    work, _, _ = campaign_inputs
    path = work / 'darwin-process.json'
    record = json.loads(path.read_text())
    for case in record['process']:
        for index, pair in enumerate(case['samples_ns']):
            pair['native'] = 80 if index % 2 else 140
    completion(record, 0)
    assert record['gate_failures'] and all(value.endswith(':inconclusive') for value in record['gate_failures'])
    path.write_text(json.dumps(record))
    result = invoke(campaign_inputs)
    assert result.returncode == 1, result.stderr
    report = json.loads((work / 'output/report.json').read_text())
    assert report['measurement_complete'] is True and report['qualification_outcome'] == 'inconclusive'
    assert len(report['subprocesses']) == 8 and len(report['merges']) == 2
    assert report['merges'][1]['qualification_outcome'] == 'pass'


@pytest.mark.parametrize('fault', ['observation', 'malformed-inventory', 'empty-inventory', 'inventory-timeout', 'signal', 'kill', 'hash'])
def test_campaign_preserves_terminal_report_after_cleanup_boundary_errors(campaign_inputs, fault) -> None:
    work, args, env = campaign_inputs
    env['CAMPAIGN_MODE'] = 'wait'
    if fault == 'kill':
        env['CAMPAIGN_DESCENDANT'] = '1'
        (work / 'descendant.py').write_text(
            "import os, pathlib, signal, time\nsignal.signal(signal.SIGTERM, signal.SIG_IGN)\n"
            "pathlib.Path(__file__).with_name('descendant-pid').write_text(str(os.getpid()))\ntime.sleep(30)\n")
    entry = work / 'entry.py'
    injection = '''
import errno, signal
fault = FAULT
actual_inventory = campaign.subprocess.run
actual_signal = campaign.os.killpg
actual_hash = campaign.sha
actual_save = campaign.save
def inventory(command, **kwargs):
    if command[0] == 'ps' and fault == 'inventory-timeout':
        raise subprocess.TimeoutExpired(command, 2)
    if command[0] == 'ps' and fault in ('observation', 'malformed-inventory', 'empty-inventory'):
        return subprocess.CompletedProcess(command, 1 if fault == 'observation' else 0,
                                           stdout='' if fault in ('observation', 'empty-inventory') else 'not a PID PGID row',
                                           stderr='injected inventory failure')
    return actual_inventory(command, **kwargs)
def send(group, number):
    if number == signal.SIGTERM and fault == 'signal':
        raise PermissionError(errno.EPERM, 'injected TERM failure')
    if number == signal.SIGKILL and fault == 'kill':
        raise PermissionError(errno.EPERM, 'injected KILL failure')
    return actual_signal(group, number)
def digest(path):
    if fault == 'hash' and path.name.endswith('.stdout'):
        raise OSError('injected artifact hashing failure')
    return actual_hash(path)
def save(path, report):
    with pathlib.Path(__file__).with_name('save-attempts').open('a') as output:
        output.write(str(report.get('state')) + '\\n')
    return actual_save(path, report)
campaign.subprocess.run = inventory
campaign.os.killpg = send
campaign.sha = digest
campaign.save = save
'''.replace('FAULT', repr(fault))
    entry.write_text(entry.read_text().replace('raise SystemExit(campaign.main', injection + '\nraise SystemExit(campaign.main'))
    process = subprocess.Popen(args, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    owned_group: int | None = None
    try:
        wait_for(work / 'child-pid')
        if fault == 'kill':
            wait_for(work / 'descendant-pid')
        initial = json.loads((work / 'output/report.json').read_text())
        owned_group = initial['subprocesses'][0]['pid']
        process.send_signal(signal.SIGINT)
        _, stderr = process.communicate(timeout=15)
        assert process.returncode == 130, stderr
        report = json.loads((work / 'output/report.json').read_text())
        assert report['state'] == 'interrupted' and not report['measurement_complete']
        assert 'interrupted' in report['error']
        assert report['cleanup_error']
        assert 'interrupted' in (work / 'save-attempts').read_text().splitlines()
        if fault in ('observation', 'malformed-inventory', 'empty-inventory', 'inventory-timeout'):
            assert report['remaining_owned_members'] is None
            assert 'observation' in report['cleanup_error'].lower()
        elif fault == 'signal':
            assert 'TERM' in report['cleanup_error'] and 'injected TERM failure' in report['cleanup_error']
            assert report['remaining_owned_members'] == []
        elif fault == 'kill':
            assert 'SIGKILL' in report['cleanup_error'] and 'injected KILL failure' in report['cleanup_error']
            assert int((work / 'descendant-pid').read_text()) in report['remaining_owned_members']
        else:
            assert 'artifact hashing' in report['cleanup_error']
            assert report['remaining_owned_members'] == []
            assert report['subprocesses'][0]['stderr_sha256']
    finally:
        if owned_group is not None:
            if owned_members(owned_group):
                try:
                    os.killpg(owned_group, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            deadline = time.monotonic() + 3
            while owned_members(owned_group):
                if time.monotonic() > deadline:
                    pytest.fail(f'failure-injection fixture group is not empty: {owned_group}')
                time.sleep(.01)
        if process.poll() is None:
            process.kill()
        process.wait()


@pytest.mark.parametrize('fault', ['observation', 'hash', 'final-hash'])
def test_campaign_finalizes_artifacts_after_completed_child_cleanup_failure(campaign_inputs, fault) -> None:
    work, _, _ = campaign_inputs
    entry = work / 'entry.py'
    injection = '''
fault = FAULT
actual_run = campaign.subprocess.run
actual_hash = campaign.sha
def inventory(command, **kwargs):
    if command[0] == 'ps' and fault == 'observation':
        return subprocess.CompletedProcess(command, 1, stdout='', stderr='injected observation failure')
    return actual_run(command, **kwargs)
def digest(path):
    final = pathlib.Path(__file__).with_name('output').joinpath('darwin-merged.json').exists()
    if path.name.endswith('.stdout') and (fault == 'hash' or fault == 'final-hash' and final):
        raise OSError('injected artifact hashing failure')
    return actual_hash(path)
campaign.subprocess.run = inventory
campaign.sha = digest
'''.replace('FAULT', repr(fault))
    entry.write_text(entry.read_text().replace('raise SystemExit(campaign.main', injection + '\nraise SystemExit(campaign.main'))
    result = invoke(campaign_inputs)
    assert result.returncode == 2, result.stderr
    report = json.loads((work / 'output/report.json').read_text())
    assert report['state'] == 'invalid' and not report['measurement_complete']
    assert report['error'] and report['cleanup_error']
    assert report['remaining_owned_members'] == (None if fault == 'observation' else [])
    active = report['subprocesses'][-1]
    assert active['status'] == 0
    assert len(active['stderr_sha256']) == 64
    if fault == 'observation':
        assert len(active['stdout_sha256']) == 64
    if fault == 'final-hash':
        assert len(report['merges']) == 2
        assert report['qualification_outcome'] is None
    assert len(active['report_sha256']) == 64


@pytest.mark.parametrize('scenario', ['already-empty', 'confirmed-esrch', 'unconfirmed-esrch'])
def test_public_stop_requires_confirmed_absence_for_signal_disappearance(tmp_path: Path, scenario: str) -> None:
    tmp_path.resolve().relative_to(ROOT)
    entry = tmp_path / 'stop-api.py'
    entry.write_text('''import errno, json, pathlib, signal, subprocess, sys
sys.path.insert(0, str(pathlib.Path.cwd() / 'tools/nativebench'))
import campaign
scenario = sys.argv[1]
child = subprocess.Popen([sys.executable, '-B', '-c', 'pass' if scenario == 'already-empty' else 'import time; time.sleep(30)'],
                         cwd=pathlib.Path.cwd(), stdin=subprocess.DEVNULL, start_new_session=True)
actual_signal = campaign.os.killpg
if scenario == 'already-empty': child.wait(timeout=3)
def send(group, number):
    if scenario == 'already-empty':
        raise PermissionError(errno.EPERM, 'signal was attempted on an empty group')
    if number == signal.SIGTERM:
        if scenario == 'confirmed-esrch':
            actual_signal(group, number)
            child.wait(timeout=3)
        raise ProcessLookupError(errno.ESRCH, 'group vanished before signal')
    return actual_signal(group, number)
campaign.os.killpg = send
try:
    try:
        result = campaign.stop(child)
        print(json.dumps({'remaining': result.remaining_owned_members, 'status': result.direct_child_status,
                          'signals': result.signal_observations}))
    except campaign.CleanupError as error:
        print(json.dumps({'error': str(error), 'remaining': error.result.remaining_owned_members,
                          'status': error.result.direct_child_status}))
        raise SystemExit(2)
finally:
    if child.poll() is None:
        actual_signal(child.pid, signal.SIGKILL)
    child.wait(timeout=3)
''')
    result = subprocess.run([sys.executable, '-B', str(entry), scenario], cwd=ROOT,
                            env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=15)
    record = json.loads(result.stdout)
    if scenario == 'unconfirmed-esrch':
        assert result.returncode == 2, result.stderr
        assert 'not confirmed empty' in record['error']
    else:
        assert result.returncode == 0, result.stderr
        assert record['remaining'] == [] and record['status'] is not None
        if scenario == 'confirmed-esrch':
            assert record['signals']
