"""Run P007 platform probes and the bounded ownership model inside Lo."""

from __future__ import annotations

import argparse
import errno
import hashlib
import json
import os
from pathlib import Path
import platform
import signal
import subprocess
from typing import Any

from model import verify_model


SOURCE = Path(__file__).resolve().parent
REPOSITORY = SOURCE.parent.parent
CASES = ("note-track", "es-admission", "coalition-admission", "setsid", "setpgid", "fork", "spawn-session", "spawn-group", "spawn-denied", "nproc-bound", "credential-change", "resource-change", "spawn-uid", "spawn-resetids", "spawn-persona", "open-marker")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workdir", type=Path, required=True)
    args = parser.parse_args()
    if not __debug__:
        parser.error("P007 checks require Python assertions; disable Python optimization")
    workdir = args.workdir.resolve()
    if not workdir.is_relative_to(REPOSITORY / "_scratch"):
        parser.error("--workdir must resolve inside this Lo repository's _scratch directory")
    if platform.system() != "Darwin":
        parser.error("P007 platform probes require Darwin")
    if os.getuid() == 0 or os.geteuid() == 0:
        parser.error("P007 platform probes require an unprivileged process")
    workdir.mkdir(parents=True, exist_ok=True)
    for name in ("tmp", "clang-cache"):
        (workdir / name).mkdir(exist_ok=True)
    environment = dict(os.environ, TMPDIR=str(workdir / "tmp"), CLANG_MODULE_CACHE_PATH=str(workdir / "clang-cache"), PYTHONDONTWRITEBYTECODE="1")

    def run(arguments: list[str]) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(arguments, cwd=workdir, env=environment, text=True, capture_output=True, timeout=20)
        if result.returncode:
            raise RuntimeError(f"{arguments[0]} exited {result.returncode}: {result.stdout} {result.stderr}")
        return result

    for name, extra in (("platform", ["-fblocks", "-lEndpointSecurity"]), ("ancestry", []), ("token", ["-lbsm"])):
        run(["xcrun", "--no-cache", "clang", "-std=c11", "-Wall", "-Wextra", "-Werror", "-Wno-deprecated-declarations", str(SOURCE / f"{name}.c"), *extra, "-o", str(workdir / name)])

    records: dict[str, list[dict[str, Any]]] = {}
    for scenario in CASES:
        output = run([str(workdir / "platform"), scenario])
        records[scenario] = [json.loads(line) for line in output.stdout.splitlines()]
    for name in ("ancestry", "token"):
        output = run([str(workdir / name)])
        records[name] = [json.loads(line) for line in output.stdout.splitlines()]

    assert records["note-track"][0]["data"] == errno.ENOTSUP
    admission = records["es-admission"][0]
    assert admission["result"] in range(7)
    assert admission["client_created"] == (admission["result"] == 0)
    coalition = records["coalition-admission"][0]
    assert coalition["symbols_available"]
    assert coalition["result"] == 0 or coalition["errno"] == errno.EPERM
    for name in ("setsid", "setpgid", "spawn-denied", "credential-change", "resource-change", "spawn-uid", "spawn-persona"):
        assert records[name][0]["errno"] == errno.EPERM
    child = next(row for row in records["spawn-resetids"] if row["case"] == "spawn-child")
    parent = next(row for row in records["spawn-resetids"] if row["case"] == "spawn-resetids")
    assert child["ruid"] == child["euid"] == parent["ruid"] == parent["euid"]
    assert child["nproc_hard"] == parent["nproc_hard"]
    assert records["open-marker"][0]["same_identity"] and records["open-marker"][0]["closed"]
    for row in records["nproc-bound"]:
        if row["case"] in ("fork-at-bound", "spawn-at-bound", "spawn-attributes-at-bound"):
            assert row["errno"] == errno.EAGAIN
    assert any(row["case"] == "fork-child" for row in records["fork"])
    for name in ("spawn-session", "spawn-group"):
        child = next(row for row in records[name] if row["case"] == "spawn-child")
        assert child["group"] == child["pid"]
        if name == "spawn-session":
            assert child["session"] == child["pid"]
    ancestry = records["ancestry"][0]
    assert ancestry["listed_child_bytes"] == 0 and ancestry["group_errno"] == errno.ESRCH
    assert ancestry["same_identity"] and ancestry["child_parent"] == 1
    assert records["ancestry"][1]["exit_confirmed"]
    token = records["token"][0]
    assert token["pid_same"] and token["execution_changed"] and token["stale_result"] == errno.ESRCH
    assert token["current_result"] == token["termination_result"] == 0
    assert token["signal"] == signal.SIGTERM
    report = {
        "platform": platform.platform(),
        "model": verify_model(),
        "records": records,
        "entitled_execution_tested": False,
        "source_sha256": {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(SOURCE.iterdir()) if path.suffix in (".c", ".py")},
    }
    (workdir / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"status": "passed", "model": report["model"], "es_admission": admission["result"], "entitled_execution_tested": False}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
