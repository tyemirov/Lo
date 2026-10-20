# Native Lowering

I006 adds native lowering between the checked graph and LLVM.
The [runtime contract](native-runtime-contract.md) gives its ABI and platform requirements.
I006 passed its implementation checks on October 20, 2026.
The full runtime port belongs to I007 through I010.

## Public Build Boundary

Use this command to generate an LLVM module:

```bash
bin/lo build program.lo --emit llvm --target darwin/arm64 --output program.ll --format json
```

The other supported target is `linux/arm64`.
The command uses the checked Lo graph without Go source generation.
It does not invoke Go, LLVM, a linker, or an SDK.
Its receipt identifies the output as an LLVM module with the `lo-native-1` ABI.
Runtime definitions are necessary before the module can become an executable.

Keep the default executable build until I011 qualifies the replacement.
Do not treat module generation as standalone distribution acceptance.
The current LLVM emission route accepts the software build configuration.
It rejects Metal selection without an automatic substitution.

The build validates source and output identities before publication.
It rejects an output that refers to the source through a direct path, symlink, or hardlink.
It writes the module to a temporary file beside the destination.
It replaces the destination only after successful generation and cancellation checks.
The existing build timeout and artifact size limit also apply to module generation.

## Lowering Boundary

The lowering representation contains explicit control flow and runtime calls.
Its verifier checks the representation before the LLVM emitter consumes it.
The emitter consumes the verified representation without access to the original checked graph.
Inspection uses the checked graph and does not invoke native lowering.

The structural contract includes literals, references, projections, conversions, calls, matches, bindings, returns, and commits.
Flow calls keep parameter order, handler identity, and source context.
Built-in calls keep result types, argument positions, labels, and callback identities.
Each call site has a unique metadata identity, including sites with the same source span.

Autodiff calls contain derivative plans and resource metadata.
The initial lowering route rejects these specialized calls explicitly.
I008 must implement their full contract before this route accepts them.
It must not treat an autodiff call as an ordinary catalog call.

## Execution Order

Keep the following order in generated control flow:

1. Charge one step before each reached statement.
2. Charge one step before each reached expression.
3. Evaluate child expressions in source order.
4. Place evaluated arguments in their declared parameter positions.
5. Execute the selected operation.
6. Charge the result's value weight after successful evaluation.

The step boundary checks cancellation before the remaining step budget.
Value-weight charges do not add cancellation boundaries.
A failed expression prevents its later value charge and all later effects.
The commit runtime operation owns its receipt charge.

A match evaluates its selector once.
It considers case heads and predicate callbacks in source order.
Only the selected case body executes.
Later heads, callbacks, and bodies do not execute after selection.

## Errors And Ownership

The ABI distinguishes successful results, recoverable failures, and terminal host failures.
A stored fallible Lo value is different from a propagated ABI failure.
Success and failure conversions construct values.
Propagation and flow exit determine whether a handler must execute.
An ordinary fallible flow returns its base value or error through the ABI status.
Its caller constructs one fallible value.
This return normalization adds no logical charge.
Top-level publication keeps its separate value contract.

Generated code validates a call's status before access to its payload.
An unknown status produces a terminal failure without access to the untrusted payload.

The flow body closes its resource scope before error framing and handler execution.
The cleanup must keep an owned reference to the returned value or error.
The flow remains active in the depth count while its handler executes.
Terminal host failures do not invoke Lo handlers.
Every completed path must release its temporary owned references.

The runtime owns list iteration and sequential fold behavior.
It must charge each iteration before callback entry.
It must keep callback order, error traces, completed-prefix receipts, and state publication.
It must retain emitted unfold values and release temporary storage at the current scope boundaries.
These obligations apply even when the emitter uses one runtime call for the processor.

Integer literals keep their full decimal representation.
Float literals keep their exact binary64 representation.
The emitter must not replace arbitrary-precision Lo arithmetic with fixed-width machine arithmetic.
Optimization must not remove, combine, or reorder observable runtime boundaries.

## Validation Scope

Public build tests include both target triples, source maps, unsupported inputs, cancellation, and output protection.
The LLVM execution harness links generated modules against a small test runtime.
It compares unoptimized and optimized execution with fixed expected results from the language contract.
Exact budget boundaries, branch order, callbacks, handlers, cancellation, and cleanup are part of I006 acceptance.

The test runtime is a fixture.
It does not implement the full Lo value model or catalog.
Full semantic migration acceptance is part of I007 through I011.
The installed offline compiler acceptance criterion is part of F017 and I011.

## Repeat The Execution Checks

The execution harness uses a Darwin arm64 host.
It generates and verifies LLVM modules and object files for both supported targets.
It links and executes the Darwin fixtures at O0 and O2.
Linux object generation does not prove Linux execution.

Prepare the pinned LLVM 21.1.8 tools inside the repository:

```bash
make prepare-native-lowering NATIVE_LOWERING_ROOT=_scratch/native-lowering
```

Preparation retrieves the archive identified by `experiments/native-backends/preparation.lock.json`.
It verifies the archive digest before extraction.
The resulting `tools.json` records the prepared tools and their digests.

Run the execution checks with a new output directory:

```bash
make test-native-lowering \
  NATIVE_LOWERING_ROOT=_scratch/native-lowering \
  NATIVE_LOWERING_STATE=_scratch/native-lowering/run-1
```

The harness rejects a nonempty output directory.
Its `results.json` records commands, source identities, generated artifacts, and fixture results.
Fixture expectations come from the language contract.
They do not come from the generated module or its execution output.

The C fixture runtime uses bounded integers for tests of the ABI and control flow.
It rejects arithmetic outside that range.
The production runtime must implement arbitrary-precision integers under I007.

## Recorded Results

The [execution receipt](evidence/native-lowering/execution.json) records the October 20, 2026 component run.
All 11 fixtures passed at O0 and O2.
The 44 object cells include both supported targets and their source maps.
The Darwin executables passed 132 budget, cancellation, and value-charge cases.
Two more cases verified rejection of an invalid status with an unusable payload pointer.
Every executed case ended with zero live fixture values, resource scopes, and active flows.

The fixtures include argument order, lazy matches, stored failures, handlers, callbacks, folds, and successful fallible-flow returns.
The public build tests also passed with an empty tool search path.
The [validation receipt](evidence/native-lowering/validation.json) records package tests, harness tests, CI, review, and source identities.
