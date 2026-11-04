# GitHub Compiler Delivery Plan

Status: planned under F042. Public delivery is not ready.
The first delivery surface is [Lo on GitHub](https://github.com/tyemirov/Lo/releases).

## Current State

The audit on November 4, 2026 found no GitHub releases or release tags.
The repository has CI and compiler package construction.
It has no selected deployment manifest or root `release`, `publish`, and `deploy` targets.

The audited Lo commit is `97819d1d5233f596bbd773373361d6d0eb13aba1`.
The installed Gateway is `v8.0.2`, from commit `0c27d260a06a3cb82b049aaf24b26bb617530f92`.
The installed lifecycle contract is `4`.

F017 supplies the existing [offline package producer](compiler-distribution.md).
That producer supplies a directory with the frontend, backend tools, runtime archives, and platform data.
It does not yet supply the required [self-contained compiler executable](self-contained-compiler.md).
F038 through F041 implement that executable. F037 owns its final qualification.

## Required Download Artifacts

The first release uses the [selected compiler matrix](self-contained-compiler-design.md#host-target-and-variant-matrix).

| Compiler artifact | Host | Generated application targets |
| --- | --- | --- |
| Software compiler | Darwin arm64 | Darwin arm64 and Linux arm64 software |
| Software compiler | Linux arm64 | Darwin arm64 and Linux arm64 software |
| Metal compiler | Darwin arm64 | Both software targets and Darwin arm64 Metal |

Each compiler artifact contains one compiler executable.
The executable contains all non-OS compiler dependencies.
An archive can also supply installation instructions, notices, and source records.
Those records are not compiler runtime dependencies.

Lo owns the exact archive names, variant identities, checksums, source provenance, and version policy.
All artifacts in one release use the same selected version and source commit.
The declared OS baselines remain those in the compiler contract.
Additional architectures require separate implementation and qualification.

## Standard Lifecycle Stages

The planned operator command is:

```sh
make release && make publish && make deploy
```

Each target delegates its phase to the installed Gateway with the selected Lo application root.
The selected manifest declares artifact identities, publication identities, deployment targets, and their phase order.
Lo supplies compiler build rules and acceptance checks.
The Gateway supplies the generic lifecycle, receipts, retries, and publication mechanisms.

| Stage | Required result |
| --- | --- |
| `make release` | Validate the source and CI. Construct and verify the compiler artifacts. Seal their identities. |
| `make publish` | Upload the sealed artifacts to the versioned GitHub release and record verified remote identities. |
| `make deploy` | Reconcile the declared GitHub delivery target and verify its release and asset identities against the publication receipt. |

Publication consumes the sealed release without another compiler build.
Deployment consumes the publication receipt without a manual digest handoff.
An interrupted phase uses the retained version and receipt state on retry.
Changed asset bytes must not replace an existing immutable publication.

The compiler has no continuously running application service.
Its initial deployment target is GitHub artifact delivery.
This scope requires no application server, container runtime, or service health endpoint.

## Implementation Sequence

### 1. Produce The Compiler

1. Complete the embedded payload in F038.
2. Complete materialization and owned cleanup in F039.
3. Use the embedded build route in F040.
4. Deliver the isolated compiler executables in F041.
5. Complete final compiler qualification in F037 before public release.

F042 lifecycle wiring can proceed alongside compiler implementation.
The first public release still requires the qualified compiler artifacts.

### 2. Declare Lo Delivery

1. Add the versionless manifest at `.mprlab/deploy/resources.yml`.
2. Use the installed Gateway's admitted declaration contract.
3. Declare `owner`, `release`, and `lifecycle` with the `artifacts`, `publications`, and `targets` catalogs.
4. Declare the three compiler variants and the GitHub delivery target.
5. Define release version selection and artifact names in Lo.
6. Add root `release`, `publish`, and `deploy` delegates.
7. Verify that artifact preparation includes the embedded payload before frontend compilation.
8. Validate the complete declaration through the installed Gateway before lifecycle acceptance.

Installed Gateway code includes GitHub binary publication and release-artifact verification.
The audit did not establish acceptance of a Lo declaration with all three compiler variants.
F042 must establish that acceptance and identify any unsupported declaration or adapter behavior.

Current Lo policy still describes the older root `resources` collection.
The installed deployment skill also describes that older model.
The current Governor template uses the declaration catalogs above.
F042 must reconcile Lo policy with the admitted Gateway contract before manifest acceptance.

Current Lo policy also prohibits external publication and deployment.
F042 must encode the user's GitHub delivery requirement before operational use.
Keep local task files and validation output inside Lo.
Limit external changes to the explicitly authorized GitHub delivery operations.

This plan does not introduce a manifest or establish its acceptance.

### 3. Validate Without Public Mutation

1. Start with failing integration checks through the real lifecycle entry points.
2. Use controlled GitHub protocol responses and repository-local state.
3. Verify artifact selection, source capture, checksums, receipts, and retry behavior.
4. Reject missing payloads, stale source, changed bytes, and incomplete publication records.
5. Verify that each phase consumes the preceding phase's recorded identities.
6. Run compiler acceptance in software isolation and Linux emulation.
7. Complete the applicable repository checks and independent review.

The seven compiler-variant and application-target pairs retain their F041 acceptance requirements.
Fixture success does not establish public GitHub publication.

### 4. Cut The First Public Release

1. Select committed default-branch source that matches the remote default branch.
2. Complete F037 and F042 implementation acceptance.
3. Validate the manifest, release decision, current credentials, and selected artifact inputs.
4. Run the standard lifecycle command.
5. Read the resulting release, publication, and deployment receipts.
6. Verify public download URLs and compare the downloaded hashes with the sealed identities.
7. Verify installation, relocation, offline compilation, and independent application execution from those downloads.

Download and compilation checks are Lo application acceptance.
They remain distinct from Gateway delivery status.
All agent validation workspaces and outputs stay inside Lo.

## Readiness Evidence

| Evidence | Current status |
| --- | --- |
| Offline compiler package producer | Implemented under F017 |
| Self-contained compiler executable | Pending F038 through F041 and F037 |
| Selected manifest and standard lifecycle targets | Missing, tracked by F042 |
| Sealed release for the new delivery contract | Not established |
| Public GitHub release assets | None observed |
| Verified deployment target | Not established |

Implementation issues close after their code, documentation, and required checks pass.
The first public operation remains recorded here until its receipts and download checks establish delivery.
