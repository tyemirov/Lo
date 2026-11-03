# Console, Terminal Results, And Declared CLI Options

Status: implemented contract for F026, F027, and F028.
Focused compiled-program checks pass on Darwin.
Linux checks pass under QEMU TCG.
Metal-linked artifacts pass the generic CLI checks without GPU work.
Final local `make ci` passed.
The [completion record](evidence/cli-runtime/checkpoint.json) contains the results.

## Values And Operations

| Operation | Result | Contract |
| --- | --- | --- |
| `Console.Line.new(stream: String, text: String, flush: Bool)` | `Console.Line!` | Pure construction of an inert nominal target. Only stdout and stderr are valid destinations. |
| Bound Console commit | `Console.Receipt` | Publish the selected destination and logical byte count after the complete write and requested flush. |
| `Program.Exit.new(code: Int, stdout: Bytes, stderr: Bytes)` | `Program.Exit!` | Pure construction of an inert terminal value with status from 0 through 255. |
| `CLI.parse(arguments, flags, name, description, abbreviations)` | `CLI.Options!` | Pure declared Boolean option parsing with the exact inputs below. |

Console lines contain UTF-8 text followed by exactly one LF byte.
Console receipts expose `stream: String`, `bytes_written: Int`, and `flushed: Bool`.
One line, including LF, has at most 1048576 bytes.
Terminal stdout and stderr have a combined maximum of 1048576 bytes.

Invalid Console inputs produce `Console.InvalidLine`. Invalid terminal inputs produce `Program.InvalidExit`.
Automatic target/value output and diagnostic snapshots show metadata without text or terminal payload bytes.
Console metadata includes destination, flush selection, and byte count.
Terminal metadata includes requested status and payload lengths.

Terminal values retain the original byte values through resource traversal.
Shared aliases keep one retained identity while expanded value weight includes each payload position.

Only a final run result selects terminal behavior.
Nested terminal values remain data. An ordinary returned Int remains an ordinary result.
Serve rejects a terminal entry result explicitly before program evaluation.
Embeddings retain inert values until the supported completion API joins owned resources and renders the terminal result.
No embedding API exits its caller's process.

## Output Ownership

The host supplies enabled destinations and cooperative output sinks.
Each sink accepts context-bound writes and flushes with confirmed partial progress.
Missing enabled destinations fail with explicit host authority errors.
Console construction grants no authority and starts no output worker.
CLI run and serve supply their inherited stdout/stderr as enabled destinations.
Embedding hosts register sinks explicitly before evaluation.

Each native CLI invocation starts with fresh signal cancellation state.

The default cumulative logical-output quota is 1048576 bytes per host session.
`--max-console-bytes` selects a uint64 byte quota. Zero disables that quota.

Each admitted attempt charges its declared logical bytes before the first output action.
Terminal output reserves both payloads before either destination receives bytes.
Counter overflow and quota exhaustion are terminal `runtime.limit` failures.
The per-record size bound remains active when the cumulative quota is disabled.

Commits serialize complete output records within a host session.
There is no external stdout/stderr merge-order guarantee.
Cancelled lock acquisition and blocked output must return without an abandoned worker.
Confirmed earlier output remains visible after later failure.

Write and flush errors produce recoverable `Console.WriteFailed` only after output ownership resolves.
Its payload contains destination, confirmed encoded bytes, and failure stage without output text.
Cancellation, uncertain ownership, and cleanup failure remain terminal.

## CLI Writer Process

CLI output uses a trusted owned writer process instead of changing inherited descriptor status flags.
A duplicated descriptor does not isolate those flags.

The CLI writer requires a retained executable path.
The host binds and protects the worker image before program evaluation.
Participating File mutations must not replace that image or modify it through an alias.
The worker accepts only its bounded output protocol and inherited console destinations.
It has no command execution or filesystem mutation interface.

The private entry argument is `--lo-console-writer`.
The worker receives its request through stdin, acknowledgments through descriptor 3, and parent liveness through descriptor 4.
The parent owns the liveness pipe's write endpoint and sends no data through it.
The worker exits when that endpoint closes, including while another worker thread blocks on console output.

The native host serializes channel creation with participating File probe and Process startup.
Other participating children cannot inherit the Console liveness endpoint during channel creation.
The gate preserves cancellation and the existing Process deadline during queued startup.
The host releases the gate before it waits for child I/O.

The parent kills and joins the writer on cancellation or protocol failure.
It reports only acknowledged byte prefixes. An uncertain unacknowledged tail never becomes successful output.

Each request frame is `LCW2`, one destination byte, one big-endian uint32 body length, and the exact output bytes.
Destination 1 is stdout. Destination 2 is stderr. Other values are invalid.
Each encoded frame body has a 2097152-byte maximum.

The worker accepts successive frames until request EOF.
A zero-length frame receives a completion acknowledgment with zero confirmed bytes.

Acknowledgments contain one kind byte, a big-endian uint64 confirmed byte count, and one closed error-class byte.
Kind 1 reports progress. Kind 2 reports completion. Kind 3 reports write failure.
Error class 0 is none, 1 is a closed pipe, and 2 is another write failure.
Confirmed byte counts apply to the current frame.

Malformed frames, inconsistent counts, early EOF, and unsuccessful worker exit fail explicitly.
The parent joins worker ownership before returning from each output operation.

Final result and diagnostic writers also use cancellable output ownership.
Cancellation cannot be followed by an unbounded write to the same blocked stderr destination.
Failure reporting uses its own bounded reporting context and keeps a failing process status when output cannot complete.
It never substitutes another output destination.

Serve readiness and stopped messages each use an independent one-second completion context.
After message output, a stop signal does not cancel the owned flush.
Blocked protocol output still fails at its completion deadline.

## Text And JSON Rendering

Text mode writes exact line or terminal bytes.
JSON mode emits one console event per record on the selected destination.
The event contains `command`, `event: "console"`, `stream`, `encoding: "base64"`, `data`, `bytes`, and `flush`.
`flush` describes the requested operation. Only the completed receipt confirms a successful flush.
`data` encodes the complete logical byte payload, including a Console line's LF.

`bytes` counts logical bytes. Partial failure progress counts encoded bytes actually acknowledged by the sink.
The final normal result keeps its result envelope.
A terminal result emits trailing stdout then stderr events, followed by a final envelope with `exit_code`.
Successful status 1 or 2 is distinct from execution failure.

Program/File/Process cleanup and retained ownership joins precede terminal output.
Console ownership remains available until that output completes.
Output or cleanup failure overrides the requested application status.
The native CLI must not use numeric status zero as a proxy for successful execution.

## Declared Options

`arguments` is `List<String>`.
`flags` is `List<Record{name: String, aliases: List<String>, group: String, help: String}>`.
`name` and `description` use the String type. `abbreviations` is Bool.
`CLI.Options` exposes `flags: Map<String, Bool>`, `help_requested: Bool`, and `help_text: String`.
Every declared canonical name is present, initially false, in declaration order.

The result retains no raw argument vector.

Option spellings use one ASCII letter or digit after `-`, or ASCII letters/digits and interior hyphens after `--`.
Long spellings cannot end with a hyphen. Metadata rejects NUL and control characters.
Canonical names and aliases must be unique. `-h` and `--help` are reserved.

Repeated selection of one canonical option is idempotent.
Distinct selected options in the same nonempty group conflict.
Exact spelling precedes abbreviation lookup.
Long-option prefixes are enabled only by `abbreviations` and must select one canonical option after alias deduplication.
Short bundles, `=value`, operands, bare `--`, unknown options, and ambiguous prefixes fail.

Declarations are validated before argument processing.
Tokens are processed left to right.
Recognized help returns immediately and ignores later tokens. Earlier argument failures remain failures.
Invalid declarations produce `CLI.InvalidDefinition`.
Invalid arguments produce `CLI.InvalidArguments`.

Parser-generated messages and operation snapshots omit the raw argument vector.
Failure metadata can contain a reason, argument index, and declared canonical candidates.
Declaration, argument, and prefix scans charge normal execution steps and observe cancellation.

Help has this exact shape, with LF terminators and fixed two-space separators:

```text
Usage: NAME [OPTIONS]

DESCRIPTION

Options:
  -h, --help  Show this help message and exit.
  CANONICAL, ALIAS  HELP
```

Declared rows follow the reserved help row in declaration order.
Aliases follow their canonical spelling in declared order.
Empty descriptions omit the description paragraph. Empty flag lists retain the reserved help row.
The parser does not query terminal width or create every possible prefix in advance.

## Startup And Acceptance

The terminal constructor is fallible.
Pure total error handlers return structured status/message data, not an unhandled fallible terminal construction.
An outer reporting flow constructs the final terminal result.
Help and invalid arguments complete before cleanup locks, probes, cleaners, or file removal.

Public acceptance covers constructors, commits, snapshots, failure progress, cancellation, pipes, limits, and self-image protection.
It covers requested statuses 0, 1, and 2 with output and retained-cleanup failure precedence.
It covers every parser rule, malformed inputs, work limits, and deterministic help.
The same small CLI example runs as compiled artifacts on Darwin and Linux under QEMU TCG.
Metal-linked artifacts exercise these generic operations without GPU work.

The [executable example](examples/cli-cleanup-preview.lo) contains only application mode selection and output.
Its pure startup handler returns status and message data.
The outer flow constructs the fallible terminal result.
The [native embedding guide](native-embedding.md) defines explicit sink registration, value ownership, diagnostics, and completion.
