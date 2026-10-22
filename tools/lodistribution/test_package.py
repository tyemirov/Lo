"""CLI contracts for offline distribution construction."""
from __future__ import annotations
import json
from collections.abc import Iterator
import os
from pathlib import Path
import subprocess
import sys
import uuid
import shutil
import pytest

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / 'tools/lodistribution/package.py'
PREPARE = ROOT / 'tools/lodistribution/prepare.py'

@pytest.fixture
def state() -> Iterator[Path]:
    path = ROOT / '_scratch/compiler-distribution/tests' / uuid.uuid4().hex
    path.mkdir(parents=True)
    yield path
    shutil.rmtree(path)

def invoke(script: Path, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, '-B', str(script), *args], cwd=ROOT,
                          env=dict(os.environ, CI='1', PYTHONDONTWRITEBYTECODE='1'),
                          stdin=subprocess.DEVNULL, capture_output=True, text=True)

def test_preparation_requires_explicit_network(state: Path) -> None:
    result = invoke(PREPARE, '--state', str(state))
    assert result.returncode != 0
    assert '--download' in result.stderr
    assert not (state / 'prepared.json').exists()

def test_package_rejects_linux_metal_before_writing(state: Path) -> None:
    result = invoke(PACKAGE, '--prepared', str(state / 'prepared.json'),
                    '--runtime', str(state / 'runtime'), '--host', 'linux/arm64',
                    '--variant', 'metal', '--go', '/opt/homebrew/bin/go', '--output', str(state / 'out'))
    assert result.returncode != 0
    assert 'Metal requires darwin/arm64' in result.stderr
    assert not (state / 'out').exists()

def test_package_rejects_corrupt_prepared_input(state: Path) -> None:
    source = state / 'clang'
    source.write_bytes(b'corrupt')
    prepared = state / 'prepared.json'
    prepared.write_text(json.dumps({'tools': {'darwin/arm64': {'clang': {'path': str(source.relative_to(ROOT)), 'sha256': '0' * 64}}}}))
    result = invoke(PACKAGE, '--prepared', str(prepared), '--runtime', str(state / 'runtime'),
                    '--host', 'darwin/arm64', '--variant', 'software', '--go', '/opt/homebrew/bin/go',
                    '--output', str(state / 'out'))
    assert result.returncode != 0
    assert 'digest' in result.stderr
    assert not (state / 'out').exists()

@pytest.fixture
def prepared_inputs() -> tuple[Path, Path]:
    prepared = ROOT / '_scratch/compiler-distribution/prepared.json'
    runtime = (ROOT / os.environ.get('LO_TEST_RUNTIME_DISTRIBUTION', '_scratch/native-runtime/distribution')).resolve()
    assert runtime.is_relative_to(ROOT / '_scratch')
    if not prepared.is_file() or not (runtime / 'build-receipt.json').is_file():
        pytest.skip('prepare the pinned compiler and native runtime inputs for package integration tests')
    return prepared, runtime


def package_args(prepared: Path, runtime: Path, output: Path) -> tuple[str, ...]:
    host = 'darwin/arm64' if sys.platform == 'darwin' else 'linux/arm64'
    return ('--prepared', str(prepared), '--runtime', str(runtime), '--host', host,
            '--variant', 'software', '--go', str(Path(shutil.which('go')).resolve()), '--output', str(output))


def test_package_rejects_incomplete_source_inventory(state: Path, prepared_inputs: tuple[Path, Path]) -> None:
    prepared, runtime = prepared_inputs
    record = json.loads(prepared.read_text())
    record['sources'] = []
    changed = state / 'prepared.json'
    changed.write_text(json.dumps(record))
    result = invoke(PACKAGE, *package_args(changed, runtime, state / 'out'))
    assert result.returncode != 0
    assert 'source inventory' in result.stderr
    assert not (state / 'out').exists()


@pytest.mark.parametrize('subject', ['tools', 'packages', 'archives'])
def test_package_rejects_forged_preparation_inventory(state: Path, prepared_inputs: tuple[Path, Path], subject: str) -> None:
    prepared, runtime = prepared_inputs
    record = json.loads(prepared.read_text())
    if subject == 'tools':
        record['tools']['darwin/arm64']['clang']['sha256'] = '0' * 64
    elif subject == 'packages':
        record['packages'] = []
    else:
        record['archives']['llvm-linux']['sha256'] = '0' * 64
    changed = state / 'prepared.json'
    changed.write_text(json.dumps(record))
    result = invoke(PACKAGE, *package_args(changed, runtime, state / 'out'))
    assert result.returncode != 0
    assert not (state / 'out').exists()


def test_package_rejects_stale_runtime_receipt(state: Path, prepared_inputs: tuple[Path, Path]) -> None:
    prepared, runtime = prepared_inputs
    changed = state / 'runtime'
    changed.mkdir()
    shutil.copyfile(runtime / 'toolchain.json', changed / 'toolchain.json')
    receipt = json.loads((runtime / 'build-receipt.json').read_text())
    receipt['runtime_source_sha256'] = '0' * 64
    (changed / 'build-receipt.json').write_text(json.dumps(receipt))
    result = invoke(PACKAGE, *package_args(prepared, changed, state / 'out'))
    assert result.returncode != 0
    assert 'runtime source' in result.stderr
    assert not (state / 'out').exists()


@pytest.mark.parametrize('subject,expected', [('manifest', 'runtime manifest'), ('process', 'launcher.c'), ('metal', 'Metal source inventory')])
def test_package_rejects_stale_runtime_identities(state: Path, prepared_inputs: tuple[Path, Path], subject: str, expected: str) -> None:
    prepared, runtime = prepared_inputs
    changed = state / 'runtime'
    changed.mkdir()
    shutil.copyfile(runtime / 'toolchain.json', changed / 'toolchain.json')
    receipt = json.loads((runtime / 'build-receipt.json').read_text())
    if subject == 'manifest':
        receipt['manifest_sha256'] = '0' * 64
    elif subject == 'process':
        receipt['process_sources']['launcher.c']['sha256'] = '0' * 64
    else:
        receipt['metal']['sources'] = {}
    (changed / 'build-receipt.json').write_text(json.dumps(receipt))
    args = package_args(prepared, changed, state / 'out')
    if subject == 'metal':
        args = tuple({'software': 'metal', 'linux/arm64': 'darwin/arm64'}.get(value, value) for value in args)
    result = invoke(PACKAGE, *args)
    assert result.returncode != 0
    assert expected in result.stderr
    assert not (state / 'out').exists()


def test_package_rejects_preexisting_receipt(state: Path) -> None:
    output = state / 'out'
    output.with_suffix('.receipt.json').write_text('existing')
    result = invoke(PACKAGE, *package_args(state / 'prepared.json', state / 'runtime', output))
    assert result.returncode != 0
    assert 'already exists' in result.stderr
    assert output.with_suffix('.receipt.json').read_text() == 'existing'


@pytest.mark.parametrize('operation', ['archive', 'receipt'])
def test_output_write_failure_cleans_artifacts_and_allows_retry(state: Path, prepared_inputs: tuple[Path, Path], operation: str) -> None:
    prepared, runtime = prepared_inputs
    output = state / 'package'
    # Inject the failing output adapter, and keep the CLI and package work real.
    harness = state / 'failing_writer.py'
    harness.write_text("""import pathlib, runpy, sys, tarfile
original_write = pathlib.Path.write_text
original_open = tarfile.open
operation, script = sys.argv[1:3]
sys.argv = sys.argv[2:]
sys.path.insert(0, str(pathlib.Path(script).parent))
def write(path, *args, **kwargs):
    if path.name.endswith('.receipt.json'):
        original_write(path, 'partial receipt')
        raise OSError('injected receipt write failure')
    return original_write(path, *args, **kwargs)
def open_archive(name=None, *args, **kwargs):
    if name is not None and pathlib.Path(name).name == 'package.tar':
        pathlib.Path(name).write_bytes(b'partial archive')
        raise OSError('injected archive write failure')
    return original_open(name, *args, **kwargs)
if operation == 'receipt':
    pathlib.Path.write_text = write
else:
    tarfile.open = open_archive
runpy.run_path(script, run_name='__main__')
""")
    result = subprocess.run([sys.executable, '-B', str(harness), operation, str(PACKAGE),
                             *package_args(prepared, runtime, output)], cwd=ROOT,
                            env=dict(os.environ, CI='1', PYTHONDONTWRITEBYTECODE='1'),
                            stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode != 0
    assert f'injected {operation} write failure' in result.stderr
    assert not output.exists()
    assert not output.with_suffix('.tar').exists()
    assert not output.with_suffix('.receipt.json').exists()
    assert not output.with_name(output.name + '.building').exists()
    retry = invoke(PACKAGE, *package_args(prepared, runtime, output))
    assert retry.returncode == 0, retry.stderr


def test_package_archive_relocates_and_ignores_ambient_go_options(state: Path, prepared_inputs: tuple[Path, Path]) -> None:
    import hashlib
    prepared, runtime = prepared_inputs
    output = state / 'package'
    environment = dict(os.environ, CI='1', PYTHONDONTWRITEBYTECODE='1',
                       GOFLAGS='-invalid-ambient-option', GOCACHEPROG=str(state / 'absent-cache-program'))
    result = subprocess.run([sys.executable, '-B', str(PACKAGE), *package_args(prepared, runtime, output)],
                            cwd=ROOT, env=environment, stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    receipt = json.loads(result.stdout)
    assert set(json.loads((output / 'toolchain.json').read_text())['targets']) == {'darwin/arm64', 'linux/arm64'}
    assert 'metal' not in json.loads((output / 'toolchain.json').read_text())
    relocation = state / 'relocated'
    relocation.mkdir()
    extracted = subprocess.run(['/usr/bin/tar', '-xf', str(ROOT / receipt['archive']), '-C', str(relocation)],
                               env={'CI': '1'}, stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert extracted.returncode == 0, extracted.stderr
    frontend = relocation / 'package/bin/lo'
    fixture = relocation / 'hello.lo'
    fixture.write_text('flow Main() -> Int { return Int.new(42) }\n')
    run = subprocess.run([str(frontend), 'check', str(fixture)], cwd=relocation,
                         env={'PATH': '', 'CI': '1', 'HOME': str(state)}, stdin=subprocess.DEVNULL,
                         capture_output=True, text=True)
    assert run.returncode == 0, run.stderr
    inventory = json.loads((output / 'inventory.json').read_text())['files']
    assert inventory['bin/lo'] == hashlib.sha256(frontend.read_bytes()).hexdigest()
    assert 'source/lo-source.tar.gz' in inventory
    provenance = json.loads((output / 'provenance.json').read_text())
    assert {entry['source'] for entry in provenance['sources']} >= {'gcc-12', 'glibc'}
    assert provenance['frontend_source_identity']['files']['cmd/lo/main.go'] == hashlib.sha256((ROOT / 'cmd/lo/main.go').read_bytes()).hexdigest()
    assert set(provenance['frontend_source_identity']['files']) <= set(provenance['source_identity']['files'])


def test_package_rejects_frontend_source_archive_drift(state: Path, prepared_inputs: tuple[Path, Path]) -> None:
    prepared, runtime = prepared_inputs
    output = state / 'package'
    replacement = state / 'changed.go'
    replacement.write_text('package changed\n')
    harness = state / 'changed_archive.py'
    harness.write_text("""import pathlib, runpy, sys, tarfile
replacement, script = sys.argv[1:3]
sys.argv = sys.argv[2:]
sys.path.insert(0, str(pathlib.Path(script).parent))
original_add = tarfile.TarFile.add
def add(self, name, arcname=None, *args, **kwargs):
    if arcname == 'cmd/lo/main.go':
        name = replacement
    return original_add(self, name, arcname, *args, **kwargs)
tarfile.TarFile.add = add
runpy.run_path(script, run_name='__main__')
""")
    result = subprocess.run([sys.executable, '-B', str(harness), str(replacement), str(PACKAGE),
                             *package_args(prepared, runtime, output)], cwd=ROOT,
                            env=dict(os.environ, CI='1', PYTHONDONTWRITEBYTECODE='1'),
                            stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode != 0
    assert 'packaged source differs from construction snapshot' in result.stderr
    assert not output.exists()
    assert not output.with_suffix('.tar').exists()


def test_package_rejects_source_snapshot_drift_after_frontend_build(state: Path, prepared_inputs: tuple[Path, Path]) -> None:
    prepared, runtime = prepared_inputs
    output = state / 'package'
    harness = state / 'changed_snapshot.py'
    harness.write_text("""import hashlib, pathlib, runpy, sys
script = sys.argv[1]
sys.argv = sys.argv[1:]
sys.path.insert(0, str(pathlib.Path(script).parent))
original_digest = hashlib.file_digest
reads = 0
class ChangedDigest:
    def hexdigest(self):
        return '0' * 64
def digest(source, *args, **kwargs):
    global reads
    if str(getattr(source, 'name', '')).endswith('/cmd/lo/main.go'):
        reads += 1
        if reads > 1:
            return ChangedDigest()
    return original_digest(source, *args, **kwargs)
hashlib.file_digest = digest
runpy.run_path(script, run_name='__main__')
""")
    result = subprocess.run([sys.executable, '-B', str(harness), str(PACKAGE),
                             *package_args(prepared, runtime, output)], cwd=ROOT,
                            env=dict(os.environ, CI='1', PYTHONDONTWRITEBYTECODE='1'),
                            stdin=subprocess.DEVNULL, capture_output=True, text=True)
    assert result.returncode != 0
    assert 'source files changed during package construction' in result.stderr
    assert not output.exists()
    assert not output.with_suffix('.tar').exists()
