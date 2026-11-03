# Pure Value Processors

F030 adds 26 processors to the implemented language contract.
These processors do not observe host state or activate commits.
Application flows select filenames, retention periods, probe records, and commands.

## Text And Bytes

| Signature | Result |
| --- | --- |
| `String.starts_with(text: String, prefix: String) -> Bool` | Exact prefix test. A zero-length prefix matches. |
| `String.ends_with(text: String, suffix: String) -> Bool` | Exact suffix test. A zero-length suffix matches. |
| `String.slice(text: String, start: Int, end: Int) -> String!` | Scalar slice with an exclusive end. |
| `String.compare(left: String, right: String) -> Int` | Scalar comparison. The result is `-1`, `0`, or `1`. |
| `String.split(text: String, separator: String) -> List<String>!` | Exact split. The result keeps zero-length fields, including the final field. |
| `String.split_first(text: String, separator: String) -> Record{before: String, after: String}!` | First exact separator. The `after` field keeps later separators. |
| `String.replace_all(text: String, old: String, replacement: String) -> String!` | Exact replacement from left to right. Matches do not overlap. |
| `String.trim(text: String) -> String` | Remove Unicode White_Space from the two ends. |
| `String.is_whitespace(text: String) -> Bool` | True for one or more Unicode White_Space scalars. |
| `String.split_whitespace(text: String) -> List<String>` | Split on a sequence of Unicode White_Space scalars. The result excludes zero-length fields. |
| `String.lines(text: String) -> List<String>` | Split at LF, CRLF, or CR. The result excludes terminators. |
| `Bytes.length(bytes: Bytes) -> Int` | Byte count, including NUL bytes. |
| `Bytes.starts_with(bytes: Bytes, prefix: Bytes) -> Bool` | Exact byte prefix test. A zero-length prefix matches. |
| `Bytes.slice(bytes: Bytes, start: Int, end: Int) -> Bytes!` | Byte slice with an exclusive end. |
| `Bytes.split(bytes: Bytes, separator: Bytes) -> List<Bytes>!` | Exact byte split. The result keeps all zero-length fields. |
| `Bytes.trim_start(bytes: Bytes, prefix: Bytes) -> Bytes!` | Remove a sequence of exact prefixes from the start. |

Text operations are case-sensitive and do not normalize Unicode.
Text indices count Unicode scalars, including separate combining scalars.
Byte indices count bytes and can divide a UTF-8 sequence.
`UTF8.String.new` remains the decoder and rejects malformed UTF-8 with `UTF8.InvalidBytes`.
NUL is valid data for text, byte, and glob operations.

For a negative index, the processor adds the input length once.
A slice requires `0 <= start <= end <= length` after this adjustment.
A valid slice with equal endpoints has zero elements.
Invalid bounds produce `String.InvalidBounds` or `Bytes.InvalidBounds`.
Arbitrarily large integer indices produce the same typed bounds failure.

A zero-length separator produces `String.InvalidSeparator` or `Bytes.InvalidSeparator`.
This rule also applies to `old` in replacement and `prefix` in byte prefix removal.
An absent separator produces `String.SeparatorAbsent` only for `String.split_first`.
Splitting a zero-length input with a valid separator produces one zero-length field.

Unicode White_Space contains exactly these 25 scalars:
`U+0009..U+000D`, `U+0020`, `U+0085`, `U+00A0`, `U+1680`, `U+2000..U+200A`,
`U+2028`, `U+2029`, `U+202F`, `U+205F`, and `U+3000`.
Whitespace splitting of a zero-length input produces no fields.
The whitespace predicate returns false for a zero-length input.

Line splitting keeps zero-length interior lines.
A final terminator does not add another line.
A zero-length input produces no lines.
Thus, `"\n"` produces one zero-length line, and `"a\n\n"` produces `"a"` followed by one zero-length line.
Unicode line separators remain ordinary scalars for `String.lines`.

## Collections

| Signature | Result |
| --- | --- |
| `List.at(values: List<T>, index: Int) -> T!` | Indexed element. Negative indices count from the end. |
| `List.slice(values: List<T>, start: Int, end: Int) -> List<T>!` | Slice with the same bounds rules as text and bytes. |
| `List.sort(values: List<T>, context..., Compare) -> List<T>` | Stable sort with explicit callback context. |

Lookup requires `0 <= index < length` after negative-index adjustment.
Invalid lookup produces `List.InvalidIndex`.
Invalid slicing produces `List.InvalidBounds`.
For `T!` elements, lookup keeps the selected failure tag without another enclosing tag.
Slicing keeps element identity, failure tags, and File-entry provenance.

The sort callback has parameters `(context..., left: T, right: T)` and a total `Int` result.
The compiler rejects fallible callbacks and callbacks with transitive host observations or commits.
A negative result places `left` first. A positive result places `right` first.
A zero result keeps their original sequence.

The callback must define a consistent comparison relation.
The runtime uses a stable merge sort and does not require a particular comparison magnitude.
Sorting keeps element identity and File-entry provenance.

The existing String view as `List<Scalar>` applies to lookup, slicing, and sorting.
The result contains Scalars and does not become a String implicitly.
Boolean composition, concatenation, chunks, duplicate removal, typed equality, and text joining remain named application flows.
These flows use existing constructors, scalar predicates, `Match.new`, and List processors.
Duplicate removal keeps the first occurrence under the selected typed predicate.
Chunk flows reject a nonpositive size and keep the final short chunk.

## Lexical Paths

| Signature | Result |
| --- | --- |
| `File.Path.to_string(path: File.Path) -> String` | Exact path text. |
| `File.Path.join(path: File.Path, relative: String) -> File.Path!` | Append relative text with a separator when necessary. |
| `File.Path.parent(path: File.Path) -> File.Path` | Lexical parent after removal of terminal separators. |
| `File.Path.name(path: File.Path) -> String` | Final lexical component after removal of terminal separators. |

Only `/` is a separator. Backslash is ordinary data.
These operations do not resolve symlinks, observe files, or grant authority.
They do not normalize `.` or `..` components.
Internal repeated separators remain unchanged.

Join rejects NUL in either operand and a relative operand that starts with `/`.
These failures use `File.InvalidPath`.
A zero-length relative operand returns the exact base text.
A zero-length base returns the relative text.

Parent removes terminal separators before selection of the preceding component.
The parent of `a//b/` is `a`.
The parent of `/a` or any all-separator path is `/`.
A relative singleton or zero-length path has a zero-length parent.
The name of an all-separator or zero-length path is a zero-length String.
The name of `a/..` is `..`.

## Globs

`Glob.new(pattern: String) -> Glob!` compiles an opaque immutable value.
Malformed patterns produce `Glob.InvalidPattern`.
`Glob.matches(pattern: Glob, text: String) -> Bool` compares the complete text with the compiled pattern.
Serialized Glob values contain the original pattern text.

| Syntax | Meaning |
| --- | --- |
| `*` | Zero or more Unicode scalars, including `/` and leading dots. |
| `?` | Exactly one Unicode scalar. |
| `[abc]` | One scalar from the class. |
| `[a-z]` | One scalar from an inclusive ascending range. |
| `[!a-z]` | One scalar outside the class. |
| `\` followed by a scalar | The next scalar is literal, inside or outside a class. |

A class must contain at least one literal or range.
An unclosed class, descending range, or terminal escape is invalid.
An unescaped hyphen between two class scalars defines a range.
A hyphen at either class end is literal.

Repeated stars have no separate recursive meaning.
Braces and parentheses are ordinary literals.
The application selects basenames and hidden-file exclusions explicitly.

## Command Display

`Process.Arguments.format_shell(arguments: List<String>) -> String!` supplies display text in POSIX shell syntax.
Each argument has single quotes. Arguments have one space between them.

An apostrophe uses the sequence `'\''`.
A zero-length argument becomes `''`. A list with no arguments becomes a zero-length String.
NUL produces `Process.InvalidArguments`.

This operation does not execute a shell or create an executable capability.
Process activation continues to receive the original argument list.

## Resource Contract And Examples

All processors use the current logical work and live-value quotas.
Expanded results require admission before allocation.
Compiled glob tokens and temporary match and sort storage also consume the live-value quota.
Glob matching uses iterative dynamic programming with no recursive backtracking.
Sort callbacks release temporary values between comparisons.
Host quota failures remain terminal and cannot become application errors.

The [file-policy example](../examples/file_policy.lo) selects direct files without removal.
The [probe-fields example](../examples/probe_fields.lo) parses exact NUL-framed names for a selected candidate batch.
The probe example rejects malformed framing, invalid UTF-8, zero-length names, and every name outside that batch.
These examples do not establish complete mac-cleanup acceptance.
