# Native Devices And Metal

I010 supplies device resources for the LLVM runtime.
The [device contract](device-resources.md) owns grants, transfers, failures, and resource limits.
The [Metal arithmetic contract](metal-arithmetic.md) owns the `metal-fp32` rules.
The [compiler plan](standalone-compiler-plan.md) owns distribution qualification and the language evolution sequence.

## Runtime Boundary

Each invocation owns its device backend and resource ledger.
An HTTP session shares the immutable grant and factory configuration with its request contexts.
Each request opens its backend only when an authorized operation needs that backend.
Each request closes its buffers and backend before request completion.

Device storage contains an opaque allocation.
It does not contain a second host copy of the tensor elements.
Transfers between CPU and device storage preserve FP32 bits.
A transfer to the same location preserves the backing allocation.
Host observation and result publication require an explicit CPU transfer.
This rule also applies to tensors inside records, lists, Parameters, and optimizer state.

The ledger accounts for actual reserved bytes.
Logical work charges remain separate from storage reservations.
Shared tensor views count their backing allocation once.
Scope release closes device buffers even when a retained value still refers to their storage record.
The runtime rejects subsequent use of released storage.
Cleanup preserves the primary failure and records cleanup failures separately.

`cuda-sim:0` implements explicit transfers.
It rejects arithmetic operations.
The production Metal backend submits operation descriptors to the existing precompiled kernels.
It does not substitute CPU arithmetic when Metal is unavailable.
Complete optimizer transfers include AdamW moments and accumulated beta powers.

## Build Selection

The software distribution contains runtime archives for `darwin/arm64` and `linux/arm64`.
Metal uses an additional archive for `darwin/arm64`.
Its link recipe includes separate Foundation, Metal, and Objective-C inputs.
The software recipe does not include these inputs.

The native producer compiles the Objective-C adapter with the pinned LLVM compiler and declarations maintained inside Lo.
The producer embeds the verified `runtime/metal/metal.metallib` library in the Metal archive.
The installed compiler needs no Metal compiler or developer SDK to link that archive.
Artifact metadata records the selected backend, runtime digest, and fixed Metal identity.

The explicit `--device-backend metal` build option selects the Metal archive.
The runtime still requires `--allow-device metal:0` for device authorization.
Build selection does not grant execution authority.
The compiler rejects Metal for the Linux target.
Admission checks the kernel bytes, length, and digest in the archive's canonical Mach-O object.
The separate kernel file and artifact metadata must identify those same bytes.

Prepare the pinned inputs as described in the [runtime guide](native-runtime.md).
The kernel files and `bin/lo-metal-build.json` receipt from `make build-metal` are necessary for the Metal producer.
`runtime/metal/manifest.json` identifies the kernel, source files, and fixed numeric profile.
Build the distribution with the explicit Metal target:

```sh
make build-native-runtime-metal NATIVE_RUNTIME_STATE=_scratch/native-runtime </dev/null
```

Compile a Metal program with the development compiler and prepared runtime distribution:

```sh
LO_BUILD_ROOT="$PWD/_scratch/build-state" bin/lo build examples/xor_metal.lo \
  --toolchain _scratch/native-runtime/distribution/toolchain.json \
  --device-backend metal --output bin/xor-metal-llvm --format json </dev/null
```

The installed Darwin Metal package supplies the same compile and link inputs.
Use its `bin/lo` without `--toolchain` after installation.
No Go, Rust, Xcode, Metal compiler, or developer SDK installation is necessary for installed compilation.
The [distribution guide](compiler-distribution.md) separates package construction from installed compilation.

Read its build identity with `bin/xor-metal-llvm metadata`.
Execution requires `bin/xor-metal-llvm run --allow-device metal:0`.

## Verification

Native tests compile Lo programs and execute their generated machine code.
Trusted C ABI fixtures inject a device driver at the native runtime boundary.
The fixture does not replace Lo evaluation, resource scopes, differentiation, or optimizer operations.
The software Metal executor processes the same operation descriptors as the production adapter.
Independent expectations specify transfer bits, numeric results, errors, and resource receipts.

The verification groups cover these contracts:

- Grants, explicit transfers, and host publication.
- Allocation reservations, shared views, scoped release, and request isolation.
- Device failures, cancellation, sticky device loss, and prevention of later commits.
- Metal arithmetic, derivative operations, optimizers, and checkpoint transfers.
- Separate Metal link inputs, kernel identity, and unavailable-device errors.
- CPU backpropagation, training, XOR, checkpoint, and adapter regressions.

Darwin execution and Linux QEMU execution supply software acceptance evidence.
These checks do not measure physical Metal execution or performance.
Physical devices are not an acceptance condition.

Run the native device and Metal checks with the selected distribution:

```sh
make test-native NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestDevice|TestLLVM.*Metal|TestLLVM.*Device' -count=1 -timeout=30m" </dev/null
```

The [validation receipt](evidence/native-devices/validation.json) records the final commands, identities, and qualification limits.
