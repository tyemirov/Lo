"""Public command checks for Linux performance qualification."""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name('performance.py')


@pytest.fixture
def work(tmp_path: Path) -> Path:
    assert tmp_path.resolve().is_relative_to(ROOT / '_scratch')
    return tmp_path


def test_performance_rejects_obsolete_scalar_sample_selection(work: Path) -> None:
    command = [sys.executable, '-B', str(SCRIPT), '--preparation', '_scratch/native-runtime',
               '--package', '_scratch/compiler-distribution/packages/lo-linux-arm64-software',
               '--frontend', 'bin/lo', '--work', str(work / 'run'), '--samples', '9']
    result = subprocess.run(command, cwd=ROOT, env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    assert result.returncode == 2
    assert 'unrecognized arguments: --samples 9' in result.stderr
    assert not (work / 'run').exists()


def test_precompiled_runner_rejects_unknown_configuration_fields(work: Path) -> None:
    binary = work / 'runner'
    env = dict(os.environ, CGO_ENABLED='0')
    env.pop('GOOS', None)
    env.pop('GOARCH', None)
    built = subprocess.run(['go', 'build', '-o', str(binary), str(SCRIPT.parent / 'performance-runner')],
                           cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False)
    assert built.returncode == 0, built.stderr
    source = work / 'config.json'
    output = work / 'report.json'
    source.write_text(json.dumps({'sample_plan': {'execution': 10, 'process': 10, 'http': 100, 'build': 10}, 'warmup': 3, 'compiler': '/compiler/bin/lo',
                                 'output': str(output), 'undeclared': True}))
    result = subprocess.run([str(binary), '--config', str(source)], cwd=ROOT, env=env,
                            stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False)
    assert result.returncode == 2
    assert 'unknown field "undeclared"' in result.stderr
    assert not output.exists()


@pytest.mark.parametrize('mutation', ['duplicate', 'empty', 'sample_plan', 'warmup'])
def test_runner_rejects_invalid_measurement_configuration(work: Path, mutation: str) -> None:
    binary = work / 'runner'
    env = dict(os.environ, CGO_ENABLED='0')
    env.pop('GOOS', None)
    env.pop('GOARCH', None)
    result = subprocess.run(['go', 'build', '-o', str(binary), str(SCRIPT.parent / 'performance-runner')],
                            cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    fixture = {'name': 'startup', 'source': '/input/source.lo', 'source_sha256': '1' * 64,
               'work': '/work/startup', 'artifact': '/work/startup/program'}
    config = {'sample_plan': {'execution': 10, 'process': 10, 'http': 100, 'build': 10}, 'warmup': 3, 'compiler': '/compiler/bin/lo',
              'output': str(work / 'report.json'), 'process': [fixture]}
    if mutation == 'duplicate':
        config['process'].append(fixture)
    elif mutation == 'empty':
        config['process'] = []
    else:
        config[mutation] = 0
    source = work / 'config.json'
    source.write_text(json.dumps(config))
    result = subprocess.run([str(binary), '--config', str(source)], cwd=ROOT, env=env,
                            stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode == 2, result.stderr
    assert not (work / 'report.json').exists()




@pytest.mark.parametrize('gate_passed', [False, True])
def test_public_qualification_retains_informational_guest_context(work: Path, monkeypatch: pytest.MonkeyPatch, gate_passed: bool) -> None:
    import importlib.util
    sys.path.insert(0, str(SCRIPT.parent))
    spec = importlib.util.spec_from_file_location('lo_performance_context_test', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    output = work / 'measurement'
    output.mkdir()
    context = {'before': {'other_benchmark_jobs': ['before-owned-load'], 'load_average_1_5_15': [8, 7, 6]},
               'after': {'other_benchmark_jobs': ['after-owned-load'], 'load_average_1_5_15': [9, 8, 7]}}
    exported = output / 'guest/0.file0'
    exported.parent.mkdir()
    exported.write_text(json.dumps({'sample_plan': {'execution': 10, 'process': 10, 'http': 100, 'build': 10}, 'other_benchmark_jobs': ['before-owned-load', 'after-owned-load'],
        'benchmark_context': context, 'benchmark_job_observation': 'Informational before and after snapshots',
        'measurement_complete': True, 'environment': {'execution': 'controlled-guest'}}))
    identity = {'frozen_provenance': {'tools': {}}, 'reference_go': 'controlled Go'}
    monkeypatch.setattr(module, 'prepare', lambda inputs: (work / 'batch.json', identity))
    monkeypatch.setattr(module, 'run_batch', lambda *a, **k: {'cases': [{'status': 0}]})
    monkeypatch.setattr(module, 'verify_provenance', lambda *a: None)
    monkeypatch.setattr(module, 'merge_identity', lambda **k: {'reference': {'source_identity': {}}, 'producer': {'source_identity': {}}, 'evaluator': {}})
    def finish(report, status):
        report['gates_passed'] = gate_passed
    monkeypatch.setattr(module, 'completion', finish)
    inputs = module.Inputs(work, work, work, output, 'go', ('execution',), ())
    report = module.qualify(inputs, False)
    assert report['benchmark_context'] == context
    assert report['other_benchmark_jobs'] == ['before-owned-load', 'after-owned-load']
    assert report['measurement_complete'] is True
    assert report['gates_passed'] is gate_passed
