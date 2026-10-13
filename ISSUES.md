# Lo v0.3 Issues

This file tracks actionable work against `specification.md`. The original report
described broad safety goals; the issues below restate them as implementable
specification and conformance tasks.

Scope note: v0.3 already defines `T!` plus `Error` as the surface fallibility
model, keeps `Null` as its own atomic type, and lists user-visible `Result<T>`,
anonymous recovery syntax, generic `Option<T>`, and concurrency syntax as
non-goals. These issues therefore harden the v0.3 contract instead of adding
conflicting surface features.

## D001 - Make Recoverable Failure Semantics Mechanically Enforceable

Status: Closed
Priority: P0
Area: error model, type checker, runtime

Resolution:
Addressed in `specification.md` by adding the recoverable failure contract,
fatal-panic boundary, trace enrichment rules, required Error.kind values for
fallible standard operations, commit failure kinds, and conformance cases for
fallibility, self-handling flows, adapter handlers, and hidden host exceptions.

Reported concern:
Recoverable operational failures must not escape as hidden host exceptions, and
fatal host panics must be reserved for unrecoverable compiler or runtime
corruption.

Spec context:
Sections 3, 4.5, 5.3, 5.5, 6.7, 10.3, 12, and 15.3 define `T!`, `Error`,
automatic bubbling, self-handling flows, commit failure, and mandatory adapter
`onError` boundaries. Section 3 explicitly excludes user-visible generic
`Result<T>` and exception syntax from v0.3.

Task:
1. Add a normative recoverable-failure contract to `specification.md`.
2. State that implementations may lower `T!` to an internal tagged result, but
   must not expose `Result<T, E>` or exception syntax in the v0.3 surface
   language.
3. Define which failures are recoverable `Error` values for every fallible
   operation in section 18.3, including `UTF8.String.new`, `Math.div`,
   `Math.mod`, `Math.min`, `Math.max`, `Map.get`, `File.read`, and fallible
   list processor invocations.
4. Define the allowed fatal-panic boundary: parser/compiler bugs, violated
   runtime invariants, and unrecoverable host corruption only.
5. Specify how error trace frames are appended when an `Error` crosses a flow
   boundary, including handler invocation and adapter boundary behavior.
6. Add conformance tests for fallible bindings, fallibility-aware pipes,
   self-handling flows, adapter `onError`, top-level failed commits, and plain
   total flows that attempt to use unresolved `U!` values.

Acceptance criteria:
- A plain total flow that contains an unresolved `U!` expression is rejected by
  the type checker.
- A fallible flow automatically propagates unresolved `U!` failures and returns
  `T!`.
- A self-handling flow resolves bubbled errors through its declared pure total
  handler.
- An HTTP adapter cannot be constructed without an `onError` flow of signature
  `(Error) -> HTTP.Response`.
- Recoverable failures from the standard library surface as `Error` values, not
  hidden host exceptions.
- No v0.3 grammar, example, or standard-library signature introduces
  user-visible `Result<T, E>`, `try`, `catch`, `throw`, or expression-level
  recovery operators.

Out of scope:
- Adding a user-visible `Result<T, E>` type.
- Adding a new propagation operator beyond v0.3 automatic bubbling and the
  existing fallibility-aware pipe behavior.

## D002 - Lock Down Null and Absence Semantics

Status: Closed
Priority: P0
Area: type system, records, maps, standard library

Resolution:
Addressed in `specification.md` by making Null a non-inhabiting atomic type,
defining the null literal, requiring exact record fields, modeling absence
through Map.has_key and fallible Map.get, documenting the post-v0.3 optional
data deferral, and adding null/record/map conformance cases.

Reported concern:
The language must not allow implicit null references. Absence must be explicit
and statically visible before code interacts with the underlying value.

Spec context:
Section 6.1 defines `Null` as its own atomic type and says other types do not
inherit from `Null`. Section 3 excludes user-visible generic fallibility
containers and union types beyond the built-in fallible modifier `!`. Section
8.6 already uses `Map.get(...) -> ValueType!` for missing-key handling instead
of nullable returns.

Task:
1. Clarify that `Null` is a concrete value of type `Null`, not a subtype of
   every type and not an implicit inhabitant of `String`, `Int`, `Bool`,
   records, maps, lists, adapter types, or `Error`.
2. Define that v0.3 has no optional record fields. If a record type declares a
   field, construction must provide that field with exactly the declared type.
3. Define absence handling for maps and adapter-shaped data: use `Map.has_key`
   for presence checks and `Map.get(...) -> ValueType!` for fallible lookup.
4. Audit all examples and standard-library signatures so no operation silently
   accepts `Null` as a value of another type.
5. Add negative type-checking examples for passing `Null` to string, numeric,
   boolean, record, HTTP response, and file path operations.
6. Add a deferred design note for a possible post-v0.3 `Option<T>` or union
   model only if the spec later needs first-class optional data.

Acceptance criteria:
- `String.concat(String.new("a"), Null)` is rejected.
- `Math.add(Int.new(1), Null)` is rejected.
- A record value that omits a declared field is rejected.
- A record field declared as `String` cannot be initialized with `Null`.
- `Map.get` remains fallible and does not return a nullable successful value.
- No v0.3 type expression or example introduces `Option<T>` or nullable
  shorthand syntax.

Out of scope:
- Adding `Option<T>` to v0.3.
- Adding nullable shorthand or implicit nullability to any existing type.

## D003 - Formalize Host Execution Constraints and Grammar Testability

Status: Closed
Priority: P1
Area: execution model, grammar, conformance suite

Resolution:
Addressed in `specification.md` by adding deterministic evaluation and host
runtime constraints, explicitly deferring memory-management and concurrency
syntax to host implementation concerns, replacing the grammar sketch with a
normative grammar and disambiguation rules, cleaning up examples, and adding a
conformance matrix.

Reported concern:
The language needs a deterministic execution model, safe dataflow-oriented
concurrency boundaries, and a rigorous specification with atomic, testable,
non-contradictory constraints.

Spec context:
Sections 1, 2, 3, 5, 10, 15, and 16 define Lo as graph-first, immutable,
acyclic, and explicitly activated through `commit`. Section 3 excludes
concurrency syntax from v0.3. Section 6.6 allows lazy, incremental, or eager
list evaluation only when observable semantics do not change. Section 17 is
currently labeled a grammar sketch, not a proven grammar.

Task:
1. Add a host-runtime determinism section to `specification.md` stating that
   values are immutable after construction, graph evaluation strategy is not
   observable, and side effects occur only through evaluated `commit`
   statements.
2. Specify that commits execute exactly once per commit statement evaluation
   and that top-level commits execute in source order after binding resolution.
3. State that ownership, borrowing, reference counting, tracing garbage
   collection, OS threads, green threads, channels, and scheduler policy are
   implementation concerns in v0.3 unless and until the language exposes them
   in a later spec.
4. Add an explicit deferral note: v0.3 has no user-visible shared mutable memory,
   locks, thread creation, channel creation, async syntax, or concurrency
   annotations.
5. Promote the grammar from "sketch" to an implementable grammar by resolving
   known ambiguity risks, including `MatchExpression` recursion,
   `MatchCaseHead = Expression | Identifier | "_"`, pipe precedence,
   projection versus qualified calls, and the arity of qualified identifiers.
6. Create a conformance matrix that maps each grammar production and semantic
   rule to at least one positive example and one negative example where a
   negative example is meaningful.

Acceptance criteria:
- The spec clearly separates v0.3 surface semantics from host-runtime memory
  management choices.
- The spec states that concurrency primitives are not part of v0.3 surface
  syntax.
- Observable evaluation order is defined for commits and adapter activation.
- Parser implementers can build a parser from the grammar without relying on
  undocumented precedence or ambiguity resolution.
- Existing examples in section 19 parse under the finalized grammar.
- The conformance matrix covers failure propagation, null rejection, commit
  activation, flow purity, adapter handler requirements, list processor flow
  references, and match branch unification.

Out of scope:
- Adding ownership or borrowing syntax to v0.3.
- Adding green threads, channels, locks, async/await, or parallel execution
  syntax to v0.3.

## Maintenance

### Recurring

- [ ] [M400R] (P2) Backlog hygiene and archive
  Goal:
  Keep the issue tracker reliable, readable, and focused on active work while preserving resolved history in the appropriate archive.

  Requirements:
  - Cadence: run weekly during active development and before each release cut.
  - Validate section names, identifier prefixes, recurrence suffixes, priority markers, dependencies, and duplicate IDs against the current `issues-md-format.md`.
  - Reconcile stale statuses, duplicate issues, broken references, obsolete instructions, and entries filed under the wrong section.
  - Move completed non-recurring history to the repository issue archive or durable documentation when the active tracker becomes noisy.
  - Keep active, blocked, planning, and recurring entries visible in `ISSUES.md`.

  Deliverables:
  - Normalized `ISSUES.md` structure and statuses.
  - Updated issue archive or docs when completed entries are removed from the active tracker.
  - A short `Last run:` note summarizing the cleanup and any follow-up issues filed.

  Validation:
  - Re-read `ISSUES.md` after edits and confirm every issue is under the right section with a unique section-aware ID.
  - Confirm recurring entries remain open and keep the `R` suffix.
  - Confirm no active, blocked, recurring, or planning work was archived.

- [ ] [M401R] (P2) Polish open issues
  Goal:
  Keep unresolved work executable by making each open issue concrete, ordered, and testable.

  Requirements:
  - Cadence: run weekly during active development and before handing a repo to automated execution.
  - Review every unresolved non-recurring issue for missing context, dependencies, repro steps, acceptance criteria, and validation expectations.
  - Make priorities concrete and ensure each open issue has actionable deliverables.
  - Merge duplicate open issues or add explicit dependency links when separate entries must remain.
  - Do not close or implement issues as part of this polish pass unless that work is separately requested.

  Deliverables:
  - Open issues with enough detail for a person or agent to execute without rediscovery.
  - New or updated dependency markers where ordering matters.
  - A short `Last run:` note listing the number of issues polished and any blockers found.

  Validation:
  - Sample the open entries after the pass and confirm each has clear next actions and validation expectations.
  - Confirm no recurring runbook was marked complete.
  - Confirm duplicates were merged or explicitly cross-referenced.

- [ ] [M402R] (P2) Architecture and policy review
  Goal:
  Catch architecture, policy, and workflow drift before it becomes hidden maintenance debt.

  Requirements:
  - Cadence: run monthly, before large refactors, and after major framework or runtime changes.
  - Review the codebase, docs, and workflow against `AGENTS.md`, `POLICY.md`, stack guides, and the current architecture notes.
  - Look for drift from forward-only contracts, edge-validation boundaries, smart-constructor usage, testing policy, and module ownership.
  - Record findings as new Maintenance issues with concrete scope, priority, and validation.
  - Close the pass with a no-action note only when the review finds no actionable drift.

  Deliverables:
  - New Maintenance issues for each actionable architecture or policy drift finding.
  - Updated notes on areas reviewed and areas intentionally left unchanged.
  - A short `Last run:` note with the review scope and outcome.

  Validation:
  - Confirm every finding is represented as an issue with owner-readable context and validation criteria.
  - Confirm no implementation changes were mixed into the review runbook unless separately requested.
  - Confirm all recurring runbooks remain open.

- [ ] [M403R] (P1) Dependency and security audit
  Goal:
  Keep third-party dependencies, runtime versions, and security-sensitive configuration within the current supported contract.

  Requirements:
  - Cadence: run weekly for active apps and before each release cut.
  - Inspect package managers, lockfiles, language toolchains, container bases, and generated clients for known vulnerabilities or stale direct dependencies.
  - Review auth, secret, CORS, CSP, SQL, network, and permission-sensitive configuration for drift from the current contract.
  - Prefer current supported dependencies; do not add compatibility shims for obsolete dependency behavior.
  - File separate Maintenance or BugFix issues for each actionable vulnerability, unsupported runtime, or security-contract gap.

  Deliverables:
  - Documented audit commands or data sources used for the pass.
  - Updated issues for each actionable dependency or security finding.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the repository-native audit, lint, or dependency checks used for the pass.
  - Confirm every finding is either filed, fixed under a separate issue, or explicitly marked not applicable with evidence.
  - Confirm no secrets or private payloads were written into the tracker.

- [ ] [M404R] (P1) CI, release, and artifact health
  Goal:
  Keep the repository's validation, release, publication, and generated artifact surfaces trustworthy.

  Requirements:
  - Cadence: run before every release, publish, or deploy, and weekly for critical services.
  - Verify repository-native CI, lint, format, coverage, release, publish, Docker image, Pages, and artifact workflows still match the documented contract.
  - Check generated artifacts, release tags, published images, and Pages outputs for source-to-public drift.
  - File concrete follow-up issues for failing gates, stale artifacts, missing release prerequisites, or undocumented workflow changes.
  - Do not perform production deployment from this runbook unless the operator explicitly requests that deployment.

  Deliverables:
  - Recorded gate status and artifact surfaces inspected.
  - Follow-up issues for each reproducible CI, release, publish, or artifact drift problem.
  - A short `Last run:` note with commands run and any skipped surfaces.

  Validation:
  - Use repository-native `make` targets or documented release helpers for checks.
  - Confirm release and deployment ownership boundaries remain separate.
  - Confirm public or published artifacts match the intended source revision when that surface is inspected.

- [ ] [M405R] (P1) Code contract and static hygiene
  Goal:
  Keep source contracts explicit, current, and statically guarded against policy drift.

  Requirements:
  - Cadence: run monthly and before large refactors.
  - Scan for dead code, unused exports, duplicated literals, silent fallbacks, legacy aliases, compatibility reads, and zero-but-invalid domain states.
  - Check static analysis, coverage, schema, and contract guards that are supposed to prevent drift.
  - File focused Maintenance issues for each concrete violation instead of broad cleanup placeholders.
  - Keep the current canonical contract only; do not preserve obsolete behavior unless a product requirement explicitly says so.

  Deliverables:
  - Issue entries for each actionable static hygiene or contract violation.
  - Notes on static tools, searches, and contract guards used during the pass.
  - A short `Last run:` note with clean result or follow-up issue IDs.

  Validation:
  - Rerun the relevant static checks, contract tests, or repository searches used to identify drift.
  - Confirm every finding has a narrow follow-up issue and does not duplicate existing backlog work.
  - Confirm no implementation changes were mixed into the audit unless separately requested.

- [ ] [M406R] (P1) Production drift and health
  Goal:
  Detect when production, public, or scheduled runtime state has drifted from the intended repository contract.

  Requirements:
  - Cadence: run weekly for deployed services and after each publish or deploy.
  - Compare current source, runtime configuration, published images, public routes, scheduled jobs, and health checks for drift.
  - Inspect real operator-facing surfaces rather than assuming merged source is deployed.
  - File follow-up issues for stale images, stale Pages output, missing routes, failed monitors, invalid production config, or undocumented runtime differences.
  - Stop before production deploy or destructive operator actions unless the operator explicitly requests them.

  Deliverables:
  - Recorded source revision, public artifact, route, image, or health surfaces inspected.
  - Follow-up issues for each source-to-runtime drift finding.
  - A short `Last run:` note with evidence links or commands used.

  Validation:
  - Verify inspected production or public surfaces directly where access is available.
  - Confirm any deploy-required finding is filed with the exact publish/deploy boundary and owner.
  - Confirm no production state was changed by the audit unless explicitly requested.

- [ ] [M407R] (P2) Documentation and runbook hygiene
  Goal:
  Keep durable documentation and runbooks aligned with the current behavior users and operators actually rely on.

  Requirements:
  - Cadence: run before release cuts and after merge bursts that change user-facing or operator-facing behavior.
  - Review README, ARCHITECTURE, PRD, CHANGELOG, docs, runbooks, setup guides, and local workflow notes for stale behavior or missing new contracts.
  - Update docs when closed issues changed durable behavior, public APIs, operator workflows, release semantics, or deployment expectations.
  - Remove or rewrite stale instructions instead of preserving obsolete alternatives.
  - File separate issues for documentation gaps that require product or implementation decisions.

  Deliverables:
  - Updated documentation or filed follow-up issues for each gap.
  - A short `Last run:` note listing docs inspected and changes made.
  - Cross-references from archived issue history to durable docs when useful.

  Validation:
  - Check links, command names, paths, and public contract descriptions touched by the pass.
  - Confirm docs describe the current canonical path only.
  - Confirm issue archive and active tracker references remain consistent.
