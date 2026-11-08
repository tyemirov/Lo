# Lo Adapter Design

Status: shared adapter contracts, Process results, executable registration, and the native host are implemented.
The selected Darwin 27 arm64 profile passed [native qualification](evidence/process-host-native.json).
Canonical public acceptance passed all 24 interpreter/generated-native cases, including server callbacks and Process-to-File composition.
Final stack CI and independent completion review passed.
The [final validation record](evidence/process-enabled-validation.json) preserves the CI log and review outcome.
Earlier injected checkpoints passed CI and independent review.
Implementation and final stack CI passed. Independent completion review passed.

This document owns the shared rules and selected domain contracts below.
The [language reference](../specification.md) continues to describe the implemented language.
The [implementation plan](adapter-plan.md) defines the change sequence and acceptance criteria.

All design work, implementation, fixtures, and validation stay inside Lo.
The [repository boundary](../.mprlab/POLICY.md#lo-repository-boundary) applies to this entire plan.

## Contents

- [Foundation](#foundation)
- [Shared operation contract](#shared-operation-contract)
- [Observations and purity](#observations-and-purity)
- [Activation and results](#activation-and-results)
- [Callbacks and higher-order operations](#callbacks-and-higher-order-operations)
- [File](#file)
- [Host invocation](#host-invocation)
- [HTTP](#http)
- [Process](#process)
- [Inspection and execution](#inspection-and-execution)
- [Current implementation and required changes](#current-implementation-and-required-changes)
- [Open decisions](#open-decisions)

## Foundation

Lo describes typed executable plans.
An adapter connects those plans to one external domain.
Its public operations keep explicit data, immutable values, named flows, fallibility, and host authorization.
The same rules apply to simple operations and higher-order operations.

File, HTTP, and Process have separate domain types.
They share language rules without an `IO` parent, inheritance, or mandatory identical method names.
The public domain is independent of the internal host mechanism.
A File observation can use a private subprocess without exposing general process execution.

An adapter operation must have a stable domain meaning beyond one application.
Retention periods, selected filenames, HTTP status policy, and accepted command exit codes belong in Lo flows.
The adapter owns boundary validation, resource access, and its documented execution guarantees.

```mermaid
flowchart LR
    Values["Immutable values"] --> Describe["Construct an action"]
    Describe --> Activate["Explicit commit"]
    Grants["Host authority"] --> Activate
    Activate --> Result["Typed completion result"]
    Observe["Authorized observation"] --> Values
    Result --> Transform["Named flows and processors"]
    Transform --> Values
```

## Shared Operation Contract

Each public operation must specify these fields before implementation:

| Field | Required definition |
| --- | --- |
| Domain | The external resource or immutable data that the operation represents. |
| Input and output | Exact types, validation rules, and successful result. |
| Evaluation phase | Value construction, immediate observation, activation, or an external event. |
| Authority | Required host grants and checks on the actual resource. |
| Callback contract | Named flow signature, explicit context, phase, order, count, and authorized effects. |
| Completion | The event that establishes success. |
| Failure | Constructor errors, boundary errors, host failures, and confirmed partial progress. |
| Resources | Work, bytes, handles, lifetime, cancellation, and release rules. |
| Inspection | Immediate requirements, stored callback requirements, and calculated targets. |
| Backend contract | Observable behavior shared by interpreter and native execution. |

Construction validates supplied data without access to external state.
Each constructor establishes its result type's invariants.
A response description specifies intended protocol data, but does not guarantee acceptance at emission.
Descriptions do not contain authority that the program can create for itself.
The runtime checks each actual target against host grants when it accesses that target.
Inspection and successful type checking do not grant authority.

## Observations and Purity

Direct authorized observations remain part of Lo.
For example, `File.read` returns bytes and `File.list` returns entry observations.
An observation runs when evaluation reaches its call.
Reusing its bound result does not repeat the observation.
Separate calls can return different values.

The implemented pure-flow rule excludes host observations and commit activation, including transitive calls.
Fallibility is separate from purity.
A pure calculation can return an error, and an external observation can succeed.
Device requirements remain a separate compiler summary under the existing tensor and device contracts.
This rule does not remove deterministic tensor operations from List callbacks.

| Position | Implemented rule |
| --- | --- |
| List callback | Pure named flow. No host observations or commits. |
| Match predicate | Pure total named flow. |
| Match case head | Pure total value calculation. |
| Match selector or selected branch | Observations can occur. Commit activation is prohibited. |
| Flow self-handler | Pure total named flow. |
| Adapter `onError` | Pure total named flow. |
| `Flow.fold` callback | Observations and commits can occur under ordinary flow fallibility rules. |
| HTTP request handler | Observations and commits can occur under the declared handler signature. |

Match is a conditional expression.
A Match expression with an observation is not pure.
Its summary includes possible observations, but execution enters only the selected branch.
This distinction supports conditional reads without speculative access.

The compiler must keep observation order, invocation count, failure order, and evaluated branches.
It must not combine, repeat, or cache external observations as pure calculations.
In particular, two sequential callback passes are not equivalent to one interleaved pass when callbacks observe external state.
List representation and incremental realization remain runtime concerns.

## Activation and Results

Lo supports bound and unbound commit statements:

```text
result = commit target
commit target
```

Both forms are implemented for nominal File targets and `HTTP.Server`.
Both forms activate a previously bound committable target.
Commit remains a statement, with no expression form inside arguments, pipes, or Match branches.

Each nominal target type has one success type in the built-in catalog.
For example, `File.Write` produces `File.WriteReceipt`, and `HTTP.Server` produces `HTTP.ServerReceipt`.
Client targets produce `HTTP.Response`.
Selected future command targets produce `Process.Result`.
This relation works without a public generic `Effect<T>` or a `Unit` type.
Different target types do not become interchangeable because their success types match.

Each evaluated commit statement makes one activation attempt.
Activation waits for the target's defined completion event.
The bound form creates its immutable result binding only after success.
The unbound form discards the successful result.
Copies of a result do not repeat activation.
Another commit of the same description is another attempt.

Both forms keep commit failure semantics.
Inside a flow, a recoverable failure propagates to the declared flow boundary.
At top level, failure aborts execution before later statements.
A failed bound commit does not store `T!` and continue under ordinary top-level binding rules.
Host denial, cancellation, and resource exhaustion keep their host diagnostic behavior.

Success confirms only the operation's stated completion event.
A server-start result confirms startup, not service termination.
A missing HTTP response does not prove that the remote action failed to occur.
The runtime provides no automatic retry, rollback, or exactly-once delivery guarantee.
Cancellation does not reverse completed external work.

Partial progress belongs in structured failure evidence when no successful result exists.
The evidence must distinguish confirmed work from attempted work with an unknown outcome.
Cleanup failures must keep earlier progress and the original failure class.

Host write failures expose `write_progress` for the current partial operation.
Its fields are `bytes_written: Int` and `stage: String`.
`last_completed_activation` identifies only the latest completed activation in an invocation.
Its fields are `operation`, `span`, and `receipt`.
Diagnostic evidence does not create a result binding.

Cleanup cancellation must keep the original operation failure class.
Recoverable write errors keep `cleanup_errors: List<String>` in `Error.input`.
Host diagnostics expose `cleanup_errors` as a JSON array.

Recoverable fold failures keep reserved progress snapshots in `Error.flow_progress: List<Record>`.
Each failing fold prepends its record, so nested records run from outermost to innermost.
Equal record contents do not remove nested invocation records.
A pure handler that returns the same error keeps this metadata.
A fresh `Error.new` resets it to an empty list.
A handled fold cannot contaminate a later unrelated error.

The records retain no tensor storage or executable authority.
Their state remains explicit program data under ordinary snapshot rules.
Type-only automatic trace inputs do not replace that state.
Error JSON omits an empty `flow_progress` list.
CLI top-level diagnostics also expose recoverable carrier records.
If a value budget rejects an error value, the host diagnostic keeps its already reserved progress records.
Failed fallible values follow the same rule without publication of the rejected value.

## Callbacks and Higher-order Operations

High-level operations are valid adapter operations when their domain contract justifies them.
Names such as `map`, `fold`, and `apply` do not establish that contract by themselves.

| Candidate | Required meaning |
| --- | --- |
| Map over values | Transform each specified value through a pure named flow. Keep the stated order and failure behavior. |
| Map over action descriptions | Construct another inert description. Declare when the result callback executes and include its requirements in inspection. |
| Fold over external data | Define observation timing, input framing, resource lifetime, accumulator transitions, and partial completion. |
| Apply | State whether this is value transformation or action construction. External activation still occurs through commit. |
| External event handler | Declare event boundaries, per-event state, callback effects, and the mandatory boundary `onError`. |

A purely derived operation can remain an ordinary Lo flow.
An adapter-specific operation must have domain semantics that the existing composition does not already express.
Avoidance of list allocation alone does not justify a File fold.
Lo already supports incremental list realization when observable behavior is unchanged.

The first implementation adds no automatic `File.map`, `HTTP.fold`, or `Process.apply` family.
Future proposals use the shared operation contract and identify their other domain guarantee.
This is an admission rule for new operations, not a prohibition on higher-order adapters.
HTTP server handlers already demonstrate a domain-specific callback boundary.

Immediate callback execution and stored callbacks have separate summaries.
Construction of a server does not execute its handler.
Inspection still includes the handler's possible observations and activations.
An adapter that dispatches external events must have a total boundary `onError`.
Pure composition follows ordinary flow failure propagation without a second external event boundary.

## File

File owns filesystem observations and changes.
`File.Path` is a resource description, not a grant.
`File.Entry` is an immutable observation with private session and directory provenance.
Lists, records, and flow arguments keep that provenance.
Published metadata cannot reconstruct it.

Entries include owner UID, access time, and inode metadata-change time from one native metadata result.
Entries and directory observations also expose device, inode, and allocated bytes from that result.
Property projections are pure.
`File.Directory` retains root or descendant observations, and `File.Scan` reports entries and vanished names explicitly.
The strict listing operation requires complete discovery.

The selected File surface is:

| Operation or target | Evaluation | Result |
| --- | --- | --- |
| `File.Path.new(text)` and `File.Path.eq(a, b)` | Pure construction or comparison | Path or Bool. |
| `File.read(path)` | Immediate observation | `Bytes!`. |
| `File.list(root)` | Immediate observation | `List<File.Entry>!`. |
| `File.directory(root)` | Immediate observation | `File.Directory!` at an exact granted root. |
| `File.scan(root)` | Immediate observation | `File.Scan!` with entries and explicitly reported vanished names. |
| `File.Directory.refresh(directory)` | Immediate observation | Current retained `File.Directory!` metadata. |
| `File.Directory.scan(directory)` | Immediate observation | `File.Scan!` through retained authority. |
| `File.Directory.entry(directory, name)` | Immediate observation | One direct `File.Entry!`. |
| `File.Directory.child(directory, entry)` | Immediate observation | Retained child `File.Directory!` under recursive authority. |
| `File.Entry.observe(entry)` | Immediate observation | One fresh entry, or an empty list for absence. |
| `File.System.usage(root)` | Immediate observation | Fresh `File.System.Usage!` under exact listing authority. |
| `File.open_paths(root)` | Immediate observation | `List<File.Path>!`. |
| `File.Write.new(path, content)` | Pure construction | Nominal `File.Write`. Commit produces `File.WriteReceipt`. |
| `File.Append.new(path, content)` | Pure construction | Nominal `File.Append`. Commit produces `File.AppendReceipt`. |
| `File.Remove.new(entries)` | Pure fallible construction | Nominal `File.Remove!`. Commit produces `File.RemoveReceipt`. |
| `File.Remove.checked(entries, context..., Eligible)` | Pure fallible construction | Nominal `File.Remove!` with a total pure guard evaluated during commit. |

Write and append accept `Bytes`.
An explicit `UTF8.Bytes.new(text: String) -> Bytes` constructor supplies text encoding.
Decoding remains explicit through `UTF8.String.new`.
The nominal File targets and their receipts are implemented.
Source examples and tests use the canonical constructors without compatibility aliases.

| Receipt | Exact public fields | Completion event |
| --- | --- | --- |
| `File.WriteReceipt` | `path: File.Path`, `bytes_written: Int` | The full write and necessary resource release succeeded. |
| `File.AppendReceipt` | `path: File.Path`, `bytes_written: Int` | The full append and necessary resource release succeeded. |
| `File.RemoveReceipt` | `total_count: Int`, `completed_count: Int`, `observed_bytes: Int` | All selected entries were removed and necessary resource release succeeded. |

Receipt values have no public constructors.
On removal success, `completed_count` equals `total_count`.
`observed_bytes` sums the selected observations and does not measure reclaimed disk space.
Failure evidence keeps the confirmed prefix, failed position, and operation stage.
Write and append failure evidence records confirmed bytes, including a partial write.

The current operation grants, access through retained directories, and entry checks remain the basis of File authority.
Observations are not atomic snapshots.
Identity checks do not make deletion atomic against an uncoordinated writer.
Write and append do not promise a transaction or durable storage after a machine failure.
The [current File design](file-adapter-design.md) records these implemented limits.

A removal guard has signature `Eligible(context..., original: File.Entry, current: File.Entry) -> Bool`.
Construction captures its flow reference and immutable contexts without observation or predicate execution.
During commit, mandatory identity checks precede the guard at preflight and immediately before each removal.
A false predicate produces `File.EntryChanged` with `predicate-false` and the current progress stage.
The current `File.RemoveReceipt` and confirmed-prefix rules apply to either constructor.
The [observation implementation contract](file-observation-extension-design.md) owns exact callback, framing, and budget rules.
The [retained descendant contract](file-descendant-observations.md) defines separate recursive grants and ancestor checks.
Descendants share one acquisition root lease without a descriptor for each visited directory.
Diagnostic snapshots cannot restore that authority.

`File.open_paths` remains a File observation with an independent grant.
Its domain meaning is an observation of open paths beneath the authorized directory.
It does not establish that a file remains open or closed after the observation.
The current `lsof` framing and filename restrictions remain documented backend limitations.
A replacement must pass acceptance before the reference can claim broader filename support.
Moving this operation to Process would not remove those limitations.

## Host Invocation

`Host.Invocation.current()` returns a nominal immutable snapshot of initial host inputs.
It contains program arguments, the initial home path, real and effective UIDs, and the captured wall-clock time.
The runner captures one result before program evaluation and shares it with server startup and HTTP callbacks.
The operation has a host-read effect even when the capture result is already available.
Property projections are pure and supply no host authority.

Source and artifact execution separate program arguments from runner options with an explicit `--`.
Invalid argument encoding or argument limits produce CLI exit status 2 before evaluation.
Invalid home, UID, clock, full snapshot size, or transport data produces a captured `Host.InvocationFailed` result.
That failure reaches Lo only when `Host.Invocation.current()` executes.
An unrelated program can execute without observing the failed capture.

Automatic snapshots redact argument values and the home path recursively, including captured removal contexts.
Explicit projections provide the selected values under ordinary output rules.
The [language reference](../specification.md#96-host-invocation) defines the public fields, limits, and argument boundary.
The [F025 contract](file-observation-extension-design.md#host-invocation) defines exact capture, framing, and redaction behavior.

`Clock.now_unix_ns()` supplies a separate fresh host observation.
Each call returns signed Unix nanoseconds without a change to the invocation snapshot.
`File.System.usage(root)` observes total, used, and available volume space through an authorized directory descriptor.
The [filesystem observation contract](file-system-observations.md) defines arithmetic, authority, errors, and trusted providers.

## HTTP

Server construction, startup receipts, nominal headers, and the general response constructor are implemented.
The header and automatic trace cases have focused validation.
Client source and selected interpreter cases are complete.
Combined interpreter protocol and composition cases passed.
Independent source review closed the current findings.
Matching focused native qualification passed.

HTTP owns protocol values, outgoing exchanges, and server boundaries.
Client actions and incoming server requests have different types because their resource descriptions differ.
All outgoing methods use activation, including methods that request a representation.
Method semantics do not make an outgoing request a pure calculation.

| Type or operation | Current contract |
| --- | --- |
| `HTTP.URL.new(text) -> HTTP.URL!` | Validate an absolute HTTP or HTTPS URL without DNS or network access. Reject credentials in the URL. |
| `HTTP.Method.new(text) -> HTTP.Method!` | Validate a method token without changing its case. |
| `HTTP.Headers.new(entries) -> HTTP.Headers!` | Validate a list of `Record{name: String, value: Bytes}` entries. |
| `HTTP.Headers.values(headers, name) -> List<Bytes>!` | Validate the lookup name and return repeated values without case sensitivity. |
| `HTTP.Response.new(status, headers, body) -> HTTP.Response` | Construct a total response from explicit nominal headers and a Bytes body. |
| `HTTP.Client.Request.new(method, url, headers, body) -> HTTP.Client.Request!` | Construct a nominal client action from validated values and a Bytes body. Reject unsupported exchange forms. |
| Bound client commit | Return `HTTP.Response` after the full response arrives within the host limits. |
| `HTTP.Server.new(port, handler, onError)` | Construct an inert server with the existing named callback signatures. |
| Bound server commit | Return `HTTP.ServerReceipt` after successful listener startup. |

Incoming `HTTP.Request` keeps `method: String`, `path: String`, and `body: Bytes`.
Its `headers` field changes to `HTTP.Headers`.
`HTTP.Response` has `status: Int`, `headers: HTTP.Headers`, and `body: Bytes`.
The general response constructor is `HTTP.Response.new(status, headers, body) -> HTTP.Response`.
It constructs a response description without network access.
The Text and Bytes constructors remain total conveniences with an empty validated `HTTP.Headers` value.
The Text constructor explicitly selects UTF-8 encoding.
All three constructors use one response representation.

The server validates response status and body constraints at emission.
This keeps a total response path available to the mandatory pure total `onError` flow.
An invalid handler response enters that error boundary.
An invalid `onError` response produces a host failure without another handler invocation or a substitute response.
Client response values come from protocol validation at the receive boundary.

Headers keep repeated entries without comma joining.
The boundary normalizes names to lowercase and orders distinct names lexically.
Values with the same name keep their protocol order.
`HTTP.Headers.values(headers, name) -> List<Bytes>!` validates the lookup name and compares it without case sensitivity.
An absent valid name returns an empty typed list.
The `entries` projection exposes the canonical list.
These are semantic header values, not a copy of wire bytes.
Protocol framing remains the transport's responsibility.

The repeatable grant is `--http-allow METHOD,ORIGIN,ADDRESS`.
It requires an exact case-sensitive method, canonical origin, and numeric connection address.
The origin has no credentials, query, fragment, or non-root path.
A grant must contain no whitespace.

The host checks method and origin before DNS access.
It selects an explicitly granted resolved address before one connection attempt.
Ambient proxy settings cannot expand authority.
The first contract does not follow redirects or retry requests automatically.
A redirect response is ordinary response data.
A later explicit request must have its own authorization.

The selected host input is a repeatable `--http-allow 'METHOD,ORIGIN,ADDRESS'` option for `run` and `serve`.
Each grant contains exactly three nonempty fields, without whitespace.
`METHOD` keeps its case.
`ORIGIN` contains an HTTP or HTTPS scheme, a hostname, and an effective port.
The host rejects credentials, queries, fragments, and paths other than an empty path or `/` in this field.
`ADDRESS` contains an IP literal without a zone identifier.
The host converts IPv4-mapped IPv6 addresses to IPv4 before comparison.
Grants with the same canonical origin and method supply one authorized address set.

The runtime checks the method and origin before DNS resolution.
It selects the first authorized resolved address in canonical lexical order.
It makes one connection attempt to that IP literal.
The original hostname controls TLS verification, SNI, and HTTP authority.
A failed attempt does not select another address.
The first client supports HTTP/1.1 only and creates a separate transport for each activation.
It does not use an ambient proxy, automatic redirects, or request replay.

The client constructor rejects CONNECT and user-supplied authority, connection, upgrade, and framing headers.
The host limits each request body and response body to 1 MiB.
It limits each response header section to 64 KiB.
It permits at most eight interim responses, with an aggregate header limit of 64 KiB.
Each exchange has a five-second timeout.
Output overflow and timeout remain host failures.
Ordinary transport and protocol failures use `HTTP.ExchangeFailed`.

Failure evidence contains the operation stage, request-write confirmation, and the known remote-outcome state.
Once transmission can occur, an incomplete exchange has an unknown remote outcome.
This evidence does not contain headers, bodies, full URLs, or query data.
Successful response evidence contains the final status, result type, operation, and commit source span.
The runtime records this completion evidence before it charges the response result.
The complete response remains ordinary program data.

Request and response bodies each have a 1 MiB limit.
Response headers have a 64 KiB limit.
Outgoing request headers use ordinary value budgets.
A separate request-header byte limit is not specified.
At most eight interim responses can consume a combined 64 KiB of headers.
The complete exchange has a five-second deadline.

The initial client supports ordinary bounded HTTP exchanges only.
Its constructor rejects CONNECT and requests for protocol switching.
The receive boundary rejects protocol-switch responses without publishing a successful response.
Interim informational responses consume bounded work and header storage before the final response.
HEAD responses and statuses that prohibit a body produce empty body bytes under their protocol rules.
No tunnel or upgraded connection becomes a Lo value.
The client requires HTTP/1.1 for every response section.

Valid final response statuses, including client and server error statuses, remain response data.
Lo flows select the acceptable statuses.
Transport failure produces a Lo boundary error with operation context and known execution progress.
Output overflow and cancellation remain host failures.
Failure after transmission can leave the remote outcome unknown.

Client failure evidence contains `stage`, `request_written`, and `remote_outcome`.
The remote outcome is `not_started` or `unknown`.
Recoverable failures keep the record in `Error.input.progress`.
Host failures expose it as `http_progress`.
Header and body payloads are absent from this evidence.

Automatic trace inputs keep field names and type-only summaries.
Nominal Headers snapshots keep names and remove value payloads.
Client request snapshots keep method, origin, root path, and header names.
They remove original path, query, body, and header value payloads.
Standalone URL snapshots also replace the path with `/` and remove the query.
Raw program-authored error input and explicit fold state are not general secret scrubbing boundaries.

`HTTP.ServerReceipt` has `address: String` and `port: Int`.
It has no public constructor and identifies the actual bound listener, including an allocated port.
The host session owns the server lifetime and orderly shutdown.
Copying or discarding the receipt has no lifecycle effect.
Another commit is another startup attempt.

Server startup errors reach the initiating flow boundary.
The server `onError` handles recoverable request-boundary errors after startup.
Each request has its own handler invocation and progress state.
No ordering across separate requests is implied.
Host failures keep the existing abort behavior.

## Process

Existing installed OS and runtime dependencies can serve as read-only execution infrastructure.
This allowance does not include another repository, external task data, or external writes.
All added sources, fixtures, caches, and output remain inside Lo.
The abandoned intentional-crash experiment is optional and establishes no native impossibility.

This section defines immutable contexts, pure values, discovery, the shared lifecycle, and selected native execution profiles.
Pure construction and source inspection need no executable access.
Native host qualification and all 24 canonical public activation cases passed. Final stack CI and independent completion review passed.

Process owns bounded execution of an explicitly described child program.
F024 adds explicit context observation, controlled executable discovery, and per-executable cleanup scopes.
The [F024 evidence](evidence/process-context-cleanup/checkpoint.json) records interpreter and native acceptance and stack validation results.
The API keeps context data separate from host authority and shell source.

| Type or operation | Selected contract |
| --- | --- |
| `Process.Context` | An immutable environment and absolute working directory, without host authority. |
| `Process.Executable.new(path: File.Path) -> Process.Executable!` | Validate an absolute executable description without access or launch. |
| `Process.Executable.resolve(context: Process.Context, name: String) -> Process.Executable!` | Discover the first executable candidate under the context's search rules. |
| `Process.Command.new(context, executable, arguments, stdin) -> Process.Command!` | Construct an inert command with `Process.Context`, `Process.Executable`, `List<String>`, and `Bytes` inputs. |
| `Process.Command.with_timeout(command, timeout_ms) -> Process.Command!` | Return an immutable command with a positive millisecond deadline. |
| `Process.Command.with_output(command, mode) -> Process.Command!` | Select `capture` or `forward` without changing the original command. |
| Bound command commit | Return `Process.Result` after bounded execution, output, and cleanup complete. |
| `Process.Result` runtime value | Exact fields: `termination: Process.Termination`, `stdout: Bytes`, `stderr: Bytes`, `output: String`, `stdout_bytes: Int`, `stderr_bytes: Int`. |
| `Process.Termination` | A nominal runtime value with `kind: String`, restricted to `exited` or `signaled`. |
| `Process.Termination.exit_code(value) -> Int!` | Return the exit code for `exited`, otherwise `Process.TerminationKind`. |
| `Process.Termination.signal(value) -> Int!` | Return the signal number for `signaled`, otherwise `Process.TerminationKind`. |

The result and termination values have no public constructors.
The nominal representation has exactly one termination variant, without nullable fields or irrelevant sentinel values.
Nonzero exits and signals remain completed-process data.
Lo flows select the accepted outcomes.
Launch and ordinary capture failures are boundary errors.
Host cancellation and limits keep host diagnostics with available partial execution evidence.

Executable and working-directory paths must be absolute and contain no NUL.
Arguments and environment values must contain no NUL.
Environment names must be nonempty and contain neither NUL nor `=`.
The constructors validate these inputs without filesystem access.
Constructor errors are distinct from executable access and launch failures.

The command context supplies the working directory and full environment.
Arguments contain the values after `argv[0]`.
The runtime supplies the selected executable path as `argv[0]`.
It encodes argument strings as UTF-8 without shell expansion or quoting changes.
It sends the specified stdin bytes, then closes that channel.
Only declared channels and necessary runtime control channels reach the child.
An empty input is explicit and closes without terminal interaction.

### Immutable Contexts And Executable Discovery

The runner captures the environment and physical working directory once before program evaluation.
Every `Process.Context.current()` observation uses those initial inputs, including observations in later server callbacks.
The inherited `PWD` value remains environment data and does not select the physical directory.
Native embeddings can call `lo_context_set_process_context` before evaluation with canonical JSON context inputs.
The native setter copies the supplied input bytes.

| Operation | Effect and result |
| --- | --- |
| `Process.Context.current()` | Host observation, `Process.Context!`. Unavailable inputs produce `Process.ContextUnavailable`. |
| `Process.Context.new(environment, directory)` | Pure, `Process.Context!`. Validate explicit `Map<String, String>` and `File.Path` inputs. |
| `Process.Context.override_environment(context, updates)` | Pure, `Process.Context!`. Replace supplied names and keep unspecified variables unchanged. |
| `Process.Context.without_environment(context, names)` | Pure, `Process.Context!`. Remove listed names, including names already absent. |
| `Process.Context.with_directory(context, directory)` | Pure, `Process.Context!`. Select an absolute directory without access to it. |
| `Process.Context.environment_value(context, name)` | Pure, `String!`. An absent name produces `Process.EnvironmentAbsent`. |

Invalid context construction or changes produce `Process.InvalidContext`.
Each change returns a new value and leaves its input unchanged.
An explicit empty environment remains empty. An empty variable value differs from an absent variable.

Automatic output contains origin and redaction status, without context environment or directory payloads.
Nested diagnostic snapshots also remove those payloads structurally.
Explicit program lookup and output of a selected environment value remain available.

`Process.Executable.resolve` is a host observation. Pure callbacks cannot call it or `Process.Context.current`.
For a name without `/`, discovery examines the context's `PATH` in order.
Missing `PATH` produces `Process.ExecutableAbsent`.
An empty, relative, or noncanonical search entry produces `Process.DiscoveryFailed`.
A name with `/` must be canonical and absolute.

Discovery resolves symlinks and returns the first executable candidate's resolved path.
An exhausted search produces `Process.ExecutableAbsent`. Other discovery failures produce `Process.DiscoveryFailed`.

Discovery selects a candidate without execution authorization.
Activation requires an exact grant for the selected resolved path, plus identity verification through launch.
An unauthorized first candidate produces `runtime.capability-denied`, without a search for a later authorized candidate.
Replacement before activation fails identity verification even when replacement bytes have the approved digest.

### Pure Process Values

Command construction and transformations are pure.
They validate immutable inputs without filesystem access, clock observation, or command execution.
The [command policy](process-command-policy.md) defines duration and output options.
Only commit requests activation.

Without Process grants, an attempted activation produces terminal `runtime.process-unavailable`.
Configured Process policy on an unsupported backend fails session setup.
Inspection reports the required profile without granting authority.

The inspector records context, executable, arguments, stdin, requested duration, and output mode.
Each immutable command variant keeps its own input descriptions.
A transformed command retains `Process.run` as its activation capability.
Constructor diagnostics and automatic snapshots exclude environment, argument, and stdin payloads.

### Native Results And Lifecycle

The native Session owns Process policy, the executable registry, participating File protection, and retained cleanup.
The [Process header](../runtime/native/include/lo_process.h) defines the trusted driver boundary.
The driver receives one admitted request and owns its command until the callback returns.
The host keeps provider code and userdata alive until all retained session work finishes.

`Process.Result` keeps termination, output mode, captured payloads, and confirmed per-stream byte counts.
The [result table](process-command-policy.md#output-and-results) defines each field.
Nonzero exit codes and signals remain completed-process data.
The application decides which outcomes it accepts.

Successful activation requires complete stdin delivery, output completion, known termination, and complete resource cleanup.
A timeout becomes recoverable `Process.Timeout` only after those cleanup guarantees hold.
Ordinary execution failure produces `Process.ExecutionFailed` after cleanup.
Caller cancellation, output failure, quota exhaustion, and unknown cleanup remain terminal.

Failure progress records confirmed creation, termination, and byte counts.
An unconfirmed start does not prove that no process exists.
Automatic diagnostics exclude partial output payloads.
An immutable progress record keeps the evidence available when the runtime produced that record.

The host supplies execution, cleanup, and per-stream output limits.
The defaults are five seconds, one second, and 1 MiB per stream.
An explicit command duration cannot exceed the selected host maximum.
The runtime rejects excessive requests before launch.

A cleanup deadline bounds the caller's wait.
It does not release unresolved ownership.
The owner continues cleanup, and the session retains dependent registries and locks.
Unknown cleanup prevents another command activation and successful resource release.
The [embedding contract](native-embedding.md) defines context completion and retained lifetime.

### Executable Registry

The native registry retains exact executable identities and their protected names.
Process admission checks the selected executable, launcher, and authorized scope before launch.
Identity replacement and digest mismatch fail before command execution.

Participating File writes, appends, and removals reject protected executable names and aliases.
The protection includes retained ancestor identities.
Unrelated sibling mutations remain available.
This protection does not exclude arbitrary concurrent changes by external host processes.

An activation retains its registry through complete cleanup.
Unresolved creation or cleanup keeps the registry in quarantine.
The host cannot clear unknown ownership through an arbitrary release request.
Registry protection supplies no child filesystem authority.
The selected launcher profile enforces that separate boundary.

### Host Policy Inputs

Compiled programs accept these inputs with `run` and `serve`.
The [policy checks](evidence/process-policy.json) passed through both public argument entry points.
Parsing validates descriptions without filesystem access. The selected native profile and canonical public acceptance passed. Final stack CI and independent completion review passed.

| CLI input | Description |
| --- | --- |
| `--process-allow SHA256,ABSOLUTE_PATH` | Repeatable command grant with a 64-digit hexadecimal digest. |
| `--process-launcher ABSOLUTE_PATH` | One trusted launcher path. The build must supply its expected identity before activation. |
| `--process-read-root ABSOLUTE_PATH` | Repeatable child read root. The selected profile uses these roots for data reads and the working directory. |
| `--process-write-root {"executable":"/absolute/tool","root":"/absolute/cache"}` | Repeatable data-write root for one exact granted initial executable. |
| `--process-prune-root {"executable":"/absolute/tool","root":"/absolute/prune"}` | Repeatable namespace-removal root for one exact granted initial executable. |
| `--process-unix-endpoint {"executable":"/absolute/tool","endpoint":"/absolute/socket"}` | Repeatable exact Unix socket endpoint for one granted initial executable. |
| `--max-process-ms MILLISECONDS` | Positive execution maximum and default for commands without an explicit duration. Default: 5000. |
| `--max-process-cleanup-ms MILLISECONDS` | Positive cleanup duration. Default: 1000. |
| `--max-process-output-bytes BYTES` | Positive output bound for each stream. Default: 1048576. |
| `--process-forward-output` | Permit forwarding through enabled Console sinks. |

A configured policy requires command grants, one launcher, and child read roots.
Cleanup scope inputs are optional and use strict JSON objects.
Parsing rejects duplicate or unknown fields, nontext values, trailing input, and malformed Unicode.
Paths must be clean, absolute, and free of NUL.
The grant separator follows the digest. Commas after that separator are part of the path.
Duplicate command paths, read roots, and launcher options are invalid.
A command grant cannot use the launcher path.
Native validation rejects a command that uses an alias of the launcher.
These inputs do not grant parent File authority or inherit parent File grants.
Without grants, the session keeps File support and rejects only attempted Process activation.
Configured Process policy on an unsupported backend fails session open before helper or command lookup.

### Selected Native Admission Limits

These limits apply to the qualified Darwin 27 arm64 profile, separately from pure constructor validation.
Pure constructors can accept descriptions that exceed these native limits.
Native admission rejects those descriptions without target execution.

| Native input | Limit |
| --- | --- |
| Command grants | At most 16 exact path/digest grants. |
| Child read roots | At most 16 roots. |
| Mutation roots | At most 16 combined write and prune roots per initial executable. |
| Unix endpoints | At most 16 endpoints per initial executable, with at most 103 UTF-8 bytes per endpoint path. |
| Arguments | At most 256 entries including `argv[0]`, so at most 255 supplied arguments. |
| Environment | At most 4096 `NAME=value` entries. |
| Argument or environment entry | At most 65536 UTF-8 bytes, including the complete `NAME=value` entry. |
| Path | At most 1024 UTF-8 bytes. |
| Command body | At most 1048576 bytes, including each entry's four-byte length prefix. |

Native helper, command, read-root, and working-directory paths must equal their resolved paths.
Native policy paths must already be canonical. Executable discovery can resolve a symlink before exact grant selection.
These restrictions do not change the constructors' lexical path contract.

Scope-count limits fail backend configuration before helper, file, or registry I/O, even when the program never activates a command.

### Cleanup Execution Profile

The default scope denies child writes and network access.
F024 adds explicit data-write roots, namespace-removal roots, and exact Unix socket endpoints for each initial executable.
The host never combines scopes from different initial executables.
Registered in-process exec chains retain the initial executable's scope, including interpreter-to-worker execution.
Both profiles deny fork and `posix_spawn`. They do not support arbitrary package-manager process trees.

Mutation roots must be canonical, disjoint within the selected scope, and contained in child read roots.
They must not contain a registered executable or the trusted launcher.
Unix endpoints must be canonical socket paths outside every mutation root.
The host rechecks roots and endpoint kinds before activation.

Data-write admission inventories all selected mutation roots through directory-relative operations, without symlink traversal.
The inventory has a 100000-entry limit and a depth limit of 128.
Changing directory identities or inconsistent hardlink counts fail admission.

This profile uses the registry's trusted host model.
External actors must not change the namespaces or hardlink sets of mutation roots during admission or command execution.
Admission scans do not lock other host processes.

Every hardlink name for a data-write inode must remain within data-write roots.
An alias outside those roots, including an alias in a prune root, prevents activation.
A prune-only scope can unlink an outside-linked name without changing the underlying file data.
It cannot move that file into a data-write root or modify its contents.

Profile enforcement protects registered images, outside files, sibling endpoints, and other executables' scopes.
The trusted launcher configuration binds the initial executable and rejects a mismatched command frame.
Command environment values never enter the launcher's fixed environment.

Repository helpers qualify exact package arguments, six environment overrides, inherited variables, directory selection, closed input, and captured results.
The package example preserves pure command descriptions and uses `Flow.fold` for explicit sequential activation.
Wrapper fixtures qualify registered exec chains separately from denied forked and spawned descendants.

A local Docker-like protocol records confirmed requests and checks responses over an authorized Unix endpoint.
Client cancellation does not reverse a confirmed daemon effect.
These fixtures do not qualify installed npm, Go, uv, pnpm, pip, Docker, or buildx implementations.
The complete mac-cleanup workflow remains separate, as specified by its [application plan](mac-cleanup-plan.md).

### Execution Profile Prerequisite

An executable grant alone cannot constrain the child's filesystem or network access.
Process must therefore have an enforceable host execution profile before launch.
The profile is host policy, not a value that a Lo program can manufacture.

The profile must cover:

- Executable identity and resistance to replacement before launch.
- Filesystem access, including directories, path traversal, and symlinks.
- Network destinations and inherited host channels.
- Environment inputs and standard input/output channels.
- Descendant processes and their authority.
- Work, output, process-count, and lifetime limits.
- Cancellation, descendant termination, output drainage, and resource release.

The native launcher must start with a fixed host environment.
Command environment variables must not enter that environment.
Loader variables can execute command-supplied code before the launcher's entry point.
Install confinement before receipt of the command environment through a separate bounded channel.
Validate the complete frame and its end before target execution.
Close the control channel before exec, and keep standard input separate.
Pass the validated environment to the target without changes or inherited entries.
An empty command environment must remain empty.

An unsupported profile fails before executable resolution or launch.
The runtime must keep the profile effective throughout child and descendant execution.
There is no unrestricted execution mode when profile enforcement is unavailable.
Process remains unavailable on an unqualified backend.

Success confirms child termination, completed output capture, and no surviving descendants under the profile.
A descendant that outlives the selected limits causes failure and host cleanup.
Termination of the primary child alone does not establish completion.
Failure evidence must keep confirmed output and distinguish execution from successful resource release.

Each selected native backend/profile must pass qualification before its activation acceptance.
A qualified macOS profile can activate while Linux remains unavailable.
Well-behaved test helpers prove command semantics but do not prove confinement.
Adversarial helpers and controlled boundary failures must establish the profile guarantees separately.
Every helper, attempted filesystem target, and output remains inside Lo during this work.

### First Native Profile

The selected profile permits one active command per host executor, with bounded admission.
It denies descendants, network access, and child filesystem writes.
The child can return captured bytes for a later parent File commit.
Only explicit command inputs and the specified standard channels reach the child.
The profile enforces output, descriptor, execution-time, and cleanup-time limits.

The conditional work bound derives from maximum host execution parallelism and total child lifetime.
For parallelism `P` and lifetime `L`, the conservative CPU-time bound is `P × L`.
This bound does not provide a separately configurable CPU quota or an enforced thread-count limit.
No-fork enforcement does not prevent threads.
Qualification must establish `P`, bounded admission, and `L` from startup through confirmed termination.
The earlier synchronous experiment did not establish that lifetime bound. The qualified supervisor tracks the complete admitted lifetime.

This bound assumes a responsive OS and scheduler that complete creation and termination operations.
A bounded caller return does not establish bounded child lifetime.
If creation or termination is unresolved, report incomplete cleanup and an unverified lifetime bound.
Ownership and quarantine prevent another admission but do not establish `L`.
Do not claim that the work bound held for that operation.

The failed hard CPU-limit experiment remains failed evidence.
The first profile does not depend on that mechanism or an unverified CPU monitor.
The selected profile passed native host qualification and canonical public acceptance.

### Native Startup Protocol

Native startup must retain ownership before asynchronous OS creation can leave a child behind.
One owner must track pending creation, the eventual primary, its channels, and its reaper.
Admission must bound pending launches as well as active commands.
Cancellation must retain that owner until creation settles and resource release succeeds.
An unresolved OS call must produce incomplete cleanup and registry quarantine.
An asynchronous wrapper does not prove that the OS call can be interrupted.

The launcher must use its fixed host environment and install confinement before receipt of command inputs.
The launcher must validate command inputs and send `READY` through a private control channel.
`READY` shows confinement and input validation.
After the final cancellation and ownership check, the host must send a separate affirmative execution permit.
Closure without that permit must abort startup. EOF alone must not authorize execution.
Permit publication defines the activation boundary.
Cancellation after that boundary can race activation and must terminate the owned child.
The driver must preserve that uncertainty instead of claiming that command code could not execute.
Launcher control and failure messages must remain separate from target stdout and stderr.

| Evidence | Meaning |
| --- | --- |
| Owned pending launch | The host keeps creation and cleanup responsibility. |
| `StartConfirmed()` | The owned OS primary exists. |
| `READY` | The launcher completed confinement and input validation. |
| Successful permit publication | Target execution can occur. Concurrent cancellation can race this boundary. |
| Target execution evidence | The selected command executed. |
| Primary termination | The owned primary terminated, which can occur before target execution. |
| Successful `Release` | Creation is finished and no owned process or private resource is left. |

The supervisor must serialize observed cancellation and permit publication.
Raw context cancellation is not an atomic pipe operation.
Before known target execution, launcher termination produces an activation failure with available primary-termination evidence.
EOF on an error channel does not independently show target execution because process death also closes the channel.
Structured startup failures must identify the stage without command payloads.

The candidate will test a parent `NOTE_EXEC` observation registered after `READY` and before the permit.
Registration failure must prevent permit publication.
Event collection and reaping must preserve execution evidence when the target exits immediately.
The observation must stay bound to the owned child until final cleanup.
The selected native startup cases passed, with completed ownership and cleanup evidence.

The [earlier isolated supervisor](evidence/process-startup-candidate.json) passed public lifecycle tests and the race check.
Its direct pending endpoints preserve byte counts and errors through late creation and channel closure.
Internal stop is separate from caller cancellation. Callback errors keep approved host classes without private payloads.
Independent review approved the deterministic checkpoint. These tests do not qualify native startup.

The [native startup suite](evidence/process-startup-native.json) passed all eight cases under the selected fixture profile.
Immediate exits retained execution evidence. EOF without a permit terminated the launcher before Stop or the deadline.
Cancellation before the permit prevented execution. Cancellation after the permit retained observed execution and completed cleanup.
All cases confirmed cleanup. The outer controller needed no emergency signal, and sealed artifacts remained unchanged.
Independent review approved this startup checkpoint. Subsequent host qualification covers the selected complete profile.

The existing environment probe does not implement this production protocol.
Its complete frame followed by EOF permits the fixed target to execute.
That behavior proves environment transport only. It does not qualify cancellation before activation.

### Qualification Status

The selected Darwin 27 arm64 profile passed [thirteen native host cases](evidence/process-host-native.json).
The [startup suite](evidence/process-startup-native.json) and [local IPC suite](evidence/process-ipc-local-probe.json) supply the related mechanism evidence.
The [public acceptance lane](evidence/process-public-native.json) passed all 24 interpreter/generated-native cases after the closed-gate behavioral red.
It includes server callbacks, Process-to-File composition, denials, wall timeout, and exact result bytes.
Cleanup completed, sealed artifacts stayed unchanged, and the controller needed no emergency signal.
Final stack CI and independent completion review passed.
Current acceptance and final stack CI passed. Independent completion review passed.

The selected host verifies the pinned helper and exact registered command identities.
Continuous protection and ownership leases cover participating File mutations and the command lifetime.
The default profile denies child writes and network access. Both default and cleanup profiles deny forked and spawned descendants.

The [F024 checkpoint](evidence/process-context-cleanup/checkpoint.json) adds context, discovery, transport, mutation, endpoint, and exec-chain qualification.
The checkpoint records interpreter integration, native integration, semantic checks, qualification-tool checks, and their exact source identities.
Metal-linked Process artifacts exercised no GPU operation.

The launcher clears SDK-enumerated registered ports, bootstrap authority, and task/current-thread exception slots before admission.
It preserves required runtime/self rights and rejects aliases of those protected endpoints.
Local controls establish selected-slot normalization, without a named IPC-denial or arbitrary inheritance claim.

The conditional work bound is `P × completed lifetime`, under the stated responsive-OS assumption.
It is not a hard CPU quota. Unresolved creation or termination leaves the lifetime bound unverified.
Ownership stays with the session until cleanup completes, including after a caller deadline.
Without Process grants, ordinary execution remains available. Only attempted Process activation returns `runtime.process-unavailable`.
Configured Process policy on an unsupported backend fails session open before program evaluation.
No unrestricted execution option exists.

Earlier bounded probes follow. Their limits describe those runs, not the later complete host qualification.
SDK deprecation is a platform limitation, not automatic disqualification.
No result establishes platform impossibility.

| Earlier evidence | Observed result | Limit at that checkpoint |
| --- | --- | --- |
| [Self-confining fixture](evidence/process-mechanism-probe.json) | Allowed reads/writes succeeded; denied file, network, and fork operations returned `EPERM`. | Trusted current-host fixture only; no arbitrary-executable startup guarantee. |
| [Pre-exec probes](evidence/process-preexec-probe.json) | Abort followed successful `sandbox_init` and the before-exec marker, without target entry. | Original-profile startup failure remains unexplained; no specific cause or denied operation established. |
| [Mapping comparisons](evidence/process-map-probe.json) | RX mapping failed with `EPERM` before sandbox installation and under confinement. | Inconclusive; no sandbox-specific restriction or startup cause established. |
| [Debugger diagnostic](evidence/process-debugger-probe.json) | Listener deadline failed after 3.112 seconds; last log operation was `about to task_for_pid(32015)`. | Wrapper stayed ready; no target entry, controller report, or reproduced startup abort. |
| [Loader diagnostic](evidence/process-loader-probe.json) | Launcher loader output preceded its four phase markers; `SIGABRT` followed the before-exec marker. | No bytes after that marker, target entry, or startup cause. |
| [Bootstrap comparisons](evidence/process-bootstrap-probe.json) | Query-only and paired enclosing-syscall variants both aborted after four launcher markers. | Syntax accepted; no target, startup cause, or qualification. Query branch stopped. |
| [Root-handle probe](evidence/process-root-open-probe.json) | Baseline root open/close succeeded; confined open returned `EPERM` without a new descriptor. | Original profile unchanged; mechanism denial only, not startup diagnosis or qualification. |
| [Root-read bootstrap](evidence/process-root-bootstrap-probe.json) | Fixed target started and reported exact argv/environment, three standard descriptors, and expected file/network/fork results. | Only directory check failed; its cause is unresolved. Trusted fixture only, not full qualification. |
| [Working-directory probe](evidence/process-cwd-probe.json) | Baseline `getcwd` was exact; confined `getcwd` returned NULL with `EPERM`. | No actual directory-change or prior target-failure cause established. |
| [Directory identity after exec](evidence/process-cwd-identity-probe.json) | Fixed-fixture stat-dot device/inode matched the sealed selected cwd; all listed mechanism checks passed. | Pathname `getcwd` remained denied; no full native qualification. |
| [Command environment isolation](evidence/process-environment-probe.json) | All 17 cases passed: constructor denial, specified environments, malformed inputs, and held-open EOF cleanup. | Fixed helpers only; synchronous process creation has no independent deadline control. |
| [Process creation and resources](evidence/process-resources-probe.json) | Creation denials, wall termination, bounded output, completed cleanup, descriptor exhaustion, and limit readback passed their selected cases. | Independent hard CPU enforcement failed. Complete lifetime, derived work bounds, and the full native profile remain unqualified. |
| [No-write and inherited channels](evidence/process-no-write-probe.json) | All eight cases passed for selected writes, namespace operations, aliases, Unix-socket access, and descriptor exclusion. | Shared mapping was unreachable. Other IPC, native startup, and full profile qualification remain pending. |
| [Native startup](evidence/process-startup-native.json) | All eight cases passed, including immediate exits, permit EOF, cancellation, and complete cleanup. | Inherited Mach rights, general native identity integration, and the complete resource contract remain pending. |
| [Initial IPC control](evidence/process-ipc-probe.json) | The selected slots became empty, but the control did not establish endpoint identity. | Inconclusive for inherited endpoint removal. |
| [Local IPC control](evidence/process-ipc-local-probe.json) | All three variants confirmed four local endpoints, SEND removal, RECEIVE destruction, and complete cleanup. | Selected-slot normalization only. It does not prove parent inheritance or named IPC denial. |
| [Host lifecycle](evidence/process-host-lifecycle.json) | Session shutdown, retained close failures, status-reader completion, and public failure metadata passed race checks. | No native activation occurred in these lifecycle tests. |

Detailed manifests, observations, bounds, and historical limits remain in the linked evidence.
The [implementation review](evidence/adapter-implementation-review.json) also keeps earlier static-construction and abandoned-probe history.

## Inspection and Execution

The catalog owns types, constructor failures, observations, activation result types, and callback contracts.
The checker and inspector consume that shared definition.
The IR keeps observations, commits, and device requirements separate.
Stored callbacks keep requirements for their later invocation phase.

Inspection identifies possible operations, grants, completion types, and callback contexts without external access.
Calculated targets remain calculated requirements until runtime authorization.
Process inspection reports the required profile and declared inputs.
Source inspection without host configuration reports the profile as unavailable. It does not claim runtime enforcement.
Process activation verifies backend support and establishes enforcement before launch.
It does not infer the executable's internal algorithm or specific effects from its path.

Interpreter and native execution must agree on results, order, errors, authority, and progress.
Neither backend can silently repeat an observation or activate a constructed target.
Resources remain host-owned, even when immutable observations or receipts outlive a particular operation.
Diagnostic output must not expose secret header or environment values.

## Current Implementation and Required Changes

| Area | Implemented behavior | Remaining work |
| --- | --- | --- |
| Observations | File reads, entries, and open paths return immutable values. | Keep observation order and selected branches in later adapters. |
| Purity | Pure callbacks exclude transitive observations and commits. Device requirements stay separate. | Apply this rule to future callback positions. |
| Commit | Bound and unbound statements use catalog-defined success types and statement failure semantics. | Add future nominal target categories. |
| File actions | Nominal Write, Append, and Remove targets return operation-specific receipts. Current-stack CI passed. | No stage 1–2 checkpoint work remains. |
| HTTP | Nominal headers, client/server behavior, and HTTP-to-File composition passed prior CI. | Canonical public Process composition passed. Final stack CI passed. |
| Process | Results, registry, lifecycle, and the selected Darwin 27 arm64 native host passed qualification and final stack CI. | No work remains for the selected profile. |
| Higher-order operations | Pure List callbacks and ordered `Flow.fold` with observations and commits. | Apply the admission contract to future domain operations. |

The [implementation review](evidence/adapter-implementation-review.json) records shared contracts, File/HTTP behavior, Process results, injected lifecycle, and composition evidence.
The focused interpreter run passed in 1.423 seconds.
The native run passed in 31.031 seconds and included all `TestFile` cases.
Independent source review closed the three operation-progress and cleanup findings.
The reviewer did not run Make.

Those earlier results did not complete the full adapter plan.
The historical stages 1–3 checkpoint passed `CI=1 make ci </dev/null` with exit status `0`.
Full interpreter integration took 54.454 seconds, and full native integration took 482.811 seconds.
Runtime bundle checks, formatting, Go vet, and the build passed.
The earlier independent checkpoint review approved stages 1–3 without a remaining source finding.
The reviewer examined source and evidence without execution of tests.

Initial CI failed in integration after 60.053 seconds.
The corrected fold carrier, value-budget preservation, and pure-head Match fixture are included in final CI.
The [implementation review](evidence/adapter-implementation-review.json) keeps the initial failure and correction history.

The validated stages 1–3 checkpoint stops before the new pure Process phase.
Pure Process work requires new validation but does not require launch qualification.
Native Process execution requires a qualified profile and its own final stack validation.

The [File implementation record](file-adapter-plan.md) separates earlier workflow evidence from the current adapter update.
Future stages must update the specification, catalog, examples, and both backends together.
Obsolete shapes and aliases must not remain as compatibility paths.

## Open Decisions

- Qualify any additional Process backend/profile separately.
- Specify a separate outgoing request-header byte limit before a broader bound claim.
- Qualify open-file observation backends before any broader filename-support claim.

These decisions do not reopen bound commit semantics or authorize unrestricted Process execution.
The [F025 contract](file-observation-extension-design.md) defines metadata, host inputs, scan reports, and conditional removal.
F025 acceptance remains separate from full mac-cleanup application acceptance.
Before implementation, specify the remaining lock, candidate-probe, and application orchestration contracts.

The earlier pure Process checkpoint passed `CI=1 make ci </dev/null` with exit status `0`.
Full interpreter integration took 43.391 seconds, and full native integration took 522.053 seconds.
Runtime bundle verification, formatting, Go vet, tests, and the final build passed.
The source finding is closed, and independent review approved the pure Process checkpoint.
This earlier checkpoint did not qualify native execution. Current implementation and final stack CI passed. Independent completion review passed.

The latest lifecycle checkpoint passed `CI=1 make ci </dev/null` with exit status `0`.
Interpreter integration took 44.126 seconds, and full native integration took 481.452 seconds.
Runtime bundle verification, formatting, Go vet, all tests, and the build passed.
The Process race selection passed in 5.162 seconds, including injected composition and recovery.
Bounded source review approved the cleanup corrections.
Independent final review approved the host-independent results and lifecycle checkpoint without blocking findings.
This earlier checkpoint covered injected execution. Native qualification and public composition have since passed. Final stack CI and independent completion review passed.
