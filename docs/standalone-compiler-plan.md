# Standalone Compiler And Language Evolution

P004 records this plan on October 16, 2026.
I004 completed the backend experiment on October 19, 2026.
The [runtime contract](native-runtime-contract.md) selects LLVM 21.1.8 with LLD 21.1.8 for the replacement design.
All executable builds use LLVM.
An explicit toolchain manifest is necessary for the development compiler.
The installed compiler selects LLVM from its own package.
LLVM module emission does not change the language specification.
The [semantic migration contract](semantic-migration.md) records current coverage and its evidence requirements.

I004, I005, P005, I006, I007, I008, I009, and I010 completed their acceptance checks.
I006 implements the [native lowering boundary](native-lowering.md) with LLVM modules for both targets.
I007 supplies core runtime values and execution under the selected ABI.
Core LLVM checks passed on Darwin arm64 and Linux arm64 in QEMU.
Each target passed 10,240 requests with retained contexts and complete cleanup.
Exact error traces, core operations, and execution limits passed their focused checks.
Final CI passed.
I008 implements numerical operations, autodiff plans, optimizers, and checkpoints.
Its acceptance includes the existing backpropagation, 200-step training, 500-step XOR, and checkpoint resumption tests through LLVM executables.
The training and XOR checks passed on Darwin arm64 and Linux arm64 in QEMU.
CPU checkpoint resumption matched uninterrupted training exactly.
Resource lifetime, metadata admission, work charges, cancellation, and exact checkpoint diagnostics passed.
All 13 analytical derivative and finite-difference fixtures passed through LLVM on Darwin.
Linux checkpoint tests passed across eight separate guest processes.
Independent review found no remaining material defect.
I009 implements native file, process, and HTTP execution.
The [adapter guide](native-adapters.md) describes capability grants, confinement, resource ownership, and verification.
Darwin arm64 supplies the current qualified process host.
Linux arm64 keeps the current process-unavailable result.
The embedded process launcher lets artifacts execute without a Lo installation.
HTTP services use different request contexts and keep worker ownership until cleanup completes.
The adapter checks include native failure injection and loopback traffic on both targets.
I009 validation completed successfully on October 20, 2026.
Each target completed 1,000 HTTP requests with complete request cleanup.
Backpropagation, training, XOR, and checkpoint regression checks completed successfully.
Final CI completed successfully.
The [adapter receipt](evidence/native-adapters/validation.json) records the results and qualification limits.

I010 implements native device resources and Metal execution.
The [device guide](native-devices.md) describes opaque buffers, explicit transfers, resource ownership, and separate Metal build inputs.
The Metal archive contains the verified precompiled kernels and the native adapter.
Build admission verifies the kernel bytes inside the selected runtime archive.

Software checks cover Metal arithmetic, backpropagation, 200-step training, 500-step XOR, and complete optimizer transfers.
Darwin arm64 and Linux arm64 under QEMU provide the software execution environments.
Physical Metal execution and performance remain unmeasured.
The [device receipt](evidence/native-devices/validation.json) records results and qualification limits.
I010 validation and final CI completed successfully on October 20, 2026.

The [runtime development guide](native-runtime.md) gives the explicit LLVM build and verification commands.
The [runtime receipt](evidence/native-runtime/validation.json) records source identities and validation results.
The [distribution guide](compiler-distribution.md) defines installed compilation for both hosts and both software targets.
F017 completed installed package acceptance on October 20, 2026.
Both compiler hosts passed offline builds for both software targets without external development tools.
The resulting executables passed independent execution without the compiler packages or source files.
Both hosts also passed 500-step XOR training through installed compilation.

I011 removed Go generation and the embedded runtime source bundle.
The interpreter remains available through explicit execution commands.

I011 passed the final focused HTTP qualification on both targets with unchanged numeric thresholds.
The user selected review of earlier correctness, offline, and non-HTTP evidence instead of another full suite run.
I011 closed on October 23, 2026, after the evidence audit and independent review passed.
The final HTTP reports remain separate from the earlier campaign.
The [measurement contract](native-compilation-contract.md#measurement-validity) defines acceptance and its limits.

### Earlier Qualification History

B001 through B006 completed their necessary corrections and reviews.
Earlier performance attempts do not show full acceptance.
Campaign9 and campaign11 ended before measurement under obsolete admission rules.
The later campaign ended with the status `invalid` during Linux process setup.
Its completed Darwin HTTP measurements had the status `inconclusive`.
The initial reports stay in the evidence record.

I011 corrected the HTTP elapsed-time calculation and added raw measurement checks.
A fixed 100-pair experiment found Linux HTTP ratios above the limit.
The runtime now uses the existing HTTP reader buffer instead of an 8 KiB zero fill for each read.
The corrected runtime completed all eight HTTP gates in a second experiment with the same design.
The focused HTTP checks completed on both targets.
Correctness and offline checks completed with the new runtime packages.

The ten-pair campaign stopped at a 180-second `records_10000` build timeout on Darwin.
Its Linux performance lanes did not start.
The failed campaign stays in the evidence record.

At that stage, full performance acceptance and the final evidence review remained pending.
The [findings](native-qualification-findings.md) contain the saved outcomes and current limits.

The recorded compiler had high memory, build time, and executable costs for large literal lists.
The HTTP correction does not address these costs.
Stage profiles identify LLVM register allocation as the largest measured compiler cost.
A diagnostic allocator change decreases time but keeps high memory costs and increases executable size.
The Go, Rust, and C++ diagnostics recorded lower large-literal costs than Lo on Darwin.
The comparison includes absolute costs and increases from baselines for empty programs.

B007 adds compact literal loops with the approved DWARF limitation.
B008 adds the canonical ABI2 metadata format.
Both changes passed focused checks on both targets and code review by a different agent.
The installed Darwin diagnostic recorded a 2.224-second median for 10,000 items, with 143.8 MB sampled RSS and a 7.66 MB executable.

The new distributions passed full correctness and offline checks.
All eight performance lanes completed.
All Darwin lanes and the Linux execution and build lanes passed.
The Linux process and HTTP lanes have the status `inconclusive`.
The evidence review found no measurement defect.

Program admission and HTTP diagnostics then measured these costs.
The parser now uses fixed row fields, and the HTTP runtime avoids a buffer copy and unused error records.
The changed paths passed public checks on both targets and review by a different agent.
The new distributions passed full correctness and offline checks.
Their full campaign completed seven lanes with `pass` and one with `inconclusive`.
Only Linux echo throughput remained inconclusive in that campaign.

A later change gives the HTTP reader a concrete connection type.
The new distributions passed correctness and offline acceptance.
The full campaign passed seven performance lanes.
Both Linux HTTP latency gates passed, but both throughput intervals cross the unchanged 1.1 limit.
I011 remained open after that campaign.

A later runtime change removes repeated tensor searches from HTTP values without tensor storage.
It passed focused public checks on both targets, full CI, the Linux corpus, and offline acceptance.
The complete campaign again passed seven performance lanes.
Linux computed-response HTTP passed, but echo latency and throughput remain inconclusive.
The raw audit confirmed every lane and target result without a threshold change.
The later oversized-request correction and focused qualification are recorded in the findings.

## Required Outcome

The user selected this acceptance criterion:

> On a supported clean machine with no Go installation, an installed Lo distribution can compile and link a representative program offline, and the resulting executable runs without Lo installed.

The replacement distribution supplies its compiler backend, linker, runtime libraries, and required platform link inputs.
It needs no separate Go, Rust, Clang, or developer SDK installation.
The executable can use the declared operating-system libraries.
It needs no Lo installation, source checkout, source file, or separate Lo runtime library.

The compiler frontend can remain a prebuilt Go executable.
The compiler implementation language does not impose an installed Go requirement.
Self-hosting is outside this plan.

## Current Contract

The [frontend](../internal/compiler/compiler.go) produces a typed [checked graph](../internal/ir/ir.go).
The [native plan](../internal/nativeplan) preserves control flow, charges, errors, and resource cleanup.
The [LLVM emitter](../internal/llvmemit/emit.go) converts that plan to native operations and runtime calls.
The [builder](../internal/nativebuild/llvm_executable.go) uses declared LLVM inputs and links the precompiled Rust runtime.
An explicit toolchain manifest is necessary for the development CLI.
Installed compilers use a manifest pinned to their package identity.
Native artifacts already execute without Go or Lo installed.

The migration covers the current catalog, including F015, F016, and I003.
It also covers current File receipts, process execution, and sequential fold behavior.
The [checkpoint contract](model-checkpoints.md) defines canonical bytes, typed imports, explicit container transfers, and complete optimizer resumption.
The proposed backend must preserve those operations.
Pending NVIDIA extensions remain separate work.

## Backend Assessment

These assessments describe engineering tradeoffs, not measured Lo performance.

| Option | Benefit | Required work | Decision |
| --- | --- | --- | --- |
| Bundled Go | Reuse the existing runtime and generator. | Package the complete toolchain and additional Metal inputs. | Meets the narrow installation requirement, but retains Go compilation. |
| LLVM and LLD | Native optimization, object generation, and debug information. | Port the runtime and supply complete platform link inputs. | Selected replacement design. |
| Cranelift and a linker | Fast code generation and a direct object API. | Port the same runtime and qualify debug and numeric behavior. | Four experiment cells passed. No production route selected. |
| Generated C with Clang and LLD | Readable generated code and conventional library calls. | Port the runtime and prevent C semantic changes. | Defer. |
| QBE with assembler and linker | Small implementation and simple IR. | Supply more toolchain components and qualify target support. | Defer. |
| WebAssembly with an embedded runtime | Portable execution representation. | Package an execution engine and define device integration. | Consider as a later target. |

[Cranelift](https://cranelift.dev/) supports AArch64 and AOT compilation.
Its [object module](https://docs.rs/cranelift-object/latest/cranelift_object/) emits object files.
[QBE](https://c9x.me/compile/) emits assembly and expects another tool to assemble and link the program.
[LLVM](https://llvm.org/docs/GarbageCollection.html) supplies runtime integration mechanisms, but does not supply Lo memory management.
[LLD](https://lld.llvm.org/) supplies ELF and Mach-O linkers.
A complete [toolchain](https://clang.llvm.org/docs/Toolchain.html) also needs runtime libraries and platform inputs.

## Selected Architecture

```text
Lo source and local dependencies
  -> frontend and verifier
  -> checked Lo graph
     -> inspector
     -> reference interpreter
     -> native lowering plan
        -> selected backend
        -> object files
        -> bundled linker + precompiled runtime + platform inputs
        -> standalone executable
```

The lowering plan makes control flow, error branches, logical charges, cleanup, and source locations explicit.
It retains Lo semantics before conversion to the selected backend IR.
The inspector continues to use the checked graph.

A Rust runtime with a narrow C ABI is the selected implementation.
The distribution contains a precompiled static runtime for each supported target.
[Rust static libraries](https://doc.rust-lang.org/reference/linkage.html) can supply this boundary without an installed Rust compiler.
System-library dependencies still require explicit link inputs.

The runtime contract resolves these P005 requirements before implementation:

- Specify value representations, type identities, and opaque handles.
- Specify ownership, retention, release, and callback lifetimes.
- Specify thread entry, request sessions, and callback cancellation.
- Separate successful values, recoverable Lo errors, and terminal host failures.
- Preserve source locations and ordered trace frames.
- Prohibit unwinding across the ABI.
- Audit cycles before selecting reference counting for immutable values.
- Preserve tensor release points independently of general heap reclamation.
- Release request storage during long-running service execution.

## Semantic Requirements

| Contract | Required behavior | Source |
| --- | --- | --- |
| Evaluation | Preserve argument, callback, branch, and flow evaluation order. | [Compiler design](compiler-design.md) |
| Effects | Separate reads, target construction, and commit activation. Prevent speculative reads and reordered commits. | [Compiler design](compiler-design.md) |
| Errors | Preserve fallibility, handlers, error kinds, source context, and ordered traces. | [Specification](../specification.md) |
| Execution limits | Preserve logical charges, value weight, tensor admission, cancellation, and flow depth. | [Native contract](native-compilation-contract.md) |
| Numeric values | Preserve arbitrary-precision integers, finite binary64 values, and the defined FP32 operations. | [Training design](training-extension-design.md) |
| Device execution | Preserve grants, explicit transfers, failures, and the resource ledger. | [Device contract](device-resources.md) |
| Metal arithmetic | Preserve normalization, intermediate checks, reduction order, and product rounding. | [Metal arithmetic](metal-arithmetic.md) |
| Checkpoints | Preserve canonical bytes, typed imports, exact state, and resumed results within each numeric profile. | [Checkpoint contract](model-checkpoints.md) |
| Files and services | Preserve confinement, request isolation, cancellation, and shutdown. | [Compiler design](compiler-design.md) |
| Inspection | Preserve transitive effects and capability reports without program execution. | [Compiler design](compiler-design.md) |

Optimization must preserve observable limit failures and their order.
An eliminated machine instruction does not remove its required logical charge.
The lowering plan must retain cancellation and resource admission boundaries.
LLVM attributes must not declare a side-effecting charge or boundary read as pure.

The numeric contract remains specific to each backend profile.
CPU and Metal comparisons use their existing rules and independently specified expectations.
They do not require equal results where the current profiles permit differences.
Disable reassociation and fused operations where they change the required result.
[LLVM floating-point controls](https://llvm.org/docs/LangRef.html) require explicit selection by Lo.

## Target And Distribution Contract

| Target | Initial execution environment | Platform decision |
| --- | --- | --- |
| `darwin/arm64` | macOS 27.0.1, Darwin 27.0.0 | Original TAPI descriptions, libSystem, and explicit framework inputs. |
| `linux/arm64` | Linux 6.12.110, glibc 2.36 | Pinned Debian startup objects and runtime libraries. |

The current native qualification covers both targets.
The replacement must preserve the current documented cross-target software builds.
The runtime contract records both hosts, both software targets, exact tools, and the narrower replacement macOS floor.
Metal remains an explicit local `darwin/arm64` build configuration under its existing contract.

The following distribution contents are required:

- Supply one compiler entry point and the selected backend tools.
- Supply the linker, runtime archive, startup objects, and compiler support libraries.
- Supply required platform descriptions and system-library link inputs.
- Record tool versions, file digests, source identities, and third-party license inventory.
- Resolve tools from the installed distribution, independently of `PATH`.
- Reject missing, corrupt, or incompatible inputs with a diagnostic.
- Prohibit downloads and package resolution during compilation.
- Preserve atomic output replacement and cancellation cleanup.
- Support installation relocation without source or build-machine paths.

The current experiments establish SDK-free macOS linking with original Lo-authored symbol descriptions.
They also establish Foundation, Objective-C, Metal, and process-sandbox symbol links.
F017 supplies the complete inputs for the implemented runtime.
Precompiled Metal kernels need no new MSL compilation during a user build.
They do not remove native framework linkage requirements.

## Implementation Sequence

The issue dependency markers define implementation order.
The validation requirements in each issue are its acceptance criteria.

| Stage | Issues | Exit result |
| --- | --- | --- |
| Document the migration | P004 | Reviewed plan and dependency graph. |
| Compare complete toolchains | I004, P005 | Four-cell experiment and one selected backend, ABI, and platform contract. |
| Record semantic coverage | I005 | Full current-language corpus with independent expectations. |
| Add native lowering | I006 | Completed: verified control, cost, error, cleanup, LLVM emission, and bounded runtime execution. |
| Port core values | I007 | Completed: core runtime and generated calls pass public fixtures. |
| Port numeric execution | I008 | Completed: CPU tensors, autodiff, optimizers, and checkpoints pass their contracts. |
| Port adapters | I009 | Files, processes, and HTTP pass confinement and lifecycle tests. |
| Port devices | I010 | Completed: transfers, Metal profiles, and resource cleanup pass software acceptance. |
| Supply the distribution | F017 | Completed: both compiler hosts pass offline compilation, relocation, rejection, and independent artifact execution. |
| Qualify and replace | I011 | Complete under the user-selected focused acceptance update: HTTP passed on both targets; earlier evidence passed review. |
| Specify composition | P006 | Modules, types, generics, and package rules. |
| Implement composition | F018, F019, F020, F021 | Modules, variants, generics, and offline packages. |
| Add library and tools | F022, F023 | Standard library, formatter, test runner, editor, and debug support. |
| Qualify application development | I012 | Complete multi-module CLI, service, and training applications. |

I004 uses a bounded experiment before full implementation.
Its representative runtime fixtures do not require the complete Lo runtime.
I006 supplies a minimal ABI test runtime before I007 implements core Lo values.
A suggested two-week effort budget limits investigation.
It is not a compiler delivery estimate.

Correctness and complete linking decide feasibility before performance comparisons.
Measurements cover distribution size, compile time, peak memory, artifact size, debug behavior, and execution time.
The current 180-second, 2-GiB, and 64-MiB build limits remain applicable until an explicit contract decision changes them.

The migration does not retain an automatic Go or interpreter fallback.
I011 removed the Go generator and runtime source bundle after the initial corpus and corrected fixture checks.
The final qualification must bind all results to the current source and installed packages.
The interpreter remains an explicit implementation of the current language for execution and differential tests.

## Offline Acceptance Procedure

1. Prepare the declared base OS in a container or VM.
2. Exclude Go, Rust, C compilers, developer SDKs, source checkouts, and undeclared build tools.
3. Use empty caches and disable external network access.
4. Install the complete Lo distribution without network access.
5. Set `CI=1` and close standard input for each command and child process.
6. Compile the representative corpus with the installed `lo build` command.
7. Record tool invocations and all external file dependencies.
8. Copy only each executable and its required application data into a second clean environment.
9. Execute without Lo, its distribution, the original source, or compiler tools.
10. Compare results, errors, effects, and limit failures with independent expectations.
11. Repeat after installation relocation and with empty caches.
12. Verify deterministic failures for missing or corrupt distribution files.
13. Verify that build failure and cancellation preserve an existing output artifact.

The corpus includes large integers, Unicode, collections, branches, and handled errors.
It also includes ordered file operations, a loopback HTTP service, CPU training, and checkpoint resumption.
Negative cases include denied capabilities, exhausted budgets, malformed checkpoints, and cancellation.
The release gate covers the full current catalog, beyond the representative corpus.

Use simulators, emulators, and controlled device implementations for device acceptance.
Record unmeasured Metal execution behavior without a physical-device prerequisite.
Keep release publication and production deployment outside implementation issue acceptance.

## Language Evolution

P006 defines extensions before their implementation.
The existing immutable values, named flows, explicit commits, and error values remain binding semantics.

- Add modules with explicit visibility, deterministic imports, and transitive effects.
- Prohibit import-time effects and implicit capability grants.
- Add user-defined records and tagged variants with exhaustive matching.
- Define type identity and construction before adding syntax.
- Add generics with bounded specialization and explicit effect constraints.
- Add one package manifest and lockfile with exact local dependency identities.
- Separate dependency retrieval from offline compilation.
- Extend text, collections, JSON, files, and HTTP through capability-controlled library operations.
- Add formatting, testing, editor navigation, diagnostics, and source-level debugging.

Recursion, closures, concurrency, and foreign-function access need separate proposals.
They change termination, inspection, ownership, or authority.
New operating systems, new architectures, and self-hosting remain outside this sequence.

## Decision Owners

| Decision | Owner | Required evidence |
| --- | --- | --- |
| LLVM with LLD | P005 | Four current experiment cells passed. LLVM generated source locations were verified. |
| Rust runtime and reference counting | P005 | Two-target ABI fixture, cycle audit, callback rules, and explicit resource release. |
| Minimum OS and system libraries | P005 | Selected baseline execution and pinned host and target input inventories. |
| macOS link-input distribution | P005 | Original repository-authored descriptions and SDK-free platform links. |
| DWARF and strict numeric lowering | P005 | Generated location probe and required I005 parity before optimization. |
| Modules, types, and package rules | P006 | Reviewed specifications and examples before implementation. |

The [experiment report](native-backend-experiment.md) records observed results separately from these requirements.
