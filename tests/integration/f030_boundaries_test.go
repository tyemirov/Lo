package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

type f030FailureCase struct{ name, expression, kind string }

func f030FailureCases() []f030FailureCase {
	return []f030FailureCase{
		{"string-start", `String.slice("é", -2, 1)`, "String.InvalidBounds"},
		{"string-end", `String.slice("é", 0, 2)`, "String.InvalidBounds"},
		{"string-reversed", `String.slice("é", 1, 0)`, "String.InvalidBounds"},
		{"string-huge", `String.slice("a", 999999999999999999999999999999, 1)`, "String.InvalidBounds"},
		{"split-empty-separator", `String.split("a", "")`, "String.InvalidSeparator"},
		{"split-first-empty-separator", `String.split_first("", "")`, "String.InvalidSeparator"},
		{"split-first-absent", `String.split_first("abc", ":")`, "String.SeparatorAbsent"},
		{"replace-empty", `String.replace_all("a", "", "b")`, "String.InvalidSeparator"},
		{"bytes-start", `Bytes.slice(Bytes.new("a"), -2, 1)`, "Bytes.InvalidBounds"},
		{"bytes-end", `Bytes.slice(Bytes.new("a"), 0, 2)`, "Bytes.InvalidBounds"},
		{"bytes-reversed", `Bytes.slice(Bytes.new("a"), 1, 0)`, "Bytes.InvalidBounds"},
		{"bytes-empty-separator", `Bytes.split(Bytes.new("a"), Bytes.new(""))`, "Bytes.InvalidSeparator"},
		{"bytes-empty-prefix", `Bytes.trim_start(Bytes.new("a"), Bytes.new(""))`, "Bytes.InvalidSeparator"},
		{"invalid-utf8", `UTF8.String.new(Bytes.slice(Bytes.new("é"), 0, 1))`, "UTF8.InvalidBytes"},
		{"list-index", `List.at(List.new(1), 1)`, "List.InvalidIndex"},
		{"list-negative", `List.at(List.new(1), -2)`, "List.InvalidIndex"},
		{"list-huge", `List.at(List.new(1), -999999999999999999999999999999)`, "List.InvalidIndex"},
		{"list-bounds", `List.slice(List.new(1), 1, 2)`, "List.InvalidBounds"},
		{"list-reversed", `List.slice(List.new(1), 1, 0)`, "List.InvalidBounds"},
		{"absolute-child", `File.Path.join(File.Path.new("a"), "/b")`, "File.InvalidPath"},
		{"nul-child", `File.Path.join(File.Path.new("a"), "b\u0000")`, "File.InvalidPath"},
		{"nul-base", `File.Path.join(File.Path.new("a\u0000"), "b")`, "File.InvalidPath"},
		{"glob-empty-class", `Glob.new("[]")`, "Glob.InvalidPattern"},
		{"glob-negated-empty-class", `Glob.new("[!]")`, "Glob.InvalidPattern"},
		{"glob-unclosed", `Glob.new("[abc")`, "Glob.InvalidPattern"},
		{"glob-descending", `Glob.new("[z-a]")`, "Glob.InvalidPattern"},
		{"glob-escaped-descending", `Glob.new("[a-\\-]")`, "Glob.InvalidPattern"},
		{"glob-escape", `Glob.new("abc\\")`, "Glob.InvalidPattern"},
		{"glob-class-escape", `Glob.new("[a\\")`, "Glob.InvalidPattern"},
		{"quote-nul", `Process.Arguments.format_shell(List.new("a\u0000b"))`, "Process.InvalidArguments"},
	}
}

func TestF030Failures(t *testing.T) {
	for _, tc := range f030FailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf(`flow Kind(error: Error) -> String { return error.kind }; flow Probe() -> String onError Kind { unused = %s; return "unexpected success" }; return Probe()`, tc.expression)
			value := f030Value(t, source)
			valueJSON(t, value, fmt.Sprintf("%q", tc.kind))
		})
	}
}

func TestF030EmptyAndLexicalBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, source, value string }{
		{"empty-prefix", `return String.starts_with("", "")`, `true`},
		{"empty-suffix", `return String.ends_with("a", "")`, `true`},
		{"empty-slice", `flow Main() -> String! { return String.slice("", 0, 0) }; return Main()`, `""`},
		{"combining-scalars", `flow Main() -> String! { return String.slice("e\u0301", 1, 2) }; return Main()`, `"́"`},
		{"no-normalization", `return String.compare("é", "e\u0301")`, `"1"`},
		{"whitespace-empty", `return String.is_whitespace("")`, `false`},
		{"whitespace-excluded", `return String.is_whitespace("\u200b")`, `false`},
		{"whitespace-all", `return String.trim("\u0085\u1680\u2028\u2029\u202f\u205f\u3000")`, `""`},
		{"whitespace-split-empty", `return String.split_whitespace(" \t ")`, `[]`},
		{"lines-empty", `return String.lines("")`, `[]`},
		{"lines-single-empty", `return String.lines("\n")`, `[{"type":"String","value":""}]`},
		{"lines-unicode-retained", `return String.lines("a\u2028b")`, `[{"type":"String","value":"a b"}]`},
		{"split-empty", `flow Main() -> List<String>! { return String.split("", ":") }; return Main()`, `[{"type":"String","value":""}]`},
		{"split-overlap", `flow Main() -> List<String>! { return String.split("aaa", "aa") }; return Main()`, `[{"type":"String","value":""},{"type":"String","value":"a"}]`},
		{"replace-delete", `flow Main() -> String! { return String.replace_all("aaa", "aa", "") }; return Main()`, `"a"`},
		{"byte-empty-prefix", `return Bytes.starts_with(Bytes.new(""), Bytes.new(""))`, `true`},
		{"byte-empty-split", `flow Main() -> List<Bytes>! { return Bytes.split(Bytes.new(""), Bytes.new("\u0000")) }; return Main()`, `[{"type":"Bytes","value":""}]`},
		{"byte-partial-prefix", `flow Main() -> Bytes! { return Bytes.trim_start(Bytes.new("ababa"), Bytes.new("ab")) }; return Main()`, `"YQ=="`},
		{"list-empty-slice", `flow Main() -> List<Int>! { return List.slice(List.new(), 0, 0) }; return Main()`, `[]`},
		{"scalar-list-at", `flow Main() -> Scalar! { return List.at("a😀", -1) }; return Main()`, `"😀"`},
		{"scalar-list-slice", `flow Main() -> List<Scalar>! { return List.slice("aé😀", 1, 3) }; return Main()`, `[{"type":"Scalar","value":"é"},{"type":"Scalar","value":"😀"}]`},
		{"path-empty-join", `flow Main() -> String! { return File.Path.to_string(File.Path.join(File.Path.new("a//"), "")) }; return Main()`, `"a//"`},
		{"path-relative-join", `flow Main() -> String! { return File.Path.to_string(File.Path.join(File.Path.new(""), "b")) }; return Main()`, `"b"`},
		{"path-root-parent", `return File.Path.to_string(File.Path.parent(File.Path.new("///")))`, `"/"`},
		{"path-top-parent", `return File.Path.to_string(File.Path.parent(File.Path.new("/a")))`, `"/"`},
		{"path-single-parent", `return File.Path.to_string(File.Path.parent(File.Path.new("a")))`, `""`},
		{"path-empty-parent", `return File.Path.to_string(File.Path.parent(File.Path.new("")))`, `""`},
		{"path-root-name", `return File.Path.name(File.Path.new("///"))`, `""`},
		{"path-dot-name", `return File.Path.name(File.Path.new("a/.."))`, `".."`},
		{"path-backslash", `return File.Path.name(File.Path.new("a\\b"))`, `"a\\b"`},
		{"quote-empty", `flow Main() -> String! { return Process.Arguments.format_shell(List.new()) }; return Main()`, `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valueJSON(t, f030Value(t, tc.source), tc.value)
		})
	}
}

func TestF030GlobBoundaries(t *testing.T) {
	for _, tc := range []struct {
		pattern, text string
		matches       bool
	}{
		{"", "", true}, {"", "a", false}, {"*", "", true}, {"?", "😀", true},
		{"?", "é", false}, {"*", ".hidden/path", true}, {"a", "ba", false},
		{"*.tmp", "A.TMP", false}, {"a**b", "a/x/b", true},
		{"[a-c]", "b", true}, {"[!a-c]", "z", true}, {"[!a-c]", "b", false},
		{"[é-ê]", "ê", true}, {"[a-cx]", "x", true}, {`\*`, "*", true},
		{`\?`, "?", true}, {`[\]]`, "]", true}, {`[\-]`, "-", true},
		{"a\x00?", "a\x00b", true}, {"{a,b}", "a", false}, {"{a,b}", "{a,b}", true},
	} {
		t.Run(fmt.Sprintf("%q/%q", tc.pattern, tc.text), func(t *testing.T) {
			source := fmt.Sprintf(`flow Main() -> Bool! { pattern = Glob.new(%q); return Glob.matches(pattern, %q) }; return Main()`, tc.pattern, tc.text)
			valueJSON(t, f030Value(t, strings.ReplaceAll(source, `\x00`, `\u0000`)), fmt.Sprint(tc.matches))
		})
	}
}

func TestF030StableSort(t *testing.T) {
	const source = `flow Compare(direction: Int, a: Record{key: Int, id: String}, b: Record{key: Int, id: String}) -> Int { return Math.mul(direction, Math.sub(a.key,b.key)) }; flow Name(row: Record{key: Int, id: String}) -> String { return row.id }; return List.map(List.sort(List.new(Record.new(key: 2,id: "a"), Record.new(key: 1,id: "b"), Record.new(key: 2,id: "c"), Record.new(key: 1,id: "d")), -1, Compare), Name)`
	valueJSON(t, f030Value(t, source), `[{"type":"String","value":"a"},{"type":"String","value":"c"},{"type":"String","value":"b"},{"type":"String","value":"d"}]`)
}
