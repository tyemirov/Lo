# Native Compiler Qualification Findings

This document records I011 findings before changes to address them.
It separates completed measurements, partial observations, and possible investigations.
The current campaign continues with its declared inputs and unchanged acceptance thresholds.
This document does not authorize a new experiment or report completed qualification.

## Evidence Snapshot

The snapshot time is `2026-10-22T02:47:26Z`.
The source identity is `b81dcd1d1928d92fe44c582488c05d4090651a8bf2051576aef0a8b60208d2dc`.
The reference implementation is the Lo interpreter.
Darwin measurements execute natively on arm64. The selected Linux environment is QEMU HVF on arm64.
Linux measurement has not started at this snapshot.

The [snapshot observations](evidence/standalone-compiler/final/measurement-capture-final/findings-snapshot-1/observations.json.gz) contain exact values and source digests.
The [preservation index](evidence/standalone-compiler/preservation-index.json) records the archived bytes and their SHA-256 digests.
These copies preserve the snapshot while the campaign continues.

| Lane | Darwin arm64 | Linux arm64 |
| --- | --- | --- |
| Checked execution | Complete. All 15 cases passed. | Not started. |
| Process latency | Complete. All 14 cases passed. | Not started. |
| Persistent HTTP | Complete. All four gates are inconclusive. | Not started. |
| Build resources | Partial. Eight cases have observations. | Not started. |

Both complete target merges and the final evidence audit remain pending.
I011 remains open. An inconclusive result does not satisfy acceptance.

## Confirmed Correctness And Offline Results

Fresh CI passed 2,635 native cases without failures or skips.
The interpreter lane passed 1,741 cases, with 157 native-only exclusions.
All 197 qualification-tool checks passed.
These checks include the complete B005 capture correction and B006 campaign cleanup correction.

The [nonperformance receipt](evidence/standalone-compiler/nonperformance-validation.json) retains the earlier full-corpus and offline results.
The [reuse receipt](evidence/standalone-compiler/nonperformance-reuse.json) records the reviewed source differences and 339 unchanged package files.
The earlier checks retain their original source identity.
All four software host-to-target combinations passed offline compilation and copied-artifact execution without Lo.
Both installed compiler hosts passed 500-step XOR training with final loss `4.251353501009969e-13`.
The semantic evidence also covers backpropagation and checkpoint resumption.

These results support the standalone compiler migration with the tested semantics preserved.
They do not establish performance acceptance.

## Darwin Execution And Process Findings

The callback geometric-mean speedup is **2.244×**, with a 95% bootstrap interval of **2.214–2.269×**.
This result passes the unchanged 2× gate.
All other checked-execution gates and all process gates passed.

| Workload | Native median | Interpreter median | Paired ratio | 95% bootstrap interval |
| --- | ---: | ---: | ---: | ---: |
| Trivial checked execution | 341.5 ns | 405.0 ns | 0.856 | 0.821–0.868 |
| `training_mvp` execution | 8.548 ms | 9.566 ms | 0.890 | 0.881–0.905 |
| `training_dense` execution | 2.208 ms | 4.369 ms | 0.505 | 0.502–0.510 |
| Trivial process execution | 4.779 ms | 5.930 ms | 0.811 | 0.737–0.843 |

The paired ratio divides native elapsed time by interpreter elapsed time within each pair.
The reported estimator uses those paired values. It is not the ratio of the displayed medians.
Checked execution excludes program admission and includes session setup, result release, and complete session cleanup.
Process latency includes executable launch, admission, execution, and output.

These results require no further Darwin execution or launch optimization for the current I011 gates.
They do not establish performance for all workloads or machines.
The training measurements use CPU execution. They do not establish Metal training throughput.

Evidence: [execution report](evidence/standalone-compiler/final/measurement-capture-final/findings-snapshot-1/darwin-execution.json.gz) and [process report](evidence/standalone-compiler/final/measurement-capture-final/findings-snapshot-1/darwin-process.json.gz).

## Darwin HTTP Finding

Both HTTP workloads use one client and 200 requests per sample on a persistent loopback connection.
The campaign retains ten measured pairs after three warmup pairs.
All four bootstrap intervals cross the unchanged 1.1 ratio limit.

| Workload and metric | Paired ratio | 95% bootstrap interval | Gate |
| --- | ---: | ---: | --- |
| Echo p95 latency | 1.129 | 0.646–1.933 | Inconclusive |
| Echo reciprocal throughput | 1.088 | 0.935–1.274 | Inconclusive |
| Computed p95 latency | 1.070 | 0.972–1.798 | Inconclusive |
| Computed reciprocal throughput | 1.132 | 1.084–1.394 | Inconclusive |

Echo median p95 latency is 122.521 µs for native execution and 116.230 µs for the interpreter.
Computed median p95 latency is 68.584 µs for native execution and 67.813 µs for the interpreter.
The intervals do not establish a pass or a performance regression beyond the permitted limit.
This measurement does not identify the cause of the variation.
External load remains recorded context. It does not remove these samples or their gate outcomes.

The next decision concerns measurement precision before a compiler or HTTP runtime change.
A possible investigation must specify its measurement precision and experiment design before execution.
No retry, request-count change, sample removal, or HTTP optimization starts as part of this record.
These sequential workloads do not establish server capacity or production tail latency.

Evidence: [HTTP report](evidence/standalone-compiler/final/measurement-capture-final/findings-snapshot-1/darwin-http.json.gz).

## Partial Build Finding

The fixed corpus constructs varied integer lists directly in source code.
It stresses compilation of large literals as well as callback execution.
It does not represent an application that reads the same list from a file at runtime.
RSS means resident set size.

| Case | Measured builds retained | Median build time | Maximum sampled RSS | Artifact bytes |
| --- | ---: | ---: | ---: | ---: |
| `startup` | 10 of 10 | 1.897 s | 51,937,280 | 2,954,448 |
| `map_1000` | 10 of 10 | 2.778 s | 241,549,312 | 4,887,408 |
| `map_10000` | 6 of 10 | 46.655 s | 1,936,392,192 | 22,043,968 |

The RSS maximum includes the first build, warmup builds, and measured builds available at the snapshot.
The time median includes only the measured builds available at the snapshot.
Short memory peaks can occur between samples.
The `map_10000` measurements and the complete build lane remain unfinished.
These values are observations, not a final build gate result.

Large literal lists expose substantial build time, memory, and artifact costs.
This observation supports an investigation of generated module size and time spent in each compiler stage.
It does not establish whether Lo lowering, LLVM optimization, linking, or another stage causes the cost.
No compiler change follows from this observation alone.
The 180-second, 2 GiB sampled-memory, and 64 MiB artifact limits remain unchanged.

Evidence: [complete partial-record snapshot](evidence/standalone-compiler/final/measurement-capture-final/findings-snapshot-1/darwin-build-partial.json.gz).

## Design Value And Limits

This campaign qualifies fixed workloads against the interpreter in recorded environments.
It can identify areas that need investigation before compiler acceptance.
It cannot select between LLVM and Cranelift because it does not compare those implementations.
It cannot determine the priority of modules, types, diagnostics, or library features.
I012 owns qualification through complete CLI, HTTP service, and training applications.

The [measurement contract](native-compilation-contract.md#measurement-validity) records statistical limitations and the complete acceptance rules.
Quiet-host admission produced no compiler-performance evidence. The current contract removes that condition.
The campaign keeps all samples and runs one owned measurement job at a time.
It performs no automatic retries.

The current work is evidence collection and documentation.
Complete the declared campaign before selecting another investigation.
Preserve each failed or inconclusive gate in the final findings.
Specify the decision and experiment design before any additional measurement.
Do not change acceptance thresholds to obtain a passing result.
