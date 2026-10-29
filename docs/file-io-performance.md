# File I/O Performance

This study measures the current File operations through public Lo entry points.
Each removal uses a generated fixture directory inside Lo.
The resource limits and File authority rules remain unchanged.

## Run The Benchmarks

Use a fresh preparation directory for the native runtime.

```bash
make prepare-native-runtime NATIVE_RUNTIME_STATE=_scratch/file-io-runtime
make build-native-runtime NATIVE_RUNTIME_STATE=_scratch/file-io-runtime
make benchmark-file-io NATIVE_RUNTIME_STATE=_scratch/file-io-runtime \
  FILE_BENCH_ARGS='-label current -samples 3'
```

The benchmark creates a unique output directory under `FILE_BENCH_WORK`.
The default parent is the repository build directory.
Use an existing directory inside `Lo/_scratch` when you set `FILE_BENCH_WORK`.
The output includes `report.json`, generated programs, and individual samples.

Select a smaller workload when you investigate one cost.

```bash
make benchmark-file-io NATIVE_RUNTIME_STATE=_scratch/file-io-runtime \
  FILE_BENCH_ARGS='-cases remove,checked-remove -sizes 100,1000,5000 -samples 3'
make benchmark-file-io NATIVE_RUNTIME_STATE=_scratch/file-io-runtime \
  FILE_BENCH_ARGS='-cases read,write,append -io-sizes 4096,65536,1048576,16000000 -samples 3'
make benchmark-file-io NATIVE_RUNTIME_STATE=_scratch/file-io-runtime \
  FILE_BENCH_ARGS='-cases directories -directory-counts 1,32,128,129 -samples 3'
```

## Measurement Methods

- Process times include startup, program admission, execution, cleanup, and the scalar JSON result.
- Interpreter process times also include source compilation.
- The native build completes before the measured process starts.
- Separate interpreter allocation samples start after source compilation.
- Native allocation tests count successful allocator requests during removal activation.
- RSS results report peak process memory.
- Fixture setup writes the input files before each sample.
- The report retains every sample and records the file cache conditions.
- Read samples include UTF-8 conversion and a scalar result.
- Write and append samples include a payload read.

Compare before and after results within one measurement method.
The interpreter and native allocation measurements have different scopes.
Darwin samples supply execution times for this host and these fixtures.
The Linux emulator checks correctness and allocation growth.

The benchmark requests 100,000,000 execution steps for these workloads.
This request does not change the default execution budget or the data limits.
Native builds and fixture setup occur outside the measured interval.
The filesystem cache is uncontrolled after fixture setup.
The report retains the first process launch and all later launches.

The median and sample percentile values summarize the retained samples.
Three samples do not show a production latency distribution.
Use deterministic allocation growth for regression gates.
Keep filesystem timing as descriptive evidence.

## Capacity Limits

| Resource | Current limit |
| --- | --- |
| Enumerated directory children | 10,000 |
| Selected removal entries | 10,000 |
| Retained directory observations | 128 per invocation |
| Raw file read | 16 MiB |
| Expanded value | 16 MiB |
| Cumulative evaluation data | 64 MiB |

Value headers, paths, conversions, and intermediate values also consume the evaluation budget.
A workload can reach a value or evaluation limit before the raw read limit.
The benchmark verifies the typed `runtime.limit` result for capacity cases.
If the workload requires more capacity, define a separate traversal contract.

A selected batch of 10,000 entries can exceed the cumulative budget during guarded removal.
One fixture stopped after 9,276 removals and retained 724 files.
The interpreter and native artifact produced identical progress with the same source, arguments, and 149-byte fixture path.
A separate native fixture with a shorter path completed all 10,000 removals.
The entry limit does not guarantee that every permitted batch fits the data budget.
The default removal samples stop at 5,000 entries.

## Regression Checks

```bash
make test-integration TEST_ARGS='-run TestFileReadValidationAllocation -count=1 -v'
make test-integration TEST_ARGS='-run TestFileObservationDirectoryCapacity -count=1 -v'
make test-native NATIVE_RUNTIME_STATE=_scratch/file-io-runtime \
  TEST_ARGS='-run TestLLVMFileRemovalAllocationScaling -count=1 -v'
make test-native NATIVE_RUNTIME_STATE=_scratch/file-io-runtime \
  TEST_ARGS='-run TestLLVMLinuxFileRemovalAllocationScaling -count=1 -v'
make test-filebench
```

The native removal test uses 64, 128, and 256 entries.
It verifies unconditional removal and removal with a total guard.
It checks discovery metadata, preflight metadata, immediate revalidation, removal results, and resource cleanup.
Each doubled batch permits at most three times the previous allocation count and byte volume.

The read test supplies a preconstructed immutable 4 MiB value at the external boundary.
It measures execution allocations and verifies the complete returned payload.
A separate case verifies the typed rejection of an oversized boundary result.
Real filesystem workloads supply the end-to-end measurements.

The directory test verifies 128 distinct observations and reuse of an existing observation.
It verifies that the 129th distinct observation returns `runtime.limit`.

Exact negative timestamp checks use injected metadata in the interpreter and both native targets.
Real filesystem checks verify the owner and stable timestamp fields.

## Measured Changes

The native removal implementation repeatedly rebuilt the complete diagnostic entry list during progress updates.
This produced quadratic allocation work for an immutable removal batch.
The Go read validator copied the complete byte value to obtain its length.
Both public allocation tests failed before the production changes.

The native runtime now creates one immutable diagnostic snapshot for the removal batch.
Progress updates share this snapshot.
The Go validator now reads the immutable byte count without a payload copy.

The measured baseline used the following native allocator counts.

| Entries | Unconditional requests | Checked requests | Unconditional bytes | Checked bytes |
| --- | ---: | ---: | ---: | ---: |
| 64 | 18,658 | 24,418 | 3,120,689 | 3,688,329 |
| 128 | 61,730 | 73,250 | 11,334,094 | 12,469,265 |
| 256 | 221,602 | 244,642 | 43,096,915 | 45,370,260 |

The same native tests produced the following results after the snapshot change.

| Entries | Unconditional requests | Checked requests | Unconditional bytes | Checked bytes |
| --- | ---: | ---: | ---: | ---: |
| 64 | 5,794 | 11,554 | 520,241 | 1,087,881 |
| 128 | 11,426 | 22,946 | 1,021,390 | 2,158,102 |
| 256 | 22,690 | 45,730 | 2,024,275 | 4,297,622 |

Allocation requests now grow linearly with these batch sizes.
At 256 entries, unconditional allocation bytes decreased by 95.3 percent.
The corresponding Linux tests also passed the allocation growth gates.
Each removal still performs discovery, preflight, and immediate metadata observations.
The tests verify all three observations for every entry.

The Go validation test decreased from 4,231,992 allocation bytes to 37,640 bytes for its preconstructed 4 MiB input.
The public 1 MiB read pipeline decreased from about 6.54 million bytes to 5.47 million bytes of allocation.
That pipeline includes input, UTF-8 conversion, and result serialization.
Other required copies remain outside the validator.
The oversized-read case retained its typed failure.
The directory capacity checks passed in both runtimes before and after the changes.

The following Darwin process times are medians of three retained samples, in milliseconds.
They include the public command costs described above.

| Workload | Entries | Interpreter before | Interpreter after | Native before | Native after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Unconditional removal | 1,000 | 102.97 | 101.38 | 168.98 | 93.96 |
| Unconditional removal | 5,000 | 517.23 | 492.66 | 2,993.32 | 442.57 |
| Unconditional removal | 10,000 | 1,060.12 | 1,003.00 | 12,508.11 | 918.51 |
| Guarded removal | 5,000 | 533.81 | 505.57 | 2,813.34 | 451.34 |
| File.list | 10,000 | 71.92 | 70.91 | 75.40 | 72.28 |
| File.scan | 10,000 | 74.08 | 72.92 | 75.38 | 87.38 |
| Repeated 64-byte append | 10,000 | 419.81 | 347.48 | 395.75 | 336.20 |

The native 10,000-entry removal median decreased by a factor of 13.6 on this host.
The deterministic allocation tests identify the algorithmic improvement.
Small timing differences in scans and appends do not show an algorithmic change.
The workload includes a native first-launch cost, filesystem variation, and process startup.
The report preserves those costs.

The matrix also covers startup, byte transfers, directory observations, and the real `File.open_paths` probe.
Payloads range from 4 KiB through the 16 MiB boundary.
Both complete general matrices contain 198 verified samples.
The probe took about 0.6 seconds per process on this host.
Repeated probe calls can dominate otherwise short scans.

## Evidence And Scope

The [measurement archive](evidence/file-io-performance.json.gz) contains individual samples, summaries, implementation hashes, workload sources, and regression output.
It retains the incomplete baseline removal campaign and its typed capacity failure.
The archive also records the same-path budget comparison.
The benchmark rejects unexpected failures and reports the observations collected before the failure.

These fixtures show costs for the current bounded operations.
They do not show throughput for an unbounded filesystem traversal or a cold filesystem cache.
Longer paths, larger payloads, and retained intermediate values consume additional budget.
Applications must stay within the current limits.
Larger traversals require a separate language design for bounded observation lifetime and incremental results.
