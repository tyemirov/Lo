"""Public CLI rejection tests; all temporary paths are supplied by pytest."""
from __future__ import annotations
import json
from pathlib import Path
import subprocess
import sys
from prepare import ROOT

def invoke(script: str, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, '-B', str(ROOT / 'tools/nativelowering' / script), *args], cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)

def test_preparation_rejects_outside_repository() -> None:
    result = invoke('prepare.py', '--state', '/')
    assert result.returncode == 1
    assert 'strictly inside Lo' in result.stderr

def test_preparation_rejects_source_directory() -> None:
    result = invoke('prepare.py', '--state', 'tools/nativelowering')
    assert result.returncode == 1
    assert 'under Lo/_scratch' in result.stderr

def test_preparation_missing_archive_is_explicit(tmp_path: Path) -> None:
    result = invoke('prepare.py', '--state', str(tmp_path))
    assert result.returncode == 1
    assert 'missing pinned archive' in result.stderr
    assert list(tmp_path.iterdir()) == []

def test_execution_rejects_stale_pin_before_output(tmp_path: Path) -> None:
    manifest = tmp_path / 'tools.json'
    manifest.write_text(json.dumps({'version': 'obsolete', 'archive': {}}))
    state = tmp_path / 'execution'
    result = invoke('run.py', '--lo', 'bin/lo', '--tools', str(manifest), '--state', str(state))
    assert result.returncode == 1
    assert 'repository LLVM pin' in result.stderr
    assert not state.exists()
