"""Public stop-controller behavior with deterministic owned-child boundaries."""
from __future__ import annotations

import errno
import importlib.util
import json
from pathlib import Path
import subprocess
import sys

import pytest

REPOSITORY = Path(__file__).resolve().parents[2]
SOURCE = REPOSITORY / "experiments/process-resources-controller/probe.py"


@pytest.fixture
def controller_type() -> type:
    specification = importlib.util.spec_from_file_location("lo_resource_controller_under_test", SOURCE)
    assert specification is not None and specification.loader is not None
    module = importlib.util.module_from_spec(specification)
    sys.modules[specification.name] = module
    specification.loader.exec_module(module)
    return module.OwnedChildController


class OwnedChild:
    pid = 12345

    def __init__(self) -> None:
        self.returncode: int | None = None
        self.events: list[str] = []

    def poll(self) -> int | None:
        self.events.append("poll")
        return self.returncode

    def wait(self, timeout: float) -> int:
        assert 0 < timeout <= 2
        self.events.append("wait")
        if self.returncode is None:
            raise subprocess.TimeoutExpired("owned-fixture", timeout)
        return self.returncode


def streams(complete: bool = True) -> dict[str, dict[str, bool]]:
    return {name: {"eof": complete, "closed": True} for name in ("stdout", "stderr")}


@pytest.mark.parametrize("signal_errno", [errno.EPERM, errno.ESRCH])
def test_signal_error_with_immediate_terminal_retains_evidence(controller_type: type, signal_errno: int) -> None:
    child = OwnedChild()

    def signal_group(pid: int, number: int) -> None:
        assert pid == child.pid and number == 9
        child.events.append("signal")
        child.returncode = 0
        raise OSError(signal_errno, "injected signal failure")

    controller = controller_type(child, signal_group)
    controller.request_stop("output_limit")
    assert child.events == ["poll", "signal", "poll"]
    result = controller.finish(streams(), timeout=2)
    assert result["stop_reason"] == "output_limit"
    assert result["signal_errors"] == [{"errno": signal_errno}]
    assert result["stop_failed"] is False
    assert result["terminal_confirmed"] is True
    assert result["cleanup_complete"] is True


def test_signal_error_alive_then_exit_is_sticky_failure(controller_type: type) -> None:
    child = OwnedChild()

    def signal_group(pid: int, number: int) -> None:
        child.events.append("signal")
        raise OSError(errno.EPERM, "injected signal failure")

    controller = controller_type(child, signal_group)
    controller.request_stop("output_limit")
    assert child.events == ["poll", "signal", "poll"]
    child.returncode = 0  # Later drainage observes exit; it cannot repair stop.
    result = controller.finish(streams(), timeout=2)
    assert result["signal_errors"] == [{"errno": errno.EPERM}]
    assert result["stop_failed"] is True
    assert result["terminal_confirmed"] is True
    assert result["cleanup_complete"] is True


def test_signal_error_unresolved_deadline_records_incomplete_cleanup(controller_type: type) -> None:
    child = OwnedChild()

    def signal_group(pid: int, number: int) -> None:
        raise OSError(errno.EPERM, "injected signal failure")

    controller = controller_type(child, signal_group)
    controller.request_stop("output_limit")
    result = controller.finish(streams(complete=False), timeout=2)
    assert result["stop_failed"] is True
    assert result["terminal_confirmed"] is False
    assert result["cleanup_complete"] is False
    assert result["signal_errors"] == [{"errno": errno.EPERM}]
    assert result["wait_errors"]


def test_successful_stop_followed_by_drain_and_reap(controller_type: type) -> None:
    child = OwnedChild()

    def signal_group(pid: int, number: int) -> None:
        assert pid == child.pid and number == 9
        child.events.append("signal")
        child.returncode = -9

    controller = controller_type(child, signal_group)
    controller.request_stop("output_limit")
    result = controller.finish(streams(), timeout=2)
    assert child.events.count("signal") == 1
    assert result["signal_errors"] == []
    assert result["stop_failed"] is False
    assert result["terminal_confirmed"] is True
    assert result["cleanup_complete"] is True


def test_already_reaped_child_is_never_signaled(controller_type: type) -> None:
    child = OwnedChild()
    child.returncode = 0

    def signal_group(pid: int, number: int) -> None:
        pytest.fail("a reaped child must never be signaled")

    controller = controller_type(child, signal_group)
    controller.request_stop("output_limit")
    result = controller.finish(streams(), timeout=2)
    assert result["cleanup_complete"] is True
    assert result["stop_failed"] is False


@pytest.mark.parametrize("arguments", [["run"], ["verify"], ["run", "unreviewed"], ["verify", "unreviewed"]])
def test_review_required_before_access(arguments: list[str]) -> None:
    result = subprocess.run([sys.executable, "-B", str(SOURCE), *arguments], cwd=REPOSITORY, stdin=subprocess.DEVNULL, capture_output=True, timeout=5, check=False)
    assert result.returncode == 2
    assert result.stdout == b""
    assert json.loads(result.stderr) == {"error": "reviewed manifest SHA256 is required"}
