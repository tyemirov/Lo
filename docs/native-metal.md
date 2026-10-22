# Native Metal Artifacts

F014 adds explicit Metal selection to native artifact construction.
The artifact uses the same `metal-fp32` kernels and numeric profile as the Metal interpreter.
The [arithmetic contract](metal-arithmetic.md) defines its numeric behavior.

## Build And Execution

The [distribution guide](compiler-distribution.md) defines installation and build-state requirements.
Use the installed Darwin Metal package for native Metal compilation:

```sh
CI=1 PATH= LO_BUILD_ROOT="$PWD/build-state" \
  "$PWD/install/lo-darwin-arm64-metal/bin/lo" build examples/metal_arithmetic.lo \
  --device-backend metal --output "$PWD/bin/metal-example" --format json </dev/null
"$PWD/bin/metal-example" run --allow-device metal:0 --format json </dev/null
"$PWD/bin/metal-example" metadata </dev/null
```

The example assumes an installed package and an existing absolute build-state directory.
The installed compiler supplies LLVM, LLD, runtime archives, and platform inputs.
No Go, Rust, Xcode, Metal compiler, developer SDK, or network access is necessary for installed compilation.
The compiler links precompiled kernels and does not compile MSL during a program build.

The default backend is `software`, also available through `--device-backend software`.
Software artifacts support `darwin/arm64` and `linux/arm64`.
Metal artifacts require a `darwin/arm64` host and target.
The software package rejects Metal selection with `build.toolchain` because its manifest has no Metal runtime.
An unsupported host or target produces `build.unsupported-target` before tool execution.
Unknown backend names and duplicate options produce `cli.invalid-invocation`.

The executable needs macOS 27 or later and the system Metal and Foundation frameworks.
Authorized Metal computation requires the selected Apple7 feature support and unified memory.
No source checkout, Lo installation, Go installation, Xcode installation, or external kernel file is necessary for execution.
An unavailable device produces `Device.Unavailable` when the program requests authorized access.
Programs that do not access Metal need no device grant.
File, listen, and device grants remain execution inputs for `run` and `serve`.

## Developer Preparation

`make build-metal` prepares the kernel files and the Metal-enabled interpreter.
Go 1.27.1, Xcode 27.1 build 27A9275, macOS SDK 27.0, and Apple Clang 21.0.0 are necessary for this developer operation.
The producer verifies those versions and the kernel tools before construction.
The [Metal build contract](metal-transfers.md) owns the fixed kernel compilers and arithmetic flags.

`runtime/metal/kernels.metal` contains the kernel source.
The producer writes `runtime/metal/metal.metallib` and `runtime/metal/manifest.json`.
The manifest records the complete source list, file digests, and numeric profile.
`bin/lo-metal-build.json` records the producer inputs and output identities.
The interpreter embeds the same kernel library through `runtime/metal/embed.go`.

The [native device guide](native-devices.md) describes `make build-native-runtime-metal`.
That producer verifies the kernel files and compiles the Objective-C adapter with the pinned LLVM compiler.
It embeds the kernel bytes in the static native runtime.
The package producer supplies that runtime and its declared link inputs to the installed compiler.

## Runtime Manifest

The software runtime does not contain the production Metal adapter.
The Darwin Metal package includes a different runtime archive and a precompiled kernel library.
Its `toolchain.json` records the runtime, kernel, linker, platform inputs, and fixed numeric identity.
The link recipe adds Foundation, Metal, and Objective-C inputs to the software recipe.

Before tool execution, the native builder verifies the declared inputs and kernel identity.
It copies the selected tools and link inputs into its build directory.
The resulting executable includes the compiled kernel bytes.
The build does not generate Go source or use cgo.

Both configurations keep the [native build limits](native-compilation-contract.md#build-limits).
Cancellation stops the active tool process group.
Build failure leaves the previous artifact unchanged and removes temporary build files.
Publication uses a rename within the output directory.

## Artifact Metadata

The `metadata` command returns one JSON object.
All artifacts record `device_backend` as `software` or `metal`.
The source, compiler, LLVM toolchain, target, and selected runtime digest identify the artifact build.

Metal artifacts also record these fields:

| Field | Meaning |
| --- | --- |
| `runtime_sha256` | Digest of the selected static runtime archive. |
| `metal.profile` | Numeric profile, currently `metal-fp32`. |
| `metal.xcode`, `metal.sdk`, `metal.clang` | Tool identities from the kernel producer. |
| `metal.metal_compiler`, `metal.kernel_linker` | Compiler identities for the supplied kernels. |
| `metal.deployment_target`, `metal.kernel_flags` | Deployment target and arithmetic configuration. |
| `metal.kernel_sha256` | Digest of the compiled kernel library. |

Metadata contains no grants or runtime device identity.
The producer manifest and receipt are not part of artifact metadata.

## Validation

Use `make test-native` with the selected `LLVM_TOOLCHAIN` for current native acceptance.
The [device guide](native-devices.md#verification) describes the software Metal driver and cross-target checks.
No physical device is necessary for these checks.
The historical F014 evidence below does not establish I011 qualification.

### Historical F014 Validation

The following tests and records describe the October 15, 2026 Go-generation implementation.
They do not describe the current LLVM build procedure.

The initial public build and artifact tests failed because `--device-backend` was absent.
The [software failure record](evidence/native-metal-red-software.txt) and [Metal failure record](evidence/native-metal-red-local.txt) preserve those results.

The local suite checks these contracts:

- Execute an artifact after source removal with an empty command search path and an unrelated working directory.
- Build through a relocated compiler with conflicting environment inputs.
- Compare artifact bytes from repeated builds with identical inputs.
- Compare interpreter and native results across three runs and three execution budgets.
- Verify signed zero, subnormal arithmetic, fixed reductions, non-square matrix products, AdamW, and 200-step SGD.
- Compare reverse-mode error traces and blocked subsequent File commits.
- Verify metadata against the interpreter kernel identity and included runtime archives.
- Inject an unavailable framework device into both executables and compare their errors.
- Preserve previous artifacts after target rejection, toolchain failure, cancellation, diagnostic overflow, and artifact overflow.
- Build default software artifacts for both selected targets through the Metal-enabled compiler.

The unavailable-device test replaces the framework device constructor through the [Apple dyld interface](https://github.com/apple-oss-distributions/dyld/blob/main/include/mach-o/dyld-interposing.h).
This test changes only the framework boundary.
The public CLI, generated program, device session, and error serialization remain real.
I002 supplies [complete training acceptance](metal-training.md), the maintained example, and its evidence record.

## Recorded Results

The October 15, 2026 [local Metal suite](evidence/native-metal-local.txt) passed with the Go race detector.
The environment used an Apple M5 Pro and macOS 27.0.1 build 26A434.
The native results matched the interpreter across all selected numeric fixtures and execution budgets.
Three 200-step SGD executions produced the exact final loss `0.00013215476064942777`.
The forced unavailable-device test produced matching `Device.Unavailable` errors and traces.

The [Linux software tests](evidence/native-metal-linux.txt) passed in a network-disabled `golang:1.27.1` container on `linux/arm64`.
They covered backend rejection, unavailable Metal access, standalone execution, cross-target construction, cancellation, compiler failure, and build limits.
The [software artifact race test](evidence/native-metal-software-race.txt) also passed on macOS.
The [tagged static check](evidence/native-metal-vet.txt) reported no diagnostics.

The [reproducibility regression](evidence/native-metal-reproducibility-red.txt) detected differing linker identifiers for identical inputs.
The F014 builder calculated a `-buildid` value from generated code and metadata for Metal artifacts.
The [Go linker](https://go.googlesource.com/go/+/9922fb1d248467741150f62b8b6ca9b56c05176e/src/cmd/link/internal/ld/macho_update_uuid.go) used that value to replace its external linker identifier.
The [repeated build check](evidence/native-metal-reproducibility.txt) and final local suite passed with this correction.

The [compiler receipt](evidence/native-metal-build.json), [runtime manifest](evidence/native-metal-manifest.json), and [artifact metadata](evidence/native-metal-artifact.json) record the selected inputs.
The [example result](evidence/native-metal-result.json) records standalone arithmetic and SGD execution.
The [final CI run](evidence/native-metal-ci.txt) passed bundle verification, formatting, static analysis, software tests, native tests, and compiler construction.
The [documentation review](evidence/native-metal-review.json) records the changed prose, local links, issue identifiers, and preserved findings.
