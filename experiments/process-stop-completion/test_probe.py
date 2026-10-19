"""Completion contract through deterministic owned-child and clock boundaries."""
from __future__ import annotations

import errno
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
from typing import Callable

import pytest

REPOSITORY = Path(__file__).resolve().parents[2]
SOURCE = REPOSITORY / "experiments/process-stop-completion/probe.py"


@pytest.fixture
def controller_type() -> type:
    specification = importlib.util.spec_from_file_location("lo_stop_completion_under_test", SOURCE)
    assert specification is not None and specification.loader is not None
    module = importlib.util.module_from_spec(specification)
    sys.modules[specification.name] = module
    specification.loader.exec_module(module)
    return module.CompletionController


class Clock:
    def __init__(self) -> None:
        self.now = 0.0

    def __call__(self) -> float:
        return self.now


class OwnedChild:
    pid = 12345

    def __init__(self) -> None:
        self.returncode: int | None = None
        self.wait_calls: list[float] = []
        self.wait_action: Callable[[], None] = lambda: None
        self.poll_error = False

    def poll(self) -> int | None:
        if self.poll_error:
            raise OSError(errno.EIO, "injected ownership observation failure")
        return self.returncode

    def wait(self, timeout: float) -> int:
        assert 0 < timeout <= 2
        self.wait_calls.append(timeout)
        self.wait_action()
        if self.returncode is None:
            raise subprocess.TimeoutExpired("owned-fixture", timeout)
        return self.returncode


def streams(complete: bool = True) -> dict[str, dict[str, bool]]:
    return {name: {"eof": complete, "closed": True} for name in ("stdout", "stderr")}


def denied_signal(pid: int, number: int) -> None:
    assert pid == OwnedChild.pid and number == 9
    raise OSError(errno.EPERM, "injected signal failure")


def test_signal_error_then_later_in_bound_completion(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()

    def terminate() -> None:
        clock.now = 1
        child.returncode = 0

    child.wait_action = terminate
    controller = controller_type(child, denied_signal, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    assert child.returncode is None
    result = controller.finish(streams(), descendants_complete=True)
    assert child.wait_calls == [2]
    assert result["signal_errors"] == [{"errno": errno.EPERM}]
    assert result["host_kill"] is False
    assert result["stop_reason"] == "output_limit"
    assert result["stop_failed"] is False
    assert result["deadline_failed"] is False
    assert result["cleanup_complete"] is True


@pytest.mark.parametrize("unknown", [False, True])
def test_alive_or_unknown_at_deadline_fails_without_positive_wait(controller_type: type, unknown: bool) -> None:
    clock, child = Clock(), OwnedChild()
    controller = controller_type(child, denied_signal, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    child.poll_error = unknown
    clock.now = 2
    result = controller.finish(streams(complete=False), descendants_complete=False)
    assert child.wait_calls == []
    assert result["stop_failed"] is True
    assert result["deadline_failed"] is True
    assert result["cleanup_complete"] is False


def test_late_terminal_cleanup_does_not_repair_deadline_failure(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()
    controller = controller_type(child, denied_signal, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    clock.now = 2.1
    child.returncode = 0
    result = controller.finish(streams(), descendants_complete=True)
    assert child.wait_calls == []
    assert result["terminal_confirmed"] is True
    assert result["cleanup_complete"] is True
    assert result["deadline_failed"] is True
    assert result["stop_failed"] is True


def test_incomplete_pipes_block_completion(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()
    controller = controller_type(child, denied_signal, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    child.returncode = 0
    clock.now = 1
    result = controller.finish(streams(complete=False), descendants_complete=True)
    assert result["terminal_confirmed"] is True
    assert result["cleanup_complete"] is False
    assert result["stop_failed"] is True


def test_unverified_descendants_block_completion(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()
    controller = controller_type(child, denied_signal, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    child.returncode = 0
    clock.now = 1
    result = controller.finish(streams(), descendants_complete=False)
    assert result["cleanup_complete"] is False
    assert result["stop_failed"] is True


def test_failed_wait_remains_failure_even_if_cleanup_is_later_confirmed(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()

    def failed_wait() -> None:
        clock.now = 1
        child.returncode = 0
        raise OSError(errno.EIO, "injected wait failure")

    child.wait_action = failed_wait
    controller = controller_type(child, denied_signal, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    result = controller.finish(streams(), descendants_complete=True)
    assert result["wait_errors"] == [{"errno": errno.EIO}]
    assert result["cleanup_complete"] is True
    assert result["stop_failed"] is True


def test_successful_signal_then_drain_and_reap(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()
    signals: list[tuple[int, int]] = []

    def signal_group(pid: int, number: int) -> None:
        signals.append((pid, number))
        child.returncode = -9

    controller = controller_type(child, signal_group, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    clock.now = 1
    result = controller.finish(streams(), descendants_complete=True)
    assert signals == [(child.pid, 9)]
    assert result["host_kill"] is True
    assert result["signal_errors"] == []
    assert result["stop_failed"] is False
    assert result["cleanup_complete"] is True


def test_never_signal_an_already_reaped_child(controller_type: type) -> None:
    clock, child = Clock(), OwnedChild()
    child.returncode = 0

    def signal_group(pid: int, number: int) -> None:
        pytest.fail("must not signal an already reaped child")

    controller = controller_type(child, signal_group, cleanup_timeout=2, clock=clock)
    controller.request_stop("output_limit")
    result = controller.finish(streams(), descendants_complete=True)
    assert result["stop_failed"] is False
    assert result["cleanup_complete"] is True


@pytest.mark.parametrize("arguments", [["run"], ["verify"], ["run", "unreviewed"], ["verify", "unreviewed"]])
def test_review_required_before_access(arguments: list[str]) -> None:
    result = subprocess.run([sys.executable, "-B", str(SOURCE), *arguments], cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True, timeout=5, check=False)
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}
