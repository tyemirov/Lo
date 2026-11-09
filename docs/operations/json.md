# JSON Operations

[Documentation](../README.md) / [Operation index](README.md)

JSON values are immutable data without host authority. Extraction uses exact kinds and keys without coercion.
All operations are pure. Cancellation and resource limits apply in addition to the recoverable failures below.

## JSON.at

```text
JSON.at(value: JSON.Value, index: Int) -> JSON.Value!
```

Return the array element at the nonnegative index. Index zero selects the first element. The result retains only the selected data.

**Failures:** `JSON.TypeMismatch` for a non-array value. `JSON.InvalidIndex` for a negative or out-of-range index.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> JSON.Value! {
    return JSON.at(JSON.decode(Bytes.new("[10,20]")), 1)
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.bool

```text
JSON.bool(value: JSON.Value) -> Bool!
```

Extract a Boolean value without coercion.

**Failures:** `JSON.TypeMismatch` for another JSON kind.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> Bool! {
    return JSON.bool(JSON.decode(Bytes.new("true")))
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.decode

```text
JSON.decode(data: Bytes) -> JSON.Value!
```

Decode exactly one strict JSON document. Keep exact number tokens and decoded Unicode strings. Reject duplicate decoded keys and invalid Unicode.

The result is immutable and has no projected fields. Container depth, item, work, value, and live-resource limits apply.

**Failures:** `JSON.InvalidData` for invalid syntax, duplicate keys, invalid Unicode, or trailing data. Resource exhaustion remains terminal.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return JSON.decode(Bytes.new("{\"count\":9007199254740993}"))
```

[Contract](../json-values-and-formatting.md)

## JSON.float

```text
JSON.float(value: JSON.Value) -> Float!
```

Convert an exact number token to the nearest binary64 value, with ties to even. Preserve signed zero and finite subnormal results.

**Failures:** `JSON.TypeMismatch` for another JSON kind. `JSON.InvalidNumber` for a nonfinite result or a nonzero value rounded to zero.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> Float! {
    return JSON.float(JSON.decode(Bytes.new("1.25")))
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.get

```text
JSON.get(value: JSON.Value, key: String) -> JSON.Value!
```

Return the object member with the exact decoded key. Keys have no case conversion or Unicode normalization. The result retains only the selected data.

**Failures:** `JSON.TypeMismatch` for a non-object value. `JSON.FieldAbsent` for an absent key.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> JSON.Value! {
    return JSON.get(JSON.decode(Bytes.new("{\"name\":\"Lo\"}")), "name")
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.int

```text
JSON.int(value: JSON.Value) -> Int!
```

Convert an integer number token to an exact Int. Reject tokens with a fraction or exponent, including `1.0` and `1e0`. Normalize negative zero.

**Failures:** `JSON.TypeMismatch` for another JSON kind. `JSON.InvalidNumber` for a fraction or exponent token. The integer-bit limit remains terminal.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> Int! {
    return JSON.int(JSON.decode(Bytes.new("9007199254740993")))
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.kind

```text
JSON.kind(value: JSON.Value) -> String
```

Return one of `null`, `bool`, `number`, `string`, `array`, or `object`.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> String! {
    return JSON.kind(JSON.decode(Bytes.new("null")))
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.length

```text
JSON.length(value: JSON.Value) -> Int!
```

Return the element count of an array or the member count of an object.

**Failures:** `JSON.TypeMismatch` for another JSON kind.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> Int! {
    return JSON.length(JSON.decode(Bytes.new("[1,2,3]")))
}
return Main()
```

[Contract](../json-values-and-formatting.md)

## JSON.string

```text
JSON.string(value: JSON.Value) -> String!
```

Extract the decoded string without coercion. The string can contain escaped NUL data from the source document.

**Failures:** `JSON.TypeMismatch` for another JSON kind.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> String! {
    return JSON.string(JSON.decode(Bytes.new("\"hello\"")))
}
return Main()
```

[Contract](../json-values-and-formatting.md)
