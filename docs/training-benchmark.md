# CPU Training Baseline for F001

F005, F006, and F007 support complete CPU training through the interpreter.
The small network completes 200 SGD updates with final loss `0.00013215476064942777`.
This result equals the independent FP32 fixture result.
Native executable generation remains open under F001.

## Workloads and Method

| Workload | Network and data | Updates | Initial loss | Final loss |
| --- | --- | --- | --- | --- |
| MVP200 | Two inputs, two ReLU units, one output, four examples | 200 | `2.46875` | `0.00013215476064942777` |
| Dense20 | 16 inputs, 16 ReLU units, eight outputs, 32 examples | 20 | `0.015582273714244366` | `0.015550335869193077` |

Both workloads use mean squared error, exact Parameter records, reverse-mode autodiff, and immutable SGD state through `List.fold`.
The [small example](../examples/training_cpu.lo) and [larger fixture](../tests/benchmark/testdata/training_dense.lo) contain fixed input values.
The larger fixture measures additional matrix work and source construction.
Its loss decrease is execution evidence, not an independent arithmetic oracle.

Measurements used an Apple M5 Pro, macOS arm64, and Go 1.27.1 on October 13, 2026.
Each command used `GOMAXPROCS=1`, one concurrent run, and a 10,000,000-step budget.
CPU execution required no external capability grants.
The small example also passed with the default execution budget.
No benchmark used a GPU or native Lo artifact.

The Go benchmarks used ten samples per phase, each with a 300 ms target duration.
Checking includes parsing, resolution, type checks, shape analysis, and derivative plan construction.
Execution starts from an already checked program and creates a fresh evaluation session for each run.
Serialization starts from an already evaluated result.
These phases exclude process startup.

The CLI benchmark used two initial unmeasured processes and ten measured processes per workload.
OS caches remained warm.
CLI latency includes process startup, file access, checking, execution, and JSON output.
Each measured result had the same output digest as its initial result.
The commands ran sequentially to prevent benchmark contention.

## Measurements

Values below are medians.
Intervals are 95% bootstrap intervals for the median, with 2,000 resamples and fixed seed `5006007`.
They describe these samples and do not establish performance on another host.

| Workload and phase | Median time | Interval | Allocated bytes per run | Allocations per run |
| --- | --- | --- | --- | --- |
| MVP200 check | 0.210 ms | 0.208–0.211 ms | 399,120 | 4,103 |
| MVP200 execution | 5.157 ms | 5.058–5.206 ms | 8,401,633 | 213,899 |
| MVP200 serialization | 0.0104 ms | 0.0103–0.0104 ms | 10,570 | 137 |
| Dense20 check | 2.371 ms | 2.353–2.435 ms | 2,067,894 | 11,085 |
| Dense20 execution | 3.015 ms | 3.002–3.043 ms | 1,647,414 | 23,169 |
| Dense20 serialization | 0.0504 ms | 0.0501–0.0506 ms | 56,093 | 536 |

| Workload | Complete CLI median | Interval |
| --- | --- | --- |
| MVP200 | 10.652 ms | 10.482–11.783 ms |
| Dense20 | 11.114 ms | 10.320–11.247 ms |

Execution time per update is approximately 25.8 microseconds for MVP200 and 150.8 microseconds for Dense20.
These ratios include setup and final observation within the evaluated program.
They are not isolated optimizer-step measurements.
Allocated bytes are cumulative Go allocation volume, not peak live storage or Lo logical value weight.
The CLI timings and phase timings use separate processes and cannot establish an exact startup cost by subtraction.

The [raw Go samples](evidence/training-benchmark-go.txt) record each phase measurement.
The [CLI samples](evidence/training-benchmark-cli.json) include source hashes, binary hash, binary size, result hashes, environment, and all elapsed times.

## Allocation Evidence and CPU Profile Limit

The [combined execution profile](evidence/training-profile.txt) covers both workloads.
Derivative evaluation accounts for approximately 76.9% of sampled allocation bytes, including its child calls.
Diagnostic snapshots account for approximately 24.1% of allocation bytes, including their child calls.
Record construction, shape plans, metadata copies, and tensor storage contribute additional allocation volume.
These percentages overlap because they include child calls.

The CPU profile attributes 88.8% of its samples to `runtime.kevent` during runtime scheduling and garbage collection.
This attribution does not establish useful Lo operation costs.
Thus, the report does not use those CPU percentages to estimate a native speed improvement.
F001 requires a reliable CPU attribution method before such estimates become acceptance evidence.
Elapsed times and allocation measurements remain separate observations.

## Implications for F001

The small training workload spends substantially more time in execution than in checking.
Removing frontend work alone will not remove its dominant measured phase.
The larger fixture has a greater checking cost because its source contains more explicit numeric inputs.
A standalone artifact can remove that repeated frontend work.

The allocation profile identifies work for the shared runtime boundary in the native plan.
Direct generated calls and locals can remove expression traversal and temporary argument maps.
F001 must also measure record construction, metadata copies, and diagnostic snapshots in the shared runtime.
Generated Go that retains these costs does not automatically remove them.
Tensor kernels already execute compiled Go loops.
Native graph compilation alone does not replace their numeric algorithms.

Use both fixtures in the F001 corpus with identical inputs, output contracts, numeric profile, and logical limits.
Compare generated execution with the checked-program execution phase above.
Measure complete artifact latency separately from execution and build time.
Retain gradient, optimizer, source-span, cancellation, and budget differential tests.
Keep the existing native performance gates until Stage 0 formally selects its complete corpus and budgets.

This baseline supports F001 planning.
It does not establish native performance, Linux execution, CUDA execution, or completion of I001.
The remaining Stage 0 work still includes callback, File, HTTP, and native build measurements.

## Reproduction

Run these commands sequentially from the repository root:

```bash
make benchmark-training
make benchmark-training-cli
make benchmark-training-profile
```

The first command reports phase times and allocations.
The second command reports CLI samples and environment data as JSON after its build commands.
The third command writes local profiles under `bin/` and prints CPU and allocation reports.
The profile commands use three seconds per execution workload.
`BENCH_COUNT` and `BENCH_TIME` control repeated phase measurements.
CLI measurements require at least ten samples.
