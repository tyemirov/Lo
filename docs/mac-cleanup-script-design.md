# Cleanup Script in Lo

Status: the complete application passes type checking and focused native acceptance under I013. Final shared stack CI passed.
The [Lo source](examples/mac-cleanup.lo) describes the cleanup workflow observed on October 28, 2026.
Its named flows now use implemented language operations.
The [implementation roadmap](mac-cleanup-implementation-plan.md) records the completed implementation and software acceptance.

## Source and Scope

The translation uses the installed `mac-cleanup` tool observed on October 28, 2026.
Its source has 553 lines and SHA-256 `a47a9d29b943ba6a8b481ec8809e1202e2860fefb0c334fc4b5f6611f4b684ea`.
The tool source was read without execution or modification.
The earlier [application plan](mac-cleanup-plan.md) did not include its current preview, inventory, recursive-cache, or whole-tree behavior.

The converted Lo source has SHA-256 `61c981884c05e6c5afdc1daab1e00b1156be648d113c392075a48bf0b6f26976`.
The full workflow fixtures preserve the definitions from that source.

Lo is a general-purpose language.
Application names, paths, commands, retention periods, reports, and exit decisions belong in this program.
The language namespaces contain generic operations for values, control flow, files, processes, and host interaction.
They contain no cleanup-specific operation or application-specific policy.
Labels such as `Gateway logs` remain application data and output text.

The source uses existing Lo declarations, exact records, named flows, projections, and calls.
It adds no import syntax, type aliases, anonymous functions, statement loops, or exception syntax.
The Python version check is an implementation requirement of the original tool, rather than a requirement for a Lo program.
Recursive removal requires retained descriptors and checked observations under its generic contract.

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
| Validated temporary roots, `.tmp` and `.temp`, with a 7-day cutoff | `TempRoots`, `TemporaryFiles` |
| Explicit Docker context, builder, endpoint checks, and ordered commands | `RejectAmbient`, `DockerApply`, `BuilderField` |
| Strict direct log discovery, ASCII digits, 30-day modification age, and distinct probe framing | `GatewayName`, `GatewayPresent`, `GatewayOpened` |
| Continue after ordinary task failure and retain its label | `Attempt`, `AddFailure`, `Xcode` |
| Allocation summary, final free-space report, and final status | `Workflow`, `Finish`, `SummaryFailure`, `SummarySuccess` |

The cache root list includes Homebrew downloads with the current recursive 7-day policy.
It does not use the earlier suffix-restricted 30-day proposal.
Xcode selection preserves `SourcePackages` and unrelated project content.
The storage inventory is allocated size, not a reclaim estimate.

Preview totals remain upper bounds because preview does not probe open files.
Preview defers per-user temporary-root discovery because that discovery requires a process.
It reports this omission and scans the configured shared temporary root.

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
Direct-file summaries use decimal integers.
F031 supplies numeric conversion and report formatting.

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
Cache summaries retain exact decimal allocation counts.
Workflow totals and storage rows use the selected F031 formatting operations.
The complete workflow passed the I013 acceptance checks.

## Value Operations And Typed Helpers

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

F031 supplies the [JSON and formatting contract](json-values-and-formatting.md).

| Operation | Application use |
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

Named branch callbacks implement each selection through the existing language contract.
The additional folds, lists, and callbacks have their normal logical charges and diagnostic frames.

### Recoverable Category Results

The application uses concrete self-handling flows for recoverable category results.
For example, `TryPerform` has the output type `Record{result: Int!, errors: List<Error>}`.
It calls `Perform` once.
Success stores the result and an empty error list.
Its pure total `CapturedPerformError` handler stores the received error in both the fallible field and a singleton error list.

The caller retains its context, task label, and accumulator.
It examines `errors` before a selected callback projects `result`.
The handler needs only its declared Error argument.
This pattern uses the existing flow-level `onError` contract.
Cancellation, host failures, and budget failures remain terminal under the implemented error contract.

The [executable control example](examples/cleanup-control-flow.lo) demonstrates selection, ordered commits, category recovery, and continuation.
It also demonstrates that a capability denial remains terminal.
The example uses only implemented operations and writes only its explicitly authorized fixture file.
The full cleanup source uses the same control and recovery rules.

### Run the Control Example

From the primary Lo checkout, run these commands.
The commands create a fresh fixture directory inside Lo.

```bash
make build
cleanup_control_work="$(mktemp -d "$PWD/_scratch/cleanup-control-example.XXXXXX")"
bin/lo check docs/examples/cleanup-control-flow.lo --format json
bin/lo build docs/examples/cleanup-control-flow.lo --output "$cleanup_control_work/control"
(
  cd "$cleanup_control_work"
  ./control run --format json
  ./control run --write-root "$cleanup_control_work" --format json -- --apply
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

## File Operations

F032 implements `device: Int`, `inode: Int`, and `allocated_bytes: Int` on `File.Entry` and `File.Directory`.
Allocated bytes equal the native block count multiplied by 512.
These metadata fields come from the same native observation as the other fields.
Their projections remain pure.
The [filesystem observation contract](file-system-observations.md) defines these fields and fresh host measurements.

F033 implements [retained descendant observations](file-descendant-observations.md) under separate recursive grants.
Root observations use `File.directory`. Descendant observations use retained directories and the selected scan contract.
Retained entry observations supply kind and absence results without new path observation operations.
Storage reports observe each selected path through its retained parent and exclude symlink entries.

F034 supplies `File.Tree.snapshot` and `File.Tree.Remove.checked` under the [tree contract](file-tree-removal.md).
I013 now constructs the checked target from the original approved snapshot.
Xcode discovery returns retained parent and root observations for each selected tree.
Shared roots retain the DerivedData parent. Each project Build root retains its observed project parent.
Discovery skips foreign owners, other devices, absent Build entries, and symlink Build entries.
It reports names that vanish during the DerivedData scan.

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
Both application snapshots must be complete and satisfy the owner, device, age, kind, and link rules.
The comparison includes allocation and excludes directory access time.
The checked target retains the original approved snapshot and repeats complete preflight before mutation.
An absent or replaced root after the probe produces a visible skip before activation.
An activation failure retains its confirmed removal progress and failed category label.
The workflow can then attempt the next independent tree.
These observations and deletion remain separate operations with a race window.

Retained descendant handles do not acquire authority from path text alone.
Observation and removal grants remain separate.
Focused native fixtures verify retained discovery targets, root replacement, partial progress, and later category execution.
Complete workflow fixtures also verify these flows through the selected descendant controller.

## Program Entry And Locks

F029 implements the entry observation and lock target used by `Locked`.
The [lock contract](program-lock-design.md) defines exact host authority, identity checks, and session ownership.

| Operation | Implemented contract |
| --- | --- |
| `Program.path()` | Observe the captured actual native executable. |
| `Lock.File.new(path)` | Construct a pure target for exclusive acquisition without a wait. Commit returns `Lock.Lease`. |

The application lock remains held through resource cleanup, including failure and cancellation.

## Generic Host Operations

| Operation | Contract |
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
Runnable acceptance uses supported execution profiles and repository-contained fixtures.

## Application Settings And Fixtures

`Main` calls `MainWithSettings` with `DefaultSettings`.
The settings record selects the shared temporary root, accepted user-temp prefix, `getconf` executable, and `du` executable.
These values are application policy, not language operations or hidden runtime switches.
The shared cleanup context retains its four fields: `apply`, `home`, `uid`, and `process`.

Full workflow fixtures preserve every declaration in the canonical source.
They replace only its final entry call with `MainWithSettings` and explicit repository-owned settings.
The native entry shim supplies controlled metadata, clock, and volume observations.
Program entry, CLI parsing, locks, process execution, selection, removal, recovery, and final status remain real.

The command fixture checks exact argument vectors and records their order.
It implements local protocol responses for the selected tools.
It does not invoke installed package cleaners, `lsof`, or a Docker daemon.
Every filesystem change stays inside the fixture directory.

## Validation Status

The complete application passes public type checking with the implemented catalog.
Native workflow fixtures passed preview, report, cleanup, npm-failure continuation, pip absence, and root-UID rejection.
Argument fixtures passed the established CLI diagnostic contract.
Focused Docker, temporary-root, inventory, lock, missing-tool, and terminal-failure checks passed after their corrections.

The complete workflow passed under the F036 controller with an actual command child and complete per-command cleanup.
The Metal-linked application CLI passed without GPU operations. Final shared stack CI passed.

| Application behavior | Acceptance source |
| --- | --- |
| Typed helpers and ordered control composition | `mac_cleanup_test.go` and `examples/cleanup-control-flow.lo`. |
| Direct files, exact probes, changes, and ordinary failure | `mac_cleanup_direct_test.go` and `mac_cleanup_policy_test.go`. |
| Retained cache traversal, allocation, foreign subtrees, and terminal progress | `mac_cleanup_cache_test.go` and `mac_cleanup_cache_policy_test.go`. |
| Whole trees, retained Xcode targets, root replacement, and partial removal | `mac_cleanup_tree_test.go` and `mac_cleanup_xcode_test.go`. |
| Complete source, arguments, modes, command order, preserved files, and fresh summaries | `mac_cleanup_workflow_test.go`. |
| Docker and temporary-root responses, inventory validation, locks, and terminal stop | `mac_cleanup_boundaries_test.go`. |
| Missing host inputs, ordered failures, Docker failure progress, and Metal-linked CLI | `mac_cleanup_completion_test.go`. |
| Complete workflow with the descendant controller and a real command child | `mac_cleanup_owned_workflow_test.go`. |
| Portable argument and UID checks under Linux QEMU | `mac_cleanup_linux_workflow_test.go`. |

The Go acceptance files are in `tests/integration`.
The command and observation fixtures are in `testdata/native-runtime`.
The fixtures compare observable category outcomes with independent expectations.
They preserve the distinction between observed allocation, confirmed deletion, and net volume change.

The cache ownership, cancellation, and allocation fixtures use controlled access times.
These fixtures keep the same deletion and progress assertions.
A separate [access-time fixture](../tests/integration/mac_cleanup_cache_policy_test.go) changes only access time and verifies that the application rejects removal.

Native [collection checks](../tests/integration/llvm_collection_scaling_test.go) passed without increases to their allocation budgets.
[File removal checks](../tests/integration/llvm_file_scaling_test.go) passed without increases to their limits on allocation growth.

The [signal fixtures](../tests/integration/llvm_signal_lock_lifetime_test.go) send OS signals.
The Darwin Process fixture executes an admitted local command.
It holds the reap receipt after the command exits.
The entry lock stays held until cleanup completes.

With the repository native toolchain prepared, run the application checks through the repository target:

```bash
make test-native TEST_ARGS='-run TestMacCleanup -count=1'
```

After preparation of the Metal-linked archive, run its application CLI check:

```bash
make build-native-runtime-metal
make test-native TEST_ARGS='-run TestLLVMMacCleanupMetalArguments -count=1'
```

This check executes help and argument handling without GPU operations.

The [earlier control record](evidence/cleanup-control-composition.json) contains historical source and structural evidence.
Current acceptance uses compiled artifacts and the single native runtime.
The fixture evidence does not establish arbitrary installed-tool behavior or entitled Endpoint Security event delivery.
