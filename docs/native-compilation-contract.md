# Native Compilation Contract

F001 uses generated Go source with Go 1.27.1 and `golang.org/x/sys` v0.49.0.
The selected targets are `darwin/arm64` and `linux/arm64`.
The interpreter and native artifacts use the same logical execution contract.

## Build Limits

| Resource | Limit | Enforcement |
| --- | --- | --- |
| Generated source | 32 MiB | Reject before Go compilation. |
| Build elapsed time | 180 seconds | Stop the complete compiler process group. |
| Toolchain diagnostic output | 1 MiB | Stop the build when output exceeds the limit. |
| Artifact size | 64 MiB | Reject before publication. |
| Peak build memory | 2 GiB | Qualify through recorded measurements. |

The initial host probe built the current CLI with a new Go build cache.
It used approximately 3.1 seconds, 315 MiB of peak memory, and an 11 MB artifact.
These measurements select conservative initial limits before native code generation.
The final corpus must satisfy these limits on both targets.
The memory budget is a qualification criterion, not a process memory limit.

## Logical Charge Order

- Charge each reached statement before its expression or effect.
- Charge each reached expression before its children.
- Evaluate arguments in source order before their placement in parameter order.
- Charge each successful expression's expanded value weight after evaluation.
- Charge each list callback iteration before the callback.
- Charge each aggregate iteration before arithmetic.
- Admit tensor work before allocation and numeric execution.
- Preserve flow depth through callbacks and error handlers.
- Poll cancellation at each charge point and the existing adapter boundaries.
- Preserve derivative snapshots, tape limits, and optimizer work charges.

## Fixed Performance Corpus

The `tests/corpus` package defines startup, callback, record, Unicode, and large-integer inputs.
Map, filter, and fold each use 1,000 and 10,000 varied integers.
The benchmark script adds exact File output and persistent HTTP response checks.
The existing training fixtures remain part of qualification.

Use three warmup samples and at least ten measured samples.
Measure checked execution separately from process latency and build time.
Run timing lanes separately to prevent measurement contention.

The callback execution gate remains a 2× geometric-mean speed improvement on each target.
Each other elapsed-time gate permits at most a 10% increase.
HTTP uses p95 latency and reciprocal throughput with the same 10% limit.
Report inconclusive results when uncertainty overlaps a gate.

## Artifact Construction

Use an embedded source bundle with an explicit package list and vendored dependencies.
Exclude compiler packages, interpreter packages, tests, and private files from that bundle.
Build in a temporary module with downloads disabled and controlled Go environment variables.
Publish the complete artifact through a rename within the output directory.
Preserve the previous artifact on failure or cancellation.
Include source and bundle digests, source identity, target, and toolchain in artifact metadata.
Exclude timestamps, temporary paths, and resource grants from metadata.

## Device Backend Selection

The default `--device-backend software` configuration disables cgo and supports both selected targets.
The explicit `--device-backend metal` configuration requires the Metal-enabled compiler and a local `darwin/arm64` target.
Unsupported combinations fail before toolchain execution.
The [native Metal contract](native-metal.md) defines its additional manifest, toolchain checks, and metadata.
Both configurations use the same build limits, cancellation, and atomic publication path.

The [acceptance report](native-compilation-results.md) records completed qualification on both targets.
