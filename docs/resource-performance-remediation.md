# Resource Tracking Performance

This revision corrects the resource tracking costs found in the [initial study](dynamic-traversal-performance.md).
The corrected interpreter completes large collection operations in approximately the same time or less.
The native allocation checks also show large improvements.
The native timing comparison still shows additional collection costs.
Complete local CI validation passed.

## Implementation

The interpreter keeps recent value records in a small array.
Other live value records use a growable map.
Both stores use the same quota accounting.
Value scope cleanup visits the returned graph once and moves kept records before it releases the remaining local records.
The interpreter uses small empty buffers and cleared records again.
The interpreter releases large candidate arrays.

The interpreter stores immutable flow metadata when it prepares a program.
Each execution machine uses its own cleared binding maps again.
Concurrent program executions keep separate maps.
Error handlers receive argument snapshots after the body releases its binding map.
The cancellation checks use the context's `Done` channel.
The error type and cancellation precedence remain unchanged.

The interpreter reads the stored length for `List.length`.
The previous path copied the complete item slice.
The repeated-length fixture now has linear allocation growth.

Both runtimes release successful filter predicate results after each decision.
The input list continues to own selected elements.
Both runtimes release the unused final unfold state after they keep the output values.
A public test filters 1,024 integers to an empty list with a 45 KiB live-value quota.
The initial implementation returns `runtime.limit` in that test.
The corrected implementation completes successfully.

The native runtime skips identity traversal for empty scopes and single primitive roots.
Value scope close uses one reachable identity set.
Native value records also use recent slots and a growable map.
Each record has one location.

Private identity collections use integer hashing.
The graph walk uses borrowed handles and inline pending storage.
Larger graphs use a growable vector.
Bulk retention reduces sparse hash capacity.
Tensor and file ledgers keep their ordered cleanup rules.
Weak references still permit external output release and repeated context invocations.

Both runtimes store immutable graph identities and maximum list widths for inherited roots.
Summary membership checks preserve receipts for descendants first charged in a later scope.
Fresh wrappers use cached child membership without copying the child identity set.

Receipt release removes its graph summary.
Summary storage grows with charged live roots.
Membership checks use the smaller side of the graph and local receipt sets.

Pin receipts contain no identities, one identity, or a growable vector.
At 2,048 discovered work items, the compact receipts remove 2,048 allocation requests and 16,384 allocated bytes.
This comparison uses the corrected runtime before and after the pin receipt change.

Hash tables can keep peak bucket capacity until session or context destruction.
Receipt release removes the related summary sets.
The live-value quota measures logical weights, not allocator capacity or process RSS.

## Interpreter Comparison

The comparison uses three implementations:

1. The exact pre-change revision `0be6847ccf3d2a66b57f0505ded7ae5b4be9888a`.
2. The initial dynamic traversal implementation from the first study.
3. The corrected workspace implementation.

All implementations use the same host, Go version, workload source, and execution step budget.
Each implementation keeps its own default resource policy.
The changes include dynamic sizing, live quotas, and lifetime tracking.
The comparison measures their combined cost.
The first two implementations use source overlays in the primary checkout.
No second checkout or worktree is used.
Each cell has 15 verified samples.
Three rounds change the implementation order.

Source compilation occurs before measurement.
The public execution helper measures runner setup, execution, cleanup, and scalar JSON output.
Every sample verifies the expected result.
Each sample set includes the first sample.
The table reports sample medians in milliseconds for 8,192 input items.
Each pipeline includes its input construction with `List.unfold`.

| Pipeline | Pre-change | Initial dynamic | Corrected |
| --- | ---: | ---: | ---: |
| Unfold and length | 6.656 | 15.600 | 6.753 |
| Map and sum | 12.142 | 28.097 | 12.334 |
| Filter and length | 11.418 | 27.859 | 10.530 |
| List fold | 11.017 | 25.267 | 9.979 |
| Flow fold | 13.992 | 29.254 | 13.243 |

Unfold and map are within approximately two percent of the pre-change medians.
The other corrected medians are lower in this sample set.
Host scheduling and garbage collection can change small timing differences.
These samples do not establish a production latency distribution.

## Native Comparison

The native comparison uses the same three implementations and 15 verified samples per cell.
Each compiler builds a separate executable with its related runtime.
Compilation occurs before measurement.
The measured interval includes process startup, runtime admission, evaluation, cleanup, and scalar JSON output.

The table reports medians in milliseconds for 8,192 input items.

| Pipeline | Pre-change | Initial dynamic | Corrected |
| --- | ---: | ---: | ---: |
| Unfold and length | 9.325 | 16.320 | 11.257 |
| Map and sum | 16.330 | 30.026 | 21.571 |
| Filter and length | 16.416 | 29.447 | 20.037 |
| List fold | 12.355 | 21.984 | 14.748 |
| Flow fold | 15.393 | 24.147 | 18.072 |

The corrected native medians remain approximately 17 to 32 percent above the pre-change medians.
These results do not show unchanged native collection performance.
The correction removes the largest repeated traversal cost.
At 1,024 items, inherited-list fold measures 5.561 ms.
The initial implementation measures 57.569 ms, and the pre-change implementation measures 5.358 ms.

The 32,768-item worklists also improve compared with the initial implementation.
The wide queue decreases from 110.921 to 74.761 ms.
The chain decreases from 102.771 to 84.190 ms.
The pre-change language has no `Flow.worklist` operation.

Longer native runs compare the identity hash implementations.
Each sample executes 256 map or fold operations over 8,192 items in one invocation.
Nine samples per implementation change the execution order.
The integer hash decreases median child CPU time by 6.6 percent for map and 3.3 percent for fold.
The old fixed resource contract rejects this long fixture.
This hash comparison applies only to revisions of the dynamic implementation.
It precedes the graph-summary correction.

A separate comparison executes four map or fold operations per invocation.
Both the exact pre-change runtime and corrected runtime complete this fixture under their default policies.
Each cell has 15 samples with wall time and child CPU time.

| Four operations over 8,192 items | Pre-change child CPU, ms | Corrected child CPU, ms |
| --- | ---: | ---: |
| Map | 17.440 | 24.672 |
| Fold | 16.666 | 21.370 |

The corrected median child CPU time is approximately 41 percent higher for map and 28 percent higher for fold.
The repeated workload also shows higher native CPU cost.

## Native Allocation Checks

The public allocator fixture measures only the fold operation after input construction.
The callback returns the same list supplied as its state.
The initial implementation performs repeated identity traversal.
The corrected implementation has two allocation requests at every tested size.

| Items and callbacks | Initial requests | Corrected requests | Initial bytes | Corrected bytes |
| --- | ---: | ---: | ---: | ---: |
| 128 | 9,009 | 2 | 1,353,632 | 56 |
| 256 | 34,909 | 2 | 5,472,192 | 56 |
| 512 | 132,783 | 2 | 21,376,208 | 56 |

A second fixture checks a scalar fold with 2,048 callbacks.
The initial implementation makes 24,584 allocation requests.
The corrected implementation makes 4,097 requests and allocates 426,032 bytes.
Each new integer needs storage for its integer data and value handle.
The corrected fixture has a fixed allocation budget for both request count and byte volume.
The fixtures verify the scalar result and the configured resource policy.
The complete timing comparison uses separately compiled native executables.

Additional public fixtures cover inherited lists with fallible callbacks and unused local values.
They also cover fresh records with two or five independent inherited lists.
The tests include positive list-item quotas.
These cases exposed additional quadratic graph traversal after the empty-scope correction.
A summary cache with four roots also failed the five-list case.
The final cache follows receipt lifetimes and passes all tested growth cases.

| Fixture at 512 items | Before bytes | Corrected bytes |
| --- | ---: | ---: |
| Go repeated length | 5,851,104 | 1,001,184 |
| Native fallible inherited-list fold | 11,790,392 | 309,480 |
| Native inherited-list fold with unused local | 11,626,552 | 145,640 |
| Go five-list record with list quota | 482,558,640 | 5,353,384 |
| Native five-list record with list quota | 174,680,524 | 1,211,188 |

The first three rows compare the implementation before each correction with its corrected implementation.
The last two rows compare the cache with four roots and the final cache.
All rows verify the program result.
The growth checks use 128, 256, and 512 input items.

## Regression Coverage

The interpreter allocation tests cover input construction, map, fold, and queued scalar work.
Their request and byte ceilings protect the reduced execution costs.
The native fixtures check allocation growth and scalar allocation cost.

They also check structured unfold callbacks and queued worklist pins.
The lifetime test checks immediate release of predicate results under a live-value quota.
Repeated-call tests check error inputs and traces.
Concurrent-call tests check independent results through one shared prepared program.

Quota, cancellation, tensor retention, file lease, cleanup failure, and confirmed progress tests remain required.
The implementation keeps distinct scalar identities and host authority checks.
It adds no queue count ceiling.
Repeated scope and invocation tests check graph-summary release under low quotas.
The native harness also checks outstanding allocation counts across 100 invocations.

## File And Tree Measurements

The final file comparison has 15 paired samples per implementation and backend for each case.
It contains 540 verified samples.
Each round changes the implementation order.
Each sample uses a prepared fixture and checks its result and file mutations.

The interpreter interval includes source parsing and checking.
The native interval includes process startup and runtime admission.
Both intervals include execution, cleanup, and scalar JSON output.
Compilation of native artifacts occurs before measurement.

The table gives median milliseconds.

| Operation | Go pre-change | Go corrected | Native pre-change | Native corrected |
| --- | ---: | ---: | ---: | ---: |
| Startup control | 5.160 | 5.526 | 4.280 | 4.298 |
| List 5,000 entries | 29.254 | 29.244 | 31.466 | 33.595 |
| Scan 5,000 entries | 29.695 | 29.689 | 28.056 | 28.712 |
| Remove 1,000 entries | 111.133 | 115.040 | 100.396 | 102.153 |
| Guarded remove, 1,000 entries | 114.104 | 115.078 | 96.301 | 100.309 |
| Read 1 MiB | 6.998 | 7.141 | 4.704 | 4.741 |
| Write 1 MiB | 7.573 | 7.485 | 5.120 | 4.743 |
| Append 1 MiB | 7.119 | 7.467 | 5.184 | 5.208 |
| Append 1,000 chunks | 40.398 | 40.631 | 37.674 | 38.285 |

The Go list and scan medians differ by less than one percent in these fixtures.
The native list median increases by 6.8 percent, and the native scan median increases by 2.3 percent.
Some other cases also remain slower.
The table uses only samples from the final source.
Earlier campaigns remain historical evidence.

Filesystem state and host activity can change these intervals.
These fixtures do not establish unchanged file throughput or performance for larger files.

The corrected tree fixture discovers paths through `Flow.worklist` and scans each authorized directory.
Each backend has nine samples at each size.
The fixture checks the observed entry count and all file contents.

| Directories | Files | Observed entries | Go, ms | Native, ms |
| --- | ---: | ---: | ---: | ---: |
| 33 | 256 | 288 | 3.479 | 8.744 |
| 129 | 1,024 | 1,152 | 12.770 | 19.338 |
| 513 | 4,096 | 4,608 | 57.928 | 66.767 |

The tree helper excludes source compilation from the interpreter interval.
The native interval includes process startup and runtime admission.
The largest case completes with the default quota of 128 live directory observations.
This is a corrected-runtime scaling check, with no pre-change comparison.

## Evidence And Validation

The initial study and its measurement archive remain historical evidence.
The [manifest](evidence/resource-performance-remediation.json) records source identities, artifact identities, checks, and final sample sets.
The [measurement archive](evidence/resource-performance-remediation.json.gz) contains the initial failures, corrected tests, helper source, and individual samples.
Only local software execution is in scope.
The native checks use Darwin ARM64 and Linux ARM64 QEMU.

Local `make ci` passed after the final implementation and correction to the preparation layout.
It includes the complete Go and native integration suites, compiler build, semantic report checks, and qualification-tool checks.
The qualification-tool suite passed 217 tests.
The canonical preparation distribution is byte-identical to the measured distribution.
Independent source reviews found no unresolved correctness findings.
The remaining native timing gap prevents acceptance of unchanged performance.
