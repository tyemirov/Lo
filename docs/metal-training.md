# Metal Training Acceptance

I002 qualifies complete training with the explicit `metal:0` backend.
The [maintained example](../examples/training_metal.lo) uses the fixed network and SGD procedure from I001.
I002 does not depend on the pending NVIDIA work in I001, F009, or F010.
Software execution and controlled failures provide acceptance without a physical-device requirement.
Local Metal execution supplies separate evidence for the compiled kernels and native artifacts.

## Fixture and Numeric Contract

The network has two inputs, two ReLU units, and one linear output.
Its input rows are `[-1,-1]`, `[-1,1]`, `[1,-1]`, and `[1,1]`.
The targets are `0`, `2`, `1`, and `3`.
The initial hidden weight matrix is the identity, and both hidden biases are zero.
The output weights are `[0.25,0.25]`, and the output bias is zero.
The loss is mean squared error over all four rows.

`List.fold` applies 200 immutable SGD updates with learning rate `0.05`.
Inputs, targets, and the four Parameter tensors each transfer to Metal once.
The initial loss transfers to CPU before the fold.
The final loss and four final Parameter tensors transfer to CPU after the fold.
The forward graph, reverse graph, and optimizer state stay on Metal between these explicit transfers.

The [Metal arithmetic contract](metal-arithmetic.md) defines `metal-fp32`.
The [independent reference](evidence/training-reference.py) calculates direct network derivatives and two AdamW updates without Lo or a tensor framework.
Its [recorded results](evidence/training-reference-results.json) contain separate binary64 and FP32 calculations.
A new execution of that reference reproduced the recorded file exactly.

The tests use `abs(actual - expected) <= atol + rtol * abs(expected)`.
The tolerances below existed before I002 qualification.
They apply to these fixed fixtures only.

| Comparison | Absolute tolerance | Relative tolerance |
| --- | --- | --- |
| Final training loss, as used by F013 | `1e-8` | `1e-5` |
| Final Parameters and first SGD update, as used by F013 | `1e-6` | `1e-5` |
| Initial analytical gradients | `1e-6` | `1e-5` |
| CPU and Metal loss and gradients at each step | `1e-5` | `1e-4` |
| F007 AdamW updates | `1e-6` | `1e-5` |
| Initial loss, metadata, transfer counts, and repeated results within one profile | Exact | Exact |

Each full training run must also finish with finite loss below `0.001`.
The three runs in each fixed execution environment must return identical numeric bits.
The independent executor models the Metal descriptors and FP32 arithmetic directly.
It does not use the CPU tensor evaluator as its arithmetic oracle.

## Acceptance Coverage

The [training tests](../tests/integration/metal_training_test.go) use the public compiler, CLI runner, and resource ledger.
The standard CLI tests cover `parse`, `check`, `inspect`, denied grants, and unavailable Metal execution.
Routine native tests repeat the CLI admission checks through software artifacts.
The local tests execute the example through both the Metal CLI and generated native artifacts.

| Acceptance criteria | Required result |
| --- | --- |
| Initial loss | Exactly `2.46875` |
| Initial gradients and first SGD update | All nine coordinates agree with both independent references |
| Full training | 200 updates, finite final loss below `0.001`, and all nine final Parameters agree with both references |
| Diagnostic execution | All 200 losses and 1,800 gradient coordinates are finite and agree with the CPU profile |
| Transfers in the maintained example | Six uploads and six downloads, with no transfer inside the fold |
| Storage in the maintained example | Peak accounted storage at most 2,048 bytes, and every allocation released once |
| Repeated execution | Three identical results for the independent executor, local Metal interpreter, and local Metal native artifact |
| AdamW | Three repeated two-step runs, nonzero decay, a zero-gradient coordinate, unchanged original Parameters, and step count two |
| Failure at step 101 | Correct error or host diagnostic, source trace where applicable, no final result, no later commit, and complete cleanup |

The separate diagnostic fixture explicitly downloads gradients and losses at every step.
Those downloads do not belong to the six-upload, six-download acceptance measurement.
The diagnostic fixture uses the same input construction and `Loss` graph as the maintained example.
The native diagnostic execution must match the Metal interpreter exactly.

The AdamW fixture starts with `[1,-2]` and uses gradients `[0.1,-0.2]` and `[0,0.3]`.
Its config has rate `0.01`, betas `0.9` and `0.999`, epsilon `1e-8`, and decay `0.1`.
The first expected update is `[0.9890000224113464,-1.9880000352859497]`.
The second expected update is `[0.9813104271888733,-1.988489031791687]`.
The [optimizer tests](../tests/integration/metal_compute_test.go) preserve the F007 fixture and its fixed tolerances.

Controlled backend failures occur during reverse execution at step 101.
The tests cover `Numeric.NonFinite`, `Device.ExecutionFailed`, `Device.Lost`, and `runtime.cancelled`.
Only the six initial uploads and initial loss download occur before these failures.
Related Metal tests cover unsupported operations and profiles, allocation limits, unavailable devices, and numeric failures before file commits.

## Reproduction

Run software acceptance without a Metal build:

```bash
python3 docs/evidence/training-reference.py
make test-integration TEST_ARGS='-run "TestMetalTraining|TestMetalCompute" -count=1 -race -v'
make test-native TEST_ARGS='-run TestMetalTrainingCLI -count=1 -v'
make ci
```

For optional local Metal execution, use the [pinned toolchain](metal-transfers.md):

```bash
make test-metal TEST_ARGS='-count=1 -race -v'
make test-native-metal TEST_ARGS='-count=1 -race -v'
./bin/lo-metal parse examples/training_metal.lo --format json
./bin/lo-metal check examples/training_metal.lo --format json
./bin/lo-metal inspect examples/training_metal.lo --format json
./bin/lo-metal run examples/training_metal.lo --allow-device metal:0 --format json
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo-metal build examples/training_metal.lo --device-backend metal --output ./bin/metal-training
./bin/metal-training run --allow-device metal:0 --format json
./bin/metal-training metadata
```

An unavailable local toolchain stops the requested Metal target with a diagnostic.
Software acceptance remains sufficient under repository policy.
When only software results are available, actual Metal kernel behavior and GPU performance remain unmeasured.

## Recorded Results

The October 16, 2026 qualification uses base revision `f9c023271e7ce6a90239f8cb91a6782d27f8c3d4` plus the I002 changes.
The [initial CLI failure](evidence/metal-training-red.txt) records the absent maintained example before implementation.
The [toolchain rejection](evidence/metal-training-toolchain-red.txt) records the installed Xcode version change before the pin update.
I002 selects Xcode 27.1 build 27A9275 as the current exact pin.
The macOS SDK, Clang, Metal compiler, math flags, and compiled kernel digest remain unchanged from F014.

The [software results](evidence/metal-training-software.txt) passed with the Go race detector.
The independent executor completed three runs with identical results and complete cleanup.
The separate gradient execution verified every step against the CPU profile.
The injected failures at step 101 prevented final publication and the later file commit.

The [local Metal results](evidence/metal-training-local.txt) passed with the Go race detector.
The environment used Go 1.27.1, Apple M5 Pro, and macOS 27.0.1 build 26A434.
The device registry identifier was `100000696`.
The kernel SHA-256 was `d2a55ee54a4b38e2bb43cb363f5f985c42fec15be7a11ead67cd44036f3b7dd6`.
Three CLI executions returned identical results.
The observed training runs matched the software results below.

The [native suite](evidence/metal-training-native.txt) and [final training check](evidence/metal-training-native-final.txt) passed with the Go race detector.
Three native runs returned the same final loss and Parameter values as the interpreter.
The native diagnostic run matched all 200 losses and 1,800 gradient coordinates exactly.
Small execution budgets produced the same host diagnostics in interpreter and native execution.

The [compiler receipt](evidence/metal-training-build.json), [runtime manifest](evidence/metal-training-manifest.json), and [artifact metadata](evidence/metal-training-artifact.json) identify the build inputs.
The [native result](evidence/metal-training-result.json) records the complete returned value.
The [qualification identity](evidence/metal-training-identity.json) records source and artifact digests.

| Observation | Software executor | Local Metal interpreter |
| --- | --- | --- |
| Initial loss | `2.46875` | `2.46875` |
| Final loss | `0.00013215476064942777` | `0.00013215476064942777` |
| Uploads / downloads | `6 / 6` | `6 / 6` |
| Computation commands | `8,216` | `8,216` |
| Peak accounted storage | `1,312` bytes | `1,312` bytes |
| Cleanup | Complete | Complete |

Accounted storage includes device buffers, transfer staging, and execution status storage.
It excludes Go heap storage and driver allocations outside the resource ledger.
These results establish correctness for this fixed fixture and recorded identity.
They do not establish a speed threshold or a general numerical error bound.
GPU performance, other GPU identities, and other toolchain profiles remain unmeasured.

The [final CI record](evidence/metal-training-ci.txt) passed runtime bundle verification, formatting, static analysis, software tests, native tests, and compiler construction.
The [documentation review](evidence/metal-training-review.json) records the changed prose, local links, issue identifiers, and preserved findings.
