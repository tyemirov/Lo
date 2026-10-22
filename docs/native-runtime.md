# LLVM Runtime Development

I007 supplies the core runtime.
I008 supplies CPU tensors, differentiation, optimizers, and checkpoints.
I009 adds file, process, and HTTP adapters.
I010 adds native device resources and Metal execution.
The [runtime contract](native-runtime-contract.md) defines their ABI and semantic requirements.
The [compiler plan](standalone-compiler-plan.md) defines the remaining migration.
The [validation receipt](evidence/native-runtime/validation.json) records test results, runtime identities, and qualification limits.
The [adapter receipt](evidence/native-adapters/validation.json) records I009 validation.

All executable builds use LLVM and a static Rust runtime.
The compiler no longer generates Go source.
The [installed compiler](compiler-distribution.md) uses LLVM from its own package.
An LLVM build does not select another backend when compilation fails.

## Supported Scope

The development distribution builds `darwin/arm64` and `linux/arm64` executables from a Darwin arm64 host.
The runtime implements core values, CPU FP32 operations, Parameters, gradients, SGD, AdamW, and canonical checkpoints.
The file adapter implements reads, writes, appends, directory observations, open-file probes, and ordered removal.
File access requires an explicit root grant.
Target construction does not activate an operation.

The [adapter guide](native-adapters.md) defines capability selection, process availability, service lifetime, and adapter verification.
The [device guide](native-devices.md) defines native device resources, Metal build selection, and software verification.
F017 supplies the complete installed distribution, including the Linux compiler host.
I011 owns language qualification after removal of Go generation.

Current corpus, offline, component, and CI checks passed.
The [nonperformance receipt](evidence/standalone-compiler/nonperformance-validation.json) records those results and their source identity.
Performance qualification and final review remain pending.
These development commands do not close the installed-distribution acceptance criterion.

## Prepare And Build

Preparation requires network access for pinned tool archives.
The runtime build uses the repository dependency sources with Cargo offline and frozen options.
The pinned C compiler builds the process transport and ring with headers from Lo.
Darwin link inputs include system, Security, and CoreFoundation symbol descriptions.
Cargo receives only the declared build environment.
Preparation rejects compiler flag, wrapper, and profile overrides from the caller.
The build receipt records the runtime source digest and the declared environment.
Make keeps caches and temporary files inside Lo.

1. Prepare the pinned Rust, LLVM, platform, and software guest inputs:

   ```sh
   make prepare-native-runtime NATIVE_RUNTIME_STATE=_scratch/native-runtime </dev/null
   ```

2. Build both runtime archives and the development distribution:

   ```sh
   make build-native-runtime NATIVE_RUNTIME_STATE=_scratch/native-runtime </dev/null
   ```

3. Build the development compiler:

   ```sh
   make build </dev/null
   ```

4. Create the development build directory:

   ```sh
   mkdir -p _scratch/native-runtime/build-state
   ```

5. Compile an executable with the selected toolchain:

   ```sh
   CI=1 LO_BUILD_ROOT="$PWD/_scratch/native-runtime/build-state" \
     bin/lo build examples/xor_cpu.lo \
     --toolchain _scratch/native-runtime/distribution/toolchain.json \
     --output bin/xor-llvm \
     --format json </dev/null
   ```

6. Execute the program:

   ```sh
   bin/xor-llvm run --format json </dev/null
   ```

Add `--target linux/arm64` to compile for the Linux target.
The pinned software guest can execute that artifact:

```sh
python3 -B tools/nativeruntime/run.py \
  --preparation _scratch/native-runtime \
  bin/xor-linux run --format json </dev/null
```

An explicit `--toolchain` input and an absolute `LO_BUILD_ROOT` are necessary for a development compiler.
The installed compiler selects its packaged manifest.
Both routes reject the removed `--codegen` option.

The artifact needs its declared operating-system libraries.
It does not need Lo, the source file, Go, Rust, or compiler tools.

## Build Admission

The compiler validates the distribution ABI, host, target, and tool versions.
It takes private copies of the declared inputs and verifies their SHA-256 digests.
Compilation and linking consume those verified copies.
The compiler owns the link recipes.
It rejects undeclared link inputs and arbitrary linker arguments.

The compiler invokes the selected tools with an empty `PATH`.
It performs no dependency retrieval during the build.
It publishes the executable only after compilation, linking, and cleanup succeed.
A failed build preserves the previous output.
The receipt identifies the code generator, ABI, and runtime archive.

## Numerical Verification

The following checks use real LLVM executables:

| Coverage | Required result |
| --- | --- |
| Analytical derivatives and finite differences | Preserve gradients for shared nodes, broadcast, matrix products, reductions, and views. |
| CPU training | Preserve the existing 200-step loss and Parameter expectations. |
| XOR | Execute 500 updates and classify all four inputs correctly. |
| Repeat execution | Preserve identical results across repeated runs within one numeric profile. |
| Checkpoint resumption | Match uninterrupted training after separate save, resume, and inference processes. |
| Canonical bytes | Match independent SGD and AdamW checkpoint expectations. |
| Invalid checkpoints | Preserve typed errors, source context, and prevention of later effects. |
| Limits and cancellation | Preserve logical charges, host failures, and resource cleanup. |
| Retained contexts | Keep storage bounded while preserving retained result handles and shared tensor storage. |

The earlier I008 checks include all 13 analytical derivative and finite-difference fixtures on Darwin.
That snapshot passed CPU training, XOR, and numerical resource tests on both targets.
Linux checkpoint tests use eight separate guest processes.
Their SGD and AdamW results include complete optimizer state.

Run focused groups separately:

```sh
make test-native NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestCPUTrainingExample|TestXORExamples|TestCheckpointXOR/cpu' -count=1" </dev/null

make test-native NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run TestAutodiff -count=1" </dev/null

make test-native NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestTensor|TestNativeTensorBudgetDifferential' -count=1" </dev/null

make test-native NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestLLVM|TestParameters|TestOptimizer|TestCheckpointFreshProcess|TestSemanticMigrationCheckpointResume' -count=1" </dev/null
```

The Linux checks use QEMU without a physical-device requirement.
Tests that directly inject Go evaluator dependencies do not establish LLVM runtime behavior.
The native ABI tests supply separate evidence for numerical cancellation, storage ownership, and failure cleanup.

## Metal Assets

Run `make build-metal </dev/null` before `make build-native-runtime-metal` or package construction.
The producer creates `runtime/metal/metal.metallib` and `runtime/metal/manifest.json`.
The manifest identifies the kernel, adapter, producer, and embed source files.
The native runtime producer checks these files against `bin/lo-metal-build.json`.

It reads the kernel library directly, without a Go source archive.
Software device models and injected failures supply device acceptance evidence.
No physical device is necessary for any check.

## Runtime Structure

The compiler lowers each checked derivative plan into a dedicated native function.
The generated function preserves forward evaluation, reverse accumulation, source frames, charges, and release points.
The runtime tape stores values and adjoints for that function.
It does not interpret Lo source.

Immutable values use reference counting.
Resource scopes account for tensor storage independently of logical value charges.
The ABI rejects tensor handles from another context or a released scope.
Retained values can outlive their context.
Their storage cannot enter another context without the required transfer contract.

Repeated context entry releases root storage that has no remaining value reference.
It preserves storage reachable through retained results, aliases, and optimizer states.
Each request keeps its cumulative logical budget.

Malformed metadata must fail admission before execution.
Terminal host failures bypass Lo handlers.
Cleanup preserves the primary error and adds separate cleanup evidence.
