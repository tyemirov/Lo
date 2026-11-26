# AGENTS.PY.md

## Scope

This file gives backend rules for Python code. Obey root `AGENTS.md` and `.mprlab/POLICY.md` for shared workflow rules.

## Core Principles

- Reuse existing modules first.
- Prefer data-driven registries and explicit domain types instead of branching.
- Use `@dataclass(frozen=True)` or Pydantic when already in use for validated domain values.
- Keep logic small, typed, and testable through public entry points.
- Inject files, network, randomness, time, and environment access.
- Validate at CLI, HTTP, file, and adapter edges.

## Code Style

- Use type hints.
- Use descriptive identifiers.
- Lift repeated literals into constants.
- Use module, class, and function docstrings where they clarify public behavior.
- Use `logging`. Do not leave stray `print` calls in libraries.
- Raise explicit exceptions for domain validation failures.

## Testing

- Apply the test pyramid in `.mprlab/POLICY.md`.
- Use pytest for unit tests, integration tests, and end-to-end tests.
- Use fixtures for reusable test doubles and `tmp_path` for tests that need filesystem access.
- Select a one-off check through the CLI, HTTP, or public package entry point when the task needs evidence from real component interactions.

## Validation

Use `.mprlab/POLICY.md` for validation.

During the change, run the smallest Python target that validates the changed contract.

When the repository contract includes it, lint must run mypy or pyright for typed Python surfaces.

## Language Contract

- Use `@dataclass(frozen=True)` or Pydantic when already in use.
- Validate in constructors or edge adapters.
- Use type hints throughout.
