"""Verify Python and Rust test definitions against the original inventory."""
from __future__ import annotations

import argparse
import ast
from dataclasses import asdict, dataclass
import json
from pathlib import Path
import re
import sys
from typing import Any, Iterator

ROOT = Path(__file__).resolve().parents[2]
RAW_STRING = re.compile(r'(?:br|r)(#{0,255})"')
CHARACTER = re.compile(r"'(?:\\(?:u\{[0-9a-fA-F_]+\}|x[0-9a-fA-F]{2}|.)|[^'\\\n])'")


@dataclass(frozen=True)
class PythonCase:
    file: str
    name: str
    parameters: tuple[str, ...]


@dataclass(frozen=True)
class RustCase:
    file: str
    name: str


def contained(path: Path, root: Path = ROOT) -> Path:
    selected = path.resolve(strict=True)
    if not selected.is_relative_to(root):
        raise ValueError(f"inventory path leaves Lo: {path}")
    return selected


def python_cases(root: Path) -> tuple[PythonCase, ...]:
    result = []

    def definitions(
        nodes: list[ast.stmt], classes: tuple[str, ...] = ()
    ) -> Iterator[tuple[ast.FunctionDef | ast.AsyncFunctionDef, str]]:
        for node in nodes:
            if isinstance(node, ast.ClassDef):
                yield from definitions(node.body, (*classes, node.name))
            elif isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name.startswith("test_"):
                yield node, "::".join((*classes, node.name))

    for name in ("tools", "experiments"):
        for path in sorted((root / name).rglob("test*.py")):
            contained(path, root)
            module = ast.parse(path.read_text(), filename=str(path))
            for function, name in definitions(module.body):
                parameters = []
                for decorator in function.decorator_list:
                    if not isinstance(decorator, ast.Call):
                        continue
                    selected = decorator.func
                    if (isinstance(selected, ast.Attribute) and selected.attr == "parametrize"
                            or isinstance(selected, ast.Name) and selected.id == "parametrize"):
                        parameters.append(ast.unparse(decorator))
                result.append(PythonCase(path.relative_to(root).as_posix(), name, tuple(parameters)))
    return tuple(result)


def rust_code(source: str) -> str:
    output = list(source)
    position = 0
    while position < len(source):
        end = position
        if source.startswith("//", position):
            end = source.find("\n", position)
            if end < 0:
                end = len(source)
        elif source.startswith("/*", position):
            depth, end = 1, position + 2
            while depth and end < len(source):
                if source.startswith("/*", end):
                    depth += 1
                    end += 2
                elif source.startswith("*/", end):
                    depth -= 1
                    end += 2
                else:
                    end += 1
            if depth:
                raise ValueError("unterminated Rust block comment")
        else:
            raw = RAW_STRING.match(source, position)
            if raw and (position == 0 or not (source[position - 1].isalnum() or source[position - 1] == "_")):
                suffix = '"' + raw[1]
                end = source.find(suffix, raw.end())
                if end < 0:
                    raise ValueError("unterminated Rust raw string")
                end += len(suffix)
            elif source[position] == '"':
                end = position + 1
                while end < len(source):
                    if source[end] == "\\":
                        end += 2
                    elif source[end] == '"':
                        end += 1
                        break
                    else:
                        end += 1
                else:
                    raise ValueError("unterminated Rust string")
            elif source[position] == "'":
                character = CHARACTER.match(source, position)
                if character:
                    end = character.end()
        if end > position:
            for index in range(position, end):
                if output[index] != "\n":
                    output[index] = " "
            position = end
        else:
            position += 1
    return "".join(output)


def rust_cases(root: Path) -> tuple[RustCase, ...]:
    result = []
    for path in sorted((root / "runtime/native/src").rglob("*.rs")):
        contained(path, root)
        source = rust_code(path.read_text())
        for attribute in re.finditer(r"#\s*\[\s*test\s*\]", source):
            tail = source[attribute.end():]
            function = re.search(r"\bfn\s+([A-Za-z_][A-Za-z_0-9]*)\s*\(", tail)
            if function is None or "{" in tail[:function.start()] or ";" in tail[:function.start()]:
                raise ValueError(f"invalid Rust test declaration in {path.relative_to(root)}")
            result.append(RustCase(path.relative_to(root).as_posix(), function[1]))
    return tuple(result)


def original_rows(data: dict[str, Any], kind: str) -> list[dict[str, Any]]:
    rows = data.get(kind)
    if not isinstance(rows, list):
        raise ValueError(f"original {kind} inventory must be a list")
    fields = {"file", "name", "parameters"} if kind == "python" else {"file", "name"}
    seen = set()
    for row in rows:
        if (not isinstance(row, dict) or set(row) != fields
                or not all(isinstance(row.get(key), str) and row[key] for key in ("file", "name"))):
            raise ValueError(f"invalid original {kind} inventory record: {row!r}")
        identity = (row["file"], row["name"])
        if identity in seen:
            raise ValueError(f"duplicate original {kind} case: {'/'.join(identity)}")
        seen.add(identity)
        if kind == "python" and (not isinstance(row["parameters"], list)
                                  or not all(isinstance(item, str) and item for item in row["parameters"])):
            raise ValueError(f"invalid original python parameters: {'/'.join(identity)}")
    return rows


def inventory(root: Path, baseline: Path) -> dict[str, list[dict[str, Any]]]:
    data = json.loads(baseline.read_text())
    if not isinstance(data, dict):
        raise ValueError("original test inventory must be an object")
    python, rust = python_cases(root), rust_cases(root)
    for kind, current in (("python", python), ("rust", rust)):
        index = {(case.file, case.name): case for case in current}
        if len(index) != len(current):
            raise ValueError(f"duplicate current {kind} test identifier")
        for original in original_rows(data, kind):
            identity = (original["file"], original["name"])
            if identity not in index:
                raise ValueError(f"original {kind} test missing: {'/'.join(identity)}")
            if kind == "python" and tuple(original["parameters"]) != index[identity].parameters:
                raise ValueError(f"original python parameters changed: {'/'.join(identity)}")
    return {"python": [asdict(case) for case in python], "rust": [asdict(case) for case in rust]}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--baseline", type=Path)
    arguments = parser.parse_args()
    try:
        root = contained(arguments.root)
        baseline = contained(arguments.baseline or root / "testdata/testsession/inventory.json", root)
        result = inventory(root, baseline)
    except (OSError, ValueError, SyntaxError) as error:
        print(f"test inventory: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
