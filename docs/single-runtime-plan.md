# Single Native Runtime Plan

P009 selects one compiled execution architecture for Lo.
The Go frontend compiles Lo programs through LLVM.
The Rust native runtime supplies application operations and resource ownership.
I014 through I018 implement this plan.
The current source still contains the interpreter until that work completes.

## Selected Command Boundary

| Interface | Selected behavior | Owner |
| --- | --- | --- |
| `lo parse SOURCE` | Parse source and report syntax. | Go frontend. |
| `lo check SOURCE` | Check types, effects, and language contracts. | Go frontend. |
| `lo inspect SOURCE` | Report the checked program without application execution. | Go frontend. |
| `lo build SOURCE --output PROGRAM` | Compile and link a native executable. | Go frontend, LLVM, and LLD. |
| `PROGRAM run [options] -- [arguments]` | Execute the compiled application. | Rust native runtime. |
| `PROGRAM serve [options] -- [arguments]` | Execute the compiled service and own its shutdown. | Rust native runtime. |
| `PROGRAM metadata` | Report the compiled program and runtime identities. | Rust native runtime. |
| Compiler `run` and `serve` commands | Reject these removed commands. | I014. |

The selected workflow is build, then execute.
Compiler input preparation follows the selected compiler distribution contract.
Application grants, execution budgets, arguments, and Console options belong to the program command.
The compiler retains build options and compiler diagnostics.

This plan adds no source execution wrapper or persistent artifact cache.
It adds no interpreter selector, fallback, or compatibility API.
Existing Go execution and embedding APIs are removed after their required behavior receives native coverage.
The compiler implementation can remain in Go.
LLVM-generated machine code and the Rust runtime form the application's execution path.

## Entry Identity And Application Inputs

`Program.path()` identifies the actual native executable for program commands.
It does not identify the Lo source or the compiler executable.
`--lock-entry` grants authority for that retained executable identity.
Concurrent invocations of the same unchanged executable must contend for its entry lock.
Separate copies and replaced executables remain subject to F029's file-object guarantee.

A fresh temporary executable for each source invocation would select a different lock identity.
The selected command boundary removes that ambiguity.
It requires no private source descriptor transport or source identity substitution.
The [lock contract](program-lock-design.md) continues to own bounded replacement and symlink guarantees.

The application captures its environment, physical directory, arguments, home, UIDs, and clock at native startup.
Compiler subprocess controls must not become application invocation inputs.
The native CLI owns output, exit status, service readiness, cancellation, and retained child cleanup.
The compiler does not add another application supervisor or output broker.

## Current Source Boundaries

| Source area | Required action |
| --- | --- |
| `cmd/lo/main.go` | Remove `eval.Program` and Go execution dispatch. Retain compiler commands and build diagnostics. |
| `internal/runner/command.go` | Separate compiler options from application policies, values, budgets, and execution APIs. |
| `internal/runner/execute.go` | Remove Go application execution, Console completion, and service supervision. |
| `internal/eval`, `runtimeops`, `execution`, `value`, `derivative`, `clioptions` | Remove dynamic evaluation, runtime values, primitive execution, numeric derivatives, and Lo option parsing. |
| `internal/adapter` and Go files in `internal/processhost` | Remove Go File, Process, HTTP, Console, lock, and lifecycle implementations. |
| `internal/autodiff` | Retain compiler derivative plans used by checking, inspection, and native lowering. |
| `internal/tensor/shape.go` and `internal/tensorfacts` | Extract pure shape validation and static work facts before removing numeric storage and execution. |
| `internal/device` | Extract build selection, profile names, and shared schema constants. Remove Go device sessions and execution. |
| `internal/compiler`, `syntax`, `source`, `ir`, `types`, `catalog`, `check`, `inspect`, `nativeplan`, `llvmemit`, `nativebuild` | Retain compiler responsibilities and remove obsolete runtime dependencies. |
| `internal/metalconfig` | Retain compiler identities and native build inputs. Remove references to deleted Go runtime files. |
| `runtime/native` | Retain the single application runtime, C ABI, native host adapters, and device variants. |

Package names alone do not establish deletion authority.
I016 must inspect imports and separate static compiler facts from dynamic Lo execution.
The proposed destinations are `internal/shapefacts` and `internal/targetconfig` for the required pure compiler contracts.
These package names are implementation targets, not existing interfaces.

### Native Assets That Must Survive

The native build consumes files currently stored under Go runtime directories.
Deleting those directories first would break the retained runtime.

| Current assets | Selected destination | Required consumers |
| --- | --- | --- |
| `internal/processhost/native/launcher.c`, `normalization.c`, and `protocol.h` | `runtime/native/host/process/` | Native archive builder, process identity generator, Make targets, distribution inputs, and qualification fixtures. |
| `internal/device/metal_bridge_darwin.m`, `metal_bridge.h`, and `metal_types.h` | `runtime/native/host/metal/` | Native Metal builder, profile inventory, kernel production, and source identities. |

I016 moves these assets without a duplicate source owner.
It updates include paths, producer inputs, generated identities, and consumers together.
The Rust helper identity remains required.
The generated Go helper identity and Go-only launcher consumers are removed when unused.
Fresh native archives must match the relocated source inventory before acceptance.

## Embedding And Failure Injection

The retained embedding model executes compiled Lo entry points through the native C ABI.
It does not accept Go callbacks as replacement Lo programs.
`execution.NewProgram`, `eval.Run`, `runner.RunContext`, `processhost.OpenTrusted`, and Go adapter injection APIs are removed.

I015 must classify each former Go test by its actual contract.
A test for a removed Go API can be retired.
A test for Lo behavior must receive native coverage before retirement.
The replacement boundary can be the native CLI or the compiled-entry C ABI.
No Go compatibility bridge remains.

The native ABI already supplies Console callbacks, device hooks, Process driver hooks, and entry and lock registration.
Its current headers and result access do not describe a complete general embedding SDK.
I015 must provide canonical declarations and bounded diagnostic access for the retained interfaces.
It must specify pointer ownership, result release, owner-thread rules, cancellation, and signal ownership.
Concurrent retained contexts must have explicit cancellation isolation.

General File, HTTP, and real Process embedding configuration is not implied by this migration.
Those application contracts can use the native CLI's explicit grants.
A broader embedding SDK requires separate scope.

Required migrated scenarios include these contracts:

- Verify authority denial without activation.
- Verify immutable invocation capture and argument framing.
- Verify complete and partial Console writes, flush failures, quotas, and cancellation.
- Verify terminal status only after required cleanup and output succeed.
- Verify foreground and process-group SIGINT and SIGTERM during service startup, active output, and delayed child cleanup.
- Verify File observation identity, foreign observations, changed ancestors, and confirmed removal progress.
- Verify Process creation, output bounds, retained pipes, delayed release, and unknown ownership.
- Verify lock contention, entry replacement, descriptor inheritance, and close failure precedence.
- Verify value accounting, resource release, callback ownership, and context completion.

Fault models remain deterministic and local to Lo.
Native driver, sink, observer, and exact-descriptor failure hooks supply the required test conditions.
They do not become another evaluator or a production execution fallback.

## Expected Results And Test Helpers

Runtime expectations must not come from the deleted Go interpreter.
Keep independently specified literals, exact checkpoint bytes, corpus arithmetic, analytical gradients, and training references.
Standard-library reference calculations in tests remain valid when they do not execute Lo semantics.
Finite differences check reverse-mode differentiation through repeated native forward execution.
They require analytical or other independent checks alongside them.

Replace source execution helpers with explicit compilation and artifact execution helpers.
Compiler rejection tests must invoke the actual compiler command.
Application tests must execute the produced artifact.
Native ABI tests must link the compiled fixture against the selected runtime archive.
Fixture reuse can compile one unchanged source once for several argument cases.
It must preserve independent builds where artifact identity is part of the test.

Remove `LO_TEST_BACKEND` and interpreter selection branches from active test execution.
Remove Go evaluator calls used to generate expected native values or diagnostics.
Do not copy the evaluator into test support under another name.

## Semantic Coverage, CI, And Measurements

The semantic inventory must classify compiler commands, program commands, and native ABI contracts explicitly.
Each required contract must reference its actual execution boundary and applicable targets.
Remove interpreter logs and the interpreter-to-native translation map.
Reject missing coverage, unexpected skips, duplicate contract IDs, stale identities, and unknown required operations.
Reject duplicate references within a contract and duplicate completion events.
One test can cover several contracts, and one contract can require several tests.
Keep source, compiler, ABI, runtime, target, and profile identities in acceptance results.

| Validation entry point | Selected responsibility |
| --- | --- |
| `make test-frontend` | Go compiler and static-planning checks without Lo application evaluation. |
| `make test-native` | One compiled application integration suite, including native ABI fixtures. |
| `make test` | Aggregate the retained compiler, tooling, and native suites without duplicate application execution. |
| `make ci` | Format, lint, test, build, semantic-report, runtime-tool, and qualification-tool checks. |

`test-frontend` is a planned target.
I017 must select package boundaries that prevent `go test ./...` from running application integration twice.
The native suite covers Darwin arm64, Linux arm64 under QEMU, and applicable Metal-linked contracts.
Controlled backend tests and software environments are sufficient acceptance environments.

The measurement cleanup includes `tools/nativebench`, `tools/filebench`, `tests/benchmark`, and qualification consumers.
Current tooling generates or calls interpreter fixtures and uses paired measurement schemas.
Remove those generators, Go runtime allocation measurements, interpreter-relative ratios, and obsolete thresholds.
Select native measurements for execution, allocations, resource use, compilation, and linking where each remains necessary.
Do not label two native measurements as independent runtime parity.
Define new schemas directly and reject the removed paired schemas.
Historical measurement records remain historical evidence, not active acceptance inputs.

## Ordered Work Packages

| Issue | Work | Dependencies | Completion evidence |
| --- | --- | --- | --- |
| P009 | Select this architecture and cleanup plan. | I011, F029 | Reviewed plan, issue graph, and documentation checks. |
| I016 | Extract compiler facts and relocate shared native assets. | P009 | Pure imports, one asset owner, fresh archives, and focused compiler/native checks. |
| I015 | Migrate native fixtures, retained embedding contracts, and independent expected results. | I016 | Explicit coverage for required behavior and no evaluator-generated expectations. |
| I014 | Remove source execution commands and isolate the compiler CLI. | I015, I016 | Compiler rejection cases and successful build plus native program execution. |
| I017 | Replace dual coverage, CI, and measurement contracts. | I014, I015 | Direct native references, one application lane, and current measurement schemas. |
| I018 | Delete the Go runtime and qualify the complete transition. | I014, I015, I016, I017 | Removal audit, native corpus, independent review, and final stack CI. |

These issues form one coherent implementation stack.
Each behavior change starts with its focused public failing case.
Pure extraction starts with current characterization coverage.
I018 owns the shared final `make ci` checkpoint after the last correction.
The component issues use focused checks during implementation.
Their acceptance requires the final stack checkpoint before closure.

### I016: Compiler Facts And Native Assets

1. Classify imports from compiler packages and all executable build tools.
2. Move required shape and target contracts into their pure compiler owners.
3. Redirect compiler, inspector, catalog, Metal, and native builder consumers.
4. Move the shared C and Objective-C assets into the native source tree.
5. Update source inventories, generators, build recipes, fixtures, and package producers.
6. Verify that retained compiler core packages and static build producers import no dynamic evaluator or host adapter.
7. Build fresh runtime archives before native validation.

The existing `cmd/lo` and `runner` execution dispatch remains transitional during I016.
I014 removes that dispatch. I018 deletes the unused Go runtime packages.

### I015: Native Coverage Before Deletion

1. Create the compiler-command, program-command, and ABI coverage ledger from current contracts.
2. Classify Go-only API tests and language behavior tests separately.
3. Port required failure and lifetime scenarios through native public boundaries.
4. Complete the retained native ABI declarations and diagnostic ownership contract.
5. Replace dynamic expected-result generation with independent expectations.
6. Convert application helpers to explicit build and artifact execution.
7. Verify that no required assertion disappears with an old helper.

### I014: Compiler Command Cleanup

1. Separate compiler options and reporting from Go runtime policy types.
2. Keep source parsing, checking, inspection, LLVM emission, and executable construction.
3. Reject compiler `run`, `serve`, application grants, and application-only budget options.
4. Remove Go evaluation dispatch and its source-entry capture from the compiler command.
5. Exercise built program arguments, output, status, service startup, and signal cancellation through native commands.
6. Verify actual executable identity and same-artifact entry lock contention.
7. Update current command examples and removed Go embedding documentation.

### I017: Validation And Measurement Cleanup

1. Replace the semantic report inputs with direct compiler and native execution references.
2. Reject interpreter logs, translation mappings, and obsolete backend selectors.
3. Run each compiled application suite once in the aggregate test target.
4. Retain compiler, tooling, ABI, and target-specific checks under explicit owners.
5. Replace interpreter benchmark generators and paired measurement consumers.
6. Verify current measurement schemas and reject removed schemas.
7. Update CI workflows and compiler distribution acceptance together.

### I018: Runtime Removal And Qualification

1. Remove obsolete Go execution packages and their direct embedding APIs.
2. Remove dynamic tensor, device, numeric derivative, Console, File, HTTP, Process, and lock implementations.
3. Remove interpreter build variants, runtime selectors, generators, and unused dependencies.
4. Check production, test, benchmark, distribution, and tool imports for deleted packages.
5. Verify that no active path evaluates a Lo checked graph in Go.
6. Update current architecture, language, CLI, embedding, and source-location documentation.
7. Verify application and ABI conformance on the declared software environments.
8. Complete independent review and correct accepted findings.
9. Run final `make ci` after the last stack correction.
10. Close the implementation stack only after all required checks pass.

## Compiler Packaging And Application Sequence

F017 and I011 establish the current offline native compiler distribution.
P008 separately selects an executable-only compiler payload.
F038 through F041 implement that payload and delivery, and F037 owns its final acceptance.
Removing the interpreter requires no Go frontend rewrite or compiler self-rebuild.
It does not wait for full F037 delivery.
Native asset and inventory consumers in that packaging work must follow I016 and I018.

F030 through F036 and I013 depend on I018 before further application runtime implementation.
Their compiler contracts and Rust runtime operations have one implementation owner after the transition.
P007 can continue its Process profile research while the cleanup proceeds.
F018 depends on I018 before module implementation.
F019 through F023 inherit the same native-only execution model through their dependencies.
The existing mac-cleanup milestones remain ordered from CLI behavior through the complete confined workflow.
I013 must build and execute native fixtures with independently specified category outcomes.
It must not require an interpreter comparison or a physical-device gate.

## Plan Acceptance And Implementation Boundary

This task changes plans and issue dependencies only.
It does not remove code, change commands, run cleanup, or claim runtime migration completion.
P009 closes after source review, issue validation, link checks, and changed-prose checks.
I018 remains open until implementation and its final checks complete.
All fixtures, tool state, temporary work, and acceptance output stay inside Lo.
