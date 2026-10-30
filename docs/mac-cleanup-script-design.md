# Cleanup Script in Lo

Status: application design source with proposed generic operations.
The [Lo source](examples/mac-cleanup.lo) describes the cleanup workflow observed on October 28, 2026.
It is not executable with the current compiler and runtimes.
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
Cache probes also reject names outside the current batch.
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
The application does not require another Process namespace or a command for each named tool.

The current reference remains authoritative for these implemented interfaces.
Proposed additions below do not change that reference or the catalog.
The existing exact File grants remain exact.
Recursive observation and removal require separate explicit authority.

## Proposed Generic Value Operations

| Operations | Proposed contract |
| --- | --- |
| `Glob.new(pattern) -> Glob!` | Compile a case-sensitive wildcard pattern. Support `*`, `?`, character classes, and escapes. |
| `Glob.matches(mask, text) -> Bool` | Pure full-string match. `*` includes separators and leading dots. The application selects basenames and excludes hidden names explicitly. |
| `Bool.all(values)`, `Bool.any(values)`, `Bool.not(value)` | Pure Boolean operations over `List<Bool>` or one `Bool`. Empty all is true. Empty any is false. |
| `List.at(values, index)` | Fallible indexed lookup. |
| `List.slice(values, start, end)` | Pure ordered slice with an exclusive end. Restrict endpoints to valid bounds. Negative indices count from the end. |
| `List.concat(left, right)` | Pure concatenation of lists with one element type. |
| `List.contains(values, value)`, `List.unique(values)`, `List.equal(left, right)` | Pure typed equality, stable duplicate removal, and ordered equality. Path equality uses `File.Path.eq`. |
| `List.chunks(values, size)` | Ordered batches with a positive size and explicit invalid-size failure. |
| `List.sort(values, Compare)` | Pure stable sort through a total pair comparator with an `Int` result. |
| `List.equal_by(left, right, Equal)` | Pure ordered equality through a total pair predicate. |
| `String.starts_with`, `String.ends_with`, `String.compare` | Pure prefix, suffix, and lexical comparison. |
| `String.slice(text, start, end)` | Pure Unicode-scalar slice with an exclusive end. Restrict endpoints to valid bounds. Negative indices count from the end. |
| `String.trim`, `String.is_whitespace`, `String.split_whitespace` | Pure whitespace operations. The whitespace predicate returns false for an empty string. |
| `String.lines(text)` | Pure line split without terminators, with universal line boundaries. |
| `String.split(text, separator)`, `String.replace_all(text, old, new)` | Pure exact string operations. |
| `String.split_first(text, separator)` | Fallible split with `before` and `after` fields. Missing separators fail. |
| `String.join(values, separator)` | Pure string concatenation with a separator. |
| `String.format(template, fields) -> String!` | Typed record substitutions with decimal grouping and width fields. Invalid templates or fields fail. |
| `Float.format_fixed(value, places) -> String!` | Fixed decimal places with explicit invalid-precision and nonfinite failures. |
| `Int.parse(text) -> Int!` | Decimal integer conversion with explicit malformed-input failure. |
| `Bytes.length`, `Bytes.starts_with` | Pure byte length and prefix test. |
| `Bytes.slice(bytes, start, end)` | Fallible byte slice with an exclusive end. |
| `Bytes.split(bytes, separator)`, `Bytes.trim_start(bytes, prefix)` | Pure exact byte split and repeated leading-prefix removal. |
| `JSON.decode(bytes) -> JSON.Value!` | Parse JSON bytes without an application-specific schema. |
| `JSON.at`, `JSON.get`, `JSON.string` | Fallible array lookup, object lookup, and string extraction. |

Negation applies to the Boolean match result.
The program does not require special negative-mask syntax or a regex engine.
The exact digit rule in `GatewayName` uses ordinary scalar predicates after filename selection.

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
The proposed descendant operations below still need their own authority and ownership contracts.
The application opens a child only when its work item is visited.
Retained state and pending items keep the resources they require.
Snapshots keep public data without executable authority.

## Proposed Generic File Operations

`File.Entry` adds `device: Int`, `inode: Int`, and `allocated_bytes: Int` for this design.
Allocated bytes equal the native block count multiplied by 512.
`File.Directory` adds device and inode identity.
These metadata fields come from the same native observation as the other fields.
Their projections remain pure.

| Operations | Proposed contract |
| --- | --- |
| `File.Path.join`, `File.Path.parent`, `File.Path.name`, `File.Path.to_string` | Pure lexical path operations. |
| `File.Path.resolve(path) -> File.Path!` | Resolve links and produce an absolute canonical path. |
| `File.Path.exists`, `File.Path.is_directory`, `File.Path.is_symlink` | Fallible observations. Only absence gives false. The symlink test examines the final entry without following it. |
| `File.Directory.open(path) -> File.Directory!` | Require a canonical directory path and retain its descriptor after an identity check. |
| `File.Directory.refresh(directory) -> File.Directory!` | Compare the named canonical path with the retained identity. Reject ancestor replacement. |
| `File.Directory.list(directory)` | Strict sorted direct observations through the retained directory. A vanished child fails. |
| `File.Directory.scan(directory)` | Sorted direct observations with explicit `ENOENT` names. Other metadata errors fail. |
| `File.Directory.entry(directory, name)` | Observe one basename without following its final link. |
| `File.Directory.child(directory, entry)` | Open an observed child directory and verify its observed identity. |
| `File.Entry.observe(entry)` | Return one fresh observation through its retained parent, or an empty list for `ENOENT`. Other failures propagate. |
| `File.Tree.snapshot(parent, root, context..., Descend) -> File.Tree!` | Observe a root and descendants through retained directories without following symlinks. |
| `File.Tree.Remove.new(parent, root)` | Construct a descriptor-safe recursive removal target for one observed directory root. Only commit removes it. |
| `File.System.usage(path)` | Read total, used, and free bytes for the containing volume. |

`File.Tree.entries` has type `List<Record{relative: String, entry: File.Entry}>`.
The root row uses `.`.
Other rows use relative names with sorted depth-first traversal.
Each opened directory must match its preceding observation.
Any vanished name or unrelated metadata failure fails the snapshot.

`Descend(context..., entry) -> Bool` is pure and total.
A false result prevents descent but preserves the directory row.
The application uses this rule to reject a foreign owner or another device without entering that subtree.
The application separately computes age, allocation, link rules, and whole-tree equality.
Directory access time is excluded only in that application's tree predicate.

Removal requires explicit recursive removal authority and the retained parent and root identities.
It never follows a symlink or substitutes a replacement ancestor.
The application compares two snapshots around the open-file probe before target construction.
That comparison and deletion remain separate operations with a race window.

Retained descendant handles do not acquire authority from path text alone.
Observation and removal grants remain separate.
Resource admission and handle release require explicit tests before this example can become executable.

## Proposed Generic Host Operations

| Operations | Proposed contract |
| --- | --- |
| `Clock.now_unix_ns()` | Read fresh signed Unix nanoseconds for each application cutoff. |
| `CLI.parse(arguments, flags, name, description, abbreviations)` | Parse declared Boolean flags, exclusive groups, repeated flags, unique prefixes, and automatic help. Invalid input gives `CLI.InvalidArguments`. |
| `Console.Line.new(stream, text, flush)` | Construct an explicit line-output target for stdout or stderr. |
| `Process.Arguments.format_shell(arguments)` | Pure shell quoting for display only. Execution continues to use argument lists. |
| `Process.Command.with_output(command, output)` | Pure command copy with `capture` or inherited stdout/stderr. Stdin remains closed. |
| `Process.Command.with_timeout(command, seconds)` | Pure command copy with a timeout subject to host admission limits. |
| `Program.path()` | Observe the interpreted entry source or the native entry executable for program-scoped identity. |
| `Lock.File.new(path, access, wait)` | Construct a lock target. Activation returns a session-owned lease or `Lock.Busy` for contention. |
| `Program.Exit.new(code, stdout, stderr)` | Pure terminal result that selects the process status after session cleanup and final output. Returning an ordinary Int does not select a process status. |

`CLI.Options` exposes `flags: Map<String,Bool>`, `help_requested: Bool`, and `help_text: String`.
All declared flags are present and false until selected.
The parser supplies `-h` and `--help` and does not accept positional operands.
The script's flags are `--dry-run` and `--report` in one exclusive group.
Help completes before lock acquisition.
The cleanup lock remains held until program cleanup completes, including failure and cancellation.
Child timeouts are 1800 seconds for cleaners, 60 for pip discovery, 30 for probes, and 180 for inventory.

A command deadline produces recoverable `Process.Timeout` only after owned-process cleanup finishes.
A self-handling flow can then return error data for category reports and continuation.
Caller cancellation, session cancellation, budget failures, and uncertain process ownership remain terminal.
A timeout does not undo confirmed filesystem or daemon effects.
This proposed command deadline is distinct from the current terminal host lifetime limit.

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
The earlier catalog comparison identified 66 proposed operation names after the control revision.
The current catalog implements `Flow.worklist`, which leaves 65 proposed names.
This list records draft dependencies and does not establish a minimal language backlog.

The full application still fails type checking at the proposed `Program.Exit` type.
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
