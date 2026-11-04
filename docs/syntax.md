# Lo Syntax Guide

[Documentation](README.md) · [Philosophy](philosophy.md) · [First program](getting-started.md) · [Operations](operations/README.md)

This guide explains implemented Lo syntax with complete programs.
The [surface grammar](../specification.md#17-surface-grammar) and [lexical rules](../specification.md#171-lexical-rules) define the normative syntax.
The examples use no proposed operations.

## Source Text And Literals

Source text uses UTF-8 without a byte order mark.
Names are case-sensitive ASCII identifiers with letters, digits, and underscores.
The first character is a letter or underscore.
Reserved keywords and predeclared names cannot be user binding or flow names.

`//` starts a comment outside a string.
LF and CRLF end lines.
A newline or semicolon separates statements outside an enclosed expression.
Block comments are not supported.

```lo
// Each field has its own type.
return Record.new(count: 12, offset: -3, ratio: 1.5e-2, ready: true, absent: null, text: "Lo\n")
```

Integers use decimal notation.
Floats use a decimal fraction, an exponent, or both.
Floats must fit the finite binary64 representation.
Strings use double quotes and Unicode scalar values.
The lexical reference lists all escapes, including `\n`, `\t`, `\uXXXX`, and `\UXXXXXXXX`.

## Bindings And Returns

```lo
first = 7
second = Math.add(first, 5)
return second
```

A binding names a value once.
There is no assignment to an existing binding.
Each flow has exactly one final return statement.
A top-level return, when present, is also final.
[Statements](../specification.md#13-statements) defines each form.

## Types And Records

Atomic types are `Int`, `Float`, `Bool`, `Null`, `Scalar`, `String`, and `Bytes`.
Built-in composite types include `List<T>`, `Map<K, V>`, `Range<T>`, and exact records.
`T` and `V` here are documentation notation, not user-defined type aliases.

```lo
flow Describe(row: Record{name: String, count: Int}) -> String {
    return String.concat(row.name, ": ", Int.to_string(row.count))
}

return Describe(Record.new(name: "items", count: 3))
```

`Record.new` takes named fields.
An exact record must have precisely the declared fields and types.
`row.name` projects a known field from a bound record.
`null` has type `Null` and does not supply a missing field or inhabit another type implicitly.

Collection constructors can infer their element types from values.
An empty constructor needs an expected type:

```lo
flow EmptyNumbers() -> List<Int> {
    return List.new()
}

return EmptyNumbers()
```

Lo also has nominal types such as `File.Path` and `Process.Command`.
They are not interchangeable with records that have similar fields.
Their [operation entries](operations/README.md) explain construction and access.

## Calls And Named Arguments

Fixed signatures accept positional arguments, named arguments, or a positional prefix followed by named arguments.
A positional argument cannot follow a named argument.
Every parameter receives exactly one argument.
Argument evaluation follows source order.

```lo
return Math.sub(right_value: 2, left_value: 9)
```

The result is `7`.
Parameter names are part of each operation's documented signature.
`List.new`, `Map.new`, and `String.concat` accept positional arguments only.
`Record.new` accepts named fields only.
[Argument contracts](../specification.md#224-constructor-and-argument-contracts) define the complete rules.

## Pipes

`|>` supplies the expression on its left as the next call's first positional argument.
Pipes associate from left to right.

```lo
flow SelectNumber() -> Int! {
    return List.new(2, 4, 6)
        |> List.slice(start: 1, end: 3)
        |> List.at(0)
}

return SelectNumber()
```

The result is a fallible integer with successful value `4`.
A newline directly before or after `|>` continues the expression.
Named arguments after the supplied first argument follow the ordinary call rules.
[Pipes](../specification.md#14-pipe-operator) defines propagation and precedence.

## Flows And Callbacks

```lo
flow Multiply(factor: Int, value: Int) -> Int {
    return Math.mul(factor, value)
}

return List.map(List.new(1, 2, 3), 4, Multiply)
```

A flow declares typed parameters and one output type.
`Multiply` in the final argument is a named flow reference.
The callback receives its explicit context before the current list element.
Pure List callbacks cannot observe host state or activate commits, including through other flows.

Flows can appear inside other flows, but nested flows do not capture outer bindings.
Required context must be a parameter.
Recursive flow cycles and anonymous functions are not supported.
The [flow rules](../specification.md#10-flow-definitions) define purity and declaration scope.

## Match Expressions

```lo
flow IsNegative(value: Int) -> Bool {
    return Math.lt(value, 0)
}

return Match.new(4,
    when IsNegative: "negative",
    0: "zero",
    Range.new(1, 10, true, false): "small",
    _: "other"
)
```

The result is `"small"`.
Case heads can be exact values, ranges, or `when` followed by a pure total predicate flow.
The first matching case wins, and only its branch evaluates.
A default `_` case is mandatory.
Branches must have one compatible output type.

Case heads and predicates must be pure and total.
A selector or selected branch can observe host state but cannot activate a commit.
An application can select a target and commit it in the outer flow.
[Match](../specification.md#11-match) defines the complete contract.

## Fallible Flows

`T!` represents a value of type `T` or a recoverable `Error`.
It is not a nullable type.

```lo
flow DivideAndAdd(left: Int, right: Int) -> Int! {
    quotient = Math.div(left, right)
    return Math.add(quotient, 1)
}

return DivideAndAdd(8, 2)
```

Inside this fallible flow, successful division binds an `Int` to `quotient`.
A division failure propagates before the dependent addition evaluates.
A plain total flow cannot consume an unresolved fallible value this way.

An expected fallible element type can retain failures as data:

```lo
flow TaggedNumbers() -> List<Int!> {
    return List.new(Math.div(1, 0), Math.div(6, 2))
}

return TaggedNumbers()
```

`List<Int!>` stores fallible elements.
`List<Int>!` instead describes a fallible result containing a list of total integers.
[Fallible values](../specification.md#223-fallible-values) defines these distinctions.

## Error Handlers

```lo
flow DivideFailed(error: Error) -> Int {
    return -1
}

flow Divide(left: Int, right: Int) -> Int onError DivideFailed {
    return Math.div(left, right)
}

return Divide(8, 0)
```

`onError` belongs to the flow signature.
The handler is a pure total named flow with signature `(Error) -> Int` for this example.
The self-handling flow publishes a total result after success or handler resolution.
It cannot declare `Int! onError DivideFailed`.
There is no expression-level handler, `try`, `catch`, or `throw` syntax.

Host cancellation and resource failures remain terminal.
[Error handling](../specification.md#12-error-handling) defines recoverable boundaries and trace behavior.

## Commit Statements

```lo
flow WriteMessage() -> Int! {
    line = Console.Line.new("stdout", "hello", true)
    receipt = commit line
    return receipt.bytes_written
}

return WriteMessage()
```

Construction is inert.
`commit line` activates the bound target.
The bound form publishes its activation result only after success.
An unbound `commit line` also activates the target but creates no result binding.

Each evaluated commit makes one activation attempt.
Committing the same target again makes another attempt.
Earlier completed effects remain after a later failure.
A flow with commits must be fallible or self-handling.
The host supplies the required output authority in this example.
[Activation](../specification.md#52-commit) and [Console](operations/applications.md#consolelinenew) describe the related failure contracts.

## Syntax And Library Operations

Syntax defines bindings, flows, calls, projections, pipes, Match, handlers, and commits.
The [operation reference](operations/README.md) defines the built-in names used by those constructs.
The [formal grammar](../specification.md#17-surface-grammar) is the exact syntax authority.
The [planned features](../specification.md#planned-features) do not extend implemented syntax.
