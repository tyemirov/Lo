# Adapter Implementation Plan

Status: stages 1–3 and the pure Process phase passed current-stack CI.
The pure Process phase also passed focused interpreter and native checks.
Independent review approved the implemented pure Process checkpoint.
Independent source review closed the current shared-contract and HTTP findings.
HTTP-to-File composition passes in both execution backends.
Process-dependent composition remains incomplete.
The latest current-stack CI includes pure Process construction, inspection, and unavailable rejection.
The earlier independent checkpoint review approved stages 1–3 without a remaining source finding.

Process qualification failed source review.
No backend is qualified.
The current candidate could not demonstrate a runnable helper, and probes stopped after independent review.

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

This document records stages 1–3 and plans the remaining code changes.
The stage status below separates implemented contracts from selected future work.
This plan does not authorize operations outside Lo.
No compatibility aliases, alternate legacy schemas, or unrestricted Process mode belong in the implementation.

## Source Ownership

| Component | Responsibility | Existing source |
| --- | --- | --- |
| Syntax and types | Bound commit statement, nominal targets, result types, and header values. | `internal/syntax`, `internal/types`, `internal/value` |
| Catalog and verifier | Shared operation metadata, activation result relation, and transitive callback rules. | `internal/catalog`, `internal/check`, `internal/ir` |
| Execution | Ordered activation, successful binding publication, failures, and progress. | `internal/eval`, `internal/execution`, `internal/runtimeops` |
| Inspection | Immediate effects, stored callbacks, calculated authority, and result types. | `internal/inspect` |
| Native compilation | Equivalent statement lowering, runtime interfaces, and runtime bundle. | `internal/codegen`, `internal/runtimebundle` |
| Adapters and host | File receipts, HTTP client/server behavior, Process profiles, and lifecycle. | `internal/adapter`, `internal/runner` |
| Acceptance | Public CLI contracts, local protocol fixtures, and interpreter/native comparisons. | `tests/integration`, `testdata`, `examples`, `docs/examples` |

The primary agent owns shared contracts and integration.
Assign one edit owner per source group during implementation.
Review shared type and catalog changes before parallel implementation.
Keep independent architecture review at the contract and final acceptance boundaries.

## Implementation Sequence

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
7. Regenerate the runtime bundle through the repository target.

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

### 4. Process Profile Qualification

Status: qualification failed at independent source review.
No backend is qualified, and no general Process adapter launch occurred.
Executable replacement, filesystem/IPC access, and work/resource controls lack the necessary enforcement evidence.
The macOS Seatbelt default-deny and fork controls are not qualified.

The scratch helper build passed.
The main candidate and unchanged-profile immediate-return child each terminated with `SIGABRT` and exit code `-6`.
Both runs had empty output and no helper observations.
The exact profile did not demonstrate a runnable helper, and probes stopped after independent review.
No external file-read allowance or profile broadening was added.
The evidence does not establish sandbox denial or platform-wide impossibility.

The first run had no core-dump limit guard.
The launch-only run applied a zero core-dump limit before child execution.
It also applied a 64 KiB limit per output file and a ten-second parent timeout.
These guards do not qualify the Process resource contract.

Static Mach-O inspection read only the scratch helper bytes.
The helper SHA-256 is `dd6ec36820a69a59bd3ab5220b5856738b39f8e9ac8bd78f450d635d78a55b88`, with CPU `CpuArm64`.
It names loader `/usr/lib/dyld` and libraries `/usr/lib/libSystem.B.dylib` and `/usr/lib/libresolv.9.dylib`.
It has no rpaths.
The inspection opened none of those referenced paths and did not launch the helper.
These names are absent from the candidate's explicit read grants.
This fact proves no runtime access, abort cause, denial, or platform impossibility.
No new probe or profile broadening occurred.

A later build-only experiment constructed an arm64 `_start: brk #0` artifact without headers or standard libraries.
It used Clang target `arm64-apple-macos11` with `-nostdlib -Wl,-static -Wl,-e,_start`.
The build passed.
Artifact inspection reported `Exec`, `CpuArm64`, and `LC_UNIXTHREAD`.
The SHA-256 is `f2709f3d825a8ef1ee0c5b2f7f4cf1021cc3350a7de8f945513cbd417537a3a2`.
The artifact has no load-dylinker, dylib, rpath, or undefined symbol.
The inspector parses the same bytes used for the SHA-256.
The `_start` symbol and ARM64 UnixThread PC both identify `0x1000002e8` (`4294968040`).
The instruction bytes are `000020d4`.
Independent local artifact decoding with `otool` also identifies `BRK #0`.
No helper launch, external referenced-path access, or profile change occurred.
This evidence establishes construction feasibility only.
It does not validate the entry ABI, startup, sandbox, executable identity, or lifetime and resource guarantees.
Independent review accepted these bounded construction claims.

Independent review rejected the proposed intentional-crash launch before execution.
A zero `RLIMIT_CORE` does not establish suppression or redirection of host crash reports.
Child IPC denial does not establish confinement of host crash-reporter output.
No launch occurred.
The primary agent removed the proposed launcher, profile, and launch Make target.
Static source, artifact, and inspection evidence remain.
Local helper disassembly shows that `runtime.exit` uses `libcCall` and a trampoline.
Syscall number constants do not establish the raw Darwin/arm64 exit ABI.
Current local evidence does not support a non-crashing fixture launch.
Independent final review confirmed a blocked qualification operation under the available repository evidence.
No concrete supported implementation or launch action remains.
The missing prerequisite is an evidenced non-crashing repository-contained execution path or demonstrated task-local control of intentional-crash reports.
Neither prerequisite is present.
Executable identity, confinement, descendants, and resource guarantees also remain unqualified.
These limits do not establish platform impossibility.
The approved static-construction evidence and unchanged-source CI remain valid.
Existing full CI remains valid for unchanged product source.
No backend is qualified, and the full adapter goal remains incomplete.


1. Specify enforceable profiles for each selected host backend.
2. Identify mechanisms for executable identity, filesystem access, network access, descendants, and resource release.
3. Add confined adversarial helpers with all attempted filesystem targets inside Lo.
4. Verify that denied operations fail before their external effects occur.
5. Verify that descendants cannot escape the profile or survive successful completion.
6. Verify rejection of unsupported profiles before executable resolution or launch.
7. Obtain independent review of the enforcement evidence before Process execution acceptance.

This stage is a prerequisite for general Process execution.
An unavailable mechanism leaves Process unavailable on that backend.
Do not replace this gate with executable allowlisting or well-behaved fixture results.
File and HTTP work can complete independently of this gate.

### 5. Process Commands and Results

Status: the pure phase is implemented, and focused interpreter validation passed.
Execution and runtime results remain dependent on profile qualification.
General Process execution is not implemented.

The plan now permits pure construction, inspection, and unavailable rejection before qualification.
This revision does not permit a launch or execution acceptance.

### Pure Process Phase

This approved phase is implemented, and focused interpreter validation passed.
It can proceed before execution profile qualification.
It adds pure constructors, inspection, and host rejection without child execution.

| Operation | Result |
| --- | --- |
| `Process.Executable.new(path: File.Path)` | `Process.Executable!` |
| `Process.Command.new(executable: Process.Executable, arguments: List<String>, environment: Map<String, String>, directory: File.Path, stdin: Bytes)` | `Process.Command!` |

Executable and directory paths must be absolute and contain no NUL.
Arguments and environment values must contain no NUL.
Environment names must be nonempty and contain neither NUL nor `=`.
Construction keeps exact lexical data without access, path resolution, shell expansion, or environment inheritance.

Command activation declares metadata capability `Process.run` and opaque static success type `Process.Result`.
`Process.run` is not a callable builtin.
This phase provides no result constructor, result accessor, or result runtime value.
CLI `run` and `serve` reject activation with host diagnostic `runtime.process-unavailable` before any executable access.
Lo `onError` cannot handle this host diagnostic.
Constructor diagnostics and nominal command snapshots omit argument, environment, and stdin payloads.
Explicit program output keeps those values.
Inspection adds no access or launch authority.
The inspector adds an optional `process` object to command targets and capabilities.
It contains `executable`, `arguments`, `environment`, `directory`, and `stdin` input descriptions.
Each description uses ordinary `kind`, `type`, `node`, `span`, and `dependencies` fields, with `value` or `expression`.
It also contains `execution_profile: {required: true, status: "unavailable"}`.
This profile status describes the current standard host, not arbitrary injected boundaries.
Executable descriptions trace the supplied `File.Path`.
Dynamic command descriptions retain whole-command dependencies.
Text inspection shows `execution_profile=required/unavailable` and the named inputs.
Source literals remain inspection data, while runtime snapshots omit sensitive payloads.

The standard runner installs `adapter.WithProcess` for `run`, `serve`, and server callbacks.
This wrapper rejects command activation before delegation or executable access.
Custom Go boundaries are trusted host implementations and must enforce qualification.
Generic commit execution validates the result type without a universal host qualification gate.
An HTTP request-handler commit returns `runtime.process-unavailable` and aborts the server.
HTTP `onError` remains pure, so a Process commit there fails with `check.effectful-callback` before execution.

Qualification remains mandatory before any launch or execution acceptance.
Executable results, Process-dependent composition, and full-plan acceptance remain pending.
The earlier File/HTTP CI remains valid for its recorded scope.
Final focused interpreter validation passed in 2.934 seconds.
Final focused native Process validation passed in 14.086 seconds.
Current-stack CI passed, including the pure Process phase.
Independent review approved the implemented pure Process checkpoint.

### Qualified Execution Phase

1. Add failing CLI fixtures for inert commands and typed completion results under a qualified profile.
2. Add constructor-failure fixtures for relative paths, NUL inputs, and invalid environment names.
3. Implement executable, arguments, environment, directory, stdin, and the specified `argv[0]` rule.
4. Implement exit and signal variants without nullable fields or irrelevant sentinel values.
5. Verify simultaneous bounded stdout and stderr capture without deadlock.
6. Verify launch failure, early input closure, output overflow, cancellation, and descendant cleanup.
7. Keep available output and execution progress on failure.
8. Verify interpreter and native result equivalence.

Acceptance depends on successful resource release as well as child termination.
Command policy remains in named Lo flows.

### 6. Composition and Documentation

Status: stages 1–3 documentation is updated.
HTTP-to-File composition passes in both execution backends.
Current-stack CI and final stages 1–3 checkpoint review passed.
Process-dependent composition, future Process validation, and full-plan review remain pending.

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
Serialize checks that regenerate or share the runtime bundle.
The validated stages 1–3 checkpoint precedes the approved pure Process phase.
Run `CI=1 make ci </dev/null` once for all current source changes.
This CI result does not complete the full adapter plan.
New pure Process work and later qualified execution each require final stack validation.
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
Process implementation and Process-dependent composition remain incomplete.
New pure Process work and later qualified execution each require final stack validation.

## Deferred Work

The initial contract does not include executable search, environment discovery, locks, file watching, HTTP retry policy, or command pipelines between live processes.
Before implementation, specify each addition through the shared operation contract.
Higher-order adapter functions remain available for justified domain semantics.
The full mac-cleanup workflow also depends on the other inputs and policies in its application plan.

The latest pure Process checkpoint passed `CI=1 make ci </dev/null` with exit status `0`.
Full interpreter integration took 43.391 seconds, and full native integration took 522.053 seconds.
Runtime bundle verification, formatting, Go vet, tests, and the final build passed.
The source finding is closed, and independent review approved the pure Process checkpoint.
Process execution is unqualified, and the full adapter plan remains incomplete.
