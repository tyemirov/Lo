# Metal Transfers

F012 implements explicit Metal transfers through compiled artifacts on macOS arm64.
The device selector is `metal:0`. Its host grant is `--allow-device metal:0`.
The software runtime reports `Device.Unavailable` for authorized Metal access.

The Darwin Metal package supports native Metal artifacts through the static runtime.
F013 adds [tensor arithmetic and training](metal-arithmetic.md). F014 owns native Metal artifacts.

## Developer Build and Execution

Prepare the native inputs with the [Metal guide](native-metal.md#developer-preparation).
Use the repository root for these commands:

```bash
make build
make build-native-runtime-metal
./bin/lo check examples/metal_transfer.lo
./bin/lo inspect examples/metal_transfer.lo --format json
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo build examples/metal_transfer.lo \
  --toolchain _scratch/native-runtime/distribution/toolchain.json \
  --device-backend metal --output ./bin/metal-transfer
./bin/metal-transfer run --allow-device metal:0 --format json
make test-native-metal TEST_ARGS='-count=1 -v'
```

The example returns shape `[2,2]` and values `[1.25,-0,1e-45,-2.5]` on `cpu`.
No command automatically selects another backend after a Metal failure.
Static compiler commands do not initialize the device.
The runtime initializes the default Apple GPU only after the grant matches the source selector.

The [native Metal guide](native-metal.md) owns kernel provenance, numeric profiles, source identities, and native artifact construction.
The repository retains verified precompiled kernels and their canonical source manifest.
The native producer compiles the Objective-C adapter with pinned LLVM and embeds the kernel bytes in the Rust archive.
The Go compiler frontend executes no Metal operations.
Installed compilation requires no Apple development tools.


## Transfer Profile and Storage

The current profile is `metal-fp32`.
The `lo_copy_words` kernel copies 32-bit integer words without numeric conversion.
Transfers keep every FP32 bit, including signed zero and subnormals.
The [arithmetic contract](metal-arithmetic.md) defines numeric computation separately from bit-preserving transfers.

Each invocation owns one command queue, one copy pipeline, and one computation pipeline.
Each nonempty buffer contains equal regions for tensor data and transfer staging.
Upload copies host bytes into staging before a GPU copy into the tensor region.
Download copies the tensor region into staging before the host reads the completed result.
Every transfer waits for its command buffer to complete.
The adapter keeps no Go pointers after a C call returns.

The resource ledger charges the buffer's `allocatedSize`, including staging and allocation padding.
This storage counts against the existing 64 MiB total limit.
Transfer staging is part of the buffer reservation, with no additional library workspace allocation.
Driver objects and command infrastructure remain outside logical tensor storage limits.
An empty tensor has a zero-byte receipt and needs no Metal buffer or kernel dispatch.
Same-location transfers share their original immutable storage.

An open receipt records the device name, registry identifier, numeric profile, runtime macOS identity, and embedded kernel hash.
Static inspection reports logical tensor bytes rather than a device reservation that does not yet exist.
Receipt identity and allocation evidence contain no tensor payload.

## Failures and Cancellation

The shared [device resource contract](device-resources.md) defines authorization, publication, budgets, and cleanup.
Metal adds `Device.UnsupportedProfile`, `Device.LibraryFailed`, and `Device.Lost` to transfer error declarations.
Failures retain the source operation and trace through the existing runtime error path.
Diagnostic snapshots do not download tensor values.

Command construction failures produce `Device.TransferFailed`.
A failed submitted command marks the invocation's session unusable and produces `Device.Lost`.
Subsequent device resource access returns `Device.Lost` without submission.
The runtime still permits CPU error handling and resource cleanup.

Cancellation prevents new submissions.
Cancellation remains a host diagnostic when a device boundary also reports a failure.
An in-progress synchronous C call finishes before its buffers are released.
The adapter then checks cancellation before the runtime can publish a result or activate a subsequent effect.
This implementation does not promise immediate GPU preemption.
Cleanup failures remain host diagnostics and prevent successful result publication.

## Historical F012 Validation

The following evidence records F012 before the LLVM migration.
The [native device guide](native-devices.md#verification) defines current native software verification.

The initial public tests rejected the absent selector, grant, transfer profile, and lost-session error.
The [initial failure record](evidence/metal-red.txt) contains that evidence.
The [native software tests](../tests/integration/llvm_device_resources_test.go) exercise compiled entries, the resource ledger, and device hooks.
They inject the storage boundary to verify failure and cancellation behavior without GPU access.
They cover authorization order, exact transfers, empty tensors, source retention, budgets, error kinds, and cleanup.
They also verify that failed transfers prevent subsequent commits and unusable sessions reject further submissions.
The [cancellation regression](evidence/metal-cancel-red.txt) first exposed incorrect error priority when cancellation coincided with a transfer failure.
The [software execution record](evidence/metal-software.txt) records the successful regression tests after correction.

The [native Metal tests](../tests/integration/native_metal_live_test.go) use the compiled Metal adapter and its embedded kernel.
On the Apple M5 Pro, 513 finite edge cases and samples matched CPU values after a transfer round trip.
Empty and rank-zero tensors also passed.
A 100-step loop completed 200 transfers with 80 peak accounted bytes and complete cleanup.
The [local execution record](evidence/metal-local.txt) records these results and the selected device identity.
The [build receipt](evidence/metal-build.json) records the qualified build inputs.

The [final CI record](evidence/metal-ci.txt) records successful formatting, static analysis, software tests, native tests, and the standard build.
The local Metal tests also passed with the Go race detector.
The historical Metal-specific static analysis used the former Go device package.
Current device execution resides in the Rust runtime.

Routine acceptance uses software integration tests without a GPU prerequisite.
Optional tests tagged `metal` exercise actual Metal execution.
Their availability does not control software acceptance.
These results establish transfers and resource handling. They do not establish Metal arithmetic, training, or performance.
