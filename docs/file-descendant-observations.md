# Retained Descendant Observations

Status: implemented F033 contract. Focused validation and independent review passed.
Final shared stack CI passed.

Lo supplies retained filesystem observations for general application traversal.
Applications select owners, devices, ages, names, and descent rules.
Path text does not supply descendant authority.

## Host Authority

`--list-root` retains its exact-root observation meaning.
`--remove-root` retains its exact-root removal meaning.
F033 adds separate `--observe-tree-root` and `--remove-tree-root` grants.
A recursive removal grant does not supply observation authority.

`File.directory`, `File.list`, and `File.scan` can observe a recursive observation grant's root.
These operations cannot use that grant to observe descendant path text.
Descendant operations require a retained parent and an entry from that parent.
If exact and recursive observation grants select the same root, root observations retain recursive authority.
`File.System.usage` continues to require an exact `--list-root` grant.

An exact observation with a recursive removal grant cannot descend.
An exact removal grant for the acquisition root cannot remove entries from a descendant parent.
Removal requires the recursive grant for the retained root, or an exact removal grant for the selected entry's parent.

## Operations

| Operation | Result | Behavior |
| --- | --- | --- |
| `File.Directory.refresh(directory)` | `File.Directory!` | Return current metadata after retained identity checks. |
| `File.Directory.scan(directory)` | `File.Scan!` | Return sorted entries and separate vanished names. |
| `File.Directory.entry(directory, name)` | `File.Entry!` | Observe one direct basename without following its final symlink. |
| `File.Directory.child(directory, entry)` | `File.Directory!` | Retain an observed child directory under recursive observation authority. |
| `File.Entry.observe(entry)` | `List<File.Entry>!` | Return one current entry, or an empty list for an absent entry. |

All five operations are host observations.
They cannot execute inside a pure callback.
`Flow.fold` and `Flow.worklist` can execute them in ordered application flows.

An entry name cannot be empty, `.`, `..`, or contain `/` or NUL.
An invalid basename produces `File.InvalidName`.
Strict entry absence produces `File.EntryAbsent`.
Other entry metadata failures produce `File.ListFailed`.

A child that is not a directory produces `File.DirectoryFailed`.
This includes a symlink to a directory.
A child directory that disappears produces `File.DirectoryAbsent`.

A replaced retained directory or ancestor produces `File.DirectoryChanged`.
Foreign sessions, released observations, mismatched parents, and missing authority produce terminal capability failures.
A metadata observation contains no independent removal authority.
Snapshot rows cannot substitute for retained entries.

## Identity And Lifetime

One retained root lease supplies the authority for its descendant observations.
Each descendant retains the relative ancestor identities needed to reopen its path.
No operation follows a symlink during that walk.
The walk uses a bounded number of temporary descriptors.
Pending ancestors do not each retain another descriptor.

The runtime verifies root and ancestor identities before and after host observation callbacks.
Exact grants receive the same ancestor checks.
An unchanged root inode cannot conceal replacement of an ancestor.
An absent root requires unchanged captured ancestors.
A changed exact grant produces a terminal capability failure.

The current execution session owns the root lease.
The runtime keeps the lease while reachable values or pending work require it.
Existing cancellation, resource quotas, and close-failure rules apply.
A cumulative traversal can exceed 128 directories without retaining 128 live directory descriptors.

A trusted metadata observer must be installed before the session starts evaluation.
Evaluation through a shared peer seals registration for the whole session.
Freeing that peer does not permit later installation.
The observer cannot change private filesystem identity.

## Validation Boundary

Public integration fixtures exercise exact and recursive authority separately.
Controlled native callbacks replace ancestors during observations and test shared-session registration.
Deep, wide, and pending-ancestor worklists exercise bounded retained ownership.
Darwin and Linux QEMU provide the software acceptance environments.
Application traversal acceptance remains under I013.
