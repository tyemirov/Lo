"""Qualification inventory, numerical, completeness and resource contract checks."""
from __future__ import annotations

import pytest

def valid_report(lane: str = 'process', name: str = 'startup') -> dict:
    report = {'samples': 10, 'warmup': 3, 'measurement_complete': True,
              'expected_inventory': {lane: [name]}, 'complete_contract': False,
              'concurrency': 1, 'other_benchmark_jobs': [], 'benchmark_job_observation': 'test census'}
    if lane == 'build':
        report[lane] = [{'name': name, 'phase': phase, 'sample': index,
                         'elapsed_ns': 180_000_000_000,
                         'peak_process_tree_rss_bytes': 2 << 30, 'artifact_bytes': 64 << 20,
                         'diagnostic_bytes': 1 << 20}
                        for phase, count in [('first-build', 1), ('warmup', 3), ('measurement', 10)]
                        for index in range(count)]
    elif lane == 'execution':
        report[lane] = [{'name': name, 'native_benchmark_verified': True, 'samples': [
            {'sample': index, 'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native'],
             'results': {'interpreter': {'ns': 100.0}, 'native': {'ns': 40, 'total_ns': 40000000, 'iterations': 1000000, 'batch_size': 64}}}
            for index in range(10)]}]
    else:
        report[lane] = [{'name': name, 'samples_ns': [
            {'sample': index, 'interpreter': 100.0, 'native': 40.0,
             'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native']}
            for index in range(10)]}]
    return report


def test_confidence_gates_and_resource_boundaries() -> None:
    import qualification as performance
    report = valid_report('execution', 'map_1000')
    assert performance.evaluate(report) == []
    assert report['callback_gate'] == 'pass'
    assert report['callback_bootstrap_95_percent'] == [2.5, 2.5]
    build = valid_report('build')
    assert performance.evaluate(build) == []
    build['build'][1]['artifact_bytes'] += 1
    assert 'build:resource-limits' in performance.evaluate(build)
    for sample in report['execution'][0]['samples']:
        sample['results']['native'].update(ns=60, total_ns=60000000)
    assert 'callback:fail' in performance.evaluate(report)


@pytest.mark.parametrize('mutation', ['omit', 'duplicate', 'extra_lane', 'short', 'duplicate_sample',
                                      'zero', 'negative', 'nan', 'infinity'])
def test_report_rejects_incomplete_or_invalid_measurements(mutation: str) -> None:
    import qualification as performance
    report = valid_report()
    samples = report['process'][0]['samples_ns']
    if mutation == 'omit':
        report['process'] = []
    elif mutation == 'duplicate':
        report['process'].append(report['process'][0])
    elif mutation == 'extra_lane':
        report['execution'] = []
    elif mutation == 'short':
        samples.pop()
    elif mutation == 'duplicate_sample':
        samples[1]['sample'] = 0
    else:
        samples[0]['native'] = {'zero': 0, 'negative': -1, 'nan': float('nan'),
                                 'infinity': float('inf')}[mutation]
    with pytest.raises(ValueError):
        performance.evaluate(report)


@pytest.mark.parametrize('mutation', ['missing_warmup', 'duplicate_warmup', 'zero_rss', 'negative_diagnostics'])
def test_report_rejects_invalid_build_evidence(mutation: str) -> None:
    import qualification as performance
    report = valid_report('build')
    if mutation == 'missing_warmup':
        report['build'].pop(1)
    elif mutation == 'duplicate_warmup':
        report['build'][2]['sample'] = 0
    elif mutation == 'zero_rss':
        report['build'][1]['peak_process_tree_rss_bytes'] = 0
    else:
        report['build'][1]['diagnostic_bytes'] = -1
    with pytest.raises(ValueError):
        performance.evaluate(report)


def test_full_contract_requires_exact_inventory_and_all_six_callbacks() -> None:
    import qualification as performance
    report = valid_report('execution', 'map_1000')
    report['complete_contract'] = True
    with pytest.raises(ValueError, match='full'):
        performance.evaluate(report)


def test_callback_bootstrap_uses_point_estimate_statistic() -> None:
    import qualification as performance
    # Even samples expose reciprocal(median(x)) != median(reciprocal(x)).
    assert performance.callback_statistic([[0.2, 0.6]]) == pytest.approx(2.5)
    report = valid_report('execution', 'map_1000')
    ratios = [0.2] * 5 + [0.6] * 5
    for sample, ratio in zip(report['execution'][0]['samples'], ratios):
        sample['results']['native'].update(ns=int(ratio * 100), total_ns=int(ratio * 100) * 1000000)
    performance.evaluate(report)
    assert report['callback_speedup'] == pytest.approx(2.5)


def test_full_expected_inventory_is_fixed() -> None:
    import qualification as performance
    inventory = performance.expected_inventory(('execution', 'process', 'http', 'build'), ())
    assert len(inventory['execution']) == 15
    assert len(inventory['process']) == 14
    assert len(inventory['http']) == 2
    assert inventory['build'] == inventory['execution']
    assert set(performance.CALLBACK_CASES) <= set(inventory['execution'])
    with pytest.raises(ValueError, match='duplicate'):
        performance.expected_inventory(('process',), ('startup', 'startup'))


def test_measurement_completion_is_distinct_from_qualification() -> None:
    import qualification as performance
    report = valid_report()
    performance.completion(report, 0)
    assert report['measurement_complete'] is True
    assert report['gates_passed'] is True
    assert report['qualification_complete'] is False
    slow = valid_report()
    for pair in slow['process'][0]['samples_ns']:
        pair['native'] = 300.0
    performance.completion(slow, 0)
    assert slow['measurement_complete'] is True
    assert slow['gates_passed'] is False
    assert slow['qualification_complete'] is False
    missing = valid_report()
    missing['process'] = []
    performance.completion(missing, 0)
    assert missing['measurement_complete'] is False
    assert missing['qualification_complete'] is False
    assert 'case inventory' in missing['measurement_error']


def test_callback_bootstrap_resamples_native_over_interpreter_ratios() -> None:
    import random
    import statistics
    import qualification as performance
    report = valid_report('execution', 'map_1000')
    ratios = [0.2] * 5 + [0.6] * 5
    for sample, ratio in zip(report['execution'][0]['samples'], ratios):
        sample['results']['native'].update(ns=int(ratio * 100), total_ns=int(ratio * 100) * 1000000)
    performance.evaluate(report)
    rng = random.Random(1001)
    estimates = sorted(1 / statistics.median(rng.choices(ratios, k=10)) for _ in range(2000))
    assert report['callback_bootstrap_95_percent'] == pytest.approx([estimates[49], estimates[1949]])


@pytest.mark.parametrize('mutation', ['short_pairs', 'duplicate_sample', 'short_requests', 'zero_p95', 'infinite_throughput'])
def test_report_rejects_invalid_http_observations(mutation: str) -> None:
    import qualification as performance
    report = valid_report('execution', 'startup')
    report.pop('execution')
    report['expected_inventory'] = {'http': ['echo']}
    pair = lambda index: {'sample': index, 'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native'], **{backend: {'samples_ns': [1.0] * 200,
                            'elapsed_ns': 300.0, 'p95_ns': 1.0, 'requests_per_second': 666666666.0}
                            for backend in ('interpreter', 'native')}}
    report['http'] = [{'name': 'echo', 'samples': [pair(index) for index in range(10)],
                       'requests_per_sample': 200, 'concurrency': 1, 'both_servers_alive': True}]
    samples = report['http'][0]['samples']
    if mutation == 'short_pairs':
        samples.pop()
    elif mutation == 'duplicate_sample':
        samples[1]['sample'] = 0
    elif mutation == 'short_requests':
        samples[0]['native']['samples_ns'].pop()
    elif mutation == 'zero_p95':
        samples[0]['native']['p95_ns'] = 0
    else:
        samples[0]['native']['requests_per_second'] = float('inf')
    with pytest.raises(ValueError):
        performance.evaluate(report)


@pytest.mark.parametrize('subject', ['concurrency', 'jobs', 'missing_census', 'unverified_native'])
def test_report_rejects_invalid_concurrency_observations_and_verification(subject: str) -> None:
    import qualification
    report = valid_report('execution', 'startup')
    if subject == 'concurrency':
        report['concurrency'] = 2
    elif subject == 'jobs':
        report['other_benchmark_jobs'] = 'invalid observation list'
    elif subject == 'missing_census':
        del report['benchmark_job_observation']
    else:
        report['execution'][0]['native_benchmark_verified'] = False
    with pytest.raises(ValueError):
        qualification.evaluate(report)


def test_allocation_accounting_is_labeled_without_a_cross_allocator_gate() -> None:
    import qualification
    report = valid_report('execution', 'startup')
    report['execution'][0]['native_allocation_volume'] = {'bytes': 1000000000, 'allocations': 1000000}
    for sample in report['execution'][0]['samples']:
        sample['results']['interpreter'].update(bytes=1, allocations=1)
    assert qualification.evaluate(report) == []
    assert report['allocation_accounting'] == qualification.ALLOCATION_ACCOUNTING
    assert 'no ratio gate' in report['allocation_accounting']['gate']


@pytest.mark.parametrize('value', [0, -1, float('nan'), float('inf'), 1 << 10000])
def test_comparison_rejects_nonpositive_or_nonfinite_observations(value: float) -> None:
    import qualification
    with pytest.raises(ValueError):
        qualification.comparison([{'interpreter': 1.0, 'native': value}])


def test_exact_full_report_qualifies_and_each_lane_omission_rejects() -> None:
    import copy
    import qualification
    report = valid_report()
    inventory = qualification.expected_inventory(qualification.LANES, ())
    report.update(expected_inventory=inventory, complete_contract=True)
    for lane in ('execution', 'process', 'build'):
        report[lane] = [record for name in inventory[lane] for record in valid_report(lane, name)[lane]]
    report['http'] = [{'name': name, 'requests_per_sample': 200, 'concurrency': 1,
                        'both_servers_alive': True, 'samples': [
                            {'sample': index, 'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native'], **{backend: {'samples_ns': [1.0] * 200,
                                'elapsed_ns': 300.0, 'p95_ns': 1.0, 'requests_per_second': 666666666.0}
                                for backend in ('interpreter', 'native')}} for index in range(10)]}
                       for name in inventory['http']]
    qualification.completion(report, 0)
    assert report['measurement_complete'] is True
    assert report['qualification_complete'] is True
    for lane in qualification.LANES:
        missing = copy.deepcopy(report)
        missing[lane].pop()
        qualification.completion(missing, 0)
        assert missing['measurement_complete'] is False
        assert missing['qualification_complete'] is False


@pytest.mark.parametrize('problem', ['missing_total', 'missing_iterations', 'missing_batch',
    'zero_total', 'fraction_total', 'zero_iterations', 'fraction_iterations', 'batch',
    'divisibility', 'too_many_iterations', 'early_stop', 'ratio', 'fraction_ns'])
def test_execution_rejects_invalid_native_batch_evidence(problem: str) -> None:
    import qualification
    report = valid_report('execution', 'startup')
    timing = report['execution'][0]['samples'][0]['results']['native']
    if problem.startswith('missing_'):
        timing.pop({'missing_total': 'total_ns', 'missing_iterations': 'iterations', 'missing_batch': 'batch_size'}[problem])
    else:
        timing.update({'zero_total': {'total_ns': 0}, 'fraction_total': {'total_ns': 40000000.5},
            'zero_iterations': {'iterations': 0}, 'fraction_iterations': {'iterations': 1000000.5},
            'batch': {'batch_size': 3}, 'divisibility': {'iterations': 999999},
            'too_many_iterations': {'iterations': 1000064},
            'early_stop': {'iterations': 64, 'total_ns': 2560},
            'ratio': {'ns': 41}, 'fraction_ns': {'ns': 40.5}}[problem])
    with pytest.raises(ValueError):
        qualification.evaluate(report)


def test_execution_accepts_unbounded_last_batch_elapsed_overshoot() -> None:
    import qualification
    report = valid_report('execution', 'startup')
    timing = report['execution'][0]['samples'][0]['results']['native']
    timing.update(ns=10000000000, iterations=64, batch_size=64, total_ns=640000000000)
    qualification.validate_measurements(report)


@pytest.mark.parametrize('lane', ['execution', 'process', 'http'])
def test_report_rejects_incorrect_observed_pair_order(lane: str) -> None:
    import qualification
    report = valid_report('execution' if lane == 'http' else lane, 'startup')
    if lane == 'http':
        report.pop('execution')
        report['expected_inventory'] = {'http': ['echo']}
        report['http'] = [{'name': 'echo', 'requests_per_sample': 200, 'concurrency': 1,
                           'both_servers_alive': True, 'samples': [
            {'sample': index, 'order': ['native', 'interpreter'] if index % 2 == 0 else ['interpreter', 'native'], **{backend: {'samples_ns': [1] * 200, 'elapsed_ns': 300,
                 'p95_ns': 1, 'requests_per_second': 200e9 / 300} for backend in ('interpreter', 'native')}}
            for index in range(10)]}]
    samples = report[lane][0]['samples_ns' if lane == 'process' else 'samples']
    samples[0]['order'] = ['interpreter', 'native']  # Retained index zero follows three warmups.
    with pytest.raises(ValueError, match='order'):
        qualification.evaluate(report)
