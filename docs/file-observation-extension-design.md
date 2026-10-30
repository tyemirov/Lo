# Host and File Observation Extension

Status: implemented and accepted for F025 on October 28, 2026.
This document defines the operations, fields, runtime boundaries, and acceptance criteria.
The [File adapter design](file-adapter-design.md) describes the current implementation.
The [mac-cleanup plan](mac-cleanup-plan.md) supplies application acceptance requirements.
The [shared adapter design](adapter-design.md) owns activation and callback rules.

## Scope and Decisions

F025 extends immutable observations and explicit removal activation.
The extension uses existing declarations, projections, named flows, pipes, and fallibility.
It requires more catalog entries and types, with no grammar extension.

- Keep property access pure and independent of filesystem access.
- Capture host inputs once before program evaluation.
- Get each file observation from one native metadata result.
- Keep retention periods, suffixes, user comparisons, and category decisions in Lo flows.
- Report vanished children explicitly through a scan report.
- Evaluate removal guards only during explicit activation.
- Keep authority in host grants and private entry provenance.
- Implement one current contract in the interpreter and native runtime.
- Reject obsolete schemas instead of adding aliases or compatibility reads.

F025 does not implement the complete cleanup application.
Cleanup locking, candidate-path open-file probes, and category orchestration require subsequent work.
The current Gateway example continues to use strict observations and unconditional removal construction.
Its selection policy is a current application contract.

## Public Operations

| Operation | Result | Evaluation phase |
| --- | --- | --- |
| `Host.Invocation.current()` | `Host.Invocation!` | Host observation of captured inputs |
| `File.directory(root: File.Path)` | `File.Directory!` | Host observation |
| `File.list(root: File.Path)` | `List<File.Entry>!` | Strict host observation, as currently implemented |
| `File.scan(root: File.Path)` | `File.Scan!` | Host observation with explicit vanished names |
| `File.Remove.new(entries: List<File.Entry>)` | `File.Remove!` | Pure construction, as currently implemented |
| `File.Remove.checked(entries, context..., Eligible)` | `File.Remove!` | Pure construction with a removal guard |

The last operation is a typed higher-order constructor.
Its predicate signature is `Eligible(context..., original: File.Entry, current: File.Entry) -> Bool`.
Each context argument has its inferred concrete type.
These operations require no user-defined types, module system, or general-purpose generics.
F025 extends the current execution interface and documents its separator and limits.
Application tooling and CLI help remain separate work.

## Host Invocation

`Host.Invocation` is a nominal immutable value with these public projections.

| Projection | Type | Meaning |
| --- | --- | --- |
| `arguments` | `List<String>` | Program arguments after the explicit CLI separator |
| `home` | `File.Path` | Initial absolute `HOME` value |
| `real_uid` | `Int` | Initial real UID |
| `effective_uid` | `Int` | Initial effective UID |
| `now_unix_ns` | `Int` | Initial wall-clock time in Unix nanoseconds |

The value has no public constructor, equality, ordering, or checkpoint representation.
User IDs are data. They grant no filesystem or process authority.
The cleanup flow decides whether an effective UID of zero is permitted.

### Capture and Lifetime

- Capture the invocation snapshot after CLI parsing and before Lo evaluation.
- Use the same initial environment capture that supplies `Process.Context`.
- Read `HOME` from that capture only.
- Require a nonempty absolute home path with valid UTF-8 and no NUL.
- Validate the home path as data without filesystem access or symlink resolution.
- Capture the real UID, effective UID, and wall-clock time once.
- Keep either the complete snapshot or its capture error in the runner context.
- Return the capture result from every `Host.Invocation.current()` call.
- Return capture errors only when the operation executes.
- Preserve unrelated program execution when the snapshot contains a capture error.

For `serve`, the server startup and its HTTP callbacks share the initial snapshot.
A request does not recapture the clock, home, user IDs, or arguments.
Independent runner invocations capture independent snapshots.
Embedded runners can inject validated snapshots through a typed host API.
The injection API is outside the Lo operation catalog.

### Argument Boundary and Limits

Source execution accepts `lo run source.lo [runner options] -- [program arguments]`.
The same separator applies to `lo serve` and native artifact runtime options.
The snapshot excludes the executable, command name, source path, runner options, and separator.
Without the separator, the argument list is empty.
After the separator, values that start with `-` are literal program arguments.
An explicit empty argument stays present.
Inspection commands do not capture or execute host observations.

- Reject invalid UTF-8 and NUL in supplied program arguments at the CLI boundary.
- Permit at most 4096 program arguments.
- Bound each encoded argument to 65536 bytes, including its terminal NUL.
- Bound the complete encoded argument vector to 1048576 bytes, including its framing.
- Bound the complete snapshot transport to 1048576 bytes, including framing and the home value.
- Represent UIDs in the inclusive range `0..4294967295`.
- Represent the clock in the signed 64-bit Unix-nanosecond range.
- Reject invalid argument data or argument limits with CLI exit status 2 before program evaluation.
- Keep `Host.InvocationFailed` for invalid home, UID, clock, complete snapshot size, or snapshot transport data.
- Return that capture failure only when `Host.Invocation.current()` executes.
- Preserve terminal budget and cancellation failures as host diagnostics.

The argument vector uses `u32be(count)` followed by one `u32be(length), payload` pair per argument.
Each payload contains UTF-8 bytes and one terminal NUL.
Its length includes that NUL and excludes the length prefix.
The complete snapshot contains the argument vector, home string, real UID, effective UID, and clock, in that order.
The home string uses the same length and payload representation.
UIDs use `u32be`, and the clock uses `i64be`.
`u32be` is an unsigned 32-bit integer with the most significant byte first.
`i64be` is a signed 64-bit integer with the same byte order and two's-complement representation.
Both runtimes calculate admission sizes from this exact representation.

The decoder validates lengths, bounds, terminal NUL bytes, interior NUL absence, UTF-8, and field shapes before construction.
It rejects truncated fields and trailing bytes.
Malformed snapshot transport produces the capture failure without a partial snapshot.
Source execution and native artifacts use the same admission and deferred-error rules.
Lo projections use `Int` even when the boundary representation has a smaller range.

### Automatic Output

Automatic output shows the nominal type, argument count, UIDs, capture time, and redaction status.
It redacts the home path and argument values.
The same rule applies inside records, lists, commands, errors, and removal contexts.
An explicit projection returns the requested value under normal Lo output rules.
Capture errors report the field and operation without the rejected home or argument contents.

## File Metadata

`File.Entry` includes these three metadata projections with its path, name, kind, size, modification time, and link count.

| Projection | Type | Meaning |
| --- | --- | --- |
| `owner_uid` | `Int` | Observed owner UID |
| `accessed_unix_ns` | `Int` | Observed last access time |
| `changed_unix_ns` | `Int` | Observed last inode metadata-change time |

`modified_unix_ns` is the observed data-modification time.
`changed_unix_ns` is not creation time.
Each timestamp uses signed 64-bit Unix nanoseconds at the native boundary.
Negative timestamps are valid when they are within that range.
An invalid UID or timestamp causes the observation to fail.
The adapter never replaces an unavailable timestamp with zero or the current clock.

Each observation contains metadata from one native result without a content read.
Reading a projection never repeats that observation.
A directory observation is not an atomic filesystem snapshot.
File updates can occur between observations of different children.

The new fields participate in serialization, diagnostic snapshots, and value-weight accounting.
Printed values contain no usable session token, directory handle, device identity, or inode identity.
Copies and list processors preserve the original private entry provenance.
A reconstructed record cannot satisfy `File.Entry` or authorize removal.

## Directory Observations

`File.directory` uses an exact `--list-root` grant.
It observes the granted directory through the same open root used by listing and removal.
It returns the nominal `File.Directory` value with these projections.

| Projection | Type |
| --- | --- |
| `path` | `File.Path` |
| `owner_uid` | `Int` |
| `accessed_unix_ns` | `Int` |
| `modified_unix_ns` | `Int` |
| `changed_unix_ns` | `Int` |

The value has private session and directory provenance.
It has no public constructor, equality, ordering, or checkpoint representation.
It cannot be supplied to either removal constructor.

- Reject symlink traversal and roots that are not directories.
- Preserve the exact grant's native identity through the observation.
- Return `File.DirectoryAbsent` for an absent, explicitly granted directory.
- Keep an absent grant inert if a directory later appears at that path.
- Return `File.DirectoryFailed` for another root metadata failure.
- Preserve existing host authority diagnostics for missing grants or replaced roots.
- Reuse open handles within the configured quota for simultaneously retained directory observations.

Temporary-root policy compares the observed owner with `Host.Invocation.real_uid` in Lo.
The application validates the path requirements from the mac-cleanup plan.
The directory operation does not select a temporary root or read `TMPDIR`.

## Scan Reports and Disappearance

`File.scan` uses the same exact `--list-root` grant and root confinement as `File.list`.
It returns the nominal immutable `File.Scan` value.

| Projection | Type | Meaning |
| --- | --- | --- |
| `directory` | `File.Directory` | Metadata for the open root before child enumeration |
| `entries` | `List<File.Entry>` | Complete observations of surviving children |
| `vanished_names` | `List<String>` | Enumerated basenames whose metadata lookup reported absence |

The report has no public constructor, equality, ordering, or checkpoint representation.
All child lookups stay relative to the open directory and do not follow symlinks.

- Count every enumerated basename before its metadata lookup.
- Include vanished names in the configured file-entry quota.
- Enforce count and storage limits during enumeration, before accumulation of the complete directory contents.
- Reject invalid UTF-8 names with `File.InvalidName`.
- Record a vanished name only when its child metadata lookup reports `ENOENT`.
- Return `File.ListFailed` for another enumeration or child metadata failure.
- Preserve root absence, replacement, and authority failures as root failures.
- Return no partial report after an observation failure.
- Sort `entries` by basename and `vanished_names` by name in Unicode scalar order.
- Keep the two collections disjoint and reject duplicate enumerated names as `File.ListFailed`.
- Validate the open root against the exact grant before publication of the report.

The report describes each enumerated name once, at its metadata lookup.
A name can reappear after a vanished observation.
The adapter does not retry that name or treat the report as a later filesystem state.

`File.list` is the strict observation operation.
It returns `File.ListFailed` when an enumerated child disappears.
These operations have different current result and failure contracts.
The common cleanup flows consume `File.scan` and report vanished names as skips.
The Gateway flow consumes `File.list` and propagates discovery failures.

## Conditional Removal

### Construction

`File.Remove.checked(entries, context..., Eligible)` constructs the same nominal target type as `File.Remove.new`.
It captures the original entries, immutable context values, and resolved predicate reference.
The target keeps a closed internal selection form: unconditional or guarded.
Both forms require exact removal authority during activation.

- Require the predicate parameters to match the context arguments, original entry, and current entry.
- Require the predicate to return total `Bool`.
- Reject direct or transitive host observations, commits, and fallible results in the predicate.
- Apply these rules to empty selections and unused targets.
- Validate the selection under the current provenance and duplicate-entry rules.
- Charge captured contexts, entries, and target data before allocation.
- Do not access the filesystem or invoke the predicate during construction.

The constructor does not supply a default predicate.
`File.Remove.new` keeps its current meaning for unconditional selections.
It does not add owner, access-time, change-time, or link-count comparisons implicitly.

### Activation Order

The [current removal contract](file-adapter-design.md#commit-and-failure-progress) owns generic progress and receipts.
Conditional removal uses its preflight, remove, and finalize stages.

1. Authorize the complete selection and reserve the current base removal work and diagnostic storage.
2. During preflight, process the original entries in selection order.
3. Get current metadata through each open directory.
4. Apply the mandatory native identity, size, modification-time, and regular-file comparisons.
5. If those comparisons pass, invoke the predicate with the fixed original entry and new current entry.
6. If the predicate returns false, return `File.EntryChanged` before the first removal.
7. After successful preflight, process the entries again in selection order.
8. Immediately before each removal, repeat the observation, mandatory comparisons, and predicate evaluation.
9. If these operations succeed, remove the basename through its open directory and record confirmed progress.
10. On failure, stop the suffix and preserve the completed prefix under the current error contract.

For each reached entry, the predicate runs at most once per phase.
A successful nonempty activation invokes it twice per entry.
An empty activation invokes it zero times.
Each invocation receives a new current observation and the same original observation.
Earlier removals can affect later observations, including the link count of another selected hardlink.
The remove phase never reuses preflight metadata.

A false predicate produces `File.EntryChanged` with the existing entry index and progress stage.
Its error input also identifies the predicate, callback phase, and fixed condition `predicate-false`.
The runtime does not infer which application expression caused the false result.
Another metadata or removal error produces `File.RemoveFailed`.
Budget exhaustion, cancellation, and invalid host contracts stay terminal host failures.
A successful activation returns the current `File.RemoveReceipt` shape.
No failed activation publishes a bound receipt.

The common cleanup flow compares owner, access time, change time, and link count against its original observation.
It also applies its eligibility predicate to the current entry with the fixed invocation snapshot and cutoff.
The flow handles `File.EntryChanged` as a skip for singleton targets through `Flow.fold`.
Other failures stay visible to the category handler.

The final metadata check and removal stay separate native operations.
The guard does not provide atomic deletion, writer exclusion, or protection against a later open.
The cleanup-instance lock has no such guarantee either.

## Lo Selection Example

This fragment demonstrates metadata projections and the exact guard signature.
It omits suffix, path, file-kind, and link-count rules that the complete application must also apply.

```lo
flow OwnedBy(user_id: Int, entry: File.Entry) -> Bool {
    return Math.eq(entry.owner_uid, user_id)
}

flow AccessStable(original: File.Entry, current: File.Entry) -> Bool {
    return Math.eq(original.accessed_unix_ns, current.accessed_unix_ns)
}

flow EligibleNow(user_id: Int, cutoff: Int, original: File.Entry, current: File.Entry) -> Bool {
    return Match.new(OwnedBy(user_id, current),
        true: Match.new(AccessStable(original, current),
            true: Math.lt(current.accessed_unix_ns, cutoff),
            _: false
        ),
        _: false
    )
}

flow RemoveObserved(root: File.Path, cutoff: Int) -> File.RemoveReceipt! {
    invocation = Host.Invocation.current()
    scan = File.scan(root)
    owned = scan.entries |> List.filter(invocation.real_uid, OwnedBy)
    target = File.Remove.checked(owned, invocation.real_uid, cutoff, EligibleNow)
    receipt = commit target
    return receipt
}
```

This fragment demonstrates batch failure, rather than category continuation.
The application uses singleton targets when a changed candidate must become a skip.
Each predicate reads immutable values and has no hidden metadata lookup.

## Internal Implementation

| Component | Required implementation |
| --- | --- |
| Catalog and types | Add the Host namespace, nominal types, operations, error kinds, field projections, and conditional construction rule. |
| CLI | Parse the explicit program-argument separator in source and artifact execution. Publish the separator and limits in CLI documentation. |
| Checker and IR | Resolve concrete contexts and predicate parameters. Record the pure constructor and activation callback separately. |
| Runner context | Capture validated invocation data or its error once. Share the initial result with HTTP callbacks. |
| Go filesystem boundary | Extend platform metadata conversion for Darwin and Linux. Get each observation through the open root. |
| Go values | Extend `File.Entry`. Add sealed directory and scan values and guarded removal descriptions. |
| Native runtime | Extend Rust metadata conversion, nominal payloads, invocation capture, and removal callback dispatch. |
| Lowering and ABI | Encode the resolved predicate identity, contexts, and exact nominal schemas in current native metadata. |
| Inspection | Report host observations, exact or dynamic roots, and conditional removal with its activation-phase predicate. |
| Serialization | Add all public fields and recursive invocation redaction without serialized authority. |

The Go field definitions belong in `internal/types/types.go` beside the current `File.Entry` projections.
The Go observation path belongs in `internal/adapter/file_entries.go` and its platform metadata adapters.
The nominal values belong in `internal/value/file.go` and the invocation value implementation.
The existing `internal/execution/process_context.go` supplies the lifetime pattern for capture results.
The Rust observation path belongs in `runtime/native/src/file_entries.rs`.
Its native context stores the immutable invocation capture result and dispatches guard flows through the ordinary flow evaluator.

Implementation must update both runtime inventories and their schema assertions together.
The interpreter and compiled artifacts must publish the same fields, errors, progress, and callback order.
Each observation operation has a host-read effect, including access to an invocation snapshot that is already captured.
Only projection from the returned values is pure.
Inspection must not capture host inputs, enumerate directories, invoke predicates, or execute commands.
Automatic trace inputs must redact an invocation snapshot inside any captured context.

### Resource Accounting

- Apply the [host resource quotas](../specification.md#89-dynamic-worklists-and-host-resource-quotas) to enumerated entries and simultaneously retained directory observations.
- Charge one metadata step for `File.directory`, in addition to ordinary operation evaluation.
- Charge one root metadata step for `File.scan`, in addition to its child discovery and sorting work.
- Charge one discovery step for every enumerated name, including vanished names.
- Reserve sorting work for the complete enumerated count before sorting either result collection.
- Include all names, metadata, nested values, and private provenance overhead in value-weight reservations.
- Apply the configured value-byte and live-value quotas.
- Reserve `3 * N` base steps for removal before preflight, as in the current removal contract.
- Include the preflight metadata lookup, final metadata lookup, and removal attempt per entry in that reservation.
- Charge no discovery step for those two activation observations.
- Compare each new observation with the configured resource quotas before allocation.
- Charge predicate evaluation and flow invocation through ordinary execution steps in addition to the base reservation.
- Charge captured contexts and preserve the current 256-frame recursion limit during callback evaluation.
- Release temporary observations on success, rejection, cancellation, and failure.
- Keep open directory handles until session cleanup completes.

## Implementation and Acceptance

F025 implementation and documentation are completed.
Required code validation passed.
F025 records the public acceptance results for this contract.

The focused fixtures passed in the interpreter and on both native targets.
Final repository CI also passed.
Darwin arm64 ran directly. Linux arm64 ran under QEMU.
Independent reviews found no unresolved defects.

1. Add public integration fixtures for the new projections and invocation snapshot before production changes.
2. Add directory and scan fixtures with controlled disappearance and unrelated metadata failures.
3. Add conditional-removal fixtures with changes between discovery, preflight, and removal.
4. Confirm the expected public failures before implementation.
5. Implement types, boundary conversion, pure construction, activation, and native parity in that order.
6. Update the language reference and current adapter documentation after implementation.
7. Run the focused repository targets during each change.
8. Complete independent review and final `make ci` after the last correction.

| Contract | Required acceptance cases |
| --- | --- |
| Invocation | Empty and literal arguments, explicit separator, home validation, UID bounds, negative clock, overflow, and capture failure. |
| Lifetime | Repeated observations after injected host changes, independent invocations, server startup, and HTTP callback reuse. |
| Redaction | Direct values, nested containers, errors, automatic output, and captured removal contexts with explicit projection access. |
| Metadata | Exact owner and timestamps, negative timestamps, range failures, pure projections, provenance copies, and serialization. |
| Directory | Missing, symlinked, replaced, unauthorized, and valid roots with an owner distinct from the injected real UID. |
| Scan | Empty results, sorted names, vanished children, reappeared names, other lookup failures, invalid names, and enumeration limits. |
| Guard construction | Unused targets, empty selections, wrong signatures, fallible predicates, transitive reads, and transitive commits. |
| Guard activation | Exact callback counts and phases, fixed originals, new current values, rejected eligibility, and predicate work exhaustion. |
| Mutation | Owner, access-time, change-time, and link-count changes without reliance on modification-time changes. |
| Progress | Zero removals after preflight failure, a confirmed prefix after remove-phase failure, singleton skips, and unchanged siblings. |
| Current behavior | Gateway strict discovery, unconditional removal, hardlinks, error propagation, and existing race-limit statements. |
| Runtime parity | Interpreter, Darwin arm64 artifacts, and Linux arm64 artifacts under software emulation. |

Tests inject clocks, identities, and metadata failures through typed host boundaries.
Fixtures use repository-local directories and actual public Lo entry points.
Tests do not require a host-user change to produce an owner mismatch.
An injected metadata case proves the specified behavior, rather than a native ownership-change operation.
Native boundary tests also compare observations with the actual fixture metadata.

## Deferred Application Work

The mac-cleanup plan owns root selection, suffixes, retention periods, and category reports.
It also owns startup rejection, cleanup locking, package commands, Docker policy, and final exit status.
Candidate-path probes require a separate contract for bounded NUL framing and exact candidate batches.
F025 does not claim that the current line-framed directory probe supplies that contract.
No acceptance step changes installed scripts, actual package caches, Docker resources, or data outside Lo.

There are no unresolved API choices within the F025 scope selected here.
Implementation findings that require a contract change must update this design before the corresponding production change.
