# Lo Philosophy

[Documentation](README.md) · [First program](getting-started.md) · [Syntax](syntax.md) · [Operations](operations/README.md)

Lo makes a program's values, dependencies, failures, and host authority explicit.
Its small language core supports file workflows, services, numeric computation, and other applications through the same contracts.
The [design principles](../specification.md#2-design-principles) define the normative foundation.

## Programs Describe Dependencies

A flow is a named graph template.
Expressions produce values, and later expressions use those values as inputs.
The compiler checks references, types, callback effects, and failure propagation before execution.

```lo
flow Double(value: Int) -> Int {
    return Math.mul(value, 2)
}

numbers = List.new(1, 2, 3)
return numbers |> List.map(Double) |> Math.sum()
```

The result is `12`.
The pipe supplies its left value as the next call's first argument.
The program names the calculation without mutable counters or loop statements.

Graph structure does not permit arbitrary reordering of host operations.
The runtime preserves the specified evaluation order and data dependencies.
[Evaluation order](../specification.md#233-evaluation-order) defines these rules.

## Values Remain Immutable

A binding names a value once.
A flow cannot change a caller's binding or the contents of an existing value.
Collection processors and optimizer steps return new values.
The implementation can share storage when that choice preserves value semantics.

```lo
original = Record.new(count: 1, label: "initial")
updated = Record.merge(original, Record.new(count: 2))
return Record.new(before: original.count, after: updated.count)
```

The result keeps `before: 1` and `after: 2`.
Storage addresses, pointer identity, and ownership transfers are not Lo operations.
[Value passing](../specification.md#57-binding-and-value-passing) defines the observable contract.

## Construction And Activation Are Separate

A target describes a requested action.
Construction does not execute that action.
An explicit `commit` statement activates a bound target.

```lo
return File.Write.new(File.Path.new("_scratch/tutorial/message.txt"), Bytes.new("hello"))
```

This program returns an inert target.
It does not create or write the file.
An application can inspect, select, or pass the target before activation.

Host observations are a separate category.
For example, `File.read` observes a file when evaluation reaches the call.
It needs explicit read authority, but no `commit` statement.
The [File reference](operations/files.md) distinguishes observations from targets.

Commit sequences are not transactions.
A later failure does not reverse an earlier completed action.
Applications keep confirmed progress and select their recovery policy explicitly.
[Activation rules](../specification.md#236-file-and-server-activation) define this boundary.

## Failures Have Types

`T!` means that an expression can produce a value of type `T` or a recoverable Lo error.
A fallible flow propagates an unresolved failure to its caller.
A self-handling flow resolves that failure through a declared pure handler.

```lo
flow DivisionFailed(error: Error) -> Int {
    return -1
}

flow Divide(left: Int, right: Int) -> Int onError DivisionFailed {
    return Math.div(left, right)
}

return Divide(8, 0)
```

This application's handler selects `-1` as its error result.
Lo does not select that policy for other applications.
Cancellation, host budget exhaustion, and unresolved resource ownership remain terminal host failures.
They do not become recoverable values through an ordinary handler.

## Authority Comes From The Host

Path text, command arguments, and a device selector describe data.
They do not grant access to a file, executable, endpoint, or device.
The host admits explicit capabilities and resource limits before an operation can use them.

Static inspection reports possible requirements without execution.
It does not replace runtime authorization or establish that an external resource remains unchanged.
The [adapter guide](native-adapters.md) explains grants and platform availability.

## Named Flows Make Dependencies Visible

Callbacks use named flows with declared parameter and result types.
Required context is an explicit argument.
A nested flow does not capture its surrounding bindings.
Pure collection callbacks cannot observe the host or activate commits.

`Flow.fold` and `Flow.worklist` support sequential effectful workflows under their explicit callback rules.
This distinction keeps pure calculations separate from host actions.
[Collections](operations/collections.md) describes both processor families.

## Application Policy Stays In Applications

File ages, filenames, command lists, reports, and application exit decisions belong in Lo flows.
Language operations provide behavior that applies across applications.
A cleanup tool, an indexer, and a service can use the same file and process contracts.

Lo's current limits are explicit.
The implemented language has no modules, imports, user-defined type declarations, recursive flows, closures, or concurrency syntax.
Built-in parameterized types such as `List<Int>` do not imply user-defined generics.
The [planned features](../specification.md#planned-features) remain separate from current syntax and operation entries.
