# Native Receipt Performance

This change decreases native receipt work identified in the [performance analysis](native-performance-attribution.md).
It also corrects quadratic allocation work when a processor retains large state values created inside its callbacks.
The corrected runtime is candidate C5 in the recorded measurements.

## Implementation

Native value receipts now use a paged arena and scope lists.
The identity map connects each active receipt to a stable slot number.
Scope lists contain these slot numbers, which decreases identity lookups during transfers.
Small scope lists use four inline slots and grow when necessary.
The arena adds pages of 64 slots and uses released slots again.

A scope puts receipts for values inside retained graph fragments at the start of its candidate list.
Weak references identify the immutable graph roots for this retained group.
The runtime keeps the group without another full comparison while every root remains reachable from the next state.
The runtime compares each new receipt with the retained graph identities.
If one of these roots is absent, the runtime examines the full group again.

The graph cache also finds large immutable fragments owned by the processor itself.
Fresh records can use the same fragments without a full copy of the cached identity sets.
Receipt release removes the related graph summary before the arena uses its slot again.
The summaries keep weak references and identity metadata, without strong references to value payloads.

Callback file scopes keep their ancestry stack.
They create an observation frame only when they retain observations.
This removes empty frame work from callbacks that use no files.
Observation retention, pins, and close errors keep their existing paths.

## Public Regression Coverage

The new tests create large lists inside the first callback.
Later callbacks keep these lists directly or inside fresh state records.
The cases include one list, a fresh record, and five independent lists.
Each case runs with and without a positive list quota.

The before runtime failed the allocation growth assertions.
The corrected C5 runtime passed them.
For the fresh-record case with a list quota, the measured allocation bytes were:

| Items and callbacks | Before, bytes | C5, bytes |
| --- | ---: | ---: |
| 128 | 986,816 | 412,956 |
| 256 | 3,154,936 | 825,500 |
| 512 | 11,032,112 | 1,650,524 |

The corrected measurements approximately double when the fixture size doubles.
The five-list case also passed its linear allocation budget.
Another test adds a retained fragment, returns a state without it later, and allocates a larger probe under a live quota.
This test passed and exercises release after the retained group changes.

Quota boundary tests retained the same minimum limits for success:

| Workload | Live bytes | Cumulative data bytes |
| --- | ---: | ---: |
| Literal map | 8,851 | 21,539 |
| Fold with an ancestor value | 4,435 | 17,191 |

The focused Darwin and Linux coverage passed after the final C5 source changes.
Its coverage includes value lifetimes, overlapping pins, worklists, file observations, removal guards, and cleanup errors.
Full local `make ci` passed with exit status zero.
The run includes Go and native integration suites, 21 semantic report tests, and 217 qualification tests.

## Paired Measurements

Each program constructs 8,192 integers and executes four map or fold operations.
Compilation and smoke execution occur before measurement.
The interval includes process startup, runtime admission, input construction, and scalar output.
Child CPU time is user time plus system time.

The final campaign contains 294 executions across both workloads.
Each variant has 21 observations per workload.
A before-control execution occurs immediately before and after each variant execution.
The variant order changes each round, and every execution verifies its scalar result.

The table contains median CPU times and median paired CPU ratios.
Each ratio divides the variant sample by the mean of its two adjacent before samples.

| Runtime | Map CPU, ms | Map ratio | Fold CPU, ms | Fold ratio |
| --- | ---: | ---: | ---: | ---: |
| Before this change | 35.078 | 1.000 | 23.438 | 1.000 |
| Earlier candidate C3 | 31.222 | 0.916 | 22.400 | 0.959 |
| Corrected candidate C5 | 29.233 | 0.829 | 21.996 | 0.932 |
| Original reference `0be6847` | 20.666 | 0.588 | 17.849 | 0.756 |

C5 decreases paired map CPU time by 17.1 percent and fold CPU time by 6.8 percent relative to the before runtime.
C5 uses less CPU in 20 of 21 map pairs and all 21 fold pairs.
The original reference is faster in this campaign.
C5 does not remove the performance regression relative to that reference.

The original reference uses its historical runtime under the common compiler with an ABI identifier change.
Its cumulative resource policy differs from the current live-value policy.
The table includes C3 and the original reference for context.
They do not validate the final source.

## Limits And Evidence

These measurements cover the two stated scalar collection workloads on Darwin ARM64.
Host conditions changed during the analysis, so the ratios use adjacent controls from the same campaign.
The results do not establish the same performance on other hosts, graph shapes, or file workloads.
The changes keep dynamic growth and add no fixed collection or graph count limit.

The canonical Darwin archive has the same SHA-256 digest as the measured C5 archive.
The canonical Darwin and Linux archives have the same final source identity.
Evidence collection writes a [manifest](evidence/native-receipt-performance.json) and an [archive](evidence/native-receipt-performance.json.gz) with the validation receipts and measurement data.
It uses the zero CI exit receipt and final source identity.
