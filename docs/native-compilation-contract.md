# Native Compilation Contract

Native compilation uses LLVM 21.1.8, LLD 21.1.8, and a precompiled Rust runtime.
The installed compiler supplies all compiler tools and platform link inputs.
No Go installation or network access is necessary for a program build.
The selected targets are `darwin/arm64` and `linux/arm64`.
The interpreter and native artifacts use the same logical execution contract.

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
The benchmark script adds exact File output and persistent HTTP response checks.
The existing training fixtures remain part of qualification.

Use three warmup samples and at least ten measured samples.
Measure checked execution independently from process latency, HTTP requests, and build time.
Keep immutable program admission outside checked execution timing.
Include new session setup, result release, and full session cleanup inside that timing.
Verify the actual compiled benchmark result before measurement.
Keep first-build and warmup resource observations with all measured build observations.
Run timing lanes separately to prevent measurement contention.
Use uniform batch calibration for native execution timing.
Double the batch from one to 64 sessions until its duration reaches one millisecond.
Keep that batch size fixed for each measured process.
Include each session and its result cleanup before the next session.
Measure until elapsed time reaches 200 milliseconds or the iteration count reaches one million.
Complete the last batch before measurement stops.
Record elapsed nanoseconds, the iteration count, and the batch size.
Divide elapsed time by all completed iterations.
Do not subtract estimated clock overhead.

The different lane reports must identify the same package, runtime, sources, tools, environment, and measurement contract.
The merge command rejects incomplete lanes and diagnostic selections.
It preserves all raw samples and calculates the gates again.

The callback execution gate remains a 2× geometric-mean speed improvement on each target.
Each other elapsed-time gate permits at most a 10% increase.
HTTP uses p95 latency and reciprocal throughput with the same 10% limit.
Report inconclusive results when uncertainty overlaps a gate.

### Measurement Validity

Start measurement after input verification. Host CPU activity is not an admission condition.
Record external load before and after each lane as context.
Do not stop unrelated processes, wait for a quiet host, or remove samples because of load.
Run only one owned measurement job at a time.

Use the interpreter first in the first warmup pair.
Change the interpreter and native order after each pair, including warmups.
Record the actual order in each measured pair.
Keep sample indices in measurement order, starting at zero after warmup.
Reject missing pairs, an incorrect order, incorrect results, or changed inputs.
Keep every measured pair, including bursts and measurement drift.
Keep first-build, warmup, and measured build resource observations.

Calculate the same confidence intervals and numeric gates from the complete data.
A complete measurement can pass, fail, or be inconclusive.
External load does not turn a failed gate into an invalid measurement.
Do not retry a campaign automatically or select samples from different attempts.
Run both targets and all four lanes even when a complete lane fails a performance gate.
Stop on incomplete measurements, incorrect results, or invalid provenance.

The paired ratio reduces variation shared by both measurements.
It does not remove contention that affects the implementations differently.
Ten pairs can give limited precision near a threshold.
The bootstrap interval calculation selects pair ratios independently and does not model prolonged temporal correlation.
Fixed alternation can coincide with periodic interference.
An inconclusive result does not satisfy acceptance.
Qualification applies to the recorded host and target environment.
It does not promise the same performance on every machine or under every workload.

### Performance Commands

Prepare the installed distributions before measurement.
Run these commands on the Darwin arm64 producer host.
Keep the source files and packages unchanged during both target runs.
Complete this campaign's other builds and tests before timing starts.
Unrelated host activity does not prevent a run.

Use the maintained campaign command to run all eight lanes serially and merge both target reports.
This command declares ten measured pairs before execution.
It retains the command status, logs, input identities, load context, and complete gate outcome.
Its repository lock prevents two campaign commands from running together.

```sh
make benchmark-native-campaign \
  COMPILER_DISTRIBUTION_OUTPUT=bin/distributions \
  NATIVE_CAMPAIGN_WORK=_scratch/native-qualification/campaign \
  LINUX_ACCELERATOR=hvf </dev/null
```

Use a new output directory for each campaign.
An unsuccessful exit can mean a complete failed or inconclusive gate, or an invalid measurement.
Read `report.json` in that directory to identify the outcome.
The command does not retry.

The individual producer commands remain available:

```sh
make benchmark-standalone \
  NATIVE_BENCH_WORK=_scratch/native-qualification/performance-darwin </dev/null
make benchmark-native-linux \
  NATIVE_BENCH_WORK=_scratch/native-qualification/performance-linux \
  LINUX_ACCELERATOR=hvf </dev/null
```

Each command runs all four lanes by default.
The Linux command uses the installed compiler inside a guest without external network access.
The final report for each target must include all four lanes.
It must set `qualification_complete` to `true`.
A diagnostic case selection does not satisfy qualification.

Set `NATIVE_BENCH_LANE` to run `execution`, `process`, `http`, or `build` independently.
Use a new work directory for each lane.
Combine the four completed lane reports for one target with this command:

```sh
python3 -B tools/nativebench/merge.py \
  --report _scratch/native-qualification/execution/report.json \
  --report _scratch/native-qualification/process/report.json \
  --report _scratch/native-qualification/http/report.json \
  --report _scratch/native-qualification/build/report.json \
  --output _scratch/native-qualification/performance-complete.json
```

Use the actual lane paths for each target.
Keep all input reports with the merged report.
The merge command preserves their paths and SHA-256 identities.

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
I011 owns qualification of the current LLVM compiler against this contract.
