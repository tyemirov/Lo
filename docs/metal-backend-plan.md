# Metal Backend Implementation Plan

P002 records the plan for an explicit Apple Metal backend.
F012, F013, F014, and I002 own implementation and acceptance.
F012 implements Metal transfers in the interpreter through the [current transfer contract](metal-transfers.md).
The remaining sections retain the planned arithmetic, native artifact, and training work.

## Goal and Scope

Execute Lo tensor operations and training on Apple silicon through `metal:0`.
Use the existing language, device grants, explicit transfers, and resource ledger.
Support the interpreter first, then native executables on `darwin/arm64`.
Keep one compute device per invocation.

The first implementation uses FP32 and synchronous operations.
It includes the current tensor operations, reverse-mode autodiff, gradient operations, SGD, and AdamW.
Multiple devices, mixed precision, asynchronous execution, and performance optimization remain outside this plan.
F009 and F010 keep their CUDA scope and are not prerequisites for Metal.

## Current Evidence

The standard backend implements `cuda-sim:0` transfers and rejects authorized `cuda:0` access with `Device.Unavailable`.
F012 adds the explicit Metal build and bit-preserving transfers.
Device arithmetic currently returns `Device.UnsupportedOperation`.
The backend interfaces expose identity, allocation, upload, download, and cleanup.
They do not yet expose tensor computation.

On October 15, 2026, a direct Swift probe compiled and executed a Metal kernel on this Mac.
The Apple M5 Pro has 20 GPU cores and reports Metal 4 support.
The kernel doubled `[1,2,3,4]` and returned `[2,4,6,8]`.
This result establishes local Metal access. It does not establish Lo execution, training, or performance.

The host has macOS 27.0.1, Xcode 27.0, and the macOS 27.0 SDK.
The Metal Toolchain component was installed during F012 implementation.
The existing native builder disables cgo for production artifacts.
The runtime bundle currently includes only Go files from repository runtime packages.
F014 must change both contracts for Metal artifacts.

## Explicit Selection

Use the following planned source and execution forms:

```lo
device = Device.new("metal:0")
remote = Tensor.to(source, device)
result = Tensor.to(remote, Device.new("cpu"))
```

```bash
make build-metal
./bin/lo-metal run examples/training_metal.lo --allow-device metal:0 --format json
```

- Accept `metal:0` as the only Metal selector.
- Reject device initialization without the matching `--allow-device metal:0` grant.
- Reject aliases, automatic selection, duplicate grants, and multiple compute locations.
- Resolve `metal:0` to the system's default Apple GPU after authorization.
- Record the selected device identity in execution evidence.
- Return `Device.Unavailable` when the build or host cannot provide Metal.
- Return `Device.UnsupportedProfile` when the selected device cannot provide the necessary numeric profile.
- Never substitute CPU, CUDA, or simulation after a Metal request fails.
- Keep `lo parse`, `lo check`, and `lo inspect` free of device initialization.

The standard build keeps explicit CPU and simulation execution.
The Metal build adds a separately selected capability.
Static inspection must report the selector, grant, transfers, and operation support without resource access.
Transfer tensors explicitly to `cpu` before host observation.

## Adapter and Build Route

Use Go, cgo, a narrow C ABI, and an Objective-C adapter to the Metal framework.
Compile repository-owned MSL kernels into an embedded `metallib` during the Metal build.
Use an explicit manifest for adapter sources, headers, kernels, and compiled resources.
Keep compiler packages and private files outside the runtime bundle.

The initial build target is `darwin/arm64` with macOS 27 and the selected Xcode 27 toolchain.
F012 records exact compiler versions, SDK identity, build flags, source hashes, and artifact hashes.
Missing build inputs stop the Metal build.
The build uses `CGO_ENABLED=1` and the `metal` Go build tag.
Other builds implement the explicit unavailable-device result without an Apple framework dependency.

Use one command queue per invocation and one completed operation before the next operation starts.
Keep Objective-C objects behind owned opaque handles.
Do not keep Go pointers across C calls.
Keep device initialization lazy and after authorization.
Use the same adapter and operation descriptors in interpreter and native execution.

Repository-owned kernels provide the first matrix multiplication and reduction implementations.
Fixed traversal order makes their numerical behavior reviewable.
Apple library acceleration can be evaluated in a subsequent change with its own numeric profile.

## Storage and Execution

F012 uses `MTLStorageModeShared` buffers with device storage and transfer staging in one reserved allocation.
Apple documents CPU and GPU access to this storage in its [storage guide](https://developer.apple.com/documentation/metal/choosing-a-resource-storage-mode-for-apple-gpus).
Shared memory does not change Lo's explicit transfer or immutable value rules.

- Keep CPU and Metal allocations distinct in the resource ledger.
- Copy bytes only at explicit transfer boundaries.
- Keep every FP32 bit during transfers, including signed zero and subnormals.
- Keep source values after transfers and optimizer updates.
- Share storage only for immutable views allowed by the tensor contract.
- Complete device work before numeric observation, resource release, return, or a subsequent observable effect.
- Charge actual buffer reservations, temporary buffers, and execution status storage to the existing budgets.
- Give empty tensors valid metadata and receipts without an invalid Metal allocation or dispatch.
- Release each allocation once after success, failure, or cancellation.

Arithmetic kernels must not read tensor values on the CPU.
Small execution status results can cross the adapter boundary for error detection.
These status results must contain no tensor payload.
Training receipts must show no implicit tensor transfers between steps.

Cancellation stops new submissions immediately.
Already submitted work keeps its resources until completion.
Cancellation prevents result publication and subsequent effects, even when the submitted command completes successfully.
The first implementation does not promise immediate GPU preemption.

## Operation and Numeric Contract

F013 extends the resource interface with closed, typed operation descriptors.
It must reuse tensor shape preparation, logical charges, source spans, and validated dtype information.
It must remove the current NVIDIA-specific arithmetic rejection from shared execution code.
Backend support checks must precede submission of the affected operation.

| Operation group | First Metal scope |
| --- | --- |
| Elementwise operations | Current FP32 arithmetic, ReLU, square root, and broadcasting. |
| Layout operations | Reshape and transpose with the current shape and axis rules. |
| Reductions | Sum and mean with fixed traversal order and current empty-region rules. |
| Matrix multiplication | Rank-two, row-major FP32 inputs and FP32 accumulation. |
| Derivatives | Current VJPs, shared contributions, stop-gradient, and gradient operations. |
| Optimizers | Immutable SGD and AdamW transitions with device-resident tensor state. |

The proposed numeric profile is `metal-fp32`.
Its identity includes Lo, kernels, compiler options, macOS build, GPU identity, and effective operation configuration.
Each fixed identity must produce the same numeric bits across three runs with the same inputs.
Interpreter and native execution must agree within the same identity.
Tolerance comparisons between profiles do not change Lo equality.

Select safe math, precise functions, and disabled contraction for the first kernel build.
Use fixed reductions without unordered floating-point atomics.
Apple defines these controls in the [MSL specification](https://developer.apple.com/metal/Metal-Shading-Language-Specification.pdf), sections 1.6 and 8.
The specification lets subnormal arithmetic flush to zero, including with precise functions.
Safe math alone cannot establish the CPU subnormal contract.

F013 must resolve the arithmetic decision below before production kernels are implemented.
Transfers must keep every bit regardless of that decision.
The preferred arithmetic profile makes flush-to-zero behavior explicit and deterministic.
The final rules must define subnormal operands, subnormal results, signed zero, division, square root, and finite-value checks.
Reject a profile that cannot meet those rules without automatic substitution.

Use the existing [fixture tolerances](training-extension-design.md#reproducibility) for normal-range primitive, derivative, optimizer, and training comparisons.
Define separate exact fixtures for the selected subnormal and signed-zero rules.
Do not increase tolerances after a failed comparison.
Do not claim a universal error bound for arbitrary programs.

## Failures and Inspection

Use the existing error categories from the [training contract](training-extension-design.md#diagnostics-and-inspection).
Extend the shared fault types and built-in error declarations where those planned categories remain unimplemented.

| Condition | Planned result |
| --- | --- |
| Missing grant | Host `runtime.capability-denied` before initialization. |
| Backend unavailable | Recoverable `Device.Unavailable`. |
| Unsupported profile or operation | `Device.UnsupportedProfile` or `Device.UnsupportedOperation`. |
| Allocation or transfer failure | `Device.AllocationFailed` or `Device.TransferFailed`. |
| Kernel compilation or library creation failure | `Device.LibraryFailed` with operation context. |
| Submitted copy command failure | F012 marks the session unusable and reports `Device.Lost` with the originating source span. |
| Arithmetic command failure | F013 defines `Device.ExecutionFailed` under its computation contract. |
| Unusable device session | `Device.Lost`, followed by rejection of subsequent submissions. |
| Numeric failure | The applicable existing `Numeric.*` error. |
| Cancellation, budget, or cleanup failure | The existing host diagnostic. |

The synchronous sequence keeps source-order failure selection.
No subsequent File commit or HTTP result can precede completion of earlier fallible device work.
Diagnostic snapshots contain tensor metadata and must not cause tensor downloads.
Inspection must expose possible failures, explicit transfers, and the synchronous execution profile.

## Native Artifacts

F014 adds the planned `lo build --device-backend metal` option.
The default artifact uses the current software backend selection.
For Metal artifacts, use a Metal-enabled compiler and a local `darwin/arm64` target.
Unsupported target combinations fail before compiler execution.

The native builder must include the Objective-C adapter, headers, and embedded `metallib` through its explicit source manifest.
It must enable cgo only for the selected Metal artifact configuration.
Artifact metadata must identify the backend, toolchain, SDK, kernels, and numeric profile configuration.
Runtime evidence must also record the actual macOS and device identity.
Resource grants remain execution inputs and do not enter artifact metadata.

The resulting executable uses system Apple frameworks and its embedded kernels.
Execution needs no source checkout, Go installation, Xcode installation, or external kernel file.
Keep the current build limits and atomic artifact publication contract.

## Implementation Sequence

| Order | Issue | Deliverable and dependencies |
| --- | --- | --- |
| 1 | F012 | Explicit selection, Metal build, adapter, transfers, budgets, and failure tests after F008. |
| 2 | F013 | Numeric decision, typed computation interface, FP32 kernels, derivatives, and optimizers after F012 and F007. |
| 3 | F014 | Native artifact support and interpreter parity after F013 and F001. |
| 4 | I002 | Complete training acceptance and evidence after F014. |

Start each behavior change with a failing integration test through the public CLI.
Use controlled backend dependencies for unavailable devices, allocation failures, command failures, and cancellation.
Keep the checker, evaluator, resource ledger, error handling, and CLI serialization real.
Use an independent software executor for Metal operation descriptors and the selected arithmetic profile.
Do not use the CPU tensor evaluator as its numerical oracle.
Do not add a public `metal-sim:0` selector for this test dependency.

Add planned `make test-metal` and `make test-native-metal` targets for explicit local Metal execution.
These targets must fail clearly when their requested Metal environment is unavailable.
Routine CI must run the software contract tests without a GPU prerequisite.
Simulation and controlled failures are sufficient acceptance environments under repository policy.
Record optional Metal execution separately and state which behavior each environment establishes.

I002 reuses the network, inputs, initialization, and 200-step SGD fixture specified by I001.
It also includes the independent AdamW fixture from F007.
Complete this acceptance independently of I001's CUDA dependency.

- Verify parse, check, inspect, run, and native execution through their public commands.
- Verify finite gradients, final loss below `0.001`, and fixed numerical tolerances.
- Verify six uploads and six downloads for the complete training fixture.
- Verify bounded live storage and complete cleanup across repeated steps.
- Verify exact metadata, error kinds, source traces, and transfer counts.
- Verify that a failed operation prevents subsequent commits and result publication.
- Repeat each fixed configuration three times.
- Run focused integration targets during implementation.
- Run `make ci` once after the final implementation change.
- Record performance measurements only after correctness tests pass.

Performance has no speed threshold in this first implementation.
Software results do not establish Metal kernel arithmetic or GPU performance.
The available local Metal execution can provide that additional evidence without becoming a hardware acceptance prerequisite.

## Contract Updates and Open Decisions

F012 updates device selectors, grant validation, inspection, and the current device resource document.
F013 updates specification numeric rules, operation support, error declarations, and training profile documentation.
F014 updates the native build contract, runtime bundle manifest, and artifact metadata.
I002 updates the README, examples, and acceptance evidence with implemented capabilities only.

| Decision | Owner | Necessary resolution |
| --- | --- | --- |
| Exact arithmetic for subnormals and signed zero | F013 | Run a bounded MSL probe and independent reference before selecting final profile rules. |
| Metal language version and device feature predicate | F012 | Resolved: MSL 3.1, Apple GPU family 7 support, unified memory, and macOS 27. |
| Source and compiled kernel identities | F012 | Resolved through the pinned build manifest and its generated receipt. |

These decisions belong to implementation preparation.
No additional user input or physical-device access is necessary.

## Planning Validation

The October 15, 2026 review verified the plan against current source and official Apple documentation.
The issue identifier, dependency, and local document link checks passed.
The Governor check and `git diff --check` passed.
The language review covered the new plan and changed prose against the verified ASD-STE100 Issue 9 reference.
Mechanical checks found no findings in that scope.
The tracker and README keep 22 findings in unchanged prose.
This review does not claim full-document language compliance for those files.
No runtime code changed, and this planning task did not run implementation tests.
