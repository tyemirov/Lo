# Cleanup Script in Lo

Status: application design source with proposed generic operations.
The [Lo source](examples/mac-cleanup.lo) describes the cleanup workflow observed on October 28, 2026.
It is not executable with the current compiler and native runtime.
This document keeps proposed interfaces outside the implemented language reference.

## Source and Scope

The translation uses the installed `mac-cleanup` tool observed on October 28, 2026.
Its source has 553 lines and SHA-256 `a47a9d29b943ba6a8b481ec8809e1202e2860fefb0c334fc4b5f6611f4b684ea`.
The tool source was read without execution or modification.
The earlier [application plan](mac-cleanup-plan.md) did not include its current preview, inventory, recursive-cache, or whole-tree behavior.

Lo is a general-purpose language.
Application names, paths, commands, retention periods, reports, and exit decisions belong in this program.
The proposed namespaces contain generic operations for values, control flow, files, processes, and host interaction.
They contain no cleanup-specific operation or application-specific policy.
Labels such as `Gateway logs` remain application data and output text.

The source uses existing Lo declarations, exact records, named flows, projections, and calls.
It adds no import syntax, type aliases, anonymous functions, statement loops, or exception syntax.
Proposed nominal types and operations are identified below.
The Python version check is an implementation requirement of the original tool, rather than a requirement for a Lo program.
The proposed recursive removal operation requires descriptor-safe removal as part of its generic contract.

## Behavior in the Application

| Source behavior | Lo application flow |
| --- | --- |
| Boolean CLI options, mutually exclusive modes, help, and argument errors | `ArgumentFlags`, `Main` |
| Exclusive nonblocking lock on the program entry file | `Locked` |
| Reject effective UID zero with exit status 2 | `Locked` |
| Initial free-space report and environment overrides | `Started` |
| Report-only allocated-storage inventory | `StoragePaths`, `StorageReport`, `StorageInventory` |
| Preview without cleaners, open-file probes, or removal | `Action`, `PreparePip`, and file application flows |
| Optional pip probe before package commands, only during cleanup | `DetectPip`, `PipWithPython` |
| npm, Go, uv, pnpm, then available pip | `CommandTasks`, `Workflow` |
| Direct npm logs, `.log`, and 30-day cutoff | `StaleFiles`, `StaleCandidate` |
| Five explicit recursive cache roots with a 7-day cutoff | `CacheTargets`, `StaleCache`, `CacheWalk` |
| Cache allocation, confirmed deletion, skips, and partial-error reports | `CacheState`, `CacheSummary` |
| Selected Xcode shared trees and project `Build` trees | `XcodeTargets`, `XcodeDirectoryTarget` |
| Whole-tree eligibility and before/after equality, never partial age-based eviction | `TreeSupported`, `SameTree`, `StaleBuildTree` |
| Both validated temporary roots, `.tmp` and `.temp`, with a 7-day cutoff | `TempRoots`, `TemporaryFiles` |
| Explicit Docker context, builder, endpoint checks, and ordered commands | `RejectAmbient`, `DockerApply`, `BuilderField` |
| Strict direct log discovery, ASCII digits, 30-day modification age, and distinct probe framing | `GatewayName`, `GatewayPresent`, `GatewayOpened` |
| Continue after ordinary task failure and retain its label | `Attempt`, `AddFailure`, `Xcode` |
| Allocation summary, final free-space report, and final status | `Workflow`, `Finish`, `SummaryFailure`, `SummarySuccess` |

The cache root list includes Homebrew downloads with the current recursive 7-day policy.
It does not use the earlier suffix-restricted 30-day proposal.
Xcode selection preserves `SourcePackages` and unrelated project content.
The storage inventory is allocated size, not a reclaim estimate.
Preview totals remain upper bounds because preview does not probe open files.

Every candidate probe accepts exit status 0 or 1.
Ordinary files and caches reject any stderr bytes and use NUL-framed exact-path batches of 100.
Ordinary-file and cache probes reject names outside the current batch.
They reject incomplete NUL fields, embedded line breaks, invalid process identifiers, and unknown field tags.
Nonempty output must contain a file name.
The [lsof field contract](https://lsof.readthedocs.io/en/latest/manpage/#output-for-other-programs) defines NUL terminators and newline set boundaries.
The direct log probe uses newline framing and rejects nonempty stripped stderr.
Whole-tree eviction rejects any probe stdout because an observed directory handle also prevents eviction.

The file workflows compare fresh observations with their originals.
Their pure guards contain owner, link-count, age, and equality rules.
Those rules remain visible Lo code.
`File.Remove.checked` still performs its mandatory generic checks before the application predicate.

## Reused Implemented Operations

The source reuses `Host.Invocation.current`, Process context operations, executable discovery, command construction, and typed termination results.
It also reuses immutable File entries, `File.Remove.checked`, `Flow.fold`, pure List processors, records, maps, and scalar comparisons.
UTF-8 conversion uses existing `UTF8.Bytes.new` and `UTF8.String.new`.
The [CLI contract](cli-runtime-design.md) defines implemented Console, terminal, and declared option operations.
The [executable CLI example](examples/cli-cleanup-preview.lo) verifies those operations without cleanup effects.
The application does not require another Process namespace or a command for each named tool.

The current reference remains authoritative for these implemented interfaces.
Proposed additions below do not change that reference or the catalog.
The existing exact File grants remain exact.
Recursive observation and removal require separate explicit authority.

## Direct-file Milestone

The direct-file flows now use `File.scan`, `File.list`, and checked singleton removal.
They contain no proposed path observations.
An absent directory produces a skip. Other observation failures stop the category.
The host requires exact observation and removal grants.
Filesystem observations reject symlink traversal.

`File.Remove.checked` supplies both removal checks and the application guard.
The application counts only a successful commit.
A changed or vanished entry produces a skip.
The application resolves `lsof` through the declared Process context.
Probe failures retain their category label through `RecordOutcome`.
Later independent categories continue after ordinary failures.

The integration fixtures extract these application flows and their dependencies without changes.
A controlled executable supplies probe output and changes fixture files inside Lo.
Dry run and empty selection need no Process or removal grants.
Direct-file summaries currently use decimal integers without grouping.
F031 supplies the complete report format.

## Recursive-cache Milestone

`StaleCache` now uses one retained root and the F033 descendant operations.
Only `File.DirectoryAbsent` produces an absent-root skip.
Each scan validates its returned directory owner, device, and retained identity.
Child selection excludes foreign owners and different devices.
The application preserves every directory and skips symlink descent.

Candidate probes use exact sorted batches and recheck directory ownership after execution.
Checked singleton removal supplies entry revalidation and the application guard.
The application counts only confirmed deletions.
An ordinary failure stops the category and preserves its preceding progress.
A terminal failure stops execution and retains worklist progress.

The eligible count and allocation include unsafe names that the application excludes from probes.
Those exclusions increase the skipped count.
For a completed cleanup scan, deleted and skipped counts equal the eligible count.
Allocation totals use `allocated_bytes`, not logical file sizes or measured reclaimed space.
Preview performs no probes or removal and can retain unresolved candidates.

The fixtures use the canonical cache flows, local probe executables, and trusted metadata injection.
They cover pending ancestors beyond 128 directories, foreign subtrees, owner changes, allocation totals, and cancellation after a confirmed deletion.
Cache summaries use decimal byte counts until F031 supplies presentation operations.
The full workflow remains I013 work.

## Value Operations And Draft Helpers

F030 supplies the [implemented value processor contract](value-processors.md).
It includes exact text and byte boundaries, indexed collections, stable sorting, lexical paths, globs, and command display.
The [file-policy example](../examples/file_policy.lo) executes direct-file selection with current metadata.
The [probe-fields example](../examples/probe_fields.lo) executes NUL-framed parsing with exact batch membership.
These examples have no removal effects.

The application uses typed helper flows for Boolean operations and collection policies.
These flows use current processors without additional builtins.

| Application helper | Current composition |
| --- | --- |
| `All`, `Any`, `Not` | `Match.new` and `List.fold`. |
| `ConcatStrings`, `ConcatPaths`, `ConcatInts`, `ConcatGlobs`, `ConcatTasks`, `ConcatStorageRows` | `List.flat_map` over two typed lists. |
| `PathMember`, `StringMember`, `ScalarMember`, `IntMember`, `UniquePaths` | Typed predicates and ordered folds. Path comparison uses `File.Path.eq`. |
| `StringsEqual`, `TreeRowsEqual` | Equal-length selection and indexed comparisons. |
| `PathChunks` | A positive-size guard, `List.unfold`, and `List.slice`. |
| `JoinStrings` | An ordered fold with an explicit separator. |

The public integration fixtures extract current flow definitions and their dependencies from the application AST.
These fixtures execute application helpers through the native compiler and program commands.
`ReportPath` declares its fallible result. `StoragePaths` uses an ordered flow fold for those results.
The complete application remains under I013.

F028 owns `CLI.parse` and does not add public value helper aliases.
The existing String view as `List<Scalar>` supplies scalar iteration.

F031 owns the remaining JSON and presentation proposals:

| Proposed operation | Required behavior |
| --- | --- |
| `String.format(template, fields) -> String!` | Typed substitutions, decimal groups, and field width. |
| `Float.format_fixed(value, places) -> String!` | Fixed decimal places with precision and nonfinite failures. |
| `Int.parse(text) -> Int!` | Decimal conversion with malformed-input failure. |
| `JSON.decode(bytes) -> JSON.Value!` | Generic JSON parsing. |
| `JSON.at`, `JSON.get`, `JSON.string` | Typed extraction with explicit failures. |

Glob negation applies to the Boolean match result.
The application does not require negative-mask syntax or a regex engine.
Filename digits remain an application predicate over Scalars.

## Control Composition

### Selected Effects

The application uses the implemented `Match.new`, `List.filter`, and `Flow.fold` operations for conditional activation.
`Match.new` evaluates only its selected branch.
Its selector and branches can read host state, but they cannot activate commits.
A bound result can supply the selector after an earlier flow completes its commits.
A direct committing call in the selector remains invalid.

Each selection first binds its selector once.
A Match expression then produces one integer branch identifier.
Each branch filters a list with one item for its own identifier and passes the result to `Flow.fold`.
The matching callback runs once.
Other callbacks receive empty input lists and do not run.
Successive folds carry the selected result through a valid accumulator.

All callback context arguments are previously bound values.
Branch-specific lookups, fallible projections, host observations, and commits stay inside the callback.
This placement matters because an empty fold still evaluates its argument expressions.
For example, an unselected branch must not project a failed `captured.result` or index an empty executable list.
The callbacks keep the original command order and application decisions.

The source now has 100 named branch callbacks for its 47 selections.
These callbacks use the existing language contract.
This revision removes the proposed `Flow.select` operation from the application.
The additional folds, lists, and callbacks have their normal logical charges and diagnostic frames.
The revision does not claim the cost or trace shape of a proposed processor.

### Recoverable Category Results

The application uses concrete self-handling flows for recoverable category results.
For example, `TryPerform` has the output type `Record{result: Int!, errors: List<Error>}`.
It calls `Perform` once.
Success stores the result and an empty error list.
Its pure total `CapturedPerformError` handler stores the received error in both the fallible field and a singleton error list.

The caller retains its context, task label, and accumulator.
It examines `errors` before a selected callback projects `result`.
The handler needs only its declared Error argument.
This pattern uses the existing flow-level `onError` contract and replaces all nine proposed `Flow.capture` calls.
Cancellation, host failures, and budget failures remain terminal under the implemented error contract.

The [executable control example](examples/cleanup-control-flow.lo) demonstrates selection, ordered commits, category recovery, and continuation.
It also demonstrates that a capability denial remains terminal.
The example uses only implemented operations and writes only its explicitly authorized fixture file.
The full cleanup source still uses the other proposals in this document.

### Run the Control Example

From the primary Lo checkout, run these commands.
The commands create a fresh fixture directory inside Lo.

```bash
make build
cleanup_control_repo="$PWD"
cleanup_control_work="$(mktemp -d "$PWD/_scratch/cleanup-control-example.XXXXXX")"
bin/lo check docs/examples/cleanup-control-flow.lo --format json
(
  cd "$cleanup_control_work"
  "$cleanup_control_repo/bin/lo" run "$cleanup_control_repo/docs/examples/cleanup-control-flow.lo" --format json
  "$cleanup_control_repo/bin/lo" run "$cleanup_control_repo/docs/examples/cleanup-control-flow.lo" --write-root "$cleanup_control_work" --format json -- --apply
)
cat "$cleanup_control_work/cleanup-control-events.log"
```

Preview returns `selected_count: 0` and leaves the fixture file absent.
Apply returns `selected_count: 3`, `completed: 12`, and failure labels `earlier` and `broken`.
The event file contains `first-1`, `first-2`, `broken-before-error`, `second-1`, and `second-2` in that order.
The failed category keeps its confirmed append and permits the second category to run.
These fixture actions do not execute installed cleaners.

## Dynamic Traversal

`Flow.worklist(items, state, context..., Step)` now supplies dynamic sequential traversal.
The [language contract](../specification.md#89-dynamic-worklists-and-host-resource-quotas) defines its callback signature, visit order, failure progress, and resource quotas.
New items precede pending items in their supplied order.
The cleanup application can retain its sorted depth-first visit order.
A true `stop` discards pending work and returns the completed state.

The queue has no fixed item-count maximum.
Host resource quotas bound retained value weight and simultaneous directory observations.
Completed helpers release unreferenced observations before later visits.
A long scan can exceed 128 total directory observations while retaining fewer than 128 at once.
The host can change that live-observation quota.

Value-byte and live-value quotas default to 16 MiB and 64 MiB.
Cumulative data has no default quota.

Steps and cancellation still stop excessive work.

This processor preserves existing authority boundaries.
A parent directory grant does not authorize an unopened child.
F033 supplies retained descendant authority and bounded root leases.
The application opens a child only when its work item is visited.
Retained state and pending items keep the resources they require.
Snapshots keep public data without executable authority.

## File Operations And Remaining Proposals

F032 implements `device: Int`, `inode: Int`, and `allocated_bytes: Int` on `File.Entry` and `File.Directory`.
Allocated bytes equal the native block count multiplied by 512.
These metadata fields come from the same native observation as the other fields.
Their projections remain pure.
The [filesystem observation contract](file-system-observations.md) defines these fields and fresh host measurements.

F033 implements [retained descendant observations](file-descendant-observations.md) under separate recursive grants.
The remaining draft `File.Directory.open` calls must use the current root operation or an existing retained observation.
Draft retained listings must use the selected scan contract with explicit disappearance policy.
Retained entry observations supply kind and absence results without new path observation operations.
The remaining draft path observations require application conversion under I013.

F034 supplies `File.Tree.snapshot` and `File.Tree.Remove.checked` under the [tree contract](file-tree-removal.md).
I013 must replace the draft's obsolete `File.Tree.Remove.new(parent, root)` call with a target bound to the approved snapshot.
Xcode discovery must retain parent and root observations instead of converting them to path-only tasks.

`File.Tree.entries` has type `List<Record{relative: String, entry: File.Entry}>`.
Its rows contain metadata without host authority.
The `complete` field records whether descent included every observed directory.
The root row uses `.`.
Other rows use relative names with sorted depth-first traversal.
Each opened directory must match its preceding observation.
Any vanished name or unrelated metadata failure fails the snapshot.

`Descend(context..., entry) -> Bool` is pure and total.
A false result prevents descent but preserves the directory row.
The application uses this rule to reject a foreign owner or another device without entering that subtree.
The application separately computes age, allocation, link rules, and whole-tree equality.
Mandatory tree comparison excludes directory access time because traversal can change it.
The application uses the same exclusion for its comparison around the probe.

Removal requires explicit recursive removal authority and the retained parent and root identities.
It never follows a symlink or substitutes a replacement ancestor.
The application compares two snapshots around the open-file probe before target construction.
The checked target must retain the original approved snapshot and repeat complete preflight before mutation.
These observations and deletion remain separate operations with a race window.

Retained descendant handles do not acquire authority from path text alone.
Observation and removal grants remain separate.
Resource admission and handle release require explicit tests before this example can become executable.

## Program Entry And Locks

F029 implements the entry observation and lock target used by `Locked`.
The [lock contract](program-lock-design.md) defines exact host authority, identity checks, and session ownership.

| Operation | Implemented contract |
| --- | --- |
| `Program.path()` | Observe the captured source entry or actual native executable. |
| `Lock.File.new(path)` | Construct a pure target for exclusive acquisition without a wait. Commit returns `Lock.Lease`. |

The application lock remains held through resource cleanup, including failure and cancellation.

## Proposed Generic Host Operations

| Proposed operation | Contract |
| --- | --- |
| `Clock.now_unix_ns()` | Implemented fresh signed Unix nanoseconds for each application cutoff. |
| `Process.Arguments.format_shell(arguments)` | Pure shell quoting for display only. Execution continues to use argument lists. |
| `Process.Command.with_output(command, mode)` | Pure command copy with `capture` or `forward`. Forwarding uses owned Console sinks. Stdin remains closed. |
| `Process.Command.with_timeout(command, timeout_ms)` | Pure command copy with a millisecond deadline subject to host admission limits. Application flows convert seconds to milliseconds. |

`CLI.Options` exposes `flags: Map<String,Bool>`, `help_requested: Bool`, and `help_text: String`.
All declared flags are present and false until selected.
The parser supplies `-h` and `--help` and does not accept positional operands.
The script's flags are `--dry-run` and `--report` in one exclusive group.
Help completes before lock acquisition.
Child timeouts are 1800 seconds for cleaners, 60 for pip discovery, 30 for probes, and 180 for inventory.

A command deadline produces recoverable `Process.Timeout` only after owned-process cleanup finishes.
A self-handling flow can then return error data for category reports and continuation.
Caller cancellation, session cancellation, budget failures, and uncertain process ownership remain terminal.
A timeout does not undo confirmed filesystem or daemon effects.
The command deadline is distinct from terminal caller cancellation and execution-budget limits.

Process authority remains explicit and scoped per executable.
The current profile and fixture evidence do not establish support for arbitrary installed command descendants.
The example describes those commands as application intent.
Runnable acceptance still requires supported execution profiles and repository-contained fixtures.

## Validation Status

On October 29, 2026, the public parser accepted all 301 application flows after the control revision.
The source has no `Flow.select` or `Flow.capture` calls and retains one `Flow.worklist` call.
The audit compared all 100 branch bodies with the previous source AST.
It verified unchanged selection heads, one selector evaluation, callback contexts, branch order, and accumulator chains.
Static review found no flow-call cycles or commit-bearing Match expressions.

The current catalog implements `Flow.worklist`, Console, terminal construction, declared options, and the F030 value processors.
The application now uses typed flows for the helper operations listed above.
Full application acceptance still requires conversion of obsolete file calls and the remaining value and Process operations.
Parser acceptance and structural review do not establish type correctness or runtime parity for the full application.
The compiler and runtimes must implement or remove the other selected proposals before executable acceptance.

The public control example passed `parse`, `check`, and interpreter execution.
Fresh Darwin artifacts passed native checks.
Fresh Linux artifacts passed the same checks under QEMU.
Preview selected no actions, required no write grant, and created no fixture file.
Apply preserved ordered effects before and after a recoverable category failure.
It retained existing counters, failure labels, and state.

Missing write authority remained terminal, and invalid arguments failed explicitly.
A separate exact outcome probe verified nested fallible result storage and lazy result projection in all three runtimes.

The [evidence record](evidence/cleanup-control-composition.json) contains source hashes, structural results, public commands, raw output, and native identities.
These checks use fixtures inside Lo and do not execute the full cleanup program or installed cleaners.
They do not establish large-tree capacity or complete cleanup parity.
Future application fixtures must cover preview, report, malformed output, contention, cancellation, partial progress, and final status.
All fixture paths and process state must remain inside Lo.
