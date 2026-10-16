# Native Metal Artifacts

F014 adds explicit Metal selection to native artifact construction.
The artifact uses the same `metal-fp32` kernels and runtime operations as the Metal interpreter.
The [arithmetic contract](metal-arithmetic.md) defines its numeric behavior.

## Build And Execution

1. Build the Metal-enabled compiler with `make build-metal`.
2. Build the artifact with the explicit backend option:

   ```bash
   ./bin/lo-metal build examples/metal_arithmetic.lo --device-backend metal --output ./bin/metal-example
   ```

3. Execute the artifact with its device grant:

   ```bash
   ./bin/metal-example run --allow-device metal:0 --format json
   ./bin/metal-example metadata
   ```

The default backend is `software`, also available through `--device-backend software`.
Both compilers use this default, regardless of source selectors or available devices.
Software artifacts support `darwin/arm64` and `linux/arm64` with cgo disabled.
Metal artifacts require a `darwin/arm64` host and target.
The standard compiler rejects a Metal build with `build.backend-unavailable` on that host.
An unsupported host or target produces `build.unsupported-target` before toolchain execution.
Unknown backend names and duplicate options produce `cli.invalid-invocation`.

Native Metal construction uses Go 1.27.1, Xcode 27.1 build 27A9275, macOS SDK 27.0, and Apple Clang 21.0.0.
The builder verifies these versions and sets the compiler, SDK, deployment target, and cgo configuration.
The kernel compilers and arithmetic flags remain fixed by the [Metal build contract](metal-transfers.md).
Native artifact construction uses the compiled kernels supplied by the compiler.
It does not compile MSL again.

The executable needs macOS 27 or later and the system Metal and Foundation frameworks.
Authorized Metal computation requires the selected Apple7 feature support and unified memory.
Execution needs no source checkout, Go installation, Xcode installation, or separate kernel files.
An unavailable device produces `Device.Unavailable` when the program requests authorized access.
Programs that do not access Metal need no device grant.
File, listen, and device grants remain execution inputs for `run` and `serve`.

## Runtime Manifest

The software runtime archive excludes the Metal adapter implementation.
`make build-metal` creates a separate archive from the explicit `metalconfig.RuntimeFiles` manifest.
This archive contains `metal_darwin.go`, `metal_bridge_darwin.m`, `metal_bridge.h`, `metal_types.h`, and `metal.metallib`.
Its generated manifest records each file digest and the pinned numeric configuration.
The Metal-enabled compiler includes both archives and the Metal manifest.

Before toolchain execution, the native builder verifies the Metal file set, file digests, kernel identity, and numeric configuration.
It extracts the selected files into its temporary build module.
Only the Metal artifact configuration enables the `metal` build tag and cgo.
The resulting executable includes the compiled kernel bytes.

Both configurations keep the [native build limits](native-compilation-contract.md#build-limits).
These limits cover toolchain probes and compilation.
Cancellation stops the active tool process group.
Build failure leaves the previous artifact unchanged and removes temporary build files.
Publication uses a rename within the output directory.

## Artifact Metadata

The `metadata` command returns one JSON object.
All artifacts record `device_backend` as `software` or `metal`.
The existing source, compiler, runtime bundle, Go toolchain, and target fields remain required.

Metal artifacts also record these fields:

| Field | Meaning |
| --- | --- |
| `metal_runtime_sha256` | Digest of the complete Metal runtime archive. |
| `metal_manifest_sha256` | Digest of its manifest. |
| `metal.profile` | Numeric profile, currently `metal-fp32`. |
| `metal.go`, `metal.xcode`, `metal.sdk`, `metal.clang` | Pinned native compiler identities. |
| `metal.metal_compiler`, `metal.kernel_linker` | Compiler identities for the supplied kernels. |
| `metal.deployment_target`, `metal.kernel_flags` | Deployment target and arithmetic configuration. |
| `metal.kernel_sha256` | Digest of the compiled kernel library. |

Metadata contains no grants or runtime device identity.
Runtime validation records the actual macOS version, device name, registry identifier, and kernel identity separately.

## Validation

Routine `make ci` includes software tests for backend selection, unavailable Metal access, standalone execution, and native build failures.
Software tests require no GPU.
The additional `make test-native-metal` target uses the explicit local Metal environment.
It rejects an unavailable toolchain instead of skipping tests.

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
The builder now calculates a `-buildid` value from generated code and metadata for Metal artifacts.
The [Go linker](https://go.googlesource.com/go/+/9922fb1d248467741150f62b8b6ca9b56c05176e/src/cmd/link/internal/ld/macho_update_uuid.go) uses that value to replace its external linker identifier.
The [repeated build check](evidence/native-metal-reproducibility.txt) and final local suite passed with this correction.

The [compiler receipt](evidence/native-metal-build.json), [runtime manifest](evidence/native-metal-manifest.json), and [artifact metadata](evidence/native-metal-artifact.json) record the selected inputs.
The [example result](evidence/native-metal-result.json) records standalone arithmetic and SGD execution.
The [final CI run](evidence/native-metal-ci.txt) passed bundle verification, formatting, static analysis, software tests, native tests, and compiler construction.
The [documentation review](evidence/native-metal-review.json) records the changed prose, local links, issue identifiers, and preserved findings.
