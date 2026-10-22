"""Admit and merge complete native qualification timing lanes.

Every report must contain the canonical merge_identity emitted by its producer.
Identity includes the actual target/environment/accelerator, installed package,
source/reference/harness/evaluator/producer inputs, and measurement contract.
Missing identity is an admission failure; old producer shapes are not inferred.
Generated payloads, work paths, and per-case binary digests are lane-local facts.
They are retained by each input report's path and SHA, not compared as identity.

Only complete whole lanes enter the merge. Reports cannot overlap lanes, omit a
lane or its cases, select diagnostic cases, or carry preliminary/incomplete data.
Raw lane records are copied intact; samples are never selected or stitched.
The shared completion boundary recomputes all gates for the exact full inventory.
An admitted failed gate produces a complete measurement report and exit status 1.
Admission failure preserves existing files and does not create an output report.
"""
from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tempfile
from typing import Any

from qualification import LANES, WARMUP, completion, expected_inventory, validate_measurements
from identity import CACHE, HARNESS, LIMITS, LOAD_POLICY, ORDER, TIMER_CONTRACT, validate_environment

ROOT = Path(__file__).resolve().parents[2]
IDENTITY_KEYS = {'target', 'environment', 'accelerator', 'package', 'source',
                 'producer', 'reference', 'harness', 'evaluator', 'measurement'}
PACKAGE_KEYS = {'compiler_sha256', 'manifest_sha256', 'runtime_sha256',
                'inventory_sha256', 'provenance_sha256'}
HARNESS_KEYS = set(HARNESS)
EVALUATOR_KEYS = {'tools/nativebench/qualification.py'}
MEASUREMENT_KEYS = {'samples', 'warmup', 'order', 'cache', 'max_steps',
                    'concurrency', 'limits', 'timer_contract', 'load_policy'}
TIMER_KEYS = set(TIMER_CONTRACT)
DIGEST = re.compile(r'[0-9a-f]{64}\Z')


def local(path: Path) -> Path:
    """Resolve every input and output inside the primary Lo repository."""
    resolved = path.resolve()
    if not resolved.is_relative_to(ROOT):
        raise ValueError(f'path must stay inside Lo: {path}')
    return resolved


def object_fields(value: Any, keys: set[str], subject: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != keys:
        raise ValueError(f'identity {subject} requires exactly {sorted(keys)}')
    return value


def text(value: Any, subject: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f'identity {subject} requires nonempty text')
    return value


def digest(value: Any, subject: str) -> str:
    if not isinstance(value, str) or not DIGEST.fullmatch(value):
        raise ValueError(f'identity {subject} requires a lowercase SHA-256')
    return value


def digest_map(value: Any, subject: str, *, paths: bool = True) -> dict[str, str]:
    if not isinstance(value, dict) or not value:
        raise ValueError(f'identity {subject} requires a nonempty digest map')
    for name, sha256 in value.items():
        text(name, subject + ' key')
        if paths:
            path = Path(name)
            if path.is_absolute() or '..' in path.parts or path.as_posix() != name or name == '.':
                raise ValueError(f'identity {subject} has an invalid source path: {name}')
        digest(sha256, subject + ':' + name)
    return value


def validate_identity(identity: Any) -> dict[str, Any]:
    """Validate the current producer contract without legacy-field inference."""
    identity = object_fields(identity, IDENTITY_KEYS, 'merge_identity')
    package = object_fields(identity['package'], PACKAGE_KEYS, 'package')
    for name, value in package.items():
        digest(value, 'package:' + name)
    source = object_fields(identity['source'], {'semantic_source_sha256'}, 'source')
    digest(source['semantic_source_sha256'], 'source:semantic_source_sha256')
    producer = object_fields(identity['producer'], {'kind', 'tools', 'source_identity'}, 'producer')
    text(producer['kind'], 'producer:kind')
    try:
        validate_environment(identity['target'], identity['environment'], identity['accelerator'], producer['kind'])
    except ValueError as error:
        raise ValueError(f'identity environment: {error}') from error
    tools = producer['tools']
    if not isinstance(tools, dict) or not tools:
        raise ValueError('identity producer tools require a nonempty map')
    for name, value in tools.items():
        text(name, 'producer tool key')
        text(value, 'producer tool:' + name)
        if name.endswith('_sha256'):
            digest(value, 'producer tool:' + name)
    digest_map(producer['source_identity'], 'producer source')
    reference = object_fields(identity['reference'], {'go', 'source_identity'}, 'reference')
    text(reference['go'], 'reference Go')
    digest_map(reference['source_identity'], 'reference source')
    if not {'go.mod', 'go.sum', 'tests/corpus/corpus.go'} <= set(reference['source_identity']):
        raise ValueError('identity reference source omits module or corpus identities')
    object_fields(identity['harness'], HARNESS_KEYS, 'harness')
    digest_map(identity['harness'], 'harness')
    object_fields(identity['evaluator'], EVALUATOR_KEYS, 'evaluator')
    digest_map(identity['evaluator'], 'evaluator')
    known_sources: dict[str, str] = {}
    for sources in (producer['source_identity'], reference['source_identity'], identity['harness'], identity['evaluator']):
        for name, sha256 in sources.items():
            if name in known_sources and known_sources[name] != sha256:
                raise ValueError(f'identity has contradictory source digests: {name}')
            known_sources[name] = sha256
    measurement = object_fields(identity['measurement'], MEASUREMENT_KEYS, 'measurement')
    count = measurement['samples']
    if type(count) is not int or not 10 <= count <= 1000:
        raise ValueError('identity samples require ten to one thousand pairs')
    if type(measurement['warmup']) is not int or measurement['warmup'] != WARMUP:
        raise ValueError('identity warmup requires three pairs')
    if type(measurement['concurrency']) is not int or measurement['concurrency'] != 1:
        raise ValueError('identity concurrency requires one')
    if type(measurement['max_steps']) is not int or measurement['max_steps'] != 10000000:
        raise ValueError('identity max_steps differs from the qualification contract')
    if measurement['order'] != ORDER:
        raise ValueError('identity order differs from the paired measurement contract')
    if measurement['load_policy'] != LOAD_POLICY:
        raise ValueError('identity load policy differs from the measurement contract')
    if measurement['cache'] != CACHE:
        raise ValueError('identity cache differs from the canonical measurement contract')
    limits = object_fields(measurement['limits'], set(LIMITS), 'build limits')
    if any(type(limits[name]) is not int or limits[name] != expected for name, expected in LIMITS.items()):
        raise ValueError('identity build limits differ from the qualification contract')
    object_fields(measurement['timer_contract'], TIMER_KEYS, 'timer contract')
    if measurement['timer_contract'] != TIMER_CONTRACT:
        raise ValueError('identity timer differs from the canonical measurement contract')
    return identity


def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result = {}
    for name, value in pairs:
        if name in result:
            raise ValueError(f'duplicate JSON field: {name}')
        result[name] = value
    return result


def reject_constant(value: str) -> None:
    raise ValueError(f'nonfinite JSON constant: {value}')


def identity_consistency(report: dict[str, Any], identity: dict[str, Any]) -> None:
    """Require raw measurement metadata to identify the admitted contract."""
    measurement = identity['measurement']
    for name in ('samples', 'warmup', 'order', 'cache', 'max_steps', 'concurrency'):
        if name not in report or report[name] != measurement[name]:
            raise ValueError(f'identity disagrees with raw report {name}')
    for name in ('system', 'machine', 'processor'):
        if name in report and report[name] != identity['environment'][name]:
            raise ValueError(f'identity disagrees with raw environment {name}')
    if 'environment' in report and report['environment'] != identity['environment']:
        raise ValueError('identity disagrees with raw report environment')
    for name, value in identity['producer']['tools'].items():
        if name in report and report[name] != value:
            raise ValueError(f'identity disagrees with raw producer tool {name}')
    if report.get('target', identity['target']) != identity['target']:
        raise ValueError('identity disagrees with raw report target')
    package = identity['package']
    for name, canonical in [('compiler_sha256', 'compiler_sha256'), ('binary_sha256', 'compiler_sha256'),
                            ('manifest_sha256', 'manifest_sha256'), ('toolchain_sha256', 'manifest_sha256'),
                            ('runtime_sha256', 'runtime_sha256'), ('inventory_sha256', 'inventory_sha256'),
                            ('provenance_sha256', 'provenance_sha256')]:
        if name in report and report[name] != package[canonical]:
            raise ValueError(f'identity disagrees with raw report {name}')
    for name in ('go', 'reference_go'):
        if name in report and report[name] != identity['reference']['go']:
            raise ValueError(f'identity disagrees with raw report {name}')
    if 'reference_source_identity' in report and report['reference_source_identity'] != identity['reference']['source_identity']:
        raise ValueError('identity disagrees with raw reference source identity')
    sources = {**identity['producer']['source_identity'], **identity['evaluator']}
    reported_sources = report.get('measurement_source_identity', {})
    if not isinstance(reported_sources, dict):
        raise ValueError('identity requires a measurement source map')
    for name, sha256 in reported_sources.items():
        if sources.get(name) != sha256:
            raise ValueError(f'identity disagrees with raw measurement source {name}')
    if 'accelerator' in report and report['accelerator'] != identity['accelerator']:
        raise ValueError('identity disagrees with raw report accelerator')


def admit(path: Path) -> tuple[dict[str, Any], dict[str, Any]]:
    path = local(path)
    contents = path.read_bytes()
    report = json.loads(contents, object_pairs_hook=unique_object, parse_constant=reject_constant)
    if not isinstance(report, dict):
        raise ValueError('input report requires one JSON object')
    if 'preliminary' in report or report.get('prepared'):
        raise ValueError('preliminary or preparation reports cannot enter a merge')
    if report.get('measurement_complete') is not True or 'measurement_error' in report or 'error' in report:
        raise ValueError('incomplete measurement report cannot enter a merge')
    if report.get('cases') or 'diagnostic' in str(report.get('scope', '')).lower():
        raise ValueError('diagnostic selections cannot enter a merge')
    identity = validate_identity(report.get('merge_identity'))
    identity_consistency(report, identity)
    lanes = tuple(lane for lane in LANES if lane in report)
    if not lanes or report.get('expected_inventory') != expected_inventory(lanes, ()):
        raise ValueError('input report must contain the exact full inventory of each whole lane')
    validate_measurements(report)
    receipt = {'path': path.relative_to(ROOT).as_posix(), 'sha256': hashlib.sha256(contents).hexdigest(),
               'lanes': list(lanes), 'other_benchmark_jobs': copy.deepcopy(report['other_benchmark_jobs']),
               'benchmark_job_observation': report['benchmark_job_observation']}
    if 'benchmark_context' in report:
        receipt['benchmark_context'] = copy.deepcopy(report['benchmark_context'])
    return report, receipt


def merge(paths: list[Path]) -> dict[str, Any]:
    """Combine disjoint complete lanes and recompute qualification from raw data."""
    admitted = [admit(path) for path in paths]
    if not admitted:
        raise ValueError('at least one complete timing report is required')
    identity = admitted[0][0]['merge_identity']
    lanes: dict[str, list[dict[str, Any]]] = {}
    for report, receipt in admitted:
        if report['merge_identity'] != identity:
            raise ValueError(f'merge identity mismatch: {receipt["path"]}')
        for lane in receipt['lanes']:
            if lane in lanes:
                raise ValueError(f'overlapping timing lane: {lane}')
            lanes[lane] = copy.deepcopy(report[lane])
    if set(lanes) != set(LANES):
        raise ValueError(f'missing timing lanes: {sorted(set(LANES) - set(lanes))}')
    measurement = identity['measurement']
    report = {name: copy.deepcopy(measurement[name])
              for name in ('samples', 'warmup', 'order', 'cache', 'max_steps', 'concurrency')}
    report.update({'target': identity['target'], 'accelerator': identity['accelerator'],
                   'merge_identity': copy.deepcopy(identity), 'input_reports': [receipt for _, receipt in admitted],
                   'expected_inventory': expected_inventory(LANES, ()), 'complete_contract': True,
                   'scope': 'full fixed corpus from whole timing lanes', 'measurement_complete': True,
                   'other_benchmark_jobs': [],
                   'benchmark_job_observation': 'Input process censuses are preserved in input_reports; whole timing lanes remain separate.',
                   **lanes})
    completion(report, 0)
    if not report['measurement_complete']:
        raise ValueError('merged measurement failed admission: ' + report['measurement_error'])
    return report


def publish(path: Path, report: dict[str, Any]) -> None:
    path = local(path)
    if path.exists():
        raise ValueError(f'output report already exists: {path}')
    path.parent.mkdir(parents=True, exist_ok=True)
    data = (json.dumps(report, indent=2, sort_keys=True, allow_nan=False) + '\n').encode()
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix='.' + path.name + '.', delete=False) as output:
            temporary = Path(output.name)
            output.write(data)
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', action='append', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    arguments = parser.parse_args()
    try:
        output = local(arguments.output)
        if output.exists():
            raise ValueError(f'output report already exists: {output}')
        report = merge(arguments.report)
        publish(output, report)
        print(json.dumps({'report': output.relative_to(ROOT).as_posix(),
                          'sha256': hashlib.sha256(output.read_bytes()).hexdigest(),
                          'measurement_complete': report['measurement_complete'],
                          'qualification_complete': report['qualification_complete']}))
        return 0 if report['qualification_complete'] else 1
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f'Qualification report merge: {error}', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
