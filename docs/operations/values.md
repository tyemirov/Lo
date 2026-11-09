# Values And Arithmetic

[Documentation home](../README.md) · [Operation index](README.md)

These operations construct atomic values, construct errors, and calculate scalar numbers.
Int arithmetic is exact. Float arithmetic uses finite binary64 values.
`Record` in signatures denotes an exact record shape derived by the compiler.

Argument failures propagate under the language fallibility rules. Host work and value quotas apply to every operation.
Host quota failures are terminal.

## On This Page

- [Bool.new](#boolnew)
- [Error.new](#errornew)
- [Float.add](#floatadd)
- [Float.div](#floatdiv)
- [Float.eq](#floateq)
- [Float.format_fixed](#floatformat_fixed)
- [Float.gt](#floatgt)
- [Float.gte](#floatgte)
- [Float.lt](#floatlt)
- [Float.lte](#floatlte)
- [Float.mul](#floatmul)
- [Float.new](#floatnew)
- [Float.sqrt](#floatsqrt)
- [Float.sub](#floatsub)
- [Float.to_int](#floatto_int)
- [Float.to_string](#floatto_string)
- [Int.new](#intnew)
- [Int.parse](#intparse)
- [Int.to_float](#intto_float)
- [Int.to_string](#intto_string)
- [Math.add](#mathadd)
- [Math.div](#mathdiv)
- [Math.eq](#matheq)
- [Math.gt](#mathgt)
- [Math.gte](#mathgte)
- [Math.lt](#mathlt)
- [Math.lte](#mathlte)
- [Math.max](#mathmax)
- [Math.min](#mathmin)
- [Math.mod](#mathmod)
- [Math.mul](#mathmul)
- [Math.product](#mathproduct)
- [Math.sub](#mathsub)
- [Math.sum](#mathsum)

## Bool.new

```text
Bool.new(value: Bool) -> Bool
```

Return the supplied `Bool` value without conversion. The compiler rejects other input types.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bool.new(true)
```

[Contract](../../specification.md#72-constructor-rules)

## Error.new

```text
Error.new(kind: String, message: String, input: Record) -> Error
```

Construct an error with a kind, message, and exact input record supplied by the application. Construction does not itself propagate the error.
A fresh error has no retained flow-progress records.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Error.new("Input.Invalid", "Expected a positive count", Record.new(count: -1))
```

[Contract](../../specification.md#67-error-model)

## Float.add

```text
Float.add(left: Float, right: Float) -> Float!
```

Return the sum of the two finite operands.
Each operation uses the binary64 rounding rule from the linked contract. Underflow can return subnormal values or signed zero.

**Failures:** `Numeric.NonFinite` when the result is not finite.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.add(1.25, 2.5)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.div

```text
Float.div(left: Float, right: Float) -> Float!
```

Return the left operand divided by the right operand.
Each operation uses the binary64 rounding rule from the linked contract. Underflow can return subnormal values or signed zero.

**Failures:** `Numeric.NonFinite` when the result is not finite. `Numeric.DivideByZero` when the divisor is positive or negative zero.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.div(3.0, 2.0)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.eq

```text
Float.eq(left: Float, right: Float) -> Bool
```

Return whether the left operand is equal to the right operand. Positive and negative zero compare as equal.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.eq(1.25, 2.5)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.format_fixed

```text
Float.format_fixed(value: Float, places: Int) -> String!
```

Format the actual binary64 value with exactly `places` fractional digits and no exponent. Use decimal rounding with ties to even.

Precision zero omits the decimal point. Preserve the negative sign when a negative value rounds to zero.

**Failures:** `Numeric.InvalidPrecision` when `places` is outside 0 through 1000. Output and work limits remain terminal.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.format_fixed(1.125, 2)
```

[Contract](../json-values-and-formatting.md)

## Float.gt

```text
Float.gt(left: Float, right: Float) -> Bool
```

Return whether the left operand is greater than the right operand. Positive and negative zero compare as equal.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.gt(1.25, 2.5)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.gte

```text
Float.gte(left: Float, right: Float) -> Bool
```

Return whether the left operand is greater than or equal to the right operand. Positive and negative zero compare as equal.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.gte(1.25, 2.5)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.lt

```text
Float.lt(left: Float, right: Float) -> Bool
```

Return whether the left operand is less than the right operand. Positive and negative zero compare as equal.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.lt(1.25, 2.5)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.lte

```text
Float.lte(left: Float, right: Float) -> Bool
```

Return whether the left operand is less than or equal to the right operand. Positive and negative zero compare as equal.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.lte(1.25, 2.5)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.mul

```text
Float.mul(left: Float, right: Float) -> Float!
```

Return the product of the two finite operands.
Each operation uses the binary64 rounding rule from the linked contract. Underflow can return subnormal values or signed zero.

**Failures:** `Numeric.NonFinite` when the result is not finite.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.mul(1.25, 2.0)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.new

```text
Float.new(value: Float) -> Float
```

Return the supplied `Float` value without conversion. The compiler rejects other input types.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.new(1.25)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.sqrt

```text
Float.sqrt(value: Float) -> Float!
```

Return the binary64 square root. The square root of negative zero is negative zero.

**Failures:** `Numeric.Domain` for a negative input. `Numeric.NonFinite` for a nonfinite result.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.sqrt(2.25)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.sub

```text
Float.sub(left: Float, right: Float) -> Float!
```

Return the left operand minus the right operand.
Each operation uses the binary64 rounding rule from the linked contract. Underflow can return subnormal values or signed zero.

**Failures:** `Numeric.NonFinite` when the result is not finite.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.sub(3.5, 1.25)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.to_int

```text
Float.to_int(value: Float) -> Int
```

Return the exact integer after removal of the fractional part toward zero.
For example, `-1.9` becomes `-1`.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.to_int(-1.9)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Float.to_string

```text
Float.to_string(value: Float) -> String
```

Return the shortest decimal text that converts back to the same binary64 value. Keep the sign of zero.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Float.to_string(-0.0)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Int.new

```text
Int.new(value: Int) -> Int
```

Return the supplied `Int` value without conversion. The compiler rejects other input types.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Int.new(42)
```

[Contract](../../specification.md#72-constructor-rules)

## Int.parse

```text
Int.parse(text: String) -> Int!
```

Parse `[+-]?[0-9]+` as an exact decimal integer. Accept leading zeros and normalize zero. Reject whitespace, separators, prefixes, and Unicode digits.

**Failures:** `Int.InvalidText` for invalid text. Integer-bit, work, and storage limits remain terminal.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Int.parse("+00042")
```

[Contract](../json-values-and-formatting.md)

## Int.to_float

```text
Int.to_float(value: Int) -> Float!
```

Return the nearest finite binary64 value. Large integers can lose precision.
The operation fails when conversion cannot return a finite value.

**Failures:** `Numeric.NonFinite` when the result cannot be finite.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Int.to_float(42)
```

[Contract](../../specification.md#241-binary64-arithmetic)

## Int.to_string

```text
Int.to_string(value: Int) -> String
```

Return the exact base-ten integer text, with a minus sign for negative values.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Int.to_string(-42)
```

[Contract](../../specification.md#83-conversion-processors)

## Math.add

```text
Math.add(left_value: Int, right_value: Int) -> Int
```

Return the exact integer sum.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.add(20, 22)
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.div

```text
Math.div(left_value: Int, right_value: Int) -> Int!
```

Return the integer quotient after removal of the fractional part toward zero. For example, `-7 / 3` returns `-2`.

**Failures:** `Math.DivideByZero` for a zero divisor.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.div(-7, 3)
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.eq

```text
Math.eq(left_value: Int, right_value: Int) -> Bool
```

Return whether the left integer is equal to the right integer. Comparison uses exact mathematical order.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.eq(2, 3)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Math.gt

```text
Math.gt(left_value: Int, right_value: Int) -> Bool
```

Return whether the left integer is greater than the right integer. Comparison uses exact mathematical order.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.gt(2, 3)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Math.gte

```text
Math.gte(left_value: Int, right_value: Int) -> Bool
```

Return whether the left integer is greater than or equal to the right integer. Comparison uses exact mathematical order.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.gte(2, 3)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Math.lt

```text
Math.lt(left_value: Int, right_value: Int) -> Bool
```

Return whether the left integer is less than the right integer. Comparison uses exact mathematical order.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.lt(2, 3)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Math.lte

```text
Math.lte(left_value: Int, right_value: Int) -> Bool
```

Return whether the left integer is less than or equal to the right integer. Comparison uses exact mathematical order.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.lte(2, 3)
```

[Contract](../../specification.md#232-comparison-and-ranges)

## Math.max

```text
Math.max(number_list: List<Int>) -> Int!
```

Return the largest integer in a nonempty list.

**Failures:** `Math.EmptyList` for an empty input list.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.max(List.new(3, 1, 2))
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.min

```text
Math.min(number_list: List<Int>) -> Int!
```

Return the smallest integer in a nonempty list.

**Failures:** `Math.EmptyList` for an empty input list.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.min(List.new(3, 1, 2))
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.mod

```text
Math.mod(left_value: Int, right_value: Int) -> Int!
```

Return the remainder. A nonzero remainder has the sign of the left operand.
For a nonzero divisor, `left = quotient * right + remainder`.

**Failures:** `Math.ModuloByZero` for a zero divisor.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.mod(-7, 3)
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.mul

```text
Math.mul(left_value: Int, right_value: Int) -> Int
```

Return the exact integer product.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.mul(6, 7)
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.product

```text
Math.product(number_list: List<Int>) -> Int
```

Return the exact product of the list elements. An empty list returns `1`.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.product(List.new(2, 3, 7))
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.sub

```text
Math.sub(left_value: Int, right_value: Int) -> Int
```

Return the exact integer difference, left minus right.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.sub(50, 8)
```

[Contract](../../specification.md#231-numeric-and-collection-results)

## Math.sum

```text
Math.sum(number_list: List<Int>) -> Int
```

Return the exact sum of the list elements. An empty list returns `0`.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Math.sum(List.new(1, 2, 3))
```

[Contract](../../specification.md#231-numeric-and-collection-results)

