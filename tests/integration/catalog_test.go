package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

type builtinExample struct{ name, source, kind string }

func builtinExamples() []builtinExample {
	return []builtinExample{
		{"Int.new", `return Int.new(123456789012345678901234567890)`, "Int"},
		{"String.new", `return String.new("héllo")`, "String"},
		{"Bool.new", `return Bool.new(false)`, "Bool"},
		{"Float.new", `return Float.new(-1.25e3)`, "Float"},
		{"Bytes.new", `return Bytes.new("bytes")`, "Bytes"},
		{"Scalar.new", `return Scalar.new("界")`, "Scalar"},
		{"List.new", `return List.new(null, null)`, "List<Null>"},
		{"Map.new", `return Map.new(Record.new(key: true, value: 1))`, "Map<Bool, Int>"},
		{"Record.new", `return Record.new(x: 1, y: "a")`, "Record{x: Int, y: String}"},
		{"Range.new", `return Range.new("a", "z", true, false)`, "Range<String>"},
		{"Error.new", `return Error.new("Custom", "message", Record.new())`, "Error"},
		{"File.Path.new", `return File.Path.new("unread")`, "File.Path"},
		{"HTTP.Server.new", `flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; return HTTP.Server.new(8080, H, E)`, "HTTP.Server"},
		{"HTTP.Response.Text.new", `return HTTP.Response.Text.new(200, "ok")`, "HTTP.Response"},
		{"HTTP.Response.Bytes.new", `return HTTP.Response.Bytes.new(200, Bytes.new("ok"))`, "HTTP.Response"},
		{"UTF8.String.new", `return UTF8.String.new(Bytes.new("ok"))`, "String!"},
		{"String.FromScalars.new", `return String.FromScalars.new(List.new(Scalar.new("a")))`, "String"},
		{"String.concat", `return String.concat("a", "b")`, "String"},
		{"String.upper", `return String.upper("a")`, "String"},
		{"String.lower", `return String.lower("A")`, "String"},
		{"String.length", `return String.length("界")`, "Int"},
		{"String.eq", `return String.eq("a", "b")`, "Bool"},
		{"Scalar.eq", `return Scalar.eq(Scalar.new("a"), Scalar.new("b"))`, "Bool"},
		{"Int.to_string", `return Int.to_string(1)`, "String"},
		{"Math.add", `return Math.add(1, 2)`, "Int"},
		{"Math.sub", `return Math.sub(1, 2)`, "Int"},
		{"Math.mul", `return Math.mul(1, 2)`, "Int"},
		{"Math.div", `return Math.div(1, 0)`, "Int!"},
		{"Math.mod", `return Math.mod(1, 0)`, "Int!"},
		{"Math.lt", `return Math.lt(1, 2)`, "Bool"},
		{"Math.lte", `return Math.lte(1, 2)`, "Bool"},
		{"Math.gt", `return Math.gt(1, 2)`, "Bool"},
		{"Math.gte", `return Math.gte(1, 2)`, "Bool"},
		{"Math.eq", `return Math.eq(1, 2)`, "Bool"},
		{"Math.sum", `return Math.sum(List.new(1, 2))`, "Int"},
		{"Math.product", `return Math.product(List.new())`, "Int"},
		{"Math.min", `return Math.min(List.new())`, "Int!"},
		{"Math.max", `return Math.max(List.new())`, "Int!"},
		{"Range.contains", `return Range.contains(Range.new(1, 4, true, true), 3)`, "Bool"},
		{"Range.is_empty", `return Range.is_empty(Range.new(1, 4, true, true))`, "Bool"},
		{"Map.has_key", `return Map.has_key(Map.new(Record.new(key: "k", value: 1)), "k")`, "Bool"},
		{"Map.get", `return Map.get(Map.new(Record.new(key: null, value: 1)), null)`, "Int!"},
		{"Record.merge", `return Record.merge(Record.new(a: 1), Record.new(a: "a"))`, "Record{a: String}"},
		{"List.map", `flow F(x: Int) -> String { return Int.to_string(x) }; return List.map(List.new(1), F)`, "List<String>"},
		{"List.filter", `flow F(x: Scalar) -> Bool { return true }; return List.filter("abc", F)`, "List<Scalar>"},
		{"List.fold", `flow F(acc: Int, x: Int) -> Int { return Math.add(acc, x) }; return List.fold(List.new(1), 0, F)`, "Int"},
		{"List.flat_map", `flow F(x: Int) -> List<Int>! { return List.new(x) }; return List.flat_map(List.new(1), F)`, "List<Int>!"},
		{"List.unfold", `flow F(x: Int) -> Record{value: String, next_state: Int}! { return Record.new(value: Int.to_string(x), next_state: Math.add(x, 1)) }; return List.unfold(0, 2, F)`, "List<String>!"},
		{"List.length", `return List.length("héllo")`, "Int"},
		{"File.read", `return File.read(File.Path.new("missing"))`, "Bytes!"},
		{"File.append", `return File.append(File.Path.new("unwritten"), "data")`, "Effect"},
		{"File.write", `return File.write(File.Path.new("unwritten"), Bytes.new("data"))`, "Effect"},
	}
}

func TestVerifierBuiltinContracts(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range builtinExamples() {
		covered[tc.name] = true
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("%+v", got)
			}
			var result struct {
				ResultType string `json:"result_type"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
				t.Fatal(err)
			}
			if result.ResultType != tc.kind {
				t.Fatalf("got %s, want %s", result.ResultType, tc.kind)
			}
		})
	}
	for _, op := range catalog.All() {
		if !covered[op.Name()] {
			t.Errorf("no public signature example for %s", op.Name())
		}
	}
}
