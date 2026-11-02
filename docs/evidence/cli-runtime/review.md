# F026 Through F028 Independent Review

Status: passed for source review and focused tests.
No blocking finding remains in the reviewed source.
Final stack CI is a separate completion requirement.

## Reviewed Scope

The review covered shared types, values, catalog entries, runtime operations, and the executable CLI example.
It covered Go Console sinks, writer ownership, runner completion, and output failure precedence.
It covered native Console values, callbacks, terminal completion, file protection, parser behavior, and child startup.
The review compared source contracts with the selected issue requirements and available test logs.

## Corrected Findings

The original Console pipe creation used `pipe` before it set `FD_CLOEXEC`.
A File probe could start between those actions and inherit the liveness endpoint.
The public regression confirmed this condition with status 43.
The common ownership lock now covers Console pipe creation, File probe startup, and native Process startup.
Lock acquisition observes cancellation.

The initial ownership lock did not observe the existing native Process deadline during a wait.
The public regression returned status 57 because a queued operation exceeded its 50-millisecond deadline.
The correction observes that deadline during lock acquisition and before child startup.
The operation reports cancellation without a started child and completes resource cleanup.

Output failure coverage omitted some requested application statuses.
The corrected Go and native embedding tests cover output failure after requested statuses 0, 1, and 2.
Existing cleanup failure tests cover the same statuses.

A stop signal could arrive after readiness bytes but before the writer acknowledgment.
The original execution context then canceled readiness completion.
Ready and stopped events now each use a separate one-second output context.
Diagnostics keep their report deadline.
Focused tests cover this signal boundary and a stop after 1500 milliseconds.

The native terminal value copied payload bytes without their original value identities.
The interpreter kept these identities after the constructor flow returned.
At a live-value quota of 3410 bytes, the interpreter failed while the native runtime succeeded.
Native terminal values now keep both original Bytes children.
The resource graph counts a shared child once and keeps expanded value weights unchanged.
Snapshots keep fresh empty Bytes children and payload-length metadata.

Focused boundary tests cover distinct payloads, shared payloads, and a record that also keeps one payload.
They compare both public runtimes at the exact live-value and expanded-value limits.

The harness previously searched for `main` before it processed C includes.
This caused an include-only fixture to fail before execution.
The harness now preprocesses the complete C source with the selected target, macro value, and include paths.
It then identifies and renames the selected entry definition.
The common entry forwards writer process invocations before it runs the fixture.
The correction changes test preparation without a runtime or bundle change.

The hash-cancellation fixture denied every `execvp` call.
This also denied the Console writer needed to report cancellation.
The fixture now forwards only the exact private writer invocation through `execve`.
The intended probe startup remains denied and its marker assertion remains active.
Darwin uses `_NSGetEnviron`, and Linux uses `environ` for writer startup.
The fixture uses the canonical three-argument CLI entry.

Related fixture review found no other `execvp` or `execve` replacement.
The Console writer uses a separate startup path from the existing `posix_spawn` replacements.

Two cleanup fixtures failed every descriptor close.
Their hooks also prevented Console pipe closure and caused the public fixtures to stop without completion.
The isolated file and HTTP checks reproduced this condition with an active writer process.
The corrected file fixture selects the first retained descriptor for its dedicated read root.
The corrected HTTP fixture selects the client descriptor from `connect`.
Each hook closes the descriptor before it reports one injected error.

Both fixtures preserve their primary error and cleanup assertions.
Each fixture also requires exactly one injected close failure.
Darwin and Linux checks cover all corrected file and HTTP cases.
The existing observation and Process hooks passed without changes.
No related interposition finding remains in the reviewed fixtures.

A completed native CLI invocation could leave its process cancellation flag set.
A later invocation then failed before it could publish its result.
The public HTTP fixture reproduced this condition after a service stop signal and thread join.
Normal CLI entry now clears the flag before Console setup.
The private writer entry still returns before this reset.
No later reset can erase a signal received during the current invocation.

The correction changes only the reset location in `cli.rs`.
The signal handler and execution cancellation checks remain unchanged.
Focused checks cover sequential invocations, readiness cancellation, Console output, service receipts, and Process cancellation.

The newly selected Linux lifecycle fixture initially failed before its service connection because guest loopback remained down.
Its Linux branch now enables loopback with the established guest socket and ioctl sequence.
The test now keeps a separate Linux wrapper and shares its ten cases and assertions across both target wrappers.
Both maintained groups passed after this fixture correction.
The runtime source and bundle remain unchanged.

Three remaining qualification fixtures also used broad close failure hooks.
Their corrected hooks select only the intended HTTP client or File directory descriptors.
Each selected hook performs the real close before it reports one injected error.
Private writer invocations can resolve real delegates before fixture initialization.
The server fixture uses a child working directory to separate its write grant from Console image ancestors.

The File cases keep local finalization, retained scope cleanup, and session cleanup distinct.
The scope case preserves the primary failure through its Lo error handler.
It requires explicit observed-directory cleanup evidence.
The finalization case now checks its structured primary error and confirmed progress instead of an incidental word.
All original status, removal count, cancellation, progress, and server cleanup assertions remain active.
The focused suite passed nineteen cases on each target.

The Linux TLS fixture previously started an embedded program through a descriptor path.
The shared guest runner now packages an explicit local ELF companion at `/companion`.
The driver receives that stable path as an argument and starts the child without an inherited image descriptor.
Companion requests always use the controlled software guest.
Ordinary requests without a companion keep their existing direct Linux path.
The driver uses the existing guest `/proc` setup and keeps its TLS and network effects inside the guest.

The public regression verifies exact stdout, stderr, and child status 7.
The original three TLS scenarios and single-request oracle remain unchanged.
The focused companion, TLS, and Linux core checks passed.
All twenty native runtime tool tests also passed.
This correction changes fixture packaging and test support without a runtime source or bundle change.

## Focused Evidence

| Evidence | Result |
| --- | --- |
| `final-review-output-status.log` | Go final flush cases and native terminal flush failures passed for statuses 0, 1, and 2. |
| `console-spawn-inheritance-red.log` | The original child inherited the Console liveness endpoint and returned status 43. |
| `console-spawn-deadline-red.log` | The initial lock exceeded the queued Process deadline and returned status 57. |
| `native-spawn-focused.log` | Console, File probe, Process execution, and transport ownership tests passed in 67.325 seconds. |
| `native-spawn-deadline-focused.log` | Both startup regressions and Process execution and ownership tests passed in 9.309 seconds. |
| `native-ready-cancellation-red.log` | Readiness output followed by a signal before acknowledgment produced status 2 and `runtime.cancelled`. |
| `readiness-go-final.log` | Go readiness, blocked output, and the server receipt test passed in 2.372 seconds. |
| `native-ready-focused.log` | Native readiness, later stop, server receipt, service cleanup, and Console tests passed in 38.288 seconds. |
| `program-exit-parity/result.json` | The original live-value thresholds were 4242 bytes for the interpreter and 3410 bytes for native execution. |
| `program-exit-parity/aliases-thresholds.json` | Independent interpreter results define distinct, shared, and record-alias boundary expectations. |
| `program-exit-parity/focused.log` | Resource parity, terminal output, redaction, quotas, embedding cleanup, readiness, and receipt tests passed in 46.041 seconds. |
| `harness-preprocessing.log` | Included entry definitions, runtime lifetimes, and Console tests passed in 22.656 seconds. |
| `probe-interposition-focused.log` | Darwin and Linux probe hash-cancellation fixtures passed in 2.649 seconds. |
| `related-syscall-fixtures.log`, `related-http-hook.log`, `related-syscall-hang.txt` | The original file and HTTP fixtures stopped without completion and kept their writer processes active. |
| `related-close-focused-final.log` | Darwin and Linux cleanup cases, observation cleanup, and Process hash cancellation passed in 12.511 seconds. |
| `cli-cancellation-red.log` | A service stop followed by another CLI invocation produced stale `runtime.cancelled` output and fixture status 23. |
| `cli-cancellation-focused.log` | Sequential CLI invocations, readiness cancellation, Console boundaries, service receipts, and Process cancellation passed in 24.194 seconds. |
| `cli-cancellation-linux.log` | All ten newly selected Linux cases failed at fixture status 14 before the service connection. |
| `cli-cancellation-both-targets.log` | Maintained Darwin and Linux wrappers passed all twenty HTTP error and sequential invocation cases in 21.361 seconds. |
| `remaining-close-case7-report.json` | The primary finalization error contains confirmed removal progress and no secondary cleanup error. |
| `remaining-close-darwin-focused.log` | All eight affected groups and nineteen cases passed on Darwin software in 7.775 seconds. |
| `remaining-close-linux-focused.log` | The same eight groups and nineteen cases passed on Linux QEMU in 21.951 seconds. |
| `remaining-close-final-review.json` | The final four-file review records exact byte digests and no blocking findings. |
| `linux-tls-companion-red.log` | The old runner rejected the public companion request with status 2. |
| `linux-tls-companion-green.log` | Companion output and status, three TLS scenarios, and four Linux core cases passed in 17.828 seconds. |
| `linux-tls-companion-tools.log` | All twenty native runtime tool tests passed in 5.67 seconds. |
| `linux-tls-companion-final-review.json` | The maintained four-file correction matches the approved draft and has no blocking finding. |

The focused commands used repository Make targets and task-owned preparation and runtime files.
The runtime source digest matches the final build receipt:
`b3eb0376362bafe13e6c5e7f2609100544f6f355bcb699b36f0a593051b4670d`.
The cancellation correction passed the Darwin software and Linux QEMU checks.
The new concurrency fixtures used the Darwin software runtime.
Their controlled child processes and output channels stayed inside Lo.
This review does not claim a completed stack CI run.

## Documentation Review

The official ASD-STE100 Issue 9 reference was verified through `prepare-ste-reference`.
The review applied the relevant Part 1 rules and Part 2 meanings to this report.
The mechanical language check passed for this report.
