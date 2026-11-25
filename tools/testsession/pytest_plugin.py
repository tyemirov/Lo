"""Publish complete pytest collection and outcomes to the session runner."""
from __future__ import annotations

import json
import os
from pathlib import Path
import shutil
import sys

_outcomes: dict[str, str] = {}
_temporary_paths: dict[str, Path] = {}
_identifiers: dict[str, str] = {}
ROOT = Path(__file__).resolve().parents[2]
_session_root: Path | None = None


def pytest_configure(config) -> None:
    global _session_root
    root = Path(os.environ["LO_TEST_SESSION_DIRECTORY"]).resolve(strict=True)
    if not root.is_dir() or not root.is_relative_to(ROOT / "_scratch"):
        raise ValueError(f"pytest session root must stay inside Lo scratch: {root}")
    _session_root = root


def session_root() -> Path:
    if _session_root is None:
        raise RuntimeError("pytest session root was not configured")
    return _session_root


def identifier(item) -> str:
    path = item.path.resolve()
    if not path.is_relative_to(ROOT):
        raise ValueError(f"collected test leaves Lo: {path}")
    suffix = item.nodeid.split("::", 1)
    selected = path.relative_to(ROOT).as_posix()
    if len(suffix) == 2:
        selected += "::" + suffix[1]
    _identifiers[item.nodeid] = selected
    return selected

def emit(action: str, identifier: str) -> None:
    identifier = _identifiers.get(identifier, identifier)
    sys.__stdout__.write("\nLO_TEST_EVENT:" + json.dumps({"Action": action, "Test": identifier}) + "\n")
    sys.__stdout__.flush()


def pytest_collection_finish(session) -> None:
    for item in session.items:
        emit("collect", identifier(item))


def pytest_deselected(items) -> None:
    for item in items:
        emit("deselect", identifier(item))


def pytest_runtest_makereport(item, call) -> None:
    path = item.funcargs.get("tmp_path")
    if isinstance(path, Path):
        root = session_root()
        selected = path.resolve()
        if not selected.is_relative_to(root) or selected == root:
            raise ValueError(f"case temporary path leaves its session: {selected}")
        _temporary_paths[item.nodeid] = selected


def pytest_runtest_logreport(report) -> None:
    if report.when == "setup":
        emit("run", report.nodeid)
    if report.failed:
        _outcomes[report.nodeid] = "fail"
    elif report.skipped:
        _outcomes.setdefault(report.nodeid, "skip")
    if report.when == "teardown":
        path = _temporary_paths.pop(report.nodeid, None)
        if path is not None:
            try:
                root = session_root()
                parent = path.parent.resolve(strict=True)
                if parent != path.parent or not parent.is_relative_to(root):
                    raise ValueError(f"case temporary parent changed before cleanup: {path.parent}")
                if path.is_symlink():
                    path.unlink()
                else:
                    shutil.rmtree(path)
            except FileNotFoundError:
                pass
        emit(_outcomes.pop(report.nodeid, "pass"), report.nodeid)
