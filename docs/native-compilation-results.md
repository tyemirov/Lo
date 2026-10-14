# Native Compilation Results

F001 implements native CPU compilation through generated Go source.
The [native contract](native-compilation-contract.md) fixes the semantic requirements, resource limits, corpus, and performance gates.
Both targets passed all required semantic, performance, and build-resource gates.
Final CI passed.

## Qualification Environment

The measurements use Go 1.27.1 and `golang.org/x/sys` v0.49.0.
The macOS host has an Apple M5 Pro processor and macOS 27.0.1 on arm64.
Linux execution uses an arm64 Docker VM on the same host.
The Linux image contains Go and a C compiler for the race tests.

```text
golang@sha256:162be5298a40ed317005c8339c6de4d10d3eef336d66dc8e9259b03ab9d3a6d2
```

The raw records contain system data, source digests, compiler digests, logical limits, and all measured samples.
Timing lanes run separately from test jobs and other timing lanes.

## Semantic Evidence

The native test lane compiles CLI execution fixtures before execution.
Parser and verifier fixtures use the source compiler.
Package API tests continue to exercise their specified execution backend.
Native acceptance uses public CLI processes and generated program entries.

| Contract | Evidence |
| --- | --- |
| All 100 built-in operations | The runtime catalog tests compare compiled execution with independent expected results. |
| Values, flows, matches, and errors | Runtime and conformance tests cover exact values, nested failures, handlers, and source traces. |
| File workflows | Tests verify file contents, authorization, repeated commits, failure order, and both workflow branches. |
| HTTP services | Tests cover real requests, malformed data, concurrent requests, activation, disconnection, and shutdown. |
| Tensor and training limits | `TestNativeTensorBudgetDifferential` compares full results and diagnostics at selected budget boundaries. |
| Effects and logical limits | `TestNativeEffectBudgetDifferential` compares file bytes and results below, at, and above completion budgets. |
| Source independence | Artifacts execute after source deletion with an empty runtime search path in an unrelated directory. |
| Source traces | `TestNativeRemovedSourceDiagnostics` compares text and JSON errors after source deletion. |
| Runtime dependencies | `TestNativeStandalone` rejects compiler and interpreter package symbols in the artifact. |
| Isolated and repeatable builds | Tests compare artifact bytes with hostile Go environment variables and an empty module cache. |
| Build failures | Tests verify output preservation, output I/O errors, target rejection, cancellation, and bounded diagnostic output. |

The [Linux native suite](evidence/native-linux-tests.txt) passed.
The [final CI run](evidence/native-ci.txt) passed bundle verification, formatting, static analysis, all tests, and the CLI build.
Its [macOS native lane](evidence/native-darwin-tests.txt) passed the complete native integration suite.
The generated artifact race tests passed on [macOS](evidence/native-darwin-race.txt) and [Linux](evidence/native-linux-race.txt).
The race harness includes concurrent HTTP requests, active-request shutdown, pending connections, self-handlers, gradients, and optimizer limits.

The production builder uses `CGO_ENABLED=0`.
The explicit repository race harness enables cgo and adds race checks to the generated artifact and shared runtime.

The [build checks](evidence/native-build-contract.txt) and [effect budget checks](evidence/native-effect-budget.txt) record focused acceptance.
The [Linux effect checks](evidence/native-linux-effects.txt) and [cross-target check](evidence/native-linux-cross.txt) also passed.
An [isolated build](evidence/native-offline.txt) succeeded in a fresh container with network access disabled.
That container had only the compiler, source input, and output directory mounted from the host.
The resulting artifact returned `10` in a network-disabled image whose only file was the native program.
The runtime image contained no Go installation, Lo compiler, source file, or repository.

The runtime image used this Dockerfile:

```dockerfile
FROM scratch
COPY program /program
ENTRYPOINT ["/program"]
```

The [initial native test](evidence/native-red.txt) failed before implementation.
The [version output regression](evidence/native-version-limit-red.txt) failed before the builder bounded version-probe output.

## Measurement Method

Execution benchmarks construct both program entries before measurement.
They exclude source checks, factory construction, and result serialization from the measured loop.
They retain input construction, runtime value allocation, and all logical charges inside each execution.
Each benchmark checks its result before measurement.
Corpus cases have independent expected values.
Training cases also compare complete interpreter and native results.

Execution measurements use ten independent process samples after three warmup samples.
Each sample measures both backends for at least 200 milliseconds.
The backend order changes between sample pairs.
`GOMAXPROCS=1` fixes execution concurrency.
Each run has a limit of ten million logical steps.

Process and HTTP measurements use 30 paired samples after three warmup samples.
Process measurements include startup, execution, and output.
File cases use separate directories with identical input bytes and grants.
HTTP cases use persistent connections and 200 requests per sample at concurrency one.
Each response must have the expected status and bytes.
HTTP measurements record every request latency, block p95 latency, and block elapsed time.

The comparison uses paired native/interpreter time ratios.
Bootstrap intervals use 2,000 resamples and a fixed seed.
The callback gate uses the geometric mean across map, filter, and fold at both input sizes.
The lower interval bound must be at least 2×.
Each other time gate requires an upper interval bound at or below `1.1`.

Build measurements use one empty-cache probe per workload.
Each probe has a separate owned Go build cache.
Ten warm-build samples follow three warmup builds.
Build measurements include the compiler and its child processes.
The monitor samples process-tree resident set size after each ten-millisecond wait.
The sum can count shared memory pages more than once.

## Execution Results

The macOS callback speed improvement is **2.68×**, with a 95% bootstrap interval of **2.66–2.69×**.
Every measured macOS CPU case passed its time gate.
The training MVP took approximately 64% of interpreter execution time.
The dense training case took approximately 92%.

The Linux callback speed improvement is **2.48×**, with a 95% bootstrap interval of **2.43–2.54×**.
Every measured Linux CPU case passed its time gate.
The training MVP took approximately 66% of interpreter execution time.
The dense training case took approximately 87%.

### macOS arm64

| Workload | Interpreter µs | Native µs | Speed improvement | Byte ratio | Allocation ratio |
| --- | ---: | ---: | ---: | ---: | ---: |
| `startup` | 0.12 | 0.05 | 2.33× | 0.57 | 0.50 |
| `list_sum` | 0.74 | 0.38 | 1.98× | 0.65 | 0.66 |
| `map_1000` | 550.45 | 223.79 | 2.47× | 0.42 | 0.57 |
| `filter_1000` | 484.69 | 161.99 | 3.00× | 0.32 | 0.48 |
| `fold_1000` | 473.89 | 152.61 | 3.10× | 0.29 | 0.50 |
| `records_1000` | 601.16 | 254.34 | 2.36× | 0.47 | 0.52 |
| `bigint_1000` | 488.89 | 168.33 | 2.90× | 0.37 | 0.50 |
| `map_10000` | 6764.91 | 3093.08 | 2.20× | 0.45 | 0.57 |
| `filter_10000` | 5916.75 | 2256.52 | 2.62× | 0.34 | 0.48 |
| `fold_10000` | 5880.48 | 2130.44 | 2.77× | 0.31 | 0.50 |
| `records_10000` | 7327.48 | 3463.97 | 2.12× | 0.50 | 0.52 |
| `bigint_10000` | 6132.15 | 2307.63 | 2.66× | 0.38 | 0.50 |
| `unicode` | 84.09 | 83.81 | 1.01× | 0.99 | 0.54 |
| `training_mvp` | 5831.94 | 3715.27 | 1.57× | 0.74 | 0.81 |
| `training_dense` | 3036.58 | 2786.24 | 1.09× | 0.82 | 0.82 |

Byte and allocation ratios are native values divided by interpreter values.
### Linux arm64

| Workload | Interpreter µs | Native µs | Speed improvement | Byte ratio | Allocation ratio |
| --- | ---: | ---: | ---: | ---: | ---: |
| `startup` | 0.18 | 0.09 | 2.06× | 0.57 | 0.50 |
| `list_sum` | 1.03 | 0.56 | 1.79× | 0.65 | 0.66 |
| `map_1000` | 899.00 | 387.47 | 2.26× | 0.42 | 0.57 |
| `filter_1000` | 772.17 | 277.44 | 2.78× | 0.32 | 0.48 |
| `fold_1000` | 769.21 | 270.01 | 2.85× | 0.29 | 0.50 |
| `records_1000` | 937.89 | 414.39 | 2.29× | 0.47 | 0.52 |
| `bigint_1000` | 801.53 | 290.75 | 2.72× | 0.37 | 0.50 |
| `map_10000` | 9608.69 | 4977.95 | 1.98× | 0.45 | 0.57 |
| `filter_10000` | 9302.68 | 3498.12 | 2.60× | 0.34 | 0.48 |
| `fold_10000` | 9155.76 | 3703.98 | 2.54× | 0.31 | 0.50 |
| `records_10000` | 11049.80 | 5478.39 | 2.00× | 0.50 | 0.52 |
| `bigint_10000` | 8683.25 | 3548.45 | 2.59× | 0.38 | 0.50 |
| `unicode` | 90.93 | 91.07 | 1.00× | 0.99 | 0.54 |
| `training_mvp` | 8526.20 | 5758.72 | 1.50× | 0.74 | 0.81 |
| `training_dense` | 3637.05 | 3170.44 | 1.14× | 0.82 | 0.82 |

## Process And HTTP Results

Every macOS process, File, and HTTP gate passed.
File execution took approximately 96% of interpreter process time.
HTTP p95 ratios were approximately `1.00` for echo and `0.99` for the computed response.
Both HTTP throughput gates passed with their uncertainty intervals.

Every Linux process, File, and HTTP gate also passed.
Linux File execution took approximately 72% of interpreter process time.
Both Linux HTTP p95 ratios were approximately `0.99`.

| Workload | macOS time ratio | Linux time ratio |
| --- | ---: | ---: |
| `startup` | 0.917 | 0.689 |
| `list_sum` | 0.932 | 0.657 |
| `map_1000` | 0.678 | 0.553 |
| `filter_1000` | 0.680 | 0.564 |
| `fold_1000` | 0.669 | 0.554 |
| `records_1000` | 0.687 | 0.540 |
| `bigint_1000` | 0.675 | 0.570 |
| `map_10000` | 0.066 | 0.074 |
| `filter_10000` | 0.060 | 0.058 |
| `fold_10000` | 0.057 | 0.054 |
| `records_10000` | 0.070 | 0.071 |
| `bigint_10000` | 0.061 | 0.063 |
| `unicode` | 0.904 | 0.652 |
| `file` | 0.962 | 0.717 |
| HTTP echo `p95` | 0.996 | 0.992 |
| HTTP echo `reciprocal_throughput` | 0.989 | 0.983 |
| HTTP computed `p95` | 0.986 | 0.994 |
| HTTP computed `reciprocal_throughput` | 0.988 | 0.990 |

Each ratio is native time divided by interpreter time.
All upper uncertainty bounds are below `1.1`.

## Build Resources

| Target | Maximum cold build | Maximum warm build | Peak sampled memory | Largest artifact |
| --- | ---: | ---: | ---: | ---: |
| `darwin/arm64` | 6.82 s | 0.74 s | 685 MiB | 14.20 MiB |
| `linux/arm64` | 6.71 s | 0.81 s | 613 MiB | 13.43 MiB |

Each target completed 15 empty-cache probes and 150 measured warm builds.
All resource measurements met the selected limits.

The builder enforces source, elapsed-time, diagnostic-output, and artifact-size limits.
The memory limit is a qualification criterion.
These measurements do not establish a process memory cap.

Large argument lists use generated helper functions with at most 64 arguments per helper.
This change reduced Go compilation memory for the 10,000-element corpus.
Small integer literals use direct integer construction at their original evaluation points.
Generated flows allocate error-frame inputs only after a failure.
These changes retain the logical charge order and immutable value contracts.
The [initial allocation profile](evidence/native-allocation-profile.txt) identified integer construction and successful flow calls as allocation costs.

## Reproduction

Run semantic acceptance with:

```bash
make test-native TEST_ARGS='-count=1 -timeout=20m'
make test-native-race \
  TEST_ARGS='-run "TestServe(RepeatedActivationAndConcurrentRequests|GracefulActiveRequest|ShutdownPendingConnection|SelfHandledFailures)|TestNative(TensorBudgetDifferential|RuntimeDifferential)" -count=1'
```

Use a new task-owned directory for each benchmark invocation.
Run the timing commands sequentially.

```bash
make -s benchmark-native NATIVE_BENCH_LANE=execution \
  NATIVE_BENCH_WORK=_scratch/native-qualification/execution
make -s benchmark-native NATIVE_BENCH_LANE=process BENCH_COUNT=30 \
  NATIVE_BENCH_WORK=_scratch/native-qualification/process
make -s benchmark-native NATIVE_BENCH_LANE=build \
  NATIVE_BENCH_WORK=_scratch/native-qualification/build
```

The [execution data](evidence/native-darwin-execution.json), [process data](evidence/native-darwin-process.json), and [build data](evidence/native-darwin-build.json) retain macOS samples.
The Linux [execution data](evidence/native-linux-execution.json), [process data](evidence/native-linux-process.json), and [build data](evidence/native-linux-build.json) retain the corresponding Linux samples.
The [initial execution baseline](evidence/native-baseline-go.txt) and [initial CLI baseline](evidence/native-baseline-cli.json) precede native qualification.
The current paired comparisons control the implementation order and measurement conditions for acceptance.
The initial macOS callback medians show a 2.64× improvement against the final native measurements.

Initial baseline collection included concurrent setup activity.
The paired acceptance measurements exclude that activity.
