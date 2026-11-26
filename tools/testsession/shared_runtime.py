"""Keep one real runtime producer result alive across dependent test groups."""
from __future__ import annotations

import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OUTPUT_VARIABLE = "LO_TEST_RUNTIME_DISTRIBUTION"
SESSION_VARIABLE = "LO_TEST_SESSION_DIRECTORY"


def contained(path: Path) -> Path:
    if not path.is_absolute():
        raise ValueError("shared runtime paths must be absolute")
    selected = path.resolve()
    if not selected.is_relative_to(ROOT / "_scratch") or selected == ROOT / "_scratch":
        raise ValueError("shared runtime paths must remain inside Lo/_scratch")
    for part in (path, *path.parents):
        if part == ROOT:
            break
        if part.is_symlink():
            raise ValueError(f"shared runtime path traverses a symlink: {part}")
    return selected


def selected() -> Path | None:
    supplied = os.environ.get(OUTPUT_VARIABLE)
    if supplied is None:
        return None
    session = os.environ.get(SESSION_VARIABLE)
    if not session:
        raise ValueError(f"{OUTPUT_VARIABLE} requires {SESSION_VARIABLE}")
    root = contained(Path(session))
    output = contained(Path(supplied))
    if not output.is_relative_to(root) or output == root:
        raise ValueError("shared runtime output must belong to its test session")
    return output


def output(private: Path) -> tuple[Path, bool]:
    shared = selected()
    if shared is None:
        return private, False
    if shared.is_relative_to(contained(private.parent)):
        raise ValueError("shared runtime output must remain outside the case temporary directory")
    if shared.exists():
        raise ValueError("shared runtime producer output must be fresh")
    return shared, True


def input(private: Path) -> Path:
    shared = selected()
    if shared is None:
        return private
    if any(not (shared / name).is_file() for name in ("toolchain.json", "build-receipt.json")):
        raise ValueError("declared shared runtime producer did not complete")
    return shared
