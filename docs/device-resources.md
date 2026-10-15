# Device Resources and Transfers

F008 implements device authorization, explicit tensor transfers, and invocation storage budgets.
The interpreter and native artifacts use the same resource contract.
The software backend implements transfers for the explicit `cuda-sim:0` selector.
The current build reports `Device.Unavailable` for authorized `cuda:0` access.
F012 adds explicit `metal:0` transfers in the Metal interpreter build.
The [Metal transfer contract](metal-transfers.md) defines that build and its validation.
F013 adds [synchronous Metal computation](metal-arithmetic.md) through typed operation descriptors.
F009 will implement NVIDIA arithmetic and its backend.
F010 will implement deferred execution and source-order failure selection for asynchronous operations.

## Public Contract

`Tensor.to(value: Tensor<FP32>, device: Device) -> Tensor<FP32>!` transfers immutable numeric data.
The transfer keeps shape, dtype, signed zero, subnormals, and every FP32 bit.
The source remains live until the synchronous transfer completes.
A same-location transfer returns the original storage without a copy or an element-work charge.
A transfer between CPU and the selected compute location creates one destination allocation.
Transfers between different compute locations are invalid.

The [example](../examples/device_transfer.lo) transfers four elements to simulation and back to CPU.

```bash
make build
./bin/lo check examples/device_transfer.lo
./bin/lo inspect examples/device_transfer.lo --format json
./bin/lo run examples/device_transfer.lo --allow-device cuda-sim:0 --format json
./bin/lo build examples/device_transfer.lo --output ./bin/device-transfer
./bin/device-transfer run --allow-device cuda-sim:0 --format json
```

The result has shape `[2,2]` and values `[1.25,-0,1e-45,-2.5]`.
The final location is `cpu`.
The [example result](evidence/device-example.json) records identical interpreter and native output.

The CLI accepts one exact non-CPU grant with `run` or `serve`.
The source selector and host grant must agree.
A missing grant produces `runtime.capability-denied`, with exit status 2, before backend initialization or device allocation.
Duplicate grants and a `cpu` grant are invalid CLI inputs.
CPU execution requires no grant.
Device descriptors, static checks, and inspection do not initialize a backend.
Each HTTP request receives a separate session with the configured grant.

| Condition | Result |
| --- | --- |
| Authorized backend unavailable | Recoverable `Device.Unavailable`. |
| Backend allocation failure | Recoverable `Device.AllocationFailed`. |
| Upload or download failure | Recoverable `Device.TransferFailed`, with no result tensor. |
| Unsupported device profile | Recoverable `Device.UnsupportedProfile`. |
| Metal library or pipeline creation failure | Recoverable `Device.LibraryFailed`. |
| Unusable device session | Recoverable `Device.Lost`, with no subsequent device submissions. |
| Arithmetic across locations | Recoverable `Tensor.DeviceMismatch`. |
| Unsupported arithmetic | Recoverable `Device.UnsupportedOperation` before output allocation or submission. |
| Computation command construction failure | Recoverable `Device.ExecutionFailed`. |
| Numeric CPU observation of a compute tensor | Recoverable `Tensor.HostObservationRequired`. |
| Device tensor in the final result | Host `runtime.tensor-publication`, with exit status 2. |
| Storage or work budget exceeded | Host `runtime.limit`, with exit status 2. |
| Cancellation | Host `runtime.cancelled`, with exit status 2. |
| Resource cleanup failure | Host `runtime.host-failure`, with exit status 2. |

Publication checks the complete result before output bytes.
The check includes records, lists, maps, Parameters, gradients, and optimizer state.
Error snapshots contain tensor metadata and cause no numeric download.
`Tensor.device` returns the actual location.
Parameters and gradients require consistent locations in their exact schemas.

## Ownership and Budgets

`internal/device` contains validated sessions, backend interfaces, and the resource ledger.
A session initializes its backend at first authorized access.
Each allocation receipt records an identity, location, and actual reserved byte count.
Immutable views share one backing allocation.
Expanded value weight still counts repeated tensor occurrences.

| Storage | Per-invocation limit |
| --- | --- |
| All live CPU buffers, device buffers, and workspace | 64 MiB |
| Tape storage, included in the total | 32 MiB |
| Optimizer moments and beta powers, included in the total | 32 MiB |
| Library workspace, included in the total | 16 MiB |

The existing logical value and step budgets also apply.
Each actual transfer charges one step per element before allocation.
The ledger includes temporary CPU data and destination buffers while the source remains live.
Backend reservations count at their reported size, including padding.
A reservation that exceeds the available budget is released before transfer.

F009 can use the workspace class through the same ledger boundary.
Metal computation charges status buffers to the workspace class.
Driver context overhead and Go process memory are outside these logical storage limits.

Execution scopes release temporary allocations at flow exits.
Fold callbacks release superseded states and keep values that remain reachable.
Unfold and map results keep their shared allocations until their enclosing scope releases them.
The runtime closes all remaining receipts after success, failure, or cancellation.
HTTP requests close their resources before response publication.
A cleanup failure prevents a successful result.

## Static Inspection and Backend Interface

Flow summaries include a transitive `devices` flag beside `reads` and `commits`.
The `device_resources` array identifies operations, selectors, contexts, conditions, and storage bytes.
The `transfers` array contains the explicit `Tensor.to` sites.
A dynamic selector has an unknown target description.
Unknown storage sizes and source locations use JSON `null`.
Known sizes describe logical element payloads before any backend padding.
Inspection includes unused flows without resource access.

The `Factory`, `Backend`, and `Buffer` interfaces isolate backend initialization, allocation, transfers, computation, and cleanup.
A compute tensor has metadata and opaque storage.
CPU numeric kernels require CPU data.
`runner.RunContext` accepts a factory and receipt observer for tests through the public CLI execution contract.
The production CLI selects a backend through the factory for its build.
The standard build provides simulation. The Metal build also provides authorized Metal transfers and computation.
Open receipts record the selected backend identity without tensor contents.
A failed CUDA request never changes its selected backend.

## Validation

The initial public CLI tests failed because `Tensor.to` and `--allow-device` were absent.
The [initial failure record](evidence/device-red.txt) contains that result.
The [integration tests](../tests/integration/device_test.go) cover interpreter and native command execution.
The [resource tests](../tests/integration/device_resources_test.go) inject backend failures through interpreter execution and CLI serialization.
They also verify receipts through the public resource interface.
Native command cases verify transfers, retained callback outputs, HTTP requests, authorization, publication failures, and metadata.

The failure tests cover unavailable backends, allocation, upload, download, cancellation, budgets, and cleanup.
They verify that transfer failure prevents a later File commit.
Receipt checks require one release per allocation and zero live bytes at completion.
A host-only error handler can recover without a numeric download.
CPU identity transfers and reshape views share one allocation.
A 100-step transfer fold performs 200 transfers with at most 64 live bytes.

The 200-step CPU training fixture also verifies resource lifetime.
Its SGD run records 8,222 allocations and a peak of 608 live bytes.
Its AdamW variant records 24,831 allocations and a peak of 1,008 live bytes.
The [lifetime test record](evidence/device-lifetime.txt) contains these measurements.
These figures describe the small test fixtures, not general model requirements.

The tests use software execution on macOS arm64.
They do not claim NVIDIA arithmetic execution or a new performance qualification.
The [native race record](evidence/device-native-race.txt) records the focused race check.
The earlier F001 timing report remains evidence for its recorded revision.

The [full CI record](evidence/device-ci.txt) contains the successful `make ci` result.
That gate includes formatting, static analysis, all Go tests, the complete native integration suite, and the compiler build.
The [documentation review](evidence/device-documentation-review.json) covers changed prose and new local links.
The [Governor record](evidence/device-governor.json) contains the successful governance check.
