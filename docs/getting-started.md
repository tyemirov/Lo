# First Lo Program

[Documentation](README.md) · [Philosophy](philosophy.md) · [Syntax](syntax.md) · [Operations](operations/README.md)

A Lo program can calculate values and return a result.

## Calculate A Total

The existing [list-sum example](../examples/list_sum.lo) contains:

```lo
numbers = List.new(Int.new(1), Int.new(2), Int.new(3), Int.new(4))
total = numbers |> Math.sum()
return total
```

The first line constructs a list of four `Int` values and binds it to `numbers`.
The second line calculates their sum and binds it to `total`.
Both bindings are immutable.

The pipe operator `|>` supplies its left value as the first argument to the call on its right.
Thus, `numbers |> Math.sum()` has the same meaning as `Math.sum(numbers)`.
The result is `10`.

## Return The Program Result

At the top level, `return total` gives `10` to the host.
The host is the application that executes the Lo program.
It receives the returned value as the program result.

The CLI displays the program result as text by default:

```text
10
```

This example returns the integer `10` and exits with status `0`.

## Add A Named Flow

```lo
flow AddOffset(offset: Int, value: Int) -> Int {
    return Math.add(offset, value)
}

return List.new(1, 2, 3) |> List.map(10, AddOffset)
```

The result is the list `11, 12, 13`.
`List.map` supplies the explicit context `10` before each element.
For each element, `AddOffset` returns its result to `List.map`.
The final return gives the complete list to the host.
The callback is a pure named flow.
The [syntax guide](syntax.md#flows-and-callbacks) explains flow signatures and callback rules.

## Observe And Activate Explicitly

Pure calculations need no File or Process grants.
Host observations and target activation have separate authority requirements.
For example, constructing `File.Write` does not write a file.
Only a reached `commit` statement attempts the write under the host's grant.

The [operation reference](operations/README.md) states the authority and failure behavior for each API.

## Continue

- [Language philosophy](philosophy.md)
- [Syntax guide](syntax.md)
- [Alphabetical operation index](operations/README.md#alphabetical-index)
- [Normative grammar](../specification.md#17-surface-grammar)
- [XOR tutorial](xor-neural-network.md)
- [Compiler installation and distribution](compiler-distribution.md)
- [Native compilation and execution](native-compilation-contract.md)
- [Development toolchain preparation](../README.md#build-and-validate)
