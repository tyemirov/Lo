# mac-cleanup in Lo

Status: the File adapter and Gateway log category are implemented.
The remaining categories still require language and adapter work.
The complete script is not implemented.
This document records behavior from the earlier script review.
Further work uses this record and fixtures inside Lo.
The [shared adapter design](adapter-design.md) owns future File, HTTP, and Process semantics.
This application plan supplies acceptance examples under that contract.

## Contents

- [Repository boundary](#repository-boundary)
- [Review findings](#review-findings)
- [Workflow contract](#workflow-contract)
- [Required language capabilities](#required-language-capabilities)
- [Implementation sequence](#implementation-sequence)
- [Acceptance](#acceptance)
- [Validation boundary](#validation-boundary)
- [Open decisions](#open-decisions)

## Repository Boundary

The [Lo repository boundary](../.mprlab/POLICY.md#lo-repository-boundary) applies to every step, without exceptions.
Keep source changes, documents, examples, fixtures, temporary files, tool caches, and generated output inside Lo.
Do not use another repository or access the installed cleanup script.
Do not install a replacement script or execute cleanup against real user data.

Paths below describe the intended program's inputs.
Tests substitute directories and host observations inside Lo.
External command behavior comes from controlled process fixtures inside Lo.
No acceptance step runs actual package-cache or Docker cleanup.

## Review Findings

| Finding | Required correction |
| --- | --- |
| The File fixture was treated as a complete workflow. | The maintained log example now covers probes, skips, and failure progress. Full-script acceptance remains separate. |
| The design introduced application coordination absent from the script. | Remove that dependency. Preserve only the cleanup program's own concurrency lock. |
| Batch removal fails on changed or absent candidates. | The log example uses singleton commits through `Flow.fold` and handles only `File.EntryChanged` as a skip. |
| The adapter rejects files with multiple links. | Generic removal now permits hardlinks. The log category has no single-link restriction. |
| File entries omit metadata required by other categories. | Add owner identity, access time, and change time to the proposed entry contract. |
| A resultless commit cannot supply command output. | Define explicit process activation with a typed completion result. |
| Temporary directories and caches can escape Lo. | Make and native build subprocesses now use explicit state directories inside Lo. |

The [File design](file-adapter-design.md) describes the current batch contract and its limits.
This plan requires further contracts for the other categories and complete-script acceptance.
It does not treat earlier focused test results as proof of complete script behavior.

## Workflow Contract

### Startup and Completion

- Reject command arguments with exit status 2.
- Reject effective user ID zero with exit status 2.
- Acquire a nonblocking exclusive lock for this cleanup program.
- Keep the lock until the program ends.
- Discover `python3` and probe optional pip availability before execution of npm, Go, uv, or pnpm cleanup.
- Execute categories in the order below.
- After a recoverable category failure, continue with the next category.
- Return exit status 1 if any category fails, or 0 if all categories succeed or skip.

The original lock is on the cleanup script itself.
It prevents concurrent cleanup invocations.
It does not prevent an application from opening or changing a candidate file.

Child processes receive explicit argument lists and closed standard input.
The captured environment adds these values:

```text
CI=1
HOMEBREW_NO_AUTO_UPDATE=1
HOMEBREW_NO_ANALYTICS=1
GOTOOLCHAIN=local
MISE_AUTO_INSTALL=0
UV_PYTHON_DOWNLOADS=never
```

### Ordered Categories

| Order | Category | Behavior |
| --- | --- | --- |
| 1 | npm | Discover the executable, then run `npm cache clean --force`. Skip when absent. |
| 2 | Go | Discover the executable, then run `go clean -cache -testcache`. Skip when absent. |
| 3 | uv | Discover the executable, then run `uv cache clean --force`. Skip when absent. |
| 4 | pnpm | Discover the executable, then run `pnpm store prune --yes`. Skip when absent. |
| 5 | pip | Discover `python3`. Include `python3 -m pip cache purge` only after `python3 -m pip --version` succeeds. |
| 6 | npm logs | Select `.log` files from `~/.npm/_logs` with the common 30-day policy below. |
| 7 | Homebrew downloads | Use the common 30-day policy for `.tar.gz`, `.tar.xz`, `.tar.zst`, and `.zip` files. |
| 8 | Temporary files | Use the common 7-day policy for `.tmp` and `.temp` files in two validated roots. |
| 9 | Docker | Validate the explicit context and builder, then run the ordered commands below. |
| 10 | Gateway logs | Use the separate log policy below. |

Homebrew downloads use `~/Library/Caches/Homebrew/downloads`.
The temporary roots are `/private/tmp` and the validated result from `/usr/bin/getconf DARWIN_USER_TEMP_DIR`.
Do not select the native temporary root from `TMPDIR`.
Resolve the native root and require a directory under `/private/var/folders/` with final component `T`.
Require the real user ID as its owner.
Both temporary roots form one category. A failure in the first root ends that category.

### Common File Policy

- Skip an absent directory and reject a symlinked root.
- Examine direct children without recursion or symlink traversal.
- Accept only regular files owned by the real user ID, with exactly one link.
- Exclude hidden names and names without an allowed suffix.
- Require full paths to match `[A-Za-z0-9/._ -]+`.
- Require access, modification, and change times to be strictly older than the cutoff.
- Skip a child that disappears during discovery.
- If no candidates remain, complete this category without an `lsof` probe.
- Probe exact candidate paths in batches of 100 with `lsof -F0n -- <paths...>`.
- Parse the NUL-framed output as bytes.
- Accept probe exit status 0 or 1. Reject any standard-error bytes.
- Skip open paths and candidates that disappear, change, or become ineligible before removal.
- Fail the category on another filesystem or probe error.
- Report candidate counts, observed bytes, removals, skips, and failures.

Directory-relative operations preserve the selected root boundary.
An open-file observation cannot prevent a later open.
Revalidation cannot make the final identity check and removal atomic.

### Gateway Log Policy

Use the fixed directory `~/Library/Caches/mprlab-gateway/deployments`.
This is a cache category with no source-project dependency.

- Skip an absent directory. Reject a symlinked root or a root that is not a directory.
- Examine sorted direct children and select regular files with basenames matching `deployment-[0-9]+\.log`.
- Use ASCII digits and a full-basename match.
- Require modification time to be strictly older than 30 days.
- Do not add the common policy's owner, single-link, or access-time filters.
- Treat a metadata failure during discovery as a category failure.
- If no candidates remain, complete this category without an `lsof` probe.
- Run `lsof -Fn +D <directory>` and parse its line-framed output.
- Accept probe exit status 0 or 1. Reject standard error when its stripped value is nonempty.
- Skip observed open paths.
- Recheck identity, size, modification time, and file kind before removal.
- Skip changed or absent candidates. Fail the category on another removal error.

The implemented log probe uses `lsof -Fn +D .` after a child enters the retained directory through its descriptor.
It maps validated relative names to the supplied root and preserves the category's line framing and standard-error rule.
Malformed records and ambiguous escaped or control-character filenames now fail explicitly.
The different `lsof` formats and standard-error rules are deliberate acceptance cases.
Do not silently replace either format with the other.

### Docker Policy

- Skip the category when the Docker executable is absent.
- Reject nonempty `DOCKER_HOST`, `DOCKER_CONTEXT`, or `BUILDX_BUILDER`.
- Use `docker --context desktop-linux` for every command.
- Parse `context inspect desktop-linux` output and require the selected Docker endpoint to start with `unix://`.
- Parse `buildx inspect desktop-linux` output.
- Require exactly one driver named `docker` and one endpoint named `desktop-linux`.
- Run `system df`, then `buildx prune --builder desktop-linux --all --force`, then `image prune --all --force`.
- Stop this category on command or validation failure.

## Required Language Capabilities

| Capability | Proposed contract |
| --- | --- |
| Host inputs | Capture home, real/effective user IDs, clock, arguments, and selected environment values as typed inputs. |
| Executable discovery | Distinguish an absent executable from a discovery failure. |
| Process construction | Describe an executable, argument list, environment, closed input, and bounded output without starting a process. |
| Process activation | Return typed exit status, stdout bytes, and stderr bytes after explicit activation. |
| Process failures | Keep nonzero child status inspectable. Report launch failure, cancellation, and output overflow distinctly. |
| File observations | Expose required metadata and distinguish absence from failure at the directory and candidate boundaries. |
| File removal | Keep generic removal separate from retention, owner, and link-count policy. Preserve confirmed progress on failure. |
| Sequential activation | Support bounded candidate execution with explicit changed/absent skips. Preserve pure List callback rules. |
| Parsing | Decode NUL and line framing, required JSON fields, and builder text with explicit malformed-input errors. |
| Cleanup lock | Acquire one cleanup-instance lock and release it on success, failure, and cancellation. |
| Category handling | Preserve category order, error reporting, later-category continuation, and the final exit status. |

Process construction must not execute commands.
The [bound commit contract](adapter-design.md#activation-and-results) supplies typed activation results.
General Process execution first needs a [qualified execution profile](adapter-design.md#execution-profile-prerequisite).
Output overflow must fail explicitly. It cannot appear as an empty successful result.
Do not put cleanup-specific names, retention periods, or Docker policy in an adapter.

## Implementation Sequence

The [File implementation record](file-adapter-plan.md) covers the completed log category and local build state.
Further work follows this sequence:

1. Implement the applicable stages of the [shared adapter plan](adapter-plan.md) before new cleanup-specific capabilities.
2. Migrate the log fixture to nominal actions and typed receipts.
3. Specify host inputs, discovery, locks, and extra metadata through the shared operation contract.
4. Add common-policy fixtures for owner identity, timestamps, suffixes, temporary roots, and NUL-framed probes.
5. Qualify the Process profile before general command execution.
6. Add process fixtures for package commands, optional executables, environment values, and category continuation.
7. Add Docker response fixtures and reject invalid contexts, builders, endpoints, and ambient overrides.
8. Add the ordered workflow with startup rejection, cleanup locking, reporting, and final exit status.
9. Update the language reference and run confined repository validation and independent review.

Keep one canonical contract for each capability.
Do not add compatibility aliases or alternative legacy execution paths.

## Acceptance

Use public Lo entry points with fixture state and process helpers inside this repository.
Verify both returned reports and resulting fixture files.
Assert the exact command order, arguments, environment, input closure, and allowed exit statuses.
Verify category failure continuation and per-candidate skips separately.
Verify empty output, malformed output, output limits, cancellation, lock contention, and partial removal.
Keep sibling fixtures and ungranted paths unchanged.
Run equivalent fixtures through the interpreter and native artifacts after their output paths satisfy the repository boundary.

## Validation Boundary

The [Makefile](../Makefile) configures temporary files, Go state, compiler caches, and supported Python tool caches inside `_scratch/build-state/`.
It uses the installed Go toolchain without automatic toolchain selection.
[Native builds](../internal/nativebuild/paths.go) require an existing absolute `LO_BUILD_ROOT` and reject symlink traversal.
They do not select a user cache or a default temporary directory.
Tests verify child-process paths and invalid build-root rejection.

If a later tool cannot satisfy this boundary, report that operation as blocked.
Do not use another repository or an external cache to remove the limitation.

## Open Decisions

- Specify application handling of bound commit results under the shared adapter contract.
- Extend `Flow.fold` orchestration to the remaining category policies while preserving pure List callbacks.
- Add common-policy metadata and discovery skips without suppressing unrelated filesystem errors.
- Select cleanup-lock identity for interpreted and native programs.
- Resolve each listed difference before claiming complete workflow acceptance.
