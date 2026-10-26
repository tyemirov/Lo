# Lo Neural Network Training Extension

P001 selects the implementation contract for dense FP32 training.
Named flows describe the forward pass and loss.
Reverse-mode autodiff produces gradients from the checked graph.
`List.fold` carries immutable Parameters and optimizer state through a bounded number of updates.

Status: design completed on October 13, 2026.
F002 through F011 and I001 own implementation and acceptance.

F002, F003, and F004 implement Float arithmetic and CPU tensors through `lo run`.
The compiler also implements Device descriptors without device initialization.
F005 implements first-order CPU autodiff and derivative plans in inspection reports.
F006 and F007 implement Parameters, gradients, stop-gradient, SGD, and AdamW.
F001 implements the CPU training operations in standalone native executables.

F008 implements device grants and explicit transfers.
F012 through F014 implement Metal transfers, arithmetic, training, and native artifacts.
F015 and F016 implement checkpoints and complete optimizer-state transfers.
The [v0.4 specification](../specification.md#version-and-scope) includes these implemented contracts.
Section 24 defines their current behavior.

NVIDIA arithmetic, deferred execution, mixed precision, and complete NVIDIA training acceptance are planned under F009, F010, F011, and I001.
The NVIDIA signatures and execution rules below describe selected future work.
The [backend support table](../specification.md#backend-support) records current execution support.

The [language specification](../specification.md) remains the authority for implemented language behavior.
The [source changes](#source-contract-changes) identify the changes required before each extension receives implementation acceptance.
The [compiler design](compiler-design.md) owns existing diagnostics, authorization, and execution rules.
The [native compilation plan](native-compilation-plan.md) defines the second execution backend for these CPU contracts.
The [native acceptance report](native-compilation-results.md) compares training results and performance on macOS arm64 and Linux arm64.

## Decision scope

The recovered training needs require numeric operations, tensors, gradients, explicit Parameters, and optimizers.
Device selection, CUDA execution, and asynchronous failure rules are new design decisions from P001.
This distinction does not change their implementation requirements.

| Decision | Selected contract | Origin | Issues |
| --- | --- | --- | --- |
| Numeric domains | Binary64 `Float` arithmetic and explicit FP32 tensor conversion. | Training need | F002 |
| Tensor values | `Tensor<FP32>`, dense row-major storage, immutable values, checked shape metadata. | Training need | F003, F004 |
| Differentiation | First-order reverse mode over a closed set of tensor operations. | Training need | F005 |
| Selected inputs | Exact records of tensors with explicit field names. | Training need | F005, F006 |
| Parameter identity | Field names identify Parameters within one validated schema. | Training need | F006 |
| Updates | Pure SGD and AdamW transitions with explicit state. | Training need | F007 |
| Iteration | Existing named callbacks and `List.fold`. | Existing foundation | I001 |
| Device effects | Resource boundaries separate from reads and commit activation. | New device decision | F008, F010 |
| NVIDIA route | Go interpreter, C ABI, cuBLAS GEMM, and precompiled CUDA kernels. | New device decision | F009 |
| Determinism | Exact results within a fixed numeric profile. Fixed fixture tolerances between profiles. | New device decision | F004, F009, F010, I001 |
| Validation | Independent arithmetic, real CLI execution, software backend simulation, and CUDA compilation. | New device decision | F009, F010, I001 |
| First network | Two inputs, two ReLU units, one output, 200 SGD steps. | Selected acceptance case | I001 |
| Mixed precision | Separate contract after FP32 acceptance. | Later extension | F011 |

The first implementation uses one compute device per invocation.
CPU staging values can coexist with that device for explicit transfers.
Multiple compute devices, distributed training, sparse tensors, convolution, attention, and higher-order derivatives are deferred.
User-defined kernels, automatic placement, random initialization, and differentiation through control flow are also deferred.
P003 defines the subsequent [checkpoint contract](model-checkpoints.md) for F015, F016, and I003.

## Types and public signatures

### Type notation

`Tensor<FP32>` is a new built-in generic type.
`FP32` is a dtype marker accepted only as a tensor type argument.
It has no standalone values or constructor.
`Float` remains a finite binary64 value.
Tensor construction makes the binary64-to-FP32 conversion explicit.

Shape and device location are immutable value metadata.
They are not source type arguments.
The checker keeps known metadata as graph facts.
Unknown metadata receives validation at the applicable constructor or operation boundary.
Core kernels consume validated descriptors and do not repeat those checks.

In signature tables, `T` means `Tensor<FP32>`.
`R` means a nonempty exact record whose fields all have type `T`.
`C...` means zero or more explicit context values with statically known types.
These symbols describe catalog rules, not source aliases or user-defined generics.
The [complete example](examples/training-mvp.lo) uses expanded source types.

| Type | Representation and public fields |
| --- | --- |
| `Device` | Immutable selector description. It contains no observable handle or address. |
| `Tensor<FP32>` | Dtype, shape, location, and immutable numeric contents. Metadata operations expose these fields. |
| `Parameters<R>` | Exact tensor record and validated key, dtype, shape, and location schema. No mutable storage identity. |
| `Gradients<R>` | Exact tensor record with the same schema as its selected Parameters. |
| `Optimizer.SGD.Config` | Validated learning rate. No defaults. |
| `Optimizer.AdamW.Config` | Validated learning rate, beta values, epsilon, and weight decay. No defaults. |
| `Optimizer.SGD.State<R>` | Read-only `parameters: Parameters<R>` and `step: Int`. The config is private immutable data. |
| `Optimizer.AdamW.State<R>` | The same public fields, plus private immutable moments, beta powers, and config. |

None of these new value types supports equality, ordering, map keys, or match case heads.
Existing records can contain them.
Exact record checking remains unchanged.
There is no `Parameter.new` API or independent Parameter object.
One field in `Parameters<R>` is one Parameter.

### Numeric operations

The `Math` namespace keeps its integer signatures.
The `Float` namespace provides explicit binary64 operations.
Implicit conversion between integer, binary64, and FP32 domains is prohibited.

F002 owns binary64 CLI operations and shared FP32 rounding and failure rules.
F003 exposes FP32 conversion through tensor construction and observation.
F004 exposes FP32 arithmetic through tensor processors.

| Signature | Contract |
| --- | --- |
| `Float.add(left: Float, right: Float) -> Float!` | Rounded binary64 addition. |
| `Float.sub(left: Float, right: Float) -> Float!` | Rounded binary64 subtraction. |
| `Float.mul(left: Float, right: Float) -> Float!` | Rounded binary64 multiplication. |
| `Float.div(left: Float, right: Float) -> Float!` | Division with zero-denominator rejection. |
| `Float.sqrt(value: Float) -> Float!` | Square root with negative-input rejection. |
| `Float.eq/lt/lte/gt/gte(left: Float, right: Float) -> Bool` | The five separate comparison operations. Signed zeros compare equal. |
| `Int.to_float(value: Int) -> Float!` | Nearest binary64 value, with overflow rejection. |
| `Float.to_int(value: Float) -> Int` | Exact integer after truncation toward zero. |
| `Float.to_string(value: Float) -> String` | Shortest round-trip binary64 decimal, with signed zero kept. |

`Float.new` remains an identity constructor for `Float`.
All numeric arithmetic uses round-to-nearest, ties-to-even.
Each operation rounds its result to its declared format.
Separate source operations do not receive fused multiply-add contraction.

Signed zero and subnormal values remain representable.
Underflow rounds to a subnormal value or signed zero.
Overflow and nonfinite results produce `Numeric.NonFinite`.
Zero division produces `Numeric.DivideByZero`.
A negative square-root input produces `Numeric.Domain`.
Square root keeps negative zero.

`Int.to_float` can lose integer precision through the stated rounding rule.
It does not promise an exact conversion for every integer.
`Float.to_int` always fits the existing integer magnitude limit for finite binary64 input.
Float comparison processors do not admit Float as a map key or range endpoint.

### Tensor construction and metadata

| Signature | Contract |
| --- | --- |
| `Device.new(selector: String) -> Device!` | Validate `cpu`, `cuda:0`, or `cuda-sim:0` without device initialization. |
| `Tensor.new(values: List<Float>, shape: List<Int>) -> T!` | Construct a CPU tensor after explicit FP32 rounding. |
| `Tensor.shape(value: T) -> List<Int>` | Return dimension sizes without numeric data access. |
| `Tensor.rank(value: T) -> Int` | Return the number of dimensions. |
| `Tensor.dtype(value: T) -> String` | Return `fp32`. |
| `Tensor.device(value: T) -> Device` | Return the immutable selector description. |
| `Tensor.to(value: T, device: Device) -> T!` | Explicit transfer with unchanged dtype, shape, and element bits. |
| `Tensor.to_float(value: T) -> Float!` | Read a CPU rank-zero tensor as an exact binary64 value. |
| `Tensor.values(value: T) -> List<Float>!` | Read CPU elements in row-major order as exact binary64 values. |

The selector `cuda-sim:0` names the software backend explicitly.
It never satisfies a request for `cuda:0`.
Other selector strings produce `Device.InvalidSelector`.
CPU-only builds still recognize `cuda:0` as a descriptor.
An attempted CUDA resource access in such a build fails with `Device.Unavailable`.

`Tensor.new` requires the element count to equal the product of its dimensions.
An empty shape has product one and represents a rank-zero tensor.
A zero dimension produces an empty tensor.
Negative dimensions and element-count overflow fail before allocation.
Empty constructor lists receive their element type from the signature.
Construction rejects FP32 overflow after conversion.

F003 owns the `Device` descriptor type and its constructor for tensor metadata.
F008 implements device sessions, grants, allocation boundaries, and transfers.
The descriptor alone does not require an active device backend.

The canonical layout is contiguous row-major order.
Reshape can share storage when the element count matches.
Transpose materializes a row-major result.
Broadcast operations produce the specified logical contents without observable aliasing.
Private storage reuse requires proof that all previous values remain unchanged.

### Tensor operations

All arithmetic operands require the same dtype and device location.
No operation inserts a tensor transfer.
Known shape conflicts are static errors.
Unknown conflicts produce typed errors before allocation or kernel submission.
Every tensor-producing operation is fallible, including shape-only operations that can allocate device memory.

| Signature | Shape and numeric contract |
| --- | --- |
| `Tensor.add/sub/mul/div(left: T, right: T) -> T!` | Separate operations with trailing-dimension broadcasting. |
| `Tensor.sqrt(value: T) -> T!` | Elementwise square root. |
| `Tensor.relu(value: T) -> T!` | Positive values are unchanged. Other values become positive zero. |
| `Tensor.matmul(left: T, right: T) -> T!` | Rank-two `[m,k]` and `[k,n]` inputs produce `[m,n]`. |
| `Tensor.broadcast(value: T, shape: List<Int>) -> T!` | Broadcast to exactly the requested shape. |
| `Tensor.reshape(value: T, shape: List<Int>) -> T!` | Preserve row-major element order and count. No inferred dimension. |
| `Tensor.transpose(value: T, axes: List<Int>) -> T!` | Require a permutation of every input axis. |
| `Tensor.sum(value: T, axes: List<Int>) -> T!` | Remove the selected axes. Accumulate in FP32. |
| `Tensor.mean(value: T, axes: List<Int>) -> T!` | Sum, then divide by the selected element count in FP32. |

Missing leading dimensions have size one for broadcasting.
Two aligned dimensions must be equal or have size one.
Size one can broadcast to zero.
An aligned zero dimension accepts only zero or one.
Rank-zero tensors broadcast to any supported shape.

Reduction axes must be unique nonnegative indices in strictly increasing order.
An empty axis list keeps the input shape and values.
Reducing every axis of a positive-rank tensor produces a rank-zero tensor.
`sum` over an empty reduction region produces positive zero.
`mean` over an empty reduction region produces `Tensor.EmptyReduction`, even when the output has no elements.

Matrix multiplication with `k = 0` produces zeros.
Zero output dimensions produce empty results without library submission.

CPU dot products multiply and then add in increasing contracted-index order.
CPU reductions add in input row-major order, starting from positive zero.
Each multiply, addition, square root, and division rounds to FP32.
Generated derivative operations obey the same rules.

## Differentiation and Parameter identity

### Gradient API

```text
Autodiff.grad(targets: R, C..., LossFlow)
  -> Record{value: Tensor<FP32>, gradients: R}!

LossFlow: (C..., R) -> Tensor<FP32>!
Autodiff.stop(value: Tensor<FP32>) -> Tensor<FP32>
```

The named loss flow receives explicit context first and selected inputs last.
A total loss with the same successful output type is also accepted.
The loss must return a rank-zero tensor on the selected compute device.
Known nonzero rank fails during checking.
Unknown nonzero rank produces `Autodiff.InvalidLoss` before the reverse pass.

The gradient result contains every selected field, including disconnected inputs.
Disconnected inputs receive zero tensors with the original shape, dtype, and location.

Context can contain tensors and immutable metadata.
Context values are constants with respect to the selected inputs.
Host storage identity cannot connect a context value to a selected input.
All selected inputs use FP32 and one device location.
The API rejects other selected field types.

`Autodiff.grad` belongs to F005 and operates directly on an exact tensor record.
It does not depend on F006 wrappers.
F006 adds Parameter identity and validated gradient lifecycle operations around that record.
This separation keeps the existing issue dependency order.

### Differentiable graph

The loss graph permits tensor arithmetic, tensor metadata, record projection, record construction, and calls to named loss helpers.
Host metadata calculations can determine shapes and axes only when they do not depend on tensor contents.
This subset admits integer literals, `List.new`, and total integer `Math` operations.
Tensor constants must enter as explicit context.
The checker rejects tensor construction and transfer inside the differentiated graph.

Reject these operations transitively before execution:

- Boundary reads, device transfers, host numeric observation, and commit activation.
- `Match.new`, list callbacks, nested `Autodiff.grad`, and optimizer operations.
- Self-handling flows and handler calls inside the differentiated graph.
- Unknown derivative rules, including binary64 `Float` arithmetic on a differentiated path.

Ordinary fallible helper flows propagate their errors without handlers inside this graph.
An outer flow can handle the final `Autodiff.grad` failure under existing `T!` rules.
Unused eager bindings remain part of the checked and evaluated loss body.
The checker does not remove unsupported operations because their result appears unused.

| Forward operation | VJP with output contribution `g` |
| --- | --- |
| `add(a,b)` | Contributions `g` and `g`. |
| `sub(a,b)` | Contributions `g` and `-g`. |
| `mul(a,b)` | Contributions `g*b` and `g*a`. |
| `div(a,b)` | Contributions `g/b` and `-(g/b)*(a/b)`, in that operation order. |
| `sqrt(a)` | Contribution `g/(2*sqrt(a))`. An active zero input produces `Autodiff.UndefinedDerivative`. |
| `relu(a)` | Contribution `g` where `a > 0`, otherwise positive zero. |
| `matmul(A,B)` | Contributions `g*transpose(B)` and `transpose(A)*g`. |
| `broadcast(a)` | Sum contributions across repeated and added dimensions. Restore the input shape. |
| `reshape(a)` | Reshape `g` to the original shape. |
| `transpose(a,axes)` | Apply the inverse axis permutation to `g`. |
| `sum(a,axes)` | Insert size-one dimensions, then broadcast `g` to the input shape. |
| `mean(a,axes)` | Divide `g` by the reduced count, then use the sum rule. |
| `stop(a)` | No derivative contribution through this operand. |

Elementwise binary VJPs also sum across broadcast dimensions for each operand.
The ReLU subgradient at zero is zero.
The `sqrt` zero rule applies when that node has a path to a selected input in the active reverse graph.
A stop-gradient boundary removes that path.

The compiler constructs the derivative graph after type and effect analysis.
It expands named helper invocations with distinct invocation identities.
The reverse pass visits nodes in reverse stable forward order.
For one node, operand contributions enter in increasing operand-index order.

Each active dependency edge contributes once, including both edges of `Tensor.mul(x, x)`.
Edges without a path to a selected input need no reverse calculation.
Each selected input starts with a zero accumulator.

The runtime keeps only values required by derivative rules.
The tape belongs to one `Autodiff.grad` call.
Success, forward failure, reverse failure, and host cancellation all release that tape.
No partial loss-and-gradient record becomes visible after failure.
Gradient evaluation does not change the selected inputs.

### Parameter and gradient operations

```text
Parameters.new(values: R) -> Parameters<R>!
Parameters.values(parameters: Parameters<R>) -> R
Gradients.new(parameters: Parameters<R>, values: R) -> Gradients<R>!
Gradients.values(gradients: Gradients<R>) -> R
Gradients.zero(parameters: Parameters<R>) -> Gradients<R>!
Gradients.add(left: Gradients<R>, right: Gradients<R>) -> Gradients<R>!
Gradients.scale(gradients: Gradients<R>, factor: Float) -> Gradients<R>!
```

Field names are explicit stable keys, ordered lexicographically for schema and diagnostic purposes.
The constructor validates a nonempty record and one shared device location.
Gradient construction validates exact keys, dtypes, shapes, and location against that schema.
Type checking rejects missing or additional fields when their record types are known.
Metadata mismatches produce `Gradient.SchemaMismatch` at the public operation boundary.
There is no hidden identity or mutable registry.

Repeated use of one field is a tied Parameter.
Two fields remain separate Parameters even when their numeric values or private storage are equal.
The constructor does not merge them.
Gradient accumulation combines matching fields in the stated order.
Reset means construction of a new `Gradients.zero` value.
Old gradient values remain unchanged.

For mean losses, weight each microbatch gradient by its example count divided by the complete batch count.
`Gradients.scale` converts its binary64 factor to FP32 once.
Combine scaled gradients in microbatch source order.
An empty complete batch is invalid.
Bitwise equality with a single combined reduction is not required across different reduction groupings.
The fixed numerical tolerance below controls that fixture.

## Optimizer transitions

```text
Optimizer.SGD.Config.new(learning_rate: Float) -> Optimizer.SGD.Config!
Optimizer.SGD.State.new(parameters: Parameters<R>, config: Optimizer.SGD.Config)
  -> Optimizer.SGD.State<R>!
Optimizer.SGD.step(state: Optimizer.SGD.State<R>, gradients: Gradients<R>)
  -> Optimizer.SGD.State<R>!

Optimizer.AdamW.Config.new(learning_rate: Float, beta1: Float, beta2: Float,
                          epsilon: Float, weight_decay: Float)
  -> Optimizer.AdamW.Config!
Optimizer.AdamW.State.new(parameters: Parameters<R>, config: Optimizer.AdamW.Config)
  -> Optimizer.AdamW.State<R>!
Optimizer.AdamW.step(state: Optimizer.AdamW.State<R>, gradients: Gradients<R>)
  -> Optimizer.AdamW.State<R>!
```

Config constructors convert all numeric fields to FP32 once.
They validate the converted values.
The learning rate and epsilon must be positive.
Each beta must satisfy `0 <= beta < 1`.
Weight decay must be nonnegative.
The product of learning rate and weight decay must be finite.

Zero, underflowed, and rounded-to-one config values receive the same validation rules.
Invalid config produces `Optimizer.InvalidConfig`.

State construction starts at step zero.
AdamW starts with zero first moments, zero second moments, and beta powers equal to one.
Moments and beta powers stay on the Parameter device.
Small config scalars enter kernels as explicit operation arguments.
They do not authorize implicit transfers of tensor contents.

For SGD, each field uses `p_next = sub(p, mul(rate, g))`.
The step count increases by one only in the returned successful state.
SGD has no momentum or weight decay in this milestone.

AdamW uses the following order for each field.
Every arithmetic operation rounds to FP32.
`b1power` and `b2power` are the previous state's beta powers.

```text
m_next = add(mul(beta1, m), mul(sub(1, beta1), g))
v_next = add(mul(beta2, v), mul(sub(1, beta2), mul(g, g)))
b1power_next = mul(b1power, beta1)
b2power_next = mul(b2power, beta2)
m_hat = div(m_next, sub(1, b1power_next))
v_hat = div(v_next, sub(1, b2power_next))
decayed = sub(p, mul(mul(rate, weight_decay), p))
update = mul(rate, div(m_hat, add(sqrt(v_hat), epsilon)))
p_next = sub(decayed, update)
step_next = step + 1
```

Epsilon is outside the square root.
Weight decay applies to all selected fields, including biases.
It is separate from gradient and moment calculations, as described in the [AdamW paper](https://arxiv.org/abs/1711.05101).
Per-field decay exclusions and learning-rate schedules are deferred.
The internal square root in AdamW is ordinary arithmetic, not a differentiated loss operation.
Zero moments are valid.

Each step validates the incoming gradient schema before work begins.
The old state remains valid after success or failure.
Any numeric or resource failure prevents publication of the entire next state.
An implementation cannot expose a partially updated Parameter record.

## Device resources and observable effects

### Effect classification

| Operation | Classification | Commit required | Differentiated loss |
| --- | --- | --- | --- |
| Device descriptor and host metadata | Value operation | No | Metadata only |
| CPU tensor construction | Value operation with resource cost | No | Rejected |
| First CUDA access and allocation | Device resource boundary | No | Internal allocation permitted |
| Tensor arithmetic and optimizer work | Value operation with device resource summary | No | Only listed tensor VJPs |
| `Tensor.to` | Explicit device transfer boundary | No | Rejected |
| CPU `Tensor.values` and `Tensor.to_float` | Numeric observation of a value | No | Rejected |
| File or HTTP read | Existing boundary read | No | Rejected |
| File write or adapter activation | Existing commit activation | Yes | Rejected |

A device resource boundary changes private execution resources, not an external application value.
This classification permits a pure training callback to allocate tensors and calculate gradients.
The callback remains pure under the existing rule: no direct or indirect commit activation.
Differentiation applies the additional restrictions above.

The compiler adds a transitive device resource summary beside the existing boundary-read and activation summaries.
Device resource operations cannot be speculated across branches, reads, or commits.
The runtime resolves earlier fallible work before a later observable boundary.
Unused flow bodies still receive static validation without device initialization.

### Selection and authorization

The source selects a `Device` explicitly.
CPU execution requires no new external capability.
CUDA and simulation require an exact host grant through `--allow-device cuda:0` or `--allow-device cuda-sim:0`.
The grant permits resource use but does not select or rewrite a source device.
Only one non-CPU selector can be granted per invocation.

Authorization denial produces the host diagnostic `runtime.capability-denied`, with exit status 2.
It occurs before device initialization, allocation, or transfer.
Unavailable devices produce a recoverable `Device.Unavailable` after authorization.
A failed CUDA request never executes on CPU or simulation.
Backend selection and profile identity appear in inspection and execution evidence.

`Tensor.to` between identical locations returns an equal immutable value without a transfer.
Transfers between CPU and the selected compute device keep every FP32 bit, including signed zero and subnormals.
Transfers between two non-CPU selectors are rejected.
The source remains live until the transfer completes.
Partial transfers do not publish a result.

CPU observation operations reject non-CPU tensors with `Tensor.HostObservationRequired`.
CLI publication recursively requires CPU tensor fields.
An untransferred result produces the host diagnostic `runtime.tensor-publication`, with exit status 2.
The CLI validates the complete result before it writes any result bytes.
The program must transfer numeric results to CPU before return publication.

Diagnostic tensor summaries contain metadata only, even when the original failure input contains device tensors.
Error reporting never introduces a tensor download.
Error construction snapshots tensor metadata instead of keeping tensor buffers in `input` and trace records.

### Scheduling and failures

The first CUDA backend uses one internal stream and one library handle per invocation.
Lo exposes no streams, events, async syntax, locks, or synchronization calls.
The initial implementation can synchronize each operation.
F010 can defer completion only while it keeps the same error and publication order.

Each submitted operation carries its source node, invocation, and stable evaluation sequence number.
The runtime waits for all earlier fallible operations before it selects a later failure.
Failure selection follows source evaluation order, not completion order.
Dependent work cannot read an unsuccessful result.
Later File reads, host observations, returns, and commits wait for preceding fallible work.
The first unresolved failure prevents those later operations.

The runtime can copy bounded status records for validation and failure selection.
These records contain status and source identifiers, not tensor contents or training metrics.
Evidence separates these control transfers from explicit numeric transfers.
There are no implicit numeric downloads between training steps.

Recoverable errors enter the existing `T!` path with the originating operation and source span.
A lost or unusable context produces `Device.Lost` and marks that invocation's device session unusable.
Later resource operations in the same session return `Device.Lost` without submission.
A host-only handler can still return an error response.
There is no automatic retry, context replacement, or backend change.

Cancellation and execution limits remain host diagnostics outside Lo handlers.
Cleanup waits for owned work or destroys the unusable context through the backend boundary.
The runtime releases every allocation receipt on success, failure, and cancellation.
It does not report a clean shutdown while owned resources remain live.
Software failure injection exercises cleanup when a device operation cannot complete normally.

## Numeric profiles and resource budgets

### Reproducibility

The CPU profile is `cpu-fp32-strict`.
It uses the explicit operation order in this contract without contraction or reassociation.
Repeated execution with identical inputs produces identical numeric bits.
The interpreter and native backend must give the same numeric results.

The NVIDIA profile is `cuda-fp32-pedantic`.
Its identity includes Lo build, kernels, CUDA libraries, driver, architecture, device SM count, and library configuration.
Results repeat bitwise within that fixed identity.
cuBLAS can use a different accumulation order from the CPU reference.
Lo does not require CPU and CUDA results to be bitwise equal.
NVIDIA limits its own bitwise guarantee by toolkit version, architecture, and SM count. [cuBLAS reproducibility](https://docs.nvidia.com/cuda/archive/13.0.2/cublas/index.html#results-reproducibility)

This is an explicit extension to specification section 5.6.
Scheduling within one profile keeps exact values, failures, and effects.
Selecting a different numeric profile is an explicit execution input and can change rounded values.
Tolerance comparisons do not change Lo equality or permit nondeterministic execution.
They apply only to the fixed acceptance fixtures listed below.
Ill-conditioned arbitrary programs receive no universal relative-error promise.

All profiles keep finite public tensor values.
Numeric scalar operations and reference reductions reject nonfinite intermediate arithmetic results.
CUDA library operations validate finite inputs and final outputs.
Their internal accumulation order belongs to the pinned library profile.
Different profiles can reject different extreme numeric cases.
Boundary failures, authorization, and effect ordering keep exact contracts across profiles.

| Fixture comparison | Absolute tolerance | Relative tolerance |
| --- | --- | --- |
| Exact representable numeric and transfer fixtures | 0 | 0 |
| FP32 primitive and analytical VJP fixtures | `1e-6` | `1e-5` |
| FP32 central finite differences, step `1e-3` | `2e-3` | `2e-2` |
| Binary64 reference finite differences, step `1e-6` | `1e-8` | `1e-7` |
| Weighted microbatch and complete batch | `1e-6` | `1e-5` |
| I001 CPU and software backend values at each step | `1e-5` | `1e-4` |
| AdamW reference values for two fixed steps | `1e-6` | `1e-5` |

The comparison is `abs(actual - expected) <= atol + rtol * abs(expected)`.
Shape, dtype, keys, error kinds, and transfer counts require exact equality.
Every I001 run must also finish with finite loss below `0.001`.
Each profile must produce bitwise identical results across three identical runs.
Finite differences use inputs away from ReLU zero and square-root zero.
Independent analytical fixtures cover those boundaries separately.

### Resource accounting

Existing host limits remain active.
Tensor work adds explicit charges before allocation or submission.
All counts use checked integer arithmetic.
An exceeded host budget produces `runtime.limit`, never a recoverable allocation error.

| Resource | Initial limit |
| --- | --- |
| Tensor rank | 8 |
| One dimension size | 1,000,000 |
| One tensor element count | 1,000,000 |
| All live owned tensor storage and workspace | 64 MiB per invocation, across CPU staging and the compute device |
| Live tape storage | 32 MiB, also included in the 64 MiB total |
| Live optimizer moments and beta powers | 32 MiB, also included in the 64 MiB total |
| Device library workspace | 16 MiB, also included in the 64 MiB total |
| Expanded value weight | Existing 16 MiB per value and 64 MiB cumulative limits |
| Work steps | Existing 1,000,000 default, replaceable by `--max-steps` |

The tensor weight is `16 + 4*N + 8*rank + len(dtype) + len(selector)` bytes.
Composite values count repeated tensors repeatedly under the existing expanded-weight rule.
Parameters and gradients add 16 bytes around their exact record weight.
Optimizer state weight includes its Parameters, step, config, moments, and beta powers.
Device handles and storage addresses have no serialized value weight.

A Device value has 16 bytes of fixed weight plus its selector bytes.
Config weight is 16 bytes plus an exact record of the named fields as rounded `Float` values.
State weight is 16 bytes plus its complete exact record, including private fields.
AdamW beta powers count as rank-zero tensors.

Live storage counts each backing allocation once, rounded up to its actual reserved byte count.
Views share one allocation receipt.
Parameters, gradients, tape, optimizer state, transfer staging, and workspace all contribute to the total.
Library-private driver context overhead is outside this Lo-owned storage limit and appears separately in qualification notes.
Logical budgets do not claim an exact bound on process or driver memory.

| Work | Additional logical steps |
| --- | --- |
| Elementwise operation, construction, conversion, broadcast, transpose, or transfer | One per output element |
| Reduction | One per visited input element, plus one division per mean output element |
| Matrix multiplication `[m,k] * [k,n]` | `2*m*n*k` |
| Reshape and metadata | No element charge beyond existing expression evaluation |
| Derivative and optimizer operations | Sum of their specified primitive operation charges |

Charges include work hidden inside a fused implementation.
Generated derivative and optimizer results also contribute to cumulative expanded value weight.
Fusion and storage reuse cannot reduce semantic charges.
F009 initially uses separate kernels without cross-operation fusion.

Tape storage ends with each derivative call.
Fold state storage ends when it becomes unreachable and owned work no longer uses it.
If a program explicitly keeps prior states, those states continue to consume the budget.

## NVIDIA backend contract

### Build and platform selection

F009 selects graph execution in the Go interpreter for the first NVIDIA implementation.
The native CPU backend from F001 does not implement this device contract.
A narrow C ABI submits validated tensor descriptors to CUDA Runtime and cuBLAS.
Repository-owned CUDA kernels implement elementwise operations, reductions, layout transforms, derivatives, and optimizer arithmetic.
`nvcc` compiles these kernels when the CUDA-enabled Lo binary is built.
Runtime compilation, PTX JIT selection, and native Lo executable generation are outside this route.

| Input | Selected baseline |
| --- | --- |
| GPU host | Linux x86_64, Ubuntu 24.04.3 |
| Go | Repository pin, currently 1.27.1 |
| Host C++ compiler | GCC 13.3.0, C++17 |
| CUDA Toolkit | 13.0 Update 2, distribution 13.0.2 |
| NVCC | 13.0.88 |
| CUDA Runtime | 13.0.96 |
| cuBLAS | 13.1.0.3 |
| Device code target | `sm_80` cubin, with no PTX entry |
| Driver floor | 580.95.05, with the exact installed driver recorded in profile identity |
| Custom kernel arithmetic | `--ftz=false --prec-div=true --prec-sqrt=true --fmad=false`, without fast math |

The [release table](https://docs.nvidia.com/cuda/archive/13.0.2/cuda-toolkit-release-notes/index.html#cuda-toolkit-major-components) supplies the component versions.
The [Linux installation guide](https://docs.nvidia.com/cuda/archive/13.0.2/cuda-installation-guide-linux/index.html#system-requirements) includes the selected host and compiler.
The [NVCC reference](https://docs.nvidia.com/cuda/archive/13.0.2/cuda-compiler-driver-nvcc/index.html) defines the arithmetic switches.
These selections require future Lo compilation and device execution evidence.

F009 adds a `make build-cuda` target with `CGO_ENABLED=1` and the `cuda` Go build tag.
The standard `make build` keeps CPU and explicit software simulation on macOS arm64 and Linux arm64.
It has no CUDA link dependency.
The CUDA build qualifies its own Linux x86_64 CPU path as well.
There is no macOS CUDA backend or implicit translation to Apple GPU APIs.

The CUDA binary links the selected shared Runtime and cuBLAS libraries.
Their versions are validated at the device boundary.
Build evidence records source hashes, compiler versions, flags, library hashes, and generated cubin hashes.
F009 records exact package artifacts and container digest as build evidence for these fixed selections.
Missing build inputs stop the CUDA build without alternative compiler or library selection.

Unsupported device architecture produces `Device.UnsupportedProfile` before graph submission.
The first supported compute capability is exactly 8.0.
Future target changes replace the supported contract through a separate reviewed change.

### Operation mapping

Matrix multiplication uses `cublasGemmEx` with `CUDA_R_32F` operands and `CUBLAS_COMPUTE_32F_PEDANTIC`.
The handle uses `CUBLAS_PEDANTIC_MATH` and `CUBLAS_ATOMICS_NOT_ALLOWED`.
The algorithm argument is `CUBLAS_GEMM_DEFAULT`, without runtime autotuning.
The pinned library owns its deterministic internal selection for `sm_80`.
One fixed stream and owned workspace keep the selected execution configuration.

TF32, FP16, BF16, and floating-point emulation modes are disabled for P1.
These API choices come from the [cuBLAS reference](https://docs.nvidia.com/cuda/archive/13.0.2/cublas/index.html#cublasgemmex).

The backend controls arithmetic configuration and records all effective overrides in its profile identity.
An override that conflicts with this profile produces `Device.UnsupportedProfile` before submission.
The host grant resolves device index zero within the invocation's validated device visibility configuration.

For row-major `A[m,k] * B[k,n]`, submit column-major `B * A` with dimensions `(n,m,k)`.
Use leading dimensions `n`, `k`, and `n` for `B`, `A`, and the output.
Both transpose flags are `CUBLAS_OP_N` after required transpose operations materialize their results.
Independent non-square fixtures verify this mapping.
Zero dimensions take the specified empty or zero result path before library submission.

Custom reductions use one fixed traversal order per output element.
They do not use unordered atomic accumulation.
The software backend interprets the same lowered descriptors and layout arguments through independent host arithmetic.
It does not call the CPU tensor evaluator as its numerical oracle.
Its profile is `cuda-sim-fp32` and its receipts identify simulation.

| Supported operations | Dtype and layout | Constraints |
| --- | --- | --- |
| Elementwise arithmetic, ReLU, broadcast | FP32, row-major | Rank 0 through 8, accepted broadcasting rules |
| Reshape, transpose | FP32, row-major | Valid counts or complete axis permutation |
| Sum, mean | FP32 input and accumulation | Sorted unique axes, accepted empty-region rules |
| Matrix multiplication and its VJPs | FP32 input and accumulation | Rank two, dimensions within limits |
| Gradient accumulation and stop-gradient | FP32, matching schemas | One compute location |
| SGD and AdamW | FP32 Parameters, moments, and arithmetic | Validated config and gradient schema |

Static checking identifies unsupported source operations without device access.
Invocation preflight checks resolved shapes, location, profile support, and resource limits before submission.
Backend execution failures remain failures, even when CPU execution could produce a value.

## Diagnostics and inspection

| Boundary | Diagnostic or `Error.kind` |
| --- | --- |
| Wrong source type or known shape conflict | `check.type-mismatch`, `check.tensor-shape` |
| Unsupported differentiated node or effect | `check.autodiff-operation`, `check.autodiff-effect` |
| Known invalid loss rank | `check.autodiff-loss` |
| Unsupported source operation for a selected backend | `check.backend-operation` when statically known |
| Invalid runtime shape, axes, or element count | `Tensor.InvalidShape` |
| Incompatible runtime shapes | `Tensor.ShapeMismatch` |
| Different input locations | `Tensor.DeviceMismatch` |
| Empty mean region | `Tensor.EmptyReduction` |
| GPU value used for host observation | `Tensor.HostObservationRequired` |
| Non-CPU tensor at CLI publication | `runtime.tensor-publication`, exit status 2 |
| Numeric overflow, zero division, or invalid domain | `Numeric.NonFinite`, `Numeric.DivideByZero`, `Numeric.Domain` |
| Invalid loss rank or undefined derivative at runtime | `Autodiff.InvalidLoss`, `Autodiff.UndefinedDerivative` |
| Gradient metadata mismatch | `Gradient.SchemaMismatch` |
| Invalid optimizer config | `Optimizer.InvalidConfig` |
| Invalid selector or unavailable device | `Device.InvalidSelector`, `Device.Unavailable` |
| Unsupported runtime profile or operation | `Device.UnsupportedProfile`, `Device.UnsupportedOperation` |
| Allocation, transfer, launch, library, or context failure | `Device.AllocationFailed`, `Device.TransferFailed`, `Device.ExecutionFailed`, `Device.LibraryFailed`, `Device.Lost` |
| Host authorization, limit, or cancellation | `runtime.capability-denied`, `runtime.limit`, `runtime.cancelled` |

Diagnostics include operation, source span, expected and actual metadata, and selected device where applicable.
The earliest failing element in row-major order determines an elementwise numeric error's index.
Private CUDA status values can appear as diagnostic details, never as the stable Lo error kind.
Context loss takes precedence over a generic launch or library category for that same operation.
Source-order rules still determine which operation's failure is published first.

`lo inspect` adds tensor metadata, selected profile requirements, device resource sites, transfers, derivative sites, and synchronization boundaries.
It reports known allocation sizes and explicit unknown sizes.
The report distinguishes graph capabilities from host grants.
It records each derivative operation's originating forward node and source span.
`lo parse`, `lo check`, and `lo inspect` do not load a CUDA driver or initialize a device.

CPU tensor JSON has `type: "Tensor<FP32>"` plus a `value` object.
That object contains `dtype`, `shape`, `device`, and row-major `values`.
Shape entries use decimal strings for Lo integers.
Element entries use shortest round-trip FP32 decimal strings, including signed zero.

The public JSON result cannot silently replace a tensor with truncated metadata.
Serialization remains bounded by the existing value and host limits.
Device tensors in diagnostics use an explicit metadata-summary shape without element contents.
F003 and F010 extend the inspector and diagnostic schemas with one canonical representation each.

Other extension values use the existing typed `type` and `value` JSON envelope.

| Value | JSON `value` contents |
| --- | --- |
| Device | Selector string. |
| Parameters or gradients | Object with exact field names and typed tensor values. |
| Optimizer config | Object with its named fields as typed, FP32-rounded `Float` values. |
| SGD state | Typed `parameters`, `step`, and `config` fields. |
| AdamW state | SGD fields plus typed `first_moment`, `second_moment`, `beta1_power`, and `beta2_power`. |

Moment fields use the exact record type `R`.
Beta-power fields use rank-zero tensors.
The JSON state contains complete immutable data, including fields unavailable through source projection.
The [checkpoint contract](model-checkpoints.md) defines state import and persistent checkpoints through the current typed value representation.

An error snapshot represents each tensor as a typed metadata record.
Its exact fields are `tensor_type: String`, `dtype: String`, `shape: List<Int>`, and `device: String`.
The `tensor_type` value is `Tensor<FP32>`.
This diagnostic record cannot serve as a tensor or differentiation target.
Nested argument and state snapshots use the same transformation.
The runtime releases failed tensor work independently of those snapshots.

## Complete training graph

The [complete Lo design fixture](examples/training-mvp.lo) supplies `Forward`, `Loss`, `TrainStep`, and explicit setup and observation.
It uses the existing grammar, expanded exact record types, and the extension signatures above.
Its source retains the future device transfer APIs.
The [CPU training example](../examples/training_cpu.lo) executes the same network without device transfers.

| Stage | Values and shapes | Boundary or state transition |
| --- | --- | --- |
| Setup | Inputs `[4,2]`, targets `[4,1]`, `w1[2,2]`, `b1[2]`, `w2[2,1]`, `b2[1]` | CPU construction, then six explicit uploads for a device run |
| First layer | `hidden = relu(add(matmul(inputs,w1),b1))`, shape `[4,2]` | Bias broadcasts across rows |
| Output | `predictions = add(matmul(hidden,w2),b2)`, shape `[4,1]` | Linear output |
| Loss | `mean(mul(predictions-targets,predictions-targets), [0,1])`, shape `[]` | Rank-zero result on the compute device |
| Gradient | `Autodiff.grad(values, inputs, targets, Loss)` | Reverse graph, four exact fields, no transfers |
| Update | `Optimizer.SGD.step(state, gradients)` | New state, step increases by one |
| Iteration | `List.fold` over 200 counter values | Explicit input context and prior returned state |
| Observation | Final loss and all four Parameter tensors | Five explicit downloads, then CPU JSON publication |

The source also downloads the initial loss once before the fold.
There are six uploads and six downloads for a complete simulated or CUDA run.
CPU-to-CPU `Tensor.to` calls produce no transfer receipts.
There are no numeric transfers inside `TrainStep`.
Counter construction uses an existing pure named `List.unfold` callback.
No flow needs a capture, recursion, mutable binding, or statement loop.

The loss graph uses three matrix VJP products per step.
The input context needs no derivative.
It reduces bias contributions over the batch axis.
The shared residual node receives both multiplication-edge contributions.
ReLU uses the selected zero subgradient.

All four Parameter fields receive SGD updates.
Final loss uses the state after update 200, not the loss before that update.

An unhandled failure prevents result publication.
An enclosing fallible flow can construct a File effect after successful CPU observation.
Only its subsequent explicit `commit` publishes persistent output.
Failure before that commit prevents the write.
Already completed earlier commits keep the existing no-rollback semantics.

### Independent arithmetic evidence

The [reference calculation](evidence/training-reference.py) uses direct network derivatives and Python standard-library arithmetic.
It does not call Lo, a tensor framework, or the compiler's derivative generator.
It checks binary64 and FP32-rounded calculations, all nine Parameter derivatives, and two AdamW updates.
It is a fixture reference, not a general IEEE 754 conformance oracle.

```bash
python3 docs/evidence/training-reference.py
```

| Observation | Reference result |
| --- | --- |
| Initial predictions | `[0, 0.25, 0.25, 0.5]` |
| Initial loss | `2.46875` |
| Initial `w1` gradients, row-major | `[-0.40625, -0.09375, -0.21875, -0.53125]` |
| Initial `b1` gradients | `[-0.40625, -0.53125]` |
| Initial `w2` gradients | `[-1.625, -2.125]` |
| Initial `b2` gradient | `[-2.5]` |
| First FP32 `w2` update | `[0.33125001192092896, 0.35624998807907104]` |
| First `b2` update | `0.125` |
| Loss after 200 updates, binary64 reference | `0.00013215420364365218` |
| Loss after 200 updates, FP32 reference | `0.00013215476064942777` |
| Largest FP32 finite-difference error at the separate smooth input | `0.00004976987838745117` |

The independent network calculation passed the I001 threshold.
Its direct derivative arithmetic can group contributions differently from generated VJPs.
Implementation comparisons use the fixed fixture tolerances.
This calculation validates the example and equations.
The [training acceptance evidence](evidence/training-stack-green.txt) and [native acceptance report](native-compilation-results.md) separately record Lo execution.

The AdamW fixture starts with `[1,-2]`, then uses gradients `[0.1,-0.2]` and `[0,0.3]`.
Its config is rate `0.01`, betas `0.9` and `0.999`, epsilon `1e-8`, and decay `0.1`.
The FP32 Parameter results are `[0.9890000224113464,-1.9880000352859497]` and `[0.9813104271888733,-1.988489031791687]`.
The script also reports moments to compare with optimizer results.

## Source contract changes

These changes are selected by P001.
Each owning feature must update the relevant specification text and conformance fixtures before its implementation acceptance.
F002 through F007 implemented their source changes in specification section 24 and the affected core sections.
The table includes those completed changes and the remaining device contracts.

| Source section | Required change | Owner |
| --- | --- | --- |
| Specification 3 and 6.1 | Add binary64 arithmetic and dtype marker scope. Retain finite values and explicit conversion. | F002 |
| Specification 6.3 and 6.6 | Add `Tensor<FP32>` and the selected built-in wrappers. Limit the list-only statement to general-purpose sequences. | F003, F006, F007 |
| Specification 5.6 | Add explicit numeric profile identity and device resources. Require exact determinism within each profile. | F003, F008, F010 |
| Specification 5.7 | Apply immutable value semantics to tensors, Parameters, gradients, and optimizer state. | F003, F006, F007 |
| Specification 8.4, 18.1, and 18.3 | Add operation signatures and declared error kinds. | F002 through F010 |
| Specification 8.7 and 23.1 | Retain fold argument order and sequential state. Add the training example as conformance evidence. | I001 |
| Specification 12 and 22.2 | Add transitive device resource summaries and stricter loss-graph restrictions. | F005, F008 |
| Specification 17 | Retain grammar productions. Admit the new built-in names and generic arities through the type catalog. | F003, F006, F007 |
| Specification 22.4 | Add constructor boundaries, Parameter schema validation, and explicit FP32 conversion. | F002, F003, F006, F007 |
| Specification 23 | Add tensor arithmetic order, resource charges, device failures, and host observation. | F004, F008, F010 |
| Compiler design types and catalog | Add nominal wrappers, known tensor metadata, and the closed derivative registry. | F003, F005, F006, F007 |
| Compiler design effects and graph | Add device summaries, derivative source maps, and evaluation sequence identities. | F005, F008, F010 |
| Compiler design CLI and serialization | Add grants, tensor JSON, diagnostic summaries, and numeric profiles. | F003, F008, F010 |
| Compiler design execution limits | Add tensor weight, logical work, and live resource receipts. | F003, F005, F007, F008 |

F001 implements these CPU contracts through generated code and shared runtime operations.
The generated code implements derivative plans, Parameters, gradients, and optimizer transitions.
F008 implements grants, synchronous transfers, and the resource ledger through the [device resource contract](device-resources.md).
F009, F010, F011, and I001 retain their remaining scope.

## Implementation and acceptance sequence

| Issue | Required public evidence |
| --- | --- |
| F002 | CLI arithmetic and conversion cases, exact representable results, rounding limits, domain errors, and fallibility rejection. |
| F003 | Constructor, shape, layout, metadata, serialization, immutable sharing, and resource-limit cases. |
| F004 | Independent non-square products, broadcasting, empty dimensions, reductions, and cancellation-sensitive numeric cases. |
| F005 | Analytical and finite-difference VJPs, shared edges, explicit context, disconnected fields, and transitive rejection cases. |
| F006 | Stable keys, tied use, separate equal-valued fields, gradient reset, stop-gradient, and weighted microbatches. |
| F007 | Independent SGD and AdamW transitions, invalid config, exact schemas, and previous-state preservation. |
| F008 | Grants, explicit transfers, denied access before initialization, allocation limits, and cleanup receipts. |
| F009 | Pinned CUDA compilation, ABI descriptor tests, independent software execution, and unsupported-profile rejection. |
| F010 | Out-of-order completion injection, source-order failures, lost contexts, cancellation, and blocked later commits. |
| I001 | Complete example through parse, check, inspect, and run on CPU and simulation, with fixed numerical gates. |
| F011 | A reviewed precision extension after I001, then its own numerical and state-transition evidence. |

Each behavior change starts with a failing integration test through the public CLI.
Derivative algorithms can also use focused unit tests after the public failure is established.
Injected backend conditions keep the real checker, evaluator, resource owner, handler path, and CLI serialization.
Tests do not replace the entire training result with a fixture response.

Software simulation establishes lowering descriptors, operation order, numerical fixtures, and lifecycle behavior.
Controlled backend tests establish failure and cancellation behavior under their injected conditions.
CUDA compilation establishes syntax, linkage, and target artifact construction.
None of these alone establishes physical GPU execution, vendor kernel arithmetic, driver scheduling, or device performance.
The acceptance record must state these limits.
Physical-device access is not a prerequisite.

F009 and I001 need no general CUDA emulator.
The selected software operation backend is the required numerical environment.
An emulator can supply additional evidence when it supports the selected cubin and ABI.
Unavailable emulator support does not authorize implicit CPU substitution for a CUDA request.

I001 runs `make test-integration`, `make test-conformance`, and the final `make ci` at its implementation completion checkpoint.
F009 also supplies the documented `make build-cuda` compilation lane.
The P001 document review does not claim that those future acceptance commands passed.

### P001 design validation

P001 recorded the following results before F003 implementation.
`make build` passed against that compiler.
`lo parse docs/examples/training-mvp.lo --format json` accepted the complete source fixture.
`lo check` rejected that fixture with `resolve.unknown-name` for `FP32`, as expected before F003.
This result established grammar reuse before extension type checking or training execution existed.
F002 through F007 subsequently implemented the CPU extension, and F001 added native execution.

The independent reference script passed all network, finite-difference, and optimizer assertions.
The document review traced P001 requirements to the selected contracts and dependent issues.
No runtime implementation or native compilation change formed part of P001.

## Open Decisions

The separate [Metal implementation plan](metal-backend-plan.md) defines the Apple backend implementation sequence.
F013 implements the [Metal arithmetic profile](metal-arithmetic.md), derivatives, and optimizers.
F012, F013, F014, and I002 own that work independently of the NVIDIA implementation sequence below.

No unresolved decision blocks the NVIDIA P1 issues described in this document.
The following decisions belong to later scope or implementation evidence.

| Item | Status and owner | Required resolution |
| --- | --- | --- |
| Package artifact hashes and container digest | F009 build evidence | Record exact artifacts for the selected versions before CUDA compilation acceptance. |
| FP16 and BF16 storage and accumulation | Deferred to F011 | Select formats, casts, master Parameters, and overflow behavior after I001. |
| Loss scaling and skipped updates | Deferred to F011 | Define complete immutable state transitions before mixed-precision implementation. |
| TF32 or other reduced matrix arithmetic | Deferred to F011 | P1 disables these modes. A later profile requires explicit acceptance rules. |
| More devices or NVIDIA architectures | Outside the first milestone | Define placement, authorization, numeric identity, and resource ownership in a new contract. |
| Differentiation through branches or list callbacks | Outside F005 | Define executed-path derivatives and tape bounds before implementation. |
| Distributed, sparse, convolution, attention, and higher-order operations | Outside this extension | Use separate feature designs and acceptance fixtures. |

P001 closes the design task only.
Each implementation issue closes only after its required evidence passes.
F008 implements device resources and explicit transfers.
F009, F010, F011, and I001 retain device arithmetic, deferred execution, precision, and complete training acceptance.
