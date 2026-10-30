# Resource Traversal And Probe Output Corrections

These corrections change Go tensor-allocation traversal and native open-file probe output limits.
The previous native receipt report still describes the measured CPU performance.

## Go Resource Traversal

The Go allocation helper stops traversal at types that cannot contain tensor storage.
Structural lists, maps, ranges, and records use their component types for this decision.
Other types still use the private child traversal because they can retain tensors in private fields.
These types include failures and types with private fields.
The correction keeps the existing `VisitWhile` child edges and counts each backing allocation one time.

The public regression fixture keeps a list of integers while each callback makes a temporary tensor.
Before the correction, cleanup examined the large list for every callback.
The list fixture failed its allocation budget.
The corrected list and record fixtures passed at 128, 256, and 512 callbacks.

The list allocation measurements were:

| Items and callbacks | Before, bytes | Corrected, bytes |
| --- | ---: | ---: |
| 128 | 1,848,320 | 356,096 |
| 256 | 6,743,776 | 671,168 |
| 512 | 25,254,192 | 1,296,192 |

These are allocation measurements from the public tests, without a new CPU timing campaign.
A private graph regression also passed.
It includes guarded tensor captures, aliases, and an `Int!` failure whose error progress retains a tensor.
The traversal still counts each shared backing allocation one time.

## Native Probe Transport

The native probe now has an 8 MiB limit on each output stream.
The stdout and stderr limits apply even when value, live, and cumulative data quotas are zero.
The runtime accepts 8 MiB and rejects the first additional byte with terminal `runtime.limit`.
A Lo handler cannot convert that limit into success.

The public harness makes stream data instead of a large source literal.
The cases include the boundary and one-byte overflow on stdout and stderr.
A fifth case supplies 8 MiB on both streams, which shows that each stream has its own limit.
All ten Darwin and Linux ARM64 cases passed with the corrected runtime.

The Darwin ARM64 cases ran on macOS.
The Linux ARM64 cases ran under QEMU.
The runtime before the correction failed both overflow cases on each platform.

The harness also verifies that overflow prevents a subsequent commit.
It verifies zero live runtime handles and no child that `waitpid` can reap.
The final failed and passed runs use the canonical three-argument `lo_cli_main` interface.
Initial diagnostic harness runs do not supply acceptance evidence.

## Validation And Evidence

The Go regression checks and the independent source review passed.
The native stream cases passed on macOS for Darwin ARM64 and under QEMU for Linux ARM64.
The full local `make ci` run passed with exit status `0`.
The run included Go integration, native integration, 21 semantic tests, and 217 qualification tests.

Evidence collection writes a [manifest](evidence/review-corrections.json) and an [archive](evidence/review-corrections.json.gz) with source identities and validation receipts.
It uses the actual CI exit receipt and the canonical runtime archive identities.
