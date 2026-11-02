# Self-Contained Native Compiler Requirement

## Authority And Status

The user requires a compiled Lo compiler with no external software dependencies except the target OS loader and system libraries.
The compiler implementation language is unrestricted.
The compiler must convert Lo source into native machine code after its own build.
It does not need to be written in Lo or compile its own implementation source.

The current distribution does not satisfy the complete requirement.
P008 owns the implementation design.
The [selected design](self-contained-compiler-design.md) embeds the current backend and runtime inputs in each compiler executable.
F037 owns implementation and acceptance.
F017 and I011 record the earlier offline distribution work with separate packaged tools and runtime files.
Their closed status does not establish acceptance of a self-contained compiler executable.

## Required Outcome

The built compiler must contain its frontend, native backend, executable construction support, and necessary runtime and platform data.
Only the target OS loader and declared system libraries can supply external software dependencies.
Lo input source and the output workspace are ordinary compilation inputs, not compiler software dependencies.

1. Permit any implementation language for the compiler and its internal components.
2. Compile Lo source into native machine code and produce an executable for the selected supported target.
3. Preserve the complete implemented language, diagnostics, resource limits, explicit authority, and commit behavior.
4. Include all necessary non-OS compiler code and data in the compiler executable.
5. Prohibit dependence on separately supplied compilers, linkers, assemblers, shells, SDKs, language runtimes, or package managers.
6. Prohibit dependence on companion backend executables, runtime archives, manifests, or non-OS shared libraries outside the compiler executable.
7. Prohibit network retrieval and preexisting caches during compilation.
8. Declare the permitted OS loader, system libraries, and baseline for each supported host and target.
9. Preserve an existing output artifact if compilation fails or the caller cancels compilation.

Go, Rust, C, or LLVM code compiled into the executable is permitted.
A language runtime compiled into the executable does not require a separately installed language runtime.
Developer toolchains can produce the compiler initially.
The built compiler must operate without those producer tools or their files.

Temporary files can contain data derived from the compiler's embedded resources or the declared Lo inputs.
They must not introduce a dependency on a separate installation, source checkout, or downloaded component.
The design must record all internal components and temporary executable images in the compiler closure.
Copying a directory of separate tools does not satisfy the single-executable acceptance check.

## Current Implementation Gaps

| Component | Current source fact | Required change |
| --- | --- | --- |
| Frontend | [Compiler](../internal/compiler/compiler.go), [checks](../internal/check), and [lowering](../internal/nativeplan/lower.go) use Go. | Retain or change the implementation language without a host toolchain dependency. |
| Native backend | [Native builder](../internal/nativebuild/llvm_executable.go) invokes packaged LLVM and LLD. | Include all necessary backend and executable construction components in the compiler executable. |
| Runtime inputs | The builder reads separate runtime archives and platform inputs. | Embed the necessary runtime and platform data. |
| Package admission | [Packaging](../tools/lodistribution/package.py) supplies a separate manifest and tool inventory. | Include the necessary identities and data without companion files. |
| Runtime production | [Runtime build](../tools/nativeruntime/build.py) uses Cargo to produce a Rust archive. | Keep producer tools outside normal compiler execution and embed the required result. |
| Acceptance | [Installed checks](compiler-distribution.md#acceptance-environments) supply a compiler directory with separate tools. | Add isolated compilation with only the compiler executable and declared OS components. |

The Go frontend and Rust runtime source files do not violate the implementation-language requirement.
The remaining dependency work concerns the built compiler artifact and its execution.
No language rewrite or compiler self-rebuild is required for acceptance.

## Acceptance

1. Build the compiler with the selected producer tools and record its complete compiler closure.
2. Copy only the compiler executable and representative Lo source into a fresh isolated environment.
3. Supply only the declared OS loader and system libraries outside those inputs.
4. Deny access to producer tools, companion package files, source checkouts, preexisting caches, and network access.
5. Compile independent Lo programs into native executables through the public `lo build` command.
6. Execute the resulting programs in a second environment without the compiler or source files.
7. Verify results, diagnostics, effects, resource limits, cancellation, and output preservation against independent expectations.
8. Repeat after compiler relocation and with empty build state.
9. Exercise the complete language corpus and supported host and target matrix.
10. Verify that every non-OS library or tool used by the compiler originates from its embedded components.
11. Verify that external file, tool, library, SDK, cache, or network dependencies cause acceptance failure.
12. Record file access, child execution, and loaded libraries for the compiler and its descendants.

Use OS isolation and software emulation for acceptance.
An empty PATH alone does not establish dependency isolation.
A compiler that only emits another language and requires an external compiler to produce machine code fails acceptance.
A directory of bundled tools, a subset fixture, or a documentation change cannot close F037.

## Selected Implementation

P008 selects the Go frontend, LLVM 21.1.8, LLD 21.1.8, and the precompiled Rust runtime.
One executable embeds the host tools, necessary libraries, target runtime archives, platform inputs, and their identities.
Each compilation uses verified resources in a private temporary workspace.
The [implementation design](self-contained-compiler-design.md) defines payload admission, ownership, cleanup, OS dependencies, and executable-only acceptance.
F038 through F041 implement the ordered changes. F037 retains final qualification.
The design does not establish that the current compiler satisfies this contract.
