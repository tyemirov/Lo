# Descendant Process Ownership

Status: P007 design and F036 implementation passed independent review.
Focused software acceptance passed.
Final shared stack CI passed.
The default profile denies descendant creation.
The [application roadmap](mac-cleanup-implementation-plan.md) gives the application dependencies.

## Selected Profile

The host selects descendant authority explicitly with `--process-descendants`.
This F036 host interface supplies explicit authority for descendant creation.
Without this selector, the existing qualified profile continues to deny fork and spawn.
That profile has less authority and does not need Endpoint Security admission.

Selection also supplies finite host limits for process-ledger entries, event work, execution tokens, and the kernel process count.
Application arguments and environment values cannot select the profile or increase these limits.
After descendant admission fails, the runtime cannot switch to the profile that denies descendants.
Neither profile supplies an unrestricted execution path.

The selected Darwin profile uses one trusted supervisor for each command activation.
The supervisor initializes an Endpoint Security client before it starts the initial command process.
The client observes the supervisor and its complete descendant process tree.
Process groups, sessions, and current parent IDs do not control membership.
Fork, spawn, registered exec chains, and detached sessions retain the initial command's scope.

The native provider needs macOS 27 APIs and the Endpoint Security entitlement.
Missing APIs, denied client creation, or setup that is not complete cause explicit unavailable admission before command creation.
There is no process-group, process-scan, or unrestricted execution alternative.
Linux activation is unavailable in this profile.
Apple documents the descendant client and its entitlement in [the client contract](https://developer.apple.com/documentation/endpointsecurity/es_new_descendants_client(_:_:)).

The [entitlement contract](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.developer.endpoint-security.client) gives the Apple authorization requirement.
An isolated trusted provider can supply the same event contract for software acceptance.
Injected provider tests do not verify entitled native-provider execution.
Missing native authorization is a platform availability result.
Implementation acceptance uses its own evidence.

## Finite Host Limits

The selected profile has five host flags.
These flags do not change the language value schema or `ContextOptions`.

| Flag | Default | Bound |
| --- | --- | --- |
| `--process-descendants` | Absent | Explicit selection of descendant authority. |
| `--max-process-descendants` | `256` | Live command processes, including the initial command and excluding the supervisor. |
| `--max-process-tokens` | `1024` | Retained execution tokens, including unresolved exec aliases. |
| `--max-process-events` | `1000000` | Activation event work, including admission and completion events. |
| `--process-kernel-limit` | `1024` | Exact hard `RLIMIT_NPROC` value for the initial command. |

Each count must be a positive decimal integer.
The event limit fits an unsigned 64-bit integer. Other counts fit unsigned 32-bit integers.
The CLI rejects repeated flags, zero, negative values, overflow, and `unlimited`.
A descendant limit flag requires `--process-descendants`.

The runtime never silently reduces a requested limit.
A requested kernel limit above the inherited hard limit causes unavailable admission.
Configuration alone does not open a provider or change a kernel limit.
Process activation performs provider, kernel-limit, and storage admission before command creation.

The process and token tables have finite storage.
The controller does not retain an event history proportional to the event-work limit.
Resource admission accounts for the tables, transfer frames, retained records, and copied executable registry before activation.

Event-work exhaustion permanently fails the activation.
It cannot produce a success or release proof.
Fixed-storage cleanup continues to process fork, exec, exit, and denied authorization events during quarantine.
This cleanup can continue after the caller deadline.
It does not reset the event counter or restore admission.
Later marker or synchronization events cannot repair the failed proof.

Table exhaustion prevents retention of additional process identities.
The supervisor continues to signal known execution tokens and retains all ownership leases.
It cannot guarantee termination of a child whose identity did not fit in the tables.
It never publishes release proof after this failure.

The native provider borrows one authorization message inside its serial callback.
It does not retain opaque SDK argument or environment payloads after that callback.
The owner answers through a separate thread and one protected reply transition.
If the owner misses the bounded message deadline, the callback denies execution and records permanent provider failure.
A late response fails without a second reply.

## Supervisor Identity And Build Inputs

The supervisor executes the same compiled image as its caller.
The image's CodeDirectory identifier and exact execution token identify that supervisor.
The parent also checks the unreaped child PID from the spawn result.
Both peers exchange identity messages before resource transfer.
They capture and check the socket peer token after this exchange.

The compiled host registers its provider factory before internal CLI dispatch.
The supervisor repeats this registration after re-exec.
Arguments and environment values cannot supply a factory or function pointer.
Internal supervisor dispatch occurs before Console initialization.
The supervisor starts in a separate session so terminal cancellation cannot destroy the resource owner before cleanup starts.
This session does not establish descendant membership.

Both Process profiles use one launcher receipt with an exact execution token.
The default profile checks that token against its unreaped initial child.
The descendant profile also waits for matching lifecycle evidence.
Either the receipt or the lifecycle event can arrive first.
Only the initial gated child can use the trusted launcher image authority.
Later exec requests retain the initial command scope and registered executable identities.

Native preparation uses installed SDK declarations for the Endpoint Security, bootstrap, and execution-token boundaries.
The objects retain the existing macOS `27.0` deployment target.
Optional Endpoint Security and token-signal functions use explicit runtime symbol admission.
The build records compiler, SDK, transitive header, source, object, and archive identities.

Package validation checks exact compiler flags and repository source and header hashes.
External SDK paths remain producer evidence.
Package consumers do not reopen those paths or need an installed SDK.
The distributed archive contains the compiled boundary objects.

## Admission And Scope

1. Start a dedicated supervisor with no existing command descendants.
2. Keep its control channel isolated from command arguments, environment values, and standard streams.
3. Retain one read-only `/dev/null` descriptor and its device and inode identity as an explicit supervisor resource.
4. Initialize the descendant client and select `ES_DEADLINE_MISS_MODE_FAIL_CLOSED`.
5. Remove default event muting before command creation.
6. Subscribe to `NOTIFY_FORK`, `AUTH_EXEC`, `NOTIFY_EXEC`, `NOTIFY_EXIT`, and `NOTIFY_OPEN` in one operation.
7. Open `/dev/null` read-only with `O_NOFOLLOW` as the initial sequence marker.
8. Call `es_sync_client` from a different thread than the event callback.
9. Accept admission only after the matching marker and successful synchronization callback.
10. Apply the initial executable's filesystem, endpoint, executable, and resource restrictions before command activation.
11. Start command execution only after its initial lifecycle events identify the expected launch.

Only the trusted supervisor generates sequence markers.
Only one marker can await completion at a time.
The open event must identify the supervisor and the retained device and inode identity.
The supervisor also verifies that the open succeeded and returned that identity.
No other supervisor operation opens this reserved marker resource during activation.
The supervisor closes each temporary marker descriptor after synchronization.

The supervisor does not start unrelated children during this activation.
The initial marker supplies the sequence baseline before untrusted execution.
A missing marker causes admission failure.

The descendant-client contract includes notify events for the supervisor itself.
The [open-event contract](https://developer.apple.com/documentation/endpointsecurity/es_event_open_t) supplies the file identity and open flags.
All open notifications count against the event-work limit.
This includes command file activity.

This profile does not subscribe to signal events.
The [signal-event contract](https://developer.apple.com/documentation/endpointsecurity/es_event_signal_t) excludes self-signals and can identify a different instigator for delegated signals.
An external signal sender does not become a process owner.
Cancellation enters through the host control protocol.
Exit notifications remove terminated owners.

The trusted initial child waits until the supervisor verifies its identity and resource transfer.
The supervisor can receive the initial fork event before the creation call returns.
The supervisor keeps that event until the launch result verifies the direct child's identity.
A creation call that returns an error needs synchronization before the supervisor resolves the initial launch without a child.
The ready signal requires a confirmed initial launcher exec event.
An initial fork event alone cannot supply that proof.

The inherited sandbox keeps access in the initial executable's scope.
An exec chain cannot add a different executable's filesystem or endpoint grants.
`AUTH_EXEC` permits only registered executable identities in that same scope.
Authorization replies disable caching.

One serial ownership gate orders shutdown and authorization replies.
After shutdown starts, the gate denies all unanswered exec requests.
An earlier allow reply can result in a subsequent exec event.
The supervisor retains that process and terminates its new execution token.
An unsuccessful authorization reply makes ownership unknown.
The [deadline API](https://developer.apple.com/documentation/endpointsecurity/es_set_deadline_miss_mode(_:_:)) also denies authorization requests after deadline or queue failure.

## Event Ownership

One serial callback updates the process ledger.
Every accepted event has the next global sequence number.
A sequence gap, duplicate sequence, unknown owner, or invalid transition makes ownership unknown.
The supervisor then enters permanent quarantine for this activation.
The [message contract](https://developer.apple.com/documentation/endpointsecurity/es_message_t) specifies sequence numbers and missing-event detection.

| Event | Ledger change |
| --- | --- |
| Initial `NOTIFY_FORK` | Add the expected initial command as an owned process. |
| Descendant `NOTIFY_FORK` | Add the child with its known parent's initial scope. |
| `AUTH_EXEC` | Keep the initial execution token and record the proposed new token for the same process. |
| `NOTIFY_EXEC` | Replace the initial token with the confirmed new execution token. |
| `NOTIFY_EXIT` | Remove the process only after a matching known execution token and complete sequence validation. |
| Reserved supervisor `NOTIFY_OPEN` | Confirm the pending sequence marker without adding a process owner. |
| Other `NOTIFY_OPEN` | Account for the event sequence without changing process ownership. |

An execution token includes the PID and its execution version.
Exec changes that version when the PID stays the same.
The [process contract](https://developer.apple.com/documentation/endpointsecurity/es_process_t) specifies this identity.

A denied exec request can cause no successful exec notification.
The ledger keeps proposed token aliases until a verified transition or exit resolves them.
Token storage has a different host limit.
An unconfirmed proposed token cannot identify an exit or open event.

Termination uses `proc_signal_with_audittoken` with a known execution token.
A stale token can return an error while the process completes an exec transition.
`ESRCH` does not remove the process from the ledger.
The supervisor waits for the related lifecycle event and uses the resulting token.
It does not replace token checks with a PID-only signal.
The [Apple implementation](https://github.com/apple-oss-distributions/xnu/blob/main/libsyscall/wrappers/libproc/libproc.c) supplies the token-based signal operation.

## Completion Protocol

The supervisor stops new command admission when cleanup starts.
It sends termination signals to known owners and continues to process lifecycle events.
A new child event adds an owner after the initial command exits.
The supervisor terminates that child before the same cleanup deadline.
An inherited output pipe stays owned until EOF and successful closure.

Successful completion needs all these conditions:

- The initial launch resolved.
- The complete event sequence has no gap.
- The process ledger has no remaining owner.
- The supervisor reaped its direct child.
- All command input and output channels completed their required cleanup.
- All output sinks completed their required writes and joins.
- A last sequence marker and synchronization callback completed on the same active client.
- The client, control channel, and remaining owned resources closed successfully.

`waitpid`, pipe EOF, an empty process scan, and `ESRCH` do not independently verify complete descendant cleanup.
Apple documents synchronous event enqueue and ordered queue drainage in [the synchronization API](https://developer.apple.com/documentation/endpointsecurity/es_sync_client(_:_:)).
The callback can also execute during client destruction.
The supervisor checks client generation and active status before it accepts the callback.
A null client or a callback from client destruction cannot verify completion.

A launcher failure before readiness needs retained initial identity, creation and reap evidence, and an empty ledger.
A matching sequence marker and synchronization callback must also complete before ordinary failure recovery.
Absent identity or incomplete cleanup keeps the activation in quarantine.

The completion argument depends on complete lifecycle ordering.
A child creation precedes the creator's exit in that sequence.
An observed exit cannot remove the last creator while an earlier child creation stays unaccounted for.
A missing creation event causes a sequence gap before a subsequent accepted event.
A missing last exit leaves its owner in the ledger.
The marker and synchronization callback close the observation interval before success.

This argument uses Apple's [event ordering contract](https://developer.apple.com/videos/play/wwdc2020/10159/) and synchronization contract.
The bounded model checks event loss and delayed delivery against this argument.
It does not replace those operating-system contracts.

## Bounds And Failure

The host selects limits for live ledger entries, token transitions, event work, output, command duration, and cleanup duration.
Ledger limits do not prevent an unobserved fork before notification delivery.
The native provider must also apply a kernel process-count bound before descendant creation.
An inherited hard `RLIMIT_NPROC` is the selected bound for an unprivileged, unchanged real UID.

This limit counts all processes for that UID.
Unrelated processes can reduce the available capacity.
It is not a count of processes for one command.

Admission checks that the real, effective, and saved user IDs have the same nonzero value.
The supervisor installs the hard limit before it applies the command sandbox.
The sandbox denies credential changes and resource-limit changes.
The syscall filter covers user IDs, group IDs, thread credentials, personas, and resource-limit controls.
The descendants client also rejects executable credential elevation for this unprivileged supervisor.
This uses the [client's execution restriction](https://developer.apple.com/documentation/endpointsecurity/es_new_descendants_client(_:_:)).

The registered executable contract excludes privileged credential authority.
The provider checks proposed exec credentials before it sends an allow reply.
Spawn credential attributes cannot change the admitted identities or the inherited hard limit.
Native fixtures verify these restrictions through direct syscalls and spawn attributes.
The [kernel creation path](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_fork.c) checks the real-UID process count.

The model does not claim synchronous fork admission for each tree.
Endpoint Security supplies a fork notification, not a fork authorization event.

The reviewed Darwin headers have no `POSIX_SPAWN_SETRLIMIT` flag or spawn attribute for `RLIMIT_NPROC`.
Resource attributes for file descriptors and ports do not change the process-count bound.
The [kernel spawn path](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_exec.c) checks privileged credential attributes and persona authority before execution.
The native probes verify UID and persona attributes, `POSIX_SPAWN_RESETIDS`, and a file-descriptor limit attribute.
These checks supplement syscall denial because spawn performs some operations in the kernel.

A command timeout becomes recoverable only after the completion protocol succeeds.
Cancellation, output-budget failure, cleanup failure, and unknown ownership stay terminal.
Quarantine prevents subsequent command activation, successful release, and application status publication.
The trusted supervisor retains the process policy, executable identities, and kernel resource leases.
Late events can identify more processes for termination.
They cannot change a damaged event history into successful release.

Quarantine does not claim that each unknown descendant stopped.
The operator can continue to see confirmed filesystem and daemon effects.
The trusted-host boundary excludes operating-system failure and forced destruction of the owner itself.
Caller cancellation and control-channel closure are supported cleanup triggers.
An unexpected supervisor exit makes ownership unknown.

## Resource Lease Lifetime

The selected profile transfers explicit ownership to the trusted supervisor before command creation.
Retention in the CLI alone cannot supply the required lifetime after CLI exit.

The transfer includes duplicate lock descriptors, executable descriptors, scope descriptors, and their immutable identity records.
It also retains the current program entry and its original ancestor witnesses when the launcher is external.
That entry record does not add command execution authority.
The supervisor acknowledges receipt only after it retains and validates each required owner.

The CLI cannot release the activation successfully before the supervisor supplies a complete cleanup receipt.
CLI exit closes the control channel and starts supervisor cleanup.
The supervisor keeps the leases during cleanup and quarantine after the CLI exits.
It cannot exit voluntarily while ownership stays unknown.

Command descendants receive no lock leases, supervisor control channels, or Endpoint Security handles.
The launcher closes these descriptors before it activates untrusted code.
The inherited sandbox denies command access to supervisor signals, task ports, and control resources.
The trusted supervisor has explicit host authority to retain these owners.
This guarantee excludes forced owner destruction by an external actor.
Native fixtures verify CLI exit, supervisor cleanup, retained lock exclusion, and release after proven completion.

One lock mutex covers the active-lease snapshot and the acquisition freeze.
While remote ownership remains active, every new lock acquisition produces `Lock.Failed` before an open or lock attempt.
This includes acquisition of a path that already has a retained lease.
Only complete remote release proof can remove the matching freeze generation.
Incomplete closure of a duplicated local lease also preserves the acquisition freeze.
Existing lease closure never issues an explicit unlock operation.

Each resource frame transfers one descriptor with its immutable identity record.
The sender waits for an indexed adoption acknowledgment before it sends the next resource.
The acknowledgment uses the same channel generation and sequence checks and carries no descriptor.
The supervisor sends it only after it owns the received descriptor.
This bounds descriptor transfers to one outstanding frame under the original deadline and cancellation checks.

The supervisor validates complete ancestor chains and executable hashes before it acknowledges the inventory.
Malformed frames retain received descriptors for explicit cleanup.
A failed close prevents successful ownership release.

Resource adoption can fail before the inventory transfer finishes.
The parent then validates the final cleanup receipt, channel closure, and supervisor reap before it releases its owners.
An adoption acknowledgment alone never proves complete inventory validation or cleanup.
Receipt arrival starts the configured cleanup wait for channel closure and reap.
This wait cannot extend an earlier cancellation or final deadline.

## Rejected Mechanisms And Observed Results

The native probes executed on macOS 27.2 with repository-owned C helpers.
All test outputs and compiler caches stayed in Lo.
The helpers did not execute installed cleaners or access actual cleanup roots.

| Mechanism | Observed result and limit |
| --- | --- |
| `NOTE_TRACK` | Darwin returned `ENOTSUP`. The [kernel filter](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_event.c) explicitly rejects descendant tracking flags. |
| Process groups | A detached descendant stayed alive after its initial group disappeared. |
| Parent-ID scans | The initial child list was empty while the controlled descendant stayed alive with parent PID 1. |
| Denied `setsid` and `setpgid` syscalls | Direct calls returned errors, but spawn attributes made a different session or group. |
| Added `posix_spawn` syscall denial | Spawn returned an error. This restriction cannot supply the selected spawn contract. |
| Coalitions | Creation returned `EPERM`. The [kernel interface](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/sys_coalition.c) needs an existing privileged coalition. |
| Endpoint Security admission | Client creation returned `ES_NEW_CLIENT_RESULT_ERR_NOT_ENTITLED`. Entitled execution was not tested. |
| Token-based signals | The initial token returned an error after exec. The current token stopped, continued, and terminated the correct child. |
| Process-count bound | A hard `RLIMIT_NPROC` of `0` denied fork and spawn with `EAGAIN` in a controlled helper. |
| Credential and limit filters | The sandbox denied same-UID `setuid` and unchanged `setrlimit` calls with `EPERM`. |
| Spawn credential attributes | UID and persona requests returned `EPERM`. `POSIX_SPAWN_RESETIDS` preserved the UID and hard process-count limit. |
| Spawn resource attributes | A file-descriptor limit attribute did not bypass a hard process-count limit of `0`. |
| Open marker | Repeated read-only opens retained the same device and inode identity. Entitled event delivery was not tested. |

The selected profile does not use these rejected mechanisms as an alternative after admission failure.
The selected contract also excludes the restricted fork-only process-group proposal.

## Reproducible Checks

Execute the probe driver with a task-owned directory in `_scratch`:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B tools/processownership/probe.py \
  --workdir _scratch/<task-id>/processownership
```

The driver builds repository-owned helpers and records their results and source identities.
The model checks 19 lifecycle traces and all selected loss and delivery prefixes.
More cases include cancellation, delayed births, exec replies, token limits, marker generations, missing cleanup conditions, client destruction, and permanent quarantine.
The model is a bounded algorithm check, not native Endpoint Security acceptance.

F036 public-entrypoint fixtures use the real ownership algorithm with an injected provider.
They exercise actual command processes, confinement, output, retained locks, and completion proof.
SDK adapter fixtures exercise native event conversion and reply ownership with controlled SDK boundaries.
The complete application fixture uses the same controller for each command and confirms an actual command child.
These checks do not establish entitled Endpoint Security connectivity or arbitrary installed-tool behavior.

Signal fixtures hold a confirmed `waitpid` result before the transport receives it.
The runtime retains the entry lock during that interval.
