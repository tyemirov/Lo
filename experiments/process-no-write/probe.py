"""Review-gated finite no-write, local IPC and descriptor observations."""
from __future__ import annotations

import errno
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import resource
import selectors
import signal
import socket
import struct
import subprocess
import sys
import time
from types import ModuleType

REPOSITORY = Path(__file__).absolute().parents[2]
SOURCE = REPOSITORY / "experiments/process-no-write"
ROOT = REPOSITORY / "_scratch/adapter-implementation-x921/native-no-write"
REFERENCE = REPOSITORY / "_scratch/adapter-implementation-x921/native-resources"
REFERENCE_SHA = "8c160bb31491d18b2da5daf5a4a1b633ac5ed198e880446c6ff1def0f44e7354"
LAUNCHER = REPOSITORY / "experiments/process-environment/launcher.c"
CONTROLLER = REPOSITORY / "experiments/process-stop-completion/probe.py"
CONTROLLER_SHA = "5d4a77fb34f54ee91e5c05ea865f63999b06860dfc91a7adb728276ef00df16d"
GROUPS = ("write", "namespace", "unix", "descriptors")
CASES = tuple(f"{authority}-{group}" for group in GROUPS for authority in ("control", "confined"))
LIMIT = 65536
TIMEOUT = 5.0
CLEANUP = 2.0
WRITE_FILES = ("read-baseline", "writeonly", "readwrite", "truncopen", "truncate", "mapfile")
NAMESPACE_FILES = ("read-baseline", "unlink-file", "rename-source", "link-source", "symlink-source", "hard-base", "symlink-base")
ARTIFACTS = {"harness": SOURCE / "probe.py", "test": SOURCE / "test_probe.py", "build": SOURCE / "probe.mk", "target_source": SOURCE / "target.c", "launcher_source": LAUNCHER, "controller_source": CONTROLLER, "reference_manifest": REFERENCE / "build.json", "reference_profile": REFERENCE / "candidate.sb", "reference_image": REFERENCE / "target", "profile": ROOT / "candidate.sb", "header": ROOT / "fixture_paths.h", "launcher_binary": ROOT / "launcher", "target_binary": ROOT / "target", "baselines": ROOT / "baselines.json"}


def inside(path: Path) -> Path:
    if path.absolute() != path.resolve() or not path.is_relative_to(REPOSITORY):
        raise ValueError("task path is not a direct repository path")
    return path


def digest(path: Path) -> str:
    return hashlib.sha256(inside(path).read_bytes()).hexdigest()


def plan() -> dict[str, object]:
    return {"groups": GROUPS, "invocations": CASES, "qualified_process_backend": False, "shared_mmap_after_denied_readwrite_open": "unreachable", "descriptor_canary_high": 97, "limits": {"execution_seconds": TIMEOUT, "cleanup_seconds": CLEANUP, "output_bytes_per_stream": LIMIT, "file_descriptors": 64}}


def selection() -> tuple[str, str, bytes]:
    if digest(REFERENCE / "build.json") != REFERENCE_SHA:
        raise ValueError("reviewed resource manifest differs")
    manifest = json.loads(inside(REFERENCE / "build.json").read_text())
    for key, path in (("profile", REFERENCE / "candidate.sb"), ("launcher_source", LAUNCHER), ("target_binary", REFERENCE / "target")):
        if digest(path) != manifest[key]:
            raise ValueError("reviewed resource input differs")
    if digest(CONTROLLER) != CONTROLLER_SHA:
        raise ValueError("reviewed completion controller differs")
    lines = inside(REFERENCE / "candidate.sb").read_text().splitlines(keepends=True)
    writes = [line for line in lines if line.startswith("(allow file-write* ")]
    expected_write = "(allow file-write* " + " ".join("(literal " + json.dumps(str(REFERENCE / name)) + ")" for name in ("allowed-write", "stdout", "stderr")) + ")\n"
    if writes != [expected_write]:
        raise ValueError("reference must contain the sole exact selected write rule")
    profile = "".join(line for line in lines if line != expected_write).replace(str(REFERENCE), str(ROOT))
    reads = []
    for case in CASES:
        names = WRITE_FILES if case.endswith("-write") else NAMESPACE_FILES + ("hard-alias", "symlink-alias") if case.endswith("-namespace") else ()
        reads.extend(ROOT / "cases" / case / name for name in names)
    profile += "(allow file-read* " + " ".join("(literal " + json.dumps(str(path)) + ")" for path in reads) + ")\n"
    header = "\n".join(f"#define {key} {json.dumps(value)}" for key, value in {"LO_PROFILE": profile, "LO_ROOT": str(ROOT), "LO_TARGET": str(ROOT / "target")}.items()) + "\n#define LO_FD_LIMIT 64\n"
    image = inside(REFERENCE / "target").read_bytes()
    if not image or len(image) > 64 << 20:
        raise ValueError("disposable image size is outside the selected bound")
    return profile, header, image


def snapshot(case: str) -> dict[str, object]:
    directory = inside(ROOT / "cases" / case)
    entries = {}
    for path in directory.iterdir():
        identity = path.lstat()
        base = {"device": identity.st_dev, "inode": identity.st_ino}
        if path.is_symlink():
            target = os.readlink(path)
            if target not in ("symlink-base", "symlink-source"):
                raise ValueError("unexpected disposable symlink destination")
            inside(directory / target)
            entries[path.name] = {**base, "kind": "symlink", "target": target}
        else:
            entries[path.name] = {**base, "kind": "file", "sha256": digest(path)}
    return entries


def canary_snapshot() -> dict[str, object]:
    path = inside(ROOT / "canary-file")
    information = path.stat()
    return {"device": information.st_dev, "inode": information.st_ino, "sha256": digest(path)}


def prepare() -> None:
    profile, header, image = selection()
    if inside(ROOT / "result.json").exists() or inside(ROOT / "baselines.json").exists():
        raise ValueError("preserve the existing no-write fixture state before preparation")
    inside(ROOT).mkdir(parents=True, exist_ok=True)
    inside(ROOT / "captures").mkdir(exist_ok=True)
    inside(ROOT / "candidate.sb").write_text(profile)
    inside(ROOT / "fixture_paths.h").write_text(header)
    for case in CASES:
        directory = inside(ROOT / "cases" / case)
        directory.mkdir(parents=True, exist_ok=False)
        names = WRITE_FILES if case.endswith("-write") else NAMESPACE_FILES if case.endswith("-namespace") else ()
        for name in names:
            inside(directory / name).write_bytes(image)
        if case.endswith("-namespace"):
            os.link(inside(directory / "hard-base"), directory / "hard-alias")
            os.symlink("symlink-base", directory / "symlink-alias")
    inside(ROOT / "canary-file").write_bytes(b"owned-descriptor-canary")
    baselines = {case: snapshot(case) for case in CASES}
    baselines["canary"] = canary_snapshot()
    inside(ROOT / "baselines.json").write_text(json.dumps(baselines, indent=2, sort_keys=True) + "\n")


def selected() -> None:
    profile, header, _ = selection()
    if inside(ROOT / "candidate.sb").read_text() != profile or inside(ROOT / "fixture_paths.h").read_text() != header:
        raise ValueError("selected no-write profile or header differs")


def seal() -> None:
    selected()
    inside(ROOT / "build.json").write_text(json.dumps({key: digest(path) for key, path in ARTIFACTS.items()}, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"manifest_sha256": digest(ROOT / "build.json")}))


def verify(expected: str) -> dict[str, str]:
    if digest(ROOT / "build.json") != expected:
        raise ValueError("reviewed no-write manifest differs")
    selected()
    actual = {key: digest(path) for key, path in ARTIFACTS.items()}
    if json.loads(inside(ROOT / "build.json").read_text()) != actual:
        raise ValueError("reviewed no-write artifacts differ")
    return actual


def controller_module() -> ModuleType:
    code = inside(CONTROLLER).read_bytes()
    if hashlib.sha256(code).hexdigest() != CONTROLLER_SHA:
        raise ValueError("reviewed completion controller differs")
    specification = importlib.util.spec_from_file_location("lo_no_write_completion", CONTROLLER)
    if specification is None:
        raise ValueError("completion module specification is unavailable")
    module = importlib.util.module_from_spec(specification)
    sys.modules[specification.name] = module
    exec(compile(code, str(CONTROLLER), "exec"), module.__dict__)
    return module


def child_limits() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (LIMIT, LIMIT))
    resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))
    resource.setrlimit(resource.RLIMIT_CPU, (1, 2))


def environment_frame(environment: dict[str, str]) -> bytes:
    entries = [(key + "=" + value).encode() for key, value in environment.items()]
    body = b"".join(struct.pack(">I", len(entry)) + entry for entry in entries)
    if len(body) > 4096 or len(entries) > 16 or any(len(entry) > 1024 for entry in entries):
        raise ValueError("fixed environment frame exceeds its contract")
    return b"LOE1" + struct.pack(">II", len(body), len(entries)) + body


def run_case(case: str, module: ModuleType) -> dict[str, object]:
    report: dict[str, object] = {"case": case, "unconfined_control": case.startswith("control-"), "errors": [], "timeout": False, "output_limit": False, "accepted_connections": 0}
    selector = selectors.DefaultSelector()
    process = controller = listener = None
    listener_bound = False
    reader = writer = None
    raw_fds = []
    sockets = []
    streams = {}
    buffers = {name: bytearray() for name in ("stdout", "stderr")}
    counts = {name: 0 for name in buffers}
    eof = {name: False for name in buffers}
    socket_path = ROOT / "cases" / case / "socket"
    environment = {"LO_NO_WRITE_CASE": case}
    start = time.monotonic()
    execution_deadline = start + TIMEOUT

    def record(operation: str, error: OSError) -> None:
        report["errors"].append({"operation": operation, "errno": error.errno})

    def close_fd(descriptor: int) -> None:
        try:
            os.close(descriptor)
        except OSError as error:
            record("close-descriptor", error)

    def accept_ready() -> None:
        try:
            connection, _ = listener.accept()
        except BlockingIOError:
            return
        try:
            report["accepted_connections"] += 1
        finally:
            try:
                connection.close()
            except OSError as error:
                record("close-accepted-socket", error)

    def stop(reason: str) -> None:
        if controller is not None:
            controller.request_stop(reason)

    try:
        if case.endswith("-unix"):
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            relative = socket_path.relative_to(REPOSITORY).as_posix()
            if len(relative.encode()) >= 104 or socket_path.exists():
                raise ValueError("owned Unix listener path is unavailable")
            listener.bind(relative)
            listener_bound = True
            listener.listen(1)
            listener.setblocking(False)
            selector.register(listener, selectors.EVENT_READ, "listener")
        if case.endswith("-descriptors"):
            try:
                fcntl.fcntl(97, fcntl.F_GETFD)
            except OSError as error:
                if error.errno != errno.EBADF:
                    raise
            else:
                raise ValueError("descriptor 97 is already occupied; it must not be overwritten")
            canary = os.open(inside(ROOT / "canary-file"), os.O_RDONLY)
            raw_fds.append(canary)
            pipe_read, pipe_write = os.pipe()
            raw_fds.extend((pipe_read, pipe_write))
            pair = socket.socketpair()
            sockets.extend(pair)
            canaries = raw_fds + [entry.fileno() for entry in sockets]
            # Allocation must not have occupied the reserved high descriptor.
            try:
                fcntl.fcntl(97, fcntl.F_GETFD)
            except OSError as error:
                if error.errno != errno.EBADF:
                    raise
            else:
                raise ValueError("descriptor allocation occupied 97; it must not be overwritten")
            os.dup2(canary, 97, inheritable=True)
            raw_fds.append(97)
            canaries.append(97)
            for descriptor in canaries:
                os.set_inheritable(descriptor, True)
            environment["LO_CANARY_FDS"] = ",".join(str(descriptor) for descriptor in canaries)
            report["canary_descriptors"] = canaries
        if case.startswith("control-"):
            argv = [str(ROOT / "target"), "values", "fixed-argument"]
            child_environment, inherited = environment, ()
        else:
            reader, writer = os.pipe()
            if not 3 <= reader < 64:
                raise ValueError("control reader exceeds inherited descriptor range")
            argv = [str(ROOT / "launcher"), str(reader), "values"]
            child_environment, inherited = {"CI": "1", "TMPDIR": str(ROOT)}, (reader,)
        process = subprocess.Popen(argv, cwd=inside(ROOT), env=child_environment, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True, pass_fds=inherited, start_new_session=True, preexec_fn=child_limits)
        controller = module.CompletionController(process, os.killpg, cleanup_timeout=CLEANUP)
        streams = {"stdout": process.stdout, "stderr": process.stderr}
        if reader is not None:
            close_fd(reader)
            reader = None
        for name, stream in streams.items():
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, name)
        payload, sent = environment_frame(environment), 0
        if writer is not None:
            os.set_blocking(writer, False)
            selector.register(writer, selectors.EVENT_WRITE, "control")
        while any(key.data != "listener" for key in selector.get_map().values()) or controller.poll() is None:
            now = time.monotonic()
            if controller.deadline is None and now >= execution_deadline:
                report["timeout"] = True
                stop("timeout")
            if controller.expired():
                report["errors"].append({"operation": "drain", "timeout": True})
                break
            end = controller.deadline if controller.deadline is not None else execution_deadline
            for key, _ in selector.select(min(0.05, max(0, end - now))):
                if key.data == "listener":
                    accept_ready()
                    continue
                if key.data == "control":
                    try:
                        amount = os.write(writer, payload[sent:sent + 7])
                        if amount <= 0:
                            raise OSError(errno.EIO, "control writer made no progress")
                        sent += amount
                        if sent == len(payload):
                            selector.unregister(writer)
                            close_fd(writer)
                            writer = None
                    except (BlockingIOError, InterruptedError):
                        continue
                    continue
                name = key.data
                allowance = LIMIT - len(buffers[name])
                try:
                    data = os.read(key.fd, 4096 if report["output_limit"] else min(4096, allowance + 1))
                except (BlockingIOError, InterruptedError):
                    continue
                if not data:
                    eof[name] = True
                    selector.unregister(key.fileobj)
                    try:
                        key.fileobj.close()
                    except OSError as error:
                        record("close-" + name, error)
                else:
                    counts[name] += len(data)
                    buffers[name].extend(data[:allowance])
                    if counts[name] > LIMIT and not report["output_limit"]:
                        report["output_limit"] = True
                        stop("output_limit")
        if listener is not None:
            accept_ready()
    except Exception as error:
        report["failure"] = str(error)[:256]
        stop("failure")
    finally:
        if controller is not None:
            controller.begin_cleanup()
            if controller.poll() is None:
                stop("cleanup")
        for descriptor in (reader, writer, *raw_fds):
            if descriptor is not None:
                close_fd(descriptor)
        for owned in sockets + ([listener] if listener is not None else []):
            try:
                owned.close()
            except OSError as error:
                record("close-socket", error)
        if listener_bound:
            try:
                inside(socket_path).unlink()
            except OSError as error:
                record("unlink-owned-listener", error)
        try:
            selector.close()
        except OSError as error:
            record("close-selector", error)
        for name, stream in streams.items():
            try:
                stream.close()
            except OSError as error:
                record("close-" + name, error)
        ownership = {name: {"eof": eof[name], "closed": name in streams and streams[name].closed} for name in buffers}
        report["controller"] = controller.finish(ownership, descendants_complete=True) if controller is not None else {"cleanup_complete": not report["errors"], "stop_failed": True}
        report["exit_code"] = process.returncode if process is not None else None
    report["cleanup_complete"] = bool(report["controller"]["cleanup_complete"] and not report["errors"])
    report["elapsed_seconds"] = time.monotonic() - start
    report["streams"] = {}
    for name, data in buffers.items():
        with inside(ROOT / "captures" / f"{case}.{name}").open("xb") as output:
            output.write(data)
        report["streams"][name] = {"observed_bytes": counts[name], "captured_bytes": len(data), **ownership[name], "sha256": hashlib.sha256(data).hexdigest(), "preview": bytes(data[:512]).decode("utf-8", errors="replace")}
    try:
        report["observations"] = json.loads(buffers["stdout"])
    except (ValueError, UnicodeDecodeError):
        report["observations"] = None
    report["healthy"] = bool(report["cleanup_complete"] and not report["controller"]["stop_failed"] and report["exit_code"] == 0 and not report["timeout"] and not report["output_limit"] and "failure" not in report)
    return report


def expected_snapshot(case: str, baseline: dict[str, object], image: bytes, actual: dict[str, object]) -> dict[str, object]:
    expected = json.loads(json.dumps(baseline))
    if case == "control-write":
        for name in ("writeonly", "readwrite", "mapfile"):
            expected[name]["sha256"] = hashlib.sha256(b"X" + image[1:]).hexdigest()
        for name in ("truncopen", "truncate"):
            expected[name]["sha256"] = hashlib.sha256(b"").hexdigest()
    elif case == "control-namespace":
        del expected["unlink-file"]
        expected["rename-destination"] = expected.pop("rename-source")
        expected["link-destination"] = expected["link-source"].copy()
        for name in ("hard-base", "hard-alias", "symlink-base"):
            expected[name]["sha256"] = hashlib.sha256(b"X" + image[1:]).hexdigest()
        created = actual.get("symlink-destination", {})
        if created.get("kind") != "symlink" or created.get("target") != "symlink-source":
            raise ValueError("control symlink creation differs")
        expected["symlink-destination"] = created
    return expected


def case_passes(result: dict[str, object]) -> bool:
    observation = result["observations"]
    if not result["healthy"] or not isinstance(observation, dict) or observation.get("cleanup_ok") is not True:
        return False
    case = result["case"]
    control = case.startswith("control-")
    operations = observation.get("operations", {})
    group = case.split("-", 1)[1]
    names = {"write": {"writeonly", "readwrite", "truncopen", "mapfile", "shared-mmap", "truncate"}, "namespace": {"mkstemp", "unlink", "rename", "link", "symlink", "hard-alias", "symlink-alias"}, "unix": {"unix-connect"}}
    if group == "descriptors":
        expected_names = {f"fd-{number}" for number in result["canary_descriptors"]} | {"standard-fds"}
        return set(operations) == expected_names and operations["standard-fds"] == {"result": 3, "errno": 0, "read_exact": False, "unreachable": False} and all(value == {"result": -1, "errno": errno.EBADF, "read_exact": False, "unreachable": False} for name, value in operations.items() if name != "standard-fds") and 97 in result["canary_descriptors"]
    if set(operations) != names[group]:
        return False
    for name, value in operations.items():
        read_required = group == "write" or name in ("hard-alias", "symlink-alias")
        if value["read_exact"] != read_required:
            return False
        if name == "shared-mmap" and not control:
            if value != {"result": -1, "errno": 0, "read_exact": True, "unreachable": True}:
                return False
        elif value != {"result": 0 if control else -1, "errno": 0 if control else errno.EPERM, "read_exact": read_required, "unreachable": False}:
            return False
    return group != "unix" or result["accepted_connections"] == (1 if control else 0)


def run(expected: str) -> int:
    reviewed = verify(expected)
    if inside(ROOT / "result.json").exists():
        raise ValueError("preserve prior no-write results")
    baseline = json.loads(inside(ROOT / "baselines.json").read_text())
    if canary_snapshot() != baseline["canary"]:
        raise ValueError("canary baseline differs")
    for case in CASES:
        if snapshot(case) != baseline[case]:
            raise ValueError("disposable fixture baseline differs")
        for stream in ("stdout", "stderr"):
            if inside(ROOT / "captures" / f"{case}.{stream}").exists():
                raise ValueError("preserve prior no-write captures")
    _, _, image = selection()
    module = controller_module()
    expected_state = {case: baseline[case] for case in CASES}
    report = {"scope": "Eight fixed fixture invocations only: disposable write/namespace controls and denials, repository-owned AF_UNIX connect, and inherited descriptor exclusion. No native Process qualification.", "descendant_scope": "Reviewed fixed target source contains no process creation; this is not a general OS descendant query.", "startup_limit": "Synchronous Popen includes an absolute deadline but cannot be independently interrupted.", "qualified_process_backend": False, "reviewed_manifest_sha256": expected, "reference_manifest_sha256": REFERENCE_SHA, "plan": plan(), "cpu_limit_note": "Historical RLIMIT_CPU(1,2) retained; not a qualified CPU quota.", "cases": []}
    for case in CASES:
        result = run_case(case, module)
        try:
            actual = snapshot(case)
            result["fixture_observations"] = actual
            expected_state[case] = expected_snapshot(case, baseline[case], image, actual)
            result["fixtures_expected"] = all(snapshot(selected_case) == expected_state[selected_case] for selected_case in CASES)
            result["artifacts_unchanged"] = verify(expected) == reviewed
            result["canary_unchanged"] = canary_snapshot() == baseline["canary"] and inside(ROOT / "canary-file").read_bytes() == b"owned-descriptor-canary"
            result["passed"] = case_passes(result) and result["fixtures_expected"] and result["artifacts_unchanged"] and result["canary_unchanged"]
        except (OSError, ValueError) as error:
            result["integrity_error"] = str(error)[:256]
            result["passed"] = False
        report["cases"].append(result)
        if not result["passed"]:
            break
    try:
        report["post_run_artifacts"] = verify(expected)
    except (OSError, ValueError) as error:
        report["integrity_error"] = str(error)[:256]
    passed = len(report["cases"]) == len(CASES) and all(result["passed"] for result in report["cases"]) and report.get("post_run_artifacts") == reviewed
    report["fixture_cases_passed"] = bool(passed)
    with inside(ROOT / "result.json").open("x") as output:
        output.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if passed else 1


def main() -> int:
    arguments = sys.argv[1:]
    if arguments and arguments[0] in ("verify", "run") and (len(arguments) != 2 or re.fullmatch(r"[0-9a-f]{64}", arguments[1]) is None):
        raise ValueError("reviewed manifest SHA256 is required")
    if Path.cwd().resolve() != inside(REPOSITORY):
        raise ValueError("run from the Lo repository")
    if arguments == ["plan"]:
        print(json.dumps(plan()))
    elif arguments == ["prepare"]:
        prepare()
    elif arguments == ["seal"]:
        seal()
    elif len(arguments) == 2 and arguments[0] == "verify":
        verify(arguments[1])
        print(json.dumps({"reviewed_build_verified": True}))
    elif len(arguments) == 2 and arguments[0] == "run":
        return run(arguments[1])
    else:
        raise ValueError("expected plan, prepare, seal, verify SHA256, or run SHA256")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(2)
