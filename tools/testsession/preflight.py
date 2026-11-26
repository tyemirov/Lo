"""Estimate a complete measured test schedule without executing test commands."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import heapq
import json
import math
import os
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[2]
SOURCE_DIRECTORIES = ("cmd", "internal", "tests", "tools", "runtime", "testdata", "examples", "experiments", "docs/examples")
SOURCE_FILES = ("go.mod", "go.sum", "Makefile", "docs/evidence/training-reference-results.json")
GENERATED_DIRECTORIES = {".git", "__pycache__", ".pytest_cache"}
JOB_FIELDS = {"id", "argv", "cwd", "environment", "format", "package", "resource", "expected", "allow_skips", "release", "after"}


class PreflightError(ValueError):
    def __init__(self, message: str, unmeasured: tuple[str, ...] = (), measured: int = 0):
        super().__init__(message)
        self.unmeasured = unmeasured
        self.measured = measured


def local(path: Path) -> Path:
    if not path.is_absolute():
        raise PreflightError(f"preflight paths must be absolute: {path}")
    selected = path.resolve()
    if not selected.is_relative_to(ROOT):
        raise PreflightError(f"preflight input leaves Lo: {path}")
    return selected


def source_fingerprint(root: Path) -> str:
    root = local(root)
    if not root.is_dir() or not (root / "go.mod").is_file():
        raise PreflightError("fingerprint root requires a repository-local directory with go.mod")
    paths: list[Path] = []

    def collect(path: Path, ancestors: frozenset[Path]) -> None:
        resolved = path.resolve(strict=True)
        if not resolved.is_relative_to(root) or not resolved.is_relative_to(ROOT):
            raise PreflightError(f"source path leaves its fingerprint root: {path}")
        if path.is_dir():
            if resolved in ancestors:
                raise PreflightError(f"source directory cycle: {path}")
            for child in sorted(path.iterdir()):
                if child.name not in GENERATED_DIRECTORIES and child.name != ".DS_Store":
                    collect(child, ancestors | {resolved})
        elif path.is_file():
            paths.append(path)
        else:
            raise PreflightError(f"source input is not a regular file or directory: {path}")

    for name in SOURCE_DIRECTORIES:
        path = root / name
        if path.exists() or path.is_symlink():
            collect(path, frozenset())
    for name in SOURCE_FILES:
        path = root / name
        if path.exists() or path.is_symlink():
            collect(path, frozenset())
    digest = hashlib.sha256()
    for path in sorted(paths):
        member = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(65536), b""):
                member.update(chunk)
        digest.update(os.fsencode(path.relative_to(root).as_posix()) + b"\0" + member.digest() + b"\n")
    return digest.hexdigest()


def reject_constant(value: str) -> None:
    raise PreflightError(f"JSON contains a non-finite numeric constant: {value}")


def unique_fields(pairs: list[tuple[str, object]]) -> dict[str, object]:
    record: dict[str, object] = {}
    for key, value in pairs:
        if key in record:
            raise PreflightError(f"JSON contains a duplicate field: {key}")
        record[key] = value
    return record


def read_json(path: Path) -> object:
    selected = local(path)
    with selected.open(encoding="utf-8") as source:
        return json.load(source, parse_constant=reject_constant, object_pairs_hook=unique_fields)


def number(value: object, subject: str) -> float:
    if type(value) not in {int, float}:
        raise PreflightError(f"{subject} must be a finite nonnegative number")
    try:
        numeric = float(value)
    except OverflowError as error:
        raise PreflightError(f"{subject} exceeds the finite numeric range") from error
    if not math.isfinite(numeric) or numeric < 0:
        raise PreflightError(f"{subject} must be a finite nonnegative number")
    return numeric


@dataclass(frozen=True)
class Resources:
    workers: int
    build_slots: int
    guest_workers: int
    max_bytes: int

    def __post_init__(self) -> None:
        for name in ("workers", "build_slots", "guest_workers", "max_bytes"):
            if type(getattr(self, name)) is not int or getattr(self, name) < 1:
                raise PreflightError(f"{name} must be a positive integer")
        if self.max_bytes > (1 << 63) - 1:
            raise PreflightError("max_bytes exceeds the runner numeric range")


@dataclass(frozen=True)
class Job:
    identifier: str
    resource: str
    after: tuple[str, ...]

    @classmethod
    def parse(cls, record: object) -> Job:
        if not isinstance(record, dict) or set(record) - JOB_FIELDS:
            raise PreflightError("manifest job has unknown fields or an invalid shape")
        identifier = record.get("id")
        if not isinstance(identifier, str) or not identifier or "\0" in identifier:
            raise PreflightError("manifest job requires a nonempty string id")
        resource = record.get("resource", "host")
        if not isinstance(resource, str) or resource not in {"host", "guest", "host-http", ""}:
            raise PreflightError(f"manifest job {identifier} has an unsupported resource")
        argv = record.get("argv")
        if not isinstance(argv, list) or not argv or any(not isinstance(arg, str) or "\0" in arg for arg in argv):
            raise PreflightError(f"manifest job {identifier} requires a nonempty string argv list")
        for key in ("cwd", "format", "package"):
            if key in record and (not isinstance(record[key], str) or "\0" in record[key]):
                raise PreflightError(f"manifest job {identifier} field {key} must be a string without NUL")
        if "environment" in record and (not isinstance(record["environment"], dict) or any(
                not isinstance(key, str) or not key or "=" in key or "\0" in key
                or not isinstance(value, str) or "\0" in value for key, value in record["environment"].items())):
            raise PreflightError(f"manifest job {identifier} has invalid environment strings")
        if "expected" in record and (not isinstance(record["expected"], list) or any(
                not isinstance(name, str) or not name for name in record["expected"])):
            raise PreflightError(f"manifest job {identifier} expected must contain test identifiers")
        if "release" in record and (not isinstance(record["release"], list) or any(
                not isinstance(path, str) or not path or "\0" in path for path in record["release"])):
            raise PreflightError(f"manifest job {identifier} release must contain nonempty paths without NUL")
        after = record.get("after", [])
        if not isinstance(after, list) or any(not isinstance(name, str) or not name or "\0" in name for name in after):
            raise PreflightError(f"manifest job {identifier} after must contain nonempty group ids without NUL")
        if len(after) != len(set(after)):
            raise PreflightError(f"manifest job {identifier} repeats a dependency")
        if "allow_skips" in record and (not isinstance(record["allow_skips"], dict) or any(
                not isinstance(key, str) or not key or not isinstance(reasons, list)
                or any(not isinstance(reason, str) or not reason for reason in reasons)
                for key, reasons in record["allow_skips"].items())):
            raise PreflightError(f"manifest job {identifier} has invalid declared skip reasons")
        return cls(identifier, resource or "host", tuple(after))


@dataclass(frozen=True)
class Manifest:
    prepare: tuple[Job, ...]
    groups: tuple[Job, ...]
    finalize: tuple[Job, ...]

    @classmethod
    def parse(cls, record: object, fingerprint: str) -> Manifest:
        if not isinstance(record, dict) or set(record) - {"source_sha256", "prepare", "groups", "finalize"}:
            raise PreflightError("manifest requires the current prepare/groups/finalize shape")
        if "source_sha256" in record and record["source_sha256"] != fingerprint:
            raise PreflightError("manifest source is stale or invalid")
        phases = []
        identifiers: set[str] = set()
        for name in ("prepare", "groups", "finalize"):
            rows = record.get(name, [])
            if not isinstance(rows, list):
                raise PreflightError(f"manifest {name} must be a list")
            jobs = tuple(Job.parse(row) for row in rows)
            for job in jobs:
                if job.identifier in identifiers:
                    raise PreflightError(f"duplicate manifest job id: {job.identifier}")
                identifiers.add(job.identifier)
            phases.append(jobs)
        if not phases[1]:
            raise PreflightError("manifest contains no test groups")
        if any(job.after for job in (*phases[0], *phases[2])):
            raise PreflightError("only manifest groups may declare after dependencies")
        groups = {job.identifier: job for job in phases[1]}
        followers: dict[str, list[str]] = {identifier: [] for identifier in groups}
        dependencies = {job.identifier: len(job.after) for job in phases[1]}
        for job in phases[1]:
            for name in job.after:
                if name not in groups:
                    raise PreflightError(f"manifest group {job.identifier} depends on an unknown group: {name}")
                followers[name].append(job.identifier)
        ready = [identifier for identifier, count in dependencies.items() if count == 0]
        visited = 0
        while ready:
            identifier = ready.pop()
            visited += 1
            for follower in followers[identifier]:
                dependencies[follower] -= 1
                if dependencies[follower] == 0:
                    ready.append(follower)
        if visited != len(groups):
            raise PreflightError("manifest group dependencies contain a cycle")
        return cls(*phases)


@dataclass(frozen=True)
class Measurement:
    durations: tuple[tuple[str, float], ...]

    @classmethod
    def parse(cls, record: object, fingerprint: str, resources: Resources) -> Measurement:
        if not isinstance(record, dict) or record.get("status") != "pass":
            raise PreflightError("measurement report did not pass")
        supplied = record.get("source_sha256")
        if not isinstance(supplied, str) or not re.fullmatch(r"[0-9a-f]{64}", supplied):
            raise PreflightError("measurement report requires a current source_sha256")
        if supplied != fingerprint:
            raise PreflightError("measurement report source is stale")
        for name in ("workers", "build_slots", "guest_workers"):
            if type(record.get(name)) is not int or record[name] != getattr(resources, name):
                raise PreflightError(f"measurement report resource configuration differs: {name}")
        maximum = record.get("max_bytes")
        if type(maximum) is not int or maximum <= 0:
            raise PreflightError("measurement report requires a positive integer max_bytes")
        if maximum > resources.max_bytes:
            raise PreflightError("measurement report storage budget exceeds the requested max_bytes")
        durations = record.get("group_seconds")
        if not isinstance(durations, dict):
            raise PreflightError("measurement report requires group_seconds")
        values = []
        for key, value in durations.items():
            if not isinstance(key, str) or not key:
                raise PreflightError("measurement duration requires a nonempty group id")
            values.append((key, number(value, f"duration of {key}")))
        return cls(tuple(values))


def schedule(groups: tuple[Job, ...], durations: dict[str, float], resources: Resources) -> float:
    pending = list(groups)
    active: list[tuple[float, int, str, str]] = []
    completed: set[str] = set()
    guests = 0
    host_http = 0
    now = 0.0
    sequence = 0
    while pending or active:
        while len(active) < resources.workers and pending:
            chosen = next((index for index, job in enumerate(pending)
                           if all(name in completed for name in job.after)
                           and (job.resource != "guest" or guests < resources.guest_workers)
                           and (job.resource != "host-http" or host_http < 1)), None)
            if chosen is None:
                break
            job = pending.pop(chosen)
            guests += int(job.resource == "guest")
            host_http += int(job.resource == "host-http")
            sequence += 1
            heapq.heappush(active, (now + durations[job.identifier], sequence, job.resource, job.identifier))
        if not active:
            raise PreflightError("manifest group resources cannot make progress")
        now, _, resource, identifier = heapq.heappop(active)
        completed.add(identifier)
        guests -= int(resource == "guest")
        host_http -= int(resource == "host-http")
    return now


def estimate(root: Path, manifest_path: Path, reports: tuple[Path, ...], resources: Resources,
             limit: float, margin: float) -> dict[str, object]:
    limit, margin = number(limit, "time limit"), number(margin, "time margin")
    if limit <= 0:
        raise PreflightError("time limit must be positive")
    fingerprint = source_fingerprint(root)
    manifest = Manifest.parse(read_json(manifest_path), fingerprint)
    durations: dict[str, float] = {}
    if not reports:
        raise PreflightError("preflight requires measurement reports")
    for path in reports:
        try:
            measurement = Measurement.parse(read_json(path), fingerprint, resources)
        except PreflightError as error:
            raise PreflightError(f"measurement {path}: {error}") from error
        for key, value in measurement.durations:
            durations[key] = max(durations.get(key, 0), value)
    jobs = (*manifest.prepare, *manifest.groups, *manifest.finalize)
    missing = tuple(sorted(job.identifier for job in jobs if job.identifier not in durations))
    measured = len(jobs) - len(missing)
    if missing:
        raise PreflightError("required manifest jobs lack passing current measurements", missing, measured)
    preparation = sum(durations[job.identifier] for job in manifest.prepare)
    execution = schedule(manifest.groups, durations, resources)
    finalization = sum(durations[job.identifier] for job in manifest.finalize)
    expected = preparation + execution + finalization + margin
    if not math.isfinite(expected):
        raise PreflightError("measured schedule exceeds the finite numeric range")
    result: dict[str, object] = {"status": "pass" if expected < limit else "fail", "source_sha256": fingerprint,
                                 "expected_seconds": expected, "measured_groups": measured, "unmeasured": [],
                                 "prepare_seconds": preparation, "groups_seconds": execution,
                                 "finalize_seconds": finalization, "margin_seconds": margin, "limit_seconds": limit}
    if expected >= limit:
        result["error"] = "measured schedule does not satisfy the strict time limit"
    return result


def main() -> int:
    cpus = os.cpu_count() or 1
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    fingerprint = commands.add_parser("fingerprint")
    fingerprint.add_argument("--root", type=Path, default=ROOT)
    selected = commands.add_parser("estimate")
    selected.add_argument("--root", type=Path, default=ROOT)
    selected.add_argument("--manifest", type=Path, required=True)
    selected.add_argument("--report", type=Path, action="append", required=True)
    selected.add_argument("--workers", type=int, default=min(16, cpus))
    selected.add_argument("--build-slots", type=int, default=min(8, cpus))
    selected.add_argument("--guest-workers", type=int, default=min(4, max(1, cpus // 4)))
    selected.add_argument("--max-bytes", type=int, default=10 << 30)
    selected.add_argument("--limit", type=float, default=270)
    selected.add_argument("--margin", type=float, default=45)
    args = parser.parse_args()
    try:
        if args.action == "fingerprint":
            print(json.dumps({"source_sha256": source_fingerprint(args.root)}))
            return 0
        result = estimate(args.root, args.manifest, tuple(args.report),
                          Resources(args.workers, args.build_slots, args.guest_workers, args.max_bytes), args.limit, args.margin)
        print(json.dumps(result))
        return 0 if result["status"] == "pass" else 1
    except (OSError, ValueError, RecursionError) as error:
        print(json.dumps({"status": "fail", "error": str(error), "expected_seconds": None,
                          "measured_groups": getattr(error, "measured", 0),
                          "unmeasured": list(getattr(error, "unmeasured", ()))}))
        return 1


if __name__ == "__main__":
    sys.exit(main())
