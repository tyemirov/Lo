# AGENTS.GO.md

## Scope

This file gives backend rules for Go code. Obey root `AGENTS.md` and `.mprlab/POLICY.md` for shared workflow rules.

## Core Principles

- Reuse existing code first.
- Prefer data structures, registries, and cohesive types instead of branching logic.
- Inject external effects: I/O, network, time, randomness, and OS state.
- Keep core logic pure where practical.
- Accept domain types instead of loose primitives when invariants exist.
- Return errors and wrap them with context.
- Keep public API surface minimal.

## Code Style

- Use descriptive identifiers. No single-letter names except conventional tiny scopes where the repo already allows them.
- Lift repeated string literals into constants.
- Use GoDoc for exported identifiers.
- No panics in library code.
- Use structured logging when the repo has a logger.
- Propagate `context.Context` through effectful boundaries.

## Temporary Diagnostic Programs

- Put diagnostic Go programs in `_scratch/<task-id>/` with one `main` package per directory.
- Obey [Temporary Task Files](POLICY.md#temporary-task-files) for ownership and removal.

## Testing

- Apply the test pyramid in `.mprlab/POLICY.md`.
- Use table-driven unit tests for business cases that share setup and differ in inputs or expected outcomes.
- Select one-off integration tests through HTTP, CLI, or public package entry points when the task needs evidence from real component interactions.
- Before tests, configure temporary directories and all Go caches inside Lo.
- Use `t.TempDir()` only when its resolved directory stays inside Lo.
- Verify that child processes preserve these directories and cannot select an external cache.
- If a target cannot obey the [repository boundary](POLICY.md#lo-repository-boundary), report the blocker without execution.

## Validation

Use `.mprlab/POLICY.md` for validation.

During the change, run the smallest Go target that validates the changed contract.

When these tools are part of the repository contract, `make lint` must include `go vet`, `staticcheck`, and `ineffassign`.

## Language Contract

- Use smart constructors returning `(Type, error)` when a type has invariants.
- Do not export invalid zero-values.
- Wrap errors with `%w`.
- `make lint` must include `go vet`, `staticcheck`, and `ineffassign` when those tools are part of the repo contract.
