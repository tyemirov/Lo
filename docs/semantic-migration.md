# Semantic Migration Coverage

I005 defines the observable behavior that the replacement compiler must keep.
The [matrix](../testdata/semantic-migration/matrix.json) records each current operation and public CLI contract.
It also records evaluation order, effects, failures, execution limits, cleanup, and numeric profiles.
The current compiler still generates Go source.
These tests do not verify standalone compiler acceptance.

## Coverage Contract

Each operation has positive and negative test references.
Each fallible operation also has an applicable runtime rejection or an explicit exception.
Each reference identifies its execution boundary and independent expectation.
The inventory test compares operation names, capabilities, and error kinds with the current catalog.
It compares CLI contracts with the current command definitions.

The fixtures include specified checkpoint bytes and independently specified optimizer states.
They verify resumed execution, ordered reads, explicit commits, and prevention of later effects after failure.
They also verify execution limits, cancellation, and resource cleanup.
CPU and Metal profiles keep their different numeric rules.
Software device implementations supply controlled device conditions.

The `Optimizer.SGD.State.new` exception applies only to checked canonical inputs.
The current constructor uses existing Parameters, validated configuration, and a zero step count.
It does not execute arithmetic or a device operation that can produce its declared domain errors.
The verifier rejection is part of its negative coverage.
Other operations must still include applicable runtime failures.

## Evidence Contract

The [report command](../tools/semanticcoverage/report.py) consumes two full integration logs.
One log records the interpreter lane.
The other log records the current native lane.
The native lane uses Go-generated executables.
Controlled runtime tests state their actual execution boundary independently.

Each run emits a source identity and matrix identity.
The source identity includes implementation files, tests, corpus inputs, independent expectations, and build inputs.
The report rejects stale identities, failed packages, absent references, skipped references, and conflicting results.
A successful test name alone does not verify execution through a different implementation.
The matrix records shared runtime tests and generated-native tests independently.

Each recorded gap has an owner and an acceptance class:

| Class | Report behavior |
| --- | --- |
| `required` | An unresolved I005 coverage requirement prevents success. |
| `disclosed_limit` | The report keeps a stated limit within the reviewed I005 scope. |
| `downstream` | The report keeps acceptance work owned by a later issue. |

The classification does not remove a requirement or verify unmeasured behavior.
Linux execution of the full corpus is part of I011.
Clean offline compiler acceptance is part of F017 and I011.
The matrix also identifies limits in error-kind combinations and controlled device variants.

## Reproduction

1. Run `make test-semantic-migration </dev/null` from the repository root.
2. If the command fails, read its diagnostic and the two integration logs.
3. Resolve a failed requirement before another acceptance run.
4. Read `_scratch/build-state/semantic-coverage.json` after a successful run.
5. Keep the report and its input logs with the implementation evidence.

The Make target uses repository-local caches, temporary files, and output.
It closes standard input for each command.
Tool preparation has its own scope from validation.
The report contains source digests, log digests, test results, and all disclosed limits.

## Current Status

The source-bound migration run passed on October 19, 2026.
All 385 required references passed in both execution lanes.
Each lane recorded 993 successful test completions, including both mandatory coverage gates.
The 13 reporter and runner regression tests also passed.

The [validation receipt](evidence/semantic-migration/validation.json) records the source identity, matrix identity, log digests, and retained limits.
The [coverage report](evidence/semantic-migration/report.json) records each required result.
Compressed full logs and the matrix snapshot are in the same evidence directory.
The [final repository validation](evidence/compiler-evolution/validation.json) also passed.
I005 passed its acceptance checks with the stated coverage limits.
