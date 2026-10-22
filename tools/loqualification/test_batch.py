"""Public command tests for batched Linux qualification."""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'tools/loqualification/batch.py'


@pytest.fixture
def work(tmp_path: Path) -> Path:
    assert tmp_path.resolve().is_relative_to(ROOT / '_scratch')
    return tmp_path


def invoke(work: Path, manifest: dict) -> subprocess.CompletedProcess[str]:
    source = work / 'cases.json'
    source.write_text(json.dumps(manifest))
    return subprocess.run([sys.executable, '-B', str(SCRIPT),
                           '--preparation', str(ROOT / '_scratch/native-runtime'),
                           '--package', str(ROOT / '_scratch/compiler-distribution/packages/lo-linux-arm64-software'),
                           '--manifest', str(source), '--work', str(work / 'guest')],
                          cwd=ROOT, env=dict(os.environ, CI='1'), stdin=subprocess.DEVNULL,
                          capture_output=True, text=True, check=False)


def test_batch_rejects_duplicate_case_ids_before_start(work: Path) -> None:
    case = {'id': 'duplicate', 'argv': ['/compiler/bin/lo', 'version'], 'cwd': '/work',
            'environment': {}, 'stdin': '', 'timeout_seconds': 10, 'outputs': []}
    got = invoke(work, {'files': [], 'cases': [case, case]})
    assert got.returncode == 1
    assert 'duplicate case ID' in got.stderr
    assert not (work / 'guest').exists()


def test_batch_rejects_payload_escape_before_start(work: Path) -> None:
    case = {'id': 'one', 'argv': ['/compiler/bin/lo', 'version'], 'cwd': '/work',
            'environment': {}, 'stdin': '', 'timeout_seconds': 10, 'outputs': []}
    got = invoke(work, {'files': [{'source': 'go.mod', 'path': '/work/../../escape'}], 'cases': [case]})
    assert got.returncode == 1
    assert 'guest path' in got.stderr
    assert not (work / 'guest').exists()


def test_receiver_rejects_missing_case_result(work: Path) -> None:
    import batch
    serial = work / 'serial.log'
    serial.write_bytes(b'LO_QUALIFICATION_COMPLETE:1\n')
    with pytest.raises(ValueError, match='missing case'):
        batch.receive(serial, ('one',))


def test_receiver_rejects_duplicate_and_truncated_stream(work: Path) -> None:
    import batch
    serial = work / 'serial.log'
    serial.write_bytes(b'LO_QUALIFICATION_DATA:0:stdout:41\n'
                       b'LO_QUALIFICATION_STREAM:0:stdout:2\n'
                       b'LO_QUALIFICATION_STREAM:0:stderr:0\n'
                       b'LO_QUALIFICATION_RESULT:0:0:12\nLO_QUALIFICATION_COMPLETE:1\n')
    with pytest.raises(ValueError, match='byte count'):
        batch.receive(serial, ('one',))
    serial.write_bytes(b'LO_QUALIFICATION_STREAM:0:stdout:0\nLO_QUALIFICATION_STREAM:0:stdout:0\n')
    with pytest.raises(ValueError, match='repeated stream'):
        batch.receive(serial, ('one',))


def test_receiver_returns_only_complete_cases(work: Path) -> None:
    import batch
    serial = work / 'serial.log'
    serial.write_bytes(b'LO_QUALIFICATION_DATA:0:stdout:31300a\n'
                       b'LO_QUALIFICATION_STREAM:0:stdout:3\n'
                       b'LO_QUALIFICATION_STREAM:0:stderr:0\n'
                       b'LO_QUALIFICATION_RESULT:0:0:1000\nLO_QUALIFICATION_COMPLETE:1\n')
    cases = batch.receive(serial, ('one',))
    assert len(cases) == 1
    assert cases[0].status == 0
    assert cases[0].stdout == b'10\n'
    assert cases[0].elapsed_ns == 1000


def test_batch_rejects_overlapping_payload_roots(work: Path) -> None:
    case = {'id': 'one', 'argv': ['/compiler/bin/lo', 'version'], 'cwd': '/work',
            'environment': {}, 'stdin': '', 'timeout_seconds': 10, 'outputs': []}
    got = invoke(work, {'files': [{'source': 'go.mod', 'path': '/work/a'},
                                  {'source': 'go.mod', 'path': '/work/a-b'},
                                  {'source': 'go.mod', 'path': '/work/a/child'}], 'cases': [case]})
    assert got.returncode == 1
    assert 'payload paths overlap' in got.stderr
    assert not (work / 'guest').exists()


def test_batch_rejects_clean_environment_override(work: Path) -> None:
    case = {'id': 'one', 'argv': ['/compiler/bin/lo', 'version'], 'cwd': '/work',
            'environment': {'PATH': '/host/tools'}, 'stdin': '', 'timeout_seconds': 10, 'outputs': []}
    got = invoke(work, {'files': [], 'cases': [case]})
    assert got.returncode == 1
    assert 'cannot override environment key' in got.stderr
    assert not (work / 'guest').exists()


def test_receiver_keeps_progress_out_of_result_stream(work: Path) -> None:
    import batch
    serial = work / 'serial.log'
    serial.write_bytes(b'LO_QUALIFICATION_PROGRESS:0:5000000000:3:31300a\n'
                       b'LO_QUALIFICATION_DATA:0:stdout:31300a\n'
                       b'LO_QUALIFICATION_STREAM:0:stdout:3\n'
                       b'LO_QUALIFICATION_STREAM:0:stderr:0\n'
                       b'LO_QUALIFICATION_RESULT:0:0:6000000000\nLO_QUALIFICATION_COMPLETE:1\n')
    result = batch.receive(serial, ('one',))[0]
    assert result.stdout == b'10\n'
    assert result.elapsed_ns == 6000000000


EXPORT = (b'LO_QUALIFICATION_DATA:0:stdout:00ff410a\n'
          b'LO_QUALIFICATION_STREAM:0:stdout:4\n'
          b'LO_QUALIFICATION_DATA:0:stderr:657272\n'
          b'LO_QUALIFICATION_STREAM:0:stderr:3\n'
          b'LO_QUALIFICATION_OUTPUT:0:0:1\n'
          b'LO_QUALIFICATION_DATA:0:file0:deadbeef\n'
          b'LO_QUALIFICATION_STREAM:0:file0:4\n'
          b'LO_QUALIFICATION_RESULT:0:7:1000\nLO_QUALIFICATION_COMPLETE:1\n')


def controlled_guest(work: Path, monkeypatch: pytest.MonkeyPatch,
                     export: bytes | None, console: bytes) -> dict:
    """Inject QEMU at the public runner boundary, retaining real batch staging."""
    import batch
    preparation, package = work / 'preparation', work / 'package'
    (preparation / 'linux-sysroot/lib').mkdir(parents=True)
    (package / 'bin').mkdir(parents=True)
    (package / 'bin/lo').write_bytes(b'frontend')
    (package / 'toolchain.json').write_text('{}')
    init = work / 'init'
    init.write_bytes(b'controlled initializer')
    monkeypatch.setattr(batch, 'initializer', lambda *_: init)
    case = {'id': 'one', 'argv': ['/compiler/bin/lo', 'version'], 'cwd': '/work',
            'environment': {}, 'stdin': '', 'timeout_seconds': 10, 'outputs': ['/result/data']}
    manifest = work / 'batch.json'
    manifest.write_text(json.dumps({'files': [], 'cases': [case]}))

    def execute(command: list[str], **kwargs: object) -> subprocess.CompletedProcess:
        if command[0] == '/usr/bin/tar':
            Path(command[command.index('-cf') + 1]).write_bytes(b'controlled archive')
        else:
            kwargs['stdout'].write(console)
            if export is not None:
                (work / 'guest/export.log').write_bytes(export)
            # Assertions occur after the fake guest writes to prove admission uses
            # the new channel rather than merely adding unused QEMU arguments.
            assert '-chardev' in command
            assert command[command.index('-chardev') + 1] == f'file,id=lo-export,path={work / "guest/export.log"}'
            assert 'virtio-serial-pci,id=lo-transport' in command
            assert 'virtserialport,bus=lo-transport.0,nr=1,chardev=lo-export,name=lo.export' in command
        return subprocess.CompletedProcess(command, 0, b'', b'')

    monkeypatch.setattr(batch.subprocess, 'run', execute)
    return batch.run(preparation, package, manifest, work / 'guest')


def test_public_batch_uses_dedicated_export_despite_noisy_console(work: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    console = b'kernel watchdog noise\nLO_QUALIFICATION_DATA:0:stdout:broken\n' + EXPORT
    report = controlled_guest(work, monkeypatch, EXPORT, console)
    assert report['cases'][0]['status'] == 7
    assert report['cases'][0]['elapsed_ns'] == 1000
    assert (work / 'guest/0.stdout').read_bytes() == b'\x00\xffA\n'
    assert (work / 'guest/0.stderr').read_bytes() == b'err'
    assert (work / 'guest/0.file0').read_bytes() == bytes.fromhex('deadbeef')
    assert (work / 'guest/serial.log').read_bytes() == console
    assert report['export'] == str((work / 'guest/export.log').relative_to(ROOT))


@pytest.mark.parametrize('export,reason', [
    (None, 'export channel missing'),
    (EXPORT[:-1], 'unterminated'),
    (b'kernel text must not enter the export\n' + EXPORT, 'unexpected export record'),
    (EXPORT.replace(b'1000', b'not-an-integer'), 'invalid guest batch protocol'),
    (EXPORT.replace(b'LO_QUALIFICATION_COMPLETE:1\n', b''), 'completion missing'),
])
def test_public_batch_rejects_bad_export_even_with_valid_console(work: Path, monkeypatch: pytest.MonkeyPatch,
                                                                export: bytes | None, reason: str) -> None:
    with pytest.raises(ValueError, match=reason):
        controlled_guest(work, monkeypatch, export, EXPORT)
    assert not (work / 'guest/report.json').exists()


@pytest.mark.parametrize('mode,status', [('timeout', 124), ('exit', 7), ('signal', 143)])
def test_public_batch_reaps_detached_descendants_before_next_case(work: Path, mode: str, status: int) -> None:
    import batch

    supplied = os.environ.get('LO_NATIVE_PREPARATION')
    if supplied is None:
        pytest.skip('LO_NATIVE_PREPARATION selects the prepared software guest')
    preparation = batch.local(Path(supplied))
    distribution = preparation / 'distribution'
    record = json.loads((distribution / 'toolchain.json').read_text())
    target = record['targets']['linux/arm64']
    package = work / 'package'
    (package / 'bin').mkdir(parents=True)
    executable, obj = package / 'bin/lo', work / 'probe.o'
    compiler = batch.verified({**record['compiler'], 'path': str(distribution / record['compiler']['path'])})
    linker = batch.verified({**target['linker'], 'path': str(distribution / target['linker']['path'])})
    values = {'{object}': str(obj), '{output}': str(executable),
              '{runtime}': str(batch.local(distribution / target['runtime']['path'])),
              **{'{input:' + name + '}': str(batch.local(distribution / pin['path']))
                 for name, pin in target['inputs'].items()}}
    commands = [[str(compiler), '--target=' + target['triple'], '-c',
                 str(ROOT / 'testdata/native-qualification/batch-process-cleanup.c'), '-o', str(obj)],
                [str(linker), *[values.get(arg, arg) for arg in target['link_args']]]]
    for command in commands:
        built = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, check=False)
        assert built.returncode == 0, built.stderr
    (package / 'toolchain.json').write_text('{}')
    manifest = work / 'batch.json'
    cases = [{'id': operation, 'argv': ['/compiler/bin/lo', operation], 'cwd': '/work',
              'environment': {}, 'stdin': '', 'timeout_seconds': 3, 'outputs': []}
             for operation in (mode, 'inspect')]
    manifest.write_text(json.dumps({'files': [], 'cases': cases}))
    report = batch.run(preparation, package, manifest, work / 'guest')
    assert [case['status'] for case in report['cases']] == [status, 0]
    assert (work / 'guest/0.stdout').read_bytes() == b'descendants ready\n'
    assert (work / 'guest/1.stdout').read_bytes() == b'descendants absent\n'
