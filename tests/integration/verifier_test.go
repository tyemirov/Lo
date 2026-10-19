package integration_test

import (
	"encoding/json"
	"testing"
)

func TestVerifierExamples(t *testing.T) {
	for _, name := range []string{"dynamic_filter", "extract_vowels", "status_effect", "count_letter_flow", "http_division", "list_sum"} {
		t.Run(name, func(t *testing.T) {
			got := run(t, "check", "examples/"+name+".lo", "--format", "json")
			if got.status != 0 || got.stderr != "" || !json.Valid([]byte(got.stdout)) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestVerifierContracts(t *testing.T) {
	cases := []struct{ name, source, kind, code string }{
		{"nested-flows", `flow Outer(x: Int) -> Int { flow Add(y: Int) -> Int { return Math.add(y, 1) }; return Add(x) }; return Outer(2)`, "Int", ""},
		{"forward-flow", `return F(2); flow F(x: Int) -> Int { return x }`, "", "syntax.return-position"},
		{"forward-handler", `flow F() -> Int onError H { return Math.div(1, 0) }; flow H(e: Error) -> Int { return 3 }; return F()`, "Int", ""},
		{"keyword-record", `flow F(x: Record{flow: String}) -> String { return x.flow }; return F(Record.new(flow: "F"))`, "String", ""},
		{"nested-fallibility", `flow F(x: Map<String, List<Int!>>) -> Map<String, List<Int!>> { return x }`, "", ""},
		{"fallible-elements", `flow F() -> List<Int!> { return List.new(1, Math.div(1, 0)) }; return F()`, "List<Int!>", ""},
		{"fallible-return", `flow F() -> Int! { return Error.new(kind: "Mine", message: "failure", input: Record.new()) }; return F()`, "Int!", ""},
		{"error-data", `flow F() -> Error { return Error.new(kind: "Mine", message: "data", input: Record.new()) }; return F()`, "Error", ""},
		{"fallible-argument", `flow F(x: Int!) -> List<Int!> { return List.new(x) }; return F(2)`, "List<Int!>", ""},
		{"top-propagation", `x = Math.div(2, 1); return Math.add(x, 1)`, "", "check.unhandled-failure"},
		{"record-shape", `a = Record.new(x: 1, y: "old"); b = Record.merge(a, Record.new(y: true)); return b.y`, "Bool", ""},
		{"unknown-record-field", `flow F(x: Record) -> Int { return x.a }`, "", "check.unknown-field"},
		{"empty-context", `flow F() -> Map<String, List<Int>> { return Map.new() }; return F()`, "Map<String, List<Int>>", ""},
		{"empty-list-context", `return Math.sum(List.new())`, "Int", ""},
		{"map-get", `flow F() -> Int! { m = Map.new(Record.new(key: "x", value: 1)); return Map.get(m, "x") }; return F()`, "Int!", ""},
		{"range", `r = Range.new(start: 1, end: 3, include_start: true, include_end: false); return Range.contains(r, 2)`, "Bool", ""},
		{"range-match", `return Match.new(2, Range.new(start: 1, end: 3, include_start: true, include_end: true): "yes", _: "no")`, "String", ""},
		{"match-default-position", `return Match.new(1, _: 2, 1: 3)`, "", "check.match-default"},
		{"match-fallibility", `flow F(x: Int) -> Int! { return Match.new(x, 1: 3, _: Math.div(1, x)) }; return F(1)`, "Int!", ""},
		{"match-head-failure", `flow F() -> Int! { return Match.new(1, Math.div(1, 1): 2, _: 3) }`, "", "check.fallible-case"},
		{"fold-context", `flow F(c: Int, acc: Int, x: Int) -> Int { return Math.add(acc, Math.mul(c, x)) }; return List.fold(List.new(1, 2), 0, 3, F)`, "Int", ""},
		{"unfold", `flow F(x: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: x, next_state: Math.add(x, 1)) }; return List.unfold(0, 3, F)`, "List<Int>", ""},
		{"flat-map", `flow F(x: Int) -> List<Int> { return List.new(x) }; return List.flat_map(List.new(1), F)`, "List<Int>", ""},
		{"fallible-list", `flow F(x: Int) -> Int! { return Math.div(1, x) }; return List.map(List.new(1), F)`, "List<Int>!", ""},
		{"wrong-predicate", `flow F(x: Int) -> Int { return x }; return Match.new(1, when F: 2, _: 3)`, "", "check.type-mismatch"},
		{"bad-scalar", `return Scalar.new("ab")`, "", "check.invalid-literal"},
		{"dynamic-scalar", `flow F(x: String) -> Scalar { return Scalar.new(x) }`, "", "check.invalid-literal"},
		{"float-infinite", `return Float.new(1e9999)`, "", "check.invalid-literal"},
		{"bytes", `return Bytes.new("abc")`, "Bytes", ""},
		{"scalar-alias", `x = "é"; return Scalar.new(x)`, "Scalar", ""},
		{"mixed-arguments", `return Math.add(left_value: 1, 2)`, "", "check.argument-order"},
		{"duplicate-record", `return Record.new(a: 1, a: 2)`, "", "check.duplicate-argument"},
		{"nested-invalid-type", `flow F(x: List<Map<List<Int>, String>>) -> Int { return 1 }`, "", "check.type-constraint"},
		{"top-capture", `x = 1; flow F() -> Int { return x }`, "", "resolve.capture"},
		{"flow-alias", `flow F() -> Int { return 1 }; alias = F`, "", "check.flow-reference"},
		{"indirect-cycle", `flow F() -> Int { return G() }; flow G() -> Int { return F() }`, "", "check.cycle"},
		{"handler-cycle", `flow F() -> Int onError H { return 1 }; flow H(e: Error) -> Int { return F() }`, "", "check.cycle"},
		{"plain-commit", `flow F() -> Int { e = File.Append.new(File.Path.new("x"), UTF8.Bytes.new("data")); commit e; return 1 }`, "", "check.unhandled-failure"},
		{"indirect-total-effect", `flow H(e: Error) -> Int { return 0 }; flow W() -> Int onError H { e = File.Append.new(File.Path.new("x"), UTF8.Bytes.new("data")); commit e; return 1 }; flow F() -> Int { return W() }`, "", "check.effectful-total"},
		{"invalid-unused", `flow F() -> String { return 1 }; return 2`, "", "check.type-mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
			if tc.code != "" {
				if got.status != 1 {
					t.Fatalf("want source rejection: %+v", got)
				}
				d := diagnostics(t, got.stderr).Diagnostics[0]
				if d.Code != tc.code {
					t.Fatalf("want %s, got %+v", tc.code, d)
				}
				return
			}
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("%+v", got)
			}
			var output struct {
				ResultType string `json:"result_type"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output.ResultType != tc.kind {
				t.Fatalf("type %s, want %s", output.ResultType, tc.kind)
			}
		})
	}
}

func TestVerifierEdgeContracts(t *testing.T) {
	cases := []struct{ name, source, kind, code string }{
		{"match-stored-failure", `flow F() -> List<Int!> { return List.new(Match.new(1, 1: Math.div(1, 0), _: 2)) }; return F()`, "List<Int!>", ""},
		{"match-error-return", `flow F(x: Int) -> Int! { return Match.new(x, 0: Error.new(kind: "Mine", message: "failed", input: Record.new()), _: x) }; return F(1)`, "Int!", ""},
		{"map-fallible-value", `flow M() -> Map<String, Int!> { return Map.new(Record.new(key: "a", value: Math.div(1, 0))) }; return Map.get(M(), "a")`, "Int!", ""},
		{"fallible-record-view", `flow F() -> Record{a: Int}! { return Record.new(a: 1) }; flow G(x: Record!) -> List<Record!> { return List.new(x) }; return G(F())`, "List<Record!>", ""},
		{"future-capture", `flow F() -> Int { return x }; x = 1`, "", "resolve.capture"},
		{"nested-unused-effect", `flow F() -> Int { flow W() -> Int! { e = File.Append.new(File.Path.new("unused"), UTF8.Bytes.new("x")); commit e; return 1 }; return 2 }; return F()`, "Int", ""},
		{"local-forward-value", `flow F() -> Int { a = b; b = 2; return a }`, "", "resolve.unknown-name"},
		{"nested-flow-not-visible", `flow F() -> Int { flow G() -> Int { return 1 }; return G() }; return G()`, "", "resolve.unknown-name"},
		{"handler-capture", `flow F() -> Int { x = 1; flow H(e: Error) -> Int { return x }; return 1 }`, "", "resolve.capture"},
		{"adapter-cycle", `flow H(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; flow F(r: HTTP.Request) -> HTTP.Response { s = HTTP.Server.new(8080, F, H); return HTTP.Response.Text.new(200, "ok") }`, "", "check.cycle"},
		{"match-hidden-head-failure", `flow F() -> Int! { return Match.new(1, Math.add(Math.div(1, 2), 1): 3, _: 4) }`, "", "check.fallible-case"},
		{"effectful-match-branch", `flow W() -> Int! { e = File.Append.new(File.Path.new("x"), UTF8.Bytes.new("x")); commit e; return 1 }; flow F() -> Int! { return Match.new(1, 1: W(), _: 2) }`, "", "check.effectful-match"},
		{"nominal-error", `flow F(e: Error) -> Error { return e }; return F(Record.new(kind: "K", message: "M", input: Record.new(), trace: List.new(1)))`, "", "check.type-mismatch"},
		{"bare-list-type", `flow F(x: List) -> Int { return 0 }`, "", "check.type-constraint"},
		{"invalid-type-argument", `flow F(x: Int<String>) -> Int { return 0 }`, "", "check.type-constraint"},
		{"duplicate-record-type", `flow F(x: Record{a: Int, a: Int}) -> Int { return 0 }`, "", "check.type-constraint"},
		{"constant-scalar", `return Scalar.new(String.concat("", String.lower("A")))`, "Scalar", ""},
		{"mixed-named-valid", `return Math.sub(9, right_value: 3)`, "Int", ""},
		{"named-order-valid", `return Math.sub(right_value: 3, left_value: 9)`, "Int", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
			if tc.code != "" {
				if got.status != 1 {
					t.Fatalf("want rejection: %+v", got)
				}
				if d := diagnostics(t, got.stderr).Diagnostics[0]; d.Code != tc.code {
					t.Fatalf("want %s: %+v", tc.code, d)
				}
				return
			}
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("%+v", got)
			}
			var output struct {
				ResultType string `json:"result_type"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &output); err != nil {
				t.Fatal(err)
			}
			if output.ResultType != tc.kind {
				t.Fatalf("got %s, want %s", output.ResultType, tc.kind)
			}
		})
	}
}

func TestVerifierRejections(t *testing.T) {
	cases := []struct{ name, source, code string }{
		{"empty-list", `return List.new()`, "check.missing-type"},
		{"empty-map", `return Map.new()`, "check.missing-type"},
		{"positional-record", `return Record.new(1)`, "check.unknown-argument"},
		{"named-list", `return List.new(value: 1)`, "check.unknown-argument"},
		{"boolean-range", `return Range.new(true, false, true, true)`, "check.type-constraint"},
		{"wrong-range-value", `return Range.contains(Range.new(1, 2, true, true), "a")`, "check.type-mismatch"},
		{"wrong-map-key", `return Map.get(Map.new(Record.new(key: "a", value: 1)), 1)`, "check.type-mismatch"},
		{"unknown-type", `flow F(x: Missing) -> Int { return 1 }`, "resolve.unknown-name"},
		{"record-extra-field", `flow F(x: Record{a: Int}) -> Int { return x.a }; return F(Record.new(a: 1, b: 2))`, "check.type-mismatch"},
		{"record-null", `flow F(x: Record{a: Int}) -> Int { return x.a }; return F(Record.new(a: null))`, "check.type-mismatch"},
		{"wrong-list-callback-output", `flow F(x: Int) -> String { return "a" }; return List.filter(List.new(1), F)`, "check.type-mismatch"},
		{"wrong-fold-state", `flow F(acc: String, x: Int) -> String { return acc }; return List.fold(List.new(1), 0, F)`, "check.type-mismatch"},
		{"wrong-fold-output", `flow F(acc: Int, x: Int) -> String { return "a" }; return List.fold(List.new(1), 0, F)`, "check.type-mismatch"},
		{"wrong-flat-map-output", `flow F(x: Int) -> String { return "a" }; return List.flat_map(List.new(1), F)`, "check.type-mismatch"},
		{"wrong-unfold-record", `flow F(x: Int) -> Record{value: Int} { return Record.new(value: x) }; return List.unfold(0, 2, F)`, "check.type-mismatch"},
		{"wrong-unfold-state", `flow F(x: Int) -> Record{value: Int, next_state: String} { return Record.new(value: x, next_state: "x") }; return List.unfold(0, 2, F)`, "check.type-mismatch"},
		{"wrong-unfold-count", `flow F(x: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: x, next_state: x) }; return List.unfold(0, "2", F)`, "check.type-mismatch"},
		{"wrong-callback-arity", `flow F(x: Int, y: Int) -> Int { return x }; return List.map(List.new(1), F)`, "check.argument-count"},
		{"wrong-handler-output", `flow H(e: Error) -> String { return "x" }; flow F() -> Int onError H { return 1 }`, "check.type-mismatch"},
		{"fallible-self-handler", `flow H(e: Error) -> Int! { return 1 }; flow F() -> Int onError H { return 1 }`, "check.type-mismatch"},
		{"wrong-http-handler", `flow F(x: Int) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; return HTTP.Server.new(8080, F, E)`, "check.type-mismatch"},
		{"wrong-http-error", `flow F(x: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> Int { return 1 }; return HTTP.Server.new(8080, F, E)`, "check.type-mismatch"},
		{"two-defaults", `return Match.new(1, _: 1, _: 2)`, "check.match-default"},
		{"incomparable-head", `return Match.new(List.new(1), List.new(1): 2, _: 3)`, "check.type-constraint"},
		{"list-as-range", `return Range.contains(List.new(1), 1)`, "check.type-mismatch"},
		{"range-as-list", `return List.length(Range.new(1, 2, true, true))`, "check.type-mismatch"},
		{"invalid-unreachable-branch", `return Match.new(1, 1: 2, _: Missing())`, "resolve.unknown-name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
			if got.status != 1 || got.stdout != "" {
				t.Fatalf("want rejection: %+v", got)
			}
			d := diagnostics(t, got.stderr).Diagnostics[0]
			if d.Code != tc.code {
				t.Fatalf("want %s: %+v", tc.code, d)
			}
		})
	}
}
