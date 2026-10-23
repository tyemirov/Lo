# Native Backend Experiment

I004 verifies native toolchain feasibility for the [standalone compiler plan](standalone-compiler-plan.md).
All four backend and target cells completed their acceptance checks on October 19, 2026.
The experiment uses bounded fixtures rather than Lo source.
The current compiler uses LLVM and the static Rust runtime.
I011 removed Go generation.
The [runtime contract](native-runtime-contract.md) records the selected replacement design.

## Current Results

The Darwin arm64 host generated and linked both target formats.
It executed the Darwin artifacts directly.
QEMU TCG executed the Linux artifacts in a Linux 6.12.110 software guest.
The [qualification receipt](../experiments/native-backends/qualification-20261019.json) records commands, digests, input identities, and specified outputs.

| Backend | Target | Object | Direct LLD link | Execution | Platform inputs |
| --- | --- | --- | --- | --- | --- |
| LLVM 21.1.8 | `aarch64-apple-darwin` | Passed | Passed | Passed | Lo-authored libSystem description. |
| Cranelift 0.116.1 | `aarch64-apple-darwin` | Passed | Passed | Passed | Lo-authored libSystem description. |
| LLVM 21.1.8 | `aarch64-unknown-linux-gnu` | Passed | Passed | Passed | Debian glibc 2.36 startup objects and libraries. |
| Cranelift 0.116.1 | `aarch64-unknown-linux-gnu` | Passed | Passed | Passed | Debian glibc 2.36 startup objects and libraries. |

All four cells used LLD 21.1.8.
Clang 21.1.8 compiled the C fixture from repository-local inputs.
Cranelift used its direct object API, with dependencies fixed by `Cargo.lock`.
Rust 1.85.1 built the Cranelift emitter during preparation.
The offline commands invoked no Go, Cargo, Rust compiler, Apple linker, or SDK lookup.

The [ABI experiment](evidence/native-runtime-abi/experiment.json) also passed both target links and executions.
It adds Rust archive ownership, thread entry, callback errors, budget failure, and full cleanup.
Its Darwin platform fixture links Foundation, Objective-C, Metal, and process-sandbox entry points without SDK headers.
It does not execute a Metal kernel.
No physical device is necessary.

## Probe Contract

The [LLVM fixture](../experiments/native-backends/probe.ll) and [Cranelift emitter](../experiments/native-backends/cranelift/src/main.rs) implement the same fixed-width operation.
For a nonnegative input, generated code multiplies the input by three and calls `lo_runtime_adjust`.
That [C runtime helper](../experiments/native-backends/runtime.c) allocates storage, invokes a callback, and releases storage.
The callback adds two.
For a negative input, generated code returns zero with status seven without the runtime call.

The caller supplies `7` and `-1`.
The independent expectation is:

```text
23 0
0 7
runtime_calls 1
```

This probe verifies arithmetic, branching, generated-to-runtime calls, allocation, callbacks, release, and error status.
Its `printf` call also uses the platform C library.
The fixture does not verify arbitrary-precision integers, full Lo errors, or native Lo lowering.
Neither emitter consumes the Lo checked graph.

The [initial runtime-call failure](../experiments/native-backends/runtime-call-red.json) exposed a Cranelift relocation error.
Position-independent code corrected the Darwin runtime call.
The [path diagnostic](../experiments/native-backends/sandbox-path-red.json) identified another QEMU startup failure.
The [corrected diagnostic](../experiments/native-backends/sandbox-path-corrected.json) verified the necessary path metadata access.

## Offline Boundary

The harness uses explicit pinned inputs inside Lo.
It rejects symlinks, external input paths, changed digests, and stale preparation.
It does not use Docker or download missing tools during qualification.
Each command uses an empty `PATH`, closed standard input, `CI=1`, and local execution state.

The Darwin sandbox gives read access to Lo and the operating system's runtime library directories.
It gives write access only inside the experiment's execution directory.
It denies network access to the command and its descendants.
Specified ancestor paths give metadata access for path resolution.
The root directory has a literal directory read grant without a recursive content grant.
These rules let QEMU start without access to external development tools.

The guard verifies denied network access and denied writes to a protected sentinel.
It also verifies the descendant restrictions and authorized writes inside execution state.
The guard must pass before all four cells.
Filtering `PATH` alone does not supply this evidence.
The host still has developer tools installed outside the authorized input roots.
This experiment does not verify a clean installed Lo distribution.

Preparation uses installed tools to build the experiment's runner and guard.
The [preparation receipt](../experiments/native-backends/preparation-commands.json) records those commands and the observed Apple SDK 27.0.
That SDK use occurs before offline qualification.
The offline C compilation and direct LLD links use only the declared local inputs and operating-system runtime libraries.

## Measurements

These measurements describe one small probe on the available host.
They do not verify a performance ranking for full Lo applications.
LLVM objects include generated-function DWARF, while Cranelift objects do not.
The compared configurations contain different debug information.

| Backend and target | Object generation, seconds | Link, seconds | Executable bytes | Emitter peak RSS, bytes |
| --- | ---: | ---: | ---: | ---: |
| LLVM Darwin | 0.036758 | 0.031121 | 34,304 | 27,803,648 |
| Cranelift Darwin | 0.016383 | 0.022411 | 34,080 | 3,686,400 |
| LLVM Linux | 0.024208 | 0.026037 | 8,960 | 28,033,024 |
| Cranelift Linux | 0.015265 | 0.024743 | 8,288 | 3,702,784 |

The [memory receipt](../experiments/native-backends/qualification-memory.json) records additional instrumented commands with unchanged artifact digests.
Linker peak RSS ranged from 16,269,312 to 18,137,088 bytes.
Linux execution times include software-emulator startup.
The experiment did not measure emulator peak memory.

The prepared tool bundle contains 955,199,772 logical bytes, including repeated linker aliases and examination tools.
This total is not the necessary size of a production compiler package.
The Darwin sysroot contains 274 bytes.
The Linux sysroot contains 35,454,059 bytes.
The [debug receipt](../experiments/native-backends/qualification-debug.json) verifies source lines in the generated LLVM objects on both targets.
It does not qualify distributed debug companions or application debugger behavior.

## Inputs And Provenance

The [preparation lock](../experiments/native-backends/preparation.lock.json) records 46 archive URLs and SHA-256 digests.
The [notice inventory](../experiments/native-backends/notices/inventory.json) records licenses, upstream sources, and kept notices.
The [crate inventory](../experiments/native-backends/notices/crates/inventory.json) records the 40 locked crate entries and their notices.
Some locked crates apply only to optional target configurations.

The selected backend and linker originate from the official LLVM 21.1.8 release.
The Linux target inputs originate from Debian glibc packages.
Lo authors the Darwin TAPI description from necessary public ABI symbol names.
It contains no copied Apple SDK text.
The repository license covers this original description.
The operating system supplies libSystem when the executable runs.

QEMU, its dependencies, the guest kernel, and isolation helpers are validation inputs.
Rust and Cargo are preparation inputs.
These components do not become installed user-build requirements.
The [platform manifest](../experiments/native-runtime-abi/platform-inputs.json) selects the full host and target input families for P005.

## Reproduction

1. Record the preparation directories and `_scratch/native-backend-probe` in the task plan.
2. Prepare the specified archives from the preparation lock inside Lo.
3. Use the preparation receipt for extraction, local assembly, and helper compilation.
4. Record tool, bundle, and sysroot digests in the input manifest.
5. Run the public harness checks:

   ```bash
   make check-native-backend-probe </dev/null
   make test-native-backend-probe </dev/null
   ```

6. Validate the prepared inputs and execute the offline matrix:

   ```bash
   make prepare-native-backend-probe experiment-native-backends \
     NATIVE_BACKEND_INPUTS=_scratch/i004-20261019/inputs.json </dev/null
   ```

7. Read `_scratch/native-backend-probe/result.json`.
8. If a stage fails, keep its result and correct its owning input or implementation.

The [input example](../experiments/native-backends/inputs.example.json) defines the manifest shape.
Its placeholder values do not constitute prepared inputs.
The preparation Make target validates existing inputs without retrieval or installation.
The source-controlled preparation receipt gives the assembly sequence.
The public harness has 21 passing CLI and containment tests.

Each target runner accepts an executable and its pinned sysroot.
The runner keeps guest inputs and temporary output inside Lo.
It verifies the guest exit status and specified application output.
The current evidence comes from the repository Make targets.

## Historical Evidence

The [October 16 receipt](evidence/native-backends/experiment.json) records an earlier four-cell smoke experiment.
Those links used Apple and GNU linkers, an Apple SDK, and a prepared Linux development container.
They did not qualify LLD or generated-to-runtime calls.
They do not supply current acceptance evidence.

The [historical compiler baseline](evidence/native-backends/current-lo-no-go.json) records the former missing-Go diagnostic.
The former `lo build` returned status `2` with `build.toolchain` when `PATH` excluded Go.
That historical check does not describe the current installed compiler.

## Remaining Acceptance

I004's bounded experiment criteria have current passing evidence.
The [final repository validation](evidence/compiler-evolution/validation.json) passed on October 19, 2026.
The I004 acceptance checks passed.

I006 connected LLVM to Lo lowering.
I007 through I010 implemented runtime values, numerical operations, adapters, and device execution.
F017 completed offline acceptance for both installed compiler hosts.
I011 owns semantic and performance qualification after removal of Go generation.

The [compiler plan](standalone-compiler-plan.md) records the current status.
