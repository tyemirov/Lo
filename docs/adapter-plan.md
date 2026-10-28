# Adapter Implementation Plan

Status: shared purity, File/HTTP behavior, Process results, executable registration, and the native host are implemented.
The selected Darwin 27 arm64 profile passed [native qualification](evidence/process-host-native.json).
The historical Go-generation implementation passed all 24 interpreter and generated-native cases.
Those cases included server callbacks and Process-to-File composition.
Its stack CI and independent completion review passed.
The [native adapter guide](native-adapters.md) defines the current LLVM checks.
The [compiler plan](standalone-compiler-plan.md) records I011 qualification.
The [final validation record](evidence/process-enabled-validation.json) preserves the CI log and review outcome.
Earlier injected lifecycle and registry checkpoints passed CI and independent review.
Implementation and final stack CI passed. Independent completion review passed.
F024 adds immutable contexts, controlled executable discovery, expanded bounded transport, and selected cleanup scopes.
The [F024 checkpoint](evidence/process-context-cleanup/checkpoint.json) records current tests and stack validation separately from earlier acceptance.

The [adapter design](adapter-design.md) owns the selected language and domain rules.
The [language reference](../specification.md) describes the current implementation until each stage qualifies.

## Contents

- [Scope](#scope)
- [Source ownership](#source-ownership)
- [Implementation sequence](#implementation-sequence)
- [Acceptance matrix](#acceptance-matrix)
- [Validation](#validation)
- [Deferred work](#deferred-work)

## Scope

Implement one common adapter contract across File, HTTP, and Process.
Keep every source change, fixture, cache, process helper, and validation output inside Lo.
Use the existing primary checkout.
Apply the [repository boundary](../.mprlab/POLICY.md#lo-repository-boundary) to all tools and child processes.

Gateway identifies one log category in a repository fixture.
It does not identify a dependency checkout or an external acceptance target.
The [mac-cleanup plan](mac-cleanup-plan.md) remains an application plan that depends on this contract.

This document records implemented adapter contracts and qualification evidence.
The stage status below separates implemented contracts from selected future work.
This plan does not authorize operations outside Lo.
No compatibility aliases, alternative legacy schemas, or unrestricted Process mode belong in the implementation.

## Source Ownership

| Component | Responsibility | Existing source |
| --- | --- | --- |
| Syntax and types | Bound commit statement, nominal targets, result types, and header values. | `internal/syntax`, `internal/types`, `internal/value` |
| Catalog and verifier | Shared operation metadata, activation result relation, and transitive callback rules. | `internal/catalog`, `internal/check`, `internal/ir` |
| Execution | Ordered activation, successful binding publication, failures, and progress. | `internal/eval`, `internal/execution`, `internal/runtimeops` |
| Inspection | Immediate effects, stored callbacks, calculated authority, and result types. | `internal/inspect` |
| Native compilation | Equivalent statement lowering, LLVM emission, and static runtime interfaces. | `internal/nativeplan`, `internal/llvmemit`, `internal/nativebuild`, `runtime/native` |
| Adapters and host | File receipts, HTTP client/server behavior, Process profiles, and lifecycle. | `internal/adapter`, `internal/runner` |
| Acceptance | Public CLI contracts, local protocol fixtures, and interpreter/native comparisons. | `tests/integration`, `testdata`, `examples`, `docs/examples` |

The primary agent owns shared contracts and integration.
Assign one edit owner per source group during implementation.
Review shared type and catalog changes before parallel implementation.
Keep independent architecture review at the contract and final acceptance boundaries.

## Implementation Sequence

The section numbers identify work areas, not a strict dependency chain.
Process core work and native qualification are separate tracks.

| Track | Prerequisite | Current state |
| --- | --- | --- |
| Commands, results, injected lifecycle, and injected composition | Shared types, purity, and activation contracts | Implemented and race tested without native qualification. |
| Native profile qualification | Supported enforcement mechanisms and separately reviewed probes | Selected Darwin 27 arm64 profile passed. Other backends remain unavailable. |
| Native OS driver and standard-host activation | Qualified profile plus shared lifecycle contract | Native driver implemented. All 24 canonical public cases passed. |
| Native composition acceptance | Qualified native driver | Public interpreter and generated-native Process-to-File cases passed. |
| Contexts, discovery, and cleanup scopes | Shared Process lifecycle and selected Darwin profile | The F024 checkpoint records interpreter and native fixture results. |

### 1. Contract Fixtures and Purity

Status: implemented. Focused interpreter and native tests passed.
Pure callbacks exclude direct and transitive observations and commits.
Device requirements stay separate.
The public fixtures also cover conditional observations and inert stored handlers.

1. Add public checker fixtures for observations in List callbacks, predicates, and error handlers.
2. Add positive fixtures for conditional observations in selected Match branches and ordered `Flow.fold` callbacks.
3. Confirm the expected failures against the current checker before production changes.
4. Update the shared effect analysis and every callback position from the design table.
5. Reconcile specification sections 5.6, 8, 10, 12, 22.2, and 23.3 with the selected rules.
6. Verify unchanged deterministic tensor training and separate device requirements.

Acceptance depends on accurate observation summaries and no speculative boundary access.

### 2. Bound Activation and File Targets

Status: implemented. Focused interpreter and native tests passed.
Independent source review closed the operation-progress and cleanup findings.
The result relation, syntax, both execution backends, inspection, nominal File targets, and receipts use one catalog contract.
`HTTP.ServerReceipt` was included because server activation shares the same boundary interface.

1. Add failing CLI fixtures for bound commit, result types, and failure before subsequent statements.
2. Define nominal File targets, receipt schemas, and partial-write failure evidence in the catalog.
3. Add the UTF-8 byte encoder and migrate write, append, and removal construction.
4. Implement statement parsing, type checking, IR lowering, interpretation, inspection, and native code generation together.
5. Update the specification, current examples, and existing File acceptance cases to the canonical contract.
6. Remove the obsolete `Effect` construction paths and type when no canonical target uses them.
7. Build the static native runtime through `make build-native-runtime`.

Acceptance depends on equivalent bound and unbound commits except for publication of the successful result.
Top-level activation failure must stop later effects in both backends.
No result binding exists after partial execution or resource-release failure.

### 3. HTTP Values, Server Receipts, and Client Actions

Status: implemented, with focused interpreter and native qualification complete.
Server startup receipts were included in stage 2.
Headers, total responses, client activation, and automatic trace summaries pass the combined suites.
Independent source review closed the current findings.
HTTP-to-File composition also passes in both execution backends.

1. Specify client grants, connection checks, limits, and error schemas before code changes.
2. Add failing public fixtures for repeated headers, response construction, and actual server-start receipts.
3. Replace the current header representation and add the general total response constructor.
4. Verify that a pure total `onError` constructs a response through the Text or Bytes convenience constructor.
5. Add a repository-local HTTP fixture for client activation and response status policy.
6. Add denied-destination, redirect, partial-response, cancellation, and output-limit cases.
7. Verify interim responses, responses without bodies, and rejection of CONNECT and protocol switching.
8. Verify server startup failure separately from request `onError` behavior.
9. Verify client results and server lifetime through interpreter and native entry points.

Acceptance depends on full response publication and explicit evidence when the remote outcome is unknown.
No request executes during construction, inspection, or compilation.

### 4. Native Process Profile Qualification

The selected Darwin 27 arm64 profile passed [native host qualification](evidence/process-host-native.json).
The [design ledger](adapter-design.md#qualification-status) links current qualification and earlier bounded mechanism evidence.
Startup, local Mach-slot normalization, identity integration, denial checks, and cleanup passed their selected suites.
The conditional `P × completed lifetime` work bound assumes a responsive OS. It is not a hard CPU quota.
No result establishes platform impossibility, and SDK deprecation is not automatic disqualification.
Canonical public acceptance passed all 24 interpreter/generated-native cases. Final stack CI and independent completion review passed.

Existing installed OS and runtime dependencies can serve as read-only execution infrastructure.
This allowance excludes other repositories, external task data, and external writes.
All added sources, fixtures, caches, and output remain inside Lo.

1. Select a supported native execution path and enforceable profile.
2. Keep the fixed launcher environment separate from command environment variables.
3. Install confinement before receipt of command environment variables, and close the bounded control channel before target execution.
4. Establish executable identity and filesystem, network, IPC, descendant, and resource restrictions.
5. Verify denied operations and cleanup through repository-contained fixtures.
6. Confirm no surviving descendants and successful resource release before result publication.
7. Obtain independent review before native launch acceptance.

The [registry phase](adapter-design.md#executable-registry-phase) is implemented, with focused public checks passed in 0.804 seconds.
Post-correction Process, File, and Gateway fixture race checks passed in 13.547 seconds.
Independent source/test review closed the nested-ownership and quarantine-test findings.
Final current-stack CI passed: interpreter integration 49.549 seconds and generated native integration 574.835 seconds.
Runtime bundle verification, formatting, Go vet, all Go tests, native integration, and build passed.
Independent final review approved the registry checkpoint with both findings closed.
Validation uses byte fixtures, real Lo File operations, and injected lifecycle drivers without native Process execution.
The registry checks an exact digest and path and protects against participating Lo File writes, including aliases.
Selected native child filesystem protection passed the separate profile qualification.
Immutable registered protection and admission/ownership leases cover the full operation and verified release.
No coordinator lock remains held through child execution or injected callbacks.
Registry temporary-file creation requires a stable namespace and matching parent pathname/retained-parent device and inode before creation.
Root-relative link publication and cleanup follow that check.
This registry-only rule does not weaken standalone File behavior or claim hostile host concurrency protection.
Any identity mismatch must fail closed.
Descriptor execution was unavailable through both tested SDK compile interfaces.
This result establishes no OS impossibility.

Registry integration requires exact grants, continuous path/inode/ancestor protection, shared File adapters, and whole-execution ownership.
The public registry tests passed after expected missing-API reds.
The earlier injected lifecycle phase did not itself supply a native launcher.
Qualification remains a prerequisite for native execution, not host-independent core validation.
Qualification applies per selected backend/profile, without a simultaneous macOS and Linux requirement.

### 5. Process Commands and Results

Status: results and the injected lifecycle core are implemented.
Focused and race checks passed, including Process-to-File composition and error recovery.
Native execution remains dependent on profile qualification.
The host-independent result and lifecycle core uses an injected OS boundary.
The selected native driver is implemented. Canonical public activation acceptance passed all 24 cases.

The plan now permits pure construction, inspection, and unavailable rejection before qualification.
This dependency rule keeps pure construction independent from native qualification.

### Pure Process Phase

This approved phase is implemented, and focused interpreter validation passed.
It can proceed before execution profile qualification.
It adds pure constructors, inspection, and host rejection without child execution.

| Operation | Result |
| --- | --- |
| `Process.Executable.new(path: File.Path)` | `Process.Executable!` |
| `Process.Command.new(context: Process.Context, executable: Process.Executable, arguments: List<String>, stdin: Bytes)` | `Process.Command!` |

Executable and directory paths must be absolute and contain no NUL.
Arguments and environment values must contain no NUL.
Environment names must be nonempty and contain neither NUL nor `=`.
Construction keeps exact lexical data without access, path resolution, or shell expansion.
The context supplies the environment and directory. Construction does not observe additional host inputs.

Command activation declares metadata capability `Process.run` and nominal success type `Process.Result`.
`Process.run` is not a callable builtin.
The earlier pure checkpoint provided no result runtime values or accessors.
The implemented host-independent core now provides results and termination accessors through trusted Go injection.
Without Process grants, ordinary execution remains available. Only attempted Process activation returns `runtime.process-unavailable`.
Configured Process policy on an unsupported backend fails session open before program evaluation.
Lo `onError` cannot handle this host diagnostic.
Constructor diagnostics and nominal command snapshots omit argument, environment, and stdin payloads.
Automatic context output and nested snapshots omit environment and directory payloads.
Explicit lookup and output of selected environment values remain available.
Inspection adds no access or launch authority.

The inspector adds an optional `process` object to command targets and capabilities.
It contains `context`, `executable`, `arguments`, and `stdin` input descriptions.
The context records source, origin, explicit inputs, base context, overrides, and removals when present.
Origins are `inherited`, `explicit`, `derived`, or `dynamic`.
Each description uses ordinary `kind`, `type`, `node`, `span`, and `dependencies` fields, with `value` or `expression`.
It also contains `execution_profile: {required: true, status: "unavailable"}`.

Without host configuration, source inspection reports this unavailable profile status. It does not establish runtime enforcement.
Explicit executable descriptions trace the supplied `File.Path`.
Discovery descriptions keep the context, name, and dynamic dependencies.
Dynamic command descriptions retain whole-command dependencies.
Text inspection shows `execution_profile=required/unavailable` and the named inputs.
Source literals remain inspection data, while runtime snapshots omit sensitive payloads.

The standard runner opens one canonical `processhost.Session` for `run`, `serve`, and server callbacks.
The session owns native policy, the executable registry, participating File adapters, and retained shutdown.
`Close(ctx)` closes admission and starts one shutdown owner. Its context bounds only the caller wait.
`WaitRetained()` joins that same owner without a cleanup retry. A caller timeout does not release ownership.
Custom Go boundaries are trusted host implementations and must enforce qualification.
Generic commit execution validates the result type without a universal host qualification gate.
HTTP request handlers can commit Process commands under the same configured session. Host failures stop the server.
HTTP `onError` remains pure, so a Process commit there fails with `check.effectful-callback` before execution.

Qualification remains mandatory before standard-host native Process activation acceptance.
Bounded repository-contained qualification probes have a separate review gate.
This phase authorizes no new probe launch.
This earlier pure checkpoint did not establish native execution. Current canonical public acceptance passed. Final stack CI and independent completion review passed.
The earlier File/HTTP CI remains valid for its recorded scope.
Final focused interpreter validation passed in 2.934 seconds.
Final focused native Process validation passed in 14.086 seconds.
Current-stack CI passed, including the pure Process phase.
Independent review approved the implemented pure Process checkpoint.

### Host-Independent Results and Lifecycle Core

Status: the host-independent result and lifecycle core is implemented.
The [lifecycle evidence](evidence/process-start-lifecycle.json) records focused checks, race checks, and current-stack CI.
Current-stack CI passed with interpreter integration in 39.879 seconds and generated-native integration in 462.176 seconds.
Independent final review approved the pending-start correction without a remaining finding.
This core can precede native execution profile qualification.
It uses an injected OS boundary without an OS launcher or CLI execution opt-in.
`NewProcessLifecycle` and `WithProcessLifecycle` are trusted Go embedding APIs only.
Without Process grants, ordinary execution remains available. Only attempted Process activation returns `runtime.process-unavailable`.
Configured Process policy on an unsupported backend fails session open before program evaluation.

The selected result shape is `{termination: Process.Termination, stdout: Bytes, stderr: Bytes}`.
`Process.Termination.kind` is `exited` or `signaled`.
Fallible `Process.Termination.exit_code` and `Process.Termination.signal` accessors select the matching variant.
Neither type has a Lo constructor.
The core derives an immutable invocation from the exact executable, `argv[0]`, environment, directory, and stdin.
Every nonnil child returned by `Start` transfers ownership, including a child returned with an error.
An owned launch can still wait for OS process creation.
The mandatory `StartConfirmed()` query and `start_confirmed` progress field separate creation evidence from ownership.
The [lifecycle contract](adapter-design.md#host-independent-results-and-lifecycle-core) defines the query and settled-release guarantees.
The core closes each pipe exactly once.
Pipe `Close` must return promptly and unblock I/O even when it reports an error.
One `Wait` continues after request cancellation through a fresh bounded cleanup context.
`Stop` covers descendants.
Successful `Release` means creation is finished and no owned primary, descendant, or private resource is left.
With successful release and no creation evidence, cancellation does not require a termination value.
Unresolved creation keeps ownership and registry quarantine.
Returned progress does not change after a later creation observation.

Success requires complete stdin delivery, stdout and stderr EOF, known termination, joined I/O, and successful `Release`.
Early stdin closure produces ordinary `Process.ExecutionFailed`.
Nonzero exit and signal termination remain completed-process data.
Cancellation and limits remain host failures.
Missing termination after known creation, or unresolved containment, produces host cleanup failure and overrides an ordinary failure.
Safe evidence preserves the original failure class and stage.

Trusted Go `ProcessFailure` keeps bounded partial stdout and stderr bytes.
Lo errors and automatic diagnostics keep counts and status only, without raw partial bytes.
Before result charging, a completion summary keeps termination and byte counts without payloads.
`MaxProcessOutputBytes` is 1 MiB per stream.
`DefaultProcessLimits` uses a five-second execution timeout and a one-second cleanup timeout.
Configured durations must be positive, and each stream limit must be from one byte through 1 MiB.

Pipe-close cleanup errors remain host failures with `cleanup_complete: false`.
A failed `Release` followed by successful `Stop` and a second successful `Release` remains a host failure.
That recovered cleanup records `cleanup_complete: true`, without an unresolved-containment claim.
Execution timeout and cancellation produce host cancellation.
Output overflow produces a host limit failure.
Ordinary activation errors use catalog kind `Process.ExecutionFailed`.

1. Add public integration fixtures for the approved result and lifecycle contract.
2. Confirm the expected failures before production changes.
3. Implement the host-independent core through an injected OS boundary.
4. Verify the core and unchanged standard-host rejection in interpreter and native paths.
5. Run final stack CI and obtain independent checkpoint review.

These checks do not qualify a native launcher or its confinement.
Native launch remains dependent on qualification.

### Qualified Native Execution Phase

The selected native driver and profile passed [thirteen host cases](evidence/process-host-native.json).
The [startup suite](evidence/process-startup-native.json) passed eight cases with affirmative permits and completed cleanup.
The [local IPC suite](evidence/process-ipc-local-probe.json) passed selected-slot normalization with positive local controls.
These results do not prove named IPC denial or arbitrary inherited endpoint removal.
The initial inherited-endpoint control remains inconclusive.

The canonical session checks the pinned helper, command registry, read roots, and selected backend before admission.
Apply the [selected native admission limits](adapter-design.md#selected-native-admission-limits) without restricting pure constructor validation.
The fixed launcher installs confinement before command input receipt.
It requires a separate affirmative permit before target execution. EOF without that permit aborts startup.
Command environment values do not enter the launcher environment.
The child cannot write files, use the network, or create descendants under the selected profile.
Only selected data read roots and the declared command channels are available.

One session owns Process and participating File adapters through interpreter, generated-native, and server execution.
`Close(ctx)` closes admission and starts one shutdown owner.
`WaitRetained()` joins that same owner without a cleanup retry.
A bounded caller return does not prove termination or release.
If cleanup remains unresolved, keep the host alive with its owner and quarantined admission.

The closed-gate behavioral red preceded canonical `Open` activation.
The [public acceptance lane](evidence/process-public-native.json) then passed all 24 cases through interpreter and generated-native entry points.
The cases include normal, nonzero, and signal results, raw bytes, policy denials, wall timeout, server callbacks, and File composition.
Cleanup completed, sealed artifacts stayed unchanged, and the controller needed no emergency signal.
Final stack CI and independent completion review passed.

Without Process grants, ordinary execution remains available. Only attempted Process activation returns `runtime.process-unavailable`.
Configured Process policy on an unsupported backend fails session open before program evaluation.
Qualification applies to the selected backend/profile, without a simultaneous macOS and Linux requirement.
There is no unrestricted execution option.

### 6. Composition and Documentation

Status: references include results and injected lifecycle behavior.
HTTP-to-File composition passes in both execution backends.
Current-stack CI and final stages 1–3 checkpoint review passed.
Injected Process-to-File composition and recovery passed the race selection.
Injected lifecycle stack CI passed.
Canonical native Process composition acceptance passed. Final stack CI and independent completion review passed.

1. Verify a file transformation with explicit decode, pure transformation, encoding, activation, and receipt use.
2. Verify an HTTP exchange followed by a conditional File action.
3. Verify a command whose output supplies another typed action.
4. Update the log cleanup fixture to use receipts without application-specific adapter functions.
5. Verify empty selections, changed entries, open-file observations, and partial completion independently.
6. Update reference navigation, operation indexes, examples, and backend status.
7. Complete final stack validation, then obtain independent review of changes and validation evidence.

These examples test one shared contract from different domains.
No example alone defines the adapter API.

## Acceptance Matrix

| Contract | Public acceptance evidence |
| --- | --- |
| Inert construction | Construct and inspect every target without files, connections, or children created. |
| Result typing | Accept each target's result type. Reject incompatible result use and noncommittable targets. |
| Failure order | Fail a bound commit and prove that later writes or requests do not execute. |
| Repeated activation | Evaluate two commits and observe two attempts, without an exactly-once delivery claim. |
| Observation order | Change a controlled source between reads and keep the two results in both backends. |
| Callback purity | Reject direct and indirect reads or commits in every pure callback position. |
| Stored callbacks | Inspect handler requirements while server construction remains inert. |
| Progress | Retain confirmed effects when a later operation, callback, or resource release fails. |
| HTTP outcomes | Return valid error statuses as data. Distinguish transmission failure from a known remote result. |
| Process outcomes | Return nonzero exit and signal data. Distinguish launch failure and host cancellation. |
| Authority | Deny calculated targets outside grants and Process operations outside the enforced profile. |
| Lifetime | Bound output and terminate owned descendants. Keep server lifetime independent of receipt copies. |
| Repository boundary | Keep every acceptance input, attempted file target, helper, and output inside Lo. |

## Validation

Use the repository Make targets and their configured local tool state.
Start each behavior change with its failing public integration fixture.
Run the smallest affected interpreter and native suites during each stage.
Serialize checks that share runtime distribution outputs.
Use different output directories for independent runtime builds.
The validated stages 1–3 checkpoint precedes the approved pure Process phase.
Run `CI=1 make ci </dev/null` once for all current source changes.
This CI result does not complete the full adapter plan.
The new native integration passed public acceptance and final stack validation.
Use focused checks for corrections, then repeat final CI after the last correction.
Review corrected findings and final evidence before completion.

Use loopback protocol fixtures and deterministic process helpers inside Lo.
Use software environments for all acceptance.
Report cross-compilation separately from execution.
Do not claim Process confinement from compilation or ordinary command success.

The [design review record](evidence/adapter-design-review.json) describes the earlier design-only acceptance.
The [implementation review](evidence/adapter-implementation-review.json) records the current stages 1–3 results and review limits.
The current focused commands are:

```bash
CI=1 make test-integration TEST_ARGS='-run "TestAdapter.*Receipt|TestTypedCommit|TestAdapterPurity|TestInspectionListContext|TestInspectionCaseHeadReads|TestEvaluatorMatchRegions" -count=1' </dev/null
CI=1 make test-native TEST_ARGS='-run "TestAdapter.*Receipt|TestTypedCommit|TestAdapterPurity|TestInspectionListContext|TestInspectionCaseHeadReads|TestEvaluatorMatchRegions|TestFile" -count=1' </dev/null
```

The interpreter command passed in 1.423 seconds.
The native command passed in 31.031 seconds.
The native selection included all `TestFile` cases.
The independent architect reviewed source and tests without a Make run.
No blocking stage 1–2 finding remained.

The stage 3 source extends activation evidence beyond receipt-only results.
HTTP response evidence must not expose body or header payloads.
Focused interpreter/native cases verify this result category.
Independent source review closed the current stage 3 findings.

The header-value fixture first reported missing `HTTP.Headers.new` operations.
Its focused interpreter suite then passed in 0.866 seconds.
Raw valid header-list arguments exposed payloads in automatic trace frames during the later regression.
The frame summary fix passed focused interpreter and native unions in 1.110 and 17.812 seconds.
The combined client, header, trace, catalog, File, and Flow suites passed in both execution backends.
The interpreter run took 22.727 seconds, and the native run took 138.832 seconds.

The combined commands are:

```bash
CI=1 make test-integration TEST_ARGS='-run "TestAdapterHTTP|TestDiagnosticTrace|TestVerifierBuiltinContracts|TestRunBuiltinCatalog|TestTypedCommit|TestAdapter.*Receipt|TestAdapterPurity|TestFile|TestFlowFold" -count=1' </dev/null
CI=1 make test-native TEST_ARGS='-run "TestAdapterHTTP|TestDiagnosticTrace|TestVerifierBuiltinContracts|TestRunBuiltinCatalog|TestTypedCommit|TestAdapter.*Receipt|TestAdapterPurity|TestFile|TestFlowFold" -count=1' </dev/null
```

The native run uses compiled Lo entry points for its applicable CLI cases.
Injected budget and cleanup API cases use the shared Go runtime.
Those API cases are not separate compiled Lo entry points.
A Boundary failure emulates cleanup class and data through `Program.Run` and `ReportRuntime`.
No actual connection-close failure was injected.

The shared STE helpers are outside Lo and were not executed.
The producing agent used the verified official reference for a limited review of changed prose.
Local link and mechanical checks do not certify STE compliance.
Historical File-only evidence does not establish later adapter contracts.
The current HTTP evidence is recorded separately.
Initial current-stack `CI=1 make ci </dev/null` failed at integration after 60.053 seconds.
Gateway failure cases lacked recoverable completed state.
One native budget fixture also used observing Match case heads.
The fixture now uses pure heads and observations in its selector and selected branch.

The approved correction adds `Error.flow_progress: List<Record>`, with empty default metadata.
Failing folds prepend reserved snapshots from outermost to innermost without removal of equal records.
Pure handlers preserve the original error metadata, while fresh errors reset it.
Handled failures must not contaminate later errors.
The final correction passed its interpreter union in 7.578 seconds and native union in 37.510 seconds.
These runs include the budget regression and final top-level no-stale assertion.
Rejected Error and failed fallible values keep their reserved progress in host limit diagnostics.
Actual-code source review closed the budget finding with no further correction blocker.
Historical stages 1–3 `CI=1 make ci </dev/null` passed with exit status `0`.
Full interpreter integration took 54.454 seconds, and full native integration took 482.811 seconds.
Runtime bundle checks, formatting, Go vet, the build, and whitespace checks passed.
The earlier independent checkpoint review approved stages 1–3 without a remaining source finding.
The reviewer examined source and evidence without execution of tests.
The injected Process core and composition are implemented.
The selected native implementation passed qualification and canonical public composition acceptance. Final stack CI and independent completion review passed.
The F024 checkpoint records interpreter and native stack validation separately from these earlier results.

### Contexts, Discovery, And Cleanup Scopes

The current canonical constructor is `Process.Command.new(context, executable, arguments, stdin)`.
`Process.Context` is nominal and immutable, with environment and absolute-directory data but no execution authority.
The runner captures the initial environment and physical directory once before evaluation.

Repeated observations and server callbacks use those same inputs. Trusted embeddings and fixtures can inject them.
`Process.Context.current` and `Process.Executable.resolve` are host observations.
Explicit context construction, overrides, removals, directory changes, lookup, and command construction remain pure.
Empty values differ from absent variables. Context changes leave their inputs unchanged.

Discovery selects the first executable candidate from the context's `PATH` before execution authorization.
Missing `PATH` means absence. Empty, relative, and noncanonical entries fail discovery.
Explicit paths must be canonical and absolute. Symlinks resolve to the candidate's target.
An unauthorized selected candidate fails without a search for a later candidate.
Registered identity checks remain effective through activation and launch.

The shared Go, C, and Rust transport permits 256 arguments including `argv[0]`, and 4096 environment entries.
Each argument or environment entry has a 65536-byte limit. The command body has a 1048576-byte limit.
The launcher keeps its fixed environment separate from child environment transport.

Automatic output and nested snapshots redact context payloads. Explicit lookup of selected values remains available.
Inspection records context origins, overrides, dynamic dependencies, and required execution authority.

Cleanup policy selects mutation roots and exact Unix endpoints for each initial executable.
Registered exec chains retain that initial scope. Forked and spawned descendants remain denied.
Data-write admission checks bounded root inventories, directory identity, and hardlink ownership across write and prune roots.
The [adapter design](adapter-design.md#cleanup-execution-profile) owns complete scope restrictions and native admission limits.

Local fixtures cover package arguments, inherited values, six overrides, optional absence, closed input, captured output, and typed termination.
Separate fixtures cover Docker-like Unix requests, responses, and confirmed daemon effects after client cancellation.
These results do not qualify installed package managers or Docker/buildx, or complete mac-cleanup workflow behavior.

The F024 checkpoint records interpreter integration, native integration, semantic checks, and qualification-tool checks.
The Metal-linked Process fixture exercised no GPU operation.
The [checkpoint](evidence/process-context-cleanup/checkpoint.json) preserves exact source identities, raw results, and review status.

## Deferred Work

The current contract does not include cleanup locks, extra file metadata, file watching, HTTP retry policy, or live command pipelines.
Before implementation, specify each addition through the shared operation contract.
Higher-order adapter functions remain available for justified domain semantics.
The full mac-cleanup workflow also depends on the other inputs and policies in its application plan.

The earlier pure Process checkpoint passed `CI=1 make ci </dev/null` with exit status `0`.
Full interpreter integration took 43.391 seconds, and full native integration took 522.053 seconds.
Runtime bundle verification, formatting, Go vet, tests, and the final build passed.
The source finding is closed, and independent review approved the pure Process checkpoint.
This earlier checkpoint did not qualify native execution. Current implementation and final stack CI passed. Independent completion review passed.

The latest lifecycle checkpoint passed `CI=1 make ci </dev/null` with exit status `0`.
Interpreter integration took 44.126 seconds, and full native integration took 481.452 seconds.
Runtime bundle verification, formatting, Go vet, all tests, and the build passed.
The Process race selection passed in 5.162 seconds, including injected composition and recovery.
Bounded source review approved the cleanup corrections.
Independent final review approved the host-independent results and lifecycle checkpoint without blocking findings.
This earlier checkpoint covered injected execution. Native qualification and public composition have since passed. Final stack CI and independent completion review passed.
