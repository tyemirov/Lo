# File Operations

[Documentation](../README.md) / [Operation index](README.md)

File values separate path text, host observations, inert targets, and committed changes.
The examples contain complete programs for type checking. Read and probe examples need explicit grants if executed.

All entries describe implemented operations. Host quotas and cancellation apply in addition to the listed typed failures.

## File.Append.new

```text
File.Append.new(file_path: File.Path, file_content: Bytes) -> File.Append
```

Construct an inert append target from a path and Bytes. Commit appends the bytes or creates an absent file. Missing parent directories are not created.

A successful bound commit returns `File.AppendReceipt` with `path` and `bytes_written`.

**Failures:** Construction is total for correctly typed arguments. Commit can produce `File.AppendFailed`. Diagnostics keep confirmed byte counts after a partial write.

**Authority:** Pure construction. Commit requires a matching `--write-root` grant.

**Example:**

```lo
return File.Append.new(File.Path.new("_scratch/example-output.txt"), UTF8.Bytes.new("next line\n"))
```

[Contract](../../specification.md#91-file-adapter)

## File.Path.eq

```text
File.Path.eq(left: File.Path, right: File.Path) -> Bool
```

Compare the two path texts exactly. Aliases, repeated separators, and dot components do not receive special treatment.

**Failures:** None for correctly typed arguments.

**Authority:** Pure comparison. No filesystem access.

**Example:**

```lo
return File.Path.eq(File.Path.new("a/b"), File.Path.new("a/./b"))
```

[Contract](../../specification.md#91-file-adapter)

## File.Path.join

```text
File.Path.join(path: File.Path, relative: String) -> File.Path!
```

Append relative text, with a slash when necessary. An empty relative value returns the original base text. Dot components remain unchanged.

**Failures:** `File.InvalidPath` if either operand contains NUL or the relative operand starts with `/`.

**Authority:** Pure text operation. The result grants no filesystem access.

**Example:**

```lo
return File.Path.join(File.Path.new("examples"), "list_sum.lo")
```

[Contract](../value-processors.md#lexical-paths)

## File.Path.name

```text
File.Path.name(path: File.Path) -> String
```

Return the last lexical component after removal of terminal separators. An empty or all-separator path has an empty name.

**Failures:** None for a correctly typed argument.

**Authority:** Pure text operation. No filesystem access.

**Example:**

```lo
return File.Path.name(File.Path.new("a/b/"))
```

[Contract](../value-processors.md#lexical-paths)

## File.Path.new

```text
File.Path.new(file_path: String) -> File.Path
```

Construct an immutable path value from exact text. Construction does not normalize the path or examine the filesystem.

**Failures:** None for a correctly typed argument. A later filesystem operation applies its path and authority checks.

**Authority:** Pure construction. The path value grants no filesystem access.

**Example:**

```lo
return File.Path.new("examples/list_sum.lo")
```

[Contract](../../specification.md#91-file-adapter)

## File.Path.parent

```text
File.Path.parent(path: File.Path) -> File.Path
```

Remove terminal separators, then return the lexical parent. A relative singleton has an empty parent. An all-separator path has parent `/`.

**Failures:** None for a correctly typed argument.

**Authority:** Pure text operation. No filesystem access.

**Example:**

```lo
return File.Path.parent(File.Path.new("a//b/"))
```

[Contract](../value-processors.md#lexical-paths)

## File.Path.to_string

```text
File.Path.to_string(path: File.Path) -> String
```

Return the exact text in the path value.

**Failures:** None for a correctly typed argument.

**Authority:** Pure projection. No filesystem access.

**Example:**

```lo
return File.Path.to_string(File.Path.new("a/../b"))
```

[Contract](../value-processors.md#lexical-paths)

## File.Remove.checked

```text
File.Remove.checked(entries: List<File.Entry>, context..., Eligible) -> File.Remove!
```

Construct a removal target with immutable context and a named guard. `Eligible(context..., original: File.Entry, current: File.Entry) -> Bool` must be pure and total.

Construction invokes no guard. During commit, mandatory identity checks precede each guard call. The guard runs during preflight and immediately before each reached removal.

A successful nonempty batch invokes the guard twice per entry. An empty batch invokes it zero times.

**Failures:** `File.InvalidSelection` during construction. Commit can produce `File.EntryChanged` or `File.RemoveFailed`. A false guard produces `File.EntryChanged`.

Invalid guard types or effects fail compiler checks. A late removal failure preserves confirmed progress without rollback.

**Authority:** Pure construction. A nonempty commit requires the exact `--remove-root` grant and valid session and directory provenance.

An empty valid batch requires no removal grant.

**Example:**

```lo
flow Eligible(original: File.Entry, current: File.Entry) -> Bool {
    return Math.eq(original.owner_uid, current.owner_uid)
}
flow Build() -> File.Remove! {
    return File.Remove.checked(List.new(), Eligible)
}
return Build()
```

[Contract](../../specification.md#91-file-adapter)

## File.Remove.new

```text
File.Remove.new(entries: List<File.Entry>) -> File.Remove!
```

Construct an inert removal target for regular entries from one session and observed directory. Duplicate names, foreign entries, and other kinds are invalid.

Commit checks the whole batch before mutation, then checks each entry again before removal. The receipt contains `total_count`, `completed_count`, and `observed_bytes`.

An empty typed selection is valid. Observed bytes measure logical file sizes, not reclaimed storage.

**Failures:** `File.InvalidSelection` during construction. Commit can produce `File.EntryChanged` or `File.RemoveFailed`. A late failure retains confirmed progress.

Removal is not atomic and has no rollback. The final identity check and removal remain separate operations.

**Authority:** Pure construction. A nonempty commit requires the exact `--remove-root` grant and valid entry provenance. A listing grant alone does not authorize removal.

An empty valid batch requires no removal grant.

**Example:**

```lo
flow EmptyEntries() -> List<File.Entry> {
    return List.new()
}
return File.Remove.new(EmptyEntries())
```

[Contract](../../specification.md#91-file-adapter)

## File.Write.new

```text
File.Write.new(file_path: File.Path, file_content: Bytes) -> File.Write
```

Construct an inert replacement target from a path and Bytes. Commit creates or replaces the file. Missing parent directories are not created.

A successful bound commit returns `File.WriteReceipt` with `path` and `bytes_written`.

**Failures:** Construction is total for correctly typed arguments. Commit can produce `File.WriteFailed`. A failed write can leave partial contents.

Host denial, cancellation, limits, and unresolved cleanup remain terminal diagnostics. Completed output does not roll back.

**Authority:** Pure construction. Commit requires a matching `--write-root` grant.

**Example:**

```lo
return File.Write.new(File.Path.new("_scratch/example-output.txt"), UTF8.Bytes.new("hello"))
```

[Contract](../../specification.md#91-file-adapter)

## File.directory

```text
File.directory(root: File.Path) -> File.Directory!
```

Observe metadata for the exact root directory. The result exposes its path, owner UID, and access, modification, and change timestamps.

**Failures:** `File.DirectoryAbsent` or `File.DirectoryFailed`. An authority failure remains a terminal host diagnostic.

**Authority:** Host observation. Execution requires an exact `--list-root` grant. A directory observation cannot become a removal selection.

**Example:**

```lo
return File.directory(File.Path.new("examples"))
```

[Contract](../../specification.md#91-file-adapter)

## File.list

```text
File.list(root: File.Path) -> List<File.Entry>!
```

Observe the direct children of one directory. The result contains immutable entries sorted by name. The operation does not recurse or read file contents.

Each entry has a path, name, kind, logical size, link count, owner UID, and access, modification, and change timestamps.

**Failures:** `File.DirectoryAbsent`, `File.ListFailed`, or `File.InvalidName`. A child that disappears during observation makes this strict operation fail.

**Authority:** Host observation. Execution requires an exact `--list-root` grant. Entries retain session and directory provenance.

**Example:**

```lo
return File.list(File.Path.new("examples"))
```

[Contract](../../specification.md#91-file-adapter)

## File.open_paths

```text
File.open_paths(root: File.Path) -> List<File.Path>!
```

Observe open paths with the fixed `lsof -Fn +D .` probe. The result contains sorted distinct paths. An observation does not establish that a path remains open or closed.

The probe accepts status 0 or 1 with stripped-empty stderr and valid records. Each output stream has an 8 MiB bound.

**Failures:** `File.OpenFilesFailed` for invalid output or launch failure. Backslashes and control characters in names are invalid for this line-framed protocol.

Output overflow, cancellation, and work limits remain terminal host diagnostics.

**Authority:** Host observation. Execution requires the separate exact `--open-files-root` grant and an available `lsof` executable.

**Example:**

```lo
return File.open_paths(File.Path.new("examples"))
```

[Contract](../../specification.md#91-file-adapter)

## File.read

```text
File.read(file_path: File.Path) -> Bytes!
```

Read file contents as Bytes. Text decoding is a separate operation with `UTF8.String.new`. Relative paths resolve from the invocation directory.

**Failures:** `File.ReadFailed` for an authorized file that cannot supply its contents. Host denial, cancellation, and resource limits remain terminal diagnostics.

**Authority:** Host observation. Execution requires a matching `--read-root` grant. This grant does not grant removal authority.

**Example:**

```lo
return File.read(File.Path.new("examples/list_sum.lo"))
```

[Contract](../../specification.md#91-file-adapter)

## File.scan

```text
File.scan(root: File.Path) -> File.Scan!
```

Observe a directory with separate entries and vanished names. The result exposes `directory`, `entries`, and `vanished_names`.

A vanished name records only a child that disappears before metadata lookup. Entries and vanished names are disjoint and sorted.

**Failures:** `File.DirectoryAbsent`, `File.ListFailed`, or `File.InvalidName`. Other discovery errors stop the scan without a partial result.

**Authority:** Host observation. Execution requires an exact `--list-root` grant. The observation does not describe an atomic filesystem snapshot.

**Example:**

```lo
return File.scan(File.Path.new("examples"))
```

[Contract](../../specification.md#91-file-adapter)
