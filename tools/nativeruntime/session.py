"""Execute native Linux cases through a bounded persistent software-guest pool."""
from __future__ import annotations

import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import queue
import shutil
import signal
import socket
import struct
import subprocess
import sys
import threading

from prepare import ROOT, local

REQUEST_MAGIC = 0x4C4F4731
RESPONSE_MAGIC = 0x4C4F5231
STREAM_LIMIT = 64 << 20
ARGUMENT_LIMIT = 1 << 20
READY = b"LOG_READY\n"
HEADER = struct.Struct("!8I")
ACCELERATORS = ("tcg", "hvf")
DEFAULT_ACCELERATOR = os.environ.get("LO_TEST_GUEST_ACCELERATOR") or (
    "hvf" if sys.platform == "darwin" and platform.machine() == "arm64" else "tcg"
)
MAX_WORKERS = 4


def receive(stream: socket.socket, length: int) -> bytes:
    parts = bytearray()
    while len(parts) < length:
        data = stream.recv(min(65536, length - len(parts)))
        if not data:
            raise ValueError("guest transport ended before the complete result")
        parts.extend(data)
    return bytes(parts)


def exchange(stream: socket.socket, identifier: int, executable: bytes, companion: bytes,
             arguments: bytes, stdin: bytes, timeout_ms: int, capture: bool) -> dict[str, object]:
    try:
        return exchange_frame(stream, identifier, executable, companion, arguments, stdin, timeout_ms, capture)
    except OSError as error:
        raise ValueError(f"guest transport failed: {error}") from error


def exchange_frame(stream: socket.socket, identifier: int, executable: bytes, companion: bytes,
                   arguments: bytes, stdin: bytes, timeout_ms: int, capture: bool) -> dict[str, object]:
    stream.sendall(HEADER.pack(REQUEST_MAGIC, identifier, timeout_ms, len(executable),
                              len(companion), len(arguments), len(stdin), int(capture)))
    for data in (executable, companion, arguments, stdin):
        if data:
            stream.sendall(data)
    magic, returned, status, stdout_size, stderr_size, high, low, flags = HEADER.unpack(receive(stream, HEADER.size))
    if magic != RESPONSE_MAGIC or returned != identifier or flags or status > 255:
        raise ValueError("guest result identity or status is invalid")
    if stdout_size > STREAM_LIMIT or stderr_size > STREAM_LIMIT:
        raise ValueError("guest result stream exceeds the declared limit")
    return {"status": status, "stdout": base64.b64encode(receive(stream, stdout_size)).decode(),
            "stderr": base64.b64encode(receive(stream, stderr_size)).decode(), "elapsed_ns": (high << 32) | low}


@dataclass(frozen=True)
class Request:
    executable: Path
    companion: Path | None
    arguments: bytes
    stdin: bytes
    timeout_ms: int
    capture: bool

    @classmethod
    def parse(cls, record: object) -> Request:
        if not isinstance(record, dict) or set(record) - {"executable", "companion", "arguments", "stdin", "capture_file", "timeout_ms", "environment", "cwd"}:
            raise ValueError("guest request has unknown fields or an invalid shape")
        executable = executable_input(record.get("executable"))
        supplied_companion = record.get("companion", "")
        companion = executable_input(supplied_companion) if supplied_companion else None
        arguments = record.get("arguments", [])
        if not isinstance(arguments, list) or any(not isinstance(arg, str) or "\0" in arg for arg in arguments):
            raise ValueError("guest arguments must be strings without NUL bytes")
        argv = [str(executable), *(["/companion"] if companion else []), *arguments]
        environment = record.get("environment", {"CI": "1", "PATH": "", "HOME": "/", "TMPDIR": "/tmp",
                                                 "TMP": "/tmp", "TEMP": "/tmp", "LC_ALL": "C", "LANG": "C"})
        if not isinstance(environment, dict) or any(
                not isinstance(key, str) or not key or "=" in key or "\0" in key
                or not isinstance(value, str) or "\0" in value for key, value in environment.items()):
            raise ValueError("guest environment requires string names and values without NUL bytes")
        if len(environment) >= 4095:
            raise ValueError("guest environment exceeds the declared entry limit")
        cwd = record.get("cwd", "/")
        if not isinstance(cwd, str) or "\0" in cwd or not cwd.startswith("/") or ".." in PurePosixPath(cwd).parts or len(os.fsencode(cwd)) >= 4096:
            raise ValueError("guest working directory must be an absolute canonical path")
        variables = [f"{key}={value}" for key, value in sorted(environment.items())]
        encoded = struct.pack("!II", len(argv), len(variables)) + b"\0".join(
            os.fsencode(arg) for arg in [*argv, *variables, cwd]) + b"\0"
        if len(encoded) >= ARGUMENT_LIMIT or len(argv) >= 4095:
            raise ValueError("guest arguments exceed the declared limit")
        try:
            stdin = base64.b64decode(record.get("stdin", ""), validate=True)
        except (TypeError, ValueError) as error:
            raise ValueError("guest stdin must be canonical base64") from error
        if len(stdin) > STREAM_LIMIT:
            raise ValueError("guest stdin exceeds the declared limit")
        timeout = record.get("timeout_ms", 120000)
        if type(timeout) is not int or not 1 <= timeout <= 120000:
            raise ValueError("guest timeout must be between 1 and 120000 milliseconds")
        supplied_capture = record.get("capture_file", "")
        if not isinstance(supplied_capture, str):
            raise ValueError("guest capture path must be a string")
        if supplied_capture:
            capture = local(Path(supplied_capture))
            if capture.name != ".lo-native-capture" or not capture.is_file() or capture.read_bytes():
                raise ValueError("capture file must be the empty opted-in scenario file")
        return cls(executable, companion, encoded, stdin, timeout, bool(supplied_capture))


def executable_input(supplied: object) -> Path:
    if not isinstance(supplied, str) or not supplied:
        raise ValueError("guest executable must name a file inside Lo")
    path = local(Path(supplied))
    if not path.is_file() or path.stat().st_size > STREAM_LIMIT:
        raise ValueError(f"guest executable is missing or exceeds the declared limit: {path}")
    with path.open("rb") as source:
        if source.read(4) != b"\x7fELF":
            raise ValueError(f"guest executable must be ELF: {path}")
    return path


def verified(root: Path, record: dict[str, str]) -> Path:
    path = local(root / record["path"])
    with path.open("rb") as source:
        if hashlib.file_digest(source, "sha256").hexdigest() != record["sha256"]:
            raise ValueError(f"guest tool digest mismatch: {path}")
    return path


def initializer(preparation: Path, work: Path, environment: dict[str, str]) -> Path:
    distribution = preparation / "distribution"
    manifest = json.loads((distribution / "toolchain.json").read_text())
    target = manifest["targets"]["linux/arm64"]
    compiler = verified(distribution, manifest["compiler"])
    linker = verified(distribution, target["linker"])
    inputs = {name: verified(distribution, record) for name, record in target["inputs"].items()}
    obj, binary = work / "init.o", work / "init"
    values = {"{object}": str(obj), "{output}": str(binary), "{runtime}": "",
              **{"{input:" + name + "}": str(path) for name, path in inputs.items()}}
    commands = [
        [str(compiler), "--target=" + target["triple"], "-O2", "-c", str(Path(__file__).with_name("session_init.c")), "-o", str(obj)],
        [str(linker), *[values.get(arg, arg) for arg in target["link_args"] if arg != "{runtime}"]],
    ]
    for name in ("probe-template", "companion-template"):
        selected = {**values, "{output}": str(work / name)}
        commands.append([str(linker), *[selected.get(arg, arg) for arg in target["link_args"] if arg != "{runtime}"]])
    for command in commands:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, check=False)
        if result.returncode:
            raise ValueError(f"guest session initializer failed: {result.stderr.decode(errors='replace')}")
    return binary


class Guest:
    def __init__(self, preparation: Path, work: Path, archive: Path, environment: dict[str, str], accelerator: str) -> None:
        work.mkdir()
        self.serial = work / "serial.log"
        self.errors = work / "qemu.stderr"
        self.stdout = self.serial.open("wb")
        self.stderr = self.errors.open("wb")
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.settimeout(30)
        port = listener.getsockname()[1]
        emulation = preparation / "emulation"
        command = [str(local(emulation / "qemu-system-aarch64")), "-machine", "virt", "-accel", accelerator,
                   "-cpu", "host" if accelerator == "hvf" else "cortex-a72", "-m", "512", "-smp", "1", "-display", "none", "-monitor", "none",
                   "-serial", "stdio", "-no-reboot", "-nodefaults", "-nic", "none",
                   "-device", "virtio-serial-pci,id=transport",
                   "-chardev", f"socket,id=case,host=127.0.0.1,port={port}",
                   "-device", "virtserialport,bus=transport.0,nr=1,chardev=case,name=lo.case",
                   "-kernel", str(local(preparation / "vmlinuz-virt")), "-initrd", str(archive),
                   "-append", "console=ttyAMA0 rdinit=/init panic=-1 loglevel=3"]
        self.process: subprocess.Popen[bytes] | None = None
        self.connection: socket.socket | None = None
        self.sequence = 0
        try:
            self.process = subprocess.Popen(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                            stdout=self.stdout, stderr=self.stderr)
            self.connection, _ = listener.accept()
            self.connection.settimeout(30)
            if receive(self.connection, len(READY)) != READY:
                raise ValueError("guest session readiness record is invalid")
        except (OSError, ValueError) as error:
            self.close()
            detail = self.serial.read_text(errors="replace")[-4096:] + self.errors.read_text(errors="replace")[-4096:]
            raise ValueError(f"guest session startup failed: {error}: {detail}") from error
        finally:
            listener.close()

    def execute(self, request: Request) -> dict[str, object]:
        if self.connection is None or self.process is None or self.process.poll() is not None:
            raise ValueError("guest session exited before request execution")
        self.sequence += 1
        self.connection.settimeout(request.timeout_ms / 1000 + 10)
        return exchange(self.connection, self.sequence, request.executable.read_bytes(),
                        request.companion.read_bytes() if request.companion else b"", request.arguments,
                        request.stdin, request.timeout_ms, request.capture)

    def close(self) -> None:
        if self.connection is not None:
            self.connection.close()
            self.connection = None
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.stdout.close()
        self.stderr.close()


class GuestPool:
    def __init__(self, preparation: Path, work: Path, workers: int, *, accelerator: str = DEFAULT_ACCELERATOR) -> None:
        if type(workers) is not int or not 1 <= workers <= MAX_WORKERS:
            raise ValueError(f"guest workers must be between 1 and {MAX_WORKERS}")
        if accelerator not in ACCELERATORS:
            raise ValueError("guest accelerator must be tcg or hvf")
        self.preparation = local(preparation)
        self.work = local(work)
        if not self.work.is_relative_to(ROOT / "_scratch") or self.work.exists():
            raise ValueError("guest session workspace must be fresh and inside Lo/_scratch")
        self.workers = workers
        self.accelerator = accelerator
        self.guests: list[Guest] = []
        self.available: queue.Queue[Guest] = queue.Queue()
        self.boots = 0

    def __enter__(self) -> GuestPool:
        self.work.mkdir(parents=True)
        for name in ("home", "cache", "config", "data", "state", "clang"):
            (self.work / name).mkdir()
        environment = {"CI": "1", "PATH": "", "HOME": str(self.work / "home"), "TMPDIR": str(self.work),
                       "TMP": str(self.work), "TEMP": str(self.work), "LC_ALL": "C", "LANG": "C",
                       "XDG_CACHE_HOME": str(self.work / "cache"), "XDG_CONFIG_HOME": str(self.work / "config"),
                       "XDG_DATA_HOME": str(self.work / "data"), "XDG_STATE_HOME": str(self.work / "state"),
                       "CLANG_MODULE_CACHE_PATH": str(self.work / "clang"),
                       "DYLD_LIBRARY_PATH": str(self.preparation / "emulation/lib")}
        try:
            init = initializer(self.preparation, self.work, environment)
            root = self.work / "root"
            for path in ("dev", "proc", "case", "base"):
                (root / path).mkdir(parents=True, exist_ok=True)
            os.link(init, root / "init")
            os.link(self.work / "probe-template", root / "base/probe")
            os.link(self.work / "companion-template", root / "base/companion")
            shutil.copytree(local(self.preparation / "linux-sysroot/lib"), root / "base/lib", copy_function=os.link)
            # PID 1 itself needs the loader before it mounts a private case root.
            os.symlink("base/lib", root / "lib")
            archive = self.work / "root.cpio"
            result = subprocess.run(["/usr/bin/tar", "--format=newc", "-cf", str(archive), "-C", str(root), "."],
                                    cwd=ROOT, env=environment, stdin=subprocess.DEVNULL, capture_output=True, check=False)
            if result.returncode:
                raise ValueError(f"guest session archive failed: {result.stderr.decode(errors='replace')}")
            for index in range(self.workers):
                guest = Guest(self.preparation, self.work / f"guest-{index}", archive, environment, self.accelerator)
                self.guests.append(guest)
                self.available.put(guest)
                self.boots += 1
            return self
        except BaseException:
            self.close()
            raise

    def execute(self, record: object) -> dict[str, object]:
        request = Request.parse(record)
        guest = self.available.get()
        try:
            return guest.execute(request)
        except (OSError, ValueError):
            guest.close()
            raise
        finally:
            self.available.put(guest)

    def close(self) -> None:
        for guest in self.guests:
            guest.close()
        self.guests.clear()
        if self.work.exists():
            shutil.rmtree(self.work)

    def __exit__(self, *_: object) -> None:
        self.close()


def serve(pool: GuestPool, endpoint: Path, ready: Path | None) -> None:
    stopped = threading.Event()
    endpoint = local(endpoint)
    if len(os.fsencode(endpoint)) >= 104:
        raise ValueError("guest session socket path exceeds the host limit")
    def stop(_signum: int, _frame: object) -> None:
        stopped.set()
        for guest in pool.guests:
            guest.close()
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    def handle(connection: socket.socket) -> None:
        with connection:
            connection.settimeout(150)
            data = bytearray()
            while not data.endswith(b"\n"):
                part = connection.recv(65536)
                if not part:
                    return
                data.extend(part)
                if len(data) > STREAM_LIMIT * 4 // 3 + ARGUMENT_LIMIT:
                    connection.sendall(b'{"error":"guest request exceeds the declared limit"}\n')
                    return
            try:
                result = pool.execute(json.loads(data))
            except (OSError, ValueError) as error:
                result = {"error": str(error)}
            connection.sendall(json.dumps(result, separators=(",", ":")).encode() + b"\n")
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as listener, ThreadPoolExecutor(max_workers=pool.workers) as executor:
            listener.bind(str(endpoint))
            listener.listen(pool.workers)
            listener.settimeout(.2)
            if ready is not None:
                local(ready).write_text(json.dumps({"socket": str(endpoint), "workers": pool.workers,
                                                  "guest_boots": pool.boots, "accelerator": pool.accelerator}) + "\n")
            while not stopped.is_set():
                try:
                    connection, _ = listener.accept()
                except socket.timeout:
                    continue
                executor.submit(handle, connection)
    finally:
        endpoint.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preparation", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--socket", type=Path, required=True)
    parser.add_argument("--ready", type=Path)
    parser.add_argument("--workers", type=int, default=2)
    parser.add_argument("--accelerator", choices=ACCELERATORS, default=DEFAULT_ACCELERATOR)
    args = parser.parse_args()
    try:
        with GuestPool(args.preparation, args.work, args.workers, accelerator=args.accelerator) as pool:
            serve(pool, args.socket, args.ready)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"guest session: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
