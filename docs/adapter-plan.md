# Adapter Implementation Plan

Status: design completed. Runtime implementation has not started under this plan.
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

This document plans future code changes.
It does not authorize external operations or claim that the proposed APIs already execute.
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

1. Add public checker fixtures for observations in List callbacks, predicates, and error handlers.
2. Add positive fixtures for conditional observations in selected Match branches and ordered `Flow.fold` callbacks.
3. Confirm the expected failures against the current checker before production changes.
4. Update the shared effect analysis and every callback position from the design table.
5. Reconcile specification sections 5.6, 8, 10, 12, 22.2, and 23.3 with the selected rules.
6. Verify unchanged deterministic tensor training and separate device requirements.

Acceptance depends on accurate observation summaries and no speculative boundary access.

### 2. Bound Activation and File Targets

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

1. Specify enforceable profiles for each selected host backend.
2. Identify mechanisms for executable identity, filesystem access, network access, descendants, and resource release.
3. Add confined adversarial helpers with all attempted filesystem targets inside Lo.
4. Verify that denied operations fail before their external effects occur.
5. Verify that descendants cannot escape the profile or survive successful completion.
6. Verify rejection of unsupported profiles before executable resolution or launch.
7. Obtain independent review of the enforcement evidence before Process acceptance.

This stage is a prerequisite for general Process execution.
An unavailable mechanism leaves Process unavailable on that backend.
Do not replace this gate with executable allowlisting or well-behaved fixture results.
File and HTTP work can complete independently of this gate.

### 5. Process Commands and Results

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
After the final stack change, run `CI=1 make ci </dev/null` once.
Use focused checks for corrections, then repeat final CI after the last correction.
Review corrected findings and final evidence before completion.

Use loopback protocol fixtures and deterministic process helpers inside Lo.
Use software environments for all acceptance.
Report cross-compilation separately from execution.
Do not claim Process confinement from compilation or ordinary command success.

For this design-only change, validate document links, anchors, source references, language, and whitespace.
Record independent review and specific validation limits in the [design review record](evidence/adapter-design-review.json).
No runtime test result from the earlier File implementation qualifies these proposed APIs.

## Deferred Work

The initial contract does not include executable search, environment discovery, locks, file watching, HTTP retry policy, or command pipelines between live processes.
Before implementation, specify each addition through the shared operation contract.
Higher-order adapter functions remain available for justified domain semantics.
The full mac-cleanup workflow also depends on the other inputs and policies in its application plan.
