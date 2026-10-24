# Native Compiler Qualification Findings

I011 qualifies the standalone LLVM compiler on Darwin arm64 and Linux arm64.
The final focused run passed all eight HTTP gates with unchanged thresholds.
The user selected HTTP-only requalification with review of the earlier correctness, offline, and non-HTTP evidence.
I011 closed on October 23, 2026, after the evidence audit and independent review passed.
The [completion record](evidence/i011-qualification-20261022/completion.json) binds the evidence, cleanup, and acceptance limits.

The HTTP reports use source identity `581218bdf95a91db62e227cc5798caf40a4377072884e74720547b00937d03c2`.
Each case retains 100 measured pairs after three warmup pairs, with 200 requests per observation.
The limit is 1.1 for both latency and reciprocal throughput.
Every interval below is a 95% bootstrap interval for the native-to-interpreter ratio.

| Target | Case | p95 latency interval | Reciprocal throughput interval | Result |
| --- | --- | --- | --- | --- |
| Darwin arm64 | Echo | 0.808–0.846 | 0.861–0.878 | Pass |
| Darwin arm64 | Computed | 0.817–0.849 | 0.861–0.877 | Pass |
| Linux arm64, HVF | Echo | 0.861–0.961 | 0.996–1.031 | Pass |
| Linux arm64, HVF | Computed | 0.858–0.962 | 0.976–1.033 | Pass |

The [focused HTTP evidence](evidence/i011-qualification-20261022/http-final-20261023/manifest.json) preserves the raw samples and their audit.
The reports keep `qualification_complete=false` because they contain only the HTTP lane.
They do not claim a new complete campaign.
The earlier nonpass reports retain their original outcomes.

## Evidence And Environments

The earlier correctness and offline candidate has source identity `4eda492e14c4e218369aa69ea62b10d386ef3cdec9bf8d2f86ec8ffecc8654fd`.
The reference implementation is the Lo interpreter.
Darwin measurements execute natively on arm64.
Linux measurements use QEMU HVF on arm64.
The software packages use LLVM 21.1.8, LLD 21.1.8, and the static Rust runtime.
The installed compiler supplies its build tools and target link inputs.

The [evidence directory](evidence/i011-qualification-20261022/) contains raw reports, compressed logs, and SHA-256 records.
The [measurement contract](native-compilation-contract.md#measurement-validity) owns the acceptance rules.
The last campaign selected ten measured pairs after three warmup pairs for each target and lane.
It keeps all samples and runs one owned measurement job at a time.
It performs no automatic retries.
External load is recorded context.

## Earlier Complete Campaign

The last complete campaign uses source identity `f26d0b99e160cc84ecfdb3f511d2a13930c7a9794eca16130573620427d878ca`.
CI passed 2,738 native checks without failures or skips.
The interpreter passed 1,742 checks with 165 native-only exclusions.
The qualification tools passed 204 checks, and the runtime and distribution tools passed 56 checks.
The Linux HVF corpus passed 1,415 checks without failures or skips.
Strict semantic coverage passed all 394 references in three lanes.

All four offline software host-to-target combinations passed.
Copied executables ran without Lo or compiler tools.
The Metal package, relocation, and nine rejection cases passed.
Both installed compiler hosts passed 500-step XOR training.

All four Darwin performance lanes passed.
Linux execution, process, and build lanes passed.
Linux computed-response latency and throughput passed.
Linux echo p95 has a median paired ratio of 1.077 with a 95% interval of 0.775 to 1.167.
Echo reciprocal throughput has a median paired ratio of 1.157 with a 95% interval of 0.988 to 1.217.
Both echo intervals cross the unchanged 1.1 limit.
The campaign remains inconclusive.

| Target | Case | Median build time | Maximum sampled process-tree RSS | Executable bytes |
| --- | --- | ---: | ---: | ---: |
| Darwin arm64 | `map_10000` | 2.193 seconds | 162,250,752 | 7,659,184 |
| Darwin arm64 | `records_10000` | 2.136 seconds | 158,859,264 | 7,659,232 |
| Linux arm64 | `map_10000` | 0.629 seconds | 168,452,096 | 13,110,896 |
| Linux arm64 | `records_10000` | 0.635 seconds | 166,039,552 | 13,111,552 |

The memory maxima include the first build, warmup builds, and ten measured builds.
The [storage campaign record](evidence/i011-qualification-20261022/storage-nonpass/manifest.json) preserves the complete run and the separate preliminary screen.
The raw audit calculated all eight lane reports and both target reports again.
All stored outcomes agree with those calculations.
A different agent verified 3,793 archived records, 491 binary identities, raw Linux exports, and the correctness and offline results.
At that stage, I011 remained open for HTTP qualification and the final evidence review.

## Oversized Request Correction

Full CI for the measurement-plan change stopped at `TestServeRequestBoundary`.
The test client received `broken pipe` instead of the required `413` response.
That run passed 2,737 native checks and failed one.
No performance lane started.
The [aborted validation record](evidence/i011-qualification-20261022/precision-aborted-validation/manifest.json) preserves the original sources and raw results.

The native server rejected a declared body above 1 MiB before it read the body.
After the response, it closed the write side and read at most 256 KiB of the remaining upload.
It then closed the connection, which could interrupt the client upload.
The new regression received the complete `413` response before it sent a 1,048,577-byte body.
The old runtime accepted 266,240 bytes, then returned `broken pipe` to the client.
The test uses a 4 KiB client send buffer to expose the early close.

The correction removes only the 256 KiB cutoff.
The server continues to discard unread bytes until EOF, an error, or the existing deadline.
The 8 KiB buffer, 500 ms deadline, socket timeouts, body limit, and error behavior stay unchanged.
The last socket read and host scheduling can extend execution beyond that deadline.
The correction does not promise complete uploads from arbitrarily slow clients.

Darwin arm64 and Linux arm64 under QEMU HVF each passed 13 focused tests and 33 subtests.
These checks include the original failed test, the new regression, a stalled client, and a subsequent healthy request.
A different agent verified the source change, package identities, and raw results from both targets.
The user stopped the subsequent full CI run and selected the focused HTTP checks for this correction.
The subsequent focused HTTP run passed all eight gates, as recorded above.
All acceptance thresholds stay unchanged.

## HTTP Measurement Precision

### Focused I011 Acceptance

On October 23, 2026, the user selected HTTP-only qualification on both targets and prohibited another full suite run.
This selection replaces the final full-suite rerun requirement for I011.
Closure will use the earlier correctness, offline, and non-HTTP results after review of the subsequent changes.
Those results retain their original source and package identities.
All eight fresh HTTP gates must pass with unchanged thresholds.

The new reports cannot supply a complete campaign with one source identity.
Historical samples and lanes will not be merged into those reports.
The earlier inconclusive campaign and user-stopped CI retain their recorded outcomes.
The evidence audit and review by a different agent remain necessary.

### Measurement Plan

Lower server CPU time does not establish lower client-observed latency or higher throughput.
The HTTP gates include client work and elapsed time outside server execution.
The exact cause of this variation remains unconfirmed.
The [measurement analysis](evidence/i011-qualification-20261022/http-variability/manifest.json) preserves the calculations and their inputs.

The retained Linux echo campaign contains ten measured pairs.
Each observation measures 200 requests before the other implementation starts its observation.
Interpreter batch times range from 4.528 to 8.226 milliseconds.
Native batch times range from 7.678 to 9.032 milliseconds.
The median elapsed ratio is 0.988 when native runs first and 1.192 when native runs second.
Each order group contains five pairs.
This association does not prove that order caused the difference.

The earlier full-campaign plan declared 100 HTTP pairs and ten pairs for each other lane on both targets.
The contract already requires at least ten measured pairs.
The new plan increases the HTTP sample count to reduce uncertainty.
It keeps three warmup pairs, 200 requests per HTTP observation, and the existing alternating order.
It keeps every sample and the same median ratios, 2,000 bootstrap resamples, 95% intervals, and numeric limits.
Temporal correlation remains a limitation of this estimator.

A complete campaign must run all eight lanes with one set of fixed inputs.
Historical reports cannot supply lanes for this campaign.
The campaign cannot extend its sample count or repeat automatically after an unsuccessful result.
A more precise result can pass or fail.
The focused I011 acceptance update above replaces the planned complete campaign rerun.

## Linux Echo Reproduction

The focused Linux checks used the same `f26d0b99` candidate and QEMU HVF.
The connection test changed only `TCP_NODELAY` on the existing native connection.
The test kept the original timeouts and verified each response.
The first run used the guest default `GOMAXPROCS=2`.
A separate run used `GOMAXPROCS=1`, as required by qualification.
Neither run showed a consistent benefit from this option.

Two further checks compared server CPU time after successful shutdown.
The short check kept the canonical client, request loop, order, and sample counts unchanged.
Its only source change added CPU totals after shutdown.

| Requests per server | Native CPU time | Interpreter CPU time |
| ---: | ---: | ---: |
| 10,000 | 354.843 ms | 490.023 ms |
| 2,600, canonical short workload | 115.619 ms | 160.630 ms |

These CPU totals include startup, warmup, idle intervals, and shutdown.
They do not measure request processing alone.
The short check had a median paired elapsed ratio of 0.822.
Individual ratios ranged from 0.514 to 1.405.
These results did not establish a consistent native slowdown or its cause.

Those focused checks made no production change and did not include a qualification campaign.
The original inconclusive campaign and all acceptance thresholds remain unchanged.
The [reproduction evidence](evidence/i011-qualification-20261022/http-echo-reproduction/manifest.json) preserves all raw results, setup failures, and the offline report correction.
A different agent verified the canonical short check and found no justification for a production fix.
I011 remained open after those diagnostic checks.

## Earlier HTTP Reader Qualification

The earlier campaign uses source identity `627be1a75c70ee397c0dd068e76200599db53532193135e830a0b6f1d35700e0`.
CI passed 2,733 native checks without failures or skips.
The interpreter passed 1,742 checks with 164 native-only exclusions.
The qualification tools passed 204 checks, and the runtime and distribution tools passed 56 checks.
The Linux HVF corpus passed 1,415 checks without failures or skips.
Strict semantic coverage passed all 394 references in three lanes.

All four offline software host-to-target combinations passed.
Copied executables ran without Lo or compiler tools.
The Metal package, relocation, and nine rejection cases passed.
Both installed compiler hosts passed 500-step XOR training.

All four Darwin performance lanes passed.
Linux execution, process, and build lanes passed.
Both Linux HTTP p95 gates passed.
Echo reciprocal throughput has a paired ratio of 1.078 with a 95% interval of 1.043 to 1.111.
Computed-response reciprocal throughput has a paired ratio of 1.061 with a 95% interval of 0.983 to 1.127.
Both intervals contain values below and above the unchanged 1.1 limit, so HTTP qualification is inconclusive.

| Target | Case | Median build time | Maximum sampled process-tree RSS | Executable bytes |
| --- | --- | ---: | ---: | ---: |
| Darwin arm64 | `map_10000` | 2.100 seconds | 162,922,496 | 7,659,184 |
| Darwin arm64 | `records_10000` | 2.104 seconds | 161,316,864 | 7,659,232 |
| Linux arm64 | `map_10000` | 0.582 seconds | 167,985,152 | 13,110,640 |
| Linux arm64 | `records_10000` | 0.582 seconds | 171,233,280 | 13,111,296 |

The [earlier reader evidence](evidence/i011-qualification-20261022/specialized-nonpass/manifest.json) keeps all samples, source identities, and package identities.
The raw audit calculated all eight lane reports and both target reports again.
All stored outcomes agree with those calculations.
A different agent verified all 3,545 archived records, 427 binary identities, source identities, package identities, and performance calculations.
The HTTP reader type change does not complete performance acceptance.
More investigation must keep the same acceptance limits.

## Earlier Parser And HTTP Acceptance

The earlier candidate has source identity `f9b5a69b9b99118142276a47489fbe95c3db8c7c31fa2ebfa25f6e132f8dbca7`.
The run fixed all 2,743 source file hashes before validation.
CI completed 2,733 native integration checks with no failures or skips.
The interpreter completed 1,742 checks with 164 native-only exclusions.
The qualification tools completed 204 checks.

The Linux HVF corpus passed 1,415 checks with no failures or skips in 280.323 seconds.
Strict semantic coverage passed all 394 references in three lanes without qualification gaps.
The runtime and distribution tools passed all 56 component checks.
The combined HTTP changes also passed nine public tests and 25 subtests on each target.

All four offline software host-to-target combinations passed.
Copied executables ran without Lo, source files, or compiler tools.
The Metal package, relocation, and nine rejection cases passed.
Both installed compiler hosts passed 500-step XOR training.
These results use the earlier packages after the parser and HTTP changes.

Full performance acceptance is pending.
The evidence audit and review by a different agent remain necessary after performance acceptance.

## Earlier Parser And HTTP Performance

All four Darwin lanes passed.
Linux execution, process, and build lanes passed.
Linux HTTP has one inconclusive gate: echo reciprocal throughput.
Its paired ratio is 1.079 with a 95% bootstrap interval of 1.067 to 1.145.
The interval crosses the unchanged 1.1 limit.
Both Linux p95 gates and computed-response throughput passed.

Linux computed-response throughput has a paired ratio of 1.048 with an interval of 1.032 to 1.082.
Every Linux echo pair has a native elapsed time greater than its interpreter elapsed time.
All samples stay in the report.
More investigation of native HTTP request costs is necessary.

The [earlier nonpass evidence](evidence/i011-qualification-20261022/qualified-nonpass/manifest.json) keeps the raw campaign, correctness checks, offline checks, and frozen source identities.
The raw audit calculated all eight lane reports and both target reports again.
All stored outcomes agree with those calculations.
A different agent verified these calculations, archived records, raw guest exports, correctness results, and offline results.
The review found no blocking evidence issue.
That inconclusive performance gate prevented closure at that stage.

The ten measured large-list builds have these results:

| Target | Case | Median build time | Maximum sampled process-tree RSS | Executable bytes |
| --- | --- | ---: | ---: | ---: |
| Darwin arm64 | `map_10000` | 2.095 seconds | 162,332,672 | 7,662,016 |
| Darwin arm64 | `records_10000` | 2.113 seconds | 164,462,592 | 7,662,080 |
| Linux arm64 | `map_10000` | 0.594 seconds | 169,504,768 | 13,120,040 |
| Linux arm64 | `records_10000` | 0.596 seconds | 157,241,344 | 13,120,696 |

The current campaign keeps the same sample counts, paired order, confidence calculations, and resource limits.
No result from an earlier campaign replaces a current gate.

## HTTP Request Stage Profile

The [request stage profile](evidence/i011-qualification-20261022/http-request-profile/manifest.json) measured eight stages with different request groups.
Each target completed 2,600 echo requests and 2,600 computed-response requests.
Each group kept 75 warmup measurements and 250 measured observations.
All 10,400 response checks passed, and all runtime ownership counters were zero after shutdown.
A different agent verified the raw results, calculations, source identities, and archived evidence.

On Linux, `read_request` had the largest median interval for worker CPU time.
Its echo and computed-response medians were 11.063 and 11.313 microseconds.
Each measured request had 380 cancellation polls in this stage.
The wall intervals also contain socket waits.
These results do not identify which part of the stage causes the cost.

The report includes each timing control and each request measurement.
Different stages use different request groups, so their measurements do not form one request total.
Small Darwin wall intervals do not show the cost at the observed clock resolution.
The complete campaign is necessary for full performance acceptance.

## HTTP Reader Type Selection

The compiler generates an HTTP reader for each of the two connection types.
The change contains three declarations and leaves all reader method bodies unchanged.
Generated server code on both targets no longer selects a connection read function from a table.
Read sizes, cancellation polls, clock calls, deadlines, framing, and ownership stay unchanged.
Full performance acceptance is pending.

New Linux checks showed that the cancellation test accepted one result when two results were possible.
The server can receive a request before the client records write completion.
The corrected assertion accepts only `write-request,false,unknown` or `read-response,true,unknown` for stage, write completion, and remote outcome.
The test that signals inside `recv` accepts only `read-response,true,unknown`.
The exit status, output requirements, and shutdown timeout stay unchanged.

The [type selection evidence](evidence/i011-qualification-20261022/http-specialization/manifest.json) contains the source patch, generated code, focused checks, and reviews.
Both targets passed ten public tests and 27 subtests after the assertion correction.

## HTTP Parser Buffer Diagnostic

A private candidate removed the server reader's second byte buffer.
All 40 trace cases passed on both targets.
The checks compared byte consumption, cancellation polls, deadlines, errors, writes, and bytes for the next request.
Four public HTTP body and pipeline cases also passed on both targets before runtime changes.
The [trace evidence](evidence/i011-qualification-20261022/http-borrowed-parser/manifest.json) contains these results and their source identities.

The fixed timing experiment compared two request formats on each target.
Each comparison used three warmup pairs and ten measured pairs, with 2,000 requests per batch.
The screen required both Linux CPU medians to decrease by at least 1,000 nanoseconds per request.
The Linux medians decreased by only 232 and 234 nanoseconds.
The candidate did not pass this screen and is not part of the maintained runtime.

A different agent verified all 208,000 requests, raw observations, clocks, ownership counters, and confidence calculations.
The [timing evidence](evidence/i011-qualification-20261022/http-borrowed-timing/manifest.json) retains the complete result and preparation corrections.
Memory transport measurements do not establish TCP performance.
This diagnostic does not change I011 acceptance limits or complete HTTP qualification.

## HTTP Cancellation Code Diagnostic

An `#[inline]` attribute did not change the selected generated functions on either target.
The `#[inline(always)]` attribute on `Context::check_cancel` moved the calls into another helper.
Both candidates kept the temporary result handling on the normal parser path.
Neither candidate proceeded to timing.

A third candidate adds `#[inline(always)]` to `Context::check_cancel` and the HTTP `cancelled` helper.
The two attributes are its only source changes.
Both function bodies stay unchanged.
The normal parser path now uses direct checks without cancellation helper calls or their temporary result values.
Error branches still construct and propagate the same faults.

The reader stack frame stays at 144 bytes on both targets.
Executable code increases by 7,552 bytes on Darwin and 7,360 bytes on Linux.
Selected other runtime functions need up to 16 more stack bytes on Darwin and 32 on Linux.
Review by a different agent accepted these results for a private timing experiment.
These code measurements do not prove a speedup or qualify I011.

The first instruction reports incorrectly counted relocation records as instructions.
A correction uses the retained raw output and verifies all 46 Linux function extents.
The original reports remain in the evidence record.
Code sizes from sections, stack measurements, and the earlier negative conclusions stay unchanged.
The [code evidence](evidence/i011-qualification-20261022/http-cancel-inline/manifest.json) retains all three candidates, raw outputs, and the count correction.

The fixed timing experiment completed all 104 processes and 208,000 requests.
Linux CPU medians decreased by 217 and 321 nanoseconds per request for the two request formats.
Both intervals were above zero, but neither median met the fixed 1,000-nanosecond screen.
The candidate is not part of the maintained runtime.
A different agent verified all process results, raw controls, clocks, checksums, cancellation polls, artifact identities, and confidence calculations.
The [timing record](evidence/i011-qualification-20261022/http-cancel-timing/manifest.json) keeps this valid result and its limits.

## HTTP Parser Stage Diagnostic

A fixed diagnostic measured five parts of the unchanged request parser.
Four cells completed 10,400 requests across both targets and both captured request formats.
Each stage has 120 warmup observations and 400 measured observations per cell.
A different agent verified the raw results, source identities, cancellation polls, cleanup, and calculations.

The table gives median CPU time in nanoseconds.

| Target and request format | Header read | Request validation | Header values | Body read | Final request |
| --- | ---: | ---: | ---: | ---: | ---: |
| Darwin, Python client | 3,500 | 250 | 584 | 875 | 333 |
| Darwin, Go client | 5,750 | 250 | 833 | 875 | 333 |
| Linux, Python client | 4,000 | 375 | 666 | 1,041 | 500 |
| Linux, Go client | 6,458 | 334 | 958 | 1,042 | 500 |

Diagnostic clocks and stage selection add cost.
Each stage uses separate request samples, so the stage durations cannot give a total request duration.
The controls remain separate from the measured durations.
The memory transport excludes network delays and does not qualify endpoint performance.
Median CPU time for header value construction was less than one microsecond in each cell.
The [parser stage record](evidence/i011-qualification-20261022/http-parser-stages/manifest.json) retains the fixed protocol, raw observations, audit, and source identities.

## HTTP Storage Checks

Source review found repeated tensor storage checks during HTTP handler execution.
The runtime marked every adapter value as a possible tensor owner.
Each check traversed the exposed request fields, including headers with only text and bytes.

The candidate sets `contains_storage` from the exposed fields of `http::Value::Headers` and the child of `http::Value::Fields`.
Other adapter variants keep the existing value.
The change keeps public value contents, weights, charges, cancellation polls, and error order unchanged.

Two internal tests completed under Linux with HVF.
The baseline failed the new assertion for values without storage and passed the nested tensor test.
The candidate passed both tests.
The tests verify storage identifiers, shared storage, foreign invocation rejection, released storage rejection, and cleanup.
Both targets passed 33 public tests and 69 subtests without failures or skips.
The [screen record](evidence/i011-qualification-20261022/storage-screen/manifest.json) contains these public test results.
The [storage check record](evidence/i011-qualification-20261022/http-storage-flags/manifest.json) retains the internal tests and exact source changes.

The fixed HTTP screen completed both targets.
Darwin computed-response p95 remains inconclusive with an interval of 0.640 to 1.103.
Linux echo p95 and reciprocal throughput remain inconclusive with intervals of 0.907 to 1.146 and 0.989 to 1.280.
The other five HTTP gates passed.
All limits stay at 1.1.
The [screen record](evidence/i011-qualification-20261022/storage-screen/manifest.json) preserves all observations, public checks, and review results.

The agents initially added a preliminary requirement that all screen gates pass before the complete campaign.
This requirement was separate from the user's acceptance criteria and the maintained qualification contract.
After these results, an explicit amendment removes that additional restriction to complete the campaign requested by the user.
Review by a different agent accepted one complete campaign for this substantive source change.
The amendment keeps all official thresholds, sample counts, paired order, and confidence calculations unchanged.
Screen samples stay separate from the complete campaign, and their inconclusive results remain in the evidence record.
The complete campaign used source identity `f26d0b99e160cc84ecfdb3f511d2a13930c7a9794eca16130573620427d878ca`.
It passed correctness and offline acceptance.
Seven performance lanes passed, but Linux echo latency and throughput remain inconclusive.
The preliminary screen and complete campaign keep their separate observations and outcomes.

## Earlier ABI2 Correctness And Offline Acceptance

The earlier compact ABI2 candidate has source identity `55b893d680144bb0e3244dd26f9f32f3f9fb7c7b7b54c8e4e104e90ba929ece4`.
The run fixed all 2,741 source file hashes before validation.
CI completed 2,705 native integration checks with no failures or skips.
The interpreter completed 1,742 checks with 163 native-only exclusions.
The qualification tools completed 204 checks.

The Linux HVF corpus completed 1,415 checks with no failures or skips.
Strict semantic coverage passed all 394 references in three lanes without qualification gaps.
The runtime and distribution tools completed all 56 component checks.
All four offline software host-to-target combinations passed.

Copied executables ran without Lo, source files, or compiler tools.
The Metal package, relocation, and nine rejection cases passed.
Both installed compiler hosts passed 500-step XOR training.
These results use the new ABI2 distributions.
Earlier candidate results do not replace them.

## Earlier ABI2 Performance Results

All four Darwin lanes passed.
The Linux execution and build lanes passed.
The Linux process and HTTP lanes have the status `inconclusive`.
All measurements are valid and stay in the report.
A different agent calculated all eight lane reports and both merged reports again from the raw measurements.
The review found no measurement or provenance defect.

The [nonpass evidence](evidence/i011-qualification-20261022/compact-nonpass/manifest.json) keeps the raw reports, source files, logs, and artifact identities.

The ten measured large-list builds have these results:

| Target | Case | Median build time | Maximum sampled process-tree RSS | Executable bytes |
| --- | --- | ---: | ---: | ---: |
| Darwin arm64 | `map_10000` | 2.125 seconds | 156,237,824 | 7,661,040 |
| Darwin arm64 | `records_10000` | 2.135 seconds | 161,431,552 | 7,661,088 |
| Linux arm64 | `map_10000` | 0.612 seconds | 160,452,608 | 13,112,408 |
| Linux arm64 | `records_10000` | 0.606 seconds | 163,749,888 | 13,113,064 |

These are installed compiler measurements through linking on each target.
The Linux measurements use HVF.
The artifact sizes include the static runtime and target link inputs.
The historical language comparison uses different execution times and runtime costs.
It does not prove equal compiler costs across languages.

Five Linux process gates have bootstrap intervals across the 1.1 limit.
They are the 1,000-item map, filter, fold, records, and bigint cases.
The HTTP p95 gates passed, but both reciprocal throughput intervals cross the limit.
The point estimates are 1.116 for echo and 1.130 for computed responses.

Program metadata admission is outside the steady HTTP request interval.
It cannot directly explain both results.
Different diagnostics measured program admission, execution, cleanup, and HTTP request costs.
The sections below describe these changes and their evidence.
No sample removal, automatic retry, threshold change, or timeout increase changes this campaign outcome.

## Program Admission Diagnostic

The [admission profile](evidence/i011-qualification-20261022/admission-profile/manifest.json) measures three stages through the public C ABI.
It uses the fixed empty, 1,000-item map, and 10,000-item map sources.
Each case has three warmup samples and ten measured samples on each target.
All six CLI result checks and all ownership checks passed.

| Target | Case | Median admission | Median program cleanup | Median session lifecycle |
| --- | --- | ---: | ---: | ---: |
| Darwin arm64 | `map_1000` | 1.757 ms | 0.079 ms | 0.319 ms |
| Linux arm64 | `map_1000` | 1.814 ms | 0.122 ms | 0.345 ms |
| Darwin arm64 | `map_10000` | 17.604 ms | 0.801 ms | 3.288 ms |
| Linux arm64 | `map_10000` | 19.576 ms | 1.085 ms | 3.532 ms |

Admission includes JSON parsing, site construction, literal preparation, and temporary metadata cleanup.
The session stage uses one admitted program outside its timing interval.
It includes context construction, execution, result release, and context cleanup.
These measurements show that admission is the largest measured stage.
They do not show that location maps cause this cost.

The profile does not contain load context from the measurement interval.
Its batches keep multiple programs alive and change allocator and cache conditions.
The stage medians cannot be added to or subtracted from the earlier process endpoint.
Review by a different agent verified the input bindings, builds, raw stages, medians, outputs, and Linux exports.

The selected parser refactor replaces each location map with six fixed fields.
Payloads keep their JSON value representation.
All 60 public admission cases passed on both targets before this refactor.
The new cases check duplicate keys, nonobject rows, unknown-key order, and JSON syntax precedence.
The refactor passed all 60 admission cases and 31 public tests on each target.

The [parser evidence](evidence/i011-qualification-20261022/metadata-row/manifest.json) keeps the source files, raw tests, and a second stage profile.
This profile uses the same C harness, sources, batches, and sample counts.
All six CLI checks and all ownership checks passed.

| Target | Case | New median admission | New median program cleanup | New median session lifecycle |
| --- | --- | ---: | ---: | ---: |
| Darwin arm64 | `map_1000` | 1.238 ms | 0.085 ms | 0.338 ms |
| Linux arm64 | `map_1000` | 1.241 ms | 0.135 ms | 0.346 ms |
| Darwin arm64 | `map_10000` | 13.578 ms | 0.816 ms | 3.521 ms |
| Linux arm64 | `map_10000` | 13.189 ms | 1.131 ms | 3.537 ms |

These different experiments show lower admission times and some higher cleanup and session times.
They do not show a performance qualification result.
The same measurement limits apply to both profiles.
The refactor needs new performance qualification.

## HTTP Error Record Diagnostic

The [HTTP phase profile](evidence/i011-qualification-20261022/http-hotpath-profile/manifest.json) measures request and response error record construction.
Each target runs the same echo and computed benchmarks.
Each benchmark has three warmup batches and ten measured batches, with 200 requests per batch.
All 10,400 requests returned the specified results.
All workers stopped, and the live context and value counts were zero.

| Target | Benchmark | Mean request record time | Mean response record time |
| --- | --- | ---: | ---: |
| Darwin arm64 | Echo | 135.286 ns | 134.042 ns |
| Darwin arm64 | Computed | 141.071 ns | 136.275 ns |
| Linux arm64 | Echo | 341.792 ns | 148.462 ns |
| Linux arm64 | Computed | 342.897 ns | 129.563 ns |

These intervals include clock calls.
Record destruction is outside these intervals.
The record intervals total approximately 0.47–0.49 microseconds per Linux request.
They do not explain the full earlier HTTP difference of approximately four microseconds per request.
This diagnostic does not measure endpoint savings or qualify HTTP performance.

Review by a different agent verified the measurements and found two missing evidence groups.
The [supplement](evidence/i011-qualification-20261022/http-hotpath-profile/supplement-raw-exports.json) contains the raw Linux exports and a reconstruction of the baseline Make file.
The reconstructed bytes have the hash recorded before the experiment.
The reviewer verified the supplement and closed both findings.
The evidence keeps the earlier source preparation failures, which produced no timing samples.

## HTTP Reader And Error Record Changes

The [Reader comparison](evidence/i011-qualification-20261022/http-reader-comparison/manifest.json) compares the previous buffer with reads from the current scratch buffer.
The candidate removes a second byte vector and its copy.
The change keeps the same read sizes, cancellation polls, deadlines, framing, and cleanup rules.

Each target completed the same eight public HTTP tests for both candidates before measurement.
Each benchmark has three warmup pairs and ten measured pairs with 200 requests per observation.
The order alternates between the two candidates.
All 20,800 requests returned the specified result.
All eight native processes returned status zero with no live contexts or values.

| Target | Benchmark | Median paired candidate minus baseline |
| --- | --- | ---: |
| Darwin arm64 | Echo | −531 ns per request |
| Darwin arm64 | Computed | −368 ns per request |
| Linux arm64 | Echo | −391 ns per request |
| Linux arm64 | Computed | −679 ns per request |

Every range of paired differences includes positive and negative values.
The Linux echo record keeps one difference of approximately −11.7 microseconds per request.
These measurements do not prove a speedup or qualify I011.
Review by a different agent verified all raw observations, source identities, and archived evidence.

The maintained runtime now reads directly from the scratch buffer.
It also constructs request and response error records only on their current error branches.
Ten public error-input cases passed on both targets before this second change.
They verify correct error kinds, messages, input values, callback counts, and ownership cleanup.
The [combined change record](evidence/i011-qualification-20261022/http-combined/manifest.json) keeps the patches and formatting results.

Code review by a different agent found no blocking issue in the combined changes.
The new distributions passed nine public HTTP tests and 25 subtests on each target.
These checks include all ten error-input cases.
Review by a different agent verified their raw results and source, runtime, and package identities.
The same source and packages completed the full campaign with one inconclusive Linux echo throughput gate.

## Earlier Correctness And Offline Acceptance

CI completed with 2,635 native integration checks and no failures or skips.
The interpreter completed 1,741 checks with 157 native-only exclusions.
The qualification tools completed 204 checks.
The Linux corpus completed 1,414 checks with no failures or skips.
The strict semantic report qualified all 394 references in three lanes with no qualification gaps.

The new packages completed all four offline software host-to-target combinations.
Copied executables ran without Lo or compiler tools.
The Metal package, relocation, and nine rejection cases completed their acceptance checks.
Both installed compiler hosts completed 500-step XOR training.
All 56 runtime and distribution component checks completed.

One component invocation used the default software runtime receipt for a Metal rejection fixture.
The fixture stopped with `KeyError: 'metal'` before it invoked the package CLI.
A different continuation selected the new runtime through `LO_TEST_RUNTIME_DISTRIBUTION`.
The corrected component invocation completed all checks.
The initial failed record stays unchanged.

The continuation keeps the completed CI and Linux results with their source, package, and log identities.
It does not describe these results as new executions.
No source or package changed between these invocations.

## Historical Campaign

The previous campaign ended with the status `invalid` at `2026-10-22T03:50:56Z`.
Its Darwin execution, process, and build lanes completed within the limits.
Its Darwin HTTP results were `inconclusive`.
Its Linux execution lane completed within the limits.
Linux process setup could not read `tools/loqualification/batch_init.c`.
The subsequent lane report was absent.

That campaign does not show full qualification.
Its initial reports and source identities stay in the evidence record.
The incomplete ABI1 campaign selected all eight lanes with the recorded packages.
It does not combine measurements from different campaigns.

## HTTP Measurement And Runtime Corrections

Each HTTP workload uses one client and 200 requests per sample on a loopback connection kept open between requests.
The Darwin producer now calculates elapsed time and throughput from one clock interval.
The report validator checks p95 latency, total elapsed time, and throughput against the raw measurements.
The focused measurement checks completed with 63 test results of `pass`.

A different experiment fixed its design before execution.
It kept 100 measured pairs after three warmup pairs on each target.
It kept the initial request count, paired order, confidence calculation, and 1.1 ratio limit.
It kept every sample and used no retries.
The experiment found Linux HTTP ratios above the limit.
Two Darwin throughput intervals crossed the limit.

`Reader::byte` filled an 8 KiB temporary buffer with zeros for each connection read.
The server adapter supplies one byte per read to keep pipelined requests.
The buffer is now a field of `Reader`.
The runtime uses the same buffer for later reads.
The refactor keeps byte framing, cancellation checks, deadlines, and the server adapter read size.
The focused HTTP checks completed with 20 test results of `pass` on each target.

The corrected runtime completed a second experiment with the same fixed design.
All eight bootstrap intervals were below the 1.1 limit.
These experiments are diagnostic evidence.
They do not replace the full ten-pair campaign.

| Target | Workload | Metric | Paired ratio | 95% bootstrap interval |
| --- | --- | --- | ---: | ---: |
| Darwin | Echo | p95 latency | 0.855 | 0.842–0.908 |
| Darwin | Echo | Reciprocal throughput | 0.933 | 0.914–0.966 |
| Darwin | Computed | p95 latency | 0.890 | 0.860–0.920 |
| Darwin | Computed | Reciprocal throughput | 1.000 | 0.974–1.019 |
| Linux | Echo | p95 latency | 0.877 | 0.841–0.928 |
| Linux | Echo | Reciprocal throughput | 0.928 | 0.905–0.987 |
| Linux | Computed | p95 latency | 0.843 | 0.812–0.882 |
| Linux | Computed | Reciprocal throughput | 0.919 | 0.852–0.969 |

Each paired ratio compares the named native metric with its interpreter metric.
The p95 ratio divides native p95 by interpreter p95.
Reciprocal throughput compares elapsed time for the same fixed request count.
The reported estimator uses those paired values.
It is not the ratio of independently calculated medians.
The [HTTP record](evidence/i011-qualification-20261022/http-resolution.json) keeps both experiments, their identities, and their full outcomes.

The experiments ran at different times.
They do not supply paired measurements between the two runtime versions.
These workloads do not show server capacity or production tail latency.

## Large Literal Lists

The fixed corpus constructs varied integer lists directly in source code.
It tests compilation of large literals and callback execution.
It does not represent an application that reads the same list from a file at runtime.
The completed historical Darwin build lane recorded these costs.

| Case | Median build time | Maximum sampled RSS | Executable size |
| --- | ---: | ---: | ---: |
| `startup` | 1.897 s | 49.5 MiB | 2.82 MiB |
| `map_1000` | 2.778 s | 230 MiB | 4.66 MiB |
| `map_10000` | 48.691 s | 1.80 GiB | 21.02 MiB |
| `filter_10000` | 48.836 s | 1.79 GiB | 21.10 MiB |
| `fold_10000` | 51.435 s | 1.80 GiB | 21.05 MiB |

The median includes ten measured builds.
The RSS maximum includes the first build, three warmup builds, and ten measured builds.
RSS means resident set size.
Short memory peaks can occur between samples.

The incomplete ABI1 campaign completed `map_10000` at a median of 45.223 seconds.
Its maximum sampled RSS was 1,926,266,880 bytes.
The executable size was 22,110,032 bytes.
The Darwin build lane then stopped when `records_10000` sample 7 reached the 180-second timeout.

The Linux performance lanes did not start.
The campaign is incomplete and does not show full qualification.
The [failed campaign record](evidence/i011-qualification-20261022/failed-continuation/manifest.json) keeps the reports and the timeout.

The recorded candidate has high build time, memory, and executable costs for large literal lists.
The HTTP correction does not decrease these compiler costs.
The current limits are 180 seconds, 2 GiB sampled memory, and 64 MiB per executable.
The user selected “reasonable: comparable to other compiled languages” compiler costs.
The user selected a comparison with Go, Rust, and C++.
That comparison must report absolute costs and increases from the baseline for an empty program.

Different compiler profiles measured two large-literal cases.
Both generated about 59 MB of LLVM IR and 60,000 basic blocks.
LLVM register allocation consumed most of the compile time.
The linker consumed less than 0.2 seconds in each initial profile.

| Case | Frontend | Lo lowering | LLVM emission | LLVM compilation | Sampled LLVM RSS |
| --- | ---: | ---: | ---: | ---: | ---: |
| `records_10000` | 0.154 s | 9.520 s | 1.473 s | 79.310 s | 1,503,854,592 bytes |
| `map_10000` | 0.163 s | 8.792 s | 1.565 s | 67.404 s | 1,483,030,528 bytes |

The initial `records_10000` report rejected the CLI result because the diagnostic validator specified the wrong JSON shape.
A different diagnostic record checks the saved output against the specified value.
It does not rebuild the program or change the initial report.

A second diagnostic added only `-mllvm -regalloc=fast` to each LLVM compilation.
LLVM compilation took 29.541 seconds for `records_10000` and 23.202 seconds for `map_10000`.
Both executables returned the specified result.
Sampled LLVM RSS stayed near 1.49 GB.
The executable size increased to 24,853,504 bytes for `records_10000`.
It increased to 24,721,360 bytes for `map_10000`.

The production compiler does not use this allocator setting.

Each profile is one instrumented observation.
The profiles are different executions and do not show a statistical performance ratio.
The diagnostic emitter exits before LLVM compilation, unlike the installed compiler process.
Thus, these RSS values do not include the installed compiler parent heap.
The [profile record](evidence/i011-qualification-20261022/literal-profile/manifest.json) keeps the inputs, reports, traces, and result check.

## Comparison With Go, Rust, And C++

A different diagnostic compiled the same fixed integer data and calculated the same result in four languages.
The cases contain zero, 1,000, and 10,000 items.
The empty case returns 7.
Each other case multiplies each item by three and calculates the sum.
The target is Darwin arm64 only.
The reference compilers are Go 1.27.1, Rust 1.85.1, and Clang 21.1.8.

Each case has one warmup and three measured builds.
The language order rotates between rounds.
The tools use warm caches, but each observation compiles the program source again.
The Go logs confirm two user package compilations and no standard library compilation in every measured build.
All successful programs returned the specified result.

| Language | Empty build | 1,000-item build | 10,000-item build | 10,000-item range | Increase from empty median |
| --- | ---: | ---: | ---: | ---: | ---: |
| Lo | 1.909 s | 2.734 s | 51.453 s | 50.224–56.189 s | 49.545 s |
| Go | 0.183 s | 0.164 s | 0.272 s | 0.170–0.336 s | 0.090 s |
| Rust | 0.277 s | 0.470 s | 0.466 s | 0.261–0.682 s | 0.189 s |
| C++ | 0.343 s | 0.589 s | 0.645 s | 0.393–0.718 s | 0.302 s |

Build times are medians of three observations.
The measurements include the harness checks and cleanup between compile and link stages.
The samples do not show equal performance or an accurate speed ratio between languages.

| Language | Empty executable | 10,000-item executable | Size increase | 10,000-item sampled RSS median |
| --- | ---: | ---: | ---: | ---: |
| Lo | 2,954,448 bytes | 21,977,920 bytes | 19,023,472 bytes | 1,927,954,432 bytes |
| Go | 2,429,874 bytes | 2,512,498 bytes | 82,624 bytes | 58,441,728 bytes |
| Rust | 33,840 bytes | 100,320 bytes | 66,480 bytes | Unavailable |
| C++ | 33,920 bytes | 116,880 bytes | 82,960 bytes | Unavailable |

RSS sampling did not capture a positive value for every Rust or C++ build.
A stored zero means that the sample is unavailable.
It does not mean zero memory use.
The record keeps all raw observations, including these zeros.

The first fixed run completed all 24 Lo and Go observations.
All 24 Rust and C++ links failed because the packaged TAPI omits `printf`.
That run keeps the status `incomplete` and every failed observation.
A different fixed reference experiment replaced `printf` with decimal output through the declared `write` symbol.
Its empty-case checks and all 24 observations completed.
The two experiments keep different identities and do not supply paired measurements between languages.

The Rust reference uses `no_std`.
The C++ reference uses no headers or C++ standard library.
Both use two object files, the packaged linker, and the same operating-system library description.
Their artifacts contain the full integer data, and neither build uses LTO.
These are small reference programs, not typical projects with dependencies.
The Go toolchain executable is identified, but the full host Go toolchain is not pinned by the distribution.

Lo also keeps arbitrary-precision integers, logical charges, cancellation checks, callback rules, and managed values.
The reference arithmetic uses the same result within signed 64-bit limits.
The references combine multiplication and summation in one loop.
Lo constructs a mapped list before summation.
These differences limit the comparison.

These recorded Lo costs do not meet the operator's compiler cost requirement for this workload.
B007 and B008 address these compiler costs.
Their subsequent build qualification is recorded in the earlier complete campaign above.
The [comparison record](evidence/i011-qualification-20261022/language-comparison/manifest.json) and [summary](evidence/i011-qualification-20261022/language-comparison/summary.json) keep the measurements and their limits.

## Literal Behavior Before The Compiler Refactor

Two new public tests completed on Darwin and Linux before a compiler code change.
Each target completed ten subtests and 15 real runtime sessions.
The checks cover ordered calls and sites, step boundaries, cancellation prefixes, literal failures, ownership release order, and escaped results.
The runtime source sites keep the initial item offsets, lines, and columns.
The current terminal `runtime.limit` diagnostics do not contain a source span in either implementation.
The tests keep that current diagnostic shape.

A different public test completed 70 status cases on each target before the compiler refactor.
The cases cover result slots set to zero, partial writes, invalid statuses, failure prefixes, shared error sites, and cleanup.
The [status record](evidence/i011-qualification-20261022/literal-status-characterization/manifest.json) keeps the commands, source snapshots, logs, and limits.
These cases inject invalid ABI results at the runtime boundary.
They do not describe legal Lo program results.

The [characterization record](evidence/i011-qualification-20261022/literal-characterization/manifest.json) keeps the source snapshots, logs, commands, and runtime identities.
The derivative checks also completed on both targets.
Synthetic derivative graphs supply references to `List.new`.
These calls keep their initial lowering.
Only direct integer literal arguments qualify for compact loops.

The earlier baseline called these reference arguments direct literals.
This description was incorrect.
The [correction record](evidence/i011-qualification-20261022/gradient-reachability-correction.json) contains the source proof and keeps the earlier records.
B008 addresses the metadata representation.
The user approved the compact-helper DWARF limitation in the [decision record](evidence/i011-qualification-20261022/dwarf-decision.json).
Runtime source records and logical behavior stay unchanged.

## Compact Literal Candidate

B007 now uses a private loop for direct integer literal lists.
The structural test keeps 259 instructions, 39 basic blocks, and 21 `alloca` instructions for 1, 1,000, and 10,000 items.
Both target triples passed this test.
The initial field and table hashes stayed unchanged across all 38 actions.
The focused runtime checks passed on Darwin and through Linux target execution with TCG.
These checks do not replace qualification of the installed Linux compiler.

A different diagnostic completed six builds and executions with the unchanged ABI1 runtime.
The following table contains single observations.
All builds ran on Darwin arm64.
Linux executables ran in the maintained QEMU HVF guest.
The diagnostic emitter exits before LLVM compilation.
Thus, these measurements exclude the installed compiler parent heap.

| Target | Case | Time through link | LLVM child peak RSS | Executable size |
| --- | --- | ---: | ---: | ---: |
| Darwin | `records_10000` | 0.765 s | 101,138,432 bytes | 17,636,800 bytes |
| Darwin | `map_10000` | 0.836 s | 102,121,472 bytes | 17,521,168 bytes |
| Darwin | Two 10,000-item groups | 1.625 s | 156,385,280 bytes | 30,928,592 bytes |
| Linux | `records_10000` | 2.091 s | 103,006,208 bytes | 23,022,384 bytes |
| Linux | `map_10000` | 2.298 s | 100,876,288 bytes | 22,898,824 bytes |
| Linux | Two 10,000-item groups | 1.890 s | 154,419,200 bytes | 36,210,312 bytes |

The operating system reports the child peak RSS through `wait4`.
This metric differs from sampled process-tree RSS.
The peak for each stage does not give a peak for the full process tree.
These observations do not show a statistical performance ratio.
The executable sizes show the metadata cost that B008 addresses.

Each private helper contains four machine call instructions and uses a 96-byte stack frame.
The Darwin helper contains 75 machine instructions.
The Linux helper contains 76 machine instructions.
The caller frame uses 320,560 bytes for one group and 640,800 bytes for two groups on both targets.
These fixed cases do not prove stack capacity for arbitrary programs.

The first probe stopped because its IR scanner repeatedly searched the large metadata line.
That scanner added about 52 seconds to each completed case.
The interrupted report stays unchanged.
A different corrected probe uses a linear scan after the link ends.
It does not combine its results with the interrupted probe.
The [candidate record](evidence/i011-qualification-20261022/literal-loop-candidate/manifest.json) and [probe record](evidence/i011-qualification-20261022/literal-loop-probe/manifest.json) contain the source snapshots, checks, measurements, and limits.

## Installed ABI2 Candidate

B008 uses shared source spans and removes fields that have no meaning for an action.
The compiler and runtime use only `lo-native-2`.
Both targets passed the selected public checks, including all 43 metadata admission cases.
The initial field and table hashes stayed unchanged across all 38 actions.
The parser and encoder reviews found no blocking issue.

A different diagnostic used the new installed Darwin compiler with the initial comparison sources.
Each case has one warmup and three measured builds.
All 12 builds and executable results passed.
The following values are medians of the three measured builds.

| Case | Installed build time | Sampled process-tree RSS | Executable size |
| --- | ---: | ---: | ---: |
| Empty | 1.856 s | 49,594,368 bytes | 2,970,960 bytes |
| 1,000 items | 1.860 s | 52,002,816 bytes | 3,433,952 bytes |
| 10,000 items | 2.224 s | 143,835,136 bytes | 7,661,024 bytes |

The 10,000-item case adds 0.368 seconds and 4,690,064 executable bytes above the empty-program medians.
RSS includes the installed compiler parent and its sampled descendants.
The 50-millisecond sampling interval can miss short memory peaks.
These observations and the historical language references ran at different times.
They do not show an accurate speed ratio or statistical equivalence.

The installed compiler also emitted modules for both target triples from the same sources.
All six emissions passed.
The following table gives metadata and module byte counts.

| Target | Case | Metadata bytes | Module bytes |
| --- | --- | ---: | ---: |
| Darwin | Empty | 1,873 | 14,391 |
| Darwin | 1,000 items | 439,136 | 711,188 |
| Darwin | 10,000 items | 4,406,704 | 6,678,921 |
| Linux | Empty | 1,872 | 14,395 |
| Linux | 1,000 items | 439,135 | 711,192 |
| Linux | 10,000 items | 4,406,703 | 6,678,925 |

Both emission routes ran on Darwin.
They do not measure Linux-host compiler performance.
The earlier metadata profiles use different diagnostic source paths.
No historical 1,000-item metadata baseline is assumed.
Metadata is a large part of the executable.

Subsequent installed qualification is recorded in the earlier complete campaign above.
The [installed diagnostic record](evidence/i011-qualification-20261022/compact-installed-diagnostics/manifest.json) keeps the observations, modules, and package identities.

## Qualification Limits

The completed runs above use their recorded source and package identities.
The last complete campaign uses source identity `f26d0b99e160cc84ecfdb3f511d2a13930c7a9794eca16130573620427d878ca`.
The final focused HTTP run uses the corrected packages and its own source identity.
The user-selected acceptance update permits review of the earlier results for unchanged behavior.
Non-HTTP and offline checks were not repeated on the final package identities.
The user stopped the subsequent full CI run; that run does not establish a full CI pass.

The installed compiler uses no external Go compiler, Rust compiler, C compiler, developer SDK, or network access.
Executables use the declared operating-system libraries.
The compiler producer uses development toolchains.
Lo is not self-hosted.

This qualification compares the current LLVM implementation with the interpreter.
It does not compare LLVM with Cranelift.
CPU training measurements do not show Metal training throughput.
I012 owns qualification through complete CLI, HTTP service, and training applications.
P006 owns the later language extension plan.
