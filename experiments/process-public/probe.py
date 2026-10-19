"""Build/seal canonical Process CLI acceptance; run only after exact review."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import select
import selectors
import signal
import subprocess
import sys
import time


REPOSITORY = Path(__file__).resolve().parents[2]
SOURCE = REPOSITORY / "experiments/process-public"
BASE = REPOSITORY / "_scratch/adapter-implementation-x921"
CASES = ["normal", "nonzero", "signaled", "pipeline-zero", "pipeline-nonzero", "denied-command", "denied-cwd", "helper-identity", "parent-read", "parent-write", "wall-timeout", "server-callback"]
CAPTURE_LIMIT = 65536


@dataclass
class Cancellation:
    requested: bool = False


@dataclass
class Ownership:
    control: int
    joined: bool = False


def inside(path: Path) -> Path:
    path = path.absolute()
    path.relative_to(REPOSITORY)
    if path.resolve() != path:
        raise ValueError("public task path is not canonical inside Lo")
    return path


def task(phase: str) -> Path:
    if phase not in {"gate-red", "qualified"}:
        raise ValueError("public phase must be gate-red or qualified")
    return inside(BASE / ("public-native-" + phase))


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def save(path: Path, content: str) -> None:
    with inside(path).open("x", encoding="utf-8") as output:
        output.write(content)


def plan() -> dict:
    return {"backends": ["interpreter", "native"], "cases": CASES, "platform": "darwin27/arm64", "entrypoints": "canonical CLI and generated-native run/serve", "standard_process_flags": ["--process-allow", "--process-launcher", "--process-read-root"], "gate_red": {"backend": "interpreter", "case": "normal", "expected_failure": "runtime.process-unavailable", "attempts": 1}, "qualified_invocations": 24, "native_run_requires": ["exact_source_and_binary_manifest", "independent_reviewed_sha256", "one_use_token"], "automatic_retry": False, "wall_timeout_code": "runtime.cancelled", "signaled_fixture": "self-SIGKILL only", "trap_fixture": False, "file_authority_inherited_from_process": False}


def selected_platform() -> None:
    if sys.platform != "darwin" or platform.machine() != "arm64" or platform.release().split(".")[0] != "27":
        raise ValueError("public acceptance requires the selected Darwin27 arm64 backend")


def prepare(phase: str) -> None:
    selected_platform()
    root = task(phase)
    if root.exists():
        raise ValueError("public snapshot exists; no replacement or retry")
    root.mkdir()
    for directory in ("fixture", "outputs", "programs", "ungranted-cwd"):
        (root / directory).mkdir()
    for backend in ("interpreter", "native"):
        (root / "programs" / backend).mkdir()
    fixture = root / "fixture"
    identity = fixture.stat()
    header = "".join(f"#define {key} {json.dumps(value)}\n" for key, value in {"LO_TARGET": str(fixture / "target")}.items())
    header += f"#define LO_ROOT_DEVICE {identity.st_dev}ULL\n#define LO_ROOT_INODE {identity.st_ino}ULL\n"
    save(fixture / "fixture_paths.h", header)
    save(fixture / "invalid-helper", "public invalid helper fixture; never execute\n")


def snapshot_helper(phase: str) -> None:
    root = task(phase)
    subprocess.run([sys.executable, "-B", str(REPOSITORY / "tools/processbuild/identity.py"), "verify", str(REPOSITORY / "bin/lo-process-launcher"), str(REPOSITORY / "internal/processhost/helper_identity.go")], cwd=REPOSITORY, stdin=subprocess.DEVNULL, check=True)
    os.link(inside(REPOSITORY / "bin/lo-process-launcher"), inside(root / "fixture/helper"))
    save(root / "helper_identity.go", (REPOSITORY / "internal/processhost/helper_identity.go").read_text())
    helper_identity(phase)


def helper_identity(phase: str) -> None:
    root = task(phase)
    if (root / "helper_identity.go").read_bytes() != (REPOSITORY / "internal/processhost/helper_identity.go").read_bytes():
        raise ValueError("snapshot helper differs from product pin; no product pin update authorized")
    subprocess.run([sys.executable, "-B", str(REPOSITORY / "tools/processbuild/identity.py"), "verify", str(root / "fixture/helper"), str(root / "helper_identity.go")], cwd=REPOSITORY, stdin=subprocess.DEVNULL, check=True)


def command(root: Path, mode: str, executable: Path | None = None, directory: Path | None = None) -> str:
    executable = root / "fixture/target" if executable is None else executable
    directory = root / "fixture" if directory is None else directory
    return f'executable = Process.Executable.new(File.Path.new({json.dumps(str(executable))})); action = Process.Command.new(executable,List.new({json.dumps(mode)}),Map.new(Record.new(key:"NAME",value:"explicit")),File.Path.new({json.dumps(str(directory))}),UTF8.Bytes.new("stdin")); result = commit action; '


def program(root: Path, backend: str, name: str) -> str:
    recover = 'flow Recover(error: Error) -> String { return "unexpected-recovery" }; '
    if name in {"normal", "nonzero", "signaled", "helper-identity"}:
        mode = name if name in {"nonzero", "signaled"} else "normal"
        return f'flow Main() -> Process.Result! {{ {command(root, mode)} return result }}; return Main()\n'
    if name in {"pipeline-zero", "pipeline-nonzero"}:
        path = root / "outputs" / (backend + "-" + name + ".bin")
        prefix = 'flow Empty() -> List<Process.Result> { return List.new() }; '
        prefix += f'flow Save(total: Int, result: Process.Result) -> Int! {{ write = File.Write.new(File.Path.new({json.dumps(str(path))}),result.stdout); receipt = commit write; return Math.add(total,receipt.bytes_written) }}; '
        return prefix + 'flow Main() -> Int! { ' + command(root, "normal" if name == "pipeline-zero" else "nonzero") + 'code = Process.Termination.exit_code(result.termination); selected = Match.new(code,0:List.new(result),_:Empty()); return Flow.fold(selected,0,Save) }; return Main()\n'
    if name == "parent-read":
        return recover + f'flow Main() -> String onError Recover {{ bytes = File.read(File.Path.new({json.dumps(str(root / "fixture/target"))})); return UTF8.String.new(bytes) }}; return Main()\n'
    if name == "parent-write":
        path = root / "outputs" / (backend + "-denied-write.bin")
        return recover + f'flow Main() -> String onError Recover {{ write = File.Write.new(File.Path.new({json.dumps(str(path))}),UTF8.Bytes.new("not-written")); receipt = commit write; return Int.to_string(receipt.bytes_written) }}; return Main()\n'
    if name in {"denied-command", "denied-cwd", "wall-timeout"}:
        invocation = command(root, "wait" if name == "wall-timeout" else "normal", root / "fixture/ungranted-target" if name == "denied-command" else None, root / "ungranted-cwd" if name == "denied-cwd" else None)
        return recover + 'flow Main() -> String onError Recover { ' + invocation + 'code = Process.Termination.exit_code(result.termination); return Int.to_string(code) }; return Main()\n'
    if name == "server-callback":
        return 'flow Handle(request: HTTP.Request) -> HTTP.Response! { ' + command(root, "normal") + 'return HTTP.Response.Bytes.new(200,result.stdout) }; flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500,"unexpected-recovery") }; flow Main() -> HTTP.ServerReceipt! { server = HTTP.Server.new(0,Handle,OnError); receipt = commit server; return receipt }; return Main()\n'
    raise ValueError("unknown finite public case")


def programs(phase: str) -> dict:
    root = task(phase)
    return {backend: {name: {"source": str(root / "programs" / backend / (name + ".lo")), "artifact": str(root / "programs" / backend / (name + ".native"))} for name in CASES} for backend in ("interpreter", "native")}


def sources(phase: str) -> None:
    root = task(phase)
    os.link(root / "fixture/target", root / "fixture/ungranted-target")
    for backend, selected in programs(phase).items():
        for name, paths in selected.items():
            source = Path(paths["source"])
            save(source, program(root, backend, name))
            for arguments in ([str(root / "lo"), "check", str(source), "--format", "json"], [str(root / "lo"), "build", str(source), "--output", paths["artifact"], "--format", "json"]):
                completed = subprocess.run(arguments, cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True, check=False)
                if completed.returncode != 0:
                    save(root / ("prepare-failure-" + backend + "-" + name + ".txt"), completed.stdout.decode(errors="replace") + completed.stderr.decode(errors="replace"))
                    raise ValueError("public fixture check/build failed; no execution attempted")


def source_inputs() -> list[Path]:
    selected = sorted(path for path in SOURCE.rglob("*") if path.is_file() and path.suffix in {".go", ".py", ".mk", ".c"})
    selected += sorted(path for path in (REPOSITORY / "internal/processhost").rglob("*") if path.is_file() and path.suffix in {".go", ".c", ".h"})
    selected += [REPOSITORY / "internal/runner/execute.go", REPOSITORY / "internal/runner/command.go", REPOSITORY / "tools/processbuild/identity.py", REPOSITORY / "go.mod", REPOSITORY / "go.sum"]
    return selected


def artifacts(phase: str) -> list[Path]:
    root = task(phase)
    selected = sorted(path for path in (root / "source-snapshot").rglob("*") if path.is_file())
    selected += [root / "helper_identity.go", root / "lo", root / "public-tests", root / "fixture/helper", root / "fixture/target", root / "fixture/ungranted-target", root / "fixture/fixture_paths.h", root / "fixture/invalid-helper"]
    for backend in programs(phase).values():
        for paths in backend.values():
            selected += [Path(paths["source"]), Path(paths["artifact"])]
    return selected


def seal(phase: str) -> None:
    root = task(phase)
    helper_identity(phase)
    # Phase snapshots preserve evidence after the product gate source changes.
    snapshot_root = root / "source-snapshot"
    snapshot_root.mkdir()
    for source in source_inputs():
        destination = snapshot_root / source.relative_to(REPOSITORY)
        destination.parent.mkdir(parents=True, exist_ok=True)
        with destination.open("xb") as output:
            output.write(source.read_bytes())
    snapshot = {str(path): digest(path) for path in artifacts(phase)}
    manifest = {"phase": phase, "cli": str(root / "lo"), "helper": str(root / "fixture/helper"), "target": str(root / "fixture/target"), "bad_helper": str(root / "fixture/invalid-helper"), "read_root": str(root / "fixture"), "write_root": str(root / "outputs"), "result": str(root / "result.json"), "token": str(root / "run-token"), "programs": programs(phase), "artifacts": snapshot}
    manifest["sources_root"] = str(snapshot_root)
    save(root / "manifest.json", json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(digest(root / "manifest.json"))


def verified(phase: str, reviewed: str) -> dict:
    root = task(phase)
    if digest(root / "manifest.json") != reviewed:
        raise ValueError("public manifest differs from reviewed digest")
    manifest = json.loads((root / "manifest.json").read_text())
    if manifest["phase"] != phase or manifest["sources_root"] != str(root / "source-snapshot") or manifest["programs"] != programs(phase) or set(manifest["artifacts"]) != {str(path) for path in artifacts(phase)}:
        raise ValueError("public finite matrix or source selection differs")
    for path, expected in manifest["artifacts"].items():
        if digest(Path(path)) != expected:
            raise ValueError("public source/binary differs from seal")
    return manifest


def retain(root: Path, reason: str) -> None:
    try:
        save(root / "ownership-retained.json", json.dumps({"reason": reason, "ownership_retained": True, "retry_allowed": False}) + "\n")
    except BaseException:
        pass
    try:
        print(reason + "; retaining ownership, no further run", file=sys.stderr, flush=True)
    except BaseException:
        pass
    while True:
        try:
            time.sleep(1)
        except BaseException:
            pass


def limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def owned(process: subprocess.Popen, read_control: int, owner: Ownership, cancellation: Cancellation, phase: str) -> dict:
    root = task(phase)
    os.close(read_control)
    watcher = select.kqueue()
    watcher.control([select.kevent(process.pid, filter=select.KQ_FILTER_PROC, flags=select.KQ_EV_ADD | select.KQ_EV_ENABLE | select.KQ_EV_ONESHOT, fflags=select.KQ_NOTE_EXIT)], 0, 0)
    selector = selectors.DefaultSelector()
    captured = {"stdout": bytearray(), "stderr": bytearray()}
    for name, stream in (("stdout", process.stdout), ("stderr", process.stderr)):
        if stream is None:
            raise ValueError("public controller stream absent")
        os.set_blocking(stream.fileno(), False)
        selector.register(stream, selectors.EVENT_READ, name)
    if cancellation.requested:
        os.close(owner.control)
        owner.control = -1
    elif os.write(owner.control, b"A") != 1:
        raise ValueError("public admission byte not published")
    started = time.monotonic()
    exited = False
    exited_at = None
    cancel_at = None
    emergency = False
    saturated = False
    cleanup = False
    checked = False
    while selector.get_map() or not exited:
        for event in watcher.control(None, 1, 0):
            if event.ident == process.pid and event.fflags & select.KQ_NOTE_EXIT:
                exited = True
                exited_at = time.monotonic()
        now = time.monotonic()
        if cancel_at is None and (cancellation.requested or saturated or now - started > (15 if phase == "gate-red" else 90)):
            cancel_at = now
            if owner.control >= 0:
                try:
                    os.write(owner.control, b"C")
                except BrokenPipeError:
                    pass
                os.close(owner.control)
                owner.control = -1
        if exited and not checked:
            checked = True
            try:
                encoded = (root / "result.json").read_bytes()
                cleanup = len(encoded) <= CAPTURE_LIMIT and json.loads(encoded).get("cleanup_complete") is True
            except (OSError, ValueError):
                cleanup = False
        needs_emergency = (cancel_at is not None and now - cancel_at > 8 and not exited) or (checked and not cleanup) or (exited_at is not None and now - exited_at > 2 and bool(selector.get_map()))
        if needs_emergency and not emergency:
            emergency = True
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except OSError:
                retain(root, "public emergency group signal failed")
        for key, _ in selector.select(0.01):
            data = os.read(key.fileobj.fileno(), 8192)
            if not data:
                selector.unregister(key.fileobj)
                key.fileobj.close()
                continue
            destination = captured[key.data]
            remaining = CAPTURE_LIMIT - len(destination)
            destination.extend(data[:remaining])
            saturated = saturated or len(data) > remaining
    if owner.control >= 0:
        os.close(owner.control)
        owner.control = -1
    code = process.wait()
    owner.joined = True
    watcher.close()
    selector.close()
    for name, content in captured.items():
        save(root / (name + ".txt"), bytes(content).decode(errors="replace"))
    return {"exit_code": code, "elapsed_seconds": time.monotonic() - started, "controller_joined": True, "explicit_cleanup_complete": cleanup, "emergency_group_signal": emergency, "cancel_requested": cancel_at is not None, "output_saturated": saturated}


def run(phase: str, reviewed: str) -> None:
    cancellation = Cancellation()
    def cancel(_number: int, _frame: object) -> None:
        cancellation.requested = True
    signal.signal(signal.SIGINT, cancel)
    signal.signal(signal.SIGTERM, cancel)
    selected_platform()
    manifest = verified(phase, reviewed)
    root = task(phase)
    if (root / "run-token").exists() or (root / "run-token.consumed").exists() or (root / "result.json").exists():
        raise ValueError("public snapshot already authorized or attempted; no retry")
    save(root / "run-token", reviewed + "\n")
    read_control, write_control = os.pipe()
    environment = dict(os.environ)
    environment.update({"CI": "1", "LO_PROCESS_PUBLIC_MANIFEST": str(root / "manifest.json"), "LO_PROCESS_PUBLIC_REVIEWED_SHA256": reviewed, "LO_PROCESS_PUBLIC_CONTROL_FD": str(read_control)})
    try:
        if cancellation.requested:
            raise ValueError("public controller cancelled before creation")
        process = subprocess.Popen([str(root / "public-tests"), "-test.run=^TestProcessPublicReviewedLane$", "-test.v", "-test.count=1", "-test.timeout=0"], cwd=SOURCE, env=environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, pass_fds=(read_control,), start_new_session=True, preexec_fn=limits)
    except BaseException:
        os.close(read_control)
        os.close(write_control)
        raise
    owner = Ownership(write_control)
    try:
        controller = owned(process, read_control, owner, cancellation, phase)
    except BaseException:
        if not owner.joined:
            if owner.control >= 0:
                try:
                    os.write(owner.control, b"C")
                except OSError:
                    pass
            retain(root, "unexpected public controller failure; cleanup unresolved")
        raise
    controller["reviewed_manifest_sha256"] = reviewed
    controller["artifacts_unchanged"] = verified(phase, reviewed) == manifest
    save(root / "controller.json", json.dumps(controller, indent=2, sort_keys=True) + "\n")
    if controller["exit_code"] != 0 or not controller["explicit_cleanup_complete"] or controller["emergency_group_signal"] or controller["cancel_requested"] or controller["output_saturated"]:
        raise ValueError("public acceptance failed; inspect immutable snapshot, no automatic retry")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("plan", "prepare", "snapshot-helper", "helper-identity", "sources", "seal", "verify", "run"))
    parser.add_argument("phase", nargs="?", default="")
    parser.add_argument("reviewed", nargs="?", default="")
    arguments = parser.parse_args()
    if arguments.command in {"verify", "run"} and (len(arguments.reviewed) != 64 or any(character not in "0123456789abcdef" for character in arguments.reviewed)):
        print(json.dumps({"error": "reviewed manifest SHA256 is required"}), file=sys.stderr)
        return 2
    if arguments.command == "plan":
        print(json.dumps(plan()))
    else:
        task(arguments.phase)
        {"prepare": prepare, "snapshot-helper": snapshot_helper, "helper-identity": helper_identity, "sources": sources, "seal": seal, "verify": verified, "run": run}[arguments.command](*([arguments.phase, arguments.reviewed] if arguments.command in {"verify", "run"} else [arguments.phase]))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        raise SystemExit(1) from error
