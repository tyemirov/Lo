# Lo Documentation

Lo is a typed, general-purpose dataflow language.
Programs describe values, their dependencies, recoverable failures, and explicit activation of host operations.
The compiler checks these contracts before the native runtime executes a program.

This documentation describes the implemented v0.4 language.
The [language reference](../specification.md) defines the normative contract.
Proposed features remain outside the operation reference until the compiler and runtime implement them.

## Learn Lo

| Question | Guide |
| --- | --- |
| Why does Lo use immutable values, named flows, and explicit commits? | [Philosophy](philosophy.md) |
| How do I write a first program and understand its result? | [First program](getting-started.md) |
| How do I write expressions, types, flows, branches, and handlers? | [Syntax guide](syntax.md) |
| What does each built-in operation accept and return? | [Operation reference](operations/README.md) |
| What are the exact grammar and semantic rules? | [Language reference](../specification.md) |

## Find An Operation

The operation reference groups built-in APIs by subject.
Each operation has a signature, behavior, failure contract, authority requirements, example, and link to its detailed contract.

| Subject | Reference |
| --- | --- |
| Atomic values and arithmetic | [Values](operations/values.md) |
| Unicode text, bytes, and globs | [Text](operations/text.md) |
| Immutable JSON values and exact extraction | [JSON](operations/json.md) |
| Lists, maps, records, ranges, and sequential flows | [Collections](operations/collections.md) |
| File paths, observations, and activation targets | [Files](operations/files.md) |
| Process contexts, executables, commands, and results | [Processes](operations/process.md) |
| HTTP values, clients, and servers | [HTTP](operations/http.md) |
| Invocation inputs, CLI options, Console output, exit status, and locks | [Applications](operations/applications.md) |
| FP32 tensors and explicit devices | [Tensors](operations/tensors.md) |
| Autodiff, Parameters, gradients, optimizers, and checkpoints | [Training](operations/training.md) |

`Match.new` is a [language expression](syntax.md#match-expressions), not an ordinary built-in call.
Record projections and `commit` also have language rules rather than operation entries.

## Tools And Host Integration

| Task | Guide |
| --- | --- |
| Prepare the compiler distribution and select a target | [Compiler distribution](compiler-distribution.md) |
| Compile a native executable | [Native compilation](native-compilation-contract.md) |
| Supply explicit File, HTTP, and Process grants | [Native adapters](native-adapters.md) |
| Embed a compiled Lo entry in another application | [Native embedding](native-embedding.md) |
| Use the retained native declarations | [Core C header](../runtime/native/include/lo_runtime.h) |
| Understand resource quotas and cancellation | [Host resource contract](../specification.md#89-dynamic-worklists-and-host-resource-quotas) |

The built-in APIs are the library available inside Lo programs.
The retained embedding API executes compiled entries through the native C ABI.
That API is not a complete host SDK for general File, HTTP, and real Process grant configuration.
The [embedding guide](native-embedding.md#explicit-host-boundaries) defines its supported boundaries.

## Examples And Tutorials

- [Small complete programs](../examples/)
- [XOR neural network](xor-neural-network.md)
- [Model checkpoints](model-checkpoints.md)
- [CPU and Metal support](../specification.md#backend-support)
- [Declared CLI example](examples/cli-cleanup-preview.lo)

Each operation example contains a complete Lo program.
Type checking an example does not execute its host operations or establish its runtime grants.
The File, Process, HTTP, and device pages describe the additional requirements for execution.

## Contract And Proposals

The language reference owns syntax and semantic requirements.
The guides explain those requirements, and the operation pages supply individual API entries.
Linked contract documents define the detailed boundaries that the reference delegates to them.
The [reference audit](language-reference-audit.md) connects language rules with implementation evidence.

The [planned features](../specification.md#planned-features) are separate from implemented behavior.
Application designs can contain proposed calls and need not be executable.
For example, the [mac-cleanup roadmap](mac-cleanup-implementation-plan.md) records work that remains outside the implemented contract.

## Documentation Changes

For an operation change, update its reference entry and detailed contract together.
Include argument types, result types, boundary failures, authority, and a complete example.
Keep each operation in the [alphabetical index](operations/README.md#alphabetical-index).
Verify each example with the public `lo check` command.
Verify local links and compare the entries with `catalog.All()` in the [built-in catalog](../internal/catalog/catalog.go).
Keep proposed operations outside the implemented index.
