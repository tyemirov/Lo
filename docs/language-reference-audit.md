# Language Reference Audit

Audit date: October 16, 2026.
Validated implementation revision: `12940d9`.

The [Lo Language Reference](../specification.md) is the language contract.
This audit records its comparison with the current implementation and tests.
It does not define a separate language contract.

## Scope and Findings

The audit included the built-in catalog, core syntax and semantics, tensors, autodiff, gradients, optimizers, and device support.
The validated catalog contained 101 operations.
All 101 names occur in the revised reference.
`Match.new` is a syntax construct and is separate from this catalog count.

| Finding | Classification | Correction |
| --- | --- | --- |
| The introduction claimed coverage only through F004. | Stale prose | The introduction now includes autodiff, SGD, AdamW, and device execution. |
| The reference described the compiler and runtime as future work. | Stale prose | Section 21 now identifies the implemented components. |
| Section 5.1 implied evaluation only when a value was referenced. | Contradictory prose | Section 5.1 now agrees with eager binding evaluation in section 23.3 and the runtime. |
| Constructor and processor indexes omitted training operations. | Missing documentation | Section 18 now includes training constructors and links to every numeric and training family. |
| The pure-processor index omitted fallible core processors. | Missing documentation | The index now includes integer division, remainder, minimum, maximum, and map lookup. |
| NVIDIA status could imply an available arithmetic backend. | Ambiguous prose | The guide separates CPU, Metal, transfer simulation, and unavailable CUDA execution. |
| The autodiff subset did not explicitly list stop-gradient beside its permitted operations. | Missing cross-reference | Section 24.5 now names `Autodiff.stop`, as specified in section 24.6. |
| Device mismatch behavior had no named error in the transfer description. | Missing documentation | Section 24.8 now distinguishes authorization precedence, the one-selector CLI grant, and `Tensor.DeviceMismatch`. |
| Plain section labels and unfenced examples made navigation difficult. | Document structure | Markdown headings, contents links, code blocks, and task links now organize the reference. |

The changes preserve numbered rule identities, including sections 24.1 through 24.8.
Conformance fixtures and existing design references retain their rule targets.
No runtime behavior changed.
The audit established no new semantic decision or implementation defect.

## Audit Boundary

Concurrent checkpoint work appeared in the shared checkout after the implementation checks.
It adds checkpoint encoding, decoding, and container transfers.
Those changes are outside the validated revision and the 101-operation inventory in this report.
The implementation and its separate checkpoint document remain unchanged by this task.
Checkpoint qualification must update the reference with its accepted contracts.

## Operation Coverage

Each row identifies the contract, catalog source, and public integration evidence for its operations.
The inventory came from `catalog.All()`.
The [catalog integration test](../tests/integration/catalog_test.go) verifies every catalog entry.
Runtime tests supply additional behavioral evidence.

| Family and exact operation names | Reference | Catalog | Tests |
| --- | --- | --- | --- |
| **Core values**: `Bool.new`, `Bytes.new`, `Error.new`, `Int.new` | [Contract](../specification.md#71-built-in-constructors) | [catalog.go](../internal/catalog/catalog.go) | [runtime_catalog](../tests/integration/runtime_catalog_test.go) |
| **Strings and scalars**: `Scalar.eq`, `Scalar.new`, `String.FromScalars.new`, `String.concat`, `String.eq`, `String.length`, `String.lower`, `String.new`, `String.upper`, `UTF8.String.new` | [Contract](../specification.md#8-processors) | [catalog.go](../internal/catalog/catalog.go) | [runtime_catalog](../tests/integration/runtime_catalog_test.go) |
| **Integer arithmetic**: `Int.to_string`, `Math.add`, `Math.div`, `Math.eq`, `Math.gt`, `Math.gte`, `Math.lt`, `Math.lte`, `Math.max`, `Math.min`, `Math.mod`, `Math.mul`, `Math.product`, `Math.sub`, `Math.sum` | [Contract](../specification.md#84-math-processors) | [catalog.go](../internal/catalog/catalog.go) | [runtime_catalog](../tests/integration/runtime_catalog_test.go) |
| **Float arithmetic**: `Float.add`, `Float.div`, `Float.eq`, `Float.gt`, `Float.gte`, `Float.lt`, `Float.lte`, `Float.mul`, `Float.new`, `Float.sqrt`, `Float.sub`, `Float.to_int`, `Float.to_string`, `Int.to_float` | [Contract](../specification.md#241-binary64-arithmetic) | [catalog.go](../internal/catalog/catalog.go) | [float](../tests/integration/float_test.go) |
| **Lists**: `List.filter`, `List.flat_map`, `List.fold`, `List.length`, `List.map`, `List.new`, `List.unfold` | [Contract](../specification.md#87-list-processors) | [catalog.go](../internal/catalog/catalog.go) | [run](../tests/integration/run_test.go) |
| **Records and maps**: `Map.get`, `Map.has_key`, `Map.new`, `Record.merge`, `Record.new` | [Contract](../specification.md#86-record-and-map-processors) | [catalog.go](../internal/catalog/catalog.go) | [runtime_catalog](../tests/integration/runtime_catalog_test.go) |
| **Ranges**: `Range.contains`, `Range.is_empty`, `Range.new` | [Contract](../specification.md#85-range-processors) | [catalog.go](../internal/catalog/catalog.go) | [runtime_catalog](../tests/integration/runtime_catalog_test.go) |
| **Files**: `File.Path.new`, `File.append`, `File.read`, `File.write` | [Contract](../specification.md#91-file-adapter) | [catalog.go](../internal/catalog/catalog.go) | [activation](../tests/integration/activation_test.go) |
| **HTTP**: `HTTP.Response.Bytes.new`, `HTTP.Response.Text.new`, `HTTP.Server.new` | [Contract](../specification.md#92-http-adapter) | [catalog.go](../internal/catalog/catalog.go) | [serve](../tests/integration/serve_test.go) |
| **Tensor values and devices**: `Device.new`, `Tensor.device`, `Tensor.dtype`, `Tensor.new`, `Tensor.rank`, `Tensor.shape`, `Tensor.to`, `Tensor.to_float`, `Tensor.values` | [Contract](../specification.md#242-tensor-values-and-shape-boundaries) | [tensor.go](../internal/catalog/tensor.go) | [tensor_contract](../tests/integration/tensor_contract_test.go) |
| **Tensor arithmetic**: `Tensor.add`, `Tensor.broadcast`, `Tensor.div`, `Tensor.matmul`, `Tensor.mean`, `Tensor.mul`, `Tensor.relu`, `Tensor.reshape`, `Tensor.sqrt`, `Tensor.sub`, `Tensor.sum`, `Tensor.transpose` | [Contract](../specification.md#243-tensor-operation-set) | [tensor.go](../internal/catalog/tensor.go) | [tensor](../tests/integration/tensor_test.go) |
| **Autodiff**: `Autodiff.grad` | [Contract](../specification.md#245-reverse-mode-autodiff-and-backpropagation) | [autodiff.go](../internal/catalog/autodiff.go) | [autodiff_contract](../tests/integration/autodiff_contract_test.go) |
| **Parameters and gradients**: `Autodiff.stop`, `Gradients.add`, `Gradients.new`, `Gradients.scale`, `Gradients.values`, `Gradients.zero`, `Parameters.new`, `Parameters.values` | [Contract](../specification.md#246-parameters-and-gradient-lifecycle) | [parameters.go](../internal/catalog/parameters.go) | [parameters](../tests/integration/parameters_test.go) |
| **SGD**: `Optimizer.SGD.Config.new`, `Optimizer.SGD.State.new`, `Optimizer.SGD.step` | [Contract](../specification.md#247-sgd-and-adamw) | [optimizer.go](../internal/catalog/optimizer.go) | [optimizer](../tests/integration/optimizer_test.go) |
| **AdamW**: `Optimizer.AdamW.Config.new`, `Optimizer.AdamW.State.new`, `Optimizer.AdamW.step` | [Contract](../specification.md#247-sgd-and-adamw) | [optimizer.go](../internal/catalog/optimizer.go) | [optimizer](../tests/integration/optimizer_test.go) |

## Semantic Coverage

| Contract | Implementation | Integration evidence |
| --- | --- | --- |
| Lexical rules and grammar | [Lexer](../internal/syntax/lexer.go), [parser](../internal/syntax/parser.go) | [Conformance entry point](../tests/integration/cli_test.go), [fixtures](../testdata/conformance/manifest.json) |
| Types, named flows, fallibility, and Match | [Checker](../internal/check/check.go), [types](../internal/check/type.go), [Match](../internal/check/match.go) | [Verifier tests](../tests/integration/verifier_test.go) |
| Eager bindings, branch selection, and first failure | [Evaluator](../internal/eval/eval.go) | [Evaluation tests](../tests/integration/eval_test.go), [stored fallibility](../tests/integration/run_test.go) |
| File and HTTP activation | [Runtime operations](../internal/runtimeops/runtime.go) | [Activation](../tests/integration/activation_test.go), [HTTP](../tests/integration/serve_test.go) |
| Tensor shapes and numeric limits | [Tensor operations](../internal/runtimeops/tensor.go), [shape analysis](../internal/tensorfacts/analyze.go) | [Tensor contracts](../tests/integration/tensor_contract_test.go), [budgets](../tests/integration/tensor_budget_test.go) |
| Supported loss graph and derivative limits | [Autodiff plan](../internal/autodiff/plan.go), [checker](../internal/check/autodiff.go) | [Autodiff contracts](../tests/integration/autodiff_contract_test.go) |
| Backpropagation and shared derivative edges | [Interpreter](../internal/eval/autodiff.go), [VJP rules](../internal/derivative/vjp.go) | [Analytical and finite-difference tests](../tests/integration/autodiff_numeric_test.go), [context tests](../tests/integration/autodiff_test.go) |
| Gradient schema and stop-gradient | [Parameter operations](../internal/runtimeops/parameters.go) | [Gradient lifecycle](../tests/integration/parameters_test.go) |
| SGD, AdamW, immutable state, and config boundaries | [Optimizer operations](../internal/runtimeops/optimizer.go), [config](../internal/value/optimizer.go) | [Optimizer tests](../tests/integration/optimizer_test.go) |
| Native derivatives | [Gradient code generation](../internal/codegen/gradient.go) | Autodiff, Parameter, and optimizer tests through `make test-native` |
| Device authorization, storage, and transfer simulation | [Device backend](../internal/device/device.go), [compute boundary](../internal/device/compute.go) | [Device tests](../tests/integration/device_test.go), [resource tests](../tests/integration/device_resources_test.go) |
| Metal arithmetic, backpropagation, and optimizers | [Metal backend](../internal/device/metal_darwin.go) | [Software compute tests](../tests/integration/metal_compute_test.go), [training tests](../tests/integration/metal_training_test.go) |

## Example Results

The new examples use the same rank-one initial tensor `[0]` and target `[3]`.
Their loss flow returns a rank-zero mean squared error.
Each iteration recalculates gradients from the current optimizer state.
The initial gradient is `[-6]`.

| Result | SGD | AdamW |
| --- | --- | --- |
| Source | [training_sgd.lo](../examples/training_sgd.lo) | [training_adamw.lo](../examples/training_adamw.lo) |
| Learning rate | `0.05` | `0.1` |
| Weight decay | None | `0.01` |
| Initial loss | `9` | `9` |
| Final loss | `6.341224434436299e-9` | `0.0021785302087664604` |
| Successful updates | `100` | `100` |
| Original tensor after training | `[0]` | `[0]` |
| Trained tensor | `[2.9999204]` | `[2.9533253]` |

Both programs passed `lo check` and interpreter execution.
Their native artifacts produced identical typed JSON results on macOS arm64.
The checks asserted the initial loss, loss decrease, update count, and unchanged original tensor.
The exact source of each example also occurs in the reference.
Different configs make these demonstrations unsuitable for optimizer performance comparisons.

## Validation

Validation used Go 1.27.1, the CPU interpreter, native macOS arm64 artifacts, and software device tests.
No live device execution was necessary for this documentation task.
The Metal claims use source review, software integration evidence, and the existing [Metal acceptance report](metal-training.md).

The following repository checks passed:

```bash
make build
make test-integration TEST_ARGS='-run TestConformance\|TestVerifierBuiltinContracts\|TestAutodiff\|TestParameters\|TestOptimizer\|TestDevice -count=1'
make test-integration TEST_ARGS='-run TestFloat\|TestTensor\|TestEvaluator\|TestRun\|TestVerifierContracts\|TestVerifierEdgeContracts\|TestVerifierRejections\|TestMetalCompute\|TestMetalTraining -count=1'
make test-native TEST_ARGS='-run TestAutodiff\|TestParameters\|TestOptimizer -count=1'
```

Document checks verified all `lo` blocks through `lo check` and the quick-start result through `lo run`.
They also verified local links, heading fragments, catalog name coverage, and numbered rule identities.
The Governor check and `git diff --check` passed.

The official ASD-STE100 Issue 9 reference passed its pinned SHA-256 verification.
The language review covered new and changed prose against Part 1 and Part 2.
Unchanged prose has mechanical findings in the README and reference.
This audit does not claim full-document language compliance.
