"""Validate complete paired native qualification evidence and calculate its gates."""
from __future__ import annotations

import math
import random
import statistics
from typing import Any

WARMUP = 3
BUILD_SECONDS = 180
BUILD_BYTES = 64 << 20
BUILD_RSS = 2 << 30
DIAGNOSTIC_BYTES = 1 << 20
ALLOCATION_ACCOUNTING = {
    'interpreter': 'Go benchmark B/op and allocs/op with normal garbage collection',
    'native': 'successful allocator requests during fresh session execution and complete cleanup; admission excluded',
    'gate': 'descriptive accounting only; no ratio gate between allocators',
}


def interval(values: list[float]) -> list[float]:
    if not values:
        raise ValueError('confidence interval requires observations')
    for value in values:
        number(value, 'confidence observation')
    rng = random.Random(1001)
    estimates = sorted(statistics.median(rng.choices(values, k=len(values))) for _ in range(2000))
    return [estimates[49], estimates[1949]]


def comparison(pairs: list[dict[str, float]]) -> dict[str, Any]:
    ratios = [number(pair['native'], 'native timing') / number(pair['interpreter'], 'interpreter timing')
              for pair in pairs]
    bounds = interval(ratios)
    return {'native_over_interpreter': statistics.median(ratios), 'bootstrap_95_percent': bounds,
            'gate': 'pass' if bounds[1] <= 1.1 else 'fail' if bounds[0] > 1.1 else 'inconclusive'}


LANES = ('execution', 'process', 'http', 'build')
CALLBACK_CASES = tuple(f'{operation}_{size}' for size in (1000, 10000)
                       for operation in ('map', 'filter', 'fold'))
CORPUS_CASES = ('startup', 'list_sum', *(f'{operation}_{size}' for size in (1000, 10000)
                 for operation in ('map', 'filter', 'fold', 'records', 'bigint')), 'unicode')
FULL_INVENTORY = {'execution': (*CORPUS_CASES, 'training_mvp', 'training_dense'),
                  'process': (*CORPUS_CASES, 'file'), 'http': ('echo', 'computed'),
                  'build': (*CORPUS_CASES, 'training_mvp', 'training_dense')}


def expected_inventory(lanes: tuple[str, ...], cases: tuple[str, ...]) -> dict[str, list[str]]:
    if len(set(lanes)) != len(lanes) or len(set(cases)) != len(cases):
        raise ValueError('duplicate performance lane or case selection')
    if not lanes or set(lanes) - set(LANES):
        raise ValueError('unknown or empty performance lane selection')
    available = set().union(*(set(FULL_INVENTORY[lane]) for lane in lanes))
    if set(cases) - available:
        raise ValueError('performance cases do not belong to selected lanes')
    inventory = {lane: [name for name in FULL_INVENTORY[lane] if not cases or name in cases]
                 for lane in lanes}
    return {lane: names for lane, names in inventory.items() if names}


def number(value: Any, subject: str, *, zero: bool = False) -> float:
    diagnostic = f'{subject} requires a finite {"nonnegative" if zero else "positive"} number'
    if type(value) not in (int, float):
        raise ValueError(diagnostic)
    try:
        converted = float(value)
    except OverflowError as error:
        raise ValueError(diagnostic) from error
    if not math.isfinite(converted) or (converted < 0 if zero else converted <= 0):
        raise ValueError(diagnostic)
    return converted


def sample_inventory(samples: Any, count: int, subject: str) -> None:
    if not isinstance(samples, list) or len(samples) != count:
        raise ValueError(f'{subject} requires exactly {count} samples')
    indices = [sample.get('sample') for sample in samples]
    if any(type(index) is not int for index in indices) or indices != list(range(count)):
        raise ValueError(f'{subject} requires unique sample indices in measurement order')


def native_batch_timing(timing: Any) -> None:
    """Admit the actual calibrated C batch, excluding calibration iterations."""
    if not isinstance(timing, dict):
        raise ValueError('native batch requires a timing object')
    for field in ('ns', 'total_ns', 'iterations', 'batch_size'):
        if type(timing.get(field)) is not int or timing[field] <= 0:
            raise ValueError(f'native batch {field} requires a positive integer')
    total, iterations, batch = timing['total_ns'], timing['iterations'], timing['batch_size']
    if batch not in (1, 2, 4, 8, 16, 32, 64) or iterations % batch:
        raise ValueError('native batch requires a calibrated size and complete batches')
    if iterations > 1000000 + batch - 1:
        raise ValueError('native batch exceeds its iteration stop envelope')
    if total < 200000000 and iterations < 1000000:
        raise ValueError('native batch did not reach either stopping condition')
    if timing['ns'] != total // iterations:
        raise ValueError('native batch ns differs from total_ns divided by iterations')


def validate_measurements(report: dict[str, Any]) -> None:
    if type(report.get('concurrency')) is not int or report['concurrency'] != 1:
        raise ValueError('qualification requires owned measurement concurrency one')
    if not isinstance(report.get('other_benchmark_jobs'), list) or not isinstance(report.get('benchmark_job_observation'), str) or not report['benchmark_job_observation'].strip():
        raise ValueError('qualification requires informational process observations')
    count = report.get('samples')
    if type(count) is not int or not 10 <= count <= 1000 or report.get('warmup') != WARMUP:
        raise ValueError('report requires ten or more samples and three warmups')
    inventory = report.get('expected_inventory')
    if not isinstance(inventory, dict) or not inventory or set(inventory) - set(LANES):
        raise ValueError('report requires its declared expected lane inventory')
    for lane, names in inventory.items():
        if not isinstance(names, list) or not names or len(set(names)) != len(names) or set(names) - set(FULL_INVENTORY[lane]):
            raise ValueError(f'{lane} has an incorrect expected case inventory')
    if report.get('complete_contract') and inventory != expected_inventory(LANES, ()):
        raise ValueError('full qualification requires the exact full case inventory and six callback cases')
    if {lane for lane in LANES if lane in report} != set(inventory):
        raise ValueError('measured lanes differ from the expected lane inventory')
    for lane, names in inventory.items():
        records = report[lane]
        if not isinstance(records, list):
            raise ValueError(f'{lane} requires case records')
        if lane == 'build':
            expected = {(name, phase, index) for name in names
                        for phase, size in [('first-build', 1), ('warmup', WARMUP), ('measurement', count)]
                        for index in range(size)}
            observed = [(record.get('name'), record.get('phase'), record.get('sample')) for record in records]
            if any(type(record.get('sample')) is not int for record in records) or len(observed) != len(expected) or set(observed) != expected:
                raise ValueError('build requires unique first-build, warmup, and measurement observations for every case')
            for record in records:
                for field in ('elapsed_ns', 'peak_process_tree_rss_bytes', 'artifact_bytes'):
                    number(record.get(field), 'build:' + field)
                number(record.get('diagnostic_bytes'), 'build:diagnostic_bytes', zero=True)
            continue
        observed = [record.get('name') for record in records]
        if len(observed) != len(names) or set(observed) != set(names):
            raise ValueError(f'{lane} measured cases differ from the expected case inventory')
        for record in records:
            if lane == 'execution' and record.get('native_benchmark_verified') is not True:
                raise ValueError('execution requires verification of the actual native benchmark binary')
            samples = record.get('samples_ns' if lane == 'process' else 'samples')
            sample_inventory(samples, count, lane + ':' + record['name'])
            if lane == 'http' and (record.get('requests_per_sample') != 200 or record.get('concurrency') != 1 or record.get('both_servers_alive') is not True):
                raise ValueError('HTTP requires 200 requests per sample and concurrency one')
            for sample in samples:
                expected_order = ['interpreter', 'native'] if (sample['sample'] + WARMUP) % 2 == 0 else ['native', 'interpreter']
                if sample.get('order') != expected_order:
                    raise ValueError('observed pair order differs from alternating warmup and measurement order')
                for backend in ('interpreter', 'native'):
                    if lane == 'execution':
                        number(sample['results'][backend]['ns'], 'execution:' + backend)
                        if backend == 'native':
                            native_batch_timing(sample['results'][backend])
                    elif lane == 'process':
                        number(sample[backend], 'process:' + backend)
                    else:
                        metric = sample[backend]
                        for field in ('p95_ns', 'elapsed_ns', 'requests_per_second'):
                            number(metric.get(field), 'http:' + field)
                        timings = metric.get('samples_ns')
                        if not isinstance(timings, list) or len(timings) != 200:
                            raise ValueError('HTTP requires 200 individual request timings')
                        for timing in timings:
                            number(timing, 'http:request timing')


def callback_statistic(ratios: list[list[float]]) -> float:
    return 1 / math.exp(statistics.mean(math.log(statistics.median(values)) for values in ratios))


def completion(report: dict[str, Any], status: int) -> None:
    measured = report.get('measurement_complete') is True
    report['measurement_complete'] = False
    report['qualification_complete'] = False
    if status or not measured:
        report['measurement_error'] = report.get('error', 'guest measurement did not complete')
        report['gates_passed'] = False
        return
    try:
        report['gate_failures'] = evaluate(report)
    except (ValueError, KeyError, TypeError) as error:
        report['measurement_error'] = str(error)
        report['gates_passed'] = False
        return
    report['measurement_complete'] = True
    report['gates_passed'] = not report['gate_failures']
    report['qualification_complete'] = bool(report['gates_passed'] and report['complete_contract']
                                            and 'preliminary' not in report)


def evaluate(report: dict[str, Any]) -> list[str]:
    validate_measurements(report)
    if 'execution' in report:
        report['allocation_accounting'] = dict(ALLOCATION_ACCOUNTING)
    failures: list[str] = []
    for record in report.get('execution', []):
        pairs = [{backend: sample['results'][backend]['ns'] for backend in ('interpreter', 'native')}
                 for sample in record['samples']]
        record.update(comparison(pairs))
        if not record['name'].startswith(('map_', 'filter_', 'fold_')) and record['gate'] != 'pass':
            failures.append('execution:' + record['name'] + ':' + record['gate'])
    callback = [record for record in report.get('execution', []) if record['name'].startswith(('map_', 'filter_', 'fold_'))]
    if callback:
        estimates: list[float] = []
        rng = random.Random(1001)
        for _ in range(2000):
            medians = []
            for record in callback:
                ratios = [sample['results']['native']['ns'] / sample['results']['interpreter']['ns']
                          for sample in record['samples']]
                medians.append(rng.choices(ratios, k=len(ratios)))
            estimates.append(callback_statistic(medians))
        estimates.sort()
        report['callback_speedup'] = callback_statistic([[sample['results']['native']['ns'] / sample['results']['interpreter']['ns'] for sample in record['samples']] for record in callback])
        report['callback_bootstrap_95_percent'] = [estimates[49], estimates[1949]]
        report['callback_gate'] = 'pass' if estimates[49] >= 2 else 'fail' if estimates[1949] < 2 else 'inconclusive'
        if report['callback_gate'] != 'pass':
            failures.append('callback:' + report['callback_gate'])
    for record in report.get('process', []):
        record.update(comparison(record['samples_ns']))
        if record['gate'] != 'pass':
            failures.append('process:' + record['name'] + ':' + record['gate'])
    for record in report.get('http', []):
        for name, metric in [('p95', 'p95_ns'), ('reciprocal_throughput', 'elapsed_ns')]:
            record[name] = comparison([{backend: sample[backend][metric] for backend in ('interpreter', 'native')}
                                       for sample in record['samples']])
            if record[name]['gate'] != 'pass':
                failures.append('http:' + record['name'] + ':' + name + ':' + record[name]['gate'])
    build_failures = [record for record in report.get('build', []) if record['elapsed_ns'] > BUILD_SECONDS * 1e9
                      or not 0 < record['peak_process_tree_rss_bytes'] <= BUILD_RSS
                      or not 0 < record['artifact_bytes'] <= BUILD_BYTES
                      or record['diagnostic_bytes'] > DIAGNOSTIC_BYTES]
    if 'build' in report:
        report['build_gate'] = 'fail' if build_failures else 'pass'
        if build_failures:
            failures.append('build:resource-limits')
    return failures

