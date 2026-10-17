# File Adapter Implementation Plan

Status: proposed implementation sequence.
This document does not report completed adapter implementation.
The [design](file-adapter-design.md) owns the public contracts.
Gateway log cleanup is the first complete acceptance workflow.

## Contents

- [Current implementation](#current-implementation)
- [Ownership and dependencies](#ownership-and-dependencies)
- [Stage 1: Entry values and discovery](#stage-1-entry-values-and-discovery)
- [Stage 2: Removal and progress](#stage-2-removal-and-progress)
- [Stage 3: Inspection and native parity](#stage-3-inspection-and-native-parity)
- [Stage 4: Gateway coordination](#stage-4-gateway-coordination)
- [Stage 5: Complete workflow](#stage-5-complete-workflow)
- [Acceptance matrix](#acceptance-matrix)
- [Validation and completion](#validation-and-completion)

## Current Implementation

| Concern | Current source | Necessary change |
| --- | --- | --- |
| Built-in operations and boundary classification | [Catalog](../internal/catalog/catalog.go) | Add listing and removal signatures, capability names, and errors. |
| Nominal types and projections | [Types](../internal/types/types.go), [checker](../internal/check/type.go) | Add `File.Entry` and its closed projection schema. |
| Runtime values | [Values](../internal/value/value.go), [targets](../internal/value/target.go) | Add entry provenance and operation-specific effect payloads. |
| Host boundary | [Execution contract](../internal/execution/contract.go), [server boundary](../internal/adapter/server.go) | Add listing and preserve the same boundary through HTTP requests. |
| Grants and filesystem operations | [File adapter](../internal/adapter/files.go), [file IO](../internal/adapter/file_io.go) | Add exact-directory list/removal grants and identity revalidation. |
| CLI validation and diagnostics | [Commands](../internal/runner/command.go), [execution](../internal/runner/execute.go) | Add grants and partial-removal diagnostic data. |
| Interpreter and shared operations | [Evaluator](../internal/eval/eval.go), [runtime operations](../internal/runtimeops/builtin.go) | Execute listing and route removal activation without duplicate logic. |
| Error mapping | [Runtime helpers](../internal/runtimeops/helpers.go) | Preserve the selected removal error kind, argument snapshot, progress, and source trace. |
| Inspection | [Analysis](../internal/inspect/analyze.go), [report](../internal/inspect/report.go) | Represent calculated entry sets and removal capability requirements. |
| Native operation dispatch | [Code generation](../internal/codegen/operations.go), [runtime bundle](../internal/runtimebundle/bundle.go) | Include the same new operations and runtime payloads. |
| Public tests | [Activation](../tests/integration/activation_test.go), [adapter tests](../tests/integration/adapter_test.go) | Extend behavior tests through the CLI and native artifacts. |

`File.Path` currently contains text, not directory authority.
Current file effects contain one path and one content value.
The new removal payload must replace that assumption with closed operation-specific payloads.
The source-level `Effect` type remains unchanged.

`commit` currently returns no value.
The plan keeps that rule and adds progress to failure diagnostics.
List callbacks already reject transitive commits, while boundary reads have a separate summary.
There is no need for effectful list callbacks or an IO base type.

## Ownership and Dependencies

The Lo repository owns the File contract, compiler integration, runtime implementation, documentation, and software fixtures.
The Gateway repository owns cache resolution, lifecycle locking, and the application wrapper.
Gateway policy must not enter Lo's generic File adapter.

One worker must own each edited module during implementation.
Stages 1 through 3 change shared Lo contracts and must integrate in order.
Stage 4 can proceed independently after the lock protocol is accepted.
Stage 5 requires all four stages.

The current Gateway source is `internal/lifecycle/deployment_progress.go` in the Gateway repository.
Its log path can be reopened during one lifecycle.
The shared lock must cover the lifecycle, not only an individual open handle.
The existing `internal/runtimepaths` package owns cache selection.

The primary agent owns integration decisions and final validation.
The architect reviews the initial implementation plan and final evidence.
No release, publication, deployment, or real cache cleanup is part of this plan's implementation acceptance.

## Stage 1: Entry Values and Discovery

1. Add a failing CLI integration test for `File.list` and `File.Entry` projections.
2. Record rejection because the operation and type are absent.
3. Add the nominal type, public projections, opaque provenance, and typed JSON representation.
4. Add the exact-directory `--list-root` grant and host boundary method.
5. Implement bounded, ordered, nonrecursive discovery.
6. Add work, value, entry-count, and retained-handle accounting.
7. Verify immutable entries through named flows, records, filters, maps, and folds.
8. Verify separate entry registries for HTTP startup and each request.
9. Verify denial without a list grant and denial for descendant directories.
10. Verify cleanup on success, cancellation, and metadata failure.

Acceptance: a granted listing returns complete ordered observations.
No file content is read, no directory is modified, and no partial list escapes failure.
Records with matching visible fields do not satisfy `File.Entry`.
Diagnostic JSON contains no opaque provenance.

## Stage 2: Removal and Progress

1. Add a failing CLI test that constructs a removal effect without changing the fixture directory.
2. Add a failing CLI test that commits a granted selection and returns its summary.
3. Define closed append, write, and removal payloads under the existing `Effect` interface.
4. Add pure selection validation and `--remove-root` authorization.
5. Add whole-batch authorization and preflight before the first removal.
6. Revalidate each entry immediately before its removal operation.
7. Reserve diagnostic and progress storage before mutation.
8. Add ordered removal, first-failure termination, and confirmed-prefix reporting.
9. Preserve progress through Lo errors, host cancellation, limits, and cleanup errors.
10. Verify that a repeated commit fails on stale observations.

Acceptance: construction performs no IO, and only commit removes files.
Read, write, and list grants cannot substitute for removal authority.
Construction and revalidation reject observed directories, symlinks, and entries with multiple links.
Protection against replacement after the final revalidation requires the design's cooperative concurrency conditions.
An empty effect succeeds without access.
All failures preserve confirmed progress without claiming rollback.

Use injected boundary scheduling to change a file between discovery and preflight.
Also inject changes between preflight and the per-entry revalidation.
Keep the real selection, grant, error, and filesystem behavior under test.

Add a controlled race fixture for replacement after the final revalidation.
Record the unsupported uncoordinated-writer boundary instead of claiming atomic identity-checked deletion.
Do not increase timeouts to hide ordering failures.

## Stage 3: Inspection and Native Parity

1. Add failing inspection tests for listing, calculated selections, and removal commits.
2. Report discovery and removal capabilities separately.
3. Preserve known root expressions without inventing filenames from an unexecuted directory scan.
4. Add native operation dispatch and the current runtime-bundle sources.
5. Run the same listing, removal, failure, and cancellation fixtures through native artifacts.
6. Verify JSON parity, source traces, grant behavior, limits, and resulting directories.
7. Verify that native artifacts accept grants only at execution time.
8. Reject checkpoint encoding of entries and removal effects.

Acceptance: inspection performs no filesystem discovery or activation.
Interpreter and native execution satisfy one public File contract.
Existing File.read, File.write, File.append, and HTTP behavior still pass their public tests.

## Stage 4: Gateway Coordination

This stage changes Gateway implementation under that repository's applicable instructions.
It is not a request to run a lifecycle or cleanup command now.

1. Add a public integration test for a writer that retains a shared lock across close and reopen.
2. Add a cleanup test that fails busy while that lifecycle remains active.
3. Add the persistent cache-level lock identity from the design.
4. Apply the shared lock to every Gateway lifecycle and tool that accesses deployment logs.
5. Add the cleanup wrapper with exclusive nonblocking acquisition.
6. Resolve the cache through the existing Gateway runtime-path contract.
7. Capture one host time and calculate the cutoff with integer nanoseconds.
8. Select the command's fixed entry point and render only the reviewed path and cutoff literals.
9. Invoke the configured installed Lo binary without a shell and with closed standard input.
10. Retain the lock in every participating child, including after parent termination.
11. Hold the wrapper's lock reference until the child exits and its report is collected.
12. Add preview execution with the same predicate and no reachable commit.
13. Update the old cleanup entry point to use the coordinated Gateway workflow for this directory.

Acceptance: a participating active lifecycle prevents cleanup before discovery.
Two cleanup processes cannot remove concurrently.
Use synchronized parent-termination tests for both cleanup and lifecycle children.
A new cleaner remains excluded while a surviving lifecycle child retains its shared lock.
A new writer or cleaner remains excluded while a surviving cleanup child retains its exclusive lock.
Exclusion ends only when the last participating child releases its reference.
Lock failure, busy state, absent dependencies, and child failure produce explicit nonzero results.
Neither command prompts, installs dependencies, modifies source, or selects a default response.
An alternative runtime-cache setting controls both logs and lock identity.

External readers and writers do not automatically participate in this protocol.
Document the guarantee for participating processes only.
Do not present an open-file probe as an equivalent mechanism.

## Stage 5: Complete Workflow

1. Promote the [proposed example](examples/gateway-log-cleanup.lo) to a maintained executable example after stages 1 through 4 pass.
2. Create fixture logs in a task-owned temporary directory.
3. Include old, recent, exact-cutoff, malformed-name, symlink, directory, and multiple-link cases.
4. Run preview with an injected host time and assert the complete selected set.
5. Run cleanup with the same fixed time and assert the returned count and observed bytes.
6. Assert that only the selected direct regular files disappeared.
7. Assert that sibling directories and ungranted targets remain unchanged.
8. Run the same fixture through an interpreter and a native artifact.
9. Verify busy-writer behavior with real cooperating processes and event synchronization.
10. Verify partial failure and cancellation with deterministic boundary injection.

Acceptance: the real public commands exercise discovery, selection, activation, reporting, and host coordination.
All filesystem tests use isolated software fixtures on macOS arm64 and Linux arm64.
No real Gateway cache is an acceptance fixture.

## Acceptance Matrix

| Scenario | Required result |
| --- | --- |
| Empty directory | Empty selection and successful no-op commit |
| No list grant | Host denial before enumeration |
| List grant without removal grant | Preview succeeds and commit is denied before mutation |
| Write grant without removal grant | Commit is denied |
| Exact root versus descendant directory | Only the exact granted directory is accepted |
| Symlinked root | Explicit rejection |
| Child symlink | Listed without traversal, excluded by selection, and rejected by removal construction |
| Multiple links | Entry is excluded by Gateway selection and rejected by removal construction |
| Duplicate entry or mixed observed roots | `File.InvalidSelection` before mutation |
| More than 10,000 entries | `runtime.limit`, with no partial list |
| 128 versus 129 retained directories | 128 distinct directories succeed within other budgets. The 129th produces `runtime.limit`. |
| Repeated listing of one directory | Reuse one handle slot while producing new observations |
| Work or value budget exhausted before commit | Failure before mutation, with zero completed removals |
| Entry or effect crosses an execution session | Reject foreign provenance before mutation, including HTTP startup and request boundaries |
| Invalid filename encoding | `File.InvalidName`, with no partial list |
| Entry disappears during discovery | `File.ListFailed` |
| Entry replaced, resized, or retimed before removal | `File.EntryChanged` |
| Root replaced after discovery | No authority transfer to the replacement directory |
| Failure on removal index 2 | Confirmed prefix count 2, no attempt on the suffix |
| Cancellation after one successful removal | Host cancellation with confirmed prefix count 1 |
| Cleanup failure after successful removals | Host failure with retained completion evidence |
| Repeated commit | Stale-entry error, with no silent success |
| Uncommitted effect | No directory mutation |
| Inspection | No discovery, locks, or removal |
| Exact-cutoff timestamp | File is retained because comparison is strict |
| Full name policy | Only `deployment-` plus ASCII digits plus `.log` is accepted |
| Participating active writer | Wrapper fails busy with zero deletions |
| Two cleanup processes | At most one obtains the exclusive lock |
| Parent terminates while cleanup child survives | Child retains exclusion against new writers and cleaners until it exits |
| Parent terminates while lifecycle child survives | Child retains exclusion against cleanup until it exits |
| Preview entry point | Inspection finds no reachable commit. Execution with only a list grant leaves every fixture unchanged. |
| Non-default Gateway cache | Only that cache's deployment directory is granted |
| Invalid injected path literal | Source rendering preserves one string literal without code injection |

## Validation and Completion

For each behavior change, run its new public integration test before production edits.
Keep the expected failing result as evidence.
During implementation, run the smallest applicable repository target.
The planned focused commands are:

```bash
make test-integration TEST_ARGS='-run TestFileDiscovery -count=1'
make test-integration TEST_ARGS='-run TestFileRemoval -count=1'
make test-integration TEST_ARGS='-run TestGatewayLogCleanup -count=1'
make test-native TEST_ARGS='-run TestFileDiscovery\|TestFileRemoval\|TestGatewayLogCleanup -count=1'
```

These test groups are future deliverables, not existing passing tests.
Gateway uses its own repository-native test target for the wrapper and lifecycle lock.
After the final Lo stack change, run `make ci` once.
Run a failing component target during corrections, then run `make ci` once after the last correction.

Before implementation completion:

- Complete the acceptance matrix and independent review.
- Add the implemented signatures and limits to the language reference.
- Update the built-in catalog count and reference audit.
- Update the compiler design, CLI grants, and native runtime documentation.
- Record interpreter, native, and host-coordination evidence.
- Complete the documentation-language checks.
- Remove task-owned temporary files.

## Design Validation

This design task reviews source contracts and documentation.
Validation on 2026-10-16 produced these results:

| Validation | Result |
| --- | --- |
| Complete proposed example | Parsing passed. Type checking rejected the absent `File.Entry` type, as expected. |
| Filename predicate | All 15 cases passed in the interpreter and a native artifact. |
| Metadata selection | All eight cases passed with ordinary record fixtures in place of `File.Entry`. |
| Local documentation links | All 88 file links and anchors passed. |
| Documentation language | No findings in the new design, plan, or glossary additions. Ten existing README findings remain outside the added section. |
| Independent architect review | All findings resolved. No remaining material defect in the design and plan. |

The metadata fixtures verify the predicate's age, name, kind, and link-count rules.
They do not verify entry provenance, filesystem removal, or Gateway locking.
Those features remain implementation work under the stages above.
