# Offline Compiler Distribution

F017 supplies the installed compiler for Darwin arm64 and Linux arm64.
The installed `lo build` command uses LLVM and LLD from its own package.
No Go, Rust, C compiler, developer SDK, or network access is necessary for installed compilation.
The [native runtime contract](native-runtime-contract.md) owns the ABI, target rules, and operating-system dependencies.
Development compilation also uses LLVM, without Go source generation.

I011 passed the current corpus, offline, component, and CI checks.
Performance qualification and final review remain pending.

## Supported Matrix

| Compiler host | Software targets | Metal target |
| --- | --- | --- |
| macOS 27.0.1, Darwin arm64 | `darwin/arm64`, `linux/arm64` | `darwin/arm64`, with the Darwin Metal package |
| Linux 6.12.110, glibc 2.36, arm64 | `darwin/arm64`, `linux/arm64` | None |

The Darwin deployment target is macOS 27.0.
The Darwin acceptance environment uses macOS 27.0.1.
Darwin executables use libSystem, Security, and CoreFoundation from the operating system.
Metal executables also use Metal and Foundation.

Linux executables use the operating-system loader, libc, libm, and `libgcc_s.so.1`.
The Linux loader path is `/lib/ld-linux-aarch64.so.1`.
For programs that call `File.open_paths`, `lsof` must be available on the execution host.
The [adapter guide](native-adapters.md) defines application capabilities and process availability.

The Linux compiler package supplies its other shared libraries:

- `libstdc++.so.6`
- `libgcc_s.so.1`
- `libxml2.so.2`
- `libicuuc.so.72`
- `libicudata.so.72`
- `liblzma.so.5`
- `libz.so.1`

Each tool execution uses verified copies of these libraries in the private build directory.
The loader receives an explicit library path and does not use its library cache.
Compiler tools do not use PATH lookup.

## Package Contents

Each archive has one directory with these contents:

| Path | Purpose |
| --- | --- |
| `bin/lo` | Prebuilt frontend with the package manifest digest. |
| `bin/clang`, `bin/ld.lld`, `bin/ld64.lld` | LLVM 21.1.8 and LLD 21.1.8. |
| `toolchain.json` | Host identity, target recipes, paths, and input digests. |
| `runtime/` | Precompiled static Lo runtimes. |
| `platform/` | Startup objects, support libraries, and platform descriptions. |
| `host/lib/` | Additional libraries for Linux compiler tools. |
| `inventory.json` | File names and SHA-256 digests. |
| `provenance.json` | Producer identities, source records, and component notices. |
| `notices/` | License and copyright notices. |
| `source/` | Lo source, vendored runtime source, and required Debian source archives. |

The software packages contain both software targets.
The Darwin Metal package also contains the Metal runtime, adapter, and precompiled kernels.
QEMU and acceptance helpers are test tools outside the product packages.

## Install And Compile

1. Select the archive for the compiler host.
2. Compare its SHA-256 with the adjacent `.receipt.json` file.
3. Extract the archive into the installation directory.
4. Create an empty directory for build state.
5. Set `LO_BUILD_ROOT` to its absolute path without symlink traversal.
6. Run the installed compiler with the source and target.

This example uses a Darwin software installation and an existing source file:

```sh
mkdir -p install build-state
tar -xf lo-darwin-arm64-software.tar -C install
CI=1 PATH= LO_BUILD_ROOT="$PWD/build-state" \
  "$PWD/install/lo-darwin-arm64-software/bin/lo" build program.lo \
  --target darwin/arm64 --output "$PWD/program" --format json </dev/null
"$PWD/program" run --format json </dev/null
```

The Linux package uses the same CLI.
Either software package can compile for either software target.
The resulting executable runs on its declared target without Lo or the source file.
It retains the current `run` and `serve` commands and capability requirements.

The installation can move without a rebuild.
The frontend resolves its actual executable location and verifies its own `toolchain.json`.
A missing manifest, changed manifest, or changed selected input causes a build error.
The compiler keeps an existing output after admission failure, tool failure, or cancellation.
Successful builds replace the output as one filesystem operation.

For Metal, use `lo-darwin-arm64-metal` and add `--device-backend metal`.
The software package cannot select Metal.
The Metal package cannot compile a Metal executable for Linux.

## Construct Packages

Package construction uses development tools inside the declared producer workflow.
Installed compilation does not use those producer tools.
All repository commands keep their state under Lo's `_scratch/` directory.

1. Prepare the pinned runtime inputs with `make prepare-native-runtime`.
2. Prepare the pinned package inputs with `make prepare-compiler-distribution`.
3. Produce the Metal assets with `make build-metal`.
4. Construct the packages with `make package-compiler-distributions`.
5. Run the tool checks with `make test-compiler-distribution-tools`.
6. Run installed acceptance with `make test-compiler-distributions`.

The Metal producer creates `runtime/metal/metal.metallib` and `runtime/metal/manifest.json`.
The runtime producer checks their source digests against `bin/lo-metal-build.json`.
Run `make build-metal` before `make build-native-runtime-metal` or package construction.
The producer reads kernel library files, without a Go source archive.

The preparation commands permit explicit downloads.
Package construction and installed compilation do not download inputs.
Construction rejects missing, changed, or incompatible prepared inputs.
It records the frontend producer, runtime source identities, input digests, and corresponding source materials.
Children receive `CI=1` and closed standard input.

Package output defaults to `_scratch/compiler-distribution/packages/`.
Acceptance output defaults to `_scratch/compiler-distribution/acceptance/`.
Existing package or acceptance output causes an error.
For another run, select new paths with `COMPILER_DISTRIBUTION_OUTPUT` and `COMPILER_DISTRIBUTION_ACCEPTANCE`.

## Acceptance Environments

Darwin acceptance uses an operating-system sandbox with an empty PATH and new state directories.
The sandbox restricts access to the package, input source, output workspace, and declared operating-system libraries.
A guard verifies the filesystem and network restrictions for the compiler and its child processes.
Artifact execution uses another sandbox that denies access to the package and source.

Linux acceptance uses QEMU TCG with no network interface.
The compiler guest contains the installed package, one source file, base libraries, and the test initializer.
It contains no external Go, Rust, C compiler, shell, or developer SDK.
Another guest executes a copy of the artifact without the compiler package or source.
The guest export channel uses an isolated virtio port.
The kernel console keeps its diagnostics outside artifact and result records.
An absent or malformed export stops acceptance before artifact publication.

The acceptance command checks all four host-to-target software combinations.
It also checks installation relocation, input corruption, prior output preservation, and empty build directories after failure.
The numerical checks include backpropagation and 500-step XOR training on both compiler hosts.
The Darwin Metal package has independent build, execution, and kernel admission checks.
Software device tests provide Metal semantic evidence without a physical-device requirement.
Physical Metal execution and performance remain unmeasured.

The [distribution receipt](evidence/compiler-distribution/validation.json) records the earlier F017 results and scope.
It does not replace I011 qualification after the compiler changes.
Keep I011 pending until the qualification checks pass.
The [current nonperformance receipt](evidence/standalone-compiler/nonperformance-validation.json) records the repeated checks after Go generation was removed.
The [semantic migration contract](semantic-migration.md) defines the I011 corpus requirements.
