# Self-Contained Compiler Implementation Design

P008 selects this design for the [self-contained compiler requirement](self-contained-compiler.md).
The design is selected. The implementation and executable acceptance remain open under F037.
The compiler can use any implementation language. No compiler rewrite in Lo or compiler self-rebuild is required.

## Selected Architecture

Keep the Go frontend, checked graph, native lowering, and LLVM emitter.
Keep LLVM 21.1.8, LLD 21.1.8, and the `lo-native-3` Rust runtime ABI.
Embed the complete host-specific compiler payload into each `lo` executable at producer time.
The payload contains backend executables, runtime archives, target link inputs, identities, and necessary non-OS host libraries.

During `lo build`, expand the selected payload members into a new private build workspace.
Execute only those verified backend images with the current explicit compile and link recipes.
Remove the workspace after tool cleanup and before success publication.
No companion installation, external toolchain, persistent cache, or network service participates in compilation.

This design selects temporary execution of embedded backend images, as permitted by the requirement.
The delivered compiler is one executable. Temporary backend files originate entirely from that executable.
Users do not install Clang, LLD, Rust, Go, or a separate Lo runtime.

| Alternative | Decision and reason |
| --- | --- |
| Embedded backend images with private materialization | Selected. Reuses qualified native operations, explicit tool arguments, and process cancellation boundaries. |
| Static LLVM and LLD library integration | Not selected. Adds a C++ boundary, producer changes, and different failure and cancellation ownership. |
| New machine-code generator and linker | Not selected. Requires new target, debug, numeric, and executable-format qualification. |
| Directory with companion compiler tools | Rejected. Does not satisfy executable-only installation. |

The [Go embed contract](https://go.dev/pkg/embed/) supplies file contents at compile time and exposes them through `embed.FS`.
The [Clang guide](https://releases.llvm.org/21.1.0/tools/clang/docs/CommandGuide/clang.html) describes object generation and the integrated assembler.
The current repository [builder](../internal/nativebuild/llvm_executable.go) already invokes the pinned compiler and linkers with explicit inputs.
These sources establish design mechanisms, not acceptance of the proposed artifact.

## Source Audit And Change Ownership

| Current source | Finding | Implementation owner |
| --- | --- | --- |
| [Frontend dispatch](../cmd/lo/main.go) | Resolves an adjacent `toolchain.json` or a caller-selected manifest. | F040 replaces both routes with the embedded identity. |
| [Native builder](../internal/nativebuild/llvm_executable.go) | Copies and verifies external tools, archives, and platform files before execution. | F039 supplies equivalent verified paths from embedded resources. |
| [Input snapshots](../internal/nativebuild/llvm_snapshot.go) | Validates the private bytes consumed by the build. | F039 preserves this invariant for expanded resources. |
| [Package producer](../tools/lodistribution/package.py) | Builds the Go frontend and copies a complete directory of runtime dependencies. | F038 produces the embedded payload. F041 replaces executable delivery. |
| [Prepared input validation](../tools/lodistribution/prepare.py) | Checks tool pins, source identities, and transitive ELF dependencies. | F038 reuses these producer checks. |
| [Native runtime producer](../tools/nativeruntime/build.py) | Builds Rust archives and target platform inputs. | Remains a producer dependency only. |
| [Distribution acceptance](../tools/lodistribution/acceptance.py) | Admits the complete package directory in isolation. | F041 admits only the final executable and explicit test inputs. |

No language extension is necessary for this architecture.
The Go frontend and Rust runtime remain implementation components.
Producer tools can remain separate because they do not participate after the final compiler build.
P009 selects the [single-runtime cleanup](single-runtime-plan.md) in I014 through I018.
I016 relocates shared native host assets and updates their source inventories.
Payload producers must consume those canonical inputs and current runtime identities.
Compiler packaging and the single-runtime transition have separate acceptance gates.

## Host, Target, And Variant Matrix

Keep the current host and target matrix and its OS baselines.
Each delivered executable contains one host's tools and both software targets' runtime and platform inputs.

| Executable variant | Compiler host baseline | Native targets |
| --- | --- | --- |
| Darwin software | macOS 27.0.1, arm64 | Darwin arm64 and Linux arm64 software |
| Linux software | Linux 6.12.110, glibc 2.36, arm64 | Darwin arm64 and Linux arm64 software |
| Darwin Metal | macOS 27.0.1, arm64 | Both software targets and Darwin arm64 Metal |

Keep the Darwin deployment target at macOS 27.0.
Reject Metal requests on Linux and in software-only compiler variants.
No automatic backend or variant substitution is permitted.

### Compiler Host Dependencies

The host dependency inventory applies to the compiler and every backend process.
It is separate from the libraries used by generated applications.

| Host | External OS components | Embedded non-OS components |
| --- | --- | --- |
| Darwin arm64 | `/usr/lib/dyld`, system shared-cache resolution, `libSystem.B.dylib`, `libresolv.9.dylib`, `libc++.1.dylib`, `libz.1.dylib`, `libxml2.2.dylib`, CoreFoundation, Security | Pinned Clang and LLD images |
| Linux arm64 | `/lib/ld-linux-aarch64.so.1`, `libc.so.6`, `libm.so.6` | Pinned Clang, LLD, `libstdc++.so.6`, `libgcc_s.so.1`, `libxml2.so.2`, `libicuuc.so.72`, `libicudata.so.72`, `liblzma.so.5`, `libz.so.1` |

The [platform record](../experiments/native-runtime-abi/platform-inputs.json) identifies the Darwin system imports and OS baselines.
The [distribution lock](../tools/lodistribution/inputs.lock.json) owns exact tool digests, Linux library digests, and the ELF dependency graph.
F038 must add the Darwin frontend and tool import inventories to the canonical dependency lock.
The existing frontend also imports libresolv, CoreFoundation, and Security, beyond the LLVM tool imports.
Preserve OS-owned transitive dependencies of the declared system libraries.
Record their resolution under the selected OS baseline. Do not admit arbitrary files under system or installation directories.

Build the frontend with the standard Go build and `CGO_ENABLED=0` for these compiler variants.
The Metal variant supplies native Metal link inputs and kernels without adding a Metal interpreter to the compiler frontend.
Audit the resulting frontend imports as well as the embedded tools.

Linux backend execution uses the declared OS loader with an explicit private library directory and `--inhibit-cache`.
Clear caller loader overrides, compiler flags, SDK variables, and resource-directory variables from child environments.
Darwin tools retain their pinned bytes and declared system imports.
Their relative library search paths must resolve only within the invocation workspace or to declared OS components.

### Target Inputs

Embed the exact inputs selected by the existing native link recipes:

- Darwin software: static Lo runtime, `libSystem.tbd`, `Security.tbd`, and `CoreFoundation.tbd`.
- Linux software: static Lo runtime, `crt1.o`, `crti.o`, `crtn.o`, libc, libm, GCC support, and `libc_nonshared.a`.
- Darwin Metal: Metal runtime archive, verified kernels and identity, Foundation, Metal, Objective-C, and software framework descriptions.

Use repository-authored Darwin descriptions. Do not require an installed Apple SDK.
Keep application loader and library contracts from the [native runtime contract](native-runtime-contract.md).
Compiler independence does not grant application capabilities or remove declared application dependencies such as the `File.open_paths` probe.

## Payload Representation And Producer

Add a production payload package under `internal/nativebundle`.
Use `embed.FS` for an index and gzip-compressed content blobs generated beneath that package.
The generated `payload/` directory is ignored by Git and contains build inputs, not a second checkout.
Keep producer temporary files, receipts, and tool caches under `_scratch/`.
Publish the generated directory only after validation, with one producer owning that directory during frontend construction.

Use one canonical index with these closed fields:

| Field | Meaning |
| --- | --- |
| `abi`, `llvm`, `host`, `variant` | Exact runtime ABI, backend identity, compiler host, and product variant |
| `members` | Logical member name, role, raw length, raw SHA-256, compressed length, and compressed SHA-256 |
| `targets` | Target triples and member identities for runtimes, link inputs, and optional Metal data |
| `host_libraries` | Exact non-OS host library member identities |
| `os_dependencies` | Declared host loader and system library inventory |
| `source_identity`, `producer_identity` | Canonical source and pinned producer inputs used to construct this payload |

Reject unknown fields, duplicate names, absolute paths, traversal, symbolic links, and undeclared members at the producer boundary.
Keep link arguments in compiler code. The index cannot introduce link flags, search paths, or commands.
Generate a Go constant with the SHA-256 of the canonical index and bind it into the frontend build.
The index is an internal build input and has no external override.

Store one blob per raw digest. Both LLD driver names can reference the same pinned image.
Sort member names, fix gzip metadata, and use one pinned compression implementation at level 6.
Use the Go toolchain selected by `go.mod` for the payload generator and frontend construction.
An identical member inventory must produce identical index and blob bytes.

Select these payload admission limits: 1 MiB index, 1,024 members, and 1 GiB for each total compressed and raw payload size.
Check sums for overflow before allocation or extraction.
The producer rejects excess sizes. It does not omit a target or substitute a smaller backend.
These limits govern the compiler payload, independently of the existing 64 MiB generated-program limit.

Reuse pinned native preparation, runtime provenance, source snapshots, notices, and corresponding-source records.
Embed notices and component inventory with the compiler and expose them through `lo build --toolchain-info`.
This planned command reports embedded identities and dependencies without materializing backend images.
F040 must dispatch this command before source reads, with no source or output argument required.
Source and release records remain producer artifacts. Normal compilation never reads them.
Preserve existing redistribution obligations in the release materials without making those materials execution dependencies.

Fresh payload production must reject stale ABI or source identities.
Existing local `lo-native-1` packages are inventory evidence only. Current source requires `lo-native-3`.
Do not use those packages as implementation acceptance or copy their runtime bytes into a new release.

## Runtime Interfaces And Ownership

`internal/nativebundle` owns immutable payload admission and host, target, and variant selection.
Its constructor returns an admitted bundle or a contextual `build.toolchain` diagnostic.
No invalid zero-value bundle is usable.
Internal tests can inject a bundle resource reader. Production uses only the embedded resources.

`Bundle.Materialize(ctx, workspace, selection)` returns an owned selection of verified tool, library, runtime, and platform paths.
The build workspace owner controls its lifetime and cleanup.
`internal/nativebuild` consumes this admitted selection and does not repeat manifest parsing.
Path validation and byte verification remain boundary operations.

1. Validate the embedded index identity and selected host, target, variant, and ABI before output publication.
2. Create a new invocation directory under the existing `LO_BUILD_ROOT` contract.
3. Select only the necessary backend images, host libraries, runtime, and platform inputs.
4. Stream decompression with a fixed 64 KiB buffer and cancellation checks.
5. Enforce compressed and raw lengths and hashes. Reject truncation, trailing bytes, and concatenated gzip streams.
6. Verify all selected members before executing any backend image.
7. Materialize aliases only inside this invocation directory.
8. Lower the checked graph and emit LLVM through the existing Go implementation.
9. Compile objects and link the native executable with the verified private paths.
10. Join owned backend processes and complete workspace cleanup before reporting successful publication.

Use `embed.FS.Open` and streaming reads. Do not copy the entire payload into heap memory.
Do not reuse a materialized tool directory between invocations or trust a caller-created cache.
Concurrent builds own different directories and cannot remove another build's files.
Keep embedded resources and backend diagnostic files inside the invocation workspace, including crash output.

The existing 180-second build deadline includes payload admission and expansion.
Preserve the 1 MiB tool diagnostic limit and current lowering, module, and output limits.
Report payload corruption as `build.toolchain`, cancellation as `build.cancelled`, and resource bounds as `build.limit`.
Report workspace or publication failures as `build.output`.
Do not increase timeouts to compensate for extraction or silently continue after cleanup failure.

Backend children receive an explicit environment with empty PATH and workspace-local temporary directories.
Use Clang's integrated compiler and assembler for LLVM input and invoke LLD directly for the final link.
Disable default Clang configuration discovery and qualify the exact driver plan against the pinned tools.
Use `--no-default-config`, `-fintegrated-cc1`, `-integrated-as`, and `-fno-crash-diagnostics`.
Set the resource directory to an invocation-local path. Reject undeclared resource reads during acceptance.
No host assembler, shell, SDK discovery, plugin, or unrecorded executable may participate.
Cancellation must terminate and join all owned backend processes before workspace removal.
Verify parent termination and child cleanup with controlled fixtures.

Parent termination alone does not establish complete cleanup.
An uncatchable termination can leave an invocation directory. It cannot become a trusted cache on a later invocation.

Keep staged output and atomic replacement in the existing publication code.
Keep the staged artifact and linker output directory beside the destination, outside the invocation directory.
The build owns all three paths, including when `LO_BUILD_ROOT` and the destination use different filesystems.
After backend processes exit, stage the artifact and remove the linker output directory.
Remove the invocation directory, recheck cancellation, then atomically replace the destination.
Remove staged output on failure. Report cleanup failures with the owned path.

Join and cleanup failures must preserve the previous destination and cannot produce a success receipt.
Receipts retain ABI, runtime, and target identities and add compiler and embedded payload identities.
Semantic identity must bind the actual payload consumed by compilation.

## Product Integration And Forward Transition

Make the embedded payload the only production input channel for native compilation.
Remove installed-directory discovery, `installedToolchainSHA256`, and the external `--toolchain` build route at F040 integration.
Reject the removed option. Do not retain aliases, dual reads, or a development fallback.
`lo build --emit llvm` continues to emit LLVM directly without extracting backend images.
Its output remains an intermediate module, not a completed native executable.

F038 and F039 first add producer and resource components without claiming a second production backend.
F040 switches the builder, CLI, and integration fixtures together.
Development tests construct the selected embedded payload from explicit prepared inputs before executable builds.
No ordinary build or test command downloads missing inputs.
Missing prepared inputs fail preparation with their exact paths and identities.

F041 changes distribution construction to emit one compiler executable per host and variant.
It updates the Make targets, semantic identity tooling, installed acceptance, and user instructions together.
Retain the current local producer commands where they remain necessary to create the payload.
Remove external package manifests and directory-layout assumptions from runtime consumers and acceptance fixtures.

## Acceptance And Evidence

| Gate | Required observation |
| --- | --- |
| Public characterization | Existing compiler fails when only its executable and Lo input are admitted. Preserve the expected failure. |
| Producer | Complete pinned closure, deterministic payload, and rejection of stale ABI, altered bytes, or incomplete dependencies. |
| Materialization | Corruption, overflow, cancellation, concurrent builds, cleanup failures, and alias isolation preserve ownership and output. |
| Clean native build | One compiler executable compiles and links with only declared OS components, source, and output workspace. |
| Dependency denial | Hidden package files, producer tools, external libraries, SDKs, caches, and network access are inaccessible. |
| Relocation | Copying the executable to another directory does not change required inputs. |
| Environment isolation | Hostile PATH, loader variables, SDK settings, and compiler flags cannot select external components. |
| Darwin tool execution | Materialized backend images and produced executables run without external signing, assembler, or SDK tools. |
| Native execution | Copied output runs in another environment without the compiler or Lo source. |
| Semantic coverage | Complete current corpus, diagnostics, authority, limits, HTTP, process, numerical, and checkpoint behavior retain independent expectations. |
| Matrix | All seven compiler-variant and target pairs in the selected matrix pass their declared software checks. |

Use the existing Darwin sandbox and Linux QEMU TCG infrastructure with new executable-only admission rules.
Qualify both software targets from each software compiler variant.
Also qualify both software targets and the Metal target from the Darwin Metal compiler variant.
Include OS services and loader shared-cache access required by the declared baseline, but no developer tools.
Test guards must demonstrate denied external reads, network operations, and executable launches.
Bind every allowed temporary executable and non-OS library to an embedded member digest.
Audit child processes and library loads. Empty PATH alone is insufficient.
Record compiler hash, payload hash, source identity, target, variant, environment, executed image hashes, and final cleanup result.

Report compressed executable size, expanded disk use, extraction time, peak memory, and cold native build time.
Preserve existing compiler performance thresholds and measurement procedures.
Size and timing records from historical packages are estimates, not performance acceptance for this design.
F037 closes only after fresh implementation checks, necessary CI, isolated matrix acceptance, and independent review pass.

### Design Evidence

The November 1, 2026 source audit examined the frontend, native builder, runtime producer, distribution producer, and isolated acceptance helpers.
Historical package inspection identified host imports and confirmed that the existing packages contain the obsolete `lo-native-1` ABI.
These observations define fresh producer checks. They do not qualify a current embedded compiler.

A Darwin arm64 Clang 21.1.8 driver probe accepted the four selected flags with empty PATH and a closed environment.
For LLVM input and the Darwin target, `-###` reported one in-process compiler stage and no external assembler.
This probe did not compile or execute an artifact. F040 and F041 retain execution and dependency-isolation acceptance.

Independent review passed after corrections to the seven-pair acceptance matrix and destination-local publication ownership.
Document checks found no changed-prose findings, broken local links, duplicate issue identifiers, or dependency cycles.
Unchanged prose retains 24 mechanical language findings outside P008's scope.
These checks close P008's design task only. F038 through F041 and F037 remain open.

## Ordered Implementation Issues

| Order | Issue | Deliverable and dependency |
| --- | --- | --- |
| 1 | F038 | Deterministic embedded payload producer and complete dependency inventory, after P008. |
| 2 | F039 | Bounded resource admission, private materialization, and owned cleanup, after F038. |
| 3 | F040 | Single embedded native build route, CLI migration, and semantic identity updates, after F039. |
| 4 | F041 | Executable-only delivery and isolated acceptance infrastructure, after F040. |
| 5 | F037 | Final semantic, dependency, matrix, and performance qualification after F041. |

The architecture, artifact shape, backend, payload representation, ownership, and transition decisions are resolved by this plan.
Implementation failures remain failures of their owning issues. They do not authorize another backend or dependency route.
