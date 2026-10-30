# Native Collection Performance Analysis

This analysis examines the remaining native performance regression after the [resource tracking corrections](resource-performance-remediation.md).
The baseline is revision `0be6847ccf3d2a66b57f0505ded7ae5b4be9888a`.
The current source is revision `d42c89b9a7e6d9736b289f6972fdabf3894971e0`.

## Source Findings

The existing collection operations used growable vectors before the change.
Both revisions create the output with `Vec::new()` and add elements with `push` or `extend_from_slice`.
The callback argument vector also has the same reuse rules.
The new `Flow.worklist` operation uses `VecDeque`, but the measured map and fold programs do not use it.

The generated runtime calls for the existing collection operations are unchanged.
The compiler changes update the ABI identifier and permit the new flow processor contract.
The release optimization settings are unchanged.

The baseline compares each value with a 16 MiB quota and cumulative evaluation data against a 64 MiB quota.
The current runtime also records value identity, live weight, scope ownership, and retained references.
It updates these records when callbacks return and scopes close.
File observation scopes also enter and close for callbacks that use no files.

Map keeps the receipt for each output element until its collection scope closes.
The receipt store and candidate set grow with the output list.
Scope close then moves the retained receipts to the parent scope.
Fold keeps only the next state, so its active candidate set stays small for scalar states.
These different ownership patterns predict a larger receipt cost for map.

Program metadata keeps the map callback's literal `3` value alive throughout the invocation.
Each callback creates an accounting receipt for that value.
Callback scope close releases this receipt because the returned integer has a different identity.
The next callback creates the receipt again.
The source predicts 32,768 such admissions and releases in this map fixture.
This count is source-derived, and its separate timing cost is unmeasured.

Each shared runtime call first verifies the context owner.
The current `lo_charge_value` and `lo_cause` paths then call `error_entry`, which verifies the owner again.
The error path must preserve the order of primary errors and scope cleanup errors.
A correction must preserve that behavior when it removes repeated checks.

## Controlled Measurements

Each program constructs 8,192 integers and executes four map or fold operations.
Compilation occurs before measurement.
Each alternative runtime has 21 samples per workload.
Each sample has a current sample immediately before and after it.
The variant order changes each round.
All samples remain in the report, and every execution verifies its scalar result.

The interval includes process startup and runtime admission.
Child CPU time is user time plus system time.
The current runtime has 84 control samples per workload.
The four-variant campaign contains 294 executions.
No other task build or benchmark runs during this campaign.

| Runtime | Map CPU, ms | Fold CPU, ms |
| --- | ---: | ---: |
| Baseline | 17.688 | 16.985 |
| Current | 25.911 | 21.971 |
| Current without logical value accounting | 19.842 | 18.864 |
| Current without file scope accounting | 26.115 | 20.828 |

The columns contain medians.
The extra CPU time is mostly user time.
This result confirms the runtime cost reported in the earlier study.
The current compiler builds each artifact, with its ABI identifier adjusted for the historical runtime.
Source review found no other compiler difference for these workloads.

## Measured Costs

The diagnostic without logical value accounting reduces map CPU time by 23.6 percent and fold CPU time by 14.2 percent.
These percentages use each sample's adjacent current controls.
This change removes approximately 6.1 ms from map and 3.1 ms from fold.
It accounts for most of the measured regression in these workloads.

The diagnostic omits quota arithmetic, receipt admission, ownership changes, and release work.
It does not isolate one hash lookup or reference-count operation.
It preserves the compiler, collection vector growth, and ordinary value construction.
The result identifies the logical value ledger as the main measured cost.

The diagnostic without file scope accounting reduces fold CPU time by 4.8 percent.
Map shows no measurable improvement in this campaign.
Thus, file scope accounting is a secondary target for these workloads.
The separate diagnostic costs can overlap and must not be added as independent percentages.

The diagnostic runtimes omit required behavior.
They show the cost of selected runtime work and are not production fixes.
Production runtime source remains unchanged.

The owner-check diagnostic has small paired median differences.
Its 95 percent bootstrap intervals include no change for both workloads.
These samples do not establish a reliable performance improvement from that change.
These measurements do not establish the repeated owner check as the main cause.

A later campaign retains the former cumulative quota checks but omits ownership records.
Its map and fold times remain close to the diagnostic without value accounting.
This result supports ownership tracking as the main target.
The `cumulative-only` diagnostic does not enforce the current live-value contract.

The later campaign ran under different host conditions.
A subsequent process observation showed substantial concurrent external CPU load.
Its current medians increased to 41.195 ms for map and 30.924 ms for fold.
Those results are consistent with the earlier findings, but they do not replace the earlier percentages.

## CPU Profiles

The CPU profiles use 500 fresh CLI invocations in one process for each runtime.
Each invocation executes the four-map program and receives a new quota budget.
All 1,000 results match the expected value.
The harness also verifies zero live values after each invocation.
The sampler completed successfully for both runtimes.

The current profile contains repeated samples in `IdentitySet::insert`, `EntryMap::get`, `Ledger::release`, and `Context::scope_close`.
Hash-table capacity changes also appear in the call stacks.
These stacks support the measured value-ledger result.
Optimized code and concurrent host load prevent a precise division of CPU time among these functions.

## Next Implementation Targets

The first target is value receipt admission and transfer for scalar collection results.
A correction must preserve live quotas, alias identities, retained children, and cleanup errors.
Map requires particular attention because its output keeps many receipts at the same time.
Repeated receipt admission for retained literals is another target.

File observation scopes are a secondary target.
A correction can create a record when the scope first retains an observation.
It must preserve scope ancestry.
The duplicate owner check is also a possible simplification, with error precedence preserved.
Further implementation and regression tests must establish the benefit of each correction.

## Evidence And Validation

The [manifest](evidence/native-performance-attribution.json) lists the diagnostic campaigns and their source revision.
The [archive](evidence/native-performance-attribution.json.gz) contains workload source, diagnostic patches, individual samples, profiles, artifact identities, and independent reviews.
All measurements use local Darwin ARM64 execution.
The source and measurement reviews found no unresolved correctness issue in the diagnostic conclusions.

This task changes documentation and preserves diagnostic evidence.
It changes no production runtime behavior.
The document checks and `git diff --check` passed.
The previous implementation CI result remains separate from this diagnostic work.
