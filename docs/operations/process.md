# Process Operations

[Documentation](../README.md) / [Operation index](README.md)

Process values describe immutable inputs and inert commands.
The qualified activation profile is Darwin. Linux command activation remains unavailable.
Examples with typed flow parameters show composition without discovery or command execution.

All entries describe implemented operations. Host quotas and cancellation apply in addition to the listed typed failures.

A typed example flow is callable with validated inputs. Its final `return 0` leaves that flow unevaluated.

## Process.Arguments.format_shell

```text
Process.Arguments.format_shell(arguments: List<String>) -> String!
```

Return display text in POSIX shell syntax. Each argument has single quotes, with one space between arguments. Apostrophes receive shell quoting.

An empty argument becomes `''`. An empty list produces an empty String. The result does not execute a shell.

**Failures:** `Process.InvalidArguments` if an argument contains NUL.

**Authority:** Pure text formatting. Process activation receives the original argument list, not this display value.

**Example:**

```lo
return Process.Arguments.format_shell(List.new("tool", "two words", ""))
```

[Contract](../value-processors.md#command-display)

## Process.Command.new

```text
Process.Command.new(context: Process.Context, executable: Process.Executable, arguments: List<String>, stdin: Bytes) -> Process.Command!
```

Construct an inert command with explicit context, executable, literal arguments, and stdin bytes.
Commit returns `Process.Result` with termination, output mode, captured bytes, and confirmed per-stream byte counts.

Nonzero exit codes and signals remain result data.
The default host duration is five seconds. The default profile denies forked or spawned descendants.
The host can explicitly select the bounded [descendant profile](../process-descendant-ownership.md).
That profile preserves the initial command scope and requires complete descendant cleanup before release.

Native admission also bounds paths, arguments, environment entries, transport bytes, output, and child resources.

**Failures:** `Process.InvalidCommand` during construction. Activation can produce `Process.ExecutionFailed` or `Process.Timeout` after complete cleanup.

Missing configuration, denied authority, cancellation, resource limits, and unresolved cleanup remain terminal host diagnostics.

**Authority:** Pure construction. Activation uses the qualified Darwin Process host with exact executable identity and child-root grants. Linux activation remains unavailable.

**Example:**

```lo
flow Build(context: Process.Context, executable: Process.Executable) -> Process.Command! {
    return Process.Command.new(context, executable, List.new("--version"), Bytes.new(""))
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Command.with_output

```text
Process.Command.with_output(command: Process.Command, mode: String) -> Process.Command!
```

Return a new command with `capture` or `forward` output. Keep the original command unchanged.
Capture retains separate stdout and stderr bytes.
Forwarding uses the enabled Console sinks and returns empty payloads with confirmed byte counts.

**Failures:** `Process.InvalidCommand` if the mode is not `capture` or `forward`.
Sink failure, output limits, cancellation, and unknown cleanup remain terminal during activation.

**Authority:** Pure construction. Forwarding needs explicit host authority and both Console destinations at activation.

**Example:**

```lo
flow Forward(command: Process.Command) -> Process.Command! {
    return Process.Command.with_output(command, "forward")
}
return 0
```

[Contract](../process-command-policy.md#output-and-results)

## Process.Command.with_timeout

```text
Process.Command.with_timeout(command: Process.Command, timeout_ms: Int) -> Process.Command!
```

Return a new command with the requested positive millisecond deadline. Keep the original command unchanged.
The deadline covers startup, execution, and output delivery.

**Failures:** `Process.InvalidCommand` if the duration is not a positive signed 64-bit millisecond value.
Activation rejects a request above the host maximum before launch.
A timeout produces recoverable `Process.Timeout` only after complete cleanup.

**Authority:** Pure construction without clock observation. The host supplies maximum duration and cleanup limits.

**Example:**

```lo
flow Timed(command: Process.Command) -> Process.Command! {
    return Process.Command.with_timeout(command, 30000)
}
return 0
```

[Contract](../process-command-policy.md#timeout-and-cleanup)

## Process.Context.current

```text
Process.Context.current() -> Process.Context!
```

Return the initial environment and physical working directory captured before program evaluation. Repeated observations use the same captured inputs.

The inherited `PWD` variable is environment data. It does not select the physical directory.

**Failures:** `Process.ContextUnavailable` if the initial capture is unavailable or invalid.

**Authority:** Host observation. The result grants no execution or filesystem authority. Pure callbacks cannot make this observation.

**Example:**

```lo
return Process.Context.current()
```

[Contract](../../specification.md#95-process-adapter)

## Process.Context.environment_value

```text
Process.Context.environment_value(context: Process.Context, name: String) -> String!
```

Return the exact value for a selected environment name. An empty value differs from an absent variable. Explicit output can expose the selected value.

**Failures:** `Process.EnvironmentAbsent` if the name is absent.

**Authority:** Pure lookup in the immutable context. No new environment observation.

**Example:**

```lo
flow ReadMode(context: Process.Context) -> String! {
    return Process.Context.environment_value(context, "MODE")
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Context.new

```text
Process.Context.new(environment: Map<String, String>, directory: File.Path) -> Process.Context!
```

Construct a context with an explicit environment and absolute working directory. An empty environment remains empty. Construction does not examine the directory.

Environment names must be nonempty and contain neither NUL nor `=`. Values and directory text must contain no NUL.

**Failures:** `Process.InvalidContext` for invalid names, values, or directory text.

**Authority:** Pure construction. No execution or filesystem authority.

**Example:**

```lo
flow ContextFor(directory: File.Path) -> Process.Context! {
    return Process.Context.new(Map.new(), directory)
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Context.override_environment

```text
Process.Context.override_environment(context: Process.Context, updates: Map<String, String>) -> Process.Context!
```

Return a new context with the supplied environment values. Other variables and the working directory stay unchanged. The input context remains unchanged.

**Failures:** `Process.InvalidContext` for an invalid environment name or value.

**Authority:** Pure transformation. No host observation or additional authority.

**Example:**

```lo
flow SetMode(context: Process.Context) -> Process.Context! {
    return Process.Context.override_environment(context, Map.new(Record.new(key: "MODE", value: "preview")))
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Context.with_directory

```text
Process.Context.with_directory(context: Process.Context, directory: File.Path) -> Process.Context!
```

Return a new context with the selected absolute directory. The environment and input context remain unchanged. Construction does not resolve or open the directory.

**Failures:** `Process.InvalidContext` if the directory is relative or contains NUL.

**Authority:** Pure transformation. Selecting directory text does not grant child access.

**Example:**

```lo
flow SelectDirectory(context: Process.Context, directory: File.Path) -> Process.Context! {
    return Process.Context.with_directory(context, directory)
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Context.without_environment

```text
Process.Context.without_environment(context: Process.Context, names: List<String>) -> Process.Context!
```

Return a new context without the listed variables. Absent names have no effect. The input context remains unchanged.

**Failures:** `Process.InvalidContext` if a listed name is empty or contains NUL or `=`.

**Authority:** Pure transformation. No host observation or additional authority.

**Example:**

```lo
flow RemoveToken(context: Process.Context) -> Process.Context! {
    return Process.Context.without_environment(context, List.new("TOKEN"))
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Executable.new

```text
Process.Executable.new(path: File.Path) -> Process.Executable!
```

Construct an executable description from an absolute path with no NUL. Construction preserves lexical text without discovery or shell expansion.

**Failures:** `Process.InvalidExecutable` if the path is invalid. A valid description does not establish existence or launch authorization.

**Authority:** Pure construction. No filesystem access or execution authority.

**Example:**

```lo
flow Describe(path: File.Path) -> Process.Executable! {
    return Process.Executable.new(path)
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Executable.resolve

```text
Process.Executable.resolve(context: Process.Context, name: String) -> Process.Executable!
```

Discover an executable with the selected context. A name without `/` uses that context's `PATH`. A name with `/` must be canonical and absolute.

Discovery resolves symlinks and selects the first executable candidate. Launch authorization cannot substitute a later candidate.

**Failures:** `Process.ExecutableAbsent` for missing `PATH` or an exhausted search. `Process.DiscoveryFailed` for other failures, including empty or noncanonical search directories.

**Authority:** Host observation. Discovery grants no launch authority. An unauthorized selected candidate fails with a terminal host diagnostic when activated.

**Example:**

```lo
flow Discover(context: Process.Context) -> Process.Executable! {
    return Process.Executable.resolve(context, "tool")
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Termination.exit_code

```text
Process.Termination.exit_code(value: Process.Termination) -> Int!
```

Return the exit code when the termination kind is `exited`. The accessor does not convert a nonzero exit into an application error.

**Failures:** `Process.TerminationKind` when the value records signal termination.

**Authority:** Pure lookup. No process activation.

**Example:**

```lo
flow ExitCode(result: Process.Result) -> Int! {
    return Process.Termination.exit_code(result.termination)
}
return 0
```

[Contract](../../specification.md#95-process-adapter)

## Process.Termination.signal

```text
Process.Termination.signal(value: Process.Termination) -> Int!
```

Return the signal number when the termination kind is `signaled`.

**Failures:** `Process.TerminationKind` when the value records ordinary exit.

**Authority:** Pure lookup. No process activation.

**Example:**

```lo
flow SignalNumber(result: Process.Result) -> Int! {
    return Process.Termination.signal(result.termination)
}
return 0
```

[Contract](../../specification.md#95-process-adapter)
