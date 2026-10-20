# Lo Issues

This file tracks work against `specification.md` and selected or proposed language extensions.
D001, D002, and D003 record completed v0.3 specification work.
The training issues implement the P001 design after v0.3.

Scope note: v0.3 uses `T!` and `Error` for fallibility and keeps `Null` as an atomic type.
P001 completed the [training extension design](docs/training-extension-design.md).
That design selects the implementation contract for F002 through F011 and I001.
F002 through F004 add Float arithmetic, tensor values, and CPU tensor operations.
F005 through F007 add autodiff, Parameters, gradients, SGD, and AdamW.
F008 adds device grants, explicit transfers, and live resource receipts.
F009, F010, F011, and I001 retain device arithmetic, deferred execution, precision, and complete training acceptance.

## D001 - Make Recoverable Failure Semantics Mechanically Enforceable

Status: Closed
Priority: P0
Area: error model, type checker, runtime

Resolution:
Addressed in `specification.md` by adding the recoverable failure contract,
fatal-panic boundary, trace enrichment rules, required Error.kind values for
fallible standard operations, commit failure kinds, and conformance cases for
fallibility, self-handling flows, adapter handlers, and hidden host exceptions.

Reported concern:
Recoverable operational failures must not escape as hidden host exceptions, and
fatal host panics must be reserved for unrecoverable compiler or runtime
corruption.

Spec context:
Sections 3, 4.5, 5.3, 5.5, 6.7, 10.3, 12, and 15.3 define `T!`, `Error`,
automatic bubbling, self-handling flows, commit failure, and mandatory adapter
`onError` boundaries. Section 3 explicitly excludes user-visible generic
`Result<T>` and exception syntax from v0.3.

Task:
1. Add a normative recoverable-failure contract to `specification.md`.
2. State that implementations may lower `T!` to an internal tagged result, but
   must not expose `Result<T, E>` or exception syntax in the v0.3 surface
   language.
3. Define which failures are recoverable `Error` values for every fallible
   operation in section 18.3, including `UTF8.String.new`, `Math.div`,
   `Math.mod`, `Math.min`, `Math.max`, `Map.get`, `File.read`, and fallible
   list processor invocations.
4. Define the allowed fatal-panic boundary: parser/compiler bugs, violated
   runtime invariants, and unrecoverable host corruption only.
5. Specify how error trace frames are appended when an `Error` crosses a flow
   boundary, including handler invocation and adapter boundary behavior.
6. Add conformance tests for fallible bindings, fallibility-aware pipes,
   self-handling flows, adapter `onError`, top-level failed commits, and plain
   total flows that attempt to use unresolved `U!` values.

Acceptance criteria:
- A plain total flow that contains an unresolved `U!` expression is rejected by
  the type checker.
- A fallible flow automatically propagates unresolved `U!` failures and returns
  `T!`.
- A self-handling flow resolves bubbled errors through its declared pure total
  handler.
- An HTTP adapter cannot be constructed without an `onError` flow of signature
  `(Error) -> HTTP.Response`.
- Recoverable failures from the standard library surface as `Error` values, not
  hidden host exceptions.
- No v0.3 grammar, example, or standard-library signature introduces
  user-visible `Result<T, E>`, `try`, `catch`, `throw`, or expression-level
  recovery operators.

Out of scope:
- Adding a user-visible `Result<T, E>` type.
- Adding a new propagation operator beyond v0.3 automatic bubbling and the
  existing fallibility-aware pipe behavior.

## D002 - Lock Down Null and Absence Semantics

Status: Closed
Priority: P0
Area: type system, records, maps, standard library

Resolution:
Addressed in `specification.md` by making Null a non-inhabiting atomic type,
defining the null literal, requiring exact record fields, modeling absence
through Map.has_key and fallible Map.get, documenting the post-v0.3 optional
data deferral, and adding null/record/map conformance cases.

Reported concern:
The language must not allow implicit null references. Absence must be explicit
and statically visible before code interacts with the underlying value.

Spec context:
Section 6.1 defines `Null` as its own atomic type and says other types do not
inherit from `Null`. Section 3 excludes user-visible generic fallibility
containers and union types beyond the built-in fallible modifier `!`. Section
8.6 already uses `Map.get(...) -> ValueType!` for missing-key handling instead
of nullable returns.

Task:
1. Clarify that `Null` is a concrete value of type `Null`, not a subtype of
   every type and not an implicit inhabitant of `String`, `Int`, `Bool`,
   records, maps, lists, adapter types, or `Error`.
2. Define that v0.3 has no optional record fields. If a record type declares a
   field, construction must provide that field with exactly the declared type.
3. Define absence handling for maps and adapter-shaped data: use `Map.has_key`
   for presence checks and `Map.get(...) -> ValueType!` for fallible lookup.
4. Audit all examples and standard-library signatures so no operation silently
   accepts `Null` as a value of another type.
5. Add negative type-checking examples for passing `Null` to string, numeric,
   boolean, record, HTTP response, and file path operations.
6. Add a deferred design note for a possible post-v0.3 `Option<T>` or union
   model only if the spec later needs first-class optional data.

Acceptance criteria:
- `String.concat(String.new("a"), Null)` is rejected.
- `Math.add(Int.new(1), Null)` is rejected.
- A record value that omits a declared field is rejected.
- A record field declared as `String` cannot be initialized with `Null`.
- `Map.get` remains fallible and does not return a nullable successful value.
- No v0.3 type expression or example introduces `Option<T>` or nullable
  shorthand syntax.

Out of scope:
- Adding `Option<T>` to v0.3.
- Adding nullable shorthand or implicit nullability to any existing type.

## D003 - Formalize Host Execution Constraints and Grammar Testability

Status: Closed
Priority: P1
Area: execution model, grammar, conformance suite

Resolution:
Addressed in `specification.md` by adding deterministic evaluation and host
runtime constraints, explicitly deferring memory-management and concurrency
syntax to host implementation concerns, replacing the grammar sketch with a
normative grammar and disambiguation rules, cleaning up examples, and adding a
conformance matrix.

Reported concern:
The language needs a deterministic execution model, safe dataflow-oriented
concurrency boundaries, and a rigorous specification with atomic, testable,
non-contradictory constraints.

Spec context:
Sections 1, 2, 3, 5, 10, 15, and 16 define Lo as graph-first, immutable,
acyclic, and explicitly activated through `commit`. Section 3 excludes
concurrency syntax from v0.3. Section 6.6 allows lazy, incremental, or eager
list evaluation only when observable semantics do not change. Section 17 is
currently labeled a grammar sketch, not a proven grammar.

Task:
1. Add a host-runtime determinism section to `specification.md` stating that
   values are immutable after construction, graph evaluation strategy is not
   observable, and side effects occur only through evaluated `commit`
   statements.
2. Specify that commits execute exactly once per commit statement evaluation
   and that top-level commits execute in source order after binding resolution.
3. State that ownership, borrowing, reference counting, tracing garbage
   collection, OS threads, green threads, channels, and scheduler policy are
   implementation concerns in v0.3 unless and until the language exposes them
   in a later spec.
4. Add an explicit deferral note: v0.3 has no user-visible shared mutable memory,
   locks, thread creation, channel creation, async syntax, or concurrency
   annotations.
5. Promote the grammar from "sketch" to an implementable grammar by resolving
   known ambiguity risks, including `MatchExpression` recursion,
   `MatchCaseHead = Expression | Identifier | "_"`, pipe precedence,
   projection versus qualified calls, and the arity of qualified identifiers.
6. Create a conformance matrix that maps each grammar production and semantic
   rule to at least one positive example and one negative example where a
   negative example is meaningful.

Acceptance criteria:
- The spec clearly separates v0.3 surface semantics from host-runtime memory
  management choices.
- The spec states that concurrency primitives are not part of v0.3 surface
  syntax.
- Observable evaluation order is defined for commits and adapter activation.
- Parser implementers can build a parser from the grammar without relying on
  undocumented precedence or ambiguity resolution.
- Existing examples in section 19 parse under the finalized grammar.
- The conformance matrix covers failure propagation, null rejection, commit
  activation, flow purity, adapter handler requirements, list processor flow
  references, and match branch unification.

Out of scope:
- Adding ownership or borrowing syntax to v0.3.
- Adding green threads, channels, locks, async/await, or parallel execution
  syntax to v0.3.

## Planning

### Neural-network training context

The [recovered discussion](chatgpt-conversation://6acdcb11-e488-83e8-991b-6d19845bcc8f) supplies the earlier backpropagation context.
That account identifies graph-first purity, named flows, typed computation graphs, immutable bindings, and explicit `commit` effects as foundations.
It identifies gaps in numeric arithmetic, tensors, shapes, autodiff, Parameters, gradient control, and optimizers such as AdamW.
The recovered account does not establish detailed GPU requirements.
P001 resolved the GPU proposals through the selected extension design.
The design defines `Tensor<FP32>`, `Autodiff.grad`, `Parameters<R>`, `Gradients<R>`, and immutable optimizer state.
Record field names identify Parameters. There is no independent `Parameter.new` API.

Current evidence:
- Specification section 24 defines the implemented Float arithmetic and CPU tensor operations.
- The reference implementation uses finite IEEE 754 binary64 values for `Float`.
- Sections 8.7 and 23.1 already define `List.fold` and immutable accumulator state.
- Sections 5.6, 5.7, 22, and 23 define purity, value semantics, evaluation order, and failure boundaries.
- `README.md` describes the Go frontend, checked graph, inspector, interpreter, and current supported platforms.
- `docs/compiler-design.md` defines CLI diagnostics, authorization, inspection, serialization, and execution budgets.
- D001, D002, and D003 cover existing failure, absence, and execution contracts.
  The training issues extend these contracts rather than repeat their completed work.

Terms for these issues:
- A tensor is an immutable numeric array with an element type, shape, and device location.
- A shape is an ordered list of dimension sizes. Rank is the number of dimensions.
- Autodiff means automatic differentiation. Reverse-mode autodiff computes gradients through a graph in reverse dependency order.
- A VJP is a vector-Jacobian product. A gradient is a loss derivative with respect to a selected input.
- A Parameter is a selected trainable value. An optimizer computes new Parameters and new optimizer state from gradients.
- A kernel is a device program. Lowering converts checked graph operations into backend operations.
- FP32, FP16, and BF16 name numeric formats. A dtype is a tensor element type.
- A tape contains intermediate values needed for reverse-mode autodiff. Stop-gradient prevents derivative propagation through a selected value.
- An MVP is the smallest complete training example defined by I001.

Design references:
The [CUDA guide](https://docs.nvidia.com/cuda/cuda-programming-guide/index.html) describes device execution and memory.
The [asynchronous execution guide](https://docs.nvidia.com/cuda/cuda-programming-guide/02-basics/asynchronous-execution.html) describes streams and synchronization.
The [cuBLAS reference](https://docs.nvidia.com/cuda/cublas/index.html#results-reproducibility) describes library operations and reproducibility limits.
These references give vendor background.
The P001 design selects the specific build inputs and numerical contract.

Priority and order:
P0 resolves language decisions before implementation. P1 establishes the complete FP32 training path. P2 extends numerical precision after that path works.

| Order | Issues | Result |
| --- | --- | --- |
| 1 | P001 | Completed extension contract and validation plan |
| 2 | F002, F003 | Implemented numeric and tensor foundations |
| 3 | F004, F005, F006, F007 | CPU operations, derivatives, Parameters, and optimizers |
| 4 | F008, F009, F010 | Device resources, NVIDIA lowering, and execution boundaries |
| 5 | I001 | Deterministic tests and the complete training MVP |
| 6 | F011 | Mixed precision |

Dependency markers define prerequisites. Independent work can proceed after its prerequisites are completed.
Validation sections contain acceptance criteria for future implementation.
Software simulation or emulation is sufficient for device acceptance.
Where software cannot reproduce device behavior, available software evidence is sufficient with its limits recorded.
Physical-device access is not an acceptance prerequisite.

### Implementation review on October 13 2026

The review covered all 21 open entries: 13 non-recurring issues and eight recurring maintenance issues.
This audit preceded P001 design completion and F002 through F004 implementation later on the same date.
The table and probes below record that earlier state.
The current implementation supplies typed graphs, named flows, immutable values, error propagation, and list callbacks with explicit state.
These features provide foundations for the proposed training extension.
Reverse-mode autodiff and the complete training path remain unimplemented.
No open issue met all its acceptance criteria for closure.

| Issue | Implemented foundation | Remaining work |
| --- | --- | --- |
| P001 | Existing language and runtime contracts. | Accept the training types, operation signatures, numerical rules, effects, and backend contract. |
| F001 | Checked graphs, interpreter, and the native compilation plan. | Add native code generation, `lo build`, standalone artifacts, and performance qualification. |
| F002 | Finite binary64 `Float` construction and serialization. | Add floating-point arithmetic, explicit conversions, and the FP32 numeric contract. |
| F003, F004 | Immutable lists, records, and integer operations. | Add tensor types, shapes, and the CPU tensor operation set. |
| F005 | Resolved acyclic graphs and named flow references. | Add derivative graph construction, VJP rules, tape accounting, and gradient acceptance tests. |
| F006, F007 | Explicit immutable state and `List.fold`. | Add trainable Parameters, gradient operations, SGD, and AdamW. |
| F008, F009, F010 | Existing File and HTTP authorization and failure boundaries. | Add device resources, transfers, NVIDIA operations, and asynchronous device semantics. |
| I001 | Public CLI tests for the existing language. | Add the complete training example and its CPU and software GPU-backend acceptance evidence. |
| F011 | Existing binary64 values. | Add explicit mixed precision after I001 passes. |

Source evidence includes the [type definitions](internal/types/types.go), [operation catalog](internal/catalog/catalog.go), and [list evaluator](internal/eval/list.go).
The catalog contains no tensor, gradient, or optimizer operations.
The source tree contains no derivative or device backend.
The current `Parameter` type in `internal/types/types.go` describes a flow argument, not a trainable value.

The following focused public integration tests passed during this review:

```bash
make test-integration TEST_ARGS='-run "TestVerifierBuiltinContracts|TestRunBuiltinCatalog|TestRunValues|TestRunListProcessors|TestCheckedGraphContracts|TestEvaluatorImmutableResults" -count=1'
```

`make build` also passed.
Temporary source programs produced these public CLI results:

| Probe | Result |
| --- | --- |
| `Float.new(1.25)` | Returned `Float` value `1.25`. |
| `Math.add(Float.new(1.25), Float.new(2.5))` | Rejected with `check.type-mismatch`: expected `Int`, got `Float`. |
| Candidate `Tensor.new` call | Rejected with `resolve.unknown-name`. |
| Candidate `Autodiff.grad` call | Rejected with `resolve.unknown-name`. |
| Named `List.fold` callback over `1, 2, 3` | Returned `123` through explicit accumulator state. |
| `lo build examples/list_sum.lo` | Rejected with `cli.invalid-invocation`: unknown command `build`. |

The candidate-name probes do not select the APIs that P001 must define.
They confirm the absence of those proposed entry points in the current CLI.
The source and catalog review establish the wider implementation gaps.
The passing foundation tests do not establish gradient or training acceptance.

At the time of this audit, P001 remained the next prerequisite for F002 through F011 and I001.
F001 remains independent of the training path through `lo run`.
M400R, M401R, M402R, M403R, M404R, M405R, M406R, and M407R remain open as recurring tasks.

- [x] [P001] (P0) Validate the neural-network training extension
  Goal:
  Establish one reviewed contract for training while retaining Lo's inspectable graph model.

  Requirements:
  - Separate the recovered backpropagation needs from the new GPU proposals in each design decision.
  - Select one extension contract before implementation of dependent issues.
  - Preserve named flows, explicit context arguments, immutable bindings, `.new(...)` construction, and `T!` error propagation.
  - Define tensor types against section 6.6, which made `List<T>` the only ordered collection type when P001 began.
  - Select a canonical representation for dtype, shape, device location, Parameters, gradients, and optimizer state.
  - Validate `List.fold` with a pure named training flow and explicit state for bounded training iterations.
  - Classify device selection, allocation, transfers, execution, and result observation as value operations, boundary operations, or commit activation.
  - Keep `commit` for observable effects and adapter activation under the selected classification.
  - Reconcile this classification with pure list callbacks and the existing boundary-read summary.
  - Keep streams and synchronization inside the runtime, without user-visible async syntax.
  - Define determinism for each backend and numerical comparison between backends.
  - Record any proposed change to section 5.6 before tolerance-based CPU/GPU comparisons become language acceptance criteria.
  - Define tensor, tape, and optimizer resource accounting against the current execution budgets.
  - Limit the first implementation to dense tensors, one selected device, first-order derivatives, and the I001 network.

  Deliverables:
  - Produce a reviewed decision document with operation signatures, effect classifications, diagnostics, and source-section changes.
  - Add a decision table for unresolved items under `Open Decisions`.
  - Select the NVIDIA compilation route, operating system, host architecture, CUDA toolchain, and library versions.
  - Record how the selected backend coexists with the currently documented CPU platforms.
  - Keep F001 native executable generation independent of the training MVP through `lo run`.
  - Reuse shared runtime contracts where applicable without adding native artifact requirements to I001.
  - Record explicit deferrals for multiple devices, distributed training, sparse tensors, convolution, attention, and higher-order derivatives.
  - Map each accepted decision to F002 through F011 and I001.

  Validation:
  - Review a complete I001 graph, including state transitions, derivative operations, transfers, failures, and result publication.
  - Resolve every decision needed by the P1 issues before their implementation begins.
  - Demonstrate that the proposed training flow needs no captures, recursion, mutable bindings, or statement loops.
  - Confirm that CPU execution is an explicit backend choice, not recovery from a failed GPU request.
  - Define simulator, emulator, and controlled backend tests with their evidence limits.

  P001 completed its design on October 13, 2026.
  The [decision document](docs/training-extension-design.md) resolves all P1 decisions and maps each contract change to its implementation issue.
  It selects explicit CPU, CUDA, and software simulation backends without automatic substitution.
  It specifies API signatures, VJPs, optimizer equations, resource budgets, diagnostics, source changes, and fixed numerical acceptance limits.

  At P001 completion, the [complete source fixture](docs/examples/training-mvp.lo) passed `lo parse`.
  `lo check` rejected the unimplemented `FP32` type with `resolve.unknown-name`, as expected.
  `make build` passed.
  The [independent reference](docs/evidence/training-reference.py) passed its network, finite-difference, and AdamW assertions.
  Its FP32 loss after 200 updates was `0.00013215476064942777`, below the I001 threshold of `0.001`.
  These checks validate the design and reference arithmetic, not Lo training execution.
  Changed prose passed the mechanical language checks after review against the official reference.
  Existing tracker and README language findings remain outside this change.

- [x] [P002] (P1) {P001,F008} Plan an explicit Metal backend
  Goal:
  Define the implementation sequence for Apple GPU execution through Metal.

  Requirements:
  - Review current device resources, tensor operations, native builds, and numerical contracts.
  - Define explicit selection, authorization, storage, failures, and acceptance environments.
  - Identify unresolved arithmetic and toolchain decisions before implementation.
  - Implement Metal independently of pending CUDA work.

  Deliverables:
  - Add the [Metal implementation plan](docs/metal-backend-plan.md) and dependent implementation issues.

  Validation:
  - Verify the plan against current source and official Apple documentation.
  - Verify issue identifiers, dependencies, document links, and changed prose.

  P002 records the planned `metal:0` selector, synchronous FP32 execution, adapter route, and native artifact changes.
  F012, F013, F014, and I002 own implementation and acceptance.
  A direct Metal kernel executed successfully on the available Apple M5 Pro.
  This probe establishes local GPU access, without evidence of Lo Metal execution.
  The plan assigns subnormal arithmetic and toolchain decisions to implementation preparation.

- [x] [P003] (P1) {F007,I002} Define reusable trained models for CPU and Metal
  Goal:
  Select the contract for model inference and training resumption across processes.

  Requirements:
  - Define typed Parameter checkpoints and complete SGD and AdamW checkpoints.
  - Require explicit transfers and the current file authorization contract.
  - Select one canonical format with strict schema validation and bounded execution.
  - Prioritize this sequence before further NVIDIA implementation.

  Deliverables:
  - Add the [checkpoint implementation contract](docs/model-checkpoints.md) and F015, F016, and I003.

  Validation:
  - Review the contract against current value serialization, optimizers, resource accounting, and native compilation.
  - Verify identifiers against the active tracker and archive.

  P003 selects typed canonical JSON, CPU checkpoint boundaries, and explicit container transfers.
  F015, F016, and I003 own implementation and acceptance.

- [x] [P004] (P1) {F016,I003} Plan the standalone compiler and language evolution
  Goal:
  Define a native compiler migration that preserves the complete current language.

  Requirements:
  - Preserve the user's offline compile-and-link acceptance criterion.
  - Compare backend options and separate proposals from current behavior.
  - Include checkpoint bytes, container transfers, and exact optimizer resumption.
  - Define implementation dependencies and software acceptance environments.

  Deliverables:
  - Add the [standalone compiler plan](docs/standalone-compiler-plan.md) and dependent issues.

  Validation:
  - Review current source contracts and primary backend documentation.
  - Verify issue identifiers, dependency existence, dependency cycles, and document links.
  - Complete the documentation checks and independent architecture review.

  P004 records the proposed LLVM and LLD destination with a precompiled runtime.
  I004 starts the experiment.
  P005 owns the final backend, ABI, and platform decisions.
  This plan does not change the current compiler or language specification.

- [x] [P005] (P1) {I004,I005} Select the native backend and runtime contract
  Goal:
  Select one complete native toolchain before the runtime migration.

  Requirements:
  - Compare the four experiment cells and record unsupported claims or missing evidence.
  - Select exact tool versions, minimum OS versions, system libraries, and the host-to-target matrix.
  - Establish SDK-free macOS link inputs and their distribution authority.
  - Define value layout, ownership, callback lifetime, thread entry, and error status in the runtime ABI.
  - Prohibit unwinding across the ABI.
  - Audit reference cycles and preserve tensor resource-release points.
  - Select debug information, strict numeric lowering, and optimization constraints.

  Deliverables:
  - Add the backend decision, runtime ABI, complete input manifest, and measured comparison.

  The [runtime contract](docs/native-runtime-contract.md) records the selected design and current evidence.
  LLVM 21.1.8, LLD 21.1.8, and the precompiled Rust runtime are selected.

  Validation:
  - Verify complete executable links and runtime calls on both targets.
  - Verify the selected inputs without an installed SDK or undeclared compiler tools.
  - Resolve every decision needed by I006 through F017 before their implementation.
  - Record license inventory and source provenance for supplied third-party components.

  Completed on October 19, 2026.
  Both ABI targets passed callbacks, thread ownership, error status, budget failure, and cleanup.
  SDK-free Darwin platform links and all 20 ABI harness tests passed.
  The [final validation receipt](docs/evidence/compiler-evolution/validation.json) records successful CI and independent review.

- [ ] [P006] (P2) {P004} Specify modules, user types, generics, and packages
  Goal:
  Define application composition while preserving Lo effects and immutable values.

  Requirements:
  - Define modules, visibility, imports, type identity, records, and tagged variants.
  - Define exhaustive matching and bounded generic specialization.
  - Preserve transitive effects, callback restrictions, fallibility, and explicit capability grants.
  - Prohibit import-time effects and network access during compilation.
  - Define one package manifest, exact lockfile identities, and offline dependency inputs.
  - Keep recursion, closures, concurrency, and foreign-function access outside this specification.

  Deliverables:
  - Add specifications and accepted multi-module CLI, service, and training examples.

  Validation:
  - Review positive and negative examples for every new rule.
  - Verify that package boundaries cannot hide effects or grant capabilities.
  - Resolve each syntax and semantic decision before its dependent feature starts.

## Features

- [x] [F001] (P1) Build native Lo executables
  Goal:
  Compile checked Lo programs into standalone native executables with the current language and runtime contracts.

  Requirements:
  - Use the accepted [native compilation plan](docs/native-compilation-plan.md) for scope and stage dependencies.
  - Establish performance baselines and numeric build budgets before native implementation.
  - Separate shared execution contracts from interpreter graph traversal.
  - Generate direct Go functions, branches, local variables, and list loops.
  - Preserve errors, source context, logical costs, authorization, and effect order.
  - Support all current built-ins, File workflows, and HTTP services.
  - Qualify macOS arm64 and Linux arm64 through native execution.
  - Reject unsupported builds without interpreter execution.

  Deliverables:
  - A `lo build` command and standalone artifacts with `run` and `serve` commands.
  - A pinned runtime bundle and isolated artifact construction.
  - Native conformance tests and differential tests with independent expected results.
  - Recorded performance results, target qualification, and updated user documentation.

  Validation:
  - Execute artifacts without Lo, Go, the original source, or the repository checkout.
  - Verify all semantic, lifecycle, capability, and limit acceptance criteria in the plan.
  - Pass the agreed performance gates and repository-native validation.
  - Keep this issue open until implementation and its required evidence pass.

  F001 completed native compilation on October 14, 2026.
  `lo build` creates standalone artifacts for macOS arm64 and Linux arm64.
  Native execution covers all 100 built-in operations, CPU training, File workflows, and HTTP services.
  Both targets passed native semantic tests, generated artifact race tests, and the required performance gates.

  Callback execution improved by 2.68× on macOS and 2.48× on Linux across the fixed corpus.
  All measured builds met the source, time, memory, diagnostic-output, and artifact-size contracts.
  The [acceptance report](docs/native-compilation-results.md) records raw samples, uncertainty, source independence, and build isolation.
  The [final CI run](docs/evidence/native-ci.txt) passed bundle verification, formatting, static analysis, all tests, and the compiler build.

### Numeric and training foundations

F002 through F010 implement the selected [training extension contract](docs/training-extension-design.md).
F011 owns the deferred mixed-precision decisions after I001.

- [x] [F002] (P1) {P001} Add floating-point arithmetic and explicit conversions
  Goal:
  Extend existing `Float` support with the arithmetic needed for training.

  Requirements:
  - Define binary64 reference arithmetic and FP32 tensor arithmetic under the P001 numeric contract.
  - Specify addition, subtraction, multiplication, division, comparison, square root, and integer-to-float conversion.
  - Specify explicit dtype conversions, representable ranges, rounding, signed zero, and subnormal behavior.
  - Define overflow, nonfinite results, division by zero, and invalid square-root behavior through declared fallibility.
  - Keep current integer arithmetic exact and current `Float.new` construction consistent with its accepted contract.
  - Require explicit conversions between integer and floating-point domains.

  Deliverables:
  - Add canonical signatures, runtime operations, value serialization, and numeric conformance fixtures.
  - Record any accepted numeric change in the specification and compiler design.

  Validation:
  - Verify exact results for representable arithmetic inputs through `lo check` and `lo run`.
  - Verify rounding boundaries, conversion limits, underflow, overflow, zero division, and invalid square roots.
  - Verify that unresolved numeric failures are rejected in total flows and propagate through fallible flows.

  F002 implemented binary64 arithmetic and comparisons with explicit integer conversions.
  CLI tests passed rounding, signed-zero, subnormal, overflow, division, square-root, and fallibility cases.
  FP32 conversion and arithmetic use the same rules through F003 and F004.
  Specification section 24.1 and the compiler design record the numeric contract.
  The [initial failure](docs/evidence/float-red.txt) and [stack validation](docs/evidence/numeric-tensor-green.txt) record acceptance evidence.

- [x] [F003] (P1) {F002} Add tensor values and shape semantics
  Goal:
  Make tensor dimensions, numeric types, and storage requirements explicit and verifiable.

  Requirements:
  - Define immutable dense tensors with dtype, rank, dimension sizes, layout, and device location.
  - Separate statically known shapes from dimensions validated at input boundaries.
  - Define rank-zero tensors, zero-length dimensions, invalid dimensions, and element-count overflow.
  - Specify trailing-dimension broadcasting, reshape element counts, transpose axes, and matrix multiplication dimensions.
  - Select one canonical initial layout and define the semantics of views and storage sharing.
  - Validate dynamic shape inputs once at their boundary before core operations consume them.
  - Keep tensor storage identity and host pointers outside observable Lo values.

  Deliverables:
  - Add tensor constructors, metadata operations, type checking, diagnostics, and bounded serialization.
  - Define tensor allocation accounting for the existing execution budget.

  Validation:
  - Reject known shape conflicts through `lo check` with expected and actual dimensions.
  - Return the specified typed failure for invalid dynamic shapes before allocation or execution.
  - Verify broadcasting, reshape, transpose, rank-zero, and empty-dimension cases against fixed expected results.
  - Verify that two bindings retain their original values after an operation shares or reuses internal storage.

  F003 implemented Tensor and Device values, static shape facts, dynamic boundaries, metadata, and bounded serialization.
  CLI tests passed rank-zero, empty, overflow, reshape, transpose, broadcast, snapshot, and immutable-value cases.
  Inspection reports the CPU profile, known shapes and storage sizes, and explicit unknown metadata.
  Output weight and logical work receive checks before tensor allocation.
  F008 retains device sessions, transfers, and live resource receipts.
  The [tensor failures](docs/evidence/tensor-red.txt) and [stack validation](docs/evidence/numeric-tensor-green.txt) record acceptance evidence.

- [x] [F004] (P1) {F003} Add the CPU tensor operation set
  Goal:
  Provide an independent reference for the forward pass, gradients, and optimizer calculations.

  Requirements:
  - Implement elementwise addition, subtraction, multiplication, division, and square root.
  - Implement matrix multiplication, broadcasting, reshape, transpose, and reductions by sum and mean.
  - Implement ReLU with an explicit zero-input rule.
  - Compose mean squared error from these operations for I001.
  - Specify reduction axes, retained dimensions, accumulation dtype, and empty-input behavior.
  - Keep all operations pure and use the F002 and F003 failure contracts.

  Deliverables:
  - Add operation signatures, reference execution, and CLI integration fixtures for every accepted operation.
  - Record the operation set that F005 and F009 must support.

  Validation:
  - Compare non-square matrix products and broadcast reductions with independently calculated results.
  - Verify cancellation-sensitive reductions against the accepted reduction order and accumulation dtype.
  - Verify that an invalid operation stops dependent evaluation and retains its source context.

  F004 implemented the complete selected CPU operation set under `cpu-fp32-strict`.
  Independent CLI fixtures passed non-square products, broadcast operations, reductions, and intermediate numeric failures.
  The [forward example](examples/tensor_forward.lo) returned the independently calculated loss from mean squared error `3`.
  Budget and cancellation tests passed through the evaluator entry point.
  A numeric failure stopped a later commit and retained source and argument metadata.
  The [stack validation](docs/evidence/numeric-tensor-green.txt) records the final repository checks.
  F005 and F009 must preserve this operation set under their derivative and backend contracts.

- [x] [F005] (P1) {F004} Add reverse-mode autodiff for named flows
  Goal:
  Compute first-order gradients of a numeric rank-zero loss through a checked graph.

  Requirements:
  - Accept a pure named loss flow, explicit context inputs, and explicit differentiation targets.
  - Define a closed differentiable subset of the F004 operations needed by I001.
  - Add VJP rules for matrix multiplication, elementwise arithmetic, broadcasting, reshape, transpose, sum, mean, and ReLU.
  - Define the ReLU derivative at zero as a reviewed subgradient choice.
  - Accumulate derivative contributions from shared graph nodes exactly once per dependency edge.
  - Define zero gradients for disconnected selected inputs and rejection for nondifferentiable selected dtypes.
  - Reject unsupported operations with their source spans before backend execution.
  - Reject boundary reads and commit activation inside differentiated flows, including indirect calls.
  - Keep training iteration outside the differentiated loss graph for the MVP.
  - Explicitly defer differentiation through `Match.new` and list callbacks unless P001 defines their derivative rules.
  - Propagate forward failures without publishing partial gradients.
  - Bound tape lifetime to one derivative evaluation, subject to the accepted resource budget.

  Deliverables:
  - Add the gradient API, derivative graph construction, operation coverage table, and source-span mapping.

  Validation:
  - Compare each VJP with independent analytical results and central finite differences away from nondifferentiable points.
  - Fix finite-difference step sizes and error tolerances in fixtures before acceptance.
  - Verify shared nodes, broadcast bias, disconnected inputs, repeated calls, and nested named pure flows.
  - Verify that unsupported branches, callbacks, effects, and reads receive deterministic diagnostics.

  F005 implemented the exact-record gradient API and bounded derivative plans with source mappings.
  CLI tests passed analytical VJPs, finite differences, shared edges, explicit context, empty shapes, and repeated helper calls.
  Rejection, failure, cancellation, work-charge, and diagnostic-context budget tests passed.
  Specification section 24.5 defines the accepted contract.
  The [initial failure](docs/evidence/autodiff-red.txt) and [stack validation](docs/evidence/training-stack-green.txt) record evidence.

- [x] [F006] (P1) {F005} Add Parameters and gradient lifecycle operations
  Goal:
  Represent trainable values and gradient accumulation through immutable state.

  Requirements:
  - Give each Parameter an explicit stable key, dtype, shape, and value.
  - Define tied Parameter use as one selected value with accumulated derivative contributions.
  - Distinguish tied Parameters from separate Parameters with equal numeric values.
  - Return gradients in an exact structure keyed to the selected Parameters.
  - Define immutable zero-gradient construction, accumulation, reset, and stop-gradient operations.
  - Define minibatch weighting so accumulated gradients match the same complete batch loss.
  - Preserve the numeric value through stop-gradient while removing its derivative contribution.
  - Carry Parameters and gradient state explicitly through named flows and `List.fold`.

  Deliverables:
  - Add Parameter construction, gradient operations, and exact structural checks.

  Validation:
  - Verify that two microbatches produce the same weighted gradient as their combined batch within the accepted tolerance.
  - Verify a tied Parameter, separate equal-valued Parameters, disconnected Parameters, reset, and repeated gradient evaluation.
  - Verify that stop-gradient preserves forward results and yields zero contribution along the stopped path.
  - Reject missing keys, duplicate keys, incompatible shapes, and incompatible dtypes at their applicable validation boundary.

  F006 implemented immutable Parameters, gradients, reset through zero construction, and stop-gradient.
  CLI tests passed exact schemas, tied fields, separate equal values, and weighted microbatch accumulation through `List.fold`.
  Earlier values remained unchanged after accumulation and scale operations.
  Specification section 24.6 defines the accepted contract.
  The [initial failure](docs/evidence/parameters-red.txt) and [stack validation](docs/evidence/training-stack-green.txt) record evidence.

- [x] [F007] (P1) {F006} Add pure SGD and AdamW state transitions
  Goal:
  Compute new Parameters and optimizer state without mutable user storage.

  Requirements:
  - Implement plain SGD for the I001 baseline and AdamW for the requested optimizer path.
  - Define explicit learning rate, step count, moment values, beta values, epsilon, and weight decay.
  - Specify AdamW bias correction, epsilon placement, and decoupled weight decay mathematically.
  - Validate optimizer configuration once through its constructor.
  - Return updated Parameters and state as a new exact value with the same Parameter keys.
  - Retain previous Parameters and state without observable mutation.
  - Publish no partial update when a gradient or numeric operation fails.
  - Keep optimizer calculations pure and reserve persistent output for an explicit effect and `commit`.

  Deliverables:
  - Add canonical optimizer signatures, configuration constructors, and state-transition fixtures.

  Validation:
  - Compare one-step and multi-step results with independent numerical references for SGD and AdamW.
  - Verify nonzero weight decay, zero gradients, invalid configuration, step increments, and original-state preservation.
  - Verify that sequential `List.fold` steps receive the previous step's returned state.

  F007 implemented validated FP32 configs and immutable SGD and AdamW states.
  CLI tests passed independent transitions, moments, zero gradients, weight decay, invalid configs, and previous-state preservation.
  Schema, numeric failure, commit-order, and primitive work-charge tests passed.
  The CPU example completed 200 SGD updates and matched the independent final loss and all nine Parameter values.
  Specification section 24.7 defines the accepted contract.
  The [initial failure](docs/evidence/optimizer-red.txt) and [stack validation](docs/evidence/training-stack-green.txt) record evidence.
  The [training benchmark](docs/training-benchmark.md) supplies execution and allocation evidence for F001.

### NVIDIA backend

F008, F009, and F010 implement the P001 device and NVIDIA contracts.
F011 extends the accepted FP32 path with a later precision contract.

- [x] [F008] (P1) {F003} Add device resources and explicit tensor transfers
  Goal:
  Define device location and resource lifetime while preserving immutable value semantics.

  Requirements:
  - Implement the P001 device selection and activation contract for one NVIDIA device per training run.
  - Require host authorization for the resolved device before resource access.
  - Keep CPU execution as an explicit selection with the same accepted operation semantics.
  - Define device allocation, ownership, shared storage, lifetime, release, and memory budgets.
  - Account for Parameters, gradients, tape values, optimizer state, temporary buffers, and library workspace.
  - Define explicit CPU-to-GPU and GPU-to-CPU transfers with typed results and preserved dtype and shape.
  - Specify transfer completion, source lifetime, and cancellation cleanup.
  - Define errors for unavailable devices, incompatible locations, failed allocations, and failed transfers.
  - Use P001 classifications for recoverable errors and host policy failures.
  - Expose device requirements, transfers, and known allocation sizes through static inspection.

  Deliverables:
  - Add the device boundary, transfer operations, resource accounting, and software backend test interface.

  Validation:
  - Verify a transfer round trip with exact FP32 values, unchanged source values, and stable shape metadata.
  - Verify denial before resource access and deterministic failure for an unavailable selected device.
  - Inject allocation and transfer failures through the public execution path.
  - Verify release after success, failure, and cancellation with allocation receipts.
  - Verify that `lo check` and `lo inspect` operate without device initialization.

  F008 implemented grants, synchronous transfers, allocation receipts, storage limits, and static device summaries.
  Interpreter and native CLI cases passed, including exact FP32 results and HTTP requests.
  Injected failures verified cancellation cleanup, blocked later commits, and release of every allocation receipt.
  The full `make ci` gate and focused native race tests passed on macOS arm64.
  The [device resource contract](docs/device-resources.md) records interfaces and validation evidence.
  F009 retains NVIDIA arithmetic. F010 retains deferred execution.

- [ ] [F009] (P1) {F007,F008} Lower checked tensor graphs to NVIDIA operations
  Goal:
  Execute the accepted tensor operation set through a defined NVIDIA backend.

  Requirements:
  - Select one canonical route from Lo's checked IR to CUDA library calls and compiled kernels.
  - Use a reviewed cuBLAS or cuBLASLt mapping for matrix multiplication.
  - Add kernels for the remaining accepted operations, including derivative and optimizer arithmetic.
  - Record support by operation, dtype, layout, shape constraints, and target architecture.
  - Validate backend support before execution and report unsupported nodes with source spans.
  - Preserve data dependencies, branch regions, fallibility, source evaluation order, and commit boundaries during lowering.
  - Keep repeated training steps on device after explicit input transfers.
  - Allow fusion or buffer reuse only when results, failures, and value semantics remain unchanged.
  - Specify build inputs and artifact identity with the accepted toolchain and backend configuration.

  Deliverables:
  - Add the selected backend, build instructions, support matrix, and graph-to-operation inspection evidence.

  Validation:
  - Compile representative forward, derivative, and optimizer graphs with the selected NVIDIA toolchain.
  - Verify shapes, strides, transpose flags, dtypes, and dependency order at the library boundary.
  - Compare lowered execution in software with the CPU reference under the accepted numerical contract.
  - Verify that unsupported nodes produce a diagnostic rather than an implicit CPU execution path.
  - Record simulator coverage and unmeasured device behavior separately from compilation evidence.

- [ ] [F010] (P1) {F009} Preserve Lo semantics during asynchronous execution
  Goal:
  Keep device scheduling, synchronization, and failures consistent with Lo's observable execution rules.

  Requirements:
  - Define internal stream and event dependencies without new Lo concurrency syntax.
  - Complete required synchronization before host observation, returned values, resource release, and dependent commit activation.
  - Resolve earlier fallible work before any later observable boundary operation.
  - Preserve source-order error selection when independent operations fail in a different completion order.
  - Translate recoverable launch, transfer, and library failures into stable `Error.kind` values.
  - Retain originating graph nodes, operations, device context, and available source spans for deferred failures.
  - Define the handling of device loss and unusable contexts, including cleanup and later-call rejection.
  - Keep authorization denial, cancellation, and execution limits as host diagnostics under the accepted P001 contract.
  - Prevent result publication and later commits after an unresolved failure.
  - Expose backend selection, transfer boundaries, possible errors, and synchronization boundaries through inspection.
  - Specify deterministic reductions, library configuration, and accumulation precision for each supported execution profile.

  Deliverables:
  - Add scheduling rules, failure mappings, cleanup behavior, and inspection-schema extensions.

  Validation:
  - Inject out-of-order completions and verify identical published values and ordered effects.
  - Inject immediate and deferred failures and verify the originating trace and selected handler.
  - Verify that an earlier device failure prevents a later file commit.
  - Verify cancellation and policy denial bypass Lo handlers and release resources.
  - Repeat the same graph under fixed backend inputs and verify the P001 reproducibility contract.

- [ ] [F011] (P2) {I001} Add explicit mixed-precision training
  Goal:
  Extend the validated FP32 path with reviewed precision choices.

  Requirements:
  - Validate proposed FP16 and BF16 storage separately from compute and accumulation precision.
  - Specify explicit conversions, FP32 master Parameters, optimizer-state precision, and reduction precision.
  - Define overflow detection, loss scaling, gradient unscaling, and skipped-step state transitions.
  - Specify whether reduced-precision matrix modes, including TF32, are permitted in each execution profile.
  - Report unsupported precision requests explicitly under the selected backend contract.

  Deliverables:
  - Add the precision policy, numeric signatures, backend mappings, and mixed-precision fixtures.

  Validation:
  - Compare loss, gradients, and Parameter updates with the FP32 reference with fixed absolute and relative tolerances.
  - Verify overflow and underflow fixtures with the specified loss-scaling state transitions.
  - Verify that a skipped update preserves Parameters and changes only the state specified by the policy.
  - Record precision limitations of the selected software validation environment.

### Metal backend

The [Metal implementation plan](docs/metal-backend-plan.md) owns the planned backend contract and implementation sequence.

- [x] [F012] (P1) {P002,F008} Add explicit Metal access and tensor transfers
  Goal:
  Provide authorized Metal storage through Lo's public execution contract.

  Requirements:
  - Add `metal:0` and its exact host grant without automatic backend substitution.
  - Add the cgo and Objective-C adapter with an explicit Metal build target.
  - Pin the Metal language version, device features, SDK, compiler flags, and kernel identities.
  - Keep device initialization after authorization and outside static compiler commands.
  - Keep transfer bits, immutable values, storage budgets, and allocation receipts.
  - Define empty allocations, cancellation cleanup, unavailable devices, and unusable sessions.

  Deliverables:
  - Add `make build-metal`, interpreter transfers, static inspection, and device contract updates.

  Validation:
  - Start with failing CLI integration tests for explicit Metal transfers.
  - Verify exact transfer results with signed zero, subnormals, empty tensors, and unchanged source values.
  - Inject unavailable-device, allocation, transfer, cancellation, and cleanup failures through the public execution path.
  - Verify authorization before initialization and complete resource release.
  - Verify that a failed Metal request never selects another backend.
  - Use software tests as sufficient acceptance evidence under repository policy.

  F012 completed on October 15, 2026.
  The [transfer contract](docs/metal-transfers.md) documents explicit selection, the pinned build, resource accounting, errors, and cancellation.
  Software integration tests passed for authorization, transfers, failures, and cleanup.
  Local Metal tests passed with the race detector on the Apple M5 Pro.
  The tests preserved 513 finite FP32 values exactly and completed 200 transfers with bounded storage.
  The [final CI record](docs/evidence/metal-ci.txt) records successful software and native suites, static checks, and the standard build.
  F013 retains arithmetic and training. F014 retains native Metal artifacts.

- [x] [F013] (P1) {F012,F007} Execute FP32 tensor operations and training through Metal
  Goal:
  Execute tensor operations, derivatives, and optimizer transitions on the selected Metal backend.

  Requirements:
  - Resolve subnormal arithmetic and signed-zero rules before production kernel implementation.
  - Define the `metal-fp32` identity and exact repeatability contract.
  - Add closed operation descriptors shared by interpreter and native runtime code.
  - Implement the accepted tensor operations, VJPs, gradient operations, SGD, and AdamW.
  - Keep tensor state on Metal between explicit transfers.
  - Use fixed operation order and synchronous completion before subsequent observable effects.
  - Keep source traces, finite-value checks, logical charges, and immutable optimizer state.
  - Reject unsupported profiles and operations before their submission.

  Deliverables:
  - Add MSL kernels, an independent software executor, numeric fixtures, and specification updates.
  - Extend `make test-metal` with arithmetic and training tests.

  Validation:
  - Start with failing CLI integration tests for device arithmetic and training.
  - Compare primitives, derivatives, and optimizer updates with independent numerical references.
  - Verify exact fixtures for the selected subnormal and signed-zero behavior.
  - Verify fixed reductions and non-square matrix products.
  - Verify no implicit tensor transfers between training steps.
  - Inject command failures and verify blocked subsequent commits, source traces, and cleanup.
  - Verify bitwise repeatability across three runs within each fixed numeric profile.

  F013 completed on October 15, 2026.
  The [arithmetic contract](docs/metal-arithmetic.md) defines `metal-fp32`, typed computation, kernels, derivatives, gradient operations, SGD, and AdamW.
  Independent software tests and local Metal tests passed, including exact profile fixtures and the Go race detector.
  Three 200-step runs per environment reached loss `0.00013215476064942777` and matched all nine reference Parameter values.
  Each run used six uploads, six downloads, and 1,312 peak accounted bytes with complete cleanup.
  Failure tests verified source traces, cancellation, workspace limits, blocked later commits, and unusable-session rejection.
  The [final CI record](docs/evidence/metal-arithmetic-ci.txt) records successful static checks, software tests, native tests, and the standard build.
  F014 extends this contract to native artifacts. I002 retains complete training acceptance.

- [x] [F014] (P1) {F013,F001} Add Metal support to native executables
  Goal:
  Execute the Metal contract from standalone Lo artifacts on macOS arm64.

  Requirements:
  - Add explicit `lo build --device-backend metal` selection.
  - Reject unsupported host and target combinations before compiler execution.
  - Include the adapter, headers, and compiled kernels through an explicit runtime manifest.
  - Enable cgo for the selected Metal artifact configuration.
  - Record backend, compiler, SDK, kernel, and profile identities in artifact metadata.
  - Keep grants as execution inputs.
  - Keep build limits, cancellation, and atomic artifact publication.

  Deliverables:
  - Add native Metal construction, the updated artifact contract, and `make test-native-metal`.

  Validation:
  - Start with failing public build and artifact execution tests.
  - Verify execution without a source checkout or separate kernel files.
  - Verify interpreter and native parity within the same numeric profile.
  - Verify unavailable-device results and unchanged previous artifacts after build failure.
  - Verify software artifacts on the current macOS and Linux targets.

  F014 completed on October 15, 2026.
  The [native Metal contract](docs/native-metal.md) defines explicit selection, runtime manifests, pinned toolchains, and artifact metadata.
  Standalone Metal execution matched the interpreter across numeric fixtures, error traces, execution budgets, and three 200-step SGD runs.
  Controlled failures verified unavailable devices, cancellation, build limits, blocked commits, and unchanged previous artifacts.
  The local Metal suite, race checks, and Linux software tests passed.
  The [final CI record](docs/evidence/native-metal-ci.txt) records successful bundle verification, static checks, software tests, native tests, and compiler construction.
  I002 records complete training acceptance.

### Reusable trained models

- [x] [F015] (P1) {P003,F006,F001} Add typed Parameter checkpoints
  Goal:
  Save trained Parameters and restore them for inference in a separate process.

  Requirements:
  - Implement `Checkpoint.encode`, `Checkpoint.decode`, and `Parameters.to` under the P003 contract.
  - Infer the exact result type from a CPU checkpoint template.
  - Validate fields, shapes, dtype, finite elements, canonical bytes, and resource limits at import.
  - Keep explicit file reads, write construction, commit activation, and device authorization.
  - Reject unsupported values and prevent implicit device transfers.

  Deliverables:
  - Add catalog rules, checker support, runtime operations, inspector facts, and native code generation.
  - Update the specification and public integration tests.

  Validation:
  - Record failing public tests before implementation.
  - Verify exact round trips, separate-process inference, and malformed input diagnostics.
  - Verify blocked later commits, allocation cleanup, and execution limits.
  - Verify interpreter and native results.

  F015 completed on October 16, 2026.
  The [checkpoint contract](docs/model-checkpoints.md) and specification section 24.9 define the implemented operations.
  Public tests passed canonical imports, independent processes, schema diagnostics, FP32 bit checks, and the 16 MiB byte limit.
  Interpreter and native catalog tests passed.

- [x] [F016] (P1) {F015,F007,F014} Restore complete optimizer state on CPU and Metal
  Goal:
  Resume SGD and AdamW without loss of optimizer state.

  Requirements:
  - Extend the canonical codec to complete SGD and AdamW states.
  - Keep configuration, step count, moments, and beta powers.
  - Add explicit `Optimizer.SGD.State.to` and `Optimizer.AdamW.State.to` operations.
  - Transfer every state tensor through current device authorization and resource accounting.
  - Reject invalid state, mismatched optimizer kinds, and partial imported results.

  Deliverables:
  - Add state codec support, container transfers, and public CPU/Metal tests.

  Validation:
  - Compare resumed and uninterrupted SGD execution within each fixed profile.
  - Verify AdamW transitions against the independent fixture.
  - Verify upload/download receipts, bounded storage, cancellation, and transfer-failure cleanup.
  - Verify software and native execution without a physical-device prerequisite.

  F016 completed on October 16, 2026.
  Complete SGD and AdamW checkpoints restore configuration, steps, moments, and beta powers.
  Explicit container transfers keep FP32 values and use current device authorization.
  Controlled failures verified partial-transfer cleanup, cancellation, resource limits, and prevention of later commits.
  Software and local Metal tests passed with the Go race detector.

### Standalone compiler and application language

- [ ] [F017] (P1) {P005,I008,I009,I010} Supply a complete offline compiler distribution
  Goal:
  Compile and link Lo programs without separately installed development tools.

  Requirements:
  - Supply the selected backend, linker, precompiled runtime, and complete platform link inputs.
  - Preserve both arm64 targets and the selected host-to-target matrix.
  - Resolve tools inside the distribution without PATH lookup or downloads.
  - Reject missing, corrupt, or incompatible inputs.
  - Preserve cancellation cleanup and atomic output replacement.
  - Support installation relocation and empty caches.

  Deliverables:
  - Add package construction, input digests, provenance, license inventory, and noninteractive installation tests.
  - Document permitted operating-system dependencies and the exact target matrix.

  Validation:
  - Compile and link offline in declared clean environments without Go, Rust, C compilers, or developer SDKs.
  - Set CI=1 and close standard input for lifecycle commands and their children.
  - Execute copied artifacts without Lo, source, repository files, or compiler tools.
  - Verify relocation, corrupt inputs, cancellation, and preservation of an existing output after failure.
  - Verify software and Metal packaging independently without a physical-device gate.

- [ ] [F018] (P2) {P006,I011} Add modules and explicit visibility
  Goal:
  Compose programs from multiple source modules.

  Requirements:
  - Implement the selected import and visibility rules.
  - Preserve deterministic resolution and diagnostics across modules.
  - Propagate effects, fallibility, and capability summaries across every imported flow.
  - Reject import cycles under the selected contract and prevent import-time execution.

  Deliverables:
  - Add parser, resolver, verifier, inspector, interpreter, and native module support.

  Validation:
  - Execute a multi-module CLI and service through public commands.
  - Reject inaccessible names, invalid cycles, hidden effects, and malformed imports with source diagnostics.
  - Verify equivalent interpreter and native results.

- [ ] [F019] (P2) {F018,P006} Add user-defined records and tagged variants
  Goal:
  Express application data with checked user-defined types.

  Requirements:
  - Implement the selected type identity and constructor rules.
  - Preserve immutable fields and checked projections.
  - Require exhaustive matching under the selected variant contract.

  Deliverables:
  - Add type declarations, construction, pattern checks, runtime values, and native representations.

  Validation:
  - Execute imported type declarations and nested variants through the public CLI.
  - Reject missing fields, invalid alternatives, incomplete matches, and identity mismatches.
  - Verify stable error context and equivalent interpreter and native behavior.

- [ ] [F020] (P2) {F019,P006} Add generics with bounded specialization
  Goal:
  Reuse typed flows and data without runtime type ambiguity.

  Requirements:
  - Implement the selected type parameter and specialization rules.
  - Preserve effects, fallibility, and callback restrictions through specialization.
  - Bound specialization count and generated code size.

  Deliverables:
  - Add generic checking, specialization, diagnostics, and native generation.

  Validation:
  - Execute generic collection and variant examples across module boundaries.
  - Reject invalid type arguments and effect constraints.
  - Verify deterministic limit failures for excessive specialization.
  - Compare interpreter and native results and errors.

- [ ] [F021] (P2) {F018,F020,P006} Add reproducible offline packages
  Goal:
  Build applications from exact local dependency inputs.

  Requirements:
  - Implement one package manifest and lockfile contract.
  - Separate dependency retrieval from compilation.
  - Validate dependency identities and reject absent or changed locked inputs.
  - Prohibit build scripts that obtain implicit capabilities or execute during import.

  Deliverables:
  - Add package commands, local dependency bundles, resolution, and diagnostics.

  Validation:
  - Build a multi-package application offline from an empty compiler cache.
  - Reject missing, corrupt, conflicting, and cyclic dependency inputs under the selected contract.
  - Verify equivalent builds after package relocation.

- [ ] [F022] (P2) {F021} Add the initial application standard library
  Goal:
  Support reusable text, collections, JSON, file, and HTTP application operations.

  Requirements:
  - Specify each public operation before implementation.
  - Preserve explicit capabilities, immutable values, fallibility, and resource limits.
  - Use the selected modules and package contract.

  Deliverables:
  - Add maintained library modules, examples, public signatures, and diagnostics.

  Validation:
  - Execute CLI, service, and training applications with the library offline.
  - Verify malformed input, denied capabilities, limits, and cancellation through public commands.
  - Compare interpreter and native behavior.

- [ ] [F023] (P2) {F018,I011} Add formatter, test runner, editor, and debug support
  Goal:
  Make multi-module application development practical.

  Requirements:
  - Use the canonical parser and source locations in every tool.
  - Preserve program meaning during formatting.
  - Expose structured diagnostics, navigation, and source-level debug information.
  - Execute tests with explicit capabilities and reproducible inputs.

  Deliverables:
  - Add formatter and test commands, editor integration, and native debug support.

  Validation:
  - Verify formatting idempotence and unchanged checked behavior.
  - Verify navigation and diagnostics across imported modules.
  - Verify source locations for native breakpoints and error traces.
  - Run the test command offline with declared grants and controlled failures.

## Improvements

- [ ] [I001] (P1) {F007,F010} Qualify deterministic training with a small complete MVP
  Goal:
  Prove the complete training path through Lo's public CLI with reproducible software evidence.

  Requirements:
  - Implement the P001 design fixture with its selected `Forward`, `Loss`, and `TrainStep` signatures.
  - Use a dense network with two inputs, two ReLU units, and one linear output.
  - Use input rows `[-1,-1]`, `[-1,1]`, `[1,-1]`, and `[1,1]` with targets `0`, `2`, `1`, and `3`.
  - Set the first weight matrix to the identity, both hidden biases to zero, and output weights to `[0.25,0.25]`.
  - Set the output bias to zero.
  - Use mean squared error over all four rows and SGD with learning rate `0.05`.
  - Update all weights and biases for 200 steps through `List.fold` with explicit immutable state.
  - Use fixed input order and fixed initial values without a random initialization dependency.
  - Transfer inputs and initial Parameters once for the GPU path.
  - Keep forward operations, gradients, optimizer updates, and intermediate state on the selected backend.
  - Transfer final metrics and Parameters explicitly for host observation.
  - Add a separate short AdamW fixture with independently calculated expected updates from F007.
  - Record absolute and relative tolerances before test execution.
  - Use CPU execution, software simulation or emulation, and controlled backend failures as the acceptance environments.

  Deliverables:
  - Add the example, independent numerical references, CLI integration tests, and a reproducible validation recipe.
  - Record source revision, backend, compiler, library versions, numeric profile, inputs, and observed results.
  - Record device execution and performance as unmeasured when the software environment cannot establish them.

  Validation:
  - Verify the example through `lo parse`, `lo check`, `lo inspect`, and `lo run`.
  - Verify initial predictions `[0,0.25,0.25,0.5]` and initial mean squared error `2.46875`.
  - Verify the first SGD output weights `[0.33125,0.35625]` and output bias `0.125` within the accepted FP32 tolerance.
  - Verify finite gradients and loss at every step.
  - Verify final mean squared error below `0.001` after 200 updates.
  - Compare CPU and software GPU-backend losses, gradients, and final Parameters under the P001 numerical contract.
  - Repeat each fixed configuration three times and verify its specified reproducibility result.
  - Verify transfer receipts show no implicit host transfers between training steps.
  - Verify bounded live allocation across repeated steps and release after completion or failure.
  - Verify invalid shapes, unsupported operations, unavailable devices, transfer failures, and deferred failures through CLI diagnostics.
  - Run `make test-integration`, `make test-conformance`, and `make ci` at the implementation completion checkpoint.

- [x] [I002] (P1) {F014} Qualify complete Metal training
  Goal:
  Establish complete training acceptance for the explicit Metal backend.

  Requirements:
  - Reuse I001's fixed network, inputs, initial Parameters, and 200-step SGD procedure.
  - Include F007's independent AdamW fixture.
  - Use the Metal profile and fixed fixture tolerances from the implementation plan.
  - Accept simulation and controlled backend tests without a physical-device prerequisite.
  - Record available local Metal execution and its evidence separately.
  - Keep this work independent of I001's pending CUDA prerequisites.

  Deliverables:
  - Add the Metal example, acceptance report, public CLI tests, and current README instructions.

  Validation:
  - Verify parse, check, inspect, run, and native execution.
  - Verify finite gradients and final loss below `0.001` after 200 steps.
  - Verify six uploads, six downloads, bounded live storage, and complete cleanup.
  - Verify three runs with the same inputs within each fixed numeric profile.
  - Verify numerical agreement with independent references under the fixed tolerances.
  - Verify failure diagnostics and prevention of subsequent observable effects.
  - Run `make ci` at the final implementation checkpoint.
  - Report unmeasured Metal behavior explicitly when only software evidence is available.

  I002 completed on October 16, 2026.
  The [acceptance report](docs/metal-training.md) records the maintained example, fixed tolerances, software acceptance, and separate local Metal results.
  The independent executor, Metal interpreter, and native artifact each returned identical results across three runs.
  The final loss was `0.00013215476064942777` after 200 SGD updates.
  All nine final Parameters matched the independent binary64 and FP32 references.
  Diagnostic execution verified 200 finite losses and 1,800 finite gradient coordinates.
  The maintained example used six uploads, six downloads, and 1,312 peak accounted bytes with complete cleanup.
  Controlled failures at step 101 prevented final output and the later file commit.
  The exact Xcode pin is now 27.1 build 27A9275.
  The software and local Metal suites passed with the Go race detector.
  The [final CI record](docs/evidence/metal-training-ci.txt) records successful software and native validation.
  I001 remains open for its independent NVIDIA scope.

- [x] [I003] (P1) {F016,I002} Qualify reusable XOR models and training resumption
  Goal:
  Demonstrate the complete train, save, load, infer, and resume workflows.

  Requirements:
  - Reuse the existing XOR network, data, initial values, and 500-step SGD procedure.
  - Save Parameters for a separate inference example with no training steps.
  - Save complete state after 250 steps and resume for another 250 steps.
  - Keep CPU and Metal selection explicit.
  - Record available local Metal evidence separately from controlled software execution.

  Deliverables:
  - Add runnable examples, execution instructions, regression coverage, and maintained validation evidence.

  Validation:
  - Verify correct classification of all four XOR rows after a fresh-process load.
  - Verify exact resumed and uninterrupted results within the same numeric profile.
  - Verify CPU, controlled Metal, interpreter, and native execution.
  - Run final `make ci` after F015 and F016 implementation.

  I003 completed on October 16, 2026.
  Six examples implement CPU and Metal save, resume, and separate inference workflows.
  The 250-plus-250 result equals the uninterrupted 500-step XOR result in the tested profiles.
  Final loss is approximately `4.25e-13`, and all four XOR classifications are correct.
  Three local interpreter workflows and three native Metal workflows returned identical predictions and complete AdamW state.
  The [final CI record](docs/evidence/checkpoint-ci.txt) records successful software tests, native tests, static checks, and compiler construction.
  The [checkpoint report](docs/model-checkpoints.md#validation-evidence) links the source identity and scoped documentation review.

### Standalone compiler migration

- [x] [I004] (P1) {P004} Compare offline LLVM and Cranelift compile-and-link paths
  Goal:
  Establish complete toolchain feasibility on both current arm64 targets.

  Requirements:
  - Use equivalent arithmetic, branch, runtime-call, and error-status probes for LLVM and Cranelift.
  - Use the direct Cranelift object API.
  - Separate network-enabled tool preparation from offline compile, link, and execution.
  - Pin tools and dependency inputs.
  - Record their digests and exact commands.
  - Record object generation, linking, execution, isolation, and external inputs for each matrix cell.
  - Distinguish low-level smoke probes from Lo frontend integration and full runtime packaging.
  - Record SDK use explicitly.
  - Do not treat PATH filtering as proof of a clean environment.

  Deliverables:
  - Add maintained experiment sources, repeatable commands, and the [experiment report](docs/native-backend-experiment.md).
  - Record all four backend/target cells and the remaining distribution gaps.

  Validation:
  - Compare exact expected success and error results for both backends on both targets.
  - Verify offline execution and report its isolation boundary.
  - Verify representative C ABI runtime and platform links beyond freestanding return-only code.
  - Keep complete Lo runtime and clean-distribution acceptance in F017 and I011.
  - Record source mapping, build time, memory, artifact size, and debug feasibility where measured.
  - Keep standalone acceptance open until the full clean-environment procedure passes.

  Completed on October 19, 2026.
  LLVM and Cranelift passed offline object generation, direct LLD links, and execution on both arm64 targets.
  The [qualification receipt](experiments/native-backends/qualification-20261019.json) records all four cells and 21 passing harness tests.
  Full compiler distribution acceptance is still open under F017 and I011.

- [x] [I005] (P1) {P004} Record complete semantic migration coverage
  Goal:
  Define observable behavior independently of the replacement backend.

  Requirements:
  - Inventory every current built-in and public CLI contract.
  - Cover evaluation order, reads, commits, errors, cancellation, charges, and cleanup.
  - Include checkpoint bytes, container transfers, and exact resumed optimizer state.
  - Distinguish CPU and Metal numeric profiles and their accepted comparisons.

  Deliverables:
  - Add a coverage matrix and independent expected results for the migration corpus.

  Validation:
  - Run fixtures through the current interpreter and native public commands.
  - Verify every current catalog operation has positive and applicable negative coverage.
  - Verify resource-limit boundaries and prevention of later effects after failure.
  - Identify absent coverage before the new backend uses the corpus.

  Completed on October 19, 2026, with the documented coverage limits.
  All 385 required references passed in both lanes, with 993 successful test completions per lane.
  The [semantic receipt](docs/evidence/semantic-migration/validation.json) records source identities, full logs, and 13 passing reporter tests.
  The [coverage contract](docs/semantic-migration.md) assigns remaining qualification work to F017 and I011.

- [x] [I006] (P1) {P005,I005} Add a native lowering plan with explicit semantics
  Goal:
  Represent native control flow without dependence on Go source generation.

  Requirements:
  - Lower the checked graph into explicit calls, branches, errors, charges, and cleanup.
  - Preserve source locations and ordered cancellation boundaries.
  - Keep inspection independent of execution and machine-code generation.
  - Validate the lowering boundary once before backend consumption.

  Deliverables:
  - Add the lowering representation, verifier, selected backend emitter, and source maps.
  - Add a minimal test runtime for charged calls, status results, and callbacks under the P005 ABI.

  Validation:
  - Record failing public build tests before implementation.
  - Verify branch, callback, error, and charge order with independent fixtures.
  - Compare optimized and unoptimized test-runtime execution at exact budget boundaries.
  - Keep complete Lo runtime execution acceptance in I007 and I011.
  - Reject unsupported lowering without automatic fallback.

  Completed on October 20, 2026.
  The [native lowering implementation](docs/native-lowering.md) adds verified control flow, LLVM emission, source maps, and a bounded ABI runtime.
  All 44 object cells passed at O0 and O2 across both targets.
  Darwin execution passed 132 budget and cancellation cases plus two invalid-status cases.
  The [validation receipt](docs/evidence/native-lowering/validation.json) records successful CI, source identities, and independent review.
  Linux executable qualification and the full runtime port are downstream acceptance work.

- [x] [I007] (P1) {I006} Port core values and execution to the native runtime
  Goal:
  Execute core Lo values through the selected native ABI.

  Requirements:
  - Implement arbitrary-precision integers, Unicode, bytes, collections, records, and serialization.
  - Preserve finite Float values, error traces, handlers, logical charges, and flow depth.
  - Implement the selected ownership and callback lifetime rules.
  - Keep host failures separate from recoverable Lo errors.

  Deliverables:
  - Add the precompiled core runtime and generated ABI calls.

  Validation:
  - Execute the I005 core corpus through public compiler and artifact entry points.
  - Verify independent expected values, serialization, errors, and cancellation outcomes.
  - Verify bounded memory across repeated calls and complete cleanup after failure.
  - Verify no unwinding crosses the ABI.

  Completed on October 20, 2026.
  The explicit LLVM executable route links the precompiled Rust runtime.
  Core LLVM checks passed on Darwin arm64 and Linux arm64 in QEMU.
  Each target passed 10,240 requests with retained contexts and complete cleanup.
  Exact error traces, step limits, core operations, and metadata rejection passed.
  Independent review found no remaining material core defect.
  The [runtime receipt](docs/evidence/native-runtime/validation.json) records the qualification scope and source identities.
  Final CI passed.

- [x] [I008] (P1) {I007,F016} Port tensors, differentiation, optimizers, and checkpoints
  Goal:
  Preserve complete CPU training and model persistence in the native runtime.

  Requirements:
  - Port the current FP32 operation set, autodiff, Parameters, gradients, SGD, and AdamW.
  - Preserve tape lifetime, numeric checks, execution charges, and tensor admission.
  - Preserve canonical checkpoint bytes and typed import diagnostics.
  - Restore complete optimizer state without changes to resumed results.
  - Lower the checked autodiff plan with its source frames, charges, and release points.
  - Implement the confined file operations required by the existing checkpoint workflows before full I009 acceptance.

  Deliverables:
  - Add numerical runtime operations, checkpoint codecs, and native fixtures.

  Validation:
  - Compare numerical results with independent references under each selected profile.
  - Verify the existing 200-step training and 500-step XOR workflows.
  - Compare uninterrupted and resumed execution exactly within the same profile.
  - Verify malformed checkpoints, resource limits, cancellation, and failure cleanup.

  Completed on October 20, 2026.
  Existing LLVM training tests failed at the absent numerical runtime and specialized autodiff boundary.
  The implementation includes the confined file operations needed for checkpoint persistence.
  The 200-step training and 500-step XOR checks passed on both targets.
  CPU checkpoint resumption matched uninterrupted training exactly.
  All 13 analytical derivative and finite-difference fixtures passed through LLVM on Darwin.
  Linux checkpoint tests passed across eight separate guest processes.
  Resource lifetime, compiled work charges, cancellation, and failure cleanup passed.
  Canonical bytes and exact malformed checkpoint diagnostics passed.
  Independent review found no remaining material defect.
  The [runtime receipt](docs/evidence/native-runtime/validation.json) records the complete evidence and qualification limits.
  Final CI passed.

- [x] [I009] (P1) {I007} Port confined files, processes, and HTTP execution
  Goal:
  Preserve adapter authority and lifecycle behavior without the Go runtime.

  Requirements:
  - Preserve opened-root path confinement and protection against path replacement races.
  - Preserve ordered commits, request isolation, listeners, cancellation, and orderly shutdown.
  - Preserve process grants, confinement, limits, receipts, termination, and descendant cleanup.
  - Keep current platform availability and unavailable errors for process operations.
  - Supply required process support without a dependency on the Lo installation during artifact execution.
  - Keep capabilities absent until explicitly granted at execution.
  - Release per-request storage during service execution.

  Deliverables:
  - Add native file, process, and HTTP adapters with the selected runtime boundary.

  Validation:
  - Execute authorized and denied file operations through artifact commands.
  - Verify traversal, symlink replacement, failed writes, and prevention of later commits.
  - Exercise concurrent loopback requests, cancellation, shutdown, and bounded resource use.
  - Verify process execution and denied authority through public artifact commands and controlled host implementations.
  - Compare CLI outputs, errors, and lifecycle events with current behavior.

  Completed on October 20, 2026.
  Native file and HTTP checks completed successfully on Darwin arm64 and Linux arm64 under QEMU.
  Darwin process checks verified confinement, termination, cancellation, ownership, safe diagnostics, and the embedded launcher.
  Linux keeps the current process-unavailable result.
  The standalone launcher check used an empty PATH after removal of the original artifact.
  File checks verified ordered removal, observation identity, replacement rejection, blocked I/O, and prevention of later commits.
  Each target completed 1,000 HTTP requests with complete request cleanup.
  Native HTTP checks verified TLS, framing, trailers, request deadlines, and shutdown.
  Regression checks verified backpropagation, 200-step training, 500-step XOR, and checkpoint resumption.
  Independent review found no unresolved material defect.
  Final CI completed successfully.
  All 11 tests for the runtime build tools completed successfully.
  The [adapter guide](docs/native-adapters.md) describes the implementation.
  The [adapter receipt](docs/evidence/native-adapters/validation.json) records source identities, results, and qualification limits.
  I010, F017, and I011 keep their existing scope.

- [ ] [I010] (P1) {I008,I009,F014} Port device resources and Metal execution
  Goal:
  Preserve explicit device behavior in the replacement runtime.

  Requirements:
  - Preserve grants, transfers, scoped release, and the resource ledger.
  - Preserve the fixed Metal numeric profile and precompiled kernel identity.
  - Preserve complete Parameter and optimizer container transfers.
  - Reject unsupported devices without automatic CPU substitution.

  Deliverables:
  - Add the native device boundary, Metal adapter, and controlled software implementations.

  Validation:
  - Verify exact transfer receipts, failure cleanup, and prevention of subsequent commits.
  - Verify profile-specific arithmetic and optimizer fixtures through software simulation or emulation.
  - Verify unavailable-device errors through the public artifact command.
  - Record unmeasured Metal behavior without requiring physical devices.
  - Verify Metal link inputs independently from software artifact inputs.

- [ ] [I011] (P1) {F017,I005} Qualify the standalone compiler and remove Go generation
  Goal:
  Replace the current native build path after complete semantic acceptance.

  Requirements:
  - Preserve the complete current catalog, CLI behavior, and documented target matrix.
  - Use the plan's clean-environment procedure for both targets.
  - Retain the interpreter as an explicit current-language implementation.
  - Remove the Go generator, source bundles, and installed-Go build requirement after parity.
  - Keep no compatibility backend or automatic fallback.

  Deliverables:
  - Add the acceptance report, updated user documentation, and removal of obsolete native build code.

  Validation:
  - Pass the complete I005 corpus with independent and differential comparisons.
  - Compile offline and execute copied artifacts in a second environment without Lo or compiler tools.
  - Verify optimization preserves numeric profiles, charges, errors, and effect order.
  - Verify the declared performance and build-resource contracts.
  - Run final make ci and the explicit clean-environment targets before closure.

- [ ] [I012] (P2) {F022,F023} Qualify complete application development
  Goal:
  Demonstrate the selected language extensions in maintained applications.

  Requirements:
  - Cover a multi-module CLI, an HTTP service, and a training application with checkpoint resumption.
  - Use user-defined types, generics, packages, library modules, and development tools.
  - Preserve explicit capabilities, deterministic inputs, and offline compilation.

  Deliverables:
  - Add applications, documented commands, conformance coverage, and measured acceptance evidence.

  Validation:
  - Build all applications offline with the installed distribution on both supported targets.
  - Execute artifacts without Lo and compare independent expected results.
  - Verify package errors, denied capabilities, execution limits, and diagnostics across modules.
  - Verify the formatter, test runner, editor navigation, and source-level debugging on these applications.

## Maintenance

### Recurring

- [ ] [M400R] (P2) Backlog hygiene and archive
  Goal:
  Keep the issue tracker reliable, readable, and focused on active work while preserving resolved history in the appropriate archive.

  Requirements:
  - Cadence: run weekly during active development and before each release cut.
  - Validate section names, identifier prefixes, recurrence suffixes, priority markers, dependencies, and duplicate IDs against the current `issues-md-format.md`.
  - Reconcile stale statuses, duplicate issues, broken references, obsolete instructions, and entries filed under the wrong section.
  - Move completed non-recurring history to the repository issue archive or durable documentation when the active tracker becomes noisy.
  - Keep active, blocked, planning, and recurring entries visible in `ISSUES.md`.

  Deliverables:
  - Normalized `ISSUES.md` structure and statuses.
  - Updated issue archive or docs when completed entries are removed from the active tracker.
  - A short `Last run:` note summarizing the cleanup and any follow-up issues filed.

  Validation:
  - Re-read `ISSUES.md` after edits and confirm every issue is under the right section with a unique section-aware ID.
  - Confirm recurring entries remain open and keep the `R` suffix.
  - Confirm no active, blocked, recurring, or planning work was archived.

- [ ] [M401R] (P2) Polish open issues
  Goal:
  Keep unresolved work executable by making each open issue concrete, ordered, and testable.

  Requirements:
  - Cadence: run weekly during active development and before handing a repo to automated execution.
  - Review every unresolved non-recurring issue for missing context, dependencies, repro steps, acceptance criteria, and validation expectations.
  - Make priorities concrete and ensure each open issue has actionable deliverables.
  - Merge duplicate open issues or add explicit dependency links when separate entries must remain.
  - Do not close or implement issues as part of this polish pass unless that work is separately requested.

  Deliverables:
  - Open issues with enough detail for a person or agent to execute without rediscovery.
  - New or updated dependency markers where ordering matters.
  - A short `Last run:` note listing the number of issues polished and any blockers found.

  Validation:
  - Sample the open entries after the pass and confirm each has clear next actions and validation expectations.
  - Confirm no recurring runbook was marked complete.
  - Confirm duplicates were merged or explicitly cross-referenced.

- [ ] [M402R] (P2) Architecture and policy review
  Goal:
  Catch architecture, policy, and workflow drift before it becomes hidden maintenance debt.

  Requirements:
  - Cadence: run monthly, before large refactors, and after major framework or runtime changes.
  - Review the codebase, docs, and workflow against `AGENTS.md`, `POLICY.md`, stack guides, and the current architecture notes.
  - Look for drift from forward-only contracts, edge-validation boundaries, smart-constructor usage, testing policy, and module ownership.
  - Record findings as new Maintenance issues with concrete scope, priority, and validation.
  - Close the pass with a no-action note only when the review finds no actionable drift.

  Deliverables:
  - New Maintenance issues for each actionable architecture or policy drift finding.
  - Updated notes on areas reviewed and areas intentionally left unchanged.
  - A short `Last run:` note with the review scope and outcome.

  Validation:
  - Confirm every finding is represented as an issue with owner-readable context and validation criteria.
  - Confirm no implementation changes were mixed into the review runbook unless separately requested.
  - Confirm all recurring runbooks remain open.

- [ ] [M403R] (P1) Dependency and security audit
  Goal:
  Keep third-party dependencies, runtime versions, and security-sensitive configuration within the current supported contract.

  Requirements:
  - Cadence: run weekly for active apps and before each release cut.
  - Inspect package managers, lockfiles, language toolchains, container bases, and generated clients for known vulnerabilities or stale direct dependencies.
  - Review auth, secret, CORS, CSP, SQL, network, and permission-sensitive configuration for drift from the current contract.
  - Prefer current supported dependencies; do not add compatibility shims for obsolete dependency behavior.
  - File separate Maintenance or BugFix issues for each actionable vulnerability, unsupported runtime, or security-contract gap.

  Deliverables:
  - Documented audit commands or data sources used for the pass.
  - Updated issues for each actionable dependency or security finding.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the repository-native audit, lint, or dependency checks used for the pass.
  - Confirm every finding is either filed, fixed under a separate issue, or explicitly marked not applicable with evidence.
  - Confirm no secrets or private payloads were written into the tracker.

- [ ] [M404R] (P1) CI, release, and artifact health
  Goal:
  Keep the repository's validation, release, publication, and generated artifact surfaces trustworthy.

  Requirements:
  - Cadence: run before every release, publish, or deploy, and weekly for critical services.
  - Verify repository-native CI, lint, format, coverage, release, publish, Docker image, Pages, and artifact workflows still match the documented contract.
  - Check generated artifacts, release tags, published images, and Pages outputs for source-to-public drift.
  - File concrete follow-up issues for failing gates, stale artifacts, missing release prerequisites, or undocumented workflow changes.
  - Do not perform production deployment from this runbook unless the operator explicitly requests that deployment.

  Deliverables:
  - Recorded gate status and artifact surfaces inspected.
  - Follow-up issues for each reproducible CI, release, publish, or artifact drift problem.
  - A short `Last run:` note with commands run and any skipped surfaces.

  Validation:
  - Use repository-native `make` targets or documented release helpers for checks.
  - Confirm release and deployment ownership boundaries remain separate.
  - Confirm public or published artifacts match the intended source revision when that surface is inspected.

- [ ] [M405R] (P1) Code contract and static hygiene
  Goal:
  Keep source contracts explicit, current, and statically guarded against policy drift.

  Requirements:
  - Cadence: run monthly and before large refactors.
  - Scan for dead code, unused exports, duplicated literals, silent fallbacks, legacy aliases, compatibility reads, and zero-but-invalid domain states.
  - Check static analysis, coverage, schema, and contract guards that are supposed to prevent drift.
  - File focused Maintenance issues for each concrete violation instead of broad cleanup placeholders.
  - Keep the current canonical contract only; do not preserve obsolete behavior unless a product requirement explicitly says so.

  Deliverables:
  - Issue entries for each actionable static hygiene or contract violation.
  - Notes on static tools, searches, and contract guards used during the pass.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the relevant static checks, contract tests, or repository searches used to identify drift.
  - Confirm every finding has a narrow follow-up issue and does not duplicate existing backlog work.
  - Confirm no implementation changes were mixed into the audit unless separately requested.

- [ ] [M406R] (P1) Production drift and health
  Goal:
  Detect when production, public, or scheduled runtime state has drifted from the intended repository contract.

  Requirements:
  - Cadence: run weekly for deployed services and after each publish or deploy.
  - Compare current source, runtime configuration, published images, public routes, scheduled jobs, and health checks for drift.
  - Inspect real operator-facing surfaces rather than assuming merged source is deployed.
  - File follow-up issues for stale images, stale Pages output, missing routes, failed monitors, invalid production config, or undocumented runtime differences.
  - Stop before production deploy or destructive operator actions unless the operator explicitly requests them.

  Deliverables:
  - Recorded source revision, public artifact, route, image, or health surfaces inspected.
  - Follow-up issues for each source-to-runtime drift finding.
  - A short `Last run:` note with evidence links or commands used.

  Validation:
  - Verify inspected production or public surfaces directly where access is available.
  - Confirm any deploy-required finding is filed with the exact publish/deploy boundary and owner.
  - Confirm no production state was changed by the audit unless explicitly requested.

- [ ] [M407R] (P2) Documentation and runbook hygiene
  Goal:
  Keep durable documentation and runbooks aligned with the current behavior users and operators actually rely on.

  Requirements:
  - Cadence: run before release cuts and after merge bursts that change user-facing or operator-facing behavior.
  - Review README, ARCHITECTURE, PRD, CHANGELOG, docs, runbooks, setup guides, and local workflow notes for stale behavior or missing new contracts.
  - Update docs when closed issues changed durable behavior, public APIs, operator workflows, release semantics, or deployment expectations.
  - Remove or rewrite stale instructions instead of preserving obsolete alternatives.
  - File separate issues for documentation gaps that require product or implementation decisions.

  Deliverables:
  - Updated documentation or filed follow-up issues for each gap.
  - A short `Last run:` note listing docs inspected and changes made.
  - Cross-references from archived issue history to durable docs when useful.

  Validation:
  - Check links, command names, paths, and public contract descriptions touched by the pass.
  - Confirm docs describe the current canonical path only.
  - Confirm issue archive and active tracker references remain consistent.
