# Native Backend And Runtime Contract

P005 owns the native backend, runtime ABI, and platform decisions.
The selected design uses LLVM 21.1.8, LLD 21.1.8, and a precompiled Rust runtime.
The current implementation still generates Go source.
This document defines the replacement contract for I006 through F017.
The P005 acceptance checks passed on October 19, 2026.
The [final validation receipt](evidence/compiler-evolution/validation.json) records the repository checks and independent review.

## Backend Decision

Use LLVM 21.1.8 for native object generation.
Use its `ld64.lld` driver for Mach-O and its `ld.lld` driver for ELF.
Generate position-independent code for both targets.
Invoke the selected linker directly with explicit platform inputs.
Reject absent or incorrect inputs before compilation.
Keep one native backend after I011 removes Go generation.

Both LLVM and Cranelift passed the [four-cell experiment](../experiments/native-backends/qualification-20261019.json).
LLVM also supplied source-location information for the generated operation.
The Cranelift fixture did not emit this information.
LLVM supplies the selected debug representation and optimization controls in one toolchain.
The small experiment does not verify comparative performance for full Lo programs.
Cranelift is an experiment, without a production compiler route.

## Platform Selection

The [platform manifest](../experiments/native-runtime-abi/platform-inputs.json) identifies both host tool archives and their digests.
It also identifies the target libraries, original macOS descriptions, and dependency inventories.

| Target and host | Replacement distribution minimum | Link inputs |
| --- | --- | --- |
| `aarch64-apple-darwin` | macOS 27.0.1, Darwin 27.0.0 | Repository-authored TAPI descriptions and a static Lo runtime. |
| `aarch64-unknown-linux-gnu` | Linux 6.12.110, glibc 2.36 | Debian glibc startup objects, libc descriptions, GCC support, and a static Lo runtime. |

The macOS minimum is an explicit replacement-distribution decision.
It is narrower than the current Metal contract, which accepts macOS 27 or later.
The current compiler behavior is unchanged.
The link deployment target is `27.0`.
The experiment executed on macOS 27.0.1 and Linux 6.12.110.
Earlier OS patches do not have replacement acceptance evidence.

Both arm64 hosts must compile software for both targets.
Metal compilation is local to the Darwin host and target.
The current experiment generated both targets on Darwin.
The Linux-host tool archive is available and its ELF dependencies were examined.
F017 must qualify installed compiler execution on the Linux host.
This host qualification does not use the Darwin result as substitute evidence.

The selected [Linux LLVM archive](evidence/native-runtime-abi/linux-host-download.json) contains LLVM and LLD 21.1.8.
Its examined requirements include glibc 2.34 symbols and `GLIBCXX_3.4.30`.
The selected Debian libraries use glibc 2.36 as their minimum version.
The [dependency inventory](evidence/native-runtime-abi/linux-host-dependencies.json) pins their package and source identities.

| Linux host component | Selected package version |
| --- | --- |
| glibc | `2.36-9+deb12u14` |
| GCC support and libstdc++ | `12.2.0-14+deb12u1` |
| libxml2 | `2.9.14+dfsg-1.3~deb12u6` |
| ICU | `72.1-3+deb12u1` |
| liblzma | `5.4.1-1+deb12u1` |
| zlib | `1:1.2.13.dfsg-1` |

Supply non-base host libraries inside the compiler distribution.
Use the declared operating-system loader, libc, and libm at execution.
Keep application runtime dependencies separate from compiler host dependencies.
Linux applications can use the declared libc and GCC support libraries without Lo installed.
Darwin applications use the operating system's libSystem and selected frameworks.
Do not ship Apple SDK files.

The [notice inventory](../experiments/native-backends/notices/inventory.json) records source provenance and kept license notices.
The additional Linux inventory keeps package copyright notices and source archive digests.
Include the necessary notices and corresponding source materials during F017 packaging.
Keep QEMU, its guest kernel, and experiment helpers outside the production compiler package.

## Runtime Boundary

Compile the runtime with Rust 1.85.1 and `panic=abort`.
Supply one static runtime archive for each target.
Keep runtime source, dependency sources, and preparation records inside Lo.
Do not invoke Rust or Cargo during an installed user build.
Keep the compiler frontend as a prebuilt Go executable.
The frontend can execute without an installed Go toolchain.

The runtime exports a C ABI with opaque value and context pointers.
Both selected targets use 64-bit pointers and little-endian byte order.
Generated code must not inspect private Rust layouts.
Use fixed-width integers for ABI fields.
Use an output pointer for every operation result.

| Result field | Layout | Contract |
| --- | --- | --- |
| `status` | Unsigned 32-bit integer, offset 0 | `0` is success, `1` is a recoverable Lo error, `2` is a terminal host failure. |
| `code` | Unsigned 32-bit integer, offset 4 | A runtime diagnostic identity, with zero for success. |
| `value` | Opaque pointer, offset 8 | One owned successful value or error value. Null for a terminal host failure. |

The result occupies 16 bytes with 8-byte alignment.
Type identity, source locations, and diagnostic names use program-owned tables.
An error value keeps its kind, source context, and ordered trace frames.
A terminal host failure records its diagnostic in the execution context.
Generated code must branch on `status` before it uses the payload.
Unknown statuses and incorrect ABI identities are terminal build or runtime failures.

Use `lo-native-1` as the canonical ABI identity in the runtime and distribution manifests.
Reject a manifest with a different identity.
Generate operation declarations from the checked catalog with one fixed call convention:

```c
void lo_operation(lo_context *context,
                  const lo_value *const *arguments,
                  uint64_t argument_count,
                  uint64_t source_location,
                  lo_result *output);
```

Each operation has its own generated symbol and catalog-checked argument count.
Generated flows use the same convention.
The caller supplies the original source-location identifier.
The runtime validates externally supplied program metadata before it admits execution.

Use borrowed input handles for operation arguments.
Return one owned handle on success or recoverable failure.
Retain a handle before its owner releases it.
Release every owned handle on each completed control-flow path.
Use atomic reference counts for immutable values shared between request threads.

Keep physical allocation failures separate from recoverable Lo domain errors.
Do logical admission checks before allocation.
Keep current value weights independently of physical allocation size.

Represent arbitrary-precision integers, text, bytes, and containers inside opaque values.
Do not lower Lo integers to fixed-width arithmetic.
Keep canonical serialization and current collection order.
The ABI fixture uses an `i64` payload only to verify the boundary mechanics.
It does not select the production representation of Lo integers.

## Ownership And Cycle Audit

Current immutable containers own values that already exist when construction starts.
The [value constructors](../internal/value/value.go) do not publish partially constructed containers.
Parameters and optimizer states follow the same construction rule.
The [server value](../internal/value/target.go) stores flow identifiers without captured environments.
The [autodiff plan](../internal/autodiff/plan.go) stores node and release identifiers.
These properties make reference counting possible without a cycle collector for the current language.

Keep the following ownership graph:

```text
program -> immutable type, flow, and source tables
session -> execution contexts, adapters, resource ledger, active requests
request -> temporary values and tape storage
value -> immutable child values
tape -> indexed nodes and retained input values
callback -> flow identifier and borrowed context
```

Do not create strong value references to sessions, adapters, contexts, or tapes.
Do not let a session-owned callback retain its session.
Use identifiers or borrowed references for these reverse connections.
Keep tape nodes in session-owned storage with index references.
Join active request workers before session destruction.
Reject any future cyclic value feature until its ownership contract is specified.

## Callbacks And Thread Entry

Each execution context belongs to one host thread.
Reject entry through a context owned by another thread.
Create a different request context for each concurrent HTTP request.
Share only immutable program tables and retained immutable values between these contexts.
Keep capability grants and resource accounting within the owning session.

Generated callbacks use the same result convention as runtime operations.
Their argument handles stay borrowed for the duration of the call.
The callback returns one owned result handle.
Store only a flow identifier for a callback that executes after construction.
Resolve that identifier against the retained program when the event occurs.
Do not retain a callback's borrowed context after return.

Keep current handler restrictions, flow depth, cancellation checks, and logical charges.
Return recoverable callback errors through the selected Lo handler path.
Stop execution after a terminal host failure.
Do not unwind through a C ABI call in either direction.
Do not use a panic as a recoverable Lo error.
An internal invariant failure aborts the process under the selected Rust profile.

## Device And Adapter Lifetimes

Keep the [device scope](../internal/device/device.go) release points in the lowering plan.
Move escaping resource roots into their enclosing scope before scope destruction.
Release temporary allocations after each completed callback iteration.
Release the remaining scope after success, error, cancellation, or denied admission.
Keep tensor ledger release independent of the last general value reference.
Validate session identity before use of a resource identifier.

Keep opened-root file confinement and explicit commit order.
Keep process grants, limits, receipts, termination, and descendant confinement.
Keep the current process availability and unavailable errors on each platform.
The current process host supports Darwin only.

On Darwin, link the process launcher into each application executable as a private entry mode.
Use the same executable and the private bootstrap channel for confined process startup.
Keep public process configuration and validate the executable identity before entry.
Do not add a Lo installation dependency to compiled applications.

Keep request storage bounded during repeated service requests.
Keep explicit device selection without automatic CPU substitution.

## Numeric And Optimization Contract

Use the current CPU and Metal numeric profiles without changes to their comparison rules.
Keep finite binary64 values and the existing FP32 operation boundaries.
Keep specified rounding, intermediate checks, and reduction order explicit.
Do not use reassociation, reciprocal estimates, or contraction that changes specified results.
Do not add LLVM fast-math flags.

Keep profile-specific elementary functions inside the pinned runtime implementation.
Do not substitute a platform math function without evidence of the specified numeric behavior.

Emit logical charges, admission checks, cancellation checks, reads, and commits as ordered runtime calls.
Do not mark these calls as pure or speculatable.
Do not remove a charge when an optimization removes arithmetic.
Keep cleanup edges on both successful and failed control flow.

Disable link-time optimization for the first native runtime contract.
Use LLVM `-O0` for the initial I006 implementation.
Qualify `-O2` against the same independent expectations before enabling it.
Retain the current 180-second, 2-GiB, and 64-MiB build limits.

Use DWARF source locations for generated flows and runtime boundaries.
Emit the original Lo file, line, and column on each relevant instruction.
Keep source tables available for runtime error traces independently of debug information.
The [debug probe](../experiments/native-backends/qualification-debug.json) verifies locations in generated LLVM objects.
It does not verify source debugging for a full Lo application.

## ABI Evidence

The [ABI experiment](evidence/native-runtime-abi/experiment.json) passed on both targets on October 19, 2026.
The Darwin host ran the macOS executable directly.
QEMU TCG ran the Linux executable in a software guest.
Both routes used the backend experiment's filesystem and network confinement.
The guard verified denied network access and denied writes outside execution state.
It also verified that descendants kept those restrictions.

The [fixture](../experiments/native-runtime-abi/contract.h) checks the result layout at compile time.
Its independent expectation covers callbacks, retained values, thread ownership, budget failure, recoverable errors, and full cleanup.
The preparation receipt binds each archive to its runtime source, Rust identity, target, and compile flags.
The public checks reject changed source, changed archives, and changed compile contracts.

The macOS platform fixture links Foundation, Objective-C, Metal, and `sandbox_init` through repository-authored TAPI descriptions.
It calls Foundation and Objective-C entry points and resolves the other function addresses.
It does not create a Metal device or execute a kernel.
The descriptions contain necessary symbol declarations, without Apple SDK headers or copied SDK descriptions.
Lo supplies these original files under its repository license.
The operating system supplies the referenced Apple libraries at execution.

## ABI Experiment Procedure

1. Prepare the pinned backend inputs inside Lo as described in the [backend report](native-backend-experiment.md).
2. Supply the pinned Rust 1.85.1 compiler and both target standard libraries inside Lo.
3. Prepare the source-bound runtime archives:

   ```bash
   python3 -B experiments/native-runtime-abi/prepare.py \
     --rust-prefix _scratch/i004-20261019/rust-prefix \
     --output _scratch/p005-20261019/prepared </dev/null
   ```

4. Add the pinned Linux `libgcc_s.so.1` to the Linux sysroot.
5. Record the resulting sysroot digest in the ABI input manifest.
6. Run the isolated link and execution checks:

   ```bash
   make -f Makefile -f experiments/native-runtime-abi/probe.mk experiment-runtime-abi \
     RUNTIME_ABI_INPUTS=_scratch/p005-20261019/abi-inputs.json \
     RUNTIME_ABI_LIBRARIES=_scratch/p005-20261019/prepared/runtime-libraries.json </dev/null
   ```

7. Read `_scratch/native-runtime-abi/result.json`.
8. If qualification fails, keep the failed stage and correct its owning input or implementation.

Runtime preparation precedes offline qualification.
The offline command uses the prepared archives without Rust, Cargo, Go, or SDK discovery.
The [Linux support receipt](evidence/native-runtime-abi/linux-support-inputs.json) identifies the necessary GCC packages.
The [Rust receipt](evidence/native-runtime-abi/rust-linux-std.json) identifies the additional Linux standard library.

## Remaining Acceptance

I006 implements the selected lowering contract and source maps.
I007 through I010 implement the full runtime and adapters.
F017 supplies and qualifies the installed distribution on both hosts.
I011 verifies full semantic parity and removes Go generation.
The current experiments do not satisfy the user's full standalone compiler criterion.
