"""Verify benchmark fixtures use the native runtime ABI."""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def test_public_generator_emits_native_abi_fixture(tmp_path: Path) -> None:
    result = subprocess.run(
        ['go', 'run', './tools/nativebench', '--output', str(tmp_path)],
        cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True,
    )
    assert result.returncode == 0, result.stderr
    fixture = tmp_path / 'startup'
    assert (fixture / 'allocation.c').is_file()
    assert (fixture / 'benchmark.c').is_file(), 'native fixture must call the native runtime ABI'
    assert not (fixture / 'program.go').exists(), 'native fixture must not generate Go'
    assert 'lo_context_run' in (fixture / 'benchmark.c').read_text()
    assert (fixture / 'source.lo').read_text() == 'return 7\n'
    assert 'eval.Program' in (fixture / 'program_test.go').read_text()
    compiled = subprocess.run(
        ['go', 'test', '-c', '-o', str(fixture / 'interpreter.test'), str(fixture / 'program_test.go')],
        cwd=ROOT, stdin=subprocess.DEVNULL, text=True, capture_output=True,
    )
    assert compiled.returncode == 0, compiled.stderr
