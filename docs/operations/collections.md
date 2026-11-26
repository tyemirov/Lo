# Collections And Sequential Flows

[Documentation home](../README.md) · [Operation index](README.md)

These operations construct collections and matchers, transform elements, and execute named callbacks.
`T`, `U`, `K`, `V`, and `S` are signature notation, not source type aliases.
`Record` in signatures denotes an exact record shape derived by the compiler.
`context...` denotes explicit context arguments before the final named callback. List and Flow callback operations accept positional arguments only.

List callbacks are pure. Flow callbacks can observe host state and activate commits.
List operations that consume an existing list also accept a String as a scalar sequence.
`Flow.worklist` requires a List for its initial items.

Argument failures propagate under the language fallibility rules. Host work and value quotas apply to every operation.
Host quota failures are terminal.

## On This Page

- [Flow.fold](#flowfold)
- [Flow.worklist](#flowworklist)
- [List.at](#listat)
- [List.filter](#listfilter)
- [List.flat_map](#listflat_map)
- [List.fold](#listfold)
- [List.length](#listlength)
- [List.map](#listmap)
- [List.new](#listnew)
- [List.slice](#listslice)
- [List.sort](#listsort)
- [List.unfold](#listunfold)
- [Map.get](#mapget)
- [Map.has_key](#maphas_key)
- [Map.new](#mapnew)
- [Match.apply](#matchapply)
- [Match.Case.new](#matchcasenew)
- [Match.new](#matchnew)
- [Range.contains](#rangecontains)
- [Range.is_empty](#rangeis_empty)
- [Range.new](#rangenew)
- [Record.merge](#recordmerge)
- [Record.new](#recordnew)

## Flow.fold

```text
Flow.fold(items: List<T>, initial_state: S, context..., Step) -> S or S!
```

Invoke the named flow in element order with an explicit accumulator. An empty list returns the initial state.
The callback receives `context..., state: S, item: T` and returns `S` or `S!`.
Unlike List callbacks, this callback can observe host state and activate commits. Confirmed effects remain after a later failure.

**Failures:** An unhandled callback failure stops the remaining items. Error progress retains the last completed state and next index.

**Authority:** The callback can observe host state or activate commits only under the relevant explicit host grants.

```lo
flow Add(total: Int, value: Int) -> Int { return Math.add(total, value) }
return Flow.fold(List.new(1, 2, 3), 0, Add)
```

[Contract](../../specification.md#88-sequential-flow-processor)

## Flow.worklist

```text
Flow.worklist(items: List<T>, initial_state: S, context..., Step) -> S or S!
```

Visit ordered items and any additional items returned by the callback. New items precede the pending items in their supplied order.
The callback receives `context..., state: S, item: T` and returns `Record{state: S, items: List<T>, stop: Bool}`, optionally fallible.

The state type itself cannot be fallible. A true `stop` discards all pending and newly returned items.
An empty initial list returns the initial state without a callback. Consumed items release resources unless retained elsewhere.

**Failures:** A callback failure stops traversal. Error progress retains completed and pending counts and the last completed state. Host quota failures remain terminal.

**Authority:** The callback can observe host state or activate commits only under the relevant explicit host grants.

```lo
flow Visit(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} {
    return Record.new(state: Math.add(state, item), items: List.new(), stop: false)
}
return Flow.worklist(List.new(1, 2, 3), 0, Visit)
```

[Contract](../../specification.md#89-dynamic-worklists-and-host-resource-quotas)

## List.at

```text
List.at(values: List<T>, index: Int) -> T!
```

Return the selected element. For a negative index, add the list length once.
The adjusted index must satisfy `0 <= index < length`. Stored fallible elements keep their failure tags without another enclosing tag.
A String input returns a Scalar result.

**Failures:** `List.InvalidIndex` for an invalid adjusted index. A selected stored failure remains a failure.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return List.at(List.new("a", "b"), -1)
```

[Contract](../value-processors.md#collections)

## List.filter

```text
List.filter(target_list: List<T>, context..., Predicate) -> List<T> or List<T>!
```

Invoke the named predicate once per element in order. Keep elements whose predicate result is true.
The predicate receives `context..., item: T` and returns `Bool` or `Bool!`. Keep the original element order and provenance.

**Failures:** A failed callback stops the operation before the next callback. The result is fallible when the callback is fallible.

**Authority:** The callback must be pure. Any device operation in the callback still requires its explicit device grant.

```lo
flow Positive(value: Int) -> Bool { return Math.gt(value, 0) }
return List.filter(List.new(-1, 0, 2), Positive)
```

[Contract](../../specification.md#87-list-processors)

## List.flat_map

```text
List.flat_map(target_list: List<T>, context..., Transform) -> List<U> or List<U>!
```

Invoke the named flow in element order. Concatenate its result lists in that order.
The callback receives `context..., item: T` and returns `List<U>` or `List<U>!`.

**Failures:** A failed callback stops the operation before the next callback. The result is fallible when the callback is fallible.

**Authority:** The callback must be pure. Any device operation in the callback still requires its explicit device grant.

```lo
flow Pair(value: Int) -> List<Int> { return List.new(value, value) }
return List.flat_map(List.new(1, 2), Pair)
```

[Contract](../../specification.md#87-list-processors)

## List.fold

```text
List.fold(target_list: List<T>, initial_state: S, context..., Step) -> S or S!
```

Replace the accumulator with each callback result in element order. An empty list returns the initial state.
The callback receives `context..., state: S, item: T` and returns `S` or `S!`.

**Failures:** A failed callback stops the operation before the next callback. The result is fallible when the callback is fallible.

**Authority:** The callback must be pure. Any device operation in the callback still requires its explicit device grant.

```lo
flow Add(total: Int, value: Int) -> Int { return Math.add(total, value) }
return List.fold(List.new(1, 2, 3), 0, Add)
```

[Contract](../../specification.md#87-list-processors)

## List.length

```text
List.length(target_list: List<T>) -> Int
```

Return the number of elements. A String input supplies its Unicode scalars and returns the scalar count.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return List.length(List.new(1, 2, 3))
```

[Contract](../../specification.md#87-list-processors)

## List.map

```text
List.map(target_list: List<T>, context..., Transform) -> List<U> or List<U>!
```

Invoke the named flow once per element in order. Return the callback results as a new list.
The callback receives `context..., item: T` and returns `U` or `U!`.

**Failures:** A failed callback stops the operation before the next callback. The result is fallible when the callback is fallible.

**Authority:** The callback must be pure. Any device operation in the callback still requires its explicit device grant.

```lo
flow Double(value: Int) -> Int { return Math.mul(value, 2) }
return List.map(List.new(1, 2, 3), Double)
```

[Contract](../../specification.md#87-list-processors)

## List.new

```text
List.new(values: T...) -> List<T>
```

Construct a list with one inferred element type. Only positional arguments are permitted.
An empty constructor requires an expected List type. A list can retain fallible elements without propagating each stored failure.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return List.new(1, 2, 3)
```

[Contract](../../specification.md#72-constructor-rules)

## List.slice

```text
List.slice(values: List<T>, start: Int, end: Int) -> List<T>!
```

Return the elements from `start` through the position before `end`. Negative bounds add the length once.
The adjusted bounds must satisfy `0 <= start <= end <= length`. Equal bounds return an empty list.
Keep element identities, fallible tags, and `File.Entry` provenance. A String input returns `List<Scalar>`.

**Failures:** `List.InvalidBounds` for invalid adjusted bounds.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return List.slice(List.new(1, 2, 3), 1, 3)
```

[Contract](../value-processors.md#collections)

## List.sort

```text
List.sort(values: List<T>, context..., Compare) -> List<T>
```

Return a stable order under a total, pure comparison flow. The comparison receives `context..., left: T, right: T` and returns Int.
A negative result places the left element first. A positive result places the right element first.
Zero keeps the original order. The callback must define a consistent comparison relation.

Keep element identities, stored failures, and `File.Entry` provenance.

**Failures:** No recoverable failure from this operation. The compiler rejects fallible or effectful comparison flows.

**Authority:** The callback must be pure. Any device operation in the callback still requires its explicit device grant.

```lo
flow Compare(left: Int, right: Int) -> Int { return Math.sub(left, right) }
return List.sort(List.new(3, 1, 2), Compare)
```

[Contract](../value-processors.md#collections)

## List.unfold

```text
List.unfold(seed_value: S, iteration_count: Int, context..., Step) -> List<T> or List<T>!
```

Invoke the named flow up to `iteration_count` times. Each result supplies the next state and one output element.
The callback receives `context..., state: S` and returns `Record{value: T, next_state: S}`, optionally fallible.
A zero or negative count returns an empty list without a callback invocation.

**Failures:** A failed callback stops the operation before the next callback. The result is fallible when the callback is fallible. A count above the host limit fails before callbacks.

**Authority:** The callback must be pure. Any device operation in the callback still requires its explicit device grant.

```lo
flow Next(state: Int) -> Record{value: Int, next_state: Int} {
    return Record.new(value: state, next_state: Math.add(state, 1))
}
return List.unfold(0, 3, Next)
```

[Contract](../../specification.md#87-list-processors)

## Map.get

```text
Map.get(target_map: Map<K, V>, target_key: K) -> V!
```

Return the stored value for the key. An absent key is an error, not null.
A stored fallible value keeps its failure tag without another enclosing tag.

**Failures:** `Map.MissingKey` when the key is absent. A selected stored failure remains a failure.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Map.get(Map.new(Record.new(key: "name", value: "Lo")), "name")
```

[Contract](../../specification.md#86-record-and-map-processors)

## Map.has_key

```text
Map.has_key(target_map: Map<K, V>, target_key: K) -> Bool
```

Return whether the map contains the supplied key. An absent key returns false rather than an error.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Map.has_key(Map.new(Record.new(key: "name", value: "Lo")), "name")
```

[Contract](../../specification.md#86-record-and-map-processors)

## Map.new

```text
Map.new(entries: Record{key: K, value: V}...) -> Map<K, V>
```

Construct a map from exact `Record{key: K, value: V}` entries. Keys have one equality-comparable type.
Permitted key types are Int, Scalar, String, Bool, and Null. Values have one inferred type.
For duplicate keys, the last entry supplies the value and the key keeps its first output position. An empty constructor needs an expected Map type.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Map.new(Record.new(key: "name", value: "Lo"))
```

[Contract](../../specification.md#72-constructor-rules)

## Match.apply

```text
Match.apply(request: T, matcher: Match<T, R>) -> R
```

Pass the request value to the stored predicates in order. The first true result selects its handler.
Invoke that handler once with the same request. When every predicate returns false, invoke the default handler.
Each application performs a fresh selection. Later predicates and unselected handlers remain unevaluated.

The result keeps the handler's exact type, including a fallible type.
Argument expressions follow the ordinary source evaluation order.

`Match.apply` also accepts `Match<T, R, Read>`. The compiler records a possible host read at application for that type.
The host checks read and device grants when the selected handler uses them.
Returned targets require a later explicit `commit` for activation.

```lo
flow Positive(value: Int) -> Bool { return Math.gt(value, 0) }
flow Double(value: Int) -> Int { return Math.mul(value, 2) }
flow Keep(value: Int) -> Int { return value }
matcher = Match.new(List.new(Match.Case.new(Positive, Double)), Keep)
return 4 |> Match.apply(matcher)
```

[Contract](../../specification.md#111-reusable-matchers)

## Match.Case.new

```text
Match.Case.new(predicate: (T) -> Bool, handler: (T) -> R) -> Match.Case<T, R>
```

Store a pure total named predicate and a named handler as an immutable case.
The signatures above describe callback parameters. The supplied arguments are named flow references.
Both callbacks receive one parameter of type `T`. The predicate returns `Bool`, and the handler returns `R`.
`R` can be fallible. A reading handler produces `Match.Case<T, R, Read>`.
Construction invokes zero callbacks. Handler commits belong in the calling flow.

```lo
flow Positive(value: Int) -> Bool { return Math.gt(value, 0) }
flow Double(value: Int) -> Int { return Math.mul(value, 2) }
return Match.Case.new(Positive, Double)
```

[Contract](../../specification.md#111-reusable-matchers)

## Match.new

```text
Match.new(cases: List<Match.Case<T, R>>, default: (T) -> R) -> Match<T, R>
```

Store the ordered cases and one required default handler as immutable data.
The default is a named flow with one parameter of type `T` and result type `R`.
A reading case or default produces `Match<T, R, Read>`.
`List.new` gives mixed pure and reading cases the common read-qualified case type.
Every case and the default share the same input type and exact result type, including fallibility.
An empty case list derives its type from the default handler.
Construction invokes zero predicates or handlers. Use `Match.apply` to evaluate a request.

```lo
flow Keep(value: Int) -> Int { return value }
matcher = Match.new(cases: List.new(), default: Keep)
return 4 |> Match.apply(matcher)
```

Matchers and cases can be stored in records and collections or passed through named flows.
Their automatic output contains callback names and case order. Diagnostic snapshots preserve the same description.

[Contract](../../specification.md#111-reusable-matchers)

## Range.contains

```text
Range.contains(target_range: Range<T>, target_value: T) -> Bool
```

Return whether the value falls inside the range under its endpoint type's order and inclusion flags.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Range.contains(Range.new(1, 4, true, false), 3)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Range.is_empty

```text
Range.is_empty(target_range: Range<T>) -> Bool
```

Return whether the range contains no value of its endpoint type. Open ranges between consecutive values are empty.
Consecutive scalars exclude surrogates. A String's immediate successor appends one U+0000 scalar.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Range.is_empty(Range.new(1, 2, false, false))
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Range.new

```text
Range.new(start: T, end: T, include_start: Bool, include_end: Bool) -> Range<T>
```

Construct an immutable range over Int, Scalar, or String endpoints of the same type.
The inclusion flags select whether the two endpoints participate. The constructor can return an empty range.
A start after the end is empty. Equal endpoints are empty unless both inclusion flags are true.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Range.new(1, 4, true, false)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Record.merge

```text
Record.merge(left_record: Record, right_record: Record) -> Record
```

Return an exact record with fields from both input records. The right record supplies each duplicate field.
A duplicate field can change type. The compiler derives the exact output shape from both inputs.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Record.merge(Record.new(name: "old", count: 1), Record.new(name: "Lo"))
```

[Contract](../../specification.md#86-record-and-map-processors)

## Record.new

```text
Record.new(field_name: T, ...) -> Record{field_name: T, ...}
```

Construct an exact structural record from named fields. Every field name must be unique.
The empty constructor returns `Record{}`. The result has exactly the supplied fields and their inferred types.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Record.new(name: "Lo", count: 3)
```

[Contract](../../specification.md#72-constructor-rules)

