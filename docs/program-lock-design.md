# Program Entry And File Locks

F029 implements program entry identity and exclusive file locks in the Rust runtime.
This document defines the implemented contract.

## Public Interfaces

| Operation | Result | Behavior |
| --- | --- | --- |
| `Program.path()` | `File.Path!` | Observe the captured program entry path. |
| `Lock.File.new(path: File.Path)` | `Lock.File` | Construct a pure, inert lock target. |
| `commit target` | `Lock.Lease!` | Acquire an exclusive lock without a wait. |

`Program.path` is a host observation.
Pure callbacks cannot use this operation.
Unavailable entry identity produces `Program.EntryUnavailable`.
The operation does not grant File or Lock authority.

`Lock.File` contains path data without host authority.
Construction does not open, create, write, or lock a file.
The selected contract permits only exclusive acquisition without a wait.
It has no shared lock, blocking acquisition, or explicit release operation.

`Lock.Lease` confirms acquisition.
It has no public fields, file descriptor, or release authority.
Its value weight is 16 bytes.
The session owns the lock independently of every Lo value.

Automatic output for both nominal types contains only `redacted: true`.
A diagnostic snapshot cannot authorize activation.
A live `Lock.File` includes its path weight plus 16 bytes.
A target snapshot has a weight of 16 bytes.

## Entry Capture

The native CLI captures the actual executable through operating-system interfaces.
It does not use an embedded source path.
A relocated executable reports its actual launch location.
The runtime retains the captured file identity.
Checked graph metadata and `argv[0]` do not establish entry identity.

On Linux, `/proc/self/exe` identifies the running executable object.
On Darwin, the main image address selects a memory region with a backing vnode.
The runtime compares this vnode identity with the retained executable descriptor.
A pathname lookup alone cannot establish running-image identity after replacement.

Embeddings have no inferred program entry.
They can inject entry identity before evaluation.
Injection gives no lock, read, write, or execution authority.
Successful entry observation requires a retained regular file identity.
Injected entry capture applies the same alias resolution and retained identity check.
Missing identity remains a typed observation failure.

Applications that do not observe entry identity can still execute.

The captured path is immutable.
It describes the location at capture, not every subsequent external rename.
Entry lock authority remains bound to the captured object.

## Host Authority

`--lock-file PATH` grants lock authority for one existing regular file.
`--lock-entry` grants lock authority for the captured program entry.
Explicit grant paths must be clean absolute paths.
Target path text must match an admitted grant exactly.
Repeated explicit grant paths fail admission.
The entry selection does not duplicate an explicit grant for the same path.

Each grant path has a maximum of 4096 UTF-8 bytes, excluding its terminal NUL.
It has a maximum of 256 normal path components, including the basename and excluding the root.

Ordinary paths and File grants do not grant lock authority.
The host admits at most 128 lock grants and 128 live lock leases per session.

Grant admission retains the file identity and the necessary directory identities.
Admission rejects symlink traversal.
It does not create, truncate, or remove a file.
Missing or invalid grant inputs produce host admission failures.
The application cannot expand admitted authority through a constructed path.

Lock authority does not change separately granted File or Process authority.
Applications can write locked file content when their File grants permit that operation.

Each acquisition opens a fresh independent file descriptor.
It does not duplicate the descriptor retained by the grant.
Duplicated descriptors share an open file description and cannot establish independent contention.

The host checks the selected file and directory identities before acquisition.
It applies `flock(LOCK_EX | LOCK_NB)` to the fresh descriptor.
It checks the selected pathname again before publication of the lease.
A detected replacement produces `Lock.Failed`.
Contention produces `Lock.Busy`.
Repeated acquisition in the same session also competes for the same exclusive lock.

Both recoverable failures have an empty record as input.
Their messages do not include private path data.
Missing authority, exceeded host limits, cancellation, and cleanup failures remain terminal host failures.

## Lifetime And Cleanup

The host session owns all acquired descriptors.
Ordinary value disposal, flow return, and request completion cannot release a lock lease.
Request sessions share the application owner.

Shutdown prevents new acquisition before resource cleanup starts.
The owner waits for active operations and retained ownership.
HTTP and Process cleanup complete before lock release.
File resource cleanup also completes before lock release.
Failure and cancellation use the same ownership sequence.

An unresolved child join retains the lock.
A caller timeout cannot transfer or discharge that ownership.
Unknown ownership without a join handle retains the lock owner in quarantine and prevents successful completion.
The final owner performs checked descriptor cleanup once.
It does not retry a descriptor close with an uncertain result.
A close failure prevents successful application completion.

Final `Program.Exit` output follows application cleanup and lock release.
The final reporting transport is outside the protected application work.
An embedding must complete checked cleanup after successful or failed evaluation.
Its destructor must preserve child joins and lock ownership.

Host process termination ends operating-system descriptor ownership.
The normal cleanup guarantee does not extend past that termination.

Acquired descriptors use close-on-exec protection.
Executed application, probe, and writer images cannot inherit them.
A trusted fork stub can briefly share descriptors before exec.
The host retains and joins that stub during creation failure and cancellation.

## Filesystem Guarantee

The lock applies to the retained file object.
Hardlinks to that object contend for the same lock.
Both runtimes use the same native lock protocol.
An interpreted caller and a native caller contend when their grants select the same object.
An interpreted source and its compiled executable are separate objects.

The lock is advisory.
Uncooperative external operations can rename or replace a locked pathname.
Such replacement does not transfer the lock to the new object.
A separately admitted replacement can acquire its own lock.
The acquisition identity checks bound the race before lease publication.
They do not establish permanent pathname exclusion.

These properties follow the [Darwin flock contract](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/flock.2.html)
and the [Linux flock contract](https://man7.org/linux/man-pages/man2/flock.2.html).
Darwin image identity uses the [kernel region interface](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/proc_info.h).

## Acceptance

- Confirm absent interfaces through public tests before production changes.
- Verify constructor purity, redaction, authority separation, and callback effects.
- Verify two-process contention and mixed interpreted/native contention.
- Verify hardlinks, symlinks, relocation, replacement, and unavailable entry identity.
- Verify independent acquisition and release after success and recoverable failure.
- Verify cancellation, retained child cleanup, and ordinary value disposal.
- Inject acquisition and close failures at the host boundary.
- Verify terminal failure after unsuccessful cleanup.
- Verify descriptor absence in executed children.
- Verify runtime parity on Darwin arm64 and Linux arm64 under QEMU.
- Complete independent review and final repository-native CI.

All test data and generated output remain inside Lo.
The mac-cleanup application and its file selection remain outside this language contract.
