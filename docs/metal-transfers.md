# Metal Transfers

F012 implements explicit Metal transfers through the interpreter on macOS arm64.
The device selector is `metal:0`. Its host grant is `--allow-device metal:0`.
The standard build and native artifacts report `Device.Unavailable` for authorized Metal access.
F013 adds [tensor arithmetic and training](metal-arithmetic.md). F014 owns native Metal artifacts.

## Build and Execute

Use the repository root for these commands:

```bash
make build-metal
./bin/lo-metal check examples/metal_transfer.lo
./bin/lo-metal inspect examples/metal_transfer.lo --format json
./bin/lo-metal run examples/metal_transfer.lo --allow-device metal:0 --format json
make test-metal TEST_ARGS='-count=1 -v'
```

The example returns shape `[2,2]` and values `[1.25,-0,1e-45,-2.5]` on `cpu`.
No command automatically selects another backend after a Metal failure.
Static compiler commands do not initialize the device.
The runtime initializes the default Apple GPU only after the grant matches the source selector.

The [build manifest](../tools/metalbuild/main.go) pins the following inputs:

| Input | Selection |
| --- | --- |
| Host and target | `darwin/arm64`, macOS 27 deployment target. |
| Go | `go1.27.1`. |
| Xcode | `27.0`, build `27A266a`. |
| SDK | macOS `27.0`. |
| Clang | `21.0.0`, build `clang-2100.3.34.2`. |
| Metal compiler and linker | `32023.921`, build `metalfe-32023.921.6`. |
| Kernel language | MSL `3.1`. |
| Kernel math options | Safe math, precise FP32 functions, and disabled contraction. |
| Device features | Apple GPU family 7 support and unified memory. |

If the Metal compiler component is absent, install it with `xcodebuild -downloadComponent MetalToolchain`.
Missing or different build inputs stop the build.
The build does not install tools or change its selected compiler automatically.
The Go build uses the selected SDK and clears conflicting Go and cgo flag overrides.

The build compiles `metal_kernels.metal` into the ignored `internal/device/metal.metallib` artifact.
Go embeds that library in `bin/lo-metal`.
The resulting compiler needs system Apple frameworks during execution, with no external kernel file.
The generated `bin/lo-metal-build.json` records tool versions, source hashes, compiler flags, kernel hash, and binary hash.
It also records the build host's macOS version and build identifier.

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

## F012 Validation

The initial public tests rejected the absent selector, grant, transfer profile, and lost-session error.
The [initial failure record](evidence/metal-red.txt) contains that evidence.
The [software tests](../tests/integration/metal_test.go) exercise the real compiler, evaluator, resource ledger, and CLI serialization.
They inject the storage boundary to verify failure and cancellation behavior without GPU access.
They cover authorization order, exact transfers, empty tensors, source retention, budgets, error kinds, and cleanup.
They also verify that failed transfers prevent subsequent commits and unusable sessions reject further submissions.
The [cancellation regression](evidence/metal-cancel-red.txt) first exposed incorrect error priority when cancellation coincided with a transfer failure.
The [software execution record](evidence/metal-software.txt) records the successful regression tests after correction.

The [local Metal tests](../tests/integration/metal_live_test.go) use the compiled Metal adapter and its embedded kernel.
On the Apple M5 Pro, 513 finite edge cases and samples matched CPU values after a transfer round trip.
Empty and rank-zero tensors also passed.
A 100-step loop completed 200 transfers with 80 peak accounted bytes and complete cleanup.
The [local execution record](evidence/metal-local.txt) records these results and the selected device identity.
The [build receipt](evidence/metal-build.json) records the qualified build inputs.

The [final CI record](evidence/metal-ci.txt) records successful formatting, static analysis, software tests, native tests, and the standard build.
The local Metal tests also passed with the Go race detector.
Metal-specific static analysis passed with `go vet -tags=metal ./internal/device` under the pinned build environment.

Routine acceptance uses software integration tests without a GPU prerequisite.
`make test-metal` explicitly requests local Metal execution and fails when that environment is unavailable.
These results establish transfers and resource handling. They do not establish Metal arithmetic, training, or performance.
