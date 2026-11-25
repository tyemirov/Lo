"""Public session preparation for all installed compiler variants."""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name("distributions.py")


def fixture(tmp_path: Path) -> tuple[Path, Path, Path, dict[str, str]]:
    prepared = tmp_path / "prepared.json"
    prepared.write_text("{}")
    runtime = tmp_path / "runtime"
    runtime.mkdir()
    for name in ("toolchain.json", "build-receipt.json"):
        (runtime / name).write_text("{}")
    work = tmp_path / "packages"
    environment = dict(os.environ, LO_TEST_SESSION_DIRECTORY=str(tmp_path))
    return prepared, runtime, work, environment


@pytest.mark.parametrize("problem", ["missing_manifest", "missing_receipt", "old_work", "outside_session"])
def test_distribution_preparation_rejects_inputs_before_work(
    tmp_path: Path, problem: str
) -> None:
    prepared, runtime, work, environment = fixture(tmp_path)
    if problem == "missing_manifest":
        (runtime / "toolchain.json").unlink()
    elif problem == "missing_receipt":
        (runtime / "build-receipt.json").unlink()
    elif problem == "old_work":
        work.mkdir()
        (work / "retained").write_text("owner state")
    else:
        selected = tmp_path / "other_session"
        selected.mkdir()
        environment["LO_TEST_SESSION_DIRECTORY"] = str(selected)
    result = subprocess.run([sys.executable, "-B", str(SCRIPT), "--prepared", str(prepared),
                             "--runtime", str(runtime), "--work", str(work)],
                            cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    assert result.returncode == 1, result.stderr
    if problem == "old_work":
        assert (work / "retained").read_text() == "owner state"
    else:
        assert not work.exists()


@pytest.mark.parametrize("failure", ["", "lo-linux-arm64-software", "identity", "missing-output", "invalid-json"])
def test_distribution_preparation_uses_public_packages_and_cleans_partial_failure(
    tmp_path: Path, failure: str
) -> None:
    prepared, runtime, work, environment = fixture(tmp_path)
    prepared_bytes = b"{ \n}\n"
    prepared.write_bytes(prepared_bytes)
    harness = tmp_path / "producer.py"
    harness.write_text(r'''
import json,pathlib,runpy,subprocess,sys
failure,script=sys.argv[1:3]
sys.argv=sys.argv[2:]
def build(command,**kwargs):
    assert command[:3]==[sys.executable,'-B',str(pathlib.Path.cwd()/'tools/lodistribution/package.py')]
    options=dict(zip(command[3::2],command[4::2]))
    output=pathlib.Path(options['--output'])
    selected_prepared=pathlib.Path(options['--prepared'])
    assert selected_prepared==output.parents[1]/'prepared.json', 'producer state must belong to package work'
    assert selected_prepared.read_bytes()==(pathlib.Path(options['--runtime']).parent/'prepared.json').read_bytes(), 'prepared JSON bytes changed'
    expected='lo-'+options['--host'].replace('/','-')+'-'+options['--variant']
    assert output.name==expected
    assert pathlib.Path(options['--runtime']).name=='runtime'
    state=selected_prepared.parent/'producer-state'
    state.mkdir(exist_ok=True)
    (state/output.name).write_text('owned producer state')
    output.mkdir(parents=True)
    (output/'partial').write_text('producer output')
    (output/'toolchain.json').write_text('{}')
    (output/'inventory.json').write_text('{}')
    output.with_suffix('.tar').write_bytes(b'archive')
    output.with_suffix('.receipt.json').write_text('{}')
    if output.name==failure:
        return subprocess.CompletedProcess(command,7,b'',b'injected package producer failure')
    receipt={'package':str(output.relative_to(pathlib.Path.cwd())),
             'host':options['--host'],'variant':options['--variant']}
    if failure=='identity':receipt['host']='wrong-host'
    if failure=='missing-output':(output/'inventory.json').unlink()
    if failure=='invalid-json':return subprocess.CompletedProcess(command,0,b'null',b'')
    return subprocess.CompletedProcess(command,0,json.dumps(receipt).encode(),b'')
subprocess.run=build
runpy.run_path(script,run_name='__main__')
''')
    result = subprocess.run([sys.executable, "-B", str(harness), failure, str(SCRIPT),
                             "--prepared", str(prepared), "--runtime", str(runtime), "--work", str(work)],
                            cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, check=False)
    if failure:
        assert result.returncode == 1
        if failure == "lo-linux-arm64-software":
            assert "injected package producer failure" in result.stderr
            assert "status=7" in result.stderr
        else:
            assert "public producer" in result.stderr
        assert not work.exists()
    else:
        assert result.returncode == 0, result.stderr
        record = json.loads(result.stdout)
        assert record["status"] == "pass"
        assert set(record["packages"]) == {"lo-darwin-arm64-software", "lo-linux-arm64-software", "lo-darwin-arm64-metal"}
        assert all(Path(path).is_dir() for path in record["packages"].values())
        assert {path.name for path in (work / "producer-state").iterdir()} == set(record["packages"])
    assert not (prepared.parent / "producer-state").exists()
    assert prepared.read_bytes() == prepared_bytes
    assert (runtime / "build-receipt.json").read_text() == "{}"


@pytest.mark.parametrize("failure", [False, True])
def test_distribution_preparation_starts_bounded_producers_before_waiting(
    tmp_path: Path, failure: bool
) -> None:
    prepared, runtime, work, environment = fixture(tmp_path)
    harness = tmp_path / "parallel_producer.py"
    tracker = tmp_path / "producer-completion.json"
    harness.write_text(r'''
import atexit,json,os,pathlib,runpy,shutil,subprocess,sys,threading,time
failure,tracker,script=sys.argv[1:4]
sys.argv=sys.argv[3:]
os.cpu_count=lambda:3
barrier=threading.Barrier(3)
lock=threading.Lock()
started=[]
completed=[]
cleanup=[]
active=peak=0
original_cleanup=shutil.rmtree
def remove(path,*args,**kwargs):
    with lock:
        cleanup.append(len(completed))
        assert len(completed)==3, 'cleanup raced unfinished package producers'
    return original_cleanup(path,*args,**kwargs)
shutil.rmtree=remove
def record():
    pathlib.Path(tracker).write_text(json.dumps({'started':started,'completed':completed,'cleanup':cleanup,'peak':peak}))
atexit.register(record)
def build(command,**kwargs):
    global active,peak
    assert command[:3]==[sys.executable,'-B',str(pathlib.Path.cwd()/'tools/lodistribution/package.py')]
    options=dict(zip(command[3::2],command[4::2]))
    output=pathlib.Path(options['--output'])
    with lock:
        started.append(output.name)
        active+=1
        peak=max(peak,active)
    try:
        try:barrier.wait(timeout=3)
        except threading.BrokenBarrierError:
            return subprocess.CompletedProcess(command,19,b'',b'three producers did not start concurrently')
        if failure=='True' and output.name=='lo-darwin-arm64-software':
            return subprocess.CompletedProcess(command,7,b'',b'injected parallel package failure')
        time.sleep(.05)
        assert output.parent.is_dir(), 'owned work removed while producer was active'
        output.mkdir()
        for name in ('toolchain.json','inventory.json'): (output/name).write_text('{}')
        output.with_suffix('.tar').write_bytes(b'archive')
        output.with_suffix('.receipt.json').write_text('{}')
        receipt={'package':str(output.relative_to(pathlib.Path.cwd())),
                 'host':options['--host'],'variant':options['--variant']}
        return subprocess.CompletedProcess(command,0,json.dumps(receipt).encode(),b'')
    finally:
        with lock:
            completed.append(output.name)
            active-=1
subprocess.run=build
runpy.run_path(script,run_name='__main__')
''')
    result = subprocess.run(
        [sys.executable, "-B", str(harness), str(failure), str(tracker), str(SCRIPT),
         "--prepared", str(prepared), "--runtime", str(runtime), "--work", str(work)],
        cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
        capture_output=True, text=True, check=False,
    )
    if failure:
        assert result.returncode == 1
        assert "injected parallel package failure" in result.stderr
        assert not work.exists()
    else:
        assert result.returncode == 0, result.stderr
        assert list(json.loads(result.stdout)["packages"]) == [
            "lo-darwin-arm64-software", "lo-linux-arm64-software", "lo-darwin-arm64-metal",
        ]
    completion = json.loads(tracker.read_text())
    assert len(completion["started"]) == len(set(completion["started"])) == 3
    assert sorted(completion["completed"]) == sorted(completion["started"])
    assert completion["peak"] == 3
    assert completion["cleanup"] == ([3] if failure else [])
    assert prepared.read_text() == "{}"
    assert (runtime / "build-receipt.json").read_text() == "{}"
