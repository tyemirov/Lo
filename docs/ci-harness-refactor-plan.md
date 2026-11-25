# CI harness refactor plan

## Status and execution restriction

This document records the original source analysis, the implemented harness, and the remaining acceptance steps.
The complete local `make ci` run passed in 263.16 seconds, or 4 minutes 23 seconds.
The session reported 1,463 successful results.
Formatting, static analysis, the compiler build, coverage validation, and cleanup are included.
The measured source and full job durations are in [the acceptance evidence](evidence/ci-harness-results.json).

Do not run `make ci` during the refactor.
Complete all test migrations before the first final CI run.
Before that run, use focused measurements to establish an expected total below 300 seconds.
If the measurements exceed the time budget, correct the harness before the final run.

## Required result

Execute every applicable repository-owned automated test in less than 300 seconds through one canonical command.
Include test executable compilation, fixture preparation, test execution, result collection, and cleanup in the measured interval.
Include formatting and static analysis in the final `make ci` interval.
Preserve each existing assertion, parameter case, required target, and public entry point.
Disable cached test results during acceptance measurements.

Keep dependency downloads and toolchain preparation outside this interval, as the current workflow requires.
Include any build that the selected tests require inside the interval.

Do not obtain the time reduction through test removal, new skip conditions, or transfer of slow tests to another gate.
Keep benchmark campaigns separate from automated correctness tests.
Retain automated tests of benchmark tools in the test manifest.
Report software execution environments and their evidence limits.

## Observed failure

The operator's integration run failed after `5883.304` seconds, approximately 98 minutes.
A 300-second result requires a reduction greater than 19.6 times for that integration run alone.
The complete test session requires additional work beyond integration tests.

`TestAdapterHTTPHeaderValidation` first reported `unexpected end of JSON input`.
Later tests reported `no space left on device` during temporary directory creation.
The supplied log does not establish the cause of the first JSON error.
It also does not identify which process exhausted the shared filesystem.

The JSON diagnostic helper decodes output before this test examines the expected process status.
This sequence can obscure a compiler or process failure.
Failure reports need the command, operation, exit status, stdout, and stderr before JSON decoding.

Before the refactor, source inspection found the following repeated work.

| Path | Original behavior | Consequence |
| --- | --- | --- |
| [Native fixtures](../tests/integration/native_test.go) | `buildForTest` holds one mutex through compilation. | Independent builds serialize even after tests gain concurrency. |
| [Native fixtures](../tests/integration/native_test.go) | Cache keys include temporary source paths. Test cleanup removes entries. | Separate immutable fixtures cannot reuse build results. |
| [Compiler snapshots](../internal/nativebuild/llvm_snapshot.go) | Each public native build hashes its private toolchain snapshots. | Each build reads substantial input data. |
| [LLVM runtime scenarios](../tests/integration/llvm_lifetime_test.go) | Each scenario repeats tool validation and five backend commands. | Module, entry, harness, and link preparation multiply across cases. |
| [Linux test bridge](../tests/integration/llvm_linux_qualification_test.go) | Each Linux invocation on the inspected Darwin host calls the guest runner. | Argument variants require separate guest boots. |
| [Guest runner](../tools/nativeruntime/run.py) | Each invocation copies libraries, creates an initramfs, and boots QEMU TCG. | Guest preparation repeats across Linux cases. |
| [Filebench tests](../tools/filebench/main_test.go) | Invocations build the frontend and use `go run` for the CLI. | The same Go binaries require repeated builds. |
| [Runner tests](../tools/loqualification/test_performance.py) | Parameter cases build the same performance runner. | Configuration assertions repeat binary preparation. |
| [Report tests](../tools/semanticcoverage/test_report.py) | Report fixtures build a frontend and launch nested Go tests. | Nested execution escapes one shared preparation schedule. |
| [Makefile](../Makefile) | The test groups execute sequentially and Go output remains buffered. | Independent work cannot overlap and active tests remain invisible. |
| HTTP runtime fixtures | Separate scenarios use the same fixed host ports. | Concurrent execution can fail during socket binding. |

The inspected Darwin distribution contains approximately 165.5 MiB of Clang, 120.8 MiB of LLD, and 20.6 MiB of runtime.
Private snapshot creation uses APFS cloning on Darwin.
Digest verification still reads approximately 307 MiB for each ordinary native build.
This is input verification, not evidence of 307 MiB of new physical storage per build.

## Complete inventory

Source inspection found 817 integration test functions in 228 Go files.
The count of 818 declarations includes `TestMain`.
There are 25 internal Go test functions and 10 tooling Go test functions.
Integration sources contain no `t.Parallel()` calls.

The LLVM and Linux name groups contain 264 and 82 test functions, respectively.
These groups overlap and do not count parameter expansion.
The Python groups currently referenced by CI contain 135 test definitions before parameter expansion.
The runtime source also contains 24 Rust `#[test]` declarations.
Source counts are inventory inputs, not proof of executed coverage.

The current `ci` dependency list is not a complete test inventory.
Separate targets contain distribution acceptance, semantic migration, Linux qualification, backend probes, and tagged Metal tests.
Some targets overlap existing cases.

Create one test manifest before test migration.
Include Go tests, fuzz seeds, Python parameter cases, Rust tests, and separate correctness acceptance targets.
Exclude vendored dependency suites and benchmark campaigns from repository-owned test identifiers.
Record every platform condition and required software environment.

Map overlapping target invocations to their unique assertions and target requirements.
Preserve both target requirements when one named test requires execution on two targets.
Require an explicit mapping from each original case to its refactored case.
Reject missing cases, duplicate assignments, unexpected skips, and incomplete worker results.

## Harness architecture

### One test session

Add a repository-owned session runner under `tools/testsession/`.
Make `make ci` call this runner after migration.
Remove the old sequential orchestration after the new command satisfies the manifest.

Create a unique session directory under `_scratch/`.
Build each Go test executable and shared helper binary once per session.
Identify each test executable by package, target, build tags, CGO configuration, and relevant flags.
Prepare separate executables for tagged cases, including `metal && darwin && arm64 && cgo`.
Prepare the frontend, performance runner, filebench CLI, process launcher, and required runtime test executables once.
Use ordinary language build caches, but execute all selected tests anew.

Give workers explicit binary paths and workspace paths.
Update compiler selection to distinguish the development frontend from an installed compiler.
The current `LO_TEST_COMPILER` branch suppresses automatic development toolchain selection.
Preserve explicit toolchain admission when workers share a development frontend.

### Immutable artifacts and private fixtures

Create a session-owned artifact registry for reusable test inputs and build results.
Reserve an artifact key under a short lock.
Build the artifact outside that lock.
Publish one completed result to all callers for that key.
Keep unrelated keys independent.
Remove incomplete outputs after build failure or cancellation.

Let the session runner own the registry and resource scheduler across all worker processes.
Use a repository-local Unix socket with typed requests and responses for worker coordination.
Keep the socket path short enough for the host's Unix socket limit.
Track each worker's requests, artifact references, child processes, and resource reservations.
On worker death, cancel its work and release its reservations after owned child cleanup.
Publish artifacts atomically after successful completion.

Keep a shared build active while another caller still requires its result.
When all callers cancel, cancel the build and remove incomplete output.
Add regressions for simultaneous requests, cancelled callers, worker death, and publication failure.

Use explicit immutable source fixtures for value and catalog assertions.
Include source identity, target, backend, runtime, tool identities, exact flags, and transformations in applicable keys.
Keep source paths in identities when diagnostics depend on those paths.
Do not change the existing mutable `sourceFile` helper into a shared source store.

Keep private fixtures for source removal, source replacement, relocation, artifact modification, entry locks, and process lifetime assertions.
Preserve `TestNativeRemovedSourceDiagnostics` and the actual running-image checks in Program lock tests.
Keep each executed program in a fresh process.
Declare exclusive host resources when existing fixtures share a fixed endpoint.
Keep unrelated groups eligible while an exclusive resource is occupied.

Share admitted tool inputs and identical entry objects within the runtime harness.
Reuse Lo objects and harness objects only when their complete build identities match.
Include `LO_CASE`, transformed source bytes, headers, and SDK identity in harness object keys.
Retain real public builds for compiler admission, packaging, cancellation, publication, and standalone artifact assertions.
Keep production snapshot and digest verification requirements unchanged.

Separate runtime fixture preparation from repeated compiler-admission coverage.
For runtime assertions, use public `lo build --emit llvm` with the session's verified backend inputs.
Compile and link those modules with the declared runtime and a reusable CLI entry object.
Execute the resulting native artifact through its real CLI.
Preserve exact compiler flags, runtime behavior, diagnostic source identities, and required artifact metadata.

Keep full public `lo build` execution for each distinct build contract, target, backend, and admission condition.
Keep real private builds for all identity, corruption, cancellation, and publication cases.
Record the original compiler boundary and replacement fixture preparation in the manifest mapping.
Do not substitute an interpreter or mocked runtime for native execution.

This change removes repeated snapshot verification and fresh backend executable paths from ordinary runtime fixture preparation.
Object reuse alone cannot remove that cost for unique programs.
The focused measurements must establish the reduction in backend launches, input reads, and process startup time.

### Process shards and resource limits

Use isolated processes as the initial concurrency boundary.
Assign disjoint test sets to workers from the frozen manifest.
Balance assignments with measured durations and declared dependencies.
Give each worker private environment variables, temporary directories, mutable fixtures, and output streams.

Process isolation preserves tests that call `t.Setenv` directly or through helpers.
For example, the catalog's `File.open_paths` case changes process environment through `installFileProbeFixture`.
Unrestricted `t.Parallel()` would make that case invalid.

Add one resource scheduler for compiler processes, guest processes, memory, and temporary storage.
Prevent workers from starting independent nested concurrency pools.
Use at most ten host workers, eight active backend builds, and four guest workers on the inspected host.
The portable defaults decrease these limits when the available CPU count is lower.
Select portable limits from available resources without a hardware acceptance prerequisite.
Change concurrency only when focused measurements show improved total time within the resource budget.

### Linux guest batches

Replace the per-invocation guest bridge with bounded guest batches.
Reuse the existing [batch protocol](../tools/loqualification/batch.py) and its result verification.
Extend that protocol before migration.
The current initializer shares writable guest state and does not reset files between cases.
The current runner also requires an installed compiler package and fixes several environment paths.

Add arbitrary executable payloads, explicit case environments, and private mutable state to the canonical protocol.
Add regressions for filesystem leakage, exact environments, process cleanup, and result-to-case identity before case migration.
Remove the old single-execution bridge after migration.

Prepare immutable guest libraries once per session.
Boot one guest per assigned batch, rather than one guest per assertion or argument variant.
Use QEMU HVF on Darwin ARM64 and QEMU TCG on other supported hosts.
Both are software acceptance environments.

Reject an unsupported explicit accelerator selection.
Do not substitute another accelerator after failure.
Preserve direct execution on Linux ARM64 where the current helper already supports it.
Keep companion scenarios in their declared execution environment.

Record each case's identifier, executable, argv, environment, cwd, stdin, timeout, streams, and exported files.
Execute each case in a fresh child process with private mutable filesystem state.
Preserve companion executables, capture behavior, loopback services, signals, and process-tree cleanup.
Terminate descendants and remove case state before the next case.
Keep explicit fresh-boot cases only where guest startup or boot isolation is the behavior under examination.

Convert synchronous guest helper calls into declared cases with result callbacks.
Represent dependent case sequences explicitly.
Return results to the existing assertions with their original identifiers.
Do not create a shared guest that permits state leakage between tests.

### Storage ownership and failure reports

Measure available disk space and session storage before expensive work.
Reserve storage for each active build and guest batch.
Release temporary modules, objects, guest images, and private fixtures after their last use.
Keep reusable artifacts only while assigned cases still require them.
Use a byte limit and active-user counts for registry retention.

Limit session storage to 10 GiB beyond existing dependency and toolchain inputs.
Measure physical allocation as well as logical file sizes where APFS clones affect the result.
Treat this target as an unverified design budget until focused measurements establish actual needs.
Do not solve resource exhaustion through deletion of another task's state or external caches.

On `ENOSPC`, report the failing operation and owned path immediately.
Stop new session work and terminate only session-owned child processes.
Report unfinished cases as aborted, never passed or silently skipped.
Preserve bounded failure evidence and release disposable owned files.
Do not parse a failed command's empty output as a successful JSON result.

Inject storage failures in public harness tests.
Verify partial-build cleanup, guest cleanup, cancellation, and subsequent execution with a fresh session.
Keep each injected fixture inside Lo.

### Progress and complete results

Stream named test start, completion, and failure events.
Report active workers and elapsed time at least every five seconds.
Record backend launches, guest boots, artifact reuse, and phase duration.
Monitor session storage every five seconds.
Report memory and storage measurements separately when available.

Keep machine-readable events separate from diagnostic stderr.
Preserve original command failure details before JSON decoding.
Require every applicable manifest case to complete successfully before session success.

## Time budget

The following limits are design targets, not measured predictions.
The middle row overlaps independent execution groups under one resource scheduler.

| Critical-path stage | Proposed wall-clock budget |
| --- | ---: |
| Compile test executables, prepare shared tools, and start static analysis | 70 seconds |
| Execute all host, guest, tooling, Rust, and isolated acceptance cases | 140 seconds |
| Verify inventory, collect results, and remove session state | 15 seconds |
| Contingency for measured variation | 45 seconds |
| Expected total ceiling after focused validation | 270 seconds |

Static analysis must finish within the first two stages.
Each execution group requires a measured schedule under the same concurrency limits.
A passing measurement can use an equal or smaller storage limit.
A measurement with a larger storage limit cannot establish the selected storage budget.
Do not add isolated group measurements and assume unlimited overlap.
Include compiler startup, hashing, guest preparation, and cleanup in the critical-path calculation.

The highest uncertainty is the number and duration of unique native build jobs after safe artifact reuse.
Guest batch duration and unavoidable process deadline cases are additional uncertainties.
If these costs exceed the budget, continue the structural refactor before the final CI run.
Do not claim five-minute feasibility from source inspection alone.

## Implementation sequence

1. Freeze the complete manifest and original assertion mapping.
2. Add structured command failures, phase events, and a bounded session workspace.
3. Add regression cases for storage exhaustion, worker failure, cancellation, and complete result accounting.
4. Build test executables and shared tools once through the session runner.
5. Replace the global build lock with per-key completion records and a shared resource scheduler.
6. Introduce immutable fixtures and refactor every applicable fixture call site.
7. Refactor runtime scenarios to share admitted inputs and identical compilation steps.
8. Convert Linux cases to guest batches with explicit isolation and dependent-case support.
9. Move all remaining Go, Python, Rust, and correctness acceptance groups into the session manifest.
10. Measure focused groups with all test-result caches disabled.
11. Correct coverage differences, resource excess, and critical-path overruns.
12. Connect `make ci` to the canonical session runner before final acceptance.
13. Run the final command only after the measured schedule predicts a total below 300 seconds.

Use failing public harness tests before each behavior change.
Use focused commands during each implementation step.
Do not invoke `make ci` as a baseline or intermediate diagnostic command.

## Preflight and final acceptance

Before the final `make ci` run, require all of the following results.

- Complete the manifest mapping for every original case and target requirement.
- Pass focused execution of every refactored group with fresh test results.
- Verify isolated environment changes, private artifact identities, and guest state cleanup.
- Pass injected disk exhaustion, cancellation, truncated result, and worker failure cases.
- Establish a resource-constrained schedule below 300 seconds, including the 45-second contingency.
- Verify shared build-slot leases for fixture builds and bounded workers for guest execution.
- Keep deliberate scheduler-constructor tests inside their assigned worker limits.
- Remove the old sequential execution route.

After preflight passes, run `make ci` once with fresh test results.
Measure from command entry through final cleanup.
Require all expected cases to pass, no unexpected skips, and total elapsed time below 300 seconds.
Require session storage within its measured budget and no remaining session-owned child process.
Report measured execution time separately from the earlier prediction.
If the final run fails, correct the failing group through focused commands before another final run.

## Implemented changes and focused evidence

The session runner now owns Go, Python, Rust, distribution, and semantic acceptance execution.
The frozen source inventory rejects removal of original test definitions and parameter expressions.
The result validator rejects missing cases, unexpected skips, undeclared deselections, and incomplete workers.
Tagged Metal coverage declares its required CGO configuration.
Ambient `CGO_ENABLED=0` cannot remove those cases from the manifest.
The CI path includes formatting, static analysis, the final compiler build, and cleanup.

The fixture coordinator shares verified backend inputs and immutable build results across worker processes.
An artifact lease protects an active build result from eviction.
The fixture registry limits its verified inputs and retained artifacts to 768 MiB.
The runtime object cache uses an accounted index with a 256 MiB limit.
Private mutation and compiler-admission cases retain their real public build boundaries.

Successful value fixtures use a distinct cache purpose.
This cache separates original program metadata from identical native function code.
JSON source hashes, paths, spans, and values remain specific to each source request.
Compiled value-fixture DWARF filenames use canonical names.
Default source-identity fixtures, compiler admission, and debugger checks keep their original build path.
Instruction, target, flag, and debug-column changes require a new code object.

Persistent Linux guests use private case filesystems and terminate descendants before the next case.
The semantic corpus retains its complete fixture set and exact source-identity inputs.
Its source payload decreased to approximately 61 MiB after removal of unrelated archive files.
Guest images use uncompressed `newc` transport.
Owned image files are removed after export.

The manifest declares the last test that uses each package output.
The runner removes those outputs only after that test succeeds.
The package producers execute concurrently with a maximum of three workers.
They share a declared Go build cache inside Lo.
Each producer still verifies its inputs and constructs a fresh package.

Distribution acceptance completes before Python package producer tests start.
The Linux semantic corpus starts independently with its retained expanded package.
Distribution acceptance uses archives and separate installed copies.
Each consumer keeps its inputs until its final use.

Verified immutable inputs with identical bytes share private hardlinks.
Each declared tool keeps its own path and command name.
The snapshot verifies each original source before it creates an alias.

Storage accounting counts each hardlinked file once.
The storage monitor includes positive growth in shared compiler caches and the declared runtime preparation directory.
Existing shared files remain available.
Each job declares its effective cache environment.
Quota regression fixtures use private caches for unrelated tools.

The final result includes the effective storage limit.
The runner stops its storage monitor before it publishes that result.

The runner validates cleanup paths inside the owned session directory before execution.
Linux package identity inputs remain available until the final semantic report completes.

| Focused check | Observed result | Evidence limit |
| --- | --- | --- |
| Shared pure cases and catalog | Eight groups completed in 19.48 seconds, compared with 55.81 seconds before parallel execution. | Earlier source revision and resource settings. |
| Native corpus | All 291 selected groups completed their assertions in 181.21 seconds. | Concurrent source changes invalidated the complete session report. |
| F034 descriptor cases | All 74 subcases passed. Darwin took 78.32 seconds, and Linux took 10.22 seconds. | Concurrent source changes invalidated the complete session report. |
| Distribution acceptance | The complete focused session passed in 100.18 seconds. Preparation included all three current packages. | This result excludes other test groups. |
| Distribution execution | Installed compiler acceptance completed in 41.24 seconds. | This interval excludes shared preparation. |
| Harness review regressions | The focused runner checks passed in 8.39 seconds. | This result establishes the tested boundaries, not total suite time. |
| Tooling and static analysis | The complete focused session passed with 619 results in 162.05 seconds. | Final review changes require a new source-matched measurement. |
| F036 cleanup cases | All 17 groups passed in 115.90 seconds. | This result excludes the remaining integration groups. |
| Complete integration execution | All integration groups passed their assertions in a 174.08-second focused session with 16 workers. | Concurrent harness changes invalidated the complete session identity. |
| Linux semantic execution | The complete Linux entry point passed in 123.91 seconds, including strict coverage validation. | Concurrent harness changes invalidated the complete session identity. A source-matched measurement is still required. |

The distribution session used ten workers, eight build slots, four guest workers, and a 6.5 GiB session limit.
Its source identity remained unchanged through cleanup.
The host is Darwin ARM64 with 18 CPUs and 64 GiB of memory.
Linux execution used QEMU HVF.
These measurements do not establish hosted CI results.

The runner uses short, unique worker paths to keep Unix socket paths within the host limit.
Python fixture directories belong to each worker and are removed after successful execution.
Long F034, F036, analytical, filebench, and selected cleanup test groups start before short integration groups.

The current schedule permits 16 host workers, eight backend builds, and four guest groups.
Host HTTP groups that share fixed ports execute one group at a time.
F036 fixtures share one immutable SDK helper within each test process.
Batched control commands retain every per-token kernel check and both cleanup boundaries.

A planning model from earlier measurements predicts approximately 250 seconds, including a 45-second margin.
This model does not replace passing measurements with the current source and resource settings.

The tagged Metal unavailable test exposed an executable-identity defect.
On this host, an injected library occurs before the executable in the dyld image list.
The runtime uses the linker's stable main-image symbol and retains its vnode identity checks.
The original injected-library assertion remains the regression.

A hardlink lookup can change the cached kernel pathname without moving the executable.
The runtime checks the invocation path against the mapped file identity.
After an executable rename, its current kernel path must match that same identity.
An injected library makes the hardlink regression deterministic.
The original protection and rename assertions remain unchanged.

The native and acceptance cohort passed with 905 results in 277.88 seconds.
This cohort includes the full Linux semantic corpus and the final coverage report.
The tooling and static cohort passed with 629 results in 179.27 seconds.
Both reports identify the same source revision.

Their combined schedule predicts 317.10 seconds, including the 45-second contingency.
This prediction exceeds the required limit.
The Linux corpus waited for 32.27 seconds of archive acceptance despite separate inputs.
The runner now starts these independent consumers concurrently.

After this change, the native cohort passed with 905 results in 229.36 seconds.
The tooling cohort passed with 630 results in 175.43 seconds.
The current forecast is 293.63 seconds, including the 45-second contingency.
All 965 canonical jobs have current passing measurements.
The initial 270-second design target remains separate from the required 300-second acceptance limit.

Use the `--describe --ci` output as the canonical schedule input.
Use passing `--report` outputs with the same source identity and concurrency settings for `preflight.py estimate`.
Require each report to declare a storage limit at or below the selected limit.
The estimator rejects missing group measurements, stale source identities, and different resource settings.
Use `--limit 300 --margin 45` for the required acceptance forecast.
Include the 45-second contingency before the final CI run.

The first full `make ci` run stopped at the 270-second session guard.
All execution groups except the benchmark-tool tests finished successfully.
No assertion failed before cancellation.
The benchmark-tool group took 232.42 seconds under the full load.
The focused forecast did not capture this contention.

Campaign fixtures calculated the same source identities for each lane.
Each fixture now calculates these identities once per target.
The public campaign fixture setup decreased from 1.93 to 0.64 seconds.
All eight focused success and injected-failure cases passed.
Production campaign verification remains unchanged.

After this fix, the native cohort passed in 219.40 seconds.
The tooling cohort passed in 140.27 seconds.
All 965 canonical jobs had passing measurements for the same source.
The accepted forecast was 279.37 seconds, including the 45-second contingency.

The final `make ci` run passed in 263.16 seconds by external wall-clock measurement.
The runner reported 263.06 seconds and 1,463 successful results.
The observed maximum storage was 8.47 GiB under the 10 GiB limit.
The source identity remained unchanged through cleanup.

This acceptance used prepared toolchains and warm build caches inside Lo.
Every test result was fresh.
The environments were Darwin ARM64 and Linux ARM64 under QEMU HVF.
This result does not establish hosted CI time or cold-cache time.
