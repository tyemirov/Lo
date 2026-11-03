# Native Compilation Contract

Native compilation uses LLVM 21.1.8, LLD 21.1.8, and a precompiled Rust runtime.
The installed compiler supplies all compiler tools and platform link inputs.
No Go installation or network access is necessary for a program build.
The selected targets are `darwin/arm64` and `linux/arm64`.
Native artifacts use the logical execution rules in the [language specification](../specification.md).

## Build Limits

| Resource | Limit | Enforcement |
| --- | --- | --- |
| LLVM module | 64 MiB | Reject before object compilation. |
| Build elapsed time | 180 seconds | Stop the complete compiler process group. |
| Toolchain diagnostic output | 1 MiB | Stop the build when output exceeds the limit. |
| Artifact size | 64 MiB | Reject before publication. |
| Peak build memory | 2 GiB | Qualify through recorded measurements. |

The full fixed corpus must satisfy these limits on both targets.
The memory budget is a qualification criterion, not a process memory limit.

The full integration suite has a 120-minute package timeout.
That timeout does not change the 180-second limit for one build.
The suite includes installed compilation for each finite-difference input and checkpoint boundary case.
The previous 30-minute budget expired during continued successful execution.
The current budget matches the Linux qualification budget.
Record process-tree RSS during each build.

## Logical Charge Order

- Charge each reached statement before its expression or effect.
- Charge each reached expression before its children.
- Evaluate arguments in source order before their placement in parameter order.
- Charge each successful expression's expanded value weight after evaluation.
- Charge each list callback iteration before the callback.
- Charge each aggregate iteration before arithmetic.
- Admit tensor work before allocation and numeric execution.
- Preserve flow depth through callbacks and error handlers.
- Poll cancellation at each charge point and the existing adapter boundaries.
- Preserve derivative snapshots, tape limits, and optimizer work charges.

## Fixed Performance Corpus

The `tests/corpus` package defines startup, callback, record, Unicode, and large-integer inputs.
Map, filter, and fold each use 1,000 and 10,000 varied integers.
File workloads verify exact bytes and filesystem changes.
HTTP workloads verify each response and the service lifecycle.
MVP200 and Dense20 use independent numerical expectations.

The [native measurements guide](native-benchmarks.md) defines the current commands, report fields, input identities, and independent expectations.
All application measurements use compiled native artifacts or the native C ABI.
The tools report native execution, process latency, HTTP costs, and build resources as separate measurements.
Go allocation counters apply only to frontend compiler checks.

### Measurement Validity

Verify input identities before measurement starts.
Keep source files, packages, runtime archives, and tools unchanged during a run.
Run one owned measurement job at a time.
Record external load before and after each lane.
Do not stop unrelated processes or discard samples because of load.

Use three warmups for each lane.
Retain ten execution samples, ten process samples, 100 HTTP samples, and ten build samples.
Retain the first build and all warmup build observations.
Keep sample indices in measurement order, starting at zero after warmup.
Verify independent results and complete resource cleanup.
Reject missing samples, incorrect results, changed inputs, and obsolete paired schemas.

The C execution measurement excludes program admission.
Its interval includes fresh session setup, execution, checked close, context release, and result release.
A separate executable records native allocator requests during that execution scope.
Its allocation volume is not peak memory or logical value weight.

The report uses native medians, p95 values, and deterministic bootstrap intervals.
The build resource limits remain numeric acceptance criteria.
Interpreter ratios and callback speed limits apply to earlier implementations.
A diagnostic selection does not establish complete qualification.
Qualification applies to the recorded target and environment.

### Performance Commands

Prepare installed distributions before a qualifying run.
Use a fresh output directory for each campaign.
Run the [campaign command](native-benchmarks.md#run-measurements) for both targets and all four lanes.

```sh
make benchmark-native-campaign \
  NATIVE_CAMPAIGN_WORK=_scratch/native-measurements/campaign \
  LINUX_ACCELERATOR=tcg
```

Read `report.json` to distinguish complete measurements, resource failures, and invalid input.
Keep every lane report with its merged report.
The merge command preserves report paths and SHA-256 identities.
The current tools reject the historical interpreter comparison schema.

## Artifact Construction

Native builds require `LO_BUILD_ROOT` to name an existing absolute directory without symlink traversal.
Invalid input produces `build.output` before build state is created.
The build places LLVM modules and private tool and link-input copies under that directory.
It does not use a user cache or the system temporary directory.
`make build` creates the repository workflow root at `_scratch/build-state/`.
Prepare the development runtime inputs with the [runtime guide](native-runtime.md#prepare-and-build) before this example.

```bash
make build
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo build examples/list_sum.lo \
  --toolchain _scratch/native-runtime/distribution/toolchain.json \
  --output ./bin/list-sum
```


An explicit `--toolchain` manifest is necessary for a development compiler.
An installed compiler uses its own pinned manifest.
The compiler validates declared input digests and takes private copies before tool execution.
The selected LLVM compiler and linker execute with an empty `PATH`.

Publish the complete artifact through a rename within the output directory.
Preserve the previous artifact on failure or cancellation.
Include source identity, runtime digest, target, compiler, and toolchain in artifact metadata.
Exclude timestamps, temporary paths, and resource grants from metadata.

The File open-path operation requires `lsof` on the execution host.
The artifact contains its descriptor-bound probe launcher and parser.
Other File operations do not require this tool.

## Device Backend Selection

The default `--device-backend software` configuration supports both selected targets.
A Darwin Metal distribution and a local `darwin/arm64` target are necessary for the explicit `--device-backend metal` configuration.
Unsupported combinations fail before tool execution.
The [native Metal contract](native-metal.md) defines its manifest, precompiled kernels, and metadata.
Both configurations use the same build limits, cancellation, and atomic publication path.

The [F001 acceptance report](native-compilation-results.md) records historical Go-generation measurements.
I017 defines the current native measurement contract.
I018 owns the final stack validation checkpoint.
