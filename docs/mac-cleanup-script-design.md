# Cleanup Script in Lo

Status: application design source with proposed generic operations.
The [Lo source](examples/mac-cleanup.lo) describes the full current cleanup workflow.
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

## Proposed Generic Control Operations

### Conditional Evaluation

`Flow.select(selector, key, expression, ..., otherwise: expression)` is a proposed lazy expression processor.
Its spelling uses the current call grammar.
It requires compiler and runtime support for selected evaluation regions.
It is not an ordinary eager function call.

- Evaluate the selector once.
- Require pure total case keys with the selector's comparison type.
- Validate every branch and its result type before execution.
- Evaluate exactly one matching expression, or the `otherwise` expression.
- Permit reads and commits in the selected expression under the caller's effect contract.
- Preserve ordinary error propagation, evaluation order, and resource charges.

The current `Match.new` contract continues to reject branches that transitively commit.
The source uses `Flow.select` for those branches.
Pure branches continue to use `Match.new`.
This proposal does not add anonymous functions or change the implemented Match contract.

### Recoverable Results

`Flow.capture(context..., NamedFlow)` returns `Record{result: T!, errors: List<Error>}` for a flow with output `T!`.
It invokes the named flow once.
Success stores its result and an empty error list.
A recoverable error occupies both the fallible result and the single-element error list.
Cancellation, host faults, and budget failures remain terminal.
The application reads `errors` before it projects a failed `result`.

### Bounded Worklists

`Flow.worklist(items, state, context..., Step)` invokes a named step sequentially without a flow-call cycle.
The step returns `Record{state: S, items: List<T>, stop: Bool}!`.
The processor infers concrete item, state, and context types from that signature.
New items go before pending items in their supplied order.
This gives the application its sorted depth-first visit order.
A true `stop` ends traversal and returns the last state.
Recoverable step errors propagate unless the application captures them.

The processor bounds pending items, callback work, and live value weight.
Retained handles remain subject to explicit resource limits and session cleanup.
The proposal does not remove existing directory or value limits.
The application opens a child only when its work item is visited.
Ordinary failures retain partial counters and stop later file work before the final report.

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
`Flow.capture` can then record the failed task and continue.
Caller cancellation, session cancellation, budget failures, and uncertain process ownership remain terminal.
A timeout does not undo confirmed filesystem or daemon effects.
This proposed command deadline is distinct from the current terminal host lifetime limit.

Process authority remains explicit and scoped per executable.
The current profile and fixture evidence do not establish support for arbitrary installed command descendants.
The example describes those commands as application intent.
Runnable acceptance still requires supported execution profiles and repository-contained fixtures.

## Validation Status

On October 28, 2026, the parser accepted all 182 application flows.
The source has 47 `Flow.select` calls and one `Flow.worklist` call.
Static review found no flow-call cycles or commit-bearing Match expressions.
The API inventory identifies all 68 proposed operation names used by the source.
Independent reviews found no unresolved source design findings after correction.

The current parser accepts the full application source.
Static review checks application flow names, call cycles, command order, and the selected file policies.
Independent review compares the Lo source with the installed tool source.
No check executes cleanup, reads real cache contents, or changes an installed script.

Syntax acceptance does not prove type correctness for proposed operations.
It does not prove runtime parity.
The compiler and both runtimes must implement each selected proposal before executable acceptance.
Future public fixtures must cover preview, report, malformed output, contention, cancellation, partial progress, and final status.
All fixture paths and process state must remain inside Lo.
