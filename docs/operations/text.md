# Text, Bytes, And Patterns

[Documentation home](../README.md) · [Operation index](README.md)

These operations transform Unicode text, byte sequences, and compiled glob patterns.
Text indices count Unicode scalars. Byte indices count bytes. Neither form counts displayed characters.

Argument failures propagate under the language fallibility rules. Host work and value quotas apply to every operation.
Host quota failures are terminal.

## On This Page

- [Bytes.length](#byteslength)
- [Bytes.new](#bytesnew)
- [Bytes.slice](#bytesslice)
- [Bytes.split](#bytessplit)
- [Bytes.starts_with](#bytesstarts_with)
- [Bytes.trim_start](#bytestrim_start)
- [Glob.matches](#globmatches)
- [Glob.new](#globnew)
- [Scalar.eq](#scalareq)
- [Scalar.new](#scalarnew)
- [String.FromScalars.new](#stringfromscalarsnew)
- [String.compare](#stringcompare)
- [String.concat](#stringconcat)
- [String.ends_with](#stringends_with)
- [String.eq](#stringeq)
- [String.is_whitespace](#stringis_whitespace)
- [String.length](#stringlength)
- [String.lines](#stringlines)
- [String.lower](#stringlower)
- [String.new](#stringnew)
- [String.replace_all](#stringreplace_all)
- [String.slice](#stringslice)
- [String.split](#stringsplit)
- [String.split_first](#stringsplit_first)
- [String.split_whitespace](#stringsplit_whitespace)
- [String.starts_with](#stringstarts_with)
- [String.trim](#stringtrim)
- [String.upper](#stringupper)
- [UTF8.Bytes.new](#utf8bytesnew)
- [UTF8.String.new](#utf8stringnew)

## Bytes.length

```text
Bytes.length(bytes: Bytes) -> Int
```

Return the byte count, including NUL bytes. The count is independent of UTF-8 scalar boundaries.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bytes.length(Bytes.new("é"))
```

[Contract](../value-processors.md#text-and-bytes)

## Bytes.new

```text
Bytes.new(value: String) -> Bytes
```

Encode the supplied String as UTF-8 bytes. NUL is valid data.
This constructor does not interpret the input as Base64.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bytes.new("héllo")
```

[Contract](../../specification.md#71-built-in-constructors)

## Bytes.slice

```text
Bytes.slice(bytes: Bytes, start: Int, end: Int) -> Bytes!
```

Return bytes from `start` through the position before `end`. Negative bounds add the byte count once.
The adjusted bounds must satisfy `0 <= start <= end <= length`. Equal bounds return empty Bytes.
A byte boundary can divide a UTF-8 sequence.

**Failures:** `Bytes.InvalidBounds` for invalid adjusted bounds, including arbitrarily large indices.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bytes.slice(Bytes.new("abc"), 1, 2)
```

[Contract](../value-processors.md#text-and-bytes)

## Bytes.split

```text
Bytes.split(bytes: Bytes, separator: Bytes) -> List<Bytes>!
```

Split at each exact byte separator. Keep empty fields, including a final empty field.
Empty input with a valid separator returns one empty field.

**Failures:** `Bytes.InvalidSeparator` for an empty separator.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bytes.split(Bytes.new("a:b:"), Bytes.new(":"))
```

[Contract](../value-processors.md#text-and-bytes)

## Bytes.starts_with

```text
Bytes.starts_with(bytes: Bytes, prefix: Bytes) -> Bool
```

Return whether the exact byte prefix matches the start of the byte sequence. An empty prefix matches.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bytes.starts_with(Bytes.new("hello"), Bytes.new("he"))
```

[Contract](../value-processors.md#text-and-bytes)

## Bytes.trim_start

```text
Bytes.trim_start(bytes: Bytes, prefix: Bytes) -> Bytes!
```

Remove a sequence of complete prefix matches from the start. Keep bytes after the first mismatch.

**Failures:** `Bytes.InvalidSeparator` for an empty prefix.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Bytes.trim_start(Bytes.new("ababx"), Bytes.new("ab"))
```

[Contract](../value-processors.md#text-and-bytes)

## Glob.matches

```text
Glob.matches(pattern: Glob, text: String) -> Bool
```

Return whether the compiled pattern matches the complete scalar sequence. Matching is case-sensitive and does not normalize Unicode.
Applications select basenames and hidden-file rules explicitly.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
flow Main() -> Bool! {
    pattern = Glob.new("*.lo")
    return Glob.matches(pattern, "example.lo")
}
return Main()
```

[Contract](../value-processors.md#globs)

## Glob.new

```text
Glob.new(pattern: String) -> Glob!
```

Compile a complete-text glob. Supported forms are `*`, `?`, character classes, ascending ranges, negated classes, and backslash escapes.
Stars can match `/` and leading dots. Braces and parentheses are literal characters.

**Failures:** `Glob.InvalidPattern` for an unclosed or empty class, a descending range, or a terminal escape.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Glob.new("*.lo")
```

[Contract](../value-processors.md#globs)

## Scalar.eq

```text
Scalar.eq(left_value: Scalar, right_value: Scalar) -> Bool
```

Return whether the two Unicode scalar values are identical.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Scalar.eq(Scalar.new("a"), Scalar.new("a"))
```

[Contract](../../specification.md#82-scalar-processors)

## Scalar.new

```text
Scalar.new(value: Scalar or statically known String) -> Scalar
```

Return an existing Scalar, or construct a Scalar from a statically known String with exactly one Unicode scalar.
A dynamic String or a String with another scalar count fails verification.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return Scalar.new("界")
```

[Contract](../../specification.md#72-constructor-rules)

## String.FromScalars.new

```text
String.FromScalars.new(scalar_list: List<Scalar>) -> String
```

Construct a String from the Unicode scalars in list order. An empty list returns an empty String.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.FromScalars.new(List.new(Scalar.new("a"), Scalar.new("界")))
```

[Contract](../../specification.md#71-built-in-constructors)

## String.compare

```text
String.compare(left: String, right: String) -> Int
```

Compare the scalar sequences lexically. Return `-1`, `0`, or `1`.
A String precedes a longer String when it is that String's complete prefix.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.compare("a", "b")
```

[Contract](../value-processors.md#text-and-bytes)

## String.concat

```text
String.concat(values: String...) -> String
```

Join the supplied Strings in argument order. The result has no added separator.
Zero arguments return an empty String. Only positional String arguments are permitted.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.concat("hello", " ", "world")
```

[Contract](../../specification.md#81-string-processors)

## String.ends_with

```text
String.ends_with(text: String, suffix: String) -> Bool
```

Return whether the complete suffix matches the end of the text. An empty suffix matches every String.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.ends_with("hello", "lo")
```

[Contract](../value-processors.md#text-and-bytes)

## String.eq

```text
String.eq(left_value: String, right_value: String) -> Bool
```

Return whether the Strings contain the same scalar sequence. Comparison is case-sensitive and does not normalize Unicode.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.eq("Hello", "hello")
```

[Contract](../../specification.md#81-string-processors)

## String.is_whitespace

```text
String.is_whitespace(text: String) -> Bool
```

Return true only when the text contains one or more Unicode White_Space scalars and no other scalars.
An empty String returns false.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.is_whitespace(" \t")
```

[Contract](../value-processors.md#text-and-bytes)

## String.length

```text
String.length(value: String) -> Int
```

Return the number of Unicode scalars, not UTF-8 bytes or displayed characters.
A combining scalar counts separately from the preceding scalar.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.length("a界")
```

[Contract](../../specification.md#65-string-model)

## String.lines

```text
String.lines(text: String) -> List<String>
```

Split at LF, CRLF, or CR. Exclude terminators and keep empty interior lines.
A final terminator adds no extra line. An empty String returns an empty list.
Other Unicode line separators remain ordinary data.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.lines("a\nb\n")
```

[Contract](../value-processors.md#text-and-bytes)

## String.lower

```text
String.lower(value: String) -> String
```

Return the simple Unicode lower-case mapping of each scalar. The operation does not use locale-specific conversion or Unicode normalization.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.lower("HELLO")
```

[Contract](../../specification.md#232-comparison-and-ranges)

## String.new

```text
String.new(value: String) -> String
```

Return the supplied String without conversion.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.new("hello")
```

[Contract](../../specification.md#72-constructor-rules)

## String.replace_all

```text
String.replace_all(text: String, old: String, replacement: String) -> String!
```

Replace exact matches from left to right. Matches do not overlap.
An empty replacement removes the matched text.

**Failures:** `String.InvalidSeparator` for an empty `old` value.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.replace_all("aaaaa", "aa", "b")
```

[Contract](../value-processors.md#text-and-bytes)

## String.slice

```text
String.slice(text: String, start: Int, end: Int) -> String!
```

Return the scalar sequence from `start` through the position before `end`.
For a negative bound, add the scalar count once. The adjusted bounds must satisfy `0 <= start <= end <= length`.
Equal bounds return an empty String.

**Failures:** `String.InvalidBounds` for invalid adjusted bounds, including arbitrarily large indices.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.slice("a界b", 1, 2)
```

[Contract](../value-processors.md#text-and-bytes)

## String.split

```text
String.split(text: String, separator: String) -> List<String>!
```

Split at each exact separator. Keep empty fields, including a final empty field.
An empty text with a valid separator returns one empty field.

**Failures:** `String.InvalidSeparator` for an empty separator.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.split("a:b:", ":")
```

[Contract](../value-processors.md#text-and-bytes)

## String.split_first

```text
String.split_first(text: String, separator: String) -> Record{after: String, before: String}!
```

Split at the first exact separator. Return the text before and after it.
The `after` field keeps any later separators.

**Failures:** `String.InvalidSeparator` for an empty separator. `String.SeparatorAbsent` when the separator does not occur.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.split_first("a:b:c", ":")
```

[Contract](../value-processors.md#text-and-bytes)

## String.split_whitespace

```text
String.split_whitespace(text: String) -> List<String>
```

Split on sequences of Unicode White_Space scalars. Exclude empty fields.
An empty or all-whitespace String returns an empty list.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.split_whitespace(" a\t b ")
```

[Contract](../value-processors.md#text-and-bytes)

## String.starts_with

```text
String.starts_with(text: String, prefix: String) -> Bool
```

Return whether the complete prefix matches the start of the text. An empty prefix matches every String.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.starts_with("hello", "he")
```

[Contract](../value-processors.md#text-and-bytes)

## String.trim

```text
String.trim(text: String) -> String
```

Remove Unicode White_Space scalars from both ends. Keep interior whitespace.
The linked contract lists all 25 recognized scalars.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.trim("  hello\t")
```

[Contract](../value-processors.md#text-and-bytes)

## String.upper

```text
String.upper(value: String) -> String
```

Return the simple Unicode upper-case mapping of each scalar. The operation does not use locale-specific conversion or Unicode normalization.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return String.upper("hello")
```

[Contract](../../specification.md#232-comparison-and-ranges)

## UTF8.Bytes.new

```text
UTF8.Bytes.new(text: String) -> Bytes
```

Encode all Unicode scalars in the String as UTF-8 bytes. NUL is valid data.

**Failures:** No recoverable failure from this operation.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return UTF8.Bytes.new("héllo")
```

[Contract](../../specification.md#71-built-in-constructors)

## UTF8.String.new

```text
UTF8.String.new(bytes: Bytes) -> String!
```

Decode the complete byte sequence as UTF-8. Invalid input returns no partial String.

**Failures:** `UTF8.InvalidBytes` for malformed UTF-8.

**Authority:** Pure. No host grant or commit is necessary.

```lo
return UTF8.String.new(Bytes.new("héllo"))
```

[Contract](../value-processors.md#text-and-bytes)

