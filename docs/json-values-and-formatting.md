# JSON values and text formatting

This document specifies the F031 contract.
Compiler checks and focused native acceptance passed on Darwin and Linux QEMU.
Final shared stack CI passed.
All selected operations are pure and require no host authority or commit activation.
Application field names, report text, and selection policy remain in application flows.

## Selected operations

The `Record` constraint below requires a concrete record with only total `String`, `Int`, `Float`, or `Bool` fields.
It does not introduce a source type or generic declaration.

| Signature | Recoverable errors |
| --- | --- |
| `JSON.decode(data: Bytes) -> JSON.Value!` | `JSON.InvalidData` |
| `JSON.get(value: JSON.Value, key: String) -> JSON.Value!` | `JSON.TypeMismatch`, `JSON.FieldAbsent` |
| `JSON.at(value: JSON.Value, index: Int) -> JSON.Value!` | `JSON.TypeMismatch`, `JSON.InvalidIndex` |
| `JSON.string(value: JSON.Value) -> String!` | `JSON.TypeMismatch` |
| `JSON.bool(value: JSON.Value) -> Bool!` | `JSON.TypeMismatch` |
| `JSON.int(value: JSON.Value) -> Int!` | `JSON.TypeMismatch`, `JSON.InvalidNumber` |
| `JSON.float(value: JSON.Value) -> Float!` | `JSON.TypeMismatch`, `JSON.InvalidNumber` |
| `JSON.kind(value: JSON.Value) -> String` | None |
| `JSON.length(value: JSON.Value) -> Int!` | `JSON.TypeMismatch` |
| `Int.parse(text: String) -> Int!` | `Int.InvalidText` |
| `Float.format_fixed(value: Float, places: Int) -> String!` | `Numeric.InvalidPrecision` |
| `String.format(template: String, fields: Record) -> String!` | `String.InvalidFormat` |

Recoverable errors use the ordinary operation location and input snapshot contract.
Cancellation and exhausted resource limits remain terminal.
An invalid native value fails at the native boundary.
These operations do not convert a terminal failure into a recoverable error.

## Immutable JSON values

`JSON.Value` is an immutable nominal type with a private representation.
It has no projected fields and carries no executable authority.
Its six kinds are `null`, `bool`, `number`, `string`, `array`, and `object`.
`JSON.kind` returns exactly one of those strings.

`JSON.get` requires an object and an exact decoded key.
Keys use Unicode scalar identity without normalization or case conversion.
An absent key produces `JSON.FieldAbsent`.
`JSON.at` requires an array and a nonnegative index, with zero as the first index.
A negative or out-of-range index produces `JSON.InvalidIndex`.

`JSON.length` accepts arrays and objects only.
It returns the array length or object member count.
Each extraction operation requires its named kind.
An incorrect kind produces `JSON.TypeMismatch` before lookup, index validation, or numeric conversion.

## Strict decoding

`JSON.decode` accepts exactly one JSON document.
JSON whitespace can occur before and after the root value.
The parser accepts the standard JSON string escapes and number grammar.
It accepts an escaped NUL inside a string.

The parser rejects these inputs with `JSON.InvalidData`:

- A byte-order mark, comments, or trailing commas.
- A second document or other trailing content.
- Raw control characters inside strings.
- Invalid UTF-8 or unpaired surrogate escapes.
- Duplicate decoded object keys at any depth.

For example, an object cannot contain both `"a"` and `"\u0061"` as keys.
Those spellings decode to the same key.
Different keys remain distinct when Unicode normalization or case conversion would make them equal.
Decoded object data does not depend on source whitespace or member order.

The JSON and checkpoint operations use one strict iterative parser.
The shared representation retains Boolean values and exact number tokens.
The parser does not replace invalid Unicode or accept duplicate keys.
Checkpoint schema lookup uses exact keys instead of obsolete folded-key lookup.
No permissive parser or compatibility switch remains.

Checkpoint decoding retains its own type validation after parsing.
It also retains exact comparison with canonical checkpoint bytes.
Valid JSON alone does not make a valid checkpoint.
Malformed checkpoint data continues to produce `Checkpoint.InvalidData`.

## Exact number tokens

Decoding validates number syntax and keeps each source token without an implicit Float conversion.
For example, `9007199254740993`, `1e400`, and `1e-4000` remain exact number tokens after decoding.
Numeric extraction is a separate operation.

`JSON.int` accepts only integer tokens without a fraction or exponent.
It normalizes negative zero to zero.
It rejects `1.0` and `1e0` with `JSON.InvalidNumber`, although both denote an integral value.
The existing maximum integer size of `1_048_576` bits remains a terminal resource limit.

`JSON.float` uses rounding to select the nearest binary64 value for the exact decimal value, with ties to even.
A nonfinite result produces `JSON.InvalidNumber`.
A nonzero exact value that becomes zero after rounding also produces `JSON.InvalidNumber`.
Finite subnormal results are valid.
An explicit negative zero keeps its sign.

These conversion rules do not change Float arithmetic underflow behavior.
Existing Float arithmetic allows subnormal results and signed zero.

| JSON number token | `JSON.int` | `JSON.float` |
| --- | --- | --- |
| `-0` | Integer zero. | Negative zero. |
| `1.0` | `JSON.InvalidNumber`. | Float one. |
| `1e0` | `JSON.InvalidNumber`. | Float one. |
| `1e400` | `JSON.InvalidNumber`. | `JSON.InvalidNumber` because the result is nonfinite. |
| `1e-4000` | `JSON.InvalidNumber`. | `JSON.InvalidNumber` because rounding produces zero from a nonzero value. |

## Diagnostic representation

The typed envelope is `{"type":"JSON.Value","value":NODE}`.
Each node has exactly `kind` and `value` fields.
Null and Boolean nodes use JSON null and Boolean payloads.
String nodes use JSON string payloads.
Number nodes use JSON string payloads that contain the exact source token.

Array payloads contain child nodes in source order.
Object payloads map decoded keys to child nodes in ascending Unicode scalar order.
Diagnostic snapshots retain data without adding authority.
This representation is diagnostic output, not an additional source constructor.

For example, the diagnostic payload for decoded `{"amount":1.0}` is:

```json
{
  "type": "JSON.Value",
  "value": {
    "kind": "object",
    "value": {
      "amount": { "kind": "number", "value": "1.0" }
    }
  }
}
```

## Depth, storage, and lifetime

The canonical native context limits gain `json_depth`.
The corresponding program option is `--max-json-depth`.
The default maximum is `256` nested containers.
Zero disables this depth maximum.
A scalar root has depth zero, and a root array or object has depth one.
The same depth limit applies to JSON and checkpoint decoding.

Parsing, traversal, diagnostic serialization, snapshots, and release must use iterative algorithms or an equivalent representation without recursive call-stack growth.
Disabling the depth maximum must not expose a stack overflow during any of those operations.
Subvalue extraction must charge all retained backing allocations.
A small extracted value cannot retain large unrelated data without the corresponding live-resource charge.

The `list_items` limit applies to each array length and object member count.
Each node has a weight of `16` bytes plus its payload and child weights.
Each object member adds `16` bytes plus its decoded key bytes.
String payload weight is its UTF-8 byte count.
Number payload weight is its exact token byte count.
Null and Boolean nodes have no additional payload weight.

Existing value, live-resource, and cumulative data limits apply to these weights.
The runtime reserves temporary parser and duplicate-key storage before allocation.
It releases temporary reservations after success or failure.
It validates arithmetic before each allocation or count increase.

Work charges cover input bytes, produced nodes, key comparisons, numeric conversion, and output bytes.
The runtime checks cancellation during bounded work increments.
Resource failure cannot produce a partial JSON value or a partial formatted result.

## Decimal integer text

`Int.parse` accepts exactly the grammar `[+-]?[0-9]+`.
Leading zeros are valid, and every representation of zero becomes integer zero.
Whitespace, separators, base prefixes, Unicode digits, a bare sign, and an empty string produce `Int.InvalidText`.

Integer-bit, value, work, and temporary-storage limits apply before expensive conversion.
Leading zeros still count as input work.
This text grammar is separate from the stricter JSON number grammar.

## Fixed Float text

`Float.format_fixed` accepts `places` from `0` through `1000`, inclusive.
Other precision values produce `Numeric.InvalidPrecision`.
The result uses a decimal point and contains no exponent or group separator.
It contains exactly the selected number of digits after the point.
At precision zero, it contains no decimal point.

The operation uses rounding to convert the actual binary64 value to the nearest decimal output, with ties to even.
For example, precision two converts `1.125` to `1.12` and `1.375` to `1.38`.
The sign of zero remains visible, including a negative value that becomes zero after rounding.
The result does not depend on host locale.

The runtime reserves bounded output storage before conversion.
A valid Lo Float cannot contain a nonfinite value.
Upstream arithmetic overflow remains observable as `Numeric.NonFinite`.

## Named templates

`String.format` accepts this grammar:

```text
template    = (literal | "{{" | "}}" | replacement)*
replacement = "{" name (":" format)? "}"
name        = [A-Za-z_][A-Za-z0-9_]*
format      = "," | ">" width (",")?
width       = "0" | [1-9][0-9]*
```

Literal text cannot contain an unmatched brace.
`{{` produces one opening brace, and `}}` produces one closing brace.
Each name selects an exact record field.
Unused fields and repeated placeholders are valid.

Bare Int and Float fields use their existing decimal text contracts.
Bare Bool fields produce `true` or `false`.
String fields are copied without another template evaluation.
Only Int fields support comma grouping.
Grouping inserts a comma between each group of three decimal digits and keeps a negative sign before the first group.

Width is a minimum Unicode scalar count.
The formatter groups digits before it adds spaces on the left.
It does not truncate longer values or measure terminal display columns.
Combining characters count as separate scalars.

| Template | Fields | Result |
| --- | --- | --- |
| `"{count:,}"` | `count: 1234567` | `"1,234,567"` |
| `"{count:>8,}"` | `count: -1234` | `"  -1,234"` |
| `"{name:>3}"` | `name: "界"` | `"  界"` |
| `"{{{name}}}"` | `name: "value"` | `"{value}"` |

Missing fields, unsupported syntax, and grouping of other scalar kinds produce `String.InvalidFormat`.
A width that exceeds unsigned-64 range also produces `String.InvalidFormat`.
A valid width beyond host output limits causes a terminal resource failure before allocation.

The type checker rejects nonscalar and fallible record fields.
Application code must supply fallible values through total scalar parameters before it constructs the formatting record.
The ordinary call boundary propagates a failure before the helper constructs the record.
No implicit failure unwrapping or generic language feature is added to formatting.

## Acceptance scope

Public fixtures must cover strict Unicode, duplicate keys, exact tokens, access failures, and container-depth boundaries.
Resource fixtures must cover value, item, work, temporary-storage, cancellation, and deep-release behavior.
The affected regression lane must retain checkpoint canonical-byte and malformed-input checks.

Formatting fixtures must cover ties, signed zero, subnormals, maximum finite values, precision limits, grouping, Unicode width, and escaped braces.
The same value fixtures must run on Darwin arm64 and Linux arm64 under QEMU.
Compiler catalogs, native metadata, diagnostics, inventories, CLI limits, and the context ABI must use the selected contract together.
