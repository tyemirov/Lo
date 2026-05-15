DAGDSL v0.3 Draft Specification

1. Purpose

DAGDSL is a typed declarative dataflow language for building transformation pipelines, file-processing jobs, and HTTP services.

The language is built around a simple discipline:

1. construct values and adapters with .new(...),
2. transform values through pure processors and named flows,
3. represent fallibility explicitly in the type system, and
4. activate side effects and activatable adapters explicitly with commit.

DAGDSL is graph-first. A program describes data dependencies, branching, fallibility, and boundary activation. It does not describe imperative step-by-step control flow.

⸻

2. Design Principles

DAGDSL follows these principles.

1. Explicit data over hidden behavior. Values, processors, flows, adapters, errors, and commits are separate concepts.
2. Construction is separate from activation. .new(...) constructs. commit activates only side effects and activatable adapters.
3. Named flows only. DAGDSL does not support lambdas or anonymous functions in v0.3.
4. One ordered collection type. The surface language exposes List<T> only. Laziness is a runtime concern.
5. Pragmatic standard library. Common arithmetic and aggregate operations belong in Math. Users should not be forced to express routine math through folds.
6. Typed boundaries. The language exposes domain-oriented adapters such as File and HTTP, not generic IO.
7. Readable pipeline syntax. DAGDSL uses |> as first-argument threading syntax.
8. Errors are values. Fallibility is explicit in the type system and can bubble or be resolved at declared handler boundaries.
9. Boundary handlers are mandatory. Adapters that invoke flows must require an onError handler so uncaught errors are always translated at the boundary.
10. Constructors are uniform. Any operation that constructs a value uses .new(...).

⸻

3. Non-Goals for v0.3

The following are out of scope in v0.3:

1. Classes and inheritance.
2. Mutable variables.
3. Imperative if / else statements.
4. Loops as statements.
5. Anonymous functions or lambdas.
6. User-visible generic fallibility containers such as Result<T>.
7. Modules, imports, or packages.
8. Union types beyond the built-in fallible type modifier !.
9. Cyclic graphs.
10. Concurrency syntax.
11. User-visible exception syntax such as throw, try, or catch.
12. A Unit type.
13. Expression-level onError operators.

⸻

4. Core Semantic Categories

DAGDSL has five core semantic categories.

4.1 Values

Immutable typed data such as strings, integers, records, maps, lists, ranges, HTTP requests, HTTP responses, and errors.

4.2 Processors

Pure operations that transform input values into output values.

Examples:

* String.concat
* Math.add
* List.filter
* Range.contains

4.3 Flows

Named reusable graph templates.

A flow may be:

* total, if it returns T,
* fallible, if it returns T!, or
* self-handling, if it returns T onError ErrorHandler.

A flow may also be:

* pure, if it contains no commit statements, or
* effectful, if it commits one or more committable targets.

Only pure named flows may be passed to pure data processors.

4.4 Adapters and Effects

Adapters model typed boundaries to the external world.

Examples:

* File.Path
* HTTP.Server

Some adapter operations produce committable targets.

Examples:

* File.append(...) -> Effect
* HTTP.Server.new(...) -> HTTP.Server

A committable target is activated only by commit.

4.5 Errors and Fallibility

Runtime failure is represented explicitly by:

* the built-in nominal composite type Error, and
* the built-in postfix fallible type modifier !.

T! means either a successful T or an Error.

Examples:

* Int!
* String!
* HTTP.Response!

DAGDSL does not expose a user-visible generic Result<T> type in v0.3.

An implementation may lower T! to an internal tagged result representation, but
that representation is not part of the surface language or public type system.
Recoverable operational failures must be represented as Error values. They must
not be exposed to DAGDSL programs as hidden host-language exceptions.

Fatal host panics are outside the recoverable Error model and are reserved for:

1. implementation bugs in the lexer, parser, compiler, type checker, or runtime,
2. violated runtime invariants after successful type checking, and
3. unrecoverable host failures such as process termination or memory corruption.

⸻

5. Execution Model

5.1 Pure Expressions

Pure expressions evaluate as ordinary expressions when referenced. They do not require commit.

Examples:

* String.new(...)
* Record.new(...)
* List.new(...)
* Range.new(...)
* Match.new(...)

5.2 Commit

commit is the only activation keyword in DAGDSL.

A commit statement activates a previously bound committable target.

A committable target may be:

1. an Effect, such as a file write or append operation, or
2. an activatable adapter instance, such as HTTP.Server.

Examples:

write_effect = File.append(file_path, content_string)
commit write_effect
target_server = HTTP.Server.new(port: Int.new(8080), handler: HelloHandler, onError: HelloError)
commit target_server

5.3 Fallible Execution and Bubbling

If an expression has type T!, then it may produce either a successful T or an Error.

If a fallible expression yields an Error, then downstream dependent nodes are not evaluated. The error bubbles outward to the nearest enclosing flow handler boundary.

When an error bubbles across a flow boundary, the runtime appends a trace frame to the error.

5.4 Top-Level Execution

A source file may contain:

* top-level bindings,
* top-level flow definitions,
* top-level commit statements, and
* zero or one top-level return statement.

Top-level commit statements are executed in source order after binding resolution.
If a top-level return statement is present, it must be the final top-level
statement. It is evaluated after any preceding top-level commit statements have
completed successfully.

A top-level return may produce either T or T!.

If a top-level return produces an uncaught Error, the host runtime reports it using an implementation-defined diagnostic format.

5.5 Commit Failure in v0.3

Statement-level commit failure participates in the same bubbling model inside flows.

Rules:

1. inside a fallible flow, failed commit produces Error and bubbles,
2. inside a self-handling flow, failed commit produces Error and is handled by the flow’s declared onError handler,
3. inside a plain total flow, commit is not allowed,
4. at top level, failed commit aborts activation.

This means any flow that uses commit must declare either -> T! or -> T onError ErrorHandler in v0.3.

5.6 Deterministic Evaluation and Host Runtime Constraints

DAGDSL values are immutable after construction. A binding names a value or a
committable target; it does not introduce mutable storage.

The runtime may evaluate pure graph nodes eagerly, lazily, incrementally, or in
parallel, provided the observable result is identical to an evaluation that
respects data dependencies and the rules in this specification. Evaluation
strategy is not observable to a DAGDSL program except through explicit commit
activation and Error bubbling.

Side effects occur only through evaluated commit statements. Each evaluated
commit statement activates its target exactly once. Reusing the same bound
Effect or activatable adapter in multiple commit statements is allowed only as
multiple explicit activations, one per evaluated commit statement.

Top-level commit statements execute in source order after top-level binding and
flow definition resolution. Inside a flow, commit statements execute in source
order along the evaluated path of that flow body. If an unresolved Error bubbles
before a later commit statement is reached, that later commit statement is not
evaluated.

Ownership, borrowing, reference counting, tracing garbage collection, OS
threads, green threads, channels, locks, and scheduler policy are host
implementation concerns in v0.3. They are not user-visible language constructs.
An implementation may use such mechanisms internally only if observable DAGDSL
semantics remain unchanged.

v0.3 exposes no shared mutable memory, thread creation, channel creation,
async/await syntax, lock syntax, concurrency annotations, or scheduler controls.

5.7 Binding and Value Passing

A binding introduces an immutable name for the value produced by its expression.

A binding does not allocate user-visible mutable storage. A bound name is not a
variable cell, pointer, reference, address, or assignable location.

When a flow is invoked, each parameter is initialized as an immutable binding to
the corresponding argument value. Passing an argument to a flow uses value
semantics. The callee cannot mutate, rebind, invalidate, consume, retain, or
observe the caller's binding.

Implementations may represent values using copying, structural sharing,
reference counting, tracing garbage collection, borrowing, host pointers, or
other implementation techniques. These choices must not be observable by a
DAGDSL program.

DAGDSL has no address-of operation, dereference operation, pointer identity
operation, move operation, borrow operation, destructor, finalizer, or aliasing
primitive in v0.3.

Two bindings may name equal values, but there is no operation that asks whether
they refer to the same host object or storage location.

Composite values such as List<T>, Map<K, V>, Record{...}, Range<T>, Error, and
boundary values are immutable after construction. Passing such a value to a flow
or processor does not copy it semantically and does not transfer ownership.

Committable targets such as Effect and HTTP.Server are first-class immutable
activation descriptions. Passing them around does not activate them. Only commit
activates them, and each evaluated commit statement activates its target exactly
once.

⸻

6. Type System

6.1 Atomic Types

The atomic built-in types are:

* Scalar
* String
* Bytes
* Int
* Float
* Bool
* Null

Scalar denotes a single Unicode scalar value.

Int denotes a signed mathematical integer. A v0.3 implementation may choose a
bounded host representation internally, but arithmetic semantics are defined in
terms of mathematical integers. Recoverable numeric domain failures are listed
in section 18.3.

Float denotes an implementation-defined finite floating-point value. v0.3 does
not define Float arithmetic processors.

Bool has exactly two values, true and false.

Null is its own atomic type. The null literal is spelled null and has type Null.
Other types do not inherit from Null. Null is not an implicit inhabitant of
String, Bytes, Int, Float, Bool, records, maps, lists, ranges, Error, File.Path,
HTTP.Request, HTTP.Response, HTTP.Server, or Effect.

The type name Null is not a value expression. Passing either the null literal or
an unresolved identifier named Null where another type is required is a type
error.

6.2 Fallible Types

If T is a type, then T! is the corresponding fallible type.

Examples:

* Int!
* Bytes!
* HTTP.Response!

T! is the only built-in user-visible representation of fallibility.

6.3 Composite Types

The composite types are:

* List<T>
* Map<K, V>
* Record{ field_name: FieldType, ... }
* Range<T>
* Error

Error is a built-in nominal composite type with required fields. It is not structurally interchangeable with Record.

Record{ ... } is an exact structural record type in v0.3. A value assigned to an
expected Record{ ... } type must provide every declared field exactly once with
the declared type. It must not omit required fields, add extra fields, or fill a
field with null unless that field is explicitly declared as Null. The bare type
Record denotes an implementation-facing record value of any exact record shape;
portable v0.3 programs may project only fields known from an exact
Record{ ... } type or from a built-in boundary type.

The equality-comparable types in v0.3 are Scalar, String, Int, Bool, and Null.
The ordered-comparable types in v0.3 are Scalar, String, and Int. Bytes, Float,
records, maps, lists, ranges, Error, and boundary types are not comparable
unless a later specification adds explicit processors for that comparison.

6.4 Boundary Types

The initial boundary-oriented built-in types are:

* File.Path
* HTTP.Request
* HTTP.Response
* HTTP.Server
* Effect

These are first-class language types, but their host-language representation is implementation-defined.

6.5 String Model

String is semantically an ordered finite sequence of Scalar values.

Where a processor expects List<Scalar>, a String value is valid input. A list of scalars can be explicitly converted back into a String using String.FromScalars.new(scalar_list).

This means the following is valid:

match_count = file_text |> List.filter(IsMatchingScalar) |> List.length()

provided IsMatchingScalar has signature (Scalar) -> Bool.

String remains a distinct surface type so that text-specific operations remain explicit.

String.length(value) -> Int returns the number of Unicode scalar values.

6.6 List Model

List<T> is the only ordered collection type exposed by the language surface.

The language does not expose a separate sequence type. Laziness, streaming, and incremental realization are runtime concerns.

A runtime may evaluate list-producing expressions lazily, incrementally, or eagerly, provided observable semantics do not change.

6.7 Error Model

Error is a built-in nominal composite value with the following required fields:

* kind: String
* message: String
* input: Record
* trace: List<Record{ flow: String, operation: String, input: Record }>

Notes:

1. input describes the immediate input that caused the failure.
2. trace accumulates flow and operation context as the error bubbles outward.
3. Error.new(kind: ..., message: ..., input: ...) constructs an error value with an implicit empty trace.
4. Error.kind values are stable strings. Implementations may add more specific
   kind strings, but must preserve the minimum required kind strings listed in
   section 18.3 for the corresponding recoverable failures.

⸻

7. Constructors

DAGDSL favors .new(...) for construction.

7.1 Built-in Constructors

* Scalar.new(value)
* String.new(value)
* Bytes.new(value)
* Int.new(value)
* Float.new(value)
* Bool.new(value)
* List.new(value_one, value_two, ...)
* Map.new(entry_one, entry_two, ...)
* Record.new(field_one: value_one, field_two: value_two, ...)
* Range.new(start: ..., end: ..., include_start: ..., include_end: ...)
* File.Path.new(path)
* HTTP.Server.new(port: ..., handler: ..., onError: ...)
* HTTP.Response.Text.new(status: ..., body: ...)
* HTTP.Response.Bytes.new(status: ..., body: ...)
* Error.new(kind: ..., message: ..., input: ...)
* UTF8.String.new(bytes) -> String!
* String.FromScalars.new(scalar_list)

7.2 Constructor Rules

1. List.new requires elements of one unified type.
2. Map.new requires each entry to have exact type Record{ key: K, value: V }
   for one unified equality-comparable key type K and one unified value type V.
   Map.new() is valid only when an expected Map<K, V> type is available from
   context.
3. Record.new produces an exact structural record type from the field names and
   values supplied to the constructor. It does not create optional fields.
4. Range.new(...) requires ordered-comparable endpoints of the same type. It is
   total and may construct an empty range.
5. Scalar.new requires input that resolves to exactly one Unicode scalar value.
6. HTTP.Server.new(...) constructs a server instance but does not start it. It requires a mandatory onError flow.
7. Any operation that constructs a value must use .new(...) in v0.3.

⸻

8. Processors

Processors are pure.

8.1 String Processors

* String.concat(value_one, value_two, ...) -> String
* String.upper(value) -> String
* String.lower(value) -> String
* String.length(value) -> Int
* String.eq(left_value, right_value) -> Bool

8.2 Scalar Processors

* Scalar.eq(left_value, right_value) -> Bool

8.3 Conversion Processors

* Int.to_string(value) -> String

8.4 Math Processors

DAGDSL includes a pragmatic Math namespace.

All Math processors in v0.3 operate on Int operands or List<Int> inputs.

* Math.add(left_value, right_value) -> Int
* Math.sub(left_value, right_value) -> Int
* Math.mul(left_value, right_value) -> Int
* Math.div(left_value, right_value) -> Int!
* Math.mod(left_value, right_value) -> Int!
* Math.lt(left_value, right_value) -> Bool
* Math.lte(left_value, right_value) -> Bool
* Math.gt(left_value, right_value) -> Bool
* Math.gte(left_value, right_value) -> Bool
* Math.eq(left_value, right_value) -> Bool
* Math.sum(number_list) -> Int
* Math.product(number_list) -> Int
* Math.min(number_list) -> Int!
* Math.max(number_list) -> Int!

8.5 Range Processors

* Range.contains(target_range, target_value) -> Bool
* Range.is_empty(target_range) -> Bool

A Range<T> is an immutable value with four semantic components: start, end, include_start, and include_end.

8.6 Record and Map Processors

* Map.has_key(target_map, target_key) -> Bool
* Map.get(target_map, target_key) -> ValueType!
* Record.merge(left_record, right_record) -> Record

Map.get returns Error when the requested key is not present. Missing keys are
not represented as null. Use Map.has_key when a flow needs a total presence
check before attempting a fallible lookup.

Record.merge returns an exact record containing every field from left_record and
right_record. If both records contain the same field name, the right_record field
is used in the result. The signature is written as Record because the exact
result shape is computed by the type checker from the input shapes.

8.7 List Processors

List processors accept pure named flow references, not anonymous functions.

The target list is always the first argument. The flow reference is always the final argument. Zero or more explicit bound context arguments may appear between them.

Examples of surface forms:

* List.map(target_list, FlowName)
* List.map(target_list, context_data, FlowName)
* List.filter(target_list, context_a, context_b, FlowName)

If extra context arguments are supplied, the referenced flow must accept them first and the list element last.

Type rules:

* List.map(target_list, FlowName) -> List<MappedType> where FlowName: (ElementType) -> MappedType
* List.map(target_list, context_one, ..., context_n, FlowName) -> List<MappedType> where FlowName: (ContextOneType, ..., ContextNType, ElementType) -> MappedType
* List.map(...) -> List<MappedType>! when the referenced flow is fallible and returns MappedType!
* List.filter(target_list, FlowName) -> List<ElementType> where FlowName: (ElementType) -> Bool
* List.filter(target_list, context_one, ..., context_n, FlowName) -> List<ElementType> where FlowName: (ContextOneType, ..., ContextNType, ElementType) -> Bool
* List.filter(...) -> List<ElementType>! when the referenced flow is fallible and returns Bool!
* List.fold(target_list, initial_state, FlowName) -> AccumulatorType where FlowName: (AccumulatorType, ElementType) -> AccumulatorType
* List.fold(target_list, initial_state, context_one, ..., context_n, FlowName) -> AccumulatorType where FlowName: (ContextOneType, ..., ContextNType, AccumulatorType, ElementType) -> AccumulatorType
* List.fold(...) -> AccumulatorType! when the referenced flow is fallible and returns AccumulatorType!
* List.flat_map(target_list, FlowName) -> List<MappedType> where FlowName: (ElementType) -> List<MappedType>
* List.flat_map(target_list, context_one, ..., context_n, FlowName) -> List<MappedType> where FlowName: (ContextOneType, ..., ContextNType, ElementType) -> List<MappedType>
* List.flat_map(...) -> List<MappedType>! when the referenced flow is fallible and returns List<MappedType>!
* List.length(target_list) -> Int
* List.unfold(seed_value, iteration_count, FlowName) -> List<ElementType> where FlowName: (StateType) -> Record{ value: ElementType, next_state: StateType }
* List.unfold(seed_value, iteration_count, context_one, ..., context_n, FlowName) -> List<ElementType> where FlowName: (ContextOneType, ..., ContextNType, StateType) -> Record{ value: ElementType, next_state: StateType }
* List.unfold(...) -> List<ElementType>! when the referenced flow is fallible and returns Record{ value: ElementType, next_state: StateType }!

The old _with operator family is removed. Explicit context passing is handled by the normal list operators.

Because String is semantically compatible with List<Scalar>, these processors may also operate on strings.

⸻

9. Adapters

Adapters define typed boundaries to the external world.

9.1 File Adapter

* File.Path.new(file_path: String) -> File.Path
* File.read(file_path: File.Path) -> Bytes!
* File.append(file_path: File.Path, file_content: String) -> Effect
* File.write(file_path: File.Path, file_content: Bytes) -> Effect

File.read is a fallible boundary read.

File.append and File.write produce effects and therefore require commit.

9.2 HTTP Adapter

* HTTP.Server.new(port: Int, handler: FlowName, onError: FlowName) -> HTTP.Server
* HTTP.Response.Text.new(status: Int, body: String) -> HTTP.Response
* HTTP.Response.Bytes.new(status: Int, body: Bytes) -> HTTP.Response

HTTP.Response.Text.new encodes body as UTF-8 bytes and constructs a response
with an empty headers map. HTTP.Response.Bytes.new uses the supplied bytes as
the body and constructs a response with an empty headers map.

handler must reference a named flow with one of these signatures:

* (HTTP.Request) -> HTTP.Response
* (HTTP.Request) -> HTTP.Response!

onError must reference a pure total named flow with signature:

* (Error) -> HTTP.Response

onError is mandatory. It guarantees that uncaught handler errors and ordinary request-processing boundary errors are translated at the adapter boundary.

9.3 HTTP Request and Response Shape

HTTP.Request is a structured semantic value with at least these fields:

* method: String
* path: String
* headers: Map<String, String>
* body: Bytes

HTTP.Response is a structured semantic value with at least these fields:

* status: Int
* headers: Map<String, String>
* body: Bytes

The adapter is responsible for parsing incoming bytes into HTTP.Request and serializing HTTP.Response back to bytes.

The listed HTTP.Request and HTTP.Response fields are always present. Portable
v0.3 programs may project only these listed fields unless a later adapter
revision specifies additional fields. Optional or extension data must be modeled
through maps and accessed with Map.has_key or Map.get rather than nullable
fields.

⸻

10. Flow Definitions

A flow is a named reusable graph template.

10.1 Forms

flow FlowName(parameter_one: TypeOne, parameter_two: TypeTwo) -> OutputType {
    statement
}
flow FlowName(parameter_one: TypeOne, parameter_two: TypeTwo) -> OutputType! {
    statement
}
flow FlowName(parameter_one: TypeOne, parameter_two: TypeTwo) -> OutputType onError ErrorHandler {
    statement
}

10.2 Flow Rules

1. A flow may appear at top level or inside another flow.
2. A flow may have zero or more parameters.
3. A flow has exactly one return statement, and that return statement must be
   the final statement in the flow body.
4. A flow body may contain bindings, nested flow definitions, commit statements, and a return statement.
5. A flow with no commit statements is a pure flow.
6. Only pure named flows may be passed to pure data processors.
7. Adapter boundary parameters may accept effectful flows as defined by the adapter signature.
8. DAGDSL does not support anonymous functions or lambdas in v0.3.
9. A nested flow does not implicitly capture outer values. Any required value must be passed explicitly as a parameter.
10. A plain total flow (-> T) may not use unresolved fallible expressions.
11. A fallible flow (-> T!) automatically propagates unresolved fallible expressions via error bubbling.
12. A self-handling flow (-> T onError ErrorHandler) executes like a fallible flow internally, but resolves any bubbled error through ErrorHandler before publishing its result.
13. ErrorHandler in a self-handling flow must be a pure total named flow with signature (Error) -> T.
14. A flow that contains commit must declare either -> T! or -> T onError ErrorHandler.

10.3 Automatic Propagation in Fallible Flows

Inside a fallible flow (-> T!) or self-handling flow (-> T onError ErrorHandler):

* if a binding expression has type U, the bound name has type U,
* if a binding expression has type U! and has no nearer flow-level handler, then on success the bound name has type U, and on failure the Error bubbles outward,
* a return expression lifts T to success when the flow returns T!,
* a return expression passes through T! unchanged when the flow returns T!.

In a self-handling flow, the nearest handler boundary is the flow signature’s onError ErrorHandler clause.

⸻

11. Match

Branching is expression-based, not statement-based.

DAGDSL uses a single pure match constructor:

Match.new(selector_value,
    case_head_one: expression_one,
    case_head_two: expression_two,
    _: default_expression
)

Each case head is one of:

* an expression compatible with the selector type for exact matching,
* an expression of type Range<T> compatible with the selector type for range
  matching,
* when FlowName for predicate matching, where FlowName is a pure total named
  flow with signature (T) -> Bool, or
* _ for the default case.

Match rules:

1. Cases are checked in source order.
2. The first matching case wins.
3. _ is required in v0.3.
4. All branch expressions must unify to one output type.
5. Only the selected branch is evaluated.
6. If a branch must trigger a side effect, the branch must evaluate to a committable target value, which must then be explicitly activated by a commit statement in the outer scope.

⸻

12. Error Handling

12.1 Expression-Level Fallibility

Expression-level fallibility is represented with T!.

12.2 Bubbling

If a fallible expression yields Error, then downstream dependent nodes are not evaluated. The error bubbles outward to the nearest enclosing handler boundary.

The handler boundary order is:

1. the current flow signature’s onError ErrorHandler, if present,
2. otherwise the next enclosing fallible caller flow,
3. otherwise the nearest adapter boundary with mandatory onError.

12.3 Self-Handling Flow Resolution

A flow declared as:

flow FlowName(...) -> T onError ErrorHandler {
    ...
}

is internally fallible but externally total.

If an unresolved error reaches the flow boundary, control transfers to ErrorHandler(error). The handler’s total T becomes the published result of the flow.

12.4 Mandatory Adapter Handlers

Every adapter that invokes user flows must require a boundary onError handler.

This guarantees that uncaught bubbled errors are translated at the boundary.

12.5 Error Trace Enrichment

Whenever an Error bubbles across a flow boundary, the runtime appends a trace frame containing the current flow name, the current operation name if available, and the current input record if available.

Trace frames are appended before invoking a self-handling flow handler or an
adapter-level onError handler. The Error value passed to the handler includes
all frames accumulated up to that boundary.

For fallible list processor invocations, an Error produced by the referenced
flow bubbles through the list processor. The runtime appends operation context
for the list processor, including the processor name and element index when that
index is available, before the Error crosses the enclosing flow boundary.

12.6 Recoverable Failure Contract

The following failures are recoverable in v0.3 and must be represented as Error
values, not hidden host exceptions:

* invalid input data accepted by a fallible constructor,
* missing data reported by a fallible lookup,
* numeric domain errors from fallible math processors,
* adapter boundary failures from fallible adapter operations,
* failed commit activation inside fallible or self-handling flows, and
* an Error returned by a fallible flow referenced by a list processor.

Inside a plain total flow, an unresolved fallible expression is a static type
error. Inside a fallible flow or self-handling flow, the same failure bubbles as
an Error according to section 10.3.

Implementations may use host exceptions internally only as an implementation
technique. Such exceptions must be caught at the implementation boundary and
translated into Error values whenever the failure is recoverable under this
specification.

⸻

13. Statements

13.1 Binding

bound_name = expression

13.2 Flow Definition

flow FlowName(target_param: ParamType) -> OutputType { ... }

13.3 Commit

commit target_name

target_name must resolve to a previously bound committable target.

13.4 Return

return return_expression

There is no run keyword and no export keyword in v0.3.

⸻

14. Pipe Operator

DAGDSL uses |> as first-argument threading syntax.

14.1 Form

left_value |> Namespace.operation(argument_two)

desugars to:

Namespace.operation(left_value, argument_two)

14.2 Flow Form

left_value |> FlowName(argument_two)

desugars to:

FlowName(left_value, argument_two)

14.3 Fallibility-Aware Pipe

Inside fallible or self-handling flows, |> is fallibility-aware.

If the left side is fallible and the next processor or flow expects the successful type, success continues and failure bubbles automatically.

Examples:

file_text = input_path
  |> File.read()
  |> UTF8.String.new()
quotient = left
  |> Math.div(right)

Pipe is surface syntax only. The lowered graph remains explicit and typed.

⸻

15. Type Checking Rules

15.1 General Rules

* Every expression has a statically known type in v0.3.
* Every binding name must be unique within its scope.
* References must resolve to a prior binding or a named flow.
* User bindings and flow names must not shadow predeclared built-in type,
  namespace, constructor, processor, adapter, or keyword names.
* Member projection must target a type that defines the projected field.
* Processor arguments must satisfy the declared processor signature.

15.2 Flow-Reference Rules

* Only pure named flows may be used where a pure data processor requires a flow reference.
* Adapter boundary parameters may accept effectful flows as defined by the adapter signature.
* HTTP.Server.new(..., handler: Name, onError: ErrorFlow) must verify:
    * Name has signature (HTTP.Request) -> HTTP.Response or (HTTP.Request) -> HTTP.Response!
    * ErrorFlow has signature (Error) -> HTTP.Response
* Nested flows do not implicitly capture outer bindings.

15.3 Fallibility Rules

* A plain total flow (-> T) may not contain unresolved expressions of type U!.
* A fallible flow (-> T!) may contain unresolved expressions of type U!, which auto-propagate on failure.
* A self-handling flow (-> T onError ErrorHandler) may contain unresolved expressions of type U!, which bubble to the flow-level handler.
* Any flow containing commit must declare either -> T! or -> T onError ErrorHandler.

15.4 Match Rules

* An exact-value case head must be equality-comparable and compatible with the
  selector type.
* A range case head must be a Range<T> whose ordered-comparable endpoint type is
  compatible with the selector type.
* A predicate flow case head must use when FlowName syntax and must reference a
  pure total flow with signature (T) -> Bool.
* All branch expressions must unify to one type.

15.5 Commit Rules

* commit may target only a bound value of type Effect or another activatable adapter type such as HTTP.Server.
* A committed target activates exactly once per commit statement evaluation.

15.6 Null, Absence, and Record Rules

* The null literal has only type Null.
* Null is assignable only where the expected type is exactly Null.
* No type in v0.3 is implicitly nullable.
* v0.3 has no optional record fields.
* A Record{ ... } value must contain exactly the declared fields when checked
  against an exact record type.
* Missing map data is represented by Map.get returning Error, not by returning
  null.
* A future revision may add first-class optional data, such as Option<T> or
  broader union types, but v0.3 does not include those features.

⸻

16. Operational Semantics for HTTP

16.1 Startup

1. Evaluate top-level bindings.
2. Construct the server with HTTP.Server.new(...).
3. Activate the server with commit target_server.

16.2 Request Lifecycle

1. The HTTP adapter parses incoming wire bytes into an HTTP.Request value.
2. If request parsing or request-level boundary handling fails before the handler is entered, the adapter invokes its mandatory onError flow.
3. The configured handler flow is invoked with the request.
4. Pure graph nodes are evaluated.
5. Unresolved fallible expressions bubble outward.
6. If an uncaught error reaches the adapter boundary, the adapter invokes its mandatory onError flow.
7. If the selected path explicitly evaluates to effects and commits them, they execute in source order.
8. The final HTTP.Response is serialized and sent.

⸻

17. Surface Grammar

This grammar is normative for v0.3. It is written in EBNF-style notation.
Semantic rules in sections 10, 12, and 15 may reject programs that are
syntactically valid.

Reserved lower-case keywords are:

* flow
* return
* commit
* onError
* when
* true
* false
* null

Built-in type, namespace, constructor, processor, and adapter names are
predeclared. User bindings and flow names must not shadow predeclared names.

Program                  = Separator*, TopLevelBody?, Separator* ;
TopLevelBody             = TopLevelStatement, (Separator+, TopLevelStatement)* ;
TopLevelStatement        = BindingStatement | FlowDefinition | CommitStatement | ReturnStatement ;
BindingStatement         = Identifier, "=", Expression ;
CommitStatement          = "commit", Identifier ;
ReturnStatement          = "return", Expression ;
FlowDefinition           = "flow", Identifier, "(", ParameterList?, ")", "->", FlowReturnSignature, Block ;
FlowReturnSignature      = TypeExpression | NonFallibleTypeExpression, "onError", Identifier ;
ParameterList            = Parameter, (",", Parameter)*, ","? ;
Parameter                = Identifier, ":", TypeExpression ;

Block                    = "{", Separator*, FlowBody, Separator*, "}" ;
FlowBody                 = ReturnStatement | FlowPrefixStatements, Separator+, ReturnStatement ;
FlowPrefixStatements     = FlowPrefixStatement, (Separator+, FlowPrefixStatement)* ;
FlowPrefixStatement      = BindingStatement | FlowDefinition | CommitStatement ;

Expression               = PipeExpression ;
PipeExpression           = PrimaryExpression, ("|>", PipeTarget)* ;
PipeTarget               = QualifiedName, "(", ArgumentList?, ")" ;

PrimaryExpression        = MatchExpression | CallExpression | ProjectionExpression | ReferenceExpression | LiteralExpression | ParenthesizedExpression ;
MatchExpression          = "Match", ".", "new", "(", Expression, ",", MatchCaseList, ")" ;
MatchCaseList            = MatchCase, (",", MatchCase)*, ","? ;
MatchCase                = MatchCaseHead, ":", Expression ;
MatchCaseHead            = "_" | "when", Identifier | Expression ;
CallExpression           = QualifiedName, "(", ArgumentList?, ")" ;
ProjectionExpression     = Identifier, ".", Identifier, (".", Identifier)* ;
ReferenceExpression      = Identifier ;
ParenthesizedExpression  = "(", Expression, ")" ;

ArgumentList             = Argument, (",", Argument)*, ","? ;
Argument                 = NamedArgument | Expression ;
NamedArgument            = Identifier, ":", Expression ;

LiteralExpression        = StringLiteral | IntegerLiteral | FloatLiteral | BooleanLiteral | NullLiteral ;
BooleanLiteral           = "true" | "false" ;
NullLiteral              = "null" ;

TypeExpression           = NonFallibleTypeExpression, "!"? ;
NonFallibleTypeExpression = BaseTypeExpression | GenericTypeExpression | RecordTypeExpression ;
BaseTypeExpression       = QualifiedName ;
GenericTypeExpression    = QualifiedName, "<", TypeList, ">" ;
TypeList                 = TypeExpression, (",", TypeExpression)*, ","? ;
RecordTypeExpression     = "Record", "{", FieldTypeList?, "}" ;
FieldTypeList            = FieldType, (",", FieldType)*, ","? ;
FieldType                = Identifier, ":", TypeExpression ;

QualifiedName            = Identifier, (".", Identifier)* ;
Separator                = Newline | ";" ;

Disambiguation rules:

1. Pipe expressions are left associative and have lower precedence than calls,
   projections, references, literals, parenthesized expressions, and Match.new.
2. An identifier chain followed immediately by "(" is parsed as CallExpression.
3. An identifier chain containing "." and not followed by "(" is parsed as
   ProjectionExpression.
4. A single identifier not followed by "(" or "." is parsed as
   ReferenceExpression.
5. In an argument list, Identifier ":" Expression is parsed as NamedArgument;
   all other arguments are positional Expression values.
6. In a Match.new case head, "_" is the default case and "when" Identifier is a
   predicate-flow case head. All other case heads are expressions and are
   classified by the type checker as exact-value or range case heads.
7. A self-handling flow return signature uses NonFallibleTypeExpression on the
   left side of onError. A fallible self-handling return such as T! onError H is
   invalid in v0.3.

⸻

18. Minimal Standard Library for v0.3

18.1 Constructors

* Scalar.new
* String.new
* Bytes.new
* Int.new
* Float.new
* Bool.new
* List.new
* Map.new
* Record.new
* Range.new
* Error.new
* File.Path.new
* UTF8.String.new
* HTTP.Server.new
* HTTP.Response.Text.new
* HTTP.Response.Bytes.new
* String.FromScalars.new

18.2 Pure Processors

* Scalar.eq
* String.concat
* String.upper
* String.lower
* String.length
* String.eq
* Int.to_string
* Range.contains
* Range.is_empty
* Math.add
* Math.sub
* Math.mul
* Math.lt
* Math.lte
* Math.gt
* Math.gte
* Math.eq
* Math.sum
* Math.product
* Map.has_key
* Record.merge
* List.map
* List.filter
* List.fold
* List.flat_map
* List.length
* List.unfold

18.3 Fallible Constructors and Processors

The following operations are fallible. Each listed failure is recoverable and
must produce an Error with at least the listed kind string. Implementations may
include additional fields in Error.input when useful.

* UTF8.String.new(bytes) -> String!
  * Failure: bytes are not valid UTF-8.
  * Required Error.kind: String.new("UTF8.InvalidBytes")
* Math.div(left_value, right_value) -> Int!
  * Failure: right_value is zero.
  * Required Error.kind: String.new("Math.DivideByZero")
* Math.mod(left_value, right_value) -> Int!
  * Failure: right_value is zero.
  * Required Error.kind: String.new("Math.ModuloByZero")
* Math.min(number_list) -> Int!
  * Failure: number_list is empty.
  * Required Error.kind: String.new("Math.EmptyList")
* Math.max(number_list) -> Int!
  * Failure: number_list is empty.
  * Required Error.kind: String.new("Math.EmptyList")
* Map.get(target_map, target_key) -> ValueType!
  * Failure: target_key is not present in target_map.
  * Required Error.kind: String.new("Map.MissingKey")
* File.read(file_path) -> Bytes!
  * Failure: the host cannot read the path as bytes, including not-found,
    permission-denied, and non-file cases.
  * Required Error.kind: String.new("File.ReadFailed")
* Any list processor invocation whose referenced pure flow is fallible.
  * Failure: the referenced flow returns Error for an element.
  * Required Error.kind: the original Error.kind returned by the referenced
    flow. The Error trace must add list processor context as described in
    section 12.5.

Commit activation failures are recoverable inside fallible and self-handling
flows. A failed File.append commit must produce Error.kind
String.new("File.AppendFailed"). A failed File.write commit must produce
Error.kind String.new("File.WriteFailed"). A failed HTTP.Server commit must
produce Error.kind String.new("HTTP.ServerStartFailed"). At top level, a failed
commit aborts activation and the host reports the Error diagnostic.

18.4 Committable Targets

* File.append
* File.write
* HTTP.Server.new

⸻

19. Examples

19.1 Dynamic Filtering with Context

flow IsAboveThreshold(context_data: Record{ limit: Int }, target_value: Int) -> Bool {
    return Math.gt(target_value, context_data.limit)
}
raw_numbers = List.new(Int.new(4), Int.new(12), Int.new(20))
threshold_config = Record.new(limit: Int.new(10))
filtered_numbers = raw_numbers |> List.filter(threshold_config, IsAboveThreshold)
return filtered_numbers

19.2 Round-trip String Processing

flow IsVowel(target_scalar: Scalar) -> Bool {
    return Match.new(target_scalar,
        Scalar.new("a"): Bool.new(true),
        Scalar.new("e"): Bool.new(true),
        Scalar.new("i"): Bool.new(true),
        Scalar.new("o"): Bool.new(true),
        Scalar.new("u"): Bool.new(true),
        _: Bool.new(false)
    )
}
flow ExtractVowels(input_text: String) -> String {
    vowel_scalars = input_text |> List.filter(IsVowel)
    return vowel_scalars |> String.FromScalars.new()
}

19.3 Evaluating Branches to Committable Effects

flow HandleStatus(status_code: Int, request_data: String) -> HTTP.Response onError StatusError {
    success_log = File.Path.new(String.new("/logs/success.log"))
    error_log = File.Path.new(String.new("/logs/error.log"))
    target_effect = Match.new(status_code,
        Int.new(200): File.append(success_log, request_data),
        _: File.append(error_log, request_data)
    )
    commit target_effect
    return HTTP.Response.Text.new(
        status: status_code,
        body: String.new("Status processed")
    )
}
flow StatusError(error: Error) -> HTTP.Response {
    return HTTP.Response.Text.new(
        status: Int.new(500),
        body: String.new("status handling failed")
    )
}

19.4 Count a Letter in a File with Self-Handling Flow

flow IsMatchingScalar(target_scalar: Scalar) -> Bool {
    return Scalar.eq(target_scalar, Scalar.new("a"))
}
flow CountFallback(error: Error) -> Int {
    return Int.new(0)
}
flow CountLetterA(file_path: File.Path) -> Int onError CountFallback {
    file_text = file_path
        |> File.read()
        |> UTF8.String.new()
    match_count = file_text
        |> List.filter(IsMatchingScalar)
        |> List.length()
    return match_count
}

19.5 HTTP Division with Bubbled Error Handling

flow Calculate(request: HTTP.Request) -> HTTP.Response! {
    denominator = Int.new(0)
    result_value = Int.new(10) |> Math.div(denominator)
    return HTTP.Response.Text.new(
        status: Int.new(200),
        body: result_value |> Int.to_string()
    )
}
flow CalculatorHttpError(error: Error) -> HTTP.Response {
    return Match.new(error.kind,
        String.new("Math.DivideByZero"): HTTP.Response.Text.new(
            status: Int.new(400),
            body: String.new("division by zero")
        ),
        _: HTTP.Response.Text.new(
            status: Int.new(500),
            body: String.new("internal error")
        )
    )
}
server = HTTP.Server.new(
    port: Int.new(8080),
    handler: Calculate,
    onError: CalculatorHttpError
)
commit server

19.6 Pragmatic List Sum

numbers = List.new(Int.new(1), Int.new(2), Int.new(3), Int.new(4))
total = numbers |> Math.sum()
return total

⸻

20. Conformance Matrix

An implementation should maintain a conformance suite with at least the cases
below. "Reject" means parser rejection for syntax-only failures and type-checker
rejection for syntactically valid programs that violate semantic rules.

| Area | Positive case | Negative case |
| --- | --- | --- |
| Program and separators | A file with bindings, flows, commits, and a final top-level return separated by newlines or semicolons. | A top-level return followed by another top-level statement. |
| FlowDefinition and Block | `flow AddOne(value: Int) -> Int { return Math.add(value, Int.new(1)) }` | A flow body with no return statement or a return statement before a later binding. |
| FlowReturnSignature | `flow Read(path: File.Path) -> Bytes! { return File.read(path) }` | `flow Bad(path: File.Path) -> Bytes! onError Handle { return File.read(path) }` |
| ParameterList | `flow Pair(left: Int, right: Int,) -> Int { return Math.add(left, right) }` | Duplicate parameter names in one flow definition. |
| TypeExpression | `Map<String, List<Int!>>` and `Record{ limit: Int }` in parameter or field positions. | `Int!!`, a record field without `:`, or a generic type with an empty type list. |
| LiteralExpression | String, integer, float, boolean, and null literals produce their declared literal types. | A malformed string literal or using `Null` as a null literal. |
| QualifiedName and CallExpression | `HTTP.Response.Text.new(status: Int.new(200), body: String.new("ok"))` | `Unknown.Namespace.call()` when Unknown.Namespace is not predeclared or bound. |
| ProjectionExpression | `request.body` where request has type HTTP.Request. | `value.missing` where value's type has no field missing. |
| PipeExpression | `Int.new(10) |> Math.div(Int.new(2))` | `Int.new(10) |> Math.div` without a call argument list. |
| MatchExpression | `Match.new(value, Int.new(1): String.new("one"), _: String.new("other"))` | A Match.new without `_` or with branches that do not unify to one output type. |
| Predicate match case | `Match.new(value, when IsPositive: String.new("yes"), _: String.new("no"))` where IsPositive is pure and `(Int) -> Bool`. | `when WriteLog` where WriteLog is effectful or does not return Bool. |
| NamedArgument | `HTTP.Response.Text.new(status: Int.new(200), body: String.new("ok"))` | Duplicate named arguments for the same parameter. |
| Fallible binding in total flow | A total flow that only uses total expressions. | `flow Bad(path: File.Path) -> Bytes { return File.read(path) }` |
| Fallible propagation | `flow Read(path: File.Path) -> Bytes! { bytes = File.read(path); return bytes }` | Treating a `T!` expression as `T` outside a fallible or self-handling boundary. |
| Self-handling flow | `flow SafeRead(path: File.Path) -> Bytes onError EmptyBytes { return File.read(path) }` | A self-handling flow whose handler is effectful or has the wrong output type. |
| Adapter onError | `HTTP.Server.new(port: Int.new(8080), handler: Handle, onError: HttpError)` | `HTTP.Server.new(port: Int.new(8080), handler: Handle)` |
| Recoverable standard failures | `Math.div(Int.new(1), Int.new(0))` produces Error.kind `Math.DivideByZero` inside a fallible boundary. | A recoverable divide-by-zero escaping as a host exception visible to DAGDSL code. |
| Null rejection | `nothing = null` binds a value whose type is Null. | Passing `null` or `Null` to `String.concat`, `Math.add`, `File.Path.new`, or `HTTP.Response.Text.new` where another type is required. |
| Record exactness | `Record.new(limit: Int.new(10))` satisfies `Record{ limit: Int }`. | A value checked against `Record{ limit: Int }` that omits limit, adds another field, or sets `limit: null`. |
| Map construction | `Map.new(Record.new(key: String.new("a"), value: Int.new(1)))` satisfies `Map<String, Int>`. | `Map.new(Record.new(name: String.new("a"), value: Int.new(1)))` because the entry field must be named key. |
| Comparable domains | `Range.new(start: Int.new(1), end: Int.new(3), include_start: true, include_end: true)` and `Map<String, Int>`. | `Range<Bytes>` or `Map<List<Int>, String>` because those key/endpoint types are not comparable in v0.3. |
| Map absence | `Map.has_key(headers, key)` is total and `Map.get(headers, key)` is fallible. | Treating a missing map value as a nullable successful value. |
| List flow references | `List.filter(numbers, IsPositive)` where IsPositive is pure and `(Int) -> Bool`. | Passing an anonymous function, an effectful flow, or a flow with the wrong parameter order to a list processor. |
| Commit activation | `effect = File.append(path, text)` followed by `commit effect` in a fallible or self-handling flow. | `commit text` where text has type String, or any commit inside a plain total flow. |
| Predeclared name shadowing | `flow UserHandler(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(status: Int.new(200), body: String.new("ok")) }` | `flow String(value: Int) -> Int { return value }` or `HTTP = String.new("x")`. |
| Grammar examples | Every example in section 19 parses under section 17 and type-checks under section 15. | Any section 19 example relying on an undeclared non-standard processor or undocumented precedence rule. |

21. Summary

DAGDSL v0.3 is a typed declarative dataflow language with:

1. explicit .new(...) construction,
2. pure processors,
3. named flows only,
4. typed adapters such as File and HTTP,
5. a single pure Match.new(...) construct for branching,
6. a first-argument threading operator |>,
7. a pragmatic Math namespace,
8. a single ordered collection type List<T>,
9. explicit activation through commit for side effects and activatable adapters,
10. atomic Null and built-in nominal composite Error,
11. built-in fallible types via T!,
12. error bubbling by default,
13. self-handling flows through signature-level onError, and
14. mandatory adapter-level onError flows to guarantee a boundary catch.

This specification is sufficient to begin implementation of:

* a lexer,
* a parser,
* a type checker,
* a graph IR lowerer,
* a runtime with error bubbling and commit, and
* initial File and HTTP adapters.
