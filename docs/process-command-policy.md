# Process Command Policy

F035 adds explicit command duration and output selection.
The host supplies the limits and the output destinations.
Application flows supply command arguments and decide how to handle a completed timeout.

## Command Values

```lo
flow Configure(command: Process.Command) -> Process.Command! {
    timed = Process.Command.with_timeout(command, 30000)
    return Process.Command.with_output(timed, "forward")
}
```

Both operations are pure and return new immutable commands.
They preserve the original context, executable, arguments, and input bytes.
Inspection preserves those inputs and the requested policy through each transformation.

`Process.Command.with_timeout(command, timeout_ms)` accepts a positive signed 64-bit count of milliseconds.
An invalid duration produces `Process.InvalidCommand`.
Construction does not read a clock or obtain host authority.
At activation, the request must fit the host limit.
An excessive request produces terminal `runtime.capability-denied` before command launch.
The host does not shorten that request.

`Process.Command.with_output(command, mode)` accepts exactly `"capture"` or `"forward"`.
Any other string produces `Process.InvalidCommand`.
`Process.Command.new` selects capture and the host duration limit.
Its serialized `timeout_ms` field contains zero for that default.
An explicit timeout always contains a positive value.

## Host Limits

The program CLI accepts these options once each.

| Option | Default | Contract |
| --- | --- | --- |
| `--max-process-ms` | `5000` | Maximum command duration in milliseconds. |
| `--max-process-cleanup-ms` | `1000` | Maximum caller wait for process cleanup after a stop. |
| `--max-process-output-bytes` | `1048576` | Maximum confirmed bytes in each output stream. |
| `--process-forward-output` | Absent | Authority to forward stdout and stderr through Console. |

The numeric limits must be positive and representable by the native host.
The host validates duration arithmetic before execution.
These options do not supply executable, directory, mutation, or endpoint authority.
The existing Process grants remain necessary.
Forward output also requires both Console destinations.

## Output And Results

Capture keeps stdout and stderr in separate bounded byte arrays.
Forward output sends each stream through the host Console output sink.
The command receives pipes for its output.
It does not receive unrelated host file descriptors.

Forward output consumes both the per-stream Process limit and the shared Console logical-byte limit.
The runtime also charges execution work and value weight.
Text output preserves the bytes.
JSON output uses the existing Console NDJSON records with Base64 data.
Each stream retains byte order.
No order between stdout and stderr is specified.

`Process.Result` contains these fields.

| Field | Type | Meaning |
| --- | --- | --- |
| `termination` | `Process.Termination` | Confirmed normal exit or signal termination. |
| `stdout` | `Bytes` | Captured stdout, or empty bytes for forward output. |
| `stderr` | `Bytes` | Captured stderr, or empty bytes for forward output. |
| `output` | `String` | The selected `"capture"` or `"forward"` value. |
| `stdout_bytes` | `Int` | Confirmed stdout bytes. |
| `stderr_bytes` | `Int` | Confirmed stderr bytes. |

Automatic diagnostic snapshots remove captured bytes and retain the counts.
Failure progress contains confirmed counts without child output payloads.
A partial sink failure retains the confirmed prefix count.
For JSON output, a complete Console record confirms its logical payload.

## Timeout And Cleanup

A command deadline covers process startup, command execution, and output delivery.
`Process.Timeout` is recoverable only after the owner confirms process termination and complete resource release.
A subsequent command can then start.
Caller cancellation, quota exhaustion, output sink failure, and cleanup failure remain terminal.
Unknown ownership also remains terminal.

If cleanup exceeds the host wait limit, the runtime retains ownership and rejects subsequent commands.
The owner continues cleanup after the caller stops its wait.
The session cannot release resources with an unknown disposition.
Confirmed filesystem and daemon effects remain after a timeout or cancellation.

## Native Embedding

`runtime/native/include/lo_process.h` owns the Process driver ABI.
`LoProcessLimits` supplies trusted host limits.
`LoProcessRequest` supplies the admitted command and output callback.
`LoProcessResult` supplies termination, progress, and cleanup evidence.
The launcher configuration remains separate from command arguments and environment values.
The runtime applies duration and output policy.

Install the driver and its limits before evaluation and before sharing the session.
Registration stays closed after any context in that session evaluates.
Freeing an evaluated peer does not reopen registration.

The synchronous driver owns the command until it returns.
Each output callback supplies 1 through 4096 bytes.
The runtime sends bounded output records to the context owner thread.
That thread invokes the Console callback and confirms the accepted bytes.
After publication, the driver waits for that confirmation or channel closure.

Concurrent output calls are serialized through confirmation.
The provider must preserve byte order within each stream and join its output threads before return.
The runtime retains the first output rejection even if the provider ignores it.
If the cleanup wait expires, the failure retains all confirmed output counts.
Provider code and userdata must remain valid until all retained session ownership ends.

F035 preserves the current descendant denial.
It does not add a Linux command execution profile.
Injected driver fixtures exercise the ABI on Darwin arm64 and Linux arm64 under QEMU.
Real command fixtures exercise the selected Darwin profile.
The [descendant profile](process-descendant-ownership.md) defines the separate F036 host selection and its lifecycle provider.
It uses the same command duration, output, and cleanup policy.
