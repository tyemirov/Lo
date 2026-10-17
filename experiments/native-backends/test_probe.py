import json
from pathlib import Path
import subprocess
import sys

SCRIPT = Path(__file__).with_name('probe.py')


def test_probe_contract():
    result = subprocess.run([sys.executable, str(SCRIPT), 'check'], capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    contract = json.loads(result.stdout)
    assert contract['expected_stdout'] == '23 0\n0 7\n'
    assert contract['targets'] == ['aarch64-apple-darwin', 'aarch64-unknown-linux-gnu']
    assert contract['backends'] == ['llvm', 'cranelift']


def test_invalid_action_fails():
    result = subprocess.run([sys.executable, str(SCRIPT), 'unknown'], capture_output=True, text=True)
    assert result.returncode == 2
    assert 'invalid choice' in result.stderr


def test_run_requires_explicit_bootstrap(tmp_path):
    isolated = tmp_path / 'experiments/native-backends/probe.py'
    isolated.parent.mkdir(parents=True)
    isolated.write_text(SCRIPT.read_text())
    result = subprocess.run([sys.executable, str(isolated), 'run'], capture_output=True, text=True)
    assert result.returncode == 1
    assert 'missing bootstrap input:' in result.stderr


def test_check_rejects_false_success(tmp_path):
    import shutil
    isolated = tmp_path / 'experiments/native-backends'
    shutil.copytree(SCRIPT.parent, isolated, ignore=shutil.ignore_patterns('__pycache__', '.pytest_cache'))
    evidence = tmp_path / 'docs/evidence/native-backends'
    evidence.mkdir(parents=True)
    cells = [dict(backend=b, target=t, passed=True, run=dict(exit_code=0, stdout='incorrect'))
             for b in ['llvm', 'cranelift']
             for t in ['aarch64-apple-darwin', 'aarch64-unknown-linux-gnu']]
    (evidence / 'experiment.json').write_text(json.dumps(dict(cells=cells)))
    result = subprocess.run([sys.executable, str(isolated / 'probe.py'), 'check'], capture_output=True, text=True)
    assert result.returncode == 1
    assert 'incorrect result' in result.stderr


def test_run_rejects_stale_preparation_before_docker(tmp_path):
    import shutil
    isolated = tmp_path / 'experiments/native-backends'
    shutil.copytree(SCRIPT.parent, isolated, ignore=shutil.ignore_patterns('__pycache__', '.pytest_cache'))
    scratch = tmp_path / '_scratch/native-backends-20261016'
    scratch.mkdir(parents=True)
    (scratch / 'bootstrap.json').write_text(json.dumps({'source_sha256': {'experiments/native-backends/Dockerfile': 'stale'}}))
    result = subprocess.run([sys.executable, str(isolated / 'probe.py'), 'run'], capture_output=True, text=True)
    assert result.returncode == 1
    assert 'stale bootstrap input' in result.stderr
