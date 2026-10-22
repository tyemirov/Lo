# Standalone Compiler Qualification Evidence

I011 uses the revised measurement contract requested by the user.
External load is context and does not prevent measurement.
Campaign11 remains historical evidence of an admission timeout without measurements.

All native executable builds use LLVM and LLD with the precompiled Rust runtime.
The compiler no longer generates Go source or includes the embedded Go runtime source bundle.
The distribution still supplies corresponding source materials in `source/`.
The interpreter remains available through explicit execution commands.
No Go installation, external compiler tools, or network access is necessary for installed compilation.

## Status And Identities

I011 is in progress. Offline, semantic, component, and full CI checks passed for source freeze7.
Campaign9 and campaign11 ended at their admission limits without a performance measurement.
Performance acceptance and the evidence audit are not complete.
Independent review of the complete qualification is still necessary.

The [nonperformance receipt](nonperformance-validation.json) records the passed checks and their archive identities.
The [performance status](performance-status.json) records the current campaign state and retained historical context.
The [reuse receipt](nonperformance-reuse.json) records the exact measurement-tool and affected fixture changes, with unchanged package identities.
The earlier semantic and offline checks retain their original source identity.
They were not rerun against the new aggregate identity.
B005 passed 178 Darwin checks, 176 Linux checks, and the host-to-Linux capture staging check after the complete correction.
All 25 B006 campaign checks passed after the cleanup correction.
Fresh final CI passed 2,635 native cases without failures or skips and all 197 qualification-tool checks.
The interpreter lane passed 1,741 cases with 157 native-only exclusions.
B005 and B006 are closed after their required checks and independent correction reviews passed.
The complete performance campaign is in progress. Its measurements remain separate evidence.
The [interim findings](../../native-qualification-findings.md) record completed Darwin lanes and partial build observations before corrective work.

Source freeze7 identifies `30456a563ea64716d16e3b0d954338fae144fdde5b66a13f570c31c6bbf501e2`.
The receipt includes all three package identities and their compiler, runtime, and manifest digests.
All four software host-to-target combinations passed offline compilation and copied-artifact execution without Lo.
The Darwin Metal package, relocation, nine rejection cases, and 56 component checks also passed.
Both installed compiler hosts passed 500-step XOR with final loss `4.251353501009969e-13`.

| Semantic lane | Passed cases | Failed cases | Excluded native-only cases |
| --- | ---: | ---: | ---: |
| Interpreter | 1,738 | 0 | 153 |
| Darwin native | 2,624 | 0 | 0 |
| Linux native | 1,414 | 0 | 0 |

The strict report passed all 394 references in these three lanes with no qualification gaps.
Each report must identify its actual source, package, runtime, toolchain, and test inputs.
After a source or package change, collect new evidence for the affected checks.

## Semantic Scope

Independent expectations and differential tests cover observable values, diagnostics, source spans, and logical charges.
They also cover evaluation order, lazy branches, cancellation, effect order, and resource cleanup.
Numerical checks include backpropagation, 200-step training, 500-step XOR, and checkpoint resumption.
The tests retain the numeric profiles and optimizer state contract.
Native device checks use controlled software dependencies and resource ledgers.

The semantic matrix records the required obligations and both mandatory coverage gates.
It covers 122 operations, 27 CLI contracts, 16 semantic groups, and six numeric-profile groups.
Its 394 original references have explicit native test mappings.
I005 does not verify all error-kind combinations.
I005 also records the source-reviewed `Optimizer.SGD.State.new` constructor exemption.

## Environments And Limits

Darwin offline acceptance uses operating-system sandboxes with an empty `PATH` and declared library access.
Linux offline acceptance uses QEMU without an external network interface.
Another isolated environment executes each copied artifact without Lo or its compiler package.
The software matrix covers both compiler hosts and both target systems.

Nine negative cases check input rejection, prior output preservation, and empty staging directories.
An isolation guard checks filesystem, network, and descendant restrictions.
Cancellation has a public CI build test. It is not an offline matrix cell.

The Darwin Metal package has compile, kernel admission, and software semantic checks.
Physical Metal execution and performance remain unmeasured.
Linux semantic and performance reports identify their selected QEMU accelerator.

One build retains the 180-second time limit and the 64 MiB module and artifact limits.
The diagnostic output limit is 1 MiB.
The measured peak build memory limit is 2 GiB.
The full integration package has a 120-minute timeout.
That timeout does not extend the limit for one build.
The [compilation contract](../../native-compilation-contract.md) defines the complete requirements.

## Performance Evidence

Each target must pass full execution, process, HTTP, and build lanes.
Execution, process, and HTTP lanes retain three warmups, ten measured sample pairs, and the observed execution order.
The build lane retains the first build, three warmup builds, and ten measured builds.
Timing jobs run one at a time.

The [current measurement contract](../../native-compilation-contract.md#measurement-validity) removes quiet-host admission.
The fixed corpus, sample counts, confidence calculations, and numeric thresholds stay the same.
Each paired lane must record the declared alternating order and chronological sample indices.
External process and load observations are retained as context.
Missing or incorrect results, incomplete samples, and changed inputs prevent admission.
A complete failed or inconclusive gate remains a measured result.
Both targets and all lanes run before the campaign reports the complete gate outcome.
No automatic retry or sample selection is permitted.

The revised producers passed a real startup diagnostic during a bounded, owned CPU workload on both targets.
Each target retained all ten measured pairs and passed result verification.
Darwin ran natively, and Linux ran under QEMU HVF.
Both startup gates passed, and all owned processes were reaped.
This diagnostic establishes workload overlap with the producer commands, including preparation.
It does not establish individual pair timestamps or full performance acceptance.

### Decisions From The Campaign

This campaign supports the I011 qualification decision for the recorded environments.
The [findings record](../../native-qualification-findings.md) separates measured results from possible investigations.
Its reference is the Lo interpreter. It cannot select between LLVM and Cranelift.
It does not determine which language features to implement next.
I012 owns qualification through complete CLI, HTTP service, and training applications.
The [measurement contract](../../native-compilation-contract.md#measurement-validity) defines statistical limits.

| Evidence | Supported decision | Next action after a failed gate |
| --- | --- | --- |
| Offline builds and semantic checks | Accept the standalone compiler with the tested behavior preserved. | Correct the failed behavior or package input. |
| Checked execution | Accept native execution cost after program admission. | Examine a CPU profile of the failed workload before a lowering or runtime change. |
| Process latency | Accept executable launch, admission, execution, and output cost. | Examine launch and admission when checked execution passes. |
| Persistent HTTP | Accept the two sequential loopback request workloads. | Examine request handling and runtime integration. |
| Build resources | Accept compilation time, sampled memory, and artifact size for the fixed programs. | Examine LLVM module size, compilation structure, and toolchain cost. |

Report each workload's absolute cost, paired ratio, interval, and gate result.
The execution case named `startup` measures a trivial program after admission.
Use the process lane to assess executable launch cost.
The callback aggregate does not replace the individual case results.
The execution and build lanes include the two CPU training workloads.
These measurements do not establish Metal training throughput or performance on every machine.
HTTP results do not establish performance with concurrent clients or request latency in production.
Build reports contain periodic memory samples. Short peaks can occur between samples.
Allocation figures remain descriptive.

If all required gates pass, complete the evidence review and stop performance work for I011.
A failed timing gate identifies an investigation scope. It does not identify the cause.
An inconclusive gate leaves acceptance unresolved.
Do not add an experiment without a specified decision and a design fixed before execution.

### Historical Quiet-Host Campaigns

The primary agent restored two generated Metal assets from qualified inputs kept inside Lo.
The assets contain the same bytes as those inputs. Source and package identities stayed unchanged.

Campaign9 used a predeclared quiet-host plan, full process trace, and final environment summary.
Its admission rule required the identified competing process to be absent.
It also required 61 quiet observations over at least 300 seconds on both clocks.

Only sustained CPU-load invalidation permitted its second full attempt.
A failed or inconclusive gate in a valid environment stopped that campaign.
Both monotonic and UTC timestamps controlled observation gaps.
Invalid environment evidence rejected the whole timing attempt.

Attempt9 recorded 720 admission observations, including 556 CPU-limit violations.
Its longest quiet run contained 11 samples, below the required 61.
The admission interval exceeded 3,600 seconds on both clocks.
No timing lane started. This outcome does not measure compiler performance.
Post-run checks confirmed unchanged source, packages, and sealed inputs.

The original driver record retains its initialized `performance_status: 1` and `disposition: pending` fields after admission failure.
They are not a benchmark exit or an active campaign.
The performance status receipt records the actual terminal admission timeout and observed driver exit.
The sealed campaign permits no further attempt after this outcome.
An independently reviewed plan and unused output paths are necessary for a new campaign.

Campaign11 recorded 720 process observations and one terminal timeout record.
It recorded 115 CPU-limit violations.
The longest quiet run contained 52 samples over about 255 seconds.
Admission required 61 quiet observations over at least 300 seconds.
The timeout interval reached 3,600 seconds on both clocks.
No process observation found the identified competing process.

No performance measurement started on either target.
The driver kept `measurement_status: null`.
Driver exec `47129` ended with status `1`.
This outcome does not measure compiler performance.

After the timeout, source, package, sealed-input, and full nonperformance identity checks passed.
Independent outcome review verified 72 different sealed records and the authorization.
The review found no errors and found the blocked status correct.
Attempt12 did not start and is not permitted after this timeout.

Reports retain raw measurements, build resource observations, and isolation facts.
The merger admits whole lanes with equal required identities and calculates all gates again.
It rejects absent lanes, duplicate lanes, diagnostic selections, and incomplete reports.
A complete measurement report can contain a failed or inconclusive gate.
Only a report with `qualification_complete: true` establishes performance qualification.
The final two target reports and all eight lane reports must have different evidence records.

## Historical Records

The [preservation index](preservation-index.json) identifies earlier records by original path and SHA-256.
It also records each compressed archive path, archive digest, purpose, and scope.
Historical failures explain the corrections. They do not establish final acceptance.
These records include the following cases:

- Retired source and package candidates and their incomplete performance runs.
- Attempt8, whose complete Darwin lane passed its gates but failed the recorded environment rule.
- Campaign9, which reached its admission limit without a performance measurement.
- B001 nested-fold cancellation progress and its focused correction.
- B002 missing Metal link export and its focused correction.
- B003 HTTP callback context and duplicate cleanup diagnostics and their focused correction.
- The candidate4 integration timeout after 30 minutes, with no individual test failures recorded before interruption.
- B004 shared-console transport corruption and its focused correction.
- The kernel watchdog failures during artifact transport, which stopped offline acceptance without a success result.
- B005 blocked semantic output capture, its large-output regression, and fresh both-target checks.
- B006 process observation and terminal record failures, with public cleanup regression checks.

B004 sends artifact and result records through an isolated virtio export channel.
The guest opens `/dev/vport0p1` directly.
The host reads protocol records from `export.log` and retains kernel diagnostics in `serial.log`.
Missing, truncated, or malformed export records remain failures.
Focused transport checks and real pinned-QEMU checks do not replace the current full corpus or final CI.

Implementation reviews do not replace the final completion review.
The final summary must identify which records qualify the final inputs.

Inspect a compressed record without an extracted copy:

```sh
gzip -cd docs/evidence/standalone-compiler/final/final-semantic-report.json.gz
```

Use the exact archive path from the preservation index.
Compare its SHA-256 with the recorded archive digest before inspection.
The index also records the digest of the uncompressed record.

## Reproduce

Use the primary Lo checkout and prepared pinned inputs inside Lo.
Keep all output and tool caches inside this repository.
The [distribution guide](../../compiler-distribution.md) defines preparation and package construction.
Go and other producer tools can be necessary for developer preparation.
Installed compilation uses the completed package alone.
Use new output directories for each reproduction.

These commands assume the prepared packages are in `bin/distributions`.
Run the semantic checks and offline matrix before performance measurement.

```sh
CI=1 make test-native-linux \
  COMPILER_DISTRIBUTION_OUTPUT=bin/distributions \
  NATIVE_QUALIFICATION_OUTPUT=_scratch/i011-reproduce/qualification \
  LINUX_ACCELERATOR=hvf </dev/null
CI=1 make test-semantic-migration \
  COMPILER_DISTRIBUTION_OUTPUT=bin/distributions \
  SEMANTIC_OUTPUT=_scratch/i011-reproduce/semantic \
  SEMANTIC_ARGS='--llvm-log linux/arm64=_scratch/i011-reproduce/qualification/linux/semantic-linux-arm64.jsonl' </dev/null
CI=1 make test-compiler-distributions \
  COMPILER_DISTRIBUTION_OUTPUT=bin/distributions \
  COMPILER_DISTRIBUTION_ACCEPTANCE=_scratch/i011-reproduce/offline </dev/null
```

Select the prepared runtime state through `NATIVE_RUNTIME_STATE` when its path differs from the Make default.
Run final CI with the installed Metal compiler and its matching manifest:

```sh
CI=1 LO_TEST_COMPILER="$PWD/bin/distributions/lo-darwin-arm64-metal/bin/lo" \
make ci \
  COMPILER_DISTRIBUTION_OUTPUT=bin/distributions \
  SEMANTIC_COMPILER=bin/distributions/lo-darwin-arm64-metal/bin/lo \
  SEMANTIC_TOOLCHAIN=bin/distributions/lo-darwin-arm64-metal/toolchain.json \
  LLVM_TOOLCHAIN=bin/distributions/lo-darwin-arm64-metal/toolchain.json \
  TEST_ARGS='-count=1' </dev/null
```
Use the [performance commands](../../native-compilation-contract.md#performance-commands) for serial measurement and strict lane merges.
Keep all input reports and their source identities with each merged report.
Do not run other owned tests, builds, or timing jobs during performance measurement.
Unrelated host activity does not prevent measurement.

The current status prose has a scoped language review and mechanical checks.
New qualification results will make an updated evidence and documentation review necessary.
Mechanical language checks do not certify the evidence or its technical conclusions.
