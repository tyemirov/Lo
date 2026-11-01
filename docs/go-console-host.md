# Go Console Host

The Go host API connects Console activation to explicit output sinks.
The [selected CLI contract](cli-runtime-design.md) owns language behavior and the `LCW1` byte framing.
This guide describes the Go API and its resource ownership.

## Output Sinks

`adapter.ConsoleSink` has this interface:

```go
type ConsoleSink interface {
    Write(context.Context, []byte) (int, error)
    Flush(context.Context) error
}
```

Each method must return when its context ends.
`Write` reports only the byte prefix that the sink accepts.
The count must be from zero through the supplied byte count.
The host continues a partial write without an error.
A zero-byte write without an error fails the record.
The host calls `Flush` only when the record requests it.

`adapter.OpenConsole(options)` constructs one output owner.
The `ConsoleOptions.Stdout` and `ConsoleOptions.Stderr` fields select the enabled destinations.
A nil sink gives no authority for that destination.
`Format` must select `adapter.ConsoleText` or `adapter.ConsoleJSON`.
`MaxBytes` selects the cumulative logical-byte quota.
Zero disables that quota.

`adapter.NewBufferConsoleSink(buffer)` supplies an output sink for a concrete `*bytes.Buffer`.
It rejects a nil buffer.
The API does not accept a general `io.Writer` as an output sink with context control.
A host must supply the context behavior of any external output sink.
The adapter does not start a separate goroutine around that sink.

## Program Execution

The runner accepts an output owner through these entry points:

```go
runner.Execute(program, arguments, console)
runner.Run(options, program, console)
runner.RunContext(ctx, options, program, factory, observer, console)
```

The runner selects the command and format from the parsed invocation.
It takes ownership of the supplied Console host for that invocation.
It returns a status after output and resource cleanup finish.
These APIs do not exit the caller's process.
`runner.Main` is the process entry point that exits with the returned status.

The following example supplies two separate memory buffers:

```go
var stdout, stderr bytes.Buffer
out, err := adapter.NewBufferConsoleSink(&stdout)
if err != nil {
    return err
}
errout, err := adapter.NewBufferConsoleSink(&stderr)
if err != nil {
    return err
}
console, err := adapter.OpenConsole(adapter.ConsoleOptions{
    Stdout: out,
    Stderr: errout,
    Format: adapter.ConsoleJSON,
    MaxBytes: adapter.DefaultConsoleBytes,
})
if err != nil {
    return err
}
status := runner.Execute(program, []string{"run", "--format", "json"}, console)
```

`execution.NewProgram(body, handlers, resultType)` requires explicit entry metadata.
A nil `resultType` declares an entry with no result.
A trusted Go callback must return the declared type.
The runtime rejects a successful result that differs from that declaration.

`eval.Program` obtains this metadata from the checked graph.
The runner rejects a declared `Program.Exit` entry for `serve` before evaluation.
The same rule applies when the declared entry type is fallible.

## Completion And Failure

`console.Wrap(boundary)` adds Console activation to an existing adapter boundary.
It preserves File observations and delegates other targets to that boundary.
The host serializes complete records across both destinations.
Context cancellation also stops a caller that waits for another record.

`console.Write` returns the known encoded-byte count for one record.
`console.WriteTerminal` reserves both terminal payloads before either output action.
It validates authority for both nonempty payloads first.
It writes stdout before stderr and requests a flush for each payload.
The runner calls this API only after Program, File, and Process cleanup finish.
The runner also joins retained Process ownership before terminal output.

`Console.WriteFailed` contains the destination, known encoded-byte count, and failure stage.
Its stage is `write` or `flush`.
The failure contains no output text or external error message.
Cancellation, quota failure, invalid sink progress, and unresolved output ownership remain terminal host failures.
An output failure overrides a requested application status.
A cleanup failure also overrides that status.

`console.Writer(ctx, stream)` supplies a writer for final results and diagnostics.
It uses the same output owner without a second application-byte charge.
It requests a flush after each rendered output write.
The runner gives failure diagnostics a separate context with a timeout of one second.
Thus a cancelled stderr write cannot cause an unlimited diagnostic write.

Serve readiness and stopped messages each use an independent one-second completion context.
A graceful stop after readiness output preserves its owned flush.

`console.Close(ctx)` cancels admitted operations and bounds the caller's wait.
`console.WaitRetained()` joins the same cleanup owner without a new cleanup attempt.
The runner calls both methods before it returns.
A host that uses the direct Console API must close and join its owner.

## CLI Writer Process

The CLI constructs its host with `adapter.OpenCLIConsole(ctx, options, stdout, stderr)`.
Both destinations are inherited `*os.File` values.
The host binds its own executable before program evaluation.
The Process session protects that image without an additional execution grant.
Participating File mutations cannot replace the image or change it through an alias.
The native Process profile also excludes that image from mutation roots.

The CLI dispatches `adapter.HandleConsoleWriter(arguments)` before its normal argument parser.
The private argument is `--lo-console-writer`.
Other Go executables must supply this dispatch explicitly before they select the CLI host.
The writer accepts bounded output bytes, destination selection, and parent liveness.
It has no command execution or file mutation operation.

The request uses stdin, and response records use file descriptor 3.
File descriptor 4 observes parent liveness through a pipe.
The parent keeps that pipe open until the operation ends.

The worker exits on pipe closure, even when its console write blocks.
Cancellation kills the worker and joins its final process status.
The parent accepts only reported byte prefixes.
The host does not change inherited descriptor status flags.

## Limits And Output

| Limit | Value |
| --- | --- |
| Default cumulative logical bytes | 1048576 |
| One Console record, including LF | 1048576 |
| Combined terminal payloads | 1048576 |
| One encoded worker body | 2097152 |

`--max-console-bytes UINT64` selects the CLI cumulative quota for `run` and `serve`.
Zero disables only the cumulative quota.
Each admitted Console attempt charges its complete logical length before output.
Terminal output charges both payloads before either destination receives bytes.
Automatic result envelopes and diagnostics do not consume this application quota.

Text output preserves the exact application bytes.
A Console line adds exactly one LF.
JSON output uses one Console event for each record on its selected destination.
The event encodes the logical bytes with Base64.
For example, the following event represents `hi` and one LF:

```json
{"command":"run","event":"console","stream":"stdout","encoding":"base64","data":"aGkK","bytes":3,"flush":true}
```

The event's `flush` field records the requested mode.
The successful receipt's `flushed` field records flush completion.
The final ordinary result keeps its result envelope.
A terminal result ends with a JSON envelope that contains `exit_code`.
Application statuses 1 and 2 can have `status: "ok"`.
Separate stdout and stderr destinations have no guarantee for their combined external order.
