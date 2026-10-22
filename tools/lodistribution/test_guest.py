"""Public command boundary for the isolated Linux compiler guest."""
from pathlib import Path
import os
import subprocess
import sys
import shutil
import uuid
import pytest


def test_guest_rejects_missing_installed_frontend() -> None:
    root = Path(__file__).resolve().parents[2]
    state = root / '_scratch/compiler-distribution/tests/missing'
    result = subprocess.run([sys.executable, '-B', str(root / 'tools/lodistribution/guest.py'),
                             '--preparation', str(root / '_scratch/native-runtime'),
                             '--package', str(state / 'package'), '--source', str(root / 'go.mod'),
                             '--target', 'linux/arm64', '--output', str(state / 'program'),
                             '--work', str(state / 'work')], cwd=root, stdin=subprocess.DEVNULL,
                            env=dict(os.environ, CI='1', PYTHONDONTWRITEBYTECODE='1'),
                            capture_output=True, text=True, check=False)
    assert result.returncode == 1
    assert 'missing installed frontend' in result.stderr
    assert not state.exists()


@pytest.mark.parametrize('wire', [
    b'LO_DISTRIBUTION_RESULT:0\n',
    b'LO_DISTRIBUTION_ARTIFACT:0\nLO_DISTRIBUTION_RESULT:0\n',
    b'LO_DISTRIBUTION_DATA:artifact:7f45\nLO_DISTRIBUTION_ARTIFACT:4\nLO_DISTRIBUTION_RESULT:0\n',
])
def test_receiver_rejects_missing_or_truncated_success_artifact(wire: bytes) -> None:
    from guest import receive
    root = Path(__file__).resolve().parents[2]
    state = root / '_scratch/compiler-distribution/tests/receiver' / uuid.uuid4().hex
    state.mkdir(parents=True)
    try:
        serial = state / 'serial.log'
        serial.write_bytes(wire)
        with pytest.raises(ValueError, match='artifact'):
            receive(serial)
    finally:
        shutil.rmtree(state)


def test_receiver_returns_verified_artifact_bytes() -> None:
    from guest import receive
    root = Path(__file__).resolve().parents[2]
    state = root / '_scratch/compiler-distribution/tests/receiver' / uuid.uuid4().hex
    state.mkdir(parents=True)
    try:
        serial = state / 'serial.log'
        serial.write_bytes(b'LO_DISTRIBUTION_DATA:artifact:7f45\n'
                           b'LO_DISTRIBUTION_ARTIFACT:2\nLO_DISTRIBUTION_RESULT:0\n')
        result = receive(serial)
        assert result.status == 0
        assert result.artifact == b'\x7fE'
    finally:
        shutil.rmtree(state)


def distribution_wire(artifact: bytes) -> bytes:
    receipt = b'{"status":"ok"}'
    return (b'LO_DISTRIBUTION_DATA:stdout:' + receipt.hex().encode() + b'\n'
            b'LO_DISTRIBUTION_DATA:artifact:' + artifact.hex().encode() + b'\n'
            b'LO_DISTRIBUTION_ARTIFACT:' + str(len(artifact)).encode() + b'\n'
            b'LO_DISTRIBUTION_RESULT:0\n')


@pytest.fixture
def injected_guest(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    import guest
    root = Path(__file__).resolve().parents[2]
    assert tmp_path.resolve().is_relative_to(root / '_scratch')
    preparation = tmp_path / 'preparation'
    (preparation / 'linux-sysroot/lib').mkdir(parents=True)
    package = tmp_path / 'package'
    (package / 'bin').mkdir(parents=True)
    (package / 'bin/lo').write_bytes(b'controlled installed frontend')
    source = tmp_path / 'source.lo'
    source.write_text('return 7')
    initializer = tmp_path / 'initializer'
    initializer.write_bytes(b'controlled initializer dependency')
    monkeypatch.setattr(guest, 'initializer', lambda *args: initializer)
    actual_run = subprocess.run
    def compile_with(console: bytes, export: bytes | None):
        work = tmp_path / 'guest'
        output = tmp_path / 'program'
        output.write_bytes(b'previous artifact')
        commands = []
        def injected_run(argv, **kwargs):
            if Path(argv[0]).name != 'qemu-system-aarch64':
                return actual_run(argv, **kwargs)
            commands.append(argv)
            kwargs['stdout'].write(console)
            if export is not None:
                (work / 'export.log').write_bytes(export)
            return subprocess.CompletedProcess(argv, 0)
        monkeypatch.setattr(guest.subprocess, 'run', injected_run)
        return guest, (preparation, package, source, 'linux/arm64', output, work), commands
    return compile_with


def test_public_compile_uses_dedicated_export_despite_console_noise(injected_guest) -> None:
    artifact = b'\x7fELFcontrolled executable\x00\xff'
    console = (b'watchdog: BUG: soft lockup\nLO_DISTRIBUTION_DATA:artifact:7f45'
               b'[ 12.3] kernel watchdog interleaving\nLO_DISTRIBUTION_RESULT:99\n')
    guest, args, commands = injected_guest(console, distribution_wire(artifact))
    result = guest.compile(*args)
    assert args[4].read_bytes() == artifact
    assert result['artifact_bytes'] == len(artifact)
    assert result['export'] == str(args[5] / 'export.log')
    assert (args[5] / 'serial.log').read_bytes() == console
    command = commands[0]
    assert 'virtio-serial-pci,id=lo-transport' in command
    assert 'virtserialport,bus=lo-transport.0,nr=1,chardev=lo-export,name=lo.export' in command
    assert 'file,id=lo-export,path=' + str(args[5] / 'export.log') in command


@pytest.mark.parametrize('export', [None, b'', b'LO_DISTRIBUTION_DATA:artifact:7f45\n',
                                   b'LO_DISTRIBUTION_DATA:artifact:not-hex\n',
                                   b'LO_DISTRIBUTION_ARTIFACT:3\nLO_DISTRIBUTION_RESULT:0\n'])
def test_public_compile_rejects_bad_dedicated_export_even_with_valid_console(injected_guest, export: bytes | None) -> None:
    guest, args, _ = injected_guest(distribution_wire(b'console artifact must not publish'), export)
    with pytest.raises((ValueError, OSError)):
        guest.compile(*args)
    assert args[4].read_bytes() == b'previous artifact'
    assert not (args[5] / 'report.json').exists()


@pytest.mark.parametrize('problem', ['unknown', 'unterminated', 'after-result', 'artifact-fields',
                                     'result-fields', 'negative-status', 'status-range', 'entry-fields'])
def test_public_compile_rejects_noncanonical_export_records(injected_guest, problem: str) -> None:
    wire = distribution_wire(b'\x7fELF exact artifact')
    if problem == 'unknown':
        wire = b'kernel console noise must never enter this channel\n' + wire
    elif problem == 'unterminated':
        wire = wire[:-1]
    elif problem == 'after-result':
        wire += b'LO_DISTRIBUTION_ENTRY:/tmp/after-result\n'
    elif problem == 'artifact-fields':
        wire = wire.replace(b'LO_DISTRIBUTION_ARTIFACT:19\n', b'LO_DISTRIBUTION_ARTIFACT:19:extra\n')
    elif problem == 'result-fields':
        wire = wire.replace(b'LO_DISTRIBUTION_RESULT:0\n', b'LO_DISTRIBUTION_RESULT:0:extra\n')
    elif problem == 'entry-fields':
        wire = b'LO_DISTRIBUTION_ENTRY\n' + wire
    elif problem == 'negative-status':
        wire = wire.replace(b'LO_DISTRIBUTION_RESULT:0\n', b'LO_DISTRIBUTION_RESULT:-1\n')
    else:
        wire = wire.replace(b'LO_DISTRIBUTION_RESULT:0\n', b'LO_DISTRIBUTION_RESULT:256\n')
    guest, args, _ = injected_guest(distribution_wire(b'valid console is irrelevant'), wire)
    with pytest.raises(ValueError):
        guest.compile(*args)
    assert args[4].read_bytes() == b'previous artifact'
    assert not (args[5] / 'report.json').exists()
