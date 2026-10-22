# Semantic Migration Coverage

I005 defines the observable behavior that the replacement compiler must keep.
The [matrix](../testdata/semantic-migration/matrix.json) records each current operation and public CLI contract.
It also records evaluation order, effects, failures, execution limits, cleanup, and numeric profiles.
All executable builds use LLVM with the native Rust runtime.
The compiler no longer generates Go source.
The corpus report and installed offline checks have different acceptance requirements.

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

Three full integration logs are necessary for the [report command](../tools/semanticcoverage/report.py).
One log records the interpreter lane.
Two logs record LLVM execution on `darwin/arm64` and `linux/arm64`.
Each matrix reference has a required native equivalent.
Controlled runtime tests state their actual execution boundary independently.

Each run records its backend, implementation, target, compiler digest, source identity, and matrix identity.
Each LLVM run also records its toolchain manifest digest and selected runtime archive digest.
The report checks these digests against the current source files and installed package.

The source identity includes implementation files, tests, corpus inputs, independent expectations, and build inputs.
The report rejects stale identities, failed packages, absent references, skipped references, and conflicting results.
A successful test name alone does not verify execution through a different implementation.
The matrix identifies interpreter tests, native LLVM tests, and native ABI tests independently.
The `native_tests` map assigns native tests to each required reference.

Each recorded gap has an owner and an acceptance class:

| Class | Report behavior |
| --- | --- |
| `required` | An unresolved I005 coverage requirement prevents success. |
| `disclosed_limit` | The report keeps a stated limit within the reviewed I005 scope. |
| `downstream` | The report keeps acceptance work owned by a later issue. |

The classification does not remove a requirement or verify unmeasured behavior.
Linux execution of the full corpus is part of I011.
F017 owns [installed offline compiler acceptance](compiler-distribution.md).
I011 retains full corpus qualification with that distribution.
The matrix also identifies limits in error-kind combinations.
Native ABI fixtures cover the controlled device contracts on both targets.

## Reproduction

Run these commands from the repository root on Darwin arm64.
Prepare the installed packages before corpus execution.
Keep the source files and packages unchanged between both target runs.

1. Run the Linux corpus in the software guest:

   ```sh
   make test-native-linux </dev/null
   ```

2. Run the interpreter and Darwin LLVM corpus with the Linux log:

   ```sh
   make test-semantic-migration \
     SEMANTIC_ARGS='--llvm-log linux/arm64=_scratch/native-qualification/linux/semantic-linux-arm64.jsonl' </dev/null
   ```

3. If a command fails, read its diagnostic and input logs.
4. Correct the failed requirement before another acceptance run.
5. Keep the report and all input logs with the implementation evidence.

The default installed compiler is `_scratch/compiler-distribution/packages/lo-darwin-arm64-software/bin/lo`.
Set `SEMANTIC_COMPILER` and `SEMANTIC_TOOLCHAIN` to select another prepared Darwin installation.
Set `COMPILER_DISTRIBUTION_OUTPUT` to select the package directory for the Linux run.
Set `NATIVE_QUALIFICATION_OUTPUT` to select a new guest output directory.
Use its `linux/semantic-linux-arm64.jsonl` path in `--llvm-log linux/arm64=PATH`.

The report is `_scratch/build-state/semantic/semantic-coverage.json` by default.
Set `SEMANTIC_OUTPUT` to select another report directory.

The Make targets keep caches, temporary files, and output inside Lo.
They close standard input for each child command.
The host builds the Linux test executable and fixtures before guest execution.
The guest uses the installed compiler without Go or external network access.
Its guest export channel is isolated from the kernel console.
The receiver rejects incomplete or malformed result records.
Tool preparation has its own scope from validation.
The report contains source digests, log digests, test results, and all disclosed limits.
`--allow-partial` creates a diagnostic report when a target log is absent.
A partial report does not satisfy I011 acceptance.

## Earlier Evidence And Current Status

The earlier I005 migration run passed on October 19, 2026.
Its native lane used Go-generated executables.
All 385 required references passed in both execution lanes.
Each lane recorded 993 successful test completions, including both mandatory coverage gates.
The 13 reporter and runner regression tests also passed.

The [validation receipt](evidence/semantic-migration/validation.json) records the source identity, matrix identity, log digests, and retained limits.
The [coverage report](evidence/semantic-migration/report.json) records each required result.
Compressed full logs and the matrix snapshot are in the same evidence directory.
The [final repository validation](evidence/compiler-evolution/validation.json) also passed.
I005 passed its acceptance checks with the stated coverage limits.

I006 adds LLVM module emission tests to the current matrix.
The October 19 receipt remains the I005 acceptance snapshot.
The [native lowering receipt](evidence/native-lowering/validation.json) records the I006 checks and their execution limits.

I011 passed the current LLVM corpus, offline, component, and CI checks.
The [current receipt](evidence/standalone-compiler/nonperformance-validation.json) records 394 required references with no qualification gaps in three lanes.
It identifies the current source and installed packages.
Performance qualification and final review remain pending.
The earlier receipts remain historical evidence.
