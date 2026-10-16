# Metal Arithmetic

F013 defines the `metal-fp32` profile for synchronous tensor computation on `metal:0`.
F014 owns native Metal artifacts. I002 owns complete training acceptance.

## Build and Execute

The [Metal build contract](metal-transfers.md) pins the host, toolchain, compiler flags, and device features.
The [example](../examples/metal_arithmetic.lo) differentiates a quadratic loss and applies one SGD update.
It returns the original `[1,-2]`, updated values near `[0.8,-1.6]`, and the initial loss `5`.

```bash
make build-metal
./bin/lo-metal check examples/metal_arithmetic.lo
./bin/lo-metal inspect examples/metal_arithmetic.lo --format json
./bin/lo-metal run examples/metal_arithmetic.lo --allow-device metal:0 --format json
make test-metal TEST_ARGS='-count=1 -race -v'
```

## Operations and Resources

The backend implements addition, subtraction, multiplication, division, square root, ReLU, broadcasting, reshape, transpose, sum, mean, and matrix multiplication.
It also implements the current VJPs, stop-gradient, shared gradient contributions, gradient lifecycle operations, SGD, and AdamW.
Parameters, gradients, moments, beta powers, and internal constants stay on Metal between explicit transfers.
Earlier tensors and optimizer states retain their values.

The interpreter and generated runtime use the same typed operation descriptors and shape planner.
Unsupported operations and profiles fail before output allocation or command submission.
Each submitted operation uses one synchronous command.
The command completes before dependent computation, publication, or a subsequent observable effect.
Reshape and empty-axis reductions share their original backing allocation.
Tape accounting counts that allocation once across its views.

Each arithmetic command uses a status buffer charged to the workspace and total storage budgets.
The status contains only a failure category and row-major output index.
The kernel uses integer atomics to select the earliest failing output element.
Tensor values never enter a CPU computation path through this status.
Empty tensor results require no command or status buffer.
Square-root derivative validation uses a status command without a result tensor.
The resource ledger includes transfer staging and all actual device reservations.

Command construction failures produce `Device.ExecutionFailed`.
A failed submitted command produces `Device.Lost` and prevents further device resource access.
Numeric failures preserve their existing error kinds, originating operations, and source traces.
Cancellation remains a host diagnostic, including when it coincides with a backend failure.
Failed operations publish no partial result and prevent subsequent commits.
Inspection reports `metal-fp32`, synchronous execution, and possible backend failures without device initialization.

## Numeric Rules

Arithmetic replaces each subnormal operand and each signed zero operand with positive zero.
Arithmetic also replaces each subnormal result and each signed zero result with positive zero.
This rule applies at every primitive multiplication, addition, subtraction, division, and square root.
Reductions apply the rule at each accumulation step.
ReLU returns positive zero for nonpositive inputs.
The ReLU derivative uses normalized inputs and gradient values.
Division rejects a zero divisor after normalization.
Square root rejects a negative input after normalization.
An active square-root derivative rejects a zero input after normalization.
Every arithmetic intermediate must remain finite.

Transfers, reshape, transpose, broadcast, and constant construction preserve FP32 bits.
An empty-axis reduction is an immutable view and preserves its input bits.
Other reductions start with positive zero and visit contributing elements in increasing row-major order.
Matrix multiplication visits the contracted dimension in increasing order and rounds each product before accumulation.
The build disables contraction and uses safe math with precise functions.

Apple permits subnormal flushing and leaves its zero sign undefined in the [MSL specification](https://developer.apple.com/metal/Metal-Shading-Language-Specification.pdf), section 8.5.
The explicit normalization rule removes that zero-sign variation from this profile.
The bounded preparation probe passed on the Apple M5 Pro before production kernel implementation.
Exact fixtures cover signed zeros, positive and negative subnormals, underflow, division, and square root.

Each fixed build and device identity must produce identical numeric bits across three executions with identical inputs.
Identity includes Lo sources, kernels, compiler options, SDK, macOS build, and GPU identity.
Comparisons with other numeric profiles use the existing fixture tolerances.
These fixture tolerances do not define a universal error bound for arbitrary programs.

## Validation

The [initial failure record](evidence/metal-arithmetic-red.txt) shows public arithmetic and derivative tests failing before implementation.
The [preparation probe](evidence/metal-arithmetic-probe.txt) records the selected zero and subnormal behavior on the Apple M5 Pro.
The [software record](evidence/metal-arithmetic-software.txt) covers the independent executor through the compiler, evaluator, CLI runner, and resource ledger.
The executor does not use Lo's CPU tensor evaluator as its arithmetic oracle.
Software acceptance requires no GPU access.

The [local Metal record](evidence/metal-arithmetic-local.txt) covers compiled kernels, the race detector, and public CLI execution.
Primitive, VJP, gradient, SGD, and AdamW fixtures passed with their existing tolerances.
Exact fixtures passed for zero normalization, subnormal arithmetic, bit-preserving layouts, and fixed reduction order.
Repeated primitive, optimizer, profile, and training executions produced identical bits within each execution environment.

The 200-step SGD fixture reached loss `0.00013215476064942777` and matched all nine reference Parameter values.
Each run used six uploads, six downloads, 8,216 computation commands, and 1,312 peak accounted bytes.
The software and local Metal environments each completed three runs with these results and complete cleanup.
The fixture checks training arithmetic and resource lifetime.
F014 implements [native artifacts](native-metal.md), and I002 supplies [complete training acceptance](metal-training.md).

Controlled failures verified unsupported profiles and operations, allocation failures, workspace limits, command failures, cancellation, and device loss.
They also verified reverse-mode source traces, blocked later commits, and rejection of subsequent device work after session loss.
A view regression verifies that tape accounting counts shared backing storage once.
The [build receipt](evidence/metal-arithmetic-build.json) identifies the tested sources, toolchain, kernels, and executable.
The [Metal static-analysis record](evidence/metal-arithmetic-vet.txt) records the tagged Go check.
The [final CI record](evidence/metal-arithmetic-ci.txt) records successful formatting, static checks, software tests, native regression tests, and the standard build.
These checks do not establish a performance threshold.
