# DAGDSL v0.3 Issues

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
Sections 1, 2, 3, 5, 10, 15, and 16 define DAGDSL as graph-first, immutable,
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
