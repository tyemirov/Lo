# mac-cleanup Application Plan

Status: the full current workflow is written as a Lo application design.
The [source](examples/mac-cleanup.lo) uses implemented operations and explicit proposals for missing generic capabilities.
The [application design](mac-cleanup-script-design.md) records source identity, behavior, proposed interfaces, and validation limits.
The source is not executable with the current compiler and runtimes.

## Language Boundary

Lo is a general-purpose language.
Its operations supply contracts that apply across applications.
The application contains names, paths, command arguments, selection rules, reports, and exit decisions.
Workflow labels are application data.
They are not language features or adapter operation names.
The [language reference](../specification.md) contains the implemented generic contract.
The [agent rule](../AGENTS.md#general-purpose-language-boundary) preserves this boundary for later work.

## Current Tool Behavior

The source translation follows the installed tool observed on October 28, 2026.
It includes changes absent from the earlier application review.

| Mode | Behavior |
| --- | --- |
| Default | Execute selected cleaners and remove eligible observed files or whole trees. |
| `--dry-run` | Show commands, candidates, and stale allocation without executing cleaners, open-file probes, or removal. |
| `--report` | Show allocated storage inventory without cleanup. |
| Help | Show declared options before lock acquisition. |

The application rejects conflicting modes and invalid arguments.
It acquires a nonblocking exclusive lock on its entry file.
It rejects effective UID zero with exit status 2.
It reports initial free space and sets the six declared child-environment overrides.
Ordinary task failures retain their labels and permit later independent tasks.
The final status is 1 after an ordinary task failure, or 0 after success and skips.

### Ordered Workflow

1. Probe optional pip availability during cleanup, before the package commands.
2. Execute or preview npm, Go, uv, pnpm, and available pip commands in order.
3. Examine direct npm log files with the 30-day policy.
4. Examine the five declared cache roots recursively with the 7-day policy.
5. Select and examine whole Xcode build or shared-cache trees with the 7-day policy.
6. Report allocation from completed scans.
7. Discover and validate both temporary roots before their 7-day direct-file cleanup.
8. Validate the selected Docker context and builder before its ordered commands.
9. Examine direct deployment logs with their separate modification-age policy.
10. Report final free space, observed volume change, and the aggregate result.

Paths and labels appear only in the application source and application documents.
Homebrew downloads now use the current recursive cache policy.
The earlier suffix-restricted Homebrew proposal is superseded by the observed tool behavior.
Xcode removal preserves unrelated project content and `SourcePackages`.
Storage inventory remains distinct from a reclaim estimate.

### File Workflows

Direct stale-file selection uses globs, owner identity, link count, path characters, and access, modification, and change ages.
Recursive cache selection additionally checks directory ownership and filesystem device identity.
It removes regular files and preserves directory structure.
Its report retains confirmed deletion counters after ordinary partial failure.

Whole-tree eviction includes the root and all visited relative entries.
The application checks supported kinds, owner, device, links, ages, and exact before/after observations.
A rejected subtree remains visible in the snapshot and prevents eviction.
Directory access time is excluded from this application's tree-age comparison.
Any observed open handle prevents whole-tree eviction.

Direct deployment logs keep their distinct filename, age, strict-discovery, and probe rules.
The exact digit predicate stays ordinary application code.
Other file policies use globs and pure metadata predicates.
The [application design](mac-cleanup-script-design.md#behavior-in-the-application) records their framing and stderr differences.

## Implemented Foundation

F024 supplies inherited Process contexts, executable discovery, bounded transport, and scoped cleanup execution fixtures.
The [F024 evidence](evidence/process-context-cleanup/checkpoint.json) records its validation boundary.
F025 supplies host snapshots, immutable file metadata, directory observations, scans, and guarded removal.
Its [contract](file-observation-extension-design.md) records completed implementation acceptance.
The existing [direct log example](examples/gateway-log-cleanup.lo) remains a focused executable fixture.
The [package example](examples/package-cache-cleanup.lo) remains a focused Process fixture.
Neither focused example establishes full script parity.

## Work Derived From the Script

The full source now supplies the application intent before further language implementation.
The application design identifies each proposed operation outside the implemented reference.
Further work follows this sequence:

1. Review the proposed generic operations against other application uses.
2. Reuse current Match, self-handling flows, and sequential folds for branch effects and category recovery.
3. Select small contracts for value helpers and descendant resource lifetime.
4. Specify retained descendant observations, metadata additions, and descriptor-safe tree removal with explicit grants and resource limits.
5. Specify program identity, locks, console output, process output and timeouts, and terminal results.
6. Add public fixture failures before implementation of each selected contract.
7. Implement each selected contract in the interpreter and native runtime.
8. Validate the full application with repository-contained command and filesystem fixtures.

Do not add a language operation for a named application or workflow step.
Keep the implemented Match effect rules and flow-cycle rules unchanged unless a separate language contract explicitly changes them.
The source now uses current `Match.new`, `onError`, and `Flow.fold` contracts for its conditional effects and recovery.
All 47 selections and nine captures now use named application flows.
The [control example](examples/cleanup-control-flow.lo) passes interpreter and native software checks.
`Flow.worklist` now supplies dynamic traversal with host resource quotas and resource release when scopes close.
The [resource contract](../specification.md#89-dynamic-worklists-and-host-resource-quotas) separates cumulative work from retained values and observations.
Proposed descendant operations still need explicit authority contracts.
The other proposed APIs still prevent execution of the full source.
The [control evidence](evidence/cleanup-control-composition.json) records the validation scope.

## Acceptance Boundary

All application acceptance uses public Lo entry points and state inside this repository.
Fixtures verify argument lists, process environment, closed input, output modes, timeouts, task order, and exit status.
File fixtures verify preview, observations, open-file skips, changed entries, partial progress, and untouched siblings.
Whole-tree fixtures verify retained identities and full eligibility before eviction.
Lock fixtures verify contention and release on success, failure, and cancellation.
Equivalent fixtures must cover the interpreter and both native software targets.

The [repository policy](../.mprlab/POLICY.md#lo-repository-boundary) controls every operation.
No acceptance step runs actual package-cache or Docker cleanup.
No step changes an installed script or stores task output outside Lo.
Syntax acceptance and document review do not establish executable parity.
