Lo Draft Specification

This specification includes the v0.3 core and the implemented F002, F003, and F004 extensions.
Section 24 defines binary64 arithmetic, tensors, CPU training, and explicit device transfers.

1. Purpose

Lo is a typed declarative dataflow language for building transformation pipelines, file-processing jobs, and HTTP services.

The language is built around a simple discipline:

1. construct values and adapters with .new(...),
2. transform values through pure processors and named flows,
3. represent fallibility explicitly in the type system, and
4. activate side effects and activatable adapters explicitly with commit.

Lo is graph-first. A program describes data dependencies, branching, fallibility, and boundary activation. It does not describe imperative step-by-step control flow.

⸻

2. Design Principles

Lo follows these principles.

1. Explicit data over hidden behavior. Values, processors, flows, adapters, errors, and commits are separate concepts.
2. Construction is separate from activation. .new(...) constructs. commit activates only side effects and activatable adapters.
3. Named flows only. Lo does not support lambdas or anonymous functions in v0.3.
4. List<T> represents general-purpose sequences. Tensor<FP32> represents dense numeric arrays. Laziness is a runtime concern.
5. Pragmatic standard library. Common arithmetic and aggregate operations belong in Math. Users should not be forced to express routine math through folds.
6. Typed boundaries. The language exposes domain-oriented adapters such as File and HTTP, not generic IO.
7. Readable pipeline syntax. Lo uses |> as first-argument threading syntax.
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

Lo has five core semantic categories.

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

* pure, if it has no direct or indirect commit activation, or
* effectful, if it activates a target directly or through a flow call.

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

Lo does not expose a user-visible generic Result<T> type in v0.3.

An implementation may lower T! to an internal tagged result representation, but
that representation is not part of the surface language or public type system.
Recoverable operational failures must be represented as Error values. They must
not be exposed to Lo programs as hidden host-language exceptions.

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

commit is the only activation keyword in Lo.

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

CPU tensor execution uses the `cpu-fp32-strict` numeric profile.
Identical inputs produce identical numeric bits under the operation order in section 24.3.
Execution strategy cannot change arithmetic order, failures, or effects.

Lo values are immutable after construction. A binding names a value or a
committable target; it does not introduce mutable storage.

The runtime may evaluate pure graph nodes eagerly, lazily, incrementally, or in
parallel, provided the observable result is identical to an evaluation that
respects data dependencies and the rules in this specification. Evaluation
strategy is not observable to a Lo program except through explicit commit
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
An implementation may use such mechanisms internally only if observable Lo
semantics remain unchanged.

v0.3 exposes no shared mutable memory, thread creation, channel creation,
async/await syntax, lock syntax, concurrency annotations, or scheduler controls.

5.7 Binding and Value Passing

Tensor storage sharing preserves all prior values.
Storage identity and host pointers are not observable.

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
Lo program.

Lo has no address-of operation, dereference operation, pointer identity
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

Float denotes a finite IEEE 754 binary64 value.
Section 24.1 specifies arithmetic, comparison, and explicit conversion.
FP32 is a dtype marker accepted only in Tensor<FP32>.
FP32 has no standalone values or constructor.

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
* Tensor<FP32>
* Device
* Parameters<R>
* Gradients<R>
* Optimizer.SGD.Config
* Optimizer.AdamW.Config
* Optimizer.SGD.State<R>
* Optimizer.AdamW.State<R>
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
except through explicit comparison processors.
Float comparison processors do not make Float a valid map key or range endpoint.
Tensor<FP32> and Device have no equality or ordering operations.
Parameters, gradients, optimizer configs, and optimizer states also have no equality or ordering operations.
Their `R` argument requires a nonempty exact record of `Tensor<FP32>` fields.

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

List<T> is the general-purpose sequence type.
Tensor<FP32> is a separate immutable numeric array with dtype, shape, and device metadata.

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

Lo favors .new(...) for construction.

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
* Int.to_float(value: Int) -> Float!
* Float.to_int(value: Float) -> Int
* Float.to_string(value: Float) -> String

8.4 Math Processors

Lo includes a pragmatic Math namespace.

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

Section 24.1 defines Float arithmetic with explicit Float operands.
Section 24.3 defines CPU tensor arithmetic.

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
5. A pure flow has no direct or indirect commit activation. Section 22 specifies the transitive rule.
6. Only pure named flows may be passed to pure data processors.
7. Adapter boundary parameters may accept effectful flows as defined by the adapter signature.
8. Lo does not support anonymous functions or lambdas in v0.3.
9. A nested flow does not implicitly capture outer values. Any required value must be passed explicitly as a parameter.
10. A plain total flow (-> T) may not use unresolved fallible expressions.
11. A fallible flow (-> T!) automatically propagates unresolved fallible expressions via error bubbling.
12. A self-handling flow (-> T onError ErrorHandler) executes like a fallible flow internally, but resolves any bubbled error through ErrorHandler before publishing its result.
13. ErrorHandler in a self-handling flow must be a pure total named flow with signature (Error) -> T.
14. An effectful flow must declare either -> T! or -> T onError ErrorHandler.

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

Lo uses a single pure match constructor:

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

Lo uses |> as first-argument threading syntax.

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
* Any effectful flow must declare either -> T! or -> T onError ErrorHandler.

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

```ebnf
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
ProjectionExpression     = Identifier, ".", MemberLabel, (".", MemberLabel)* ;
ReferenceExpression      = Identifier ;
ParenthesizedExpression  = "(", Expression, ")" ;

ArgumentList             = Argument, (",", Argument)*, ","? ;
Argument                 = NamedArgument | Expression ;
NamedArgument            = MemberLabel, ":", Expression ;

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
FieldType                = MemberLabel, ":", TypeExpression ;

QualifiedName            = Identifier, (".", MemberLabel)* ;
MemberLabel              = Identifier | "flow" | "return" | "commit" | "onError" | "when" | "true" | "false" | "null" ;
Separator                = Newline | ";" ;
```

Disambiguation rules:

1. Pipe expressions are left associative and have lower precedence than calls,
   projections, references, literals, parenthesized expressions, and Match.new.
2. An identifier chain followed immediately by "(" is parsed as CallExpression.
3. An identifier chain containing "." and not followed by "(" is parsed as
   ProjectionExpression.
4. A single identifier not followed by "(" or "." is parsed as
   ReferenceExpression.
5. In an argument list, parse `MemberLabel ":" Expression` as `NamedArgument`.
   Parse all other arguments as positional expressions.
6. In a Match.new case head, "_" is the default case and "when" Identifier is a
   predicate-flow case head. All other case heads are expressions and are
   classified by the type checker as exact-value or range case heads.
7. A self-handling flow return signature uses NonFallibleTypeExpression on the
   left side of onError. A fallible self-handling return such as T! onError H is
   invalid in v0.3.

17.1 Lexical Rules

Source files contain UTF-8 text without a byte order mark.
Identifiers use ASCII letters, ASCII digits, and underscores.
The first character must be a letter or underscore.
The single underscore is reserved for a match default.
Keywords cannot serve as identifiers.

Names are case-sensitive.
Keywords can serve as member labels, as specified in section 22.1.

Spaces and horizontal tabs separate tokens.
A newline is LF or CRLF.
A lone carriage return is invalid.
The sequence `//` starts a comment outside a string literal.
A comment ends before the next newline or at the end of the file.
Block comments are not part of the grammar.

Strings use double quotes.
A string contains Unicode scalar values and the escape sequences listed below.
Raw control characters are invalid inside a string.
A string cannot contain a raw newline.

| Escape | Value |
| --- | --- |
| `\"` | Double quote |
| `\\` | Backslash |
| `\n` | Line feed |
| `\r` | Carriage return |
| `\t` | Horizontal tab |
| `\b` | Backspace |
| `\f` | Form feed |
| `\uXXXX` | One Unicode scalar specified by four hexadecimal digits |
| `\UXXXXXXXX` | One Unicode scalar specified by eight hexadecimal digits |

An escape must identify a Unicode scalar.
Surrogate code points and values above `U+10FFFF` are invalid.
No other escape sequence is valid.

Integer literals use decimal digits with an optional leading minus sign.
Zero is `0` or `-0`.
Other integer literals start with a nonzero digit after the optional sign.

A float literal adds a decimal fraction, an exponent, or both to this integer form.
A decimal point must have digits on each side.
An exponent starts with `e` or `E`, followed by an optional sign and at least one digit.
Numeric separators and nondecimal numeric literals are invalid.
The lexer retains the exact numeric text.

Newlines inside argument lists, parameter lists, type argument lists, record types, and parenthesized expressions are whitespace.
Outside these constructs, newlines separate statements.
A newline directly before or after `|>` continues the same expression.
Blank lines and comments can occur in such a continuation.
A semicolon always separates statements and cannot continue an expression.
Whitespace can occur between the components of a qualified name and before its call parenthesis.

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
* Device.new
* Tensor.new

18.2 Pure Processors

The Float and Tensor processors in section 24 are also pure.

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

Section 24 defines the additional numeric and tensor failures.

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
| Recoverable standard failures | `Math.div(Int.new(1), Int.new(0))` produces Error.kind `Math.DivideByZero` inside a fallible boundary. | A recoverable divide-by-zero escaping as a host exception visible to Lo code. |
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

Lo v0.3 is a typed declarative dataflow language with:

1. explicit .new(...) construction,
2. pure processors,
3. named flows only,
4. typed adapters such as File and HTTP,
5. a single pure Match.new(...) construct for branching,
6. a first-argument threading operator |>,
7. a pragmatic Math namespace,
8. general-purpose List<T> sequences and dense Tensor<FP32> arrays,
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

22. Verifier Contract Details

22.1 Names and Flow References

Each block declares all its flow names before verification of its statements.
A flow name is visible in its declaration block and nested blocks.
A nested flow name is not visible outside its declaration block.
Parameters and local bindings share the namespace of their flow body.
Local bindings can reference only earlier value bindings in the same invocation.
Flow bodies cannot capture values from another invocation or from top-level bindings.

Flow references are valid only in declared callback and handler positions.
They are not ordinary values and cannot form aliases through bindings.
The verifier rejects cycles through calls, callbacks, and handler references.
This rule includes references stored in adapter construction.
Adapter construction does not execute a stored callback.

Record field names and member names can use reserved keywords.
Named argument labels can also use reserved keywords.
These labels do not declare variables.

22.2 Transitive Effects

A flow is effectful when its evaluated body can activate a commit directly or through an invoked flow.
List callbacks and match predicates participate in the same analysis.
An unused nested definition does not make its enclosing flow effectful.
Construction of an adapter does not activate its handlers.
The verifier checks all flow bodies, including unused flows.

Every effectful flow must have a fallible output or a self-handler.
A pure callback must not invoke an effectful flow indirectly.
Match selectors, case heads, and branches must not invoke effectful flows.
A branch can construct a committable target for an outer commit.
Boundary reads have a separate summary and do not constitute commit activation.
The runtime must not speculate boundary reads.

22.3 Fallible Values

A fallible value has a success tag or a failure tag.
The failure payload is an `Error` value.
`Error` by itself is ordinary nominal data.
In a context that expects `T!`, a value of type `T` becomes a tagged success.
In that context, an `Error` becomes a tagged failure when `T` is not `Error`.
An `Error` supplied where `Error!` is expected becomes a tagged success.

Explicit expected types can retain tagged values in parameters, record fields, and collection elements.
Thus, `List<Int!>` contains tagged elements, while `List<Int>!` has one tag for the complete list.
An expected fallible type retains the tag instead of automatic propagation.
Other consumers of `T!` can obtain `T` only inside a fallible or self-handling flow.
The graph contains an explicit propagation operation for such a consumer.

For `Map<K, T!>`, `Map.get` returns `T!` with one failure tag.
A present key returns its stored fallible value.
A missing key returns `Map.MissingKey` as a failure of the same type.
Conversion from an exact record to bare `Record` preserves an enclosing failure tag.

A fallible or self-handling flow propagates fallible binding results before it publishes the binding.
A plain total flow rejects a fallible binding result.
At top level, a binding retains its fallible type.
Top-level composition cannot implicitly remove that type modifier.
A final top-level return can publish a fallible result.

Branches with types `T` and `T!` unify to `T!`.
Other branch types must be equal after contextual construction.
The verifier does not add optional fields or widen exact record shapes.
A return from a fallible flow uses its declared output as the expected type.
A return from a self-handling flow permits propagation to its declared handler.

22.4 Constructor and Argument Contracts

`Int.new`, `String.new`, `Float.new`, and `Bool.new` accept one value of their output type.
`Bytes.new(value: String)` encodes its input as UTF-8 bytes.
`Scalar.new` accepts a Scalar or a statically known String with exactly one Unicode scalar.
The verifier rejects a String input whose length it cannot establish.
`Float` uses finite IEEE 754 binary64 values in the reference implementation.
The verifier rejects float literals outside that finite representation.
`Float.new` preserves its Float input without implicit integer conversion.
`Tensor.new` converts each Float input to FP32 explicitly.
Section 24.2 defines its shape and element-count boundaries.
Sections 24.6 and 24.7 define Parameter schemas, gradient boundaries, and validated optimizer configs.

Fixed signatures accept positional arguments, named arguments, or a positional prefix followed by named arguments.
A positional argument cannot follow a named argument.
Each parameter receives exactly one argument.
Arguments evaluate in source order, including arguments supplied by a pipe.
The pipe supplies the first positional argument.

`List.new`, `Map.new`, and `String.concat` accept positional arguments only.
`Record.new` accepts named fields only.
Record fields must be unique.
`List.new()` and `Map.new()` need an expected collection type.
List callbacks use positional arguments with the flow reference last.

`Record.merge` accepts two exact records.
Its result keeps the right field when a field occurs in each input.
The output field types come from these selected fields.
Bare `Record` accepts an exact record of any shape, but does not expose unknown fields for projection.

22.5 Match Contracts

A match has exactly one default, which must be its final case.
Exact case heads must be total and equality-comparable.
Range heads must be total with endpoints of the selector type.
A predicate must be a pure total flow with one parameter of the selector type.
The verifier checks every case, including cases that cannot execute for a constant selector.
The runtime evaluates heads in source order until the first match.
It evaluates only the selected branch.

⸻

23. Runtime Contracts

The interpreter and native backend use these runtime contracts.
The two backends use the same evaluation order, errors, authorization rules, and logical execution limits.

23.1 Numeric and Collection Results

Integer operations use exact mathematical integers within the host execution budget.
Division removes the fractional part toward zero.
The following relation defines the remainder for a nonzero divisor:

```text
left = Math.div(left, right) * right + Math.mod(left, right)
```

A nonzero remainder has the sign of the left operand.
Division by zero produces `Math.DivideByZero`.
Remainder calculation with a zero divisor produces `Math.ModuloByZero`.

The sum of an empty integer list is zero.
The product of an empty integer list is one.
The minimum and maximum of an empty integer list produce `Math.EmptyList`.

Map construction evaluates all entries in source order.
For a repeated key, the last entry supplies the value.
The key retains its first position in serialized map output.
A lookup returns the stored fallible tag when the map value type is fallible.
It does not add another fallible tag.

List processors invoke callbacks in element order.
A failed callback stops the processor before its next invocation.
An empty fold returns its initial state.
Unfold uses the previous callback result as its next state.
A zero or negative unfold count produces an empty list without callback invocation.
A count above the host limit produces a host failure before callback invocation.

23.2 Comparison and Ranges

Integer comparison uses mathematical order.
Scalar comparison uses Unicode scalar order.
String comparison uses scalar order at the first unequal position.
A string precedes a longer string that has the complete shorter string as its prefix.

A range contains values between its endpoints under their type's order.
Each endpoint participates only when its corresponding inclusion flag is true.
A start after the end produces an empty range.
Equal endpoints produce an empty range unless both inclusion flags are true.
An open range between consecutive values is also empty.

Consecutive integers differ by one.
Consecutive scalars exclude the Unicode surrogate interval.
The immediate successor of a string is that string with one final U+0000 scalar.
String case conversion uses the Unicode tables of the pinned Go toolchain.
It applies the simple upper or lower mapping to each scalar.

23.3 Evaluation Order

The runtime evaluates statements in source order.
It evaluates every binding in an entered body, including bindings without later references.
A binding reference uses the stored value without another evaluation of its initializer.
Each flow invocation has its own parameter and binding values.

Call arguments evaluate in source order, independent of their named parameter positions.
A pipe evaluates its left expression before its explicit arguments.
Match evaluation follows section 22.5.
The runtime does not evaluate unselected branches or subsequent case heads after a match.

Every evaluated `File.read` performs a new boundary read.
Separate reads of one path can return different contents.
The runtime does not combine reads or move them across branch, invocation, or handler boundaries.

An unresolved failure stops the current evaluation at its propagation operation.
Later arguments and statements in that evaluation do not execute.
The first failure reached in evaluation order takes precedence.
A handler begins only after propagation reaches its boundary.

Section 22.3 controls stored fallible values.
A top-level binding can retain a failure as data while later statements execute.
A fallible or self-handling flow propagates an unresolved binding failure even when later statements do not reference that binding.
A top-level return with an outer failure tag produces an unhandled Lo error.
Failure tags inside returned containers remain data.

23.4 Runtime Error Context

An operation error retains its required kind and immediate input record.
Fixed operation inputs use the canonical parameter names from the built-in catalog.
An explicit `Error.new` keeps its supplied input record and starts with an empty trace.
Stored errors retain their trace until propagation adds a frame.

A flow boundary adds one frame before invocation of its self-handler.
A fallible flow also adds one frame before publication of a failed result.
The frame contains the flow name, the recorded failure operation, and the flow's named input values.
A caller records the failed callee as its operation.
The input record can be empty for a flow without parameters.

A failed list callback adds its flow frame before the list processor frame.
The processor frame contains the enclosing flow name, processor name, and zero-based index.
Its input also contains `element` when the processor has an input list.
A fold or unfold frame also contains `state`.
The processor frame precedes the enclosing flow boundary frame.
At top level, the processor frame uses an empty flow name.

An unavailable operation name uses an empty string.
Unavailable input context uses an empty record.
Trace updates construct a new error value and do not change stored errors.
Authorization denial, cancellation, and execution limits are host failures.
Lo handlers cannot catch these host failures.

23.5 Host Execution Contract

The host supplies adapter authorization and execution limits.
A successful check or inspection does not grant authorization.
Each boundary operation uses its actual runtime target for authorization.
A calculated path receives no authorization from its source expression alone.

The reference CLI denies file reads unless a supplied directory root contains the resolved path.
Relative program paths resolve from the invocation directory.
Path normalization removes `.` and resolves `..` before root selection.
The most specific matching root supplies the file operation.
Root access rejects symlinks that escape that root.
It also rejects absolute symlinks.
An authorized path that cannot supply bytes produces `File.ReadFailed`.
UTF-8 decoding rejects invalid bytes with `UTF8.InvalidBytes`.

The compiler design specifies CLI authorization inputs, resource limits, and result serialization.
Host limits do not change the declared fallibility of Lo operations.
An exceeded limit aborts execution with a host diagnostic.

23.6 File and Server Activation

File effect construction does not access a file.
An evaluated commit activates its target once.
Two evaluated commits of the same target cause two activations.
Commits execute in source order within their invocation.
A failed commit stops the current body before later statements execute.
An applicable self-handler can return a result to the caller, as specified in section 23.4.

`File.append` appends the UTF-8 bytes of its text input.
`File.write` replaces the file contents with its byte input.
Both operations create the target file if it does not exist.
They do not create missing parent directories.
A failed activation uses the error kind specified in section 18.3.
Its input record contains `file_path` and `file_content`.

A later failure does not reverse a completed file effect.
A failed write can leave partial contents.
Commit sequences have no transaction and no automatic retry.
Each HTTP request has its own invocation order.
The runtime does not specify an order across different requests.

Server construction stores its port and handler references without a listener.
An evaluated server commit creates a new listener.
Valid ports are integers from `0` through `65535`.
Port `0` lets the operating system select an available port.
An invalid port or a failed authorized listener operation produces `HTTP.ServerStartFailed`.
Its input record contains `port`.
The host capability contract controls the listen address and authorized ports.

23.7 HTTP Request and Response Processing

Each HTTP request starts a new invocation of the configured handler.
The request fields have the types in section 9.3.
The adapter validates request text as UTF-8 before handler entry.
The path contains the decoded URL path without the query.
The body contains bytes without implicit text decoding.

The header map starts with the `Host` entry.
Other entries use canonical HTTP header names in sorted order.
Repeated header values use `, ` as their separator.
Repeated `Cookie` values use `; ` as their separator.

| Boundary condition | Error kind |
| --- | --- |
| Invalid UTF-8 in request text | `HTTP.InvalidRequest` |
| Request body exceeds the host limit | `HTTP.RequestTooLarge` |
| Request body cannot be read completely | `HTTP.RequestReadFailed` |
| Invalid response status or body | `HTTP.InvalidResponse` |

Request errors contain available method and path bytes in `Error.input`.
Response errors contain the response fields in `Error.input`.
An uncaught Lo error from the request handler keeps its original kind and trace.
Each of these errors enters the mandatory adapter `onError` handler.

Before `onError` entry, the adapter adds one trace frame with an empty flow name and an input record that contains `port`.
Request errors and propagated handler errors use operation `HTTP.request`.
Response validation errors use operation `HTTP.respond`.
An error from a self-handling flow reaches the adapter only if that flow does not return a successful response.

Response status codes are integers from `200` through `599`.
Responses with status codes `204`, `205`, and `304` must have an empty body.
The response to `HEAD` omits body bytes on the connection.
Section 9.2 specifies the empty headers in constructed responses.
The HTTP transport can add protocol headers.

An invalid response from `onError` is a fatal host failure.
The adapter does not invoke `onError` again or construct a substitute response.
Host authorization failures and execution limits bypass Lo handlers.
The request handler and `onError` share one execution budget.

The host HTTP parser can reject malformed protocol input before a Lo request exists.
Such protocol rejections do not enter Lo handlers.
An unusable connection prevents delivery of an error response.
The compiler design specifies request limits, cancellation, startup, and shutdown for the reference CLI.


24. Numeric, Tensor, and Device Extension

F002 through F007 implement Float arithmetic, CPU tensors, reverse-mode autodiff, Parameters, gradients, and optimizers.
F001 implements these operations in native executables.
The reference interpreter and native artifacts use the same numeric and runtime contracts.
All operations in this section are pure under the rule that prohibits commit activation.
Device resource boundaries have a separate transitive effect summary.
F008 adds device resources and explicit transfers.
F009, F010, F011, and I001 own device arithmetic, deferred execution, precision, and complete training acceptance.

24.1 Binary64 Arithmetic

| Signature | Result |
| --- | --- |
| `Float.add/sub/mul/div(left: Float, right: Float) -> Float!` | Binary64 arithmetic. |
| `Float.sqrt(value: Float) -> Float!` | Binary64 square root. |
| `Float.eq/lt/lte/gt/gte(left: Float, right: Float) -> Bool` | Numeric comparison, with equal positive and negative zero. |
| `Int.to_float(value: Int) -> Float!` | Nearest finite binary64 value. |
| `Float.to_int(value: Float) -> Int` | Exact integer after truncation toward zero. |
| `Float.to_string(value: Float) -> String` | Shortest round-trip decimal, with signed zero kept. |

Each arithmetic operation rounds separately with round-to-nearest, ties-to-even.
An implementation must not combine multiplication and addition into one rounding operation.
Underflow produces subnormal values or signed zero.
Nonfinite results produce `Numeric.NonFinite`.
Division by positive or negative zero produces `Numeric.DivideByZero`.
A negative square-root input produces `Numeric.Domain`.
The square root of negative zero is negative zero.

Int and Float operations require explicit conversion between their domains.
Integer arithmetic remains exact.
Float construction remains an identity operation on a finite Float input.
Float comparisons do not extend the comparable domains for maps, ranges, or Match case heads.

24.2 Tensor Values and Shape Boundaries

`Tensor<FP32>` represents an immutable, contiguous, row-major array of finite FP32 values.
Its shape and device location are metadata, not type arguments.
`Tensor`, `Tensor<Int>`, and standalone `FP32` are invalid types.
`Device` is an immutable nominal descriptor.
The following tables use `T` to mean `Tensor<FP32>`.
This abbreviation is not a source type alias.

| Signature | Contract |
| --- | --- |
| `Device.new(selector: String) -> Device!` | Accept `cpu`, `cuda:0`, `cuda-sim:0`, or `metal:0`. |
| `Tensor.new(values: List<Float>, shape: List<Int>) -> T!` | Convert finite binary64 inputs to FP32 and construct a CPU tensor. |
| `Tensor.shape(value: T) -> List<Int>` | Return ordered dimensions. |
| `Tensor.rank(value: T) -> Int` | Return the number of dimensions. |
| `Tensor.dtype(value: T) -> String` | Return `fp32`. |
| `Tensor.device(value: T) -> Device` | Return the actual location descriptor. |
| `Tensor.to(value: T, device: Device) -> T!` | Transfer to the selected location with unchanged FP32 bits and shape. |
| `Tensor.to_float(value: T) -> Float!` | Convert one rank-zero element exactly to binary64. |
| `Tensor.values(value: T) -> List<Float>!` | Convert all elements exactly to binary64 in row-major order. |

Device construction validates the selector without device initialization.
An invalid selector produces `Device.InvalidSelector`.
The current software backend executes explicit transfers for `cuda-sim:0`.
The Metal interpreter build executes explicit transfers and synchronous FP32 computation for `metal:0`.
The standard build and native artifacts report `Device.Unavailable` for authorized Metal access.
Authorized `cuda:0` access produces `Device.Unavailable` until F009 supplies that backend.
Unsupported backend arithmetic produces `Device.UnsupportedOperation` before submission.

An empty shape has one element and rank zero.
A zero dimension makes the element count zero.
Every dimension must be nonnegative and fit a signed 64-bit integer.
For nonempty tensors, the dimension product must also fit a signed 64-bit integer.
Invalid dimensions, element-count overflow, and constructor count mismatch produce `Tensor.InvalidShape`.
FP32 conversion uses ties-to-even rounding and rejects nonfinite results with `Numeric.NonFinite`.

Known shape conflicts produce `check.tensor-shape` with a source span and expected and actual metadata.
Shape facts propagate through literals, bindings, records, projections, and named flow arguments and results.
The checker joins facts across Match branches.
General arithmetic and list callbacks can leave dimensions unknown.
Dynamic constructor and operation boundaries validate unknown shapes before output allocation.
CPU and Metal kernels consume the validated shapes and axes.

Reshape preserves the element count and row-major element order.
It can share immutable storage.
Transpose constructs a new row-major result.
All previous bindings retain their values after these operations.
No Lo operation exposes tensor storage identity or host pointers.

24.3 Tensor Operation Set

| Signature | Contract |
| --- | --- |
| `Tensor.add/sub/mul/div(left: T, right: T) -> T!` | Elementwise arithmetic with trailing-dimension broadcasting. |
| `Tensor.sqrt(value: T) -> T!` | Elementwise square root. |
| `Tensor.relu(value: T) -> T!` | Keep positive inputs. Replace all other inputs with positive zero. |
| `Tensor.matmul(left: T, right: T) -> T!` | Multiply `[m,k]` by `[k,n]` to produce `[m,n]`. |
| `Tensor.broadcast(value: T, shape: List<Int>) -> T!` | Broadcast to exactly the requested shape. |
| `Tensor.reshape(value: T, shape: List<Int>) -> T!` | Change shape with the same element count. |
| `Tensor.transpose(value: T, axes: List<Int>) -> T!` | Permute every axis exactly once. |
| `Tensor.sum(value: T, axes: List<Int>) -> T!` | Sum selected axes and remove those dimensions. |
| `Tensor.mean(value: T, axes: List<Int>) -> T!` | Sum selected axes, then divide by the selected element count. |

Broadcast dimensions align from the right.
Missing leading dimensions have size one.
Aligned dimensions must be equal or have size one.
Size one can broadcast to zero.
Zero agrees only with zero or one.
Rank-zero tensors can broadcast to any supported shape.

Reduction axes must be unique, nonnegative, and strictly increasing.
An empty axis list preserves the shape and values, including signed zero.
Sum over an empty region returns positive zero.
Mean over an empty region produces `Tensor.EmptyReduction`, including when the output is empty.
Invalid axes produce `Tensor.InvalidShape`.
Incompatible shapes and invalid runtime rank for `Tensor.to_float` produce `Tensor.ShapeMismatch`.

Matrix multiplication requires rank-two operands.
A zero contracted dimension produces positive zeros.
Zero output dimensions produce an empty tensor.
Each dot product multiplies and then adds in increasing contracted-index order.
Reductions add elements in input row-major order, starting from positive zero.
Every multiplication, addition, division, and square root rounds separately to FP32.

Every numeric intermediate must remain finite.
Numeric failures use the error kinds in section 24.1.
The earliest failing row-major element determines the elementwise error index.
Failures stop dependent execution and preserve operation and source context.
No partial tensor result is published.

Mean squared error composes subtraction, elementwise multiplication, and mean.
The executable `examples/tensor_forward.lo` composes a two-layer ReLU network and returns rank-zero loss `3`.
F005 and F013 implement derivatives for these operations on CPU and Metal.
F009 retains the NVIDIA backend.

The `metal-fp32` profile uses the same shapes, operations, logical charges, and finite-value checks.
It normalizes subnormal arithmetic operands and results to positive zero.
It also normalizes signed arithmetic zeros to positive zero.
Division and square-root domain checks apply after normalization.
An active square-root derivative rejects a normalized zero input.
Transfers, layout operations, constant construction, and empty-axis reductions preserve their input bits.
Matrix products and reductions use fixed traversal order, with no fused multiplication and addition.
Device tensors remain at their selected location until an explicit transfer.
The profile requires identical bits across repeated executions with the same build, device, system, and inputs.

24.4 Resource and Diagnostic Contracts

Tensor rank is at most eight.
Each dimension and each tensor element count is at most 1,000,000.
The existing expanded-value limits remain 16 MiB per value and 64 MiB per invocation.
Tensor weight is `16 + 4*N + 8*rank + len(dtype) + len(selector)` bytes.
Device weight is `16 + len(selector)` bytes.
Containers count repeated tensor values repeatedly.

The runtime charges tensor work and checks result weight before output allocation.
Construction, conversion, elementwise operations, broadcast, and transpose add one step per output element.
Reduction adds one step per input element.
Mean also adds one step per output division.
An empty axis list keeps these logical charges even when storage is shared.
Matrix multiplication adds `2*m*n*k` steps.
Reshape and metadata add no element steps.

Host budget failures produce `runtime.limit` and bypass Lo handlers.
Host cancellation produces `runtime.cancelled` and bypasses Lo handlers.
Neither condition causes tensor truncation.
The compiler design owns allocation accounting and the inspection schema.

Tensor diagnostics replace tensor contents with metadata records.
The fields are `tensor_type: String`, `dtype: String`, `shape: List<Int>`, and `device: String`.
The same transformation applies inside lists, maps, records, and fallible values.
These records retain no tensor storage.
Public tensor results contain complete row-major data under the compiler design serialization contract.

24.5 Reverse-mode Autodiff

`Autodiff.grad(targets, context..., Loss)` returns `Record{value: Tensor<FP32>, gradients: R}!`.
`R` is a nonempty exact record whose fields all have type `Tensor<FP32>`.
`Loss` is a named flow with signature `(context..., R) -> Tensor<FP32>!`.
A total loss flow with the same successful output type is also valid.
Arguments are positional.
The loss receives context first and the selected record last.

The loss must return a rank-zero tensor.
A known positive rank produces `check.autodiff-loss` before execution.
An unknown positive rank produces `Autodiff.InvalidLoss` before reverse evaluation.
Context values are constants for differentiation, even when they share storage with selected fields.
Each selected field has an independent gradient accumulator.
Disconnected fields receive positive-zero tensors with the original shape.

The loss graph permits these source constructs:

- Tensor operations in the VJP table below and tensor metadata operations.
- Literals, record construction, record projection, `List.new`, and `Int.new`.
- Total integer `Math` operations.
- Calls to named helpers within this same subset.

The checker rejects unsupported operations with `check.autodiff-unsupported` and the originating source span.
Restrictions apply to all eager bindings and all indirect calls.
Boundary reads, commits, tensor construction, host numeric observation, and device transfers are unsupported.
Match branches, list callbacks, self-handling flows, nested gradients, Float arithmetic, and optimizer operations are unsupported.
An outer flow can handle the final gradient failure.

| Forward operation | Contribution from output gradient `g` |
| --- | --- |
| `add(a,b)` | `g` for each operand. |
| `sub(a,b)` | `g` for `a`, and `-g` for `b`. |
| `mul(a,b)` | `g*b` for `a`, and `g*a` for `b`. |
| `div(a,b)` | `g/b` for `a`, and `-(g/b)*(a/b)` for `b`, in that order. |
| `sqrt(a)` | `g/(2*sqrt(a))`. |
| `relu(a)` | `g` where `a > 0`, otherwise positive zero. |
| `matmul(A,B)` | `matmul(g,transpose(B))` and `matmul(transpose(A),g)`. |
| `broadcast(a)` | Sum across repeated and added dimensions, then restore the input shape. |
| `reshape(a)` | Restore the input shape. |
| `transpose(a,axes)` | Apply the inverse permutation. |
| `sum(a,axes)` | Insert size-one dimensions, then broadcast to the input shape. |
| `mean(a,axes)` | Divide by the reduced count, then apply the sum rule. |

Binary elementwise rules reduce broadcast contributions to each operand shape.
ReLU has a zero subgradient at zero.
An active square-root input equal to zero produces `Autodiff.UndefinedDerivative`.
An unused derivative path does not produce this error.
Forward operations on unused bindings still execute and can fail.

The reverse pass uses reverse stable forward order.
Operand contributions use increasing operand-index order.
Each dependency edge contributes once, including both edges of `Tensor.mul(x,x)`.
Each selected accumulator starts at positive zero.
All generated arithmetic uses the FP32 rules in section 24.3.
Generated results and primitive work receive the resource charges in section 24.4.

The derivative plan permits at most 100,000 expression visits, 100,000 nodes, and 256 active helper invocations.
An exceeded compiler bound produces `check.autodiff-limit`.
Saved tensor storage has a 32 MiB tape limit.
Shared views count their storage once.
The existing 64 MiB cumulative value limit also applies.
The tape belongs to one gradient call and becomes unreachable after success, failure, or cancellation.
No failure publishes a partial loss or gradient record.

24.6 Parameters and Gradient Lifecycle

`R` has the exact tensor record contract from section 24.5.
Each field name identifies one Parameter within that record schema.
Field names use lexicographic order for schema checks and diagnostics.
Two fields remain separate Parameters when their values or private storage are equal.
Repeated use of one field accumulates contributions into that field's gradient.

| Signature | Contract |
| --- | --- |
| `Parameters.new(values: R) -> Parameters<R>!` | Construct immutable Parameters. |
| `Parameters.values(parameters: Parameters<R>) -> R` | Return the exact tensor record. |
| `Gradients.new(parameters: Parameters<R>, values: R) -> Gradients<R>!` | Validate the gradient schema. |
| `Gradients.values(gradients: Gradients<R>) -> R` | Return the exact tensor record. |
| `Gradients.zero(parameters: Parameters<R>) -> Gradients<R>!` | Construct positive-zero gradients. |
| `Gradients.add(left: Gradients<R>, right: Gradients<R>) -> Gradients<R>!` | Add matching fields in FP32. |
| `Gradients.scale(gradients: Gradients<R>, factor: Float) -> Gradients<R>!` | Convert the factor to FP32 once, then multiply each field. |
| `Autodiff.stop(value: Tensor<FP32>) -> Tensor<FP32>` | Preserve the value and remove its derivative path. |

Constructors require a nonempty record with exact keys and FP32 tensor fields.
Each tensor record requires one shared device location.
Gradient construction and addition validate each shape before numeric work.
A metadata conflict produces `Gradient.SchemaMismatch`.
The checker rejects missing keys, additional keys, duplicate keys, and incompatible field types.
FP32 scale conversion and arithmetic retain the numeric failures from section 24.1.

Reset means construction of a new `Gradients.zero` value.
Earlier Parameters and gradients remain unchanged.
Named flows and `List.fold` carry these values explicitly.
There is no hidden Parameter registry or mutable gradient accumulator.

For a mean loss, multiply each microbatch gradient by its example count divided by the complete batch count.
Add the scaled gradients in microbatch source order.
An empty complete batch is invalid.
The complete-batch comparison uses absolute tolerance `1e-6` and relative tolerance `1e-5`.
Different reduction groups need not produce identical bits.

Parameters and gradients have 16 bytes of wrapper weight plus their exact record weight.
Their JSON `value` is an object of named, typed tensors.
Error snapshots replace their tensor fields with the metadata records in section 24.4.

24.7 Pure Optimizer Transitions

| Signature | Contract |
| --- | --- |
| `Optimizer.SGD.Config.new(learning_rate: Float) -> Optimizer.SGD.Config!` | Validate the FP32 rate. |
| `Optimizer.AdamW.Config.new(learning_rate: Float, beta1: Float, beta2: Float, epsilon: Float, weight_decay: Float) -> Optimizer.AdamW.Config!` | Validate all FP32 config fields. |
| `Optimizer.SGD.State.new(parameters: Parameters<R>, config: Optimizer.SGD.Config) -> Optimizer.SGD.State<R>!` | Construct state at step zero. |
| `Optimizer.AdamW.State.new(parameters: Parameters<R>, config: Optimizer.AdamW.Config) -> Optimizer.AdamW.State<R>!` | Construct state with zero moments and unit beta powers. |
| `Optimizer.SGD.step(state: Optimizer.SGD.State<R>, gradients: Gradients<R>) -> Optimizer.SGD.State<R>!` | Return the next SGD state. |
| `Optimizer.AdamW.step(state: Optimizer.AdamW.State<R>, gradients: Gradients<R>) -> Optimizer.AdamW.State<R>!` | Return the next AdamW state. |

Config constructors convert each field to FP32 once, then validate the converted values.
The learning rate and epsilon must be positive.
Each beta must be in `[0,1)`.
Weight decay must be nonnegative.
The product of learning rate and weight decay must be finite FP32.
Invalid config produces `Optimizer.InvalidConfig`, including overflow, underflow to zero, and betas rounded to one.

State exposes only `parameters: Parameters<R>` and `step: Int` through source projection.
SGD stores its immutable config.
AdamW also stores first and second moment records and rank-zero beta-power tensors.
JSON contains the complete state, including these private fields.
Config fields use typed Float values equal to the validated FP32 values.

SGD applies `p_next = sub(p, mul(rate, g))` to each field.
It has no momentum or weight decay.
AdamW uses this arithmetic order for each field:

```text
m_next = add(mul(beta1, m), mul(sub(1, beta1), g))
v_next = add(mul(beta2, v), mul(sub(1, beta2), mul(g, g)))
b1power_next = mul(b1power, beta1)
b2power_next = mul(b2power, beta2)
m_hat = div(m_next, sub(1, b1power_next))
v_hat = div(v_next, sub(1, b2power_next))
decayed = sub(p, mul(mul(rate, weight_decay), p))
update = mul(rate, div(m_hat, add(sqrt(v_hat), epsilon)))
p_next = sub(decayed, update)
step_next = step + 1
```

Beta powers advance once per step and apply to every field.
Every arithmetic operation rounds to FP32.
Epsilon is outside the square root.
Weight decay applies to all fields and is separate from moment calculations.
The optimizer square root is ordinary arithmetic and permits zero moments.

Each step validates the complete gradient schema before numeric work.
The next state retains the same keys and shapes.
Earlier Parameters, gradients, and states remain unchanged after success or failure.
No failure publishes a partial state or permits a dependent commit.
The step count increases only in the returned successful state.
`List.fold` supplies each update with the previous returned state.

Live AdamW moments and beta powers have a 32 MiB limit.
Generated primitives receive the work and value charges in section 24.4.
Config and state weights add 16 bytes to their complete exact record weights.
Diagnostic snapshots retain metadata for nested tensors and optimizer fields.


24.8 Device Resources and Explicit Transfers

`Tensor.to` requires an exact host grant for every non-CPU location it uses.
The CLI accepts one non-CPU grant with `run` or `serve`.
Use `--allow-device cuda:0`, `--allow-device cuda-sim:0`, or `--allow-device metal:0` for the selected backend.
CPU operations require no device grant.
A duplicate grant or unsupported selector produces `cli.invalid-invocation` with exit status 2.
Authorization denial produces `runtime.capability-denied` before device initialization, allocation, or transfer.
Authorization does not change the source selector.

Each run or HTTP request has a separate device session and its allocation receipts.
The session initializes its backend at first authorized access.
Transfers complete before execution continues.
A transfer keeps its source allocation live until completion.
Same-location transfers share the original immutable storage without a copy.

A transfer between different compute locations produces an error.
`Device.Unavailable`, `Device.AllocationFailed`, and `Device.TransferFailed` are recoverable errors.
An unsupported device profile produces `Device.UnsupportedProfile`.
Metal library or pipeline creation can produce `Device.LibraryFailed`.
Computation command construction failures produce `Device.ExecutionFailed`.
A failed submitted Metal command makes its session unusable.
An unusable session produces `Device.Lost` and rejects subsequent device resource access without submission.
CPU error handling and resource cleanup remain available after device loss.
Cancellation remains a host diagnostic when a device boundary also reports a failure.
A failed transfer publishes no output tensor.

`Tensor.values` and `Tensor.to_float` require CPU data and otherwise produce `Tensor.HostObservationRequired`.
CLI publication recursively requires CPU tensors in records, lists, maps, Parameters, gradients, and optimizer state.
A device tensor at publication produces `runtime.tensor-publication` with exit status 2 before any result bytes.
Error snapshots contain tensor metadata and never cause numeric downloads.

The live resource ledger counts each backing allocation once at its actual reserved size.
Tensor views share one receipt, but expanded value weight still counts each occurrence.
The total limit is 64 MiB per invocation across CPU and device storage.

Tape storage and optimizer moments each have a 32 MiB limit within that total.
The optimizer limit includes beta powers.
Library workspace has a 16 MiB limit within the total.
A resource budget failure produces `runtime.limit` outside Lo handlers.
Transfers charge one logical step per copied element.

Flow exits release temporary allocations.
Callback loops release superseded state and keep output values that remain reachable.
Success, failure, and cancellation close the invocation's remaining receipts and backend.
A cleanup failure prevents successful result publication.
The [device resource contract](docs/device-resources.md) defines the backend interface and validation evidence.
The [Metal transfer contract](docs/metal-transfers.md) defines the current Metal build and storage.
The [Metal arithmetic contract](docs/metal-arithmetic.md) defines `metal-fp32`, operation support, and computation evidence.
