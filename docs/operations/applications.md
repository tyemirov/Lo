# Application Operations

[Documentation](../README.md) / [Operation index](README.md)

Application operations cover invocation inputs, declared options, console targets, terminal results, and file locks.
Construction, observation, activation, and final completion remain separate steps.

All entries describe implemented operations. Host quotas and cancellation apply in addition to the listed typed failures.

## CLI.parse

```text
CLI.parse(arguments: List<String>, flags: List<Record{aliases: List<String>, group: String, help: String, name: String}>, name: String, description: String, abbreviations: Bool) -> CLI.Options!
```

Parse declared Boolean flags into `CLI.Options`. The result exposes `flags`, `help_requested`, and deterministic `help_text`. Canonical flags start false.

Aliases select the canonical name. Repeated selection keeps the flag true. Distinct flags in one nonempty group conflict. Optional long-option abbreviations must be unambiguous.

Definitions are validated first. `-h` or `--help` stops token processing immediately. Operands, bare `--`, short bundles, and assigned values are rejected.

**Failures:** `CLI.InvalidDefinition` for an invalid declaration. `CLI.InvalidArguments` for invalid arguments. Resource limits and cancellation remain terminal host diagnostics.

**Authority:** Pure parsing of supplied values. Invocation argument capture is a separate host observation.

**Example:**

```lo
return CLI.parse(
    List.new("--verbose"),
    List.new(Record.new(name: "--verbose", aliases: List.new("-v"), group: "", help: "Show detail")),
    "example", "Example program", false
)
```

[Contract](../../specification.md#97-console-output-terminal-results-and-declared-options)

## Clock.now_unix_ns

```text
Clock.now_unix_ns() -> Int!
```

Observe fresh Unix time in nanoseconds. Negative timestamps are valid. The result must fit a signed 64-bit integer.
This operation does not change the timestamp captured in `Host.Invocation.current()`.

**Failures:** `Clock.Failed` for a clock error or an out-of-range timestamp.

**Authority:** Host observation. No explicit clock grant or commit is necessary. Pure callbacks cannot call this operation.

```lo
return Clock.now_unix_ns()
```

[Contract](../file-system-observations.md#fresh-time)

## Console.Line.new

```text
Console.Line.new(stream: String, text: String, flush: Bool) -> Console.Line!
```

Construct an inert line target for `stdout` or `stderr`. UTF-8 text plus one appended LF byte must fit within 1048576 bytes.

Commit writes the complete record and performs the requested flush. The receipt contains `stream`, `bytes_written`, and `flushed`. The byte count includes LF.

**Failures:** `Console.InvalidLine` during construction. Commit can produce `Console.WriteFailed` with the confirmed byte count and the write or flush stage.

Cancellation, quota exhaustion, invalid sink progress, and unresolved ownership remain terminal diagnostics. Earlier confirmed output remains visible.

**Authority:** Pure construction. Commit requires an enabled host output sink. The CLI enables stdout and stderr with a default 1048576-byte cumulative quota.

**Example:**

```lo
return Console.Line.new("stdout", "hello", true)
```

[Contract](../../specification.md#97-console-output-terminal-results-and-declared-options)

## Host.Invocation.current

```text
Host.Invocation.current() -> Host.Invocation!
```

Return arguments, home, real UID, effective UID, and the clock captured before program evaluation. Projections from the result are pure.

Arguments contain only values after the program command's explicit `--` separator. Repeated calls and HTTP callbacks use the same initial snapshot.

The clock is an initial timestamp, not a fresh clock observation. Automatic output redacts home and argument values.

**Failures:** `Host.InvocationFailed` when the captured input is unavailable or invalid. A program that never observes the snapshot does not report that capture failure.

**Authority:** Host observation, even for previously captured data. The snapshot grants no File or Process authority.

**Example:**

```lo
flow Main() -> Int! {
    initial = Host.Invocation.current()
    return List.length(initial.arguments)
}
return Main()
```

[Contract](../../specification.md#96-host-invocation)

## Lock.File.new

```text
Lock.File.new(path: File.Path) -> Lock.File
```

Construct an inert exclusive file-lock target. Commit acquires without a wait and returns an opaque `Lock.Lease`. Repeated acquisition can contend within one session.

The session retains the lock through owned cleanup. Value disposal cannot release the lease. There is no public release operation.

A lock applies to the file object and its hardlinks. It does not prevent an external pathname replacement.

**Failures:** Construction is total for a correctly typed path. Commit can produce `Lock.Busy` or `Lock.Failed` after resolved cleanup.

Missing authority, cancellation, resource limits, and cleanup failures remain terminal host diagnostics.

**Authority:** Pure construction. Commit requires an exact `--lock-file` grant or `--lock-entry`. Grants reject symlink traversal and require an existing regular file.

**Example:**

```lo
return Lock.File.new(File.Path.new("examples/list_sum.lo"))
```

[Contract](../../specification.md#98-program-entry-and-file-locks)

## Program.Exit.new

```text
Program.Exit.new(code: Int, stdout: Bytes, stderr: Bytes) -> Program.Exit!
```

Construct terminal data with status `0..255` and explicit stdout and stderr Bytes. The combined payload bound is 1048576 bytes.

Only a final run result selects terminal behavior. Nested terminal values remain data. The host completes cleanup before terminal output and status publication.

An ordinary returned Int remains a result value. Serve rejects a terminal entry type before evaluation.

**Failures:** `Program.InvalidExit` for an invalid status or payload size. Output, cleanup, cancellation, or execution failure overrides the requested status.

**Authority:** Pure construction. Final run completion requires enabled host output destinations. The value does not terminate an embedding host process.

**Example:**

```lo
return Program.Exit.new(0, UTF8.Bytes.new("done\n"), Bytes.new(""))
```

[Contract](../../specification.md#97-console-output-terminal-results-and-declared-options)

## Program.path

```text
Program.path() -> File.Path!
```

Observe the captured executable entry path. The runtime uses operating-system identity checks, not `argv[0]` or embedded source metadata. The captured path remains immutable.

**Failures:** `Program.EntryUnavailable` if entry identity is unavailable. An embedding must supply explicit identity before this observation.

**Authority:** Host observation. The path grants no file access or lock authority.

**Example:**

```lo
return Program.path()
```

[Contract](../../specification.md#98-program-entry-and-file-locks)
