package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func f030Value(t *testing.T, source string) runtimeValue {
	t.Helper()
	value := runValue(t, source)
	if strings.HasSuffix(value.Type, "!") {
		var inner runtimeValue
		if err := json.Unmarshal(value.Value, &inner); err != nil {
			t.Fatal(err)
		}
		return inner
	}
	return value
}

type f030Case struct{ name, source, typ, value string }

func f030Cases() []f030Case {
	return []f030Case{
		{"String.starts_with", `return String.starts_with("éclair", "é")`, "Bool", `true`},
		{"String.ends_with", `return String.ends_with("a😀", "😀")`, "Bool", `true`},
		{"String.slice", `flow Main() -> String! { return String.slice("aé😀z", 1, -1) }; return Main()`, "String", `"é😀"`},
		{"String.compare", `return String.compare("é", "z")`, "Int", `"1"`},
		{"String.split", `flow Main() -> List<String>! { return String.split("a::b::", "::") }; return Main()`, "List<String>", `[{"type":"String","value":"a"},{"type":"String","value":"b"},{"type":"String","value":""}]`},
		{"String.split_first", `flow Main() -> Record{before: String, after: String}! { return String.split_first("a:b:c", ":") }; return Main()`, "Record{after: String, before: String}", `{"before":{"type":"String","value":"a"},"after":{"type":"String","value":"b:c"}}`},
		{"String.replace_all", `flow Main() -> String! { return String.replace_all("aaaaa", "aa", "b") }; return Main()`, "String", `"bba"`},
		{"String.trim", `return String.trim("\u2003a\u00a0")`, "String", `"a"`},
		{"String.is_whitespace", `return String.is_whitespace("\t\u2003")`, "Bool", `true`},
		{"String.split_whitespace", `return String.split_whitespace(" a\t\u2003b ")`, "List<String>", `[{"type":"String","value":"a"},{"type":"String","value":"b"}]`},
		{"String.lines", `return String.lines("a\r\n\rb\n")`, "List<String>", `[{"type":"String","value":"a"},{"type":"String","value":""},{"type":"String","value":"b"}]`},
		{"Bytes.length", `return Bytes.length(Bytes.new("é\u0000"))`, "Int", `"3"`},
		{"Bytes.starts_with", `return Bytes.starts_with(Bytes.new("é"), Bytes.new("é"))`, "Bool", `true`},
		{"Bytes.slice", `flow Main() -> Bytes! { return Bytes.slice(Bytes.new("é"), 0, 1) }; return Main()`, "Bytes", `"ww=="`},
		{"Bytes.split", `flow Main() -> List<Bytes>! { return Bytes.split(Bytes.new("a\u0000\u0000"), Bytes.new("\u0000")) }; return Main()`, "List<Bytes>", `[{"type":"Bytes","value":"YQ=="},{"type":"Bytes","value":""},{"type":"Bytes","value":""}]`},
		{"Bytes.trim_start", `flow Main() -> Bytes! { return Bytes.trim_start(Bytes.new("ababx"), Bytes.new("ab")) }; return Main()`, "Bytes", `"eA=="`},
		{"List.at", `flow Main() -> String! { return List.at(List.new("a", "b"), -1) }; return Main()`, "String", `"b"`},
		{"List.slice", `flow Main() -> List<Int>! { return List.slice(List.new(1, 2, 3), 1, -1) }; return Main()`, "List<Int>", `[{"type":"Int","value":"2"}]`},
		{"List.sort", `flow Compare(a: String, b: String) -> Int { return String.compare(a,b) }; return List.sort(List.new("b", "a", "a"), Compare)`, "List<String>", `[{"type":"String","value":"a"},{"type":"String","value":"a"},{"type":"String","value":"b"}]`},
		{"File.Path.to_string", `return File.Path.to_string(File.Path.new("a/../b"))`, "String", `"a/../b"`},
		{"File.Path.join", `flow Main() -> File.Path! { return File.Path.join(File.Path.new("a/"), "../b") }; return Main()`, "File.Path", `"a/../b"`},
		{"File.Path.parent", `return File.Path.to_string(File.Path.parent(File.Path.new("a//b/")))`, "String", `"a"`},
		{"File.Path.name", `return File.Path.name(File.Path.new("/a/b/"))`, "String", `"b"`},
		{"Glob.new", `flow Main() -> Bool! { mask = Glob.new("a*[!0-9]?"); return Glob.matches(mask, "a/.xz") }; return Main()`, "Bool", `true`},
		{"Glob.matches", `flow Main() -> Bool! { mask = Glob.new("*"); return Glob.matches(mask, ".a/b") }; return Main()`, "Bool", `true`},
		{"Process.Arguments.format_shell", `flow Main() -> String! { return Process.Arguments.format_shell(List.new("", "a b", "a'b", "$(x)")) }; return Main()`, "String", `"'' 'a b' 'a'\\''b' '$(x)'"`},
	}
}

func TestF030Values(t *testing.T) {
	for _, tc := range f030Cases() {
		t.Run(tc.name, func(t *testing.T) {
			value := f030Value(t, tc.source)
			if value.Type != tc.typ {
				t.Fatalf("type = %s, want %s", value.Type, tc.typ)
			}
			valueJSON(t, value, tc.value)
		})
	}
}
