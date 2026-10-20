# Standalone Compiler And Language Evolution

P004 records this plan on October 16, 2026.
I004 completed the backend experiment on October 19, 2026.
The [runtime contract](native-runtime-contract.md) selects LLVM 21.1.8 with LLD 21.1.8 for the replacement design.
The current compiler still generates Go source and requires Go 1.27.1 for native builds.
This plan does not change that implementation or the language specification.
The [semantic migration contract](semantic-migration.md) records current coverage and its evidence requirements.

I004, I005, and P005 passed their acceptance checks.
I006 is the next compiler implementation step.
The installed standalone compiler criterion is still open under F017 and I011.

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
The [native generator](../internal/codegen/generate.go) converts that graph to direct Go control flow.
The [builder](../internal/nativebuild/build.go) locates Go and compiles an embedded source bundle.
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
F017 must supply the complete inputs for the implemented runtime.
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
| Add native lowering | I006 | Verified control, cost, error, and cleanup representation. |
| Port core values | I007 | Core runtime and generated calls pass public fixtures. |
| Port numeric execution | I008 | Tensors, autodiff, optimizers, and checkpoints pass their contracts. |
| Port adapters | I009 | Files, processes, and HTTP pass confinement and lifecycle tests. |
| Port devices | I010 | Transfers, Metal profiles, and resource cleanup pass software acceptance. |
| Supply the distribution | F017 | Complete relocatable offline compiler package. |
| Qualify and replace | I011 | Full parity, clean-environment acceptance, and removal of obsolete Go build code. |
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
I011 removes the Go generator and runtime source bundle after complete parity.
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
