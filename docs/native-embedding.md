# Compiled Program Embedding

Lo embedding executes a compiled entry through the native C ABI.
The [core header](../runtime/native/include/lo_runtime.h) owns declarations, statuses, and pointer ownership.
The host links the compiled entry and its selected Rust runtime archive.
The compiler supplies parse, check, inspect, and build commands.

## Program And Context Ownership

`lo_program_new` checks the compiled metadata and returns one owned program reference.
A context retains its program, so the caller can release the original program reference after context creation.
One immutable program can supply contexts on different threads.
Each context belongs to its creation thread.

Run, cancel, close, and free the context on that owner thread.
Send external cancellation requests to the owner, including through a registered callback.
The embedding host owns signal policy.
Embedding contexts do not install signal handlers or observe the native CLI cancellation flag.
The compiled program's CLI owns signals for program commands.

Successful and recoverable result slots each transfer one value reference to the caller.
Retain a value before another owner needs a reference.
Release each owned value reference once.
A retained value remains readable after context release.

`lo_context_close` joins retained work and reports cleanup success.
Repeated close returns its recorded result.
`lo_context_complete` validates the final value and publishes terminal output after cleanup.
Failure leaves the caller's status output unchanged.
The host must preserve callback state until all dependent work ends.

## Diagnostics

`lo_context_diagnostic` writes UTF-8 diagnostic JSON into a caller-owned buffer.
The document has no terminal NUL and has a maximum size of 1048576 bytes.
A zero-capacity call with a null buffer obtains the required size.
The API never writes a partial document.

| Status | Meaning |
| --- | --- |
| `LO_DIAGNOSTIC_OK` | The buffer contains the complete document. |
| `LO_DIAGNOSTIC_SHORT_BUFFER` | The required size is available without a write. |
| `LO_DIAGNOSTIC_NONE` | No diagnostic is available. |
| `LO_DIAGNOSTIC_INVALID` | An argument or result slot is invalid. |
| `LO_DIAGNOSTIC_WRONG_THREAD` | The caller is not the context owner. |
| `LO_DIAGNOSTIC_TOO_LARGE` | The document exceeds the bound. |

An optional result slot selects a recoverable error from that context.
A stored terminal or cleanup fault takes precedence.
A null result slot selects only the stored fault.
The API emits no Console output and changes no signal policy.

## Explicit Host Boundaries

The [Console header](../runtime/native/include/lo_console.h) declares nonblocking write and flush callbacks.
The host supplies destination authority and the cumulative output quota before evaluation.
Write callbacks confirm an accepted byte prefix.
Console registration does not grant File, Process, HTTP, device, or lock authority.

The [entry and lock header](../runtime/native/include/lo_program_lock.h) declares explicit identity and lock registration.
The [lock contract](program-lock-design.md) owns retained identity and cleanup guarantees.

Device and Process test drivers remain explicit trusted injection boundaries.
The [Process header](../runtime/native/include/lo_process.h) owns the test-driver request and response contract.
Forwarded output callbacks run on the context owner thread through the registered Console sinks.
Keep Process driver state alive until its owned work and cleanup finish.

General File, HTTP, and real Process grant configuration is outside this retained embedding API.
Compiled program commands supply those explicit application grants.

The [host observation header](../runtime/native/include/lo_host_observer.h) declares synchronous clock and volume providers.
Install each provider on the owner thread before session evaluation.
Keep callback code and userdata valid until all session requests finish.
Providers can cancel their context but cannot reenter or destroy it.
Volume providers run after native measurement under exact directory authority.

A provider returns zero for success, positive errno for an observation failure, or a negative value for terminal failure.
Invalid provider metadata produces a terminal host failure.
The [file observation header](../runtime/native/include/lo_file_observer.h) includes the signed allocation block count.
That provider cannot replace native device or inode identity.
The [observation contract](file-system-observations.md) defines these values and their failure behavior.
