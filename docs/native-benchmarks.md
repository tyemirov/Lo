# Native Measurements

Lo has one application runtime, implemented in Rust.
The compiler builds native artifacts before application measurements start.
The [build contract](native-compilation-contract.md#build-limits) defines the build resource limits.
The tools report execution costs and checks against independent expectations.
The tools reject interpreter fields and paired sample schemas.
Earlier reports retain their recorded numbers and source identities.

## Measurement Scopes

A sample is one recorded measurement.
An execution batch is a group of complete native sessions.

| Lane | Scope | Raw fields |
| --- | --- | --- |
| `execution` | Fresh session setup, execution, checked close, context release, and result release. Program admission occurs before the interval. | `sample`, `ns`, `total_ns`, `iterations`, `batch_size` |
| `process` | A fresh artifact process includes startup, admission, cleanup, and output. Compilation occurs before the interval. | `sample`, `wall_ns` |
| `http` | Requests through one native service. Each response must have the expected status and bytes. | `sample`, `samples_ns`, `elapsed_ns`, `p95_ns`, `requests_per_second` |
| `build` | The interval includes the compiler, its children, object compilation, and the linker. | `phase`, `sample`, `elapsed_ns`, `peak_process_tree_rss_bytes`, `artifact_bytes`, `diagnostic_bytes` |

Each lane uses three warmups.
The fixed plan has ten execution samples, ten process samples, 100 HTTP samples, and ten build samples.
Build records also retain the first build and all warmup builds.
Each HTTP sample contains 200 requests at concurrency one.
HTTP records require valid `serve` readiness and stopped envelopes.
The stopped envelope confirms completed child joins and cleanup.

The C execution program doubles the batch size from one through 64 until a batch takes at least one millisecond.
It then measures complete batches until elapsed time reaches 200 milliseconds or the iteration count reaches one million.
Each session completes checked cleanup before the next session starts.
The program records the actual duration and completed iteration count.
It does not subtract a clock estimate.

A separate C program counts allocator requests that return a buffer.
Its interval includes native session setup, execution, and checked cleanup.
Its `native_allocation_volume` object contains `bytes`, `allocations`, and `method`.
These values describe total allocation requests, not peak memory or logical value weight.
The time measurement uses a separate executable without allocation counters.

The report retains raw measurements, medians, p95 values, and deterministic bootstrap intervals.
Only the build resource limits supply numeric acceptance thresholds.
Correct results, complete records, and unchanged inputs remain necessary.
The former interpreter ratios and callback speed limits apply to earlier implementations.

## Independent Expectations

The fixed corpus supplies exact scalar expectations through `tests/corpus/corpus.go`.
File workloads verify the scalar result and complete filesystem changes.
HTTP workloads verify each response.

MVP200 uses the independent values in `docs/evidence/training-reference-results.json`.
Dense20 uses a scalar matrix calculation in `tools/nativebench/oracles.py`.
That calculation executes 20 SGD updates without Lo tensor operations or autodiff.
The two training checks verify loss values, parameter fields, tensor shapes, and parameter values.
Repeated native output alone does not supply an independent expectation.

## Input Identity And Reports

Each lane report for qualification identifies one installed compiler package and its runtime archive.
The identity also includes the target, environment, accelerator, producer tools, C programs, independent expectations, and report evaluator.
The `oracle` identity contains the source files that define independent expectations.
The tools verify these identities before and after measurements.
A source change invalidates that run.

`measurement_complete` identifies complete, valid measurements.
`gates_passed` identifies successful result checks and build resource checks.
`qualification_complete` requires the complete fixed corpus across all four lanes for one target.
A development compiler or a `--case` selection produces diagnostic data without complete qualification.
A complete campaign must include a complete report for each target.

The merge tool accepts four complete, nonoverlapping lanes with identical input identities.
It retains each input report path and SHA-256 digest.
It recalculates validity and resource outcomes from the raw records.
It rejects incomplete reports, diagnostic selections, duplicate samples, changed identities, and obsolete paired schemas.

## Run Measurements

Prepare the installed distributions with the [runtime guide](native-runtime.md#prepare-and-build).
Use a fresh work directory for each run.
Keep the source files and packages unchanged during a run.
Run one owned measurement job at a time.
Keep unrelated host activity as report context.
Do not stop unrelated processes or discard samples because of host load.

Run an individual native lane on Darwin:

```sh
make benchmark-standalone \
  NATIVE_BENCH_LANE=execution \
  NATIVE_BENCH_WORK=_scratch/native-measurements/darwin-execution
```

Run the corresponding Linux lane through the prepared software guest:

```sh
make benchmark-native-linux \
  NATIVE_BENCH_LANE=execution \
  NATIVE_BENCH_WORK=_scratch/native-measurements/linux-execution \
  LINUX_ACCELERATOR=tcg
```

Select `process`, `http`, or `build` for a different lane.
Omit the lane selection to run all four lanes.
The Linux result describes the recorded QEMU configuration.
It does not establish performance on a native Linux host.

Run all eight lanes through the campaign owner:

```sh
make benchmark-native-campaign \
  NATIVE_CAMPAIGN_WORK=_scratch/native-measurements/campaign \
  LINUX_ACCELERATOR=tcg
```

The campaign retains command statuses, logs, identities, load observations, and raw reports.
Its repository lock prevents concurrent campaign owners.
A complete build resource failure remains a recorded failure.
The campaign stops on invalid inputs, incorrect results, or incomplete measurements.
It does not retry automatically.

Merge four separate lane reports with their actual paths:

```sh
python3 -B tools/nativebench/merge.py \
  --report _scratch/native-measurements/execution/report.json \
  --report _scratch/native-measurements/process/report.json \
  --report _scratch/native-measurements/http/report.json \
  --report _scratch/native-measurements/build/report.json \
  --output _scratch/native-measurements/complete.json
```

Keep all input reports with the merged report.
Read `report.json` after a command failure.
Keep invalid or partial data separate from qualifying evidence.

## Other Commands

`make benchmark-file-io` builds each File workload before its process measurements.
The [File guide](file-io-performance.md) defines its fixtures, quotas, and methods.
Its generic CLI measurements do not include allocator counters.
Dedicated native tests supply removal allocation checks.

`make benchmark-training` measures frontend checks with Go compiler allocation counters.
`make benchmark-compiler` measures the other fixed frontend cases.
These counters describe compiler work, not native application allocation.

`make benchmark-training-cli` builds the two training artifacts before process measurements.
It uses two warmups and a minimum of ten measured processes for each artifact.
The [training report](training-benchmark.md) separates current commands from the earlier interpreter baseline.
