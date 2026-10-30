# Dynamic Traversal Performance

The initial resource contract increased execution time and allocation costs in existing collection operations.
The initial interpreter worklists had linear allocation growth in the measured cases.
The initial native runtime also had repeated graph traversal during scope cleanup.
Those results did not show unchanged performance.
The [performance changes](resource-performance-remediation.md) record the subsequent implementation and measurements.

## Comparison

The baseline is Git revision `0be6847ccf3d2a66b57f0505ded7ae5b4be9888a`, before the dynamic traversal changes.
The comparison uses the initial dynamic traversal implementation from the preceding validation run.
Both implementations use the same Go version, compiler tools, host, workload source, and execution step budget.
Each implementation uses its default resource quotas.

The baseline compiler uses a Go source overlay in the primary checkout.
The baseline native build uses only the historical runtime and required process source inputs.
No second checkout or worktree is used.

The current ABI is `lo-native-3`.
The baseline ABI is `lo-native-2`.
Each compiler builds its artifacts with its related runtime.
The existing native preparation directory has stale source identities and is excluded from the comparison.
The evidence records the exact source and artifact identities.

## Collection Operations

Each case has nine samples per implementation and backend.
The implementation order changes between the three rounds.
Each round has three samples.
Every sample verifies the returned integer.

The measured interval excludes source compilation.
The interpreter helper uses the public `runner.Execute` entry point with a compiled program.
Its allocation counters include execution setup, execution, cleanup, and scalar output.

The native measurements include process startup and runtime admission.
Native compilation occurs before measurement.
Every comparison launch remains in the sample set.
The native list-state cases also have earlier current-artifact calibration launches.
The archive keeps those calibration results separately.

No startup time is subtracted.

The following times are medians in milliseconds for 8,192 input items.
Each pipeline first creates its input with `List.unfold`.
Thus, each row includes input construction.

| Pipeline | Interpreter before | Interpreter current | Ratio | Native before | Native current | Ratio |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `List.unfold` and length | 8.06 | 19.75 | 2.45 | 22.02 | 31.25 | 1.42 |
| `List.map` and sum | 17.34 | 41.00 | 2.36 | 18.57 | 33.40 | 1.80 |
| `List.filter` and length | 19.29 | 39.21 | 2.03 | 14.10 | 27.31 | 1.94 |
| `List.fold` with integer state | 13.86 | 30.88 | 2.23 | 19.49 | 29.87 | 1.53 |
| `Flow.fold` with integer state | 18.26 | 32.27 | 1.77 | 19.61 | 34.78 | 1.77 |

Interpreter allocation volume approximately doubles in these pipelines.
For input construction alone, allocation volume increases from 11.08 MiB to 22.84 MiB.
Allocation requests increase from 270,758 to 418,563.
The logical live-value quota does not represent these allocator bytes.

### Scope Cleanup

A separate `List.fold` callback returns the same list supplied as its state.
This case avoids `Flow.fold` progress snapshots.
Its scalar result is the final list length.

| Items and callbacks | Native before, ms | Native current, ms | Ratio |
| --- | ---: | ---: | ---: |
| 128 | 6.33 | 7.48 | 1.18 |
| 256 | 6.58 | 10.10 | 1.53 |
| 512 | 8.10 | 23.88 | 2.95 |
| 1,024 | 8.88 | 91.00 | 10.24 |

The source review identifies the additional repeated traversal.
`value_resources::Ledger::scope_keep` and `scope_retain` construct the complete reachable identity set, including when the scope owns no values.
Each callback closes a scope, and the fold also retains its state.
A callback count proportional to the state size causes quadratic graph visits.
The ordered identity set adds lookup cost to those visits.
The Go runtime already skips some graph visits when a scope has no release candidates.

The interpreter CPU profile also shows scope cleanup and map operations.
The profile includes forced garbage collection between repetitions, outside the measured execution interval.
Its whole-process percentages do not represent execution percentages.
The allocation counters and repeated timings provide the primary comparison.

## Growable Worklists

The allocation table uses interpreter helper counters.
The study uses both a supplied worklist and a chain that discovers one new item per callback.
Each callback returns the required state, child list, and stop flag.
These cases measure the complete processor contract.
They do not isolate a queue insertion from callback and progress costs.

| Items | Supplied worklist allocation requests | Supplied worklist allocation MiB | Discovered chain allocation requests | Discovered chain allocation MiB |
| --- | ---: | ---: | ---: | ---: |
| 512 | 68,188 | 3.67 | 70,580 | 3.13 |
| 2,048 | 271,054 | 14.52 | 281,008 | 12.34 |
| 8,192 | 1,082,465 | 57.94 | 1,122,742 | 49.18 |
| 32,768 | 4,328,059 | 232.25 | 4,489,663 | 196.57 |

Four times the work causes approximately four times the allocation requests and bytes.
The source review confirms that the queues do not repeatedly copy pending items.
The Go implementation uses chunks, and the native implementation uses `VecDeque`.
The tests do not expose a queue growth regression at these sizes.
The allocation cost per item still requires attention.

### Dynamic Directory Scans

A separate worklist discovers directories from each `File.scan` result.
Each child directory contains eight regular files.
The source filters directory entries before it adds new paths to the worklist.
Both runtimes keep the default resource quotas.
Each cell has nine verified samples.

| Directories visited | Regular files observed | Interpreter median, ms | Native median, ms |
| ---: | ---: | ---: | ---: |
| 33 | 256 | 8.86 | 17.84 |
| 129 | 1024 | 35.31 | 45.75 |
| 513 | 4096 | 157.32 | 154.53 |

The largest case visits 513 directories with a default quota of 128 simultaneously retained directory observations.
The fixture contents remain unchanged after each backend completes.
These current-implementation measurements establish scale behavior for this warm-cache fixture.
They do not provide a pre-change throughput comparison for dynamic traversal.

## File Operations

The file campaign uses workloads from the repository `filebench` tool and generated fixtures inside Lo.
It measures listing, scanning, removal, guarded removal, reads, writes, and appends.
Each timing case has three verified samples per implementation and backend.
Paired rounds change the implementation order.
Both implementations use the same source and fixture path.
Native compilation and fixture setup occur outside the measured interval.

The interpreter file timings include source compilation through the public CLI.
The native file timings include process startup and runtime admission.
The filesystem cache is uncontrolled and normally warm after fixture setup.
The small sample set does not establish a production latency distribution.

An initial broad campaign completed 200 baseline samples.
The current campaign then failed during fixture creation with `no space left on device`.
Both broad campaigns are excluded from the file timing comparison.
The replacement campaign keeps one fixture group and removes it after measurement.
The archive preserves the failed campaign and its reported filesystem error.

The original producer links the current interpreter allocation worker in both broad campaigns.
Those worker counters do not measure the baseline implementation and are excluded from before/current allocation comparisons.
The collection helper has separate baseline and current binaries for its allocation comparison.

The following file times are sample medians in milliseconds.

| Workload | Size | Interpreter before | Interpreter current | Native before | Native current |
| --- | ---: | ---: | ---: | ---: | ---: |
| `startup` | 1 | 24.70 | 15.82 | 15.02 | 14.86 |
| `list` | 5000 | 71.13 | 73.43 | 132.72 | 125.94 |
| `scan` | 5000 | 61.74 | 66.66 | 125.68 | 112.10 |
| `remove` | 1000 | 256.69 | 288.07 | 207.02 | 145.09 |
| `checked-remove` | 1000 | 222.04 | 232.67 | 224.47 | 191.91 |
| `read` | 1048576 | 23.19 | 26.34 | 11.80 | 15.37 |
| `write` | 1048576 | 20.18 | 27.63 | 12.17 | 14.25 |
| `append` | 1048576 | 13.37 | 13.53 | 20.41 | 15.56 |
| `append-many` | 1000 | 80.66 | 108.95 | 104.15 | 91.75 |

Size is an entry count for list, scan, and removal cases.
Size is a byte count for read, write, and append cases.
The `append-many` size is the number of 64-byte appends.
The startup size is one scalar result.

These small samples have mixed timing changes.
They do not establish unchanged file throughput or a general file throughput regression.
The collection allocation results give stronger evidence for the resource accounting regressions.

The native removal allocation tests also pass on Darwin and Linux ARM64 QEMU.
At 256 entries, unconditional removal retains 22,690 allocator requests and 2,024,275 bytes.
Guarded removal uses 48,452 requests and 4,463,454 bytes.
The earlier recorded result has 45,730 requests and 4,297,622 bytes.
Thus, guarded removal adds approximately 6.0 percent more requests and 3.9 percent more bytes.
Both removal forms retain linear allocation growth at 64, 128, and 256 entries.

## Required Follow-Up

1. Skip native identity traversal when a scope has no values to release or retain.
2. Reuse one reachable identity set when scope close requires both release and retention.
3. Create Go scope candidate maps only when the scope first needs them.
4. When a value has neither resource, do not create tensor or file-lease pin records.
5. Measure the effect of scope storage and shared graph collectors on Go allocation costs.
6. Preserve unique value identities, quota enforcement, resource retention, typed failures, and confirmed progress.
7. Add public allocation growth checks for the repeated-list-state case.
8. Repeat the comparisons after the changes.

Keep the dynamic traversal change outside performance acceptance until the large regressions are corrected.
Correctness validation alone does not establish performance acceptance.
The initial study changed no compiler or runtime production source.

## Evidence

The [summary](evidence/dynamic-traversal-performance.json) records the sample summaries and source identities.
The [archive](evidence/dynamic-traversal-performance.json.gz) contains individual samples, workload sources, native allocation logs, and the independent source review.
All timing results use the local Darwin ARM64 CPU environment.
The filesystem error limits conclusions about host storage conditions during the study.
Linux ARM64 QEMU supplies removal allocation and correctness evidence.
The study does not claim Linux host throughput, cold-cache throughput, or GPU performance.
