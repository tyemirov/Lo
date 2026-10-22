"""Public CLI contracts for whole-lane qualification report admission.

These synthetic records exercise the report boundary and are not timing evidence.
The merger requires one canonical merge_identity from each producer. It must not
infer missing provenance from lane-specific generated payloads or work paths.
"""
from __future__ import annotations

import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

from qualification import LANES, expected_inventory
from identity import CACHE, TIMER_CONTRACT

ROOT = Path(__file__).resolve().parents[2]
CLI = ROOT / 'tools/nativebench/merge.py'
DIGEST = '1' * 64
LIMITS = {'build_seconds': 180, 'artifact_bytes': 64 << 20,
          'diagnostic_bytes': 1 << 20, 'peak_process_tree_rss_bytes': 2 << 30}


@pytest.fixture
def work(tmp_path: Path) -> Path:
    directory = tmp_path.resolve()
    directory.relative_to(ROOT)
    return directory


def stable_identity() -> dict:
    """Declare source-bound identity independently of per-lane generated files."""
    return {
        'target': 'darwin/arm64',
        'environment': {'system': 'macOS 27.0.1', 'machine': 'arm64',
                        'processor': 'synthetic report fixture', 'execution': 'native'},
        'accelerator': 'none',
        'package': {'compiler_sha256': DIGEST, 'manifest_sha256': DIGEST,
                    'runtime_sha256': DIGEST, 'inventory_sha256': DIGEST,
                    'provenance_sha256': DIGEST},
        'source': {'semantic_source_sha256': DIGEST},
        'producer': {'kind': 'darwin-native', 'tools': {'python': '3.13.5'},
                     'source_identity': {'scripts/qualify_native.py': DIGEST}},
        'reference': {'go': 'go version go1.27.1 darwin/arm64',
                      'source_identity': {'go.mod': DIGEST, 'go.sum': DIGEST,
                                          'tests/corpus/corpus.go': DIGEST,
                                          'internal/eval/eval.go': DIGEST}},
        'harness': {'tools/nativebench/main.go': DIGEST,
                    'tools/nativebench/harness.c.txt': DIGEST,
                    'tools/nativebench/allocation.c.txt': DIGEST},
        'evaluator': {'tools/nativebench/qualification.py': DIGEST},
        'measurement': {'samples': 10, 'warmup': 3, 'order': 'alternating paired samples',
                        'cache': CACHE,
                        'max_steps': 10000000, 'concurrency': 1, 'limits': LIMITS.copy(),
                        'timer_contract': dict(TIMER_CONTRACT),
                        'load_policy': 'record external load; do not gate or discard samples'},
    }


def lane_report(lane: str) -> dict:
    """Use every case and complete raw samples from exactly one timing lane."""
    identity = stable_identity()
    report = {'merge_identity': identity, 'target': identity['target'],
              'samples': 10, 'warmup': 3, 'max_steps': 10000000, 'concurrency': 1,
              'order': 'alternating paired samples', 'cache': identity['measurement']['cache'],
              'measurement_complete': True, 'qualification_complete': False,
              'gates_passed': True, 'gate_failures': [], 'complete_contract': False,
              'scope': 'full fixed corpus', 'cases': [], 'lanes': [lane],
              'other_benchmark_jobs': [], 'benchmark_job_observation': 'synthetic isolated census',
              'expected_inventory': expected_inventory((lane,), ())}
    names = report['expected_inventory'][lane]
    if lane == 'execution':
        report[lane] = [{'name': name, 'source_sha256': DIGEST,
                         'native_benchmark_verified': True,
                         # Work paths and generated binary bytes can differ by lane.
                         'binaries': {'native': '2' * 64},
                         'samples': [{'sample': index,
                                      'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native'],
                                      'results': {'interpreter': {'ns': 100.0, 'bytes': 1, 'allocations': 1},
                                                  'native': {'ns': 40, 'total_ns': 40000000, 'iterations': 1000000, 'batch_size': 64}}} for index in range(10)]}
                        for name in names]
    elif lane == 'process':
        report[lane] = [{'name': name, 'source_sha256': DIGEST,
                         'samples_ns': [{'sample': index, 'interpreter': 100, 'native': 40,
                                         'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native']}
                                        for index in range(10)]} for name in names]
    elif lane == 'http':
        report[lane] = [{'name': name, 'source_sha256': DIGEST, 'requests_per_sample': 200,
                         'concurrency': 1, 'both_servers_alive': True,
                         'samples': [{'sample': index,
                            'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native'],
                            **{backend: {'samples_ns': [1] * 200,
                            'p95_ns': 1, 'elapsed_ns': 300, 'requests_per_second': 200e9 / 300}
                            for backend in ('interpreter', 'native')}} for index in range(10)]}
                        for name in names]
    else:
        report[lane] = [{'name': name, 'source_sha256': DIGEST, 'phase': phase,
                         'sample': index, 'elapsed_ns': 1000000,
                         'peak_process_tree_rss_bytes': 1 << 20, 'artifact_bytes': 1 << 20,
                         'diagnostic_bytes': 0}
                        for name in names
                        for phase, count in [('first-build', 1), ('warmup', 3), ('measurement', 10)]
                        for index in range(count)]
    return report


def reports(work: Path) -> list[Path]:
    result = []
    for lane in LANES:
        path = work / (lane + '.json')
        record = lane_report(lane)
        record['payload_identities'] = {lane + '/source.lo': {'sha256': DIGEST}}
        record['generated_work'] = str(work / lane)
        path.write_text(json.dumps(record) + '\n')
        result.append(path)
    return result


def invoke(paths: list[Path], output: Path) -> subprocess.CompletedProcess[str]:
    argv = [sys.executable, '-B', str(CLI), '--output', str(output)]
    for path in paths:
        argv.extend(['--report', str(path)])
    return subprocess.run(argv, cwd=ROOT, env=dict(os.environ, CI='1'),
                          stdin=subprocess.DEVNULL, text=True, capture_output=True, check=False)


def mutate(path: Path, change) -> None:
    report = json.loads(path.read_text())
    change(report)
    path.write_text(json.dumps(report) + '\n')


def test_public_merge_admits_complete_whole_lanes_and_keeps_receipts(work: Path) -> None:
    paths = reports(work)
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode == 0, result.stderr
    merged = json.loads(output.read_text())
    assert merged['measurement_complete'] is True
    assert merged['qualification_complete'] is True
    assert merged['expected_inventory'] == expected_inventory(LANES, ())
    assert merged['merge_identity'] == stable_identity()
    receipts = merged['input_reports']
    assert len(receipts) == len(LANES)
    for lane, source in zip(LANES, paths):
        original = json.loads(source.read_text())
        receipt = next(record for record in receipts if record['path'] == source.relative_to(ROOT).as_posix())
        assert receipt['sha256'] == hashlib.sha256(source.read_bytes()).hexdigest()
        assert receipt['lanes'] == [lane]
        assert receipt['other_benchmark_jobs'] == []
        assert receipt['benchmark_job_observation'] == original['benchmark_job_observation']
        # Preserve raw records, including indices, first build, and warmup samples.
        for actual, expected in zip(merged[lane], original[lane], strict=True):
            for name, value in expected.items():
                assert actual[name] == value
    assert merged['callback_speedup'] == pytest.approx(2.5)


def test_public_merge_keeps_external_load_without_rejecting_samples(work: Path) -> None:
    paths = reports(work)
    context = {'before': {'other_benchmark_jobs': [{'pid': 345, 'command': 'unrelated benchmark'}],
                          'load_average': [7.0, 5.0, 4.0]},
               'after': {'other_benchmark_jobs': [], 'load_average': [6.0, 5.0, 4.0]}}
    for path in paths:
        mutate(path, lambda report: report.update(
            other_benchmark_jobs=context['before']['other_benchmark_jobs'], benchmark_context=context))
    output = work / 'loaded.json'
    result = invoke(paths, output)
    assert result.returncode == 0, result.stderr
    merged = json.loads(output.read_text())
    assert merged['measurement_complete'] is True
    assert all(receipt['benchmark_context'] == context for receipt in merged['input_reports'])
    assert all(receipt['other_benchmark_jobs'] == context['before']['other_benchmark_jobs']
               for receipt in merged['input_reports'])
    assert len(merged['process'][0]['samples_ns']) == 10


@pytest.mark.parametrize('lane', ['execution', 'process', 'http'])
@pytest.mark.parametrize('mutation', ['missing_order', 'wrong_order', 'reordered_pairs'])
def test_public_merge_requires_recorded_pair_schedule(work: Path, lane: str, mutation: str) -> None:
    paths = reports(work)
    path = paths[LANES.index(lane)]
    def change(report: dict) -> None:
        samples = report[lane][0]['samples_ns' if lane == 'process' else 'samples']
        if mutation == 'missing_order':
            del samples[0]['order']
        elif mutation == 'wrong_order':
            samples[0]['order'].reverse()
        else:
            samples[0], samples[1] = samples[1], samples[0]
    mutate(path, change)
    output = work / 'invalid.json'
    result = invoke(paths, output)
    assert result.returncode == 2, result.stdout
    assert not output.exists()


@pytest.mark.parametrize('ratios,expected', [
    ([0.9] * 10, 'pass'), ([1.2] * 10, 'fail'), ([0.9] * 5 + [1.3] * 5, 'inconclusive')])
@pytest.mark.parametrize('load', [[1] * 10, [1, 1, 20, 1, 1, 50, 1, 1, 1, 1], list(range(1, 11))])
def test_public_merge_retains_loaded_pairs_and_reproduces_gates(
        work: Path, ratios: list[float], expected: str, load: list[int]) -> None:
    paths = reports(work)
    def change(report: dict) -> None:
        report['other_benchmark_jobs'] = ['controlled background workload']
        for sample, ratio, factor in zip(report['process'][0]['samples_ns'], ratios, load, strict=True):
            sample.update(interpreter=1000 * factor, native=round(1000 * factor * ratio))
    mutate(paths[1], change)
    before = json.loads(paths[1].read_text())['process'][0]['samples_ns']
    outputs = [work / 'loaded-first.json', work / 'loaded-second.json']
    for output in outputs:
        result = invoke(paths, output)
        assert result.returncode == (0 if expected == 'pass' else 1), result.stderr
    first, second = [json.loads(output.read_text()) for output in outputs]
    assert first == second
    assert first['measurement_complete'] is True
    assert first['process'][0]['samples_ns'] == before
    assert first['process'][0]['gate'] == expected


def test_public_merge_rejects_obsolete_load_admission_contract(work: Path) -> None:
    paths = reports(work)
    mutate(paths[0], lambda report: report['merge_identity']['measurement'].pop('load_policy'))
    output = work / 'obsolete.json'
    assert invoke(paths, output).returncode == 2
    assert not output.exists()


@pytest.mark.parametrize('subject', ['source', 'reference_source', 'producer', 'harness', 'evaluator', 'compiler',
    'manifest', 'runtime', 'inventory', 'provenance', 'target', 'environment', 'accelerator', 'samples', 'warmup',
    'order', 'cache', 'limits', 'timer_contract', 'reference_go'])
def test_public_merge_rejects_mixed_provenance(work: Path, subject: str) -> None:
    paths = reports(work)
    def change(report: dict) -> None:
        identity = report['merge_identity']
        if subject == 'source':
            identity['source']['semantic_source_sha256'] = '2' * 64
        elif subject == 'reference_source':
            identity['reference']['source_identity']['internal/eval/eval.go'] = '2' * 64
        elif subject in ('producer', 'harness', 'evaluator'):
            container = identity[subject]['source_identity'] if subject == 'producer' else identity[subject]
            container[next(iter(container))] = '2' * 64
        elif subject in ('compiler', 'manifest', 'runtime', 'inventory', 'provenance'):
            identity['package'][subject + '_sha256'] = '2' * 64
        elif subject == 'target':
            identity['target'] = 'linux/arm64'
        elif subject == 'environment':
            identity['environment']['processor'] = 'different processor'
        elif subject == 'accelerator':
            identity['accelerator'] = 'hvf'
        elif subject == 'reference_go':
            identity['reference']['go'] = 'go version different'
        else:
            identity['measurement'][subject] = {'seconds': 999} if subject == 'limits' else 'different'
    mutate(paths[1], change)
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode != 0
    assert 'identity' in result.stderr.lower(), result.stderr
    assert not output.exists()


@pytest.mark.parametrize('problem', ['duplicate', 'missing', 'preliminary', 'incomplete',
                                     'diagnostic', 'corrupt', 'missing_identity', 'overlap_samples'])
def test_public_merge_rejects_unadmitted_reports(work: Path, problem: str) -> None:
    paths = reports(work)
    if problem == 'duplicate':
        paths.append(paths[0])
    elif problem == 'missing':
        paths.pop()
    elif problem == 'preliminary':
        mutate(paths[0], lambda report: report.update(preliminary={'reason': 'old runtime'}))
    elif problem == 'incomplete':
        mutate(paths[0], lambda report: report.update(measurement_complete=False))
    elif problem == 'diagnostic':
        mutate(paths[0], lambda report: report.update(scope='diagnostic selection', cases=['startup']))
    elif problem == 'corrupt':
        paths[0].write_text('{not JSON}\n')
    elif problem == 'missing_identity':
        mutate(paths[0], lambda report: report.pop('merge_identity'))
    else:
        mutate(paths[0], lambda report: report['execution'][0]['samples'].__setitem__(1,
                            copy.deepcopy(report['execution'][0]['samples'][0])))
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode != 0
    assert 'merge' in result.stderr.lower(), result.stderr
    assert not output.exists()


def test_public_merge_rejects_partial_lane_without_stitching_cases(work: Path) -> None:
    paths = reports(work)
    mutate(paths[0], lambda report: report['execution'].pop())
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode != 0
    assert 'merge' in result.stderr.lower(), result.stderr
    assert not output.exists()


def test_public_merge_recomputes_failed_gates_without_selecting_samples(work: Path) -> None:
    paths = reports(work)
    def slower(report: dict) -> None:
        for record in report['execution']:
            if record['name'].startswith(('map_', 'filter_', 'fold_')):
                for sample in record['samples']:
                    sample['results']['native'].update(ns=60, total_ns=60000000)
        report['gates_passed'] = True
    mutate(paths[0], slower)
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode == 1, result.stderr
    merged = json.loads(output.read_text())
    assert merged['measurement_complete'] is True
    assert merged['qualification_complete'] is False
    assert merged['callback_gate'] == 'fail'
    assert merged['callback_speedup'] == pytest.approx(100 / 60)


@pytest.mark.parametrize('problem', ['raw_environment', 'contradictory_source_maps'])
def test_public_merge_rejects_internally_contradictory_identity(work: Path, problem: str) -> None:
    paths = reports(work)
    if problem == 'raw_environment':
        mutate(paths[0], lambda report: report.update(system='different system'))
    else:
        for path in paths:
            mutate(path, lambda report: report['merge_identity']['producer']['source_identity'].update(
                {'tools/nativebench/harness.c.txt': '2' * 64}))
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode != 0
    assert 'identity' in result.stderr.lower(), result.stderr
    assert not output.exists()


@pytest.mark.parametrize('problem', ['cache', 'timer', 'target_producer', 'execution', 'accelerator'])
def test_public_merge_rejects_matching_noncanonical_contracts(work: Path, problem: str) -> None:
    paths = reports(work)
    def change(report: dict) -> None:
        identity = report['merge_identity']
        if problem == 'cache':
            report['cache'] = identity['measurement']['cache'] = 'obsolete cold cache'
        elif problem == 'timer':
            identity['measurement']['timer_contract']['batch_calibration'] = 'obsolete per-iteration clock'
        elif problem == 'target_producer':
            report['target'] = identity['target'] = 'linux/arm64'
        elif problem == 'execution':
            identity['environment']['execution'] = 'qemu-guest'
        else:
            identity['accelerator'] = report['accelerator'] = 'hvf'
    for path in paths:
        mutate(path, change)
    output = work / 'merged.json'
    result = invoke(paths, output)
    assert result.returncode != 0
    assert 'merge' in result.stderr.lower(), result.stderr
    assert not output.exists()
