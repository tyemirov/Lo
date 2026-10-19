# File Adapter Implementation Plan

The File adapter and Gateway log category are implemented in Lo.
The [design](file-adapter-design.md) defines their current contracts.
The [mac-cleanup plan](mac-cleanup-plan.md) covers the remaining complete-script work.
The [shared adapter plan](adapter-plan.md) controls future adapter changes.
The completed stages below record the earlier File workflow acceptance.
The nominal target and receipt update passed final stack CI.

## Contents

- [Scope and ownership](#scope-and-ownership)
- [Implemented stages](#implemented-stages)
- [Acceptance matrix](#acceptance-matrix)
- [Validation](#validation)
- [Remaining scope](#remaining-scope)

## Scope and Ownership

Every source change, document, fixture, temporary file, cache, and generated output stays inside Lo.
The [repository boundary](../.mprlab/POLICY.md#lo-repository-boundary) applies without exceptions.
Gateway identifies a log category only.
The acceptance workflow uses repository fixtures and an injected cutoff.
It does not access another repository, an installed script, or real application data.

The primary agent owns integration and final validation.
Separate workers own bounded runtime and native-build changes.
The architect reviews shared contracts and final evidence.
No release, publication, installation, or real cleanup is part of this task.

## Implemented Stages

| Stage | Implementation |
| --- | --- |
| Shared purity | List callbacks, Match predicates and heads, self-handlers, and adapter `onError` exclude observations and commits. |
| Typed activation | Nominal File targets, bound commit syntax, File receipts, and actual server startup receipts. |
| Entry values and discovery | Nominal `File.Entry`, six projections, opaque session provenance, exact list grants, bounded sorted discovery. |
| Removal and progress | Pure nominal `File.Remove.new` construction, exact removal grants, complete preflight, ordered removal, and confirmed-prefix diagnostics. |
| Open-file observations | Separate grant, descriptor-bound host probe, bounded output, explicit parsing errors, cancellation, and typed paths. |
| Sequential activation | `Flow.fold` with named callbacks, ordered commits, first-failure termination, and cumulative state diagnostics. |
| Workflow policy | Missing-directory status, ASCII filename predicate, strict cutoff, open-file exclusions, hardlinks, and changed-candidate skips. |
| Inspection and native execution | Static read/activation reports and shared interpreter/native contracts. |
| Build confinement | Repository-local Make state and required validated `LO_BUILD_ROOT` for native builds. |

`File` has no `IO` parent and does not convert itself into a collection.
`File.list` returns a list of observations.
List callbacks cannot include host observations or commits, including transitive calls.
The workflow uses `Flow.fold` for explicit sequential activation.

Public-entrypoint tests preceded production changes for new language behavior.
Initial failures included unknown File operations, absent `Flow.fold`, missing-directory grant rejection, and hardlink rejection.
Review regressions also exposed malformed probe output and missing directory status.
The output-limit regression exposed an inherited buffer method that bypassed the bounded writer.
The implementation now uses a private buffer and validates probe records before publication.

## Acceptance Matrix

| Scenario | Required result | Maintained evidence |
| --- | --- | --- |
| Ordered direct discovery | Complete sorted list, including hidden entries and metadata only | [File workflow tests](../tests/integration/file_workflow_test.go) |
| Empty directory | Successful empty selection, with no probe | [Cleanup tests](../tests/integration/gateway_cleanup_test.go) |
| Missing directory | Observable `directory_status: absent`, with no probe | Cleanup tests |
| Missing or wrong grant | Host denial before access or mutation | File workflow and [File failure tests](../tests/integration/file_failures_test.go) |
| Exact root and descendants | Exact root only for list, removal, and probe root selection | File failure tests |
| Inert missing grant | Later creation cannot acquire authority | [Probe binding tests](../tests/integration/file_probe_binding_test.go) |
| Symlinked root or child | Reject root traversal, observe child without traversal, reject child removal | File failure tests |
| Hardlinks | Remove selected basenames and retain unselected links | [Probe and policy tests](../tests/integration/file_probe_test.go) |
| Duplicate basename or mixed roots | `File.InvalidSelection` before mutation | File failure tests |
| Entry, directory, work, or value limit | `runtime.limit` without partial listing or false success | File failure and probe tests |
| Foreign session entry | Reject provenance, including HTTP boundaries | File failure tests |
| Invalid filename | `File.InvalidName` | File failure tests |
| Discovery metadata failure | `File.ListFailed` | File failure tests |
| Changed or absent candidate | Generic batch fails, cleanup workflow skips and continues | Cleanup tests |
| Other removal error | Stop suffix and retain completed prefix | File failure and cleanup tests |
| Replaced probe path | Probe the retained directory only | Probe binding tests |
| Probe exit status 0 or 1 | Accept valid output with empty stripped stderr | Probe and policy tests |
| Probe launch, stderr, or parse error | `File.OpenFilesFailed`, with no cleanup mutation | Probe and cleanup tests |
| Probe cancellation or overflow | Stop the actual child and return the host diagnostic | Probe binding and cleanup tests |
| Nested fold failure | Outer and inner completed prefixes plus in-flight removal progress | [Flow tests](../tests/integration/flow_fold_test.go) |
| Session or grant cleanup failure | Preserve original error class and completed fold state | Flow tests |
| Preview and inspection | No reachable commit, no removal authority, no mutation | Cleanup tests |
| Exact cutoff and filename | Retain exact-cutoff files and reject malformed basenames | File workflow tests |
| Build paths | Confine subprocess caches and temporary output inside Lo | [Native path tests](../tests/integration/native_paths_test.go) |

## Validation

The Makefile supplies all build and test state under `_scratch/build-state/`.
The runtime-bundle generator and integration setup share generated files.
Run these targets sequentially:

```bash
CI=1 make test-integration TEST_ARGS='-run TestFlowFold\|TestFile\|TestGatewayLogCleanup\|TestNativeBuildPaths\|TestNativeBuildRootValidation -count=1' </dev/null
CI=1 make test-native TEST_ARGS='-run TestFlowFold\|TestFile\|TestGatewayLogCleanup -count=1' </dev/null
CI=1 make ci </dev/null
```

Acceptance uses the CPU interpreter, native macOS arm64 artifacts, and controlled filesystem/process fixtures inside Lo.
Injected schedules exercise races and failures that ordinary execution cannot reproduce reliably.
Native CLI fixtures separately exercise step-limit failure and cumulative progress.
Linux cross-compilation can validate the target source contract, but does not prove Linux execution.
No container or external validation environment is required.

### Earlier Workflow Validation

The earlier validation status is recorded in the [reference audit](language-reference-audit.md#file-adapter-update).
Independent review findings are resolved.
The earlier File workflow `make ci` passed, including interpreter and native integration suites.
These results do not qualify the current nominal target and bound commit changes.
The maintained example also cross-compiled for Linux arm64 without Linux execution.
The completion checks include document links, language checks, catalog coverage, and `git diff --check`.
Task-owned temporary evidence is removed after useful results enter maintained tests and documents.
The shared Make cache remains available for later repository tasks.

### Current Adapter Update

The current update adds the shared purity contract and nominal File targets.
`UTF8.Bytes.new` explicitly encodes text for write and append.
Bound commits return `File.WriteReceipt`, `File.AppendReceipt`, or `File.RemoveReceipt` after activation success.
Server startup returns `HTTP.ServerReceipt` with the actual listener address and port.

The [purity tests](../tests/integration/adapter_purity_test.go) cover observations, commits, conditional reads, and stored handlers.
The [receipt tests](../tests/integration/adapter_receipts_test.go) cover nominal target results and failure behavior.
Focused interpreter and native validation passed.
Independent source review closed the stage 1–2 findings.
The [implementation review](evidence/adapter-implementation-review.json) records commands, results, and review limits.
Final stack CI passed.
HTTP client activation and the selected native Process profile passed public acceptance.

## Remaining Scope

The Gateway log category is the first complete File workflow.
The complete mac-cleanup script still needs typed host inputs, category continuation, cleanup locking, and additional file metadata.
Typed Process results are implemented. The selected native profile prohibits child writes and cannot run cache-cleanup commands.
Implement shared adapter semantics before further application-specific extensions.
The [File stage](adapter-plan.md#2-bound-activation-and-file-targets) adds nominal actions and typed receipts.
Its implementation uses `File.Write.new`, `File.Append.new`, and `File.Remove.new`.
Bound commits publish operation-specific receipts after activation success.
The stage passed its final stack validation checkpoint.
Those requirements do not authorize access outside Lo.
