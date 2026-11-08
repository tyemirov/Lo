# Tree snapshots and checked removal

This document specifies the implemented F034 contract.
Focused native checks and independent review passed.
Final stack CI remains pending.
The [metadata contract](file-system-observations.md) defines file observations.
The [retained observation contract](file-descendant-observations.md) defines descendant authority.

## Tree observation

The host observation has this signature:

```lo
File.Tree.snapshot(
  parent: File.Directory,
  root: File.Entry,
  contexts...,
  Descend(contexts..., entry: File.Entry) -> Bool
) -> File.Tree!
```

`parent` must retain recursive observation authority.
`root` must be a directory entry observed through that parent.
The operation requires the `File.observe_tree` capability.
An exact listing grant does not supply recursive observation authority.
The operation does not follow a symlink root or a symlink within the tree.

`File.Tree` exposes two fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `entries` | `List<Record{relative: String, entry: File.Entry}>` | Ordered metadata rows. |
| `complete` | `Bool` | Whether descent included every observed directory. |

The first row represents the root and has relative name `.`.
Other relative names have no leading slash.
Rows use preorder traversal with ascending sibling names.
For example, a directory `a` precedes `a/child`, which precedes its sibling `b`.

`Descend` must be a pure, total flow with a `Bool` result.
The runtime invokes it for directories only, including the root.
A rejected directory remains in `entries`, but its descendants do not.
Any rejection sets `complete` to false, including rejection of an empty directory.
Regular files, symlinks, and other non-directory entries remain observable rows.

The rows contain metadata without executable authority.
A row cannot supply authority to `File.Entry.observe`, `File.Directory.child`, or `File.Remove.new`.
The private tree retains one acquisition lease, identity witnesses, and the expected tree state.
The runtime does not retain one descriptor for each row.

The traversal uses an iterative algorithm and a bounded number of temporary descriptors.
The total row count includes the root and rejected directory rows.
Existing file-entry, work, value, and live-resource limits apply to the whole snapshot.
Completed traversal steps release their temporary descriptors.

## Removal construction

The pure constructor has this signature:

```lo
File.Tree.Remove.checked(
  expected: File.Tree,
  contexts...,
  Eligible(contexts..., row: Record{relative: String, entry: File.Entry}) -> Bool
) -> File.Tree.Remove!
```

The constructor captures the expected tree, callback, and contexts.
It performs no filesystem observation, callback invocation, or mutation.
It rejects an incomplete tree or a tree without private observation data with `File.InvalidSelection`.
`Eligible` must be a pure, total flow with a `Bool` result.

The target activates through `commit` and returns `File.RemoveReceipt!`.
The approved tree remains immutable during activation.
Application selection rules belong in the callback and its contexts.
These rules can include owner, device, age, entry kind, or link count.

## Removal authority

Activation requires the separate `File.remove_tree` capability.
One recursive removal grant must cover the selected root.
Its complete identity witnesses must form an identity-equal prefix of the selected root witnesses.
A path string prefix is insufficient.

The grant can cover the acquisition root or the narrower selected root.
A grant below the selected root cannot authorize removal of the entire selection.
An exact `--remove-root` grant cannot authorize tree removal.
These rules do not change the existing `File.Remove` authority contract.

The runtime checks session identity, live leases, observation witnesses, and removal authority at activation.
It checks the applicable witnesses again before each unlink.
Foreign sessions, released leases, absent authority, and replaced grant ancestors cause terminal failures.
After the final root unlink, the runtime does not reopen the removed grant root.

## Complete preflight

Before the first mutation, the runtime reobserves the entire selected topology.
Every expected name must remain present with its expected identity.
Added names, replaced entries, and vanished entries reject the activation before mutation.
The runtime also checks the program entry, active lock identities, protected executables, and namespace resources for every row.
A protected later row thus keeps earlier candidates unchanged.

Mandatory metadata comparison includes private identity and every public metadata field except directory access time.
It includes `allocated_bytes` and retains access-time comparison for non-directory entries.
Directory enumeration can change directory access time.
The eligibility callback still receives the fresh access time from that observation.

The runtime invokes `Eligible` for fresh rows during preflight.
Every row must pass before the first unlink.
A false result causes `File.EntryChanged` with condition `predicate-false`.
The callback does not run again after mutation starts.
This rule prevents application age checks from rejecting directory changes caused by the preceding child removal.

The runtime reserves the removal plan, private expected state, and progress storage before mutation.
Cancellation and exhausted resource limits stop activation through the existing terminal failure channel.

## Removal and private expected state

Removal uses postorder traversal with ascending sibling names.
The root is last.
Before each unlink, the runtime compares the selected row and retained ancestors with their current expected state.
It also checks protected runtime resources again.

For each parent inside the selected tree, the remaining names must equal approved names minus confirmed removals.
The enclosing parent can contain unrelated siblings outside the selection.
For that parent, the runtime checks retained identity and the selected root binding.
The runtime does not expand the selection to include new names.

Immediately after a successful unlink, the runtime records confirmed progress.
This update precedes further observations, cancellation checks, and resource closure.
The removed name leaves the private expected name set.
The public snapshot does not change.

The runtime then reobserves the direct parent.
Its private identity, entry kind, and owner must remain unchanged.
Its remaining names must equal the updated expected set.
The runtime can then update only directory size, allocation, link count, modification time, and change time in private expected state.

Selected hardlink aliases share an index based on private device and inode identity.
After an unlink, the runtime reobserves the remaining selected aliases.
Their names, identity, kind, owner, size, allocation, modification time, and access time must remain unchanged.
Their common link count must decrease by exactly one.
The runtime can then update only their private link count and change time.

Hardlinks outside the selected tree remain outside the removal plan.
The generic operation does not require a single link.
Applications can impose that restriction through `Eligible`.
Symlink rows are unlinked without traversal of their targets.
Other non-directory rows use ordinary unlink semantics.

## Receipts and failures

The successful `File.RemoveReceipt` contains these fields:

| Field | Meaning |
| --- | --- |
| `total_count` | Number of approved rows, including the root. |
| `completed_count` | Number of confirmed removals. |
| `observed_bytes` | Sum of original approved `size_bytes` for confirmed rows. |

The logical byte sum includes directories and symlinks.
It does not measure reclaimed storage or use later directory sizes.
Applications can report allocation observations separately.

Tree failure progress contains six fields:

| Field | Meaning |
| --- | --- |
| `total_count` | Number of approved rows. |
| `completed_count` | Length of the confirmed postorder prefix. |
| `completed_bytes` | Original approved logical bytes for that prefix. |
| `failed_index` | Index of the failed row in the original preorder list. |
| `failed_relative` | Relative name of that row. |
| `stage` | `preflight`, `remove`, `postcheck`, or `finalize`. |

A failure without one affected row uses index `-1` and an empty relative name.
A failed postcheck includes the preceding successful unlink in confirmed progress.
Cancellation or close failure after the final unlink retains the complete removal count.
Recoverable messages also state the confirmed count, total count, approved logical bytes, and failure stage.
The message identifies the failed relative name when one row is known.
Applications that handle an error can report this evidence without decoding the input record.

| Boundary | Recoverable failure | Condition |
| --- | --- | --- |
| Snapshot | `File.DirectoryChanged` | A retained directory, selected root identity, or observed ancestor changed. |
| Snapshot | `File.DirectoryAbsent` | The selected root disappeared before acquisition. |
| Snapshot | `File.DirectoryFailed` | The selected root is not a directory, or a directory operation failed. |
| Snapshot | `File.ListFailed` | Child discovery failed, including disappearance of an enumerated name. |
| Snapshot | `File.InvalidName` | A discovered name contains invalid UTF-8. |
| Constructor | `File.InvalidSelection` | The tree is incomplete or lacks private observation data. |
| Activation | `File.EntryChanged` | Topology, identity, metadata, eligibility, or a postcheck differs from expected state. |
| Activation | `File.RemoveFailed` | Another observation or removal operation failed. |

A lone close failure is terminal.
If cleanup also fails after a recoverable primary failure, the existing Files contract keeps that primary failure and its `cleanup_errors`.
Terminal authority, cancellation, and resource failures remain terminal when cleanup also fails.
No failure rolls back a confirmed removal.

## Race limits

Snapshot and preflight each contain a sequence of observations.
Neither operation supplies one atomic filesystem view.
The final identity check and native unlink remain separate operations.
An external actor can replace an entry between those operations.

Removal can change directory metadata and hardlink metadata.
An external actor can change those same fields before the following observation.
The runtime cannot always attribute a change within that interval to one actor.
The expected-state rules do not establish atomic deletion or complete race detection.

A change in another branch can remain undetected until after a confirmed removal prefix.
The runtime stops at the first detected mismatch or failed operation.
It reports confirmed progress and performs no further removal.
It does not adopt unexpected rows, expand the approved selection, or promise rollback.
