"""Check public child-process measurement boundaries."""
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import qualify_native


def test_measured_child_has_closed_stdin(tmp_path: Path) -> None:
    elapsed, result = qualify_native.measured(
        [sys.executable, '-B', '-c', 'import sys;print(len(sys.stdin.read()))'], tmp_path)
    assert elapsed > 0
    assert result == '0'


def test_http_partial_readiness_has_deadline(tmp_path: Path) -> None:
    child = subprocess.Popen(
        [sys.executable, '-B', '-c', 'import sys,time;sys.stdout.write("{");sys.stdout.flush();time.sleep(30)'],
        cwd=tmp_path, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        with pytest.raises(TimeoutError, match='readiness'):
            qualify_native.ready(child, timeout=.1)
    finally:
        child.kill()
        child.communicate(timeout=5)


def test_execution_producer_records_canonical_order_before_serialization(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    import json
    import qualification

    package = tmp_path / 'startup'
    package.mkdir()
    for name in ('source.lo', 'program_test.go', 'benchmark.c', 'allocation.c', 'benchmark.test',
                 'native-benchmark', 'native-allocations', 'program'):
        (package / name).write_text('controlled ' + name)
    def capture(argv, root):
        if argv[:2] == ['go', 'run']:
            return str(package)
        if argv[0] == str(package / 'benchmark.test'):
            return 'BenchmarkExecution 1000 1000 ns/op 8 B/op 1 allocs/op'
        if argv[-1] == '--verify':
            return '7'
        if argv[0] == str(package / 'native-allocations'):
            return '{}'
        return json.dumps({'ns': 200, 'total_ns': 200000000, 'iterations': 1000000, 'batch_size': 64})
    monkeypatch.setattr(qualify_native, 'capture', capture)
    monkeypatch.setattr(qualify_native.subprocess, 'run', lambda *args, **kwargs: None)
    monkeypatch.setattr(qualify_native, 'native_fixture',
                        lambda config, path, allocations=False: path / ('native-allocations' if allocations else 'native-benchmark'))
    monkeypatch.setattr(qualify_native, 'build', lambda *args: None)
    monkeypatch.setattr(qualify_native, 'measured', lambda *args: (1, '7'))
    config = qualify_native.Config(ROOT, tmp_path, package / 'program', 10, 'startup')
    records = qualify_native.execution(config)
    report = {'execution': records, 'samples': 10, 'warmup': 3, 'concurrency': 1,
              'other_benchmark_jobs': [], 'benchmark_job_observation': 'controlled producer test',
              'expected_inventory': {'execution': ['startup']}, 'complete_contract': False,
              'measurement_complete': True}
    qualification.completion(report, 0)
    assert report['measurement_complete'] is True, report.get('measurement_error')
    assert report['gates_passed'] is True
    for sample in records[0]['samples']:
        assert type(sample['order']) is list
        assert sample['order'] == (['native', 'interpreter'] if sample['sample'] % 2 == 0 else ['interpreter', 'native'])


@pytest.mark.parametrize('gate_passed', [False, True])
def test_public_producer_records_load_without_admission(tmp_path: Path, monkeypatch: pytest.MonkeyPatch, gate_passed: bool) -> None:
    import json
    binary = tmp_path / 'package/bin/lo'
    binary.parent.mkdir(parents=True)
    binary.write_text('controlled installed compiler')
    (binary.parent.parent / 'toolchain.json').write_text('{}')
    observations = [[{'pid': 123, 'command': 'owned-background-before'}], [{'pid': 456, 'command': 'owned-background-after'}]]
    observed = iter(observations)
    monkeypatch.setattr(qualify_native, 'benchmark_jobs', lambda root: next(observed))
    monkeypatch.setattr(qualify_native.os, 'getloadavg', lambda: (8.0, 7.0, 6.0))
    monkeypatch.setattr(qualify_native, 'capture_provenance', lambda *a, **k: {'tools': {}})
    monkeypatch.setattr(qualify_native, 'verify_provenance', lambda *a, **k: None)
    monkeypatch.setattr(qualify_native, 'merge_identity', lambda **k: {'reference': {'source_identity': {}}, 'producer': {'source_identity': {}}, 'evaluator': {}})
    monkeypatch.setattr(qualify_native, 'capture', lambda *a: 'go version controlled')
    monkeypatch.setattr(qualify_native, 'processor_identity', lambda *a: 'controlled arm64')
    called = []
    monkeypatch.setattr(qualify_native, 'process', lambda config: called.append('measured') or [])
    def finish(report, status):
        report['measurement_complete'] = True
        report['gates_passed'] = gate_passed
    monkeypatch.setattr(qualify_native, 'completion', finish)
    work = tmp_path / 'measure'
    monkeypatch.setattr(sys, 'argv', ['qualify_native', '--binary', str(binary), '--workdir', str(work), '--lane', 'process', '--case', 'startup'])
    if gate_passed:
        qualify_native.main()
    else:
        with pytest.raises(SystemExit) as error:
            qualify_native.main()
        assert error.value.code == 1
    report = json.loads((work / 'report.json').read_text())
    assert called == ['measured']
    assert report['measurement_complete'] is True
    assert report['other_benchmark_jobs'] == observations[0] + observations[1]
    assert report['benchmark_context']['before']['other_benchmark_jobs'] == observations[0]
    assert report['benchmark_context']['after']['other_benchmark_jobs'] == observations[1]
    assert report['benchmark_context']['before']['load_average_1_5_15'] == [8.0, 7.0, 6.0]


def test_process_producer_emits_exact_order(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    import json
    monkeypatch.setattr(qualify_native, 'capture', lambda *a: json.dumps([{'name': 'startup', 'source': 'return 7', 'expected': '7'}]))
    monkeypatch.setattr(qualify_native, 'build', lambda *a: None)
    observed = []
    def measured(argv, directory):
        observed.append('interpreter' if argv[0] == '/controlled/compiler' else 'native')
        return 10, '7'
    monkeypatch.setattr(qualify_native, 'measured', measured)
    records = qualify_native.process(qualify_native.Config(ROOT, tmp_path, Path('/controlled/compiler'), 10, 'startup'))
    for sample in records[0]['samples_ns']:
        offset = (sample['sample'] + 3) * 2
        assert sample['order'] == observed[offset:offset+2]


def test_http_producer_emits_exact_order(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    observed = []
    class Server:
        returncode = None
        def __init__(self, argv, **kwargs):
            self.backend = 'interpreter' if argv[0] == '/controlled/compiler' else 'native'
        def poll(self):
            return self.returncode
        def send_signal(self, signal):
            pass
        def communicate(self, timeout):
            self.returncode = 0
            return '', ''
    class Client:
        def __init__(self, address, timeout):
            self.backend = address
        def request(self, *args, **kwargs):
            observed.append(self.backend)
        def getresponse(self):
            return type('Response', (), {'status': 200, 'read': lambda self: b'10'})()
        def close(self):
            pass
    monkeypatch.setattr(qualify_native, 'build', lambda *a: None)
    monkeypatch.setattr(qualify_native.subprocess, 'Popen', Server)
    monkeypatch.setattr(qualify_native, 'ready', lambda server: {'servers': [{'address': server.backend}]})
    monkeypatch.setattr(qualify_native.http.client, 'HTTPConnection', Client)
    records = qualify_native.http_bench(qualify_native.Config(ROOT, tmp_path, Path('/controlled/compiler'), 10, 'computed'))
    for sample in records[0]['samples']:
        offset = (sample['sample'] + 3) * 400
        assert sample['order'] == [observed[offset], observed[offset+200]]
