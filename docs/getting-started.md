# First Lo Program

[Documentation](README.md) · [Philosophy](philosophy.md) · [Syntax](syntax.md) · [Operations](operations/README.md)

This guide uses the repository checkout.
All source files and build output in the procedure stay inside that checkout.
The [distribution guide](compiler-distribution.md) defines compiler packages and supported targets.
The [repository README](../README.md#build-and-validate) defines developer toolchain preparation.

## Prepare The Compiler

For a source checkout with the documented Go toolchain, build the frontend:

```sh
make build
```

The resulting `bin/lo` supplies `parse`, `check`, `inspect`, and `build`.
Parsing, type checking, and static inspection do not execute the application.
Executable construction also needs the native toolchain described in the preparation guide.

## Read A Complete Program

The existing [list-sum example](../examples/list_sum.lo) contains:

```lo
numbers = List.new(Int.new(1), Int.new(2), Int.new(3), Int.new(4))
total = numbers |> Math.sum()
return total
```

`numbers` and `total` are immutable bindings.
`List.new` constructs a homogeneous list of integers.
`|>` passes the list as the first argument to `Math.sum`.
The final `return` supplies the program result, which is `10`.

## Type Checking And Inspection

From the repository root, execute:

```sh
./bin/lo check examples/list_sum.lo --format json
./bin/lo inspect examples/list_sum.lo --format json
```

Type checking verifies references, argument types, fallibility, and transitive flow effects.
Static inspection reports the program's operations and possible host requirements.
Neither command evaluates the calculation or activates a target.
If a command reports a diagnostic, correct the source before compilation.

## Compile And Execute

After native toolchain preparation, execute:

```sh
mkdir -p _scratch/tutorial
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo build examples/list_sum.lo \
  --toolchain _scratch/native-runtime/distribution/toolchain.json \
  --output _scratch/tutorial/list-sum
./_scratch/tutorial/list-sum run --format json
```

The compiler produces a native executable.
That executable's `run` command returns the integer `10` in the result envelope.
An installed compiler distribution uses its packaged toolchain, as described in its guide.

## Add A Named Flow

```lo
flow AddOffset(offset: Int, value: Int) -> Int {
    return Math.add(offset, value)
}

return List.new(1, 2, 3) |> List.map(10, AddOffset)
```

The result is the list `11, 12, 13`.
`List.map` supplies the explicit context `10` before each element.
The callback is a pure named flow.
The [syntax guide](syntax.md#flows-and-callbacks) explains flow signatures and callback rules.

## Observe And Activate Explicitly

Pure calculations need no File or Process grants.
Host observations and target activation have separate authority requirements.
For example, constructing `File.Write` does not write a file.
Only a reached `commit` statement attempts the write under the host's grant.

The [operation reference](operations/README.md) states the authority and failure behavior for each API.
The [adapter guide](native-adapters.md) explains executable grant options.
The [embedding guide](native-embedding.md) defines the narrower retained C ABI.

## Continue

- [Language philosophy](philosophy.md)
- [Syntax guide](syntax.md)
- [Alphabetical operation index](operations/README.md#alphabetical-index)
- [Normative grammar](../specification.md#17-surface-grammar)
- [XOR tutorial](xor-neural-network.md)
