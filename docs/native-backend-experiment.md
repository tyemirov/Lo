# Native Backend Experiment

I004 starts the [standalone compiler plan](standalone-compiler-plan.md).
This report separates backend smoke results from full Lo compiler acceptance.
The experiment does not replace the current generator or implement a new Lo runtime.

## Results On October 16, 2026

All four smoke cells passed object generation, linking, and execution.
Each executable returned status `0` and the exact expected output.

| Backend | Target | Object | Link | Execution | External link inputs |
| --- | --- | --- | --- | --- | --- |
| LLVM through Apple Clang | `darwin/arm64` | Passed | Passed | Passed | Apple SDK 27.0, Apple linker, and libSystem. |
| Cranelift | `darwin/arm64` | Passed | Passed | Passed | Apple SDK 27.0, Apple linker, and libSystem. |
| LLVM through `llc-14` | `linux/arm64` | Passed | Passed | Passed | Debian development libraries, GNU linker, and glibc. |
| Cranelift | `linux/arm64` | Passed | Passed | Passed | Debian development libraries, GNU linker, and glibc. |

The Linux image supplied Rust 1.85.1, Cranelift 0.116.1, and LLVM 14.0.6.
The Darwin host supplied Apple Clang 21.0.0.
The [receipt](evidence/native-backends/experiment.json) records exact tool, source, and image identities.
The selected image supplied no Go command or Go installation at its standard location.
The probes invoked no Go command.
The Darwin host still had development tools installed.

Linux object generation, linking, and execution used containers with no network interface route.
Cranelift also generated Darwin objects in that isolated Linux container.
Darwin LLVM generation, linking, and execution used the host network-denial sandbox.
A numeric socket probe returned `EPERM` under that sandbox.
The harness requires the isolation checks to pass before it reports complete success.

The [initial result](evidence/native-backends/experiment-initial.json) records the Linux LLVM invocation failure before correction.
The corrected command uses `llc-14` directly for object generation.
The [harness validation](evidence/native-backends/experiment-validation.json) records public CLI tests and their initial failure evidence.
Preparation now binds the image to the Dockerfile, Cranelift source, and lockfile digests.
The experiment rejects stale preparation before it invokes Docker.

LLD was absent from the prepared image and was not qualified.
Both successful link routes used their platform linkers.
The tests did not measure peak memory, source-level debugging, or complete distribution size.
Recorded phase times include container and process startup costs.
They do not establish a backend performance comparison.

## Probe Contract

The [LLVM fixture](../experiments/native-backends/probe.ll) and [Cranelift emitter](../experiments/native-backends/cranelift/src/main.rs) implement the same fixed-width operation.
For a nonnegative input `x`, the operation returns `3*x+2` and status `0`.
For a negative input, the operation returns value `0` and status `7`.
The [C caller](../experiments/native-backends/runtime.c) supplies inputs `7` and `-1`.
The independently expected output is:

```text
23 0
0 7
```

This test exercises arithmetic, a branch, object emission, a C ABI call, and a status result.
The caller also uses `printf`, which establishes a system-library link.
It does not establish arbitrary-precision integers, Lo errors, logical charges, or the complete runtime ABI.
The LLVM fixture is handwritten IR.
The Cranelift emitter uses its object API directly.
Neither probe consumes Lo source or its checked graph.

## Reproduction

The current harness needs Docker and the macOS development tools.
The Docker daemon must execute Linux arm64 containers.
Tool preparation requires network access.
The experiment uses network isolation after preparation.
The pinned experiment versions are not production toolchain selections.

1. Record `_scratch/native-backends-20261016` in the task execution plan before the experiment.
2. Run the public harness checks:

   ```bash
   make check-native-backend-probe
   make test-native-backend-probe
   ```

3. Prepare the declared tools and locked dependencies:

   ```bash
   make prepare-native-backend-probe
   ```

4. Run object generation, linking, and execution without network access:

   ```bash
   make experiment-native-backends
   ```

5. Read the [machine-readable receipt](evidence/native-backends/experiment.json).
6. Preserve useful evidence before removal of the task-owned scratch directory.

The preparation command is separate from the experiment command.
A missing preparation receipt stops the experiment with a diagnostic.
Changed preparation sources require an explicit new preparation command.
The experiment does not download missing tools or change dependencies during validation.
Commands use `CI=1` and closed standard input.

The base image, direct LLVM package versions, and Cargo lockfile are pinned.
The receipt records transitive Debian packages without a repository snapshot pin.
Reuse of the recorded local image preserves those installed inputs.
A fresh image build can select different transitive Debian packages.

## Evidence Boundaries

Each matrix cell records object generation, linking, execution, isolation, and external inputs separately.
The receipt contains exact commands, source digests, tool identities, and observed output.
The release acceptance procedure remains in the [plan](standalone-compiler-plan.md#offline-acceptance-procedure).

Linux commands use a prepared development container with `--network=none`.
This container supplies compiler tools and libc development files.
It does not establish a minimal installed Lo distribution or SDK-free compilation.

Darwin commands use the available development host and a network-denial sandbox.
They can use the installed Apple SDK and system libraries.
This establishes offline command execution, not absence of the SDK or host tools.
Filtering `PATH` does not establish a clean environment.

The [current compiler baseline](evidence/native-backends/current-lo-no-go.json) records an independent missing-Go check.
The existing `lo build` command returned status `2` with `build.toolchain` when `PATH` excluded Go.
That check confirms the current dependency lookup.
It does not establish an operating system without Go installed.

## Remaining Acceptance Work

- Connect the selected backend to Lo native lowering.
- Implement the complete runtime and its semantic corpus.
- Qualify LLD and every platform link input in the selected distribution.
- Establish distributable macOS link inputs without an installed SDK.
- Select minimum OS versions and the complete host-to-target matrix.
- Compile in clean environments with empty caches and no undeclared tools.
- Execute copied artifacts in separate clean environments without Lo or compiler tools.
- Measure peak memory, distribution size, debug behavior, and representative workload performance.
- Qualify Metal framework links and the existing numeric profile independently.

I004 remains open until its bounded experiment criteria pass.
Its closure does not require the complete runtime or production distribution.
P005 selects the production backend after that evidence is available.
F017 and I011 own distribution implementation and standalone compiler acceptance.

## Repository Validation

The [review record](evidence/native-backends/review.json) records issue dependencies, agent assignments, architecture review, and repository checks.
The [documentation review](evidence/native-backends/documentation-review.json) records the reviewed scope and preserved language findings.
The changed prose has no new mechanical findings.
Existing findings in unchanged root guidance, tracker text, and README text remain outside this task.
