# Reusable Trained Models

P003 defines this contract.
F015 implements Parameter checkpoints.
F016 adds complete optimizer state and explicit state transfers.
I003 qualifies the CPU and Metal workflows with XOR.
This work takes priority over the independent F009, F010, and I001 NVIDIA sequence.

## Scope

A trained model consists of checked Lo inference code and named Parameters.
A checkpoint contains data only.
The inference code supplies the network operations and expected Parameter shapes.
The loader does not execute checkpoint content.

The first workflow trains XOR, writes its Parameters, and loads them in a separate inference process.
The second workflow stops after 250 SGD updates, writes complete state, and resumes for 250 updates.
The resumed result must equal an uninterrupted 500-step result within the same numeric profile.
An independent AdamW case must keep moments, beta powers, configuration, and step count.

## Public Operations

The signatures below use `T` for one supported checkpoint type.
`T` is `Parameters<R>`, `Optimizer.SGD.State<R>`, or `Optimizer.AdamW.State<R>`.
`R` is a nonempty exact record of FP32 tensors.
These signatures describe compiler rules and do not add user-defined generic syntax.

```text
Checkpoint.encode(value: T) -> Bytes!
Checkpoint.decode(data: Bytes, template: T) -> T!
Parameters.to(parameters: Parameters<R>, device: Device) -> Parameters<R>!
Optimizer.SGD.State.to(state: Optimizer.SGD.State<R>, device: Device) -> Optimizer.SGD.State<R>!
Optimizer.AdamW.State.to(state: Optimizer.AdamW.State<R>, device: Device) -> Optimizer.AdamW.State<R>!
```

`Checkpoint.encode` requires CPU tensors.
`Checkpoint.decode` requires a CPU checkpoint template and returns CPU tensors.
The template supplies the exact type, Parameter names, and tensor shapes.
Template numeric values and optimizer configuration do not replace checkpoint values.
The loader restores the checkpoint configuration and validates it through the current constructors.
The loader rejects a different optimizer kind.

The transfer operations move every tensor in the selected container.
AdamW transfers include both moments and both scalar beta powers.
The operations keep configuration, step count, field names, shapes, and FP32 bit patterns.
They reuse `Tensor.to` authorization, accounting, cancellation, and failure behavior.
Same-device transfers keep the existing storage.
Transfers do not publish a partial container after failure.

Encoding and decoding are pure operations with explicit fallibility.
They do not access files or cause implicit device transfers.
They are outside the differentiable operation subset.

File access uses the current `File.read`, `File.write`, and `commit` contract.
Reads require the selected read root.
Writes require the selected write root and explicit commit activation.
The checkpoint operations do not add another file adapter or grant.

## Canonical Bytes

The checkpoint format is the existing typed JSON value representation, encoded as UTF-8 bytes.
The root value is one supported checkpoint type, without a CLI result envelope.
Encoding uses compact JSON with the current canonical field order and numeric strings.
The current format is the only accepted format.
There are no aliases, migrations, or alternative readers.

Every tensor records `dtype`, `shape`, `device`, and row-major `values`.
The dtype is `fp32` and the device is `cpu`.
Shapes use decimal integer strings.
Tensor elements use shortest round-trip FP32 strings, including signed zero.

Optimizer configuration keeps its validated FP32-rounded Float values.
SGD state includes Parameters, configuration, and a nonnegative step count.
AdamW state also includes both moments and both beta powers.

The decoder checks exact types, exact fields, dimensions, element counts, and finite values.
It rejects unknown fields, duplicate keys, missing fields, extra values, trailing content, and noncanonical encodings.
It rejects invalid configuration, negative steps, negative second moments, and beta powers outside `[0,1]`.
The decoded value must encode to the same bytes before publication.
This equality check rejects alternative encodings without a second accepted representation.

The format does not contain executable code, source paths, device handles, or host authorization.
It does not contain a compatibility version.
It keeps current data without a promise to read future or obsolete schemas.

## Limits and Failures

The encoded checkpoint cannot exceed the existing 16 MiB byte limit.
Decoding obeys the existing tensor shape, value weight, allocation, and evaluation limits.
Checkpoint work consumes the execution budget.
Cancellation is a host diagnostic and bypasses Lo handlers.
Device denial also is a host diagnostic.

`Checkpoint.InvalidData` reports invalid or noncanonical bytes.
`Checkpoint.SchemaMismatch` reports a different type, field set, or tensor shape.
`Tensor.HostObservationRequired` reports a checkpoint operation with device tensors.
Device transfers keep the current device error kinds and originating operation context.
File operations keep their existing file errors.
Any unresolved failure prevents a later commit.
Temporary decoded or transferred storage is released after failure or cancellation.

## Compiler and Runtime Changes

| Component | Change |
| --- | --- |
| `internal/catalog` | Add checkpoint signatures, transfer signatures, error kinds, and device classification. |
| `internal/check` | Infer exact checkpoint output types from the template and reject unsupported values. |
| `internal/runtimeops` | Add the bounded codec and container transfers through the current tensor runtime. |
| `internal/eval` | Dispatch the new checked operations. |
| `internal/codegen` | Emit the same runtime calls for native artifacts. |
| `internal/inspect` and `internal/tensorfacts` | Keep shapes and report transfers, CPU decode results, and possible errors. |
| `internal/runtimebundle` | Regenerate the native runtime bundle from the selected source. |
| `tests/integration` | Exercise independent processes, malformed data, failures, and CPU/Metal parity. |

The implementation reuses current value constructors and optimizer invariants.
It does not add a second optimizer implementation.
It does not import arbitrary runtime values.

## Acceptance

1. Add public CLI tests before implementation.
2. Record the expected missing-operation failure.
3. Verify Parameter save/load and inference in separate processes.
4. Compare 250-plus-250 SGD updates with 500 uninterrupted updates.
5. Verify AdamW state restoration with independent expected transitions.
6. Repeat the selected workflows on CPU and the controlled Metal executor.
7. Verify explicit transfers, bounded storage, and cleanup after success and injected failures.
8. Verify authorization, malformed data, shape mismatch, cancellation, and prevention of later commits.
9. Verify parse, check, inspect, interpreter execution, and native execution.
10. Record available local Metal execution separately from software acceptance.
11. Run final `make ci` after the complete implementation stack.

Software acceptance has no physical-device prerequisite.
Cross-backend loading keeps stored FP32 values through explicit transfers.
Exact resumed arithmetic is guaranteed only within a fixed numeric profile.

## Deferred Work

Random initialization, dataset ingestion, classification losses, and MNIST are separate work.
Checkpoint compression, streaming models larger than the current limits, and external model formats are also deferred.
The current file-write contract does not promise crash-atomic checkpoint replacement.

## Execution

From the repository root, build the compiler and execute the CPU workflow:

```bash
make build
./bin/lo run examples/xor_save_cpu.lo --write-root ./bin --format json
./bin/lo run examples/xor_resume_cpu.lo --read-root ./bin --write-root ./bin --format json
./bin/lo run examples/xor_infer_cpu.lo --read-root ./bin --format json
```

The [save example](../examples/xor_save_cpu.lo) writes `bin/xor-state.json` after 250 updates.
The [resume example](../examples/xor_resume_cpu.lo) restores that state and completes the next 250 updates.
It writes the final Parameters to `bin/xor-parameters.json`.
The [inference example](../examples/xor_infer_cpu.lo) loads those Parameters and returns predictions without training.
Each command starts a separate process.
The JSON checkpoint files contain data, and `File.write` replaces their previous contents.

For the explicit Metal workflow, use these commands:

```bash
make build-metal
./bin/lo-metal run examples/xor_save_metal.lo --allow-device metal:0 --write-root ./bin --format json
./bin/lo-metal run examples/xor_resume_metal.lo --allow-device metal:0 --read-root ./bin --write-root ./bin --format json
./bin/lo-metal run examples/xor_infer_metal.lo --allow-device metal:0 --read-root ./bin --format json
```

The Metal examples use the same checkpoint bytes as the CPU examples.
An explicit state transfer moves the loaded CPU tensors to `metal:0`.
Each training process uses six uploads and six downloads.
Inference uses five uploads and one download.
The training loops contain no host transfers.

Native artifacts use the same operations and execution grants:

```bash
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo build examples/xor_infer_cpu.lo --output ./bin/xor-infer
./bin/xor-infer run --read-root ./bin --format json
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo-metal build examples/xor_infer_metal.lo --device-backend metal --output ./bin/xor-infer-metal
./bin/xor-infer-metal run --allow-device metal:0 --read-root ./bin --format json
```

## Open Decisions

There are no open decisions for this implementation scope.

## Validation Evidence

The [initial failure](evidence/checkpoint-red.txt) records the missing transfer operations before implementation.
The [software tests](evidence/checkpoint-software.txt) cover typed imports, invalid data, optimizer state, resource limits, and controlled Metal execution.
The [native tests](evidence/checkpoint-native.txt) cover fresh-process imports and the XOR workflow through standalone CPU artifacts.
The [local Metal tests](evidence/checkpoint-metal.txt) record three interpreter workflows and three native workflows with the Go race detector.
Each local workflow includes XOR resumption and inference, plus complete AdamW resumption.
The [Metal build receipt](evidence/checkpoint-metal-build.json) records the toolchain, kernel digest, and binary digest.
The [execution identity](evidence/checkpoint-identity.json) records the checkpoint source digests and XOR results.

The [final CI result](evidence/checkpoint-ci.txt) records passing software tests, native tests, static checks, and the compiler build.
The [documentation review](evidence/checkpoint-review.json) records the changed prose, issue references, and local link checks.

The resumed XOR result equals the uninterrupted 500-step result in the tested CPU and Metal profiles.
The final mean squared error is approximately `4.25e-13`.
Predictions are approximately `[0.0000006641, 0.9999993, 0.9999995, 0.0000007215]`.
All four threshold classifications are correct.
The software Metal executor reports six uploads and six downloads for each training process, with complete allocation cleanup.
