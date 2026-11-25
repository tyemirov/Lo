"""Ownership contracts for producer results shared between test groups."""
from __future__ import annotations

from pathlib import Path
import shutil

import pytest

from tools.testsession import shared_runtime


def test_runtime_output_survives_case_cleanup_and_is_consumable_by_next_group(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    case = tmp_path / "case"
    case.mkdir()
    output = tmp_path / "runtime"
    monkeypatch.setenv("LO_TEST_SESSION_DIRECTORY", str(tmp_path))
    monkeypatch.setenv("LO_TEST_RUNTIME_DISTRIBUTION", str(output))
    selected, shared = shared_runtime.output(case / "private")
    assert selected == output and shared
    selected.mkdir()
    (selected / "toolchain.json").write_text("{}")
    (selected / "build-receipt.json").write_text("{}")
    shutil.rmtree(case)
    assert shared_runtime.input(case / "unused") == output
    with pytest.raises(ValueError, match="must be fresh"):
        shared_runtime.output(case / "private")


@pytest.mark.parametrize("problem", ["relative", "outside_session", "inside_case", "missing_receipt"])
def test_runtime_selection_rejects_wrong_ownership_or_unfinished_producer(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, problem: str
) -> None:
    session = tmp_path / "session"
    session.mkdir()
    case = session / "case"
    case.mkdir()
    selected = session / "runtime"
    if problem == "relative":
        selected = Path("relative-runtime")
    elif problem == "outside_session":
        selected = tmp_path / "outside"
    elif problem == "inside_case":
        selected = case / "runtime"
    monkeypatch.setenv("LO_TEST_SESSION_DIRECTORY", str(session))
    monkeypatch.setenv("LO_TEST_RUNTIME_DISTRIBUTION", str(selected))
    with pytest.raises(ValueError):
        if problem == "missing_receipt":
            shared_runtime.input(case / "unused")
        else:
            shared_runtime.output(case / "unused")


def test_private_runtime_output_remains_owned_by_its_test(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.delenv("LO_TEST_RUNTIME_DISTRIBUTION", raising=False)
    private = tmp_path / "private"
    assert shared_runtime.output(private) == (private, False)
