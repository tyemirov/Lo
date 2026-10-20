# LLVM Runtime Development

I007 supplies the core runtime.
I008 supplies CPU tensors, differentiation, optimizers, and checkpoints.
The [runtime contract](native-runtime-contract.md) defines their ABI and semantic requirements.
The [compiler plan](standalone-compiler-plan.md) defines the remaining migration.
The [validation receipt](evidence/native-runtime/validation.json) records test results, runtime identities, and qualification limits.

The explicit LLVM build route uses native code and a static Rust runtime.
The default build route still uses Go until I011.
An LLVM build does not select another backend when compilation fails.

## Supported Scope

The development distribution builds `darwin/arm64` and `linux/arm64` executables from a Darwin arm64 host.
The runtime implements core values, CPU FP32 operations, Parameters, gradients, SGD, AdamW, and canonical checkpoints.
`File.read` and `File.Write.new` supply the confined file operations required by checkpoint workflows.
File access requires an explicit root grant.
Target construction does not activate a write.

I009 owns the remaining file, process, and HTTP operations.
The checkpoint cancellation tests use open pipe descriptors with a connected peer.
Cancellation while a write waits for a pipe reader remains unqualified under I009.
I010 owns Metal and device execution.
F017 owns the complete installed distribution, including the Linux compiler host.
I011 owns full language qualification and removal of Go generation.
These development commands do not close the installed-distribution acceptance criterion.

## Prepare And Build

Preparation requires network access for pinned tool archives.
The runtime build uses the repository dependency sources with Cargo offline and frozen options.
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

4. Compile an executable through the explicit LLVM route:

   ```sh
   bin/lo build examples/xor_cpu.lo \
     --codegen llvm \
     --toolchain _scratch/native-runtime/distribution/toolchain.json \
     --output bin/xor-llvm \
     --format json </dev/null
   ```

5. Execute the program:

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

The Darwin checks include all 13 analytical derivative and finite-difference fixtures.
Both targets pass CPU training, XOR, and numerical resource tests.
Linux checkpoint tests use eight separate guest processes.
Their SGD and AdamW results include complete optimizer state.

Run focused groups separately:

```sh
make test-native-llvm NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestCPUTrainingExample|TestXORExamples|TestCheckpointXOR/cpu' -count=1" </dev/null

make test-native-llvm NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run TestAutodiff -count=1" </dev/null

make test-native-llvm NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestTensor|TestNativeTensorBudgetDifferential' -count=1" </dev/null

make test-native-llvm NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestLLVM|TestParameters|TestOptimizer|TestCheckpointFreshProcess|TestSemanticMigrationCheckpointResume' -count=1" </dev/null
```

The Linux checks use QEMU without a physical-device requirement.
Tests that directly inject Go evaluator dependencies do not establish LLVM runtime behavior.
The native ABI tests supply separate evidence for numerical cancellation, storage ownership, and failure cleanup.

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
