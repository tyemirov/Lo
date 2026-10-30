package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type runtimeValue struct {
	Type    string          `json:"type"`
	Value   json.RawMessage `json:"value"`
	Status  string          `json:"status"`
	Error   *runtimeValue   `json:"error"`
	Kind    string          `json:"kind"`
	Message string          `json:"message"`
	Input   *runtimeValue   `json:"input"`
	Trace   []runtimeValue  `json:"trace"`
}

func runValue(t *testing.T, source string, options ...string) runtimeValue {
	t.Helper()
	args := append([]string{"run", sourceFile(t, source), "--format", "json"}, options...)
	got := run(t, args...)
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("run: %+v", got)
	}
	var out struct {
		Command, Status string
		Result          runtimeValue
	}
	if err := json.Unmarshal([]byte(got.stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Command != "run" || out.Status != "ok" {
		t.Fatalf("result envelope: %s", got.stdout)
	}
	return out.Result
}

func valueJSON(t *testing.T, v runtimeValue, expected string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(v.Value, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("value: %s, want %s", v.Value, expected)
	}
}

func TestRunValues(t *testing.T) {
	for _, tc := range []struct{ name, source, typ, value string }{
		{"sum", "return Math.sum(List.new(1, 2, 3, 4))", "Int", `"10"`},
		{"large", "return Math.mul(18446744073709551616, 18446744073709551616)", "Int", `"340282366920938463463374607431768211456"`},
		{"immutable", "a = 18446744073709551616; b = Math.add(a, 1); return Math.sub(b, a)", "Int", `"1"`},
		{"unicode", `return String.length("aé😀")`, "Int", `"3"`},
		{"scalars", `return String.FromScalars.new(List.new(Scalar.new("é"), Scalar.new("😀")))`, "String", `"é😀"`},
		{"concat", `return String.concat("a", String.upper("é"), String.lower("B"))`, "String", `"aÉb"`},
		{"bytes", `return Bytes.new("é")`, "Bytes", `"w6k="`},
		{"bool", `return Bool.new(true)`, "Bool", `true`},
		{"null", `return null`, "Null", `null`},
		{"float", `return Float.new(1.25)`, "Float", `"1.25"`},
		{"path", `return File.Path.new("relative.txt")`, "File.Path", `"relative.txt"`},
		{"sum-empty", `flow Empty() -> List<Int> { return List.new() }; return Math.sum(Empty())`, "Int", `"0"`},
		{"product-empty", `flow Empty() -> List<Int> { return List.new() }; return Math.product(Empty())`, "Int", `"1"`},
		{"merge", `a = Record.new(x: 1, y: "old"); b = Record.merge(a, Record.new(y: "new")); return b.y`, "String", `"new"`},
		{"map-duplicate", `flow Get() -> Int! { m = Map.new(Record.new(key: "x", value: 1), Record.new(key: "x", value: 2)); return Map.get(m, "x") }; flow Unwrap(error: Error) -> Int { return 0 }; flow Total() -> Int onError Unwrap { return Get() }; return Total()`, "Int", `"2"`},
		{"range-empty", `return Range.is_empty(Range.new(1, 2, false, false))`, "Bool", `true`},
		{"range-includes", `return Range.contains(Range.new("a", "z", true, false), "m")`, "Bool", `true`},
		{"match-lazy", `return Match.new(1, 1: 7, _: Math.div(1, 0))`, "Int!", `{"type":"Int","value":"7"}`},
		{"match-predicate", `flow Positive(x: Int) -> Bool { return Math.gt(x, 0) }; return Match.new(2, when Positive: "yes", _: "no")`, "String", `"yes"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := runValue(t, tc.source)
			if v.Type != tc.typ {
				t.Fatalf("type %s, want %s", v.Type, tc.typ)
			}
			valueJSON(t, v, tc.value)
		})
	}
	got := run(t, "run", "examples/list_sum.lo")
	if got.status != 0 || got.stdout != "10\n" || got.stderr != "" {
		t.Fatalf("text result: %+v", got)
	}
	got = run(t, "run", sourceFile(t, "flow Unused() -> Int { return 1 }"), "--format", "json")
	if got.status != 0 || strings.Contains(got.stdout, "result") {
		t.Fatalf("absent return: %+v", got)
	}
}

func TestRunListProcessors(t *testing.T) {
	for _, tc := range []struct{ name, source, typ, value string }{
		{"map-context", `flow Add(amount: Int, item: Int) -> Int { return Math.add(amount, item) }; return List.map(List.new(1, 2), 10, Add)`, "List<Int>", `[{"type":"Int","value":"11"},{"type":"Int","value":"12"}]`},
		{"filter-string", `flow A(s: Scalar) -> Bool { return Scalar.eq(s, Scalar.new("a")) }; return List.length(List.filter("banana", A))`, "Int", `"3"`},
		{"fold-context", `flow Step(scale: Int, acc: Int, x: Int) -> Int { return Math.add(Math.mul(acc, scale), x) }; return List.fold(List.new(1, 2, 3), 0, 10, Step)`, "Int", `"123"`},
		{"flat-map", `flow Pair(x: Int) -> List<Int> { return List.new(x, Math.add(x, 1)) }; return List.flat_map(List.new(1, 3), Pair)`, "List<Int>", `[{"type":"Int","value":"1"},{"type":"Int","value":"2"},{"type":"Int","value":"3"},{"type":"Int","value":"4"}]`},
		{"unfold", `flow Step(delta: Int, state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, delta)) }; return List.unfold(0, 3, 2, Step)`, "List<Int>", `[{"type":"Int","value":"0"},{"type":"Int","value":"2"},{"type":"Int","value":"4"}]`},
		{"unfold-negative", `flow Step(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: state) }; return List.unfold(0, -1, Step)`, "List<Int>", `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := runValue(t, tc.source)
			if v.Type != tc.typ {
				t.Fatal(v.Type)
			}
			valueJSON(t, v, tc.value)
		})
	}
}

func TestRunRecoverableErrors(t *testing.T) {
	for _, tc := range []struct{ source, kind string }{
		{`return Math.div(1, 0)`, "Math.DivideByZero"},
		{`return Math.mod(1, 0)`, "Math.ModuloByZero"},
		{`flow Empty() -> List<Int> { return List.new() }; return Math.min(Empty())`, "Math.EmptyList"},
		{`flow Empty() -> List<Int> { return List.new() }; return Math.max(Empty())`, "Math.EmptyList"},
		{`return Map.get(Map.new(Record.new(key: "a", value: 1)), "b")`, "Map.MissingKey"},
	} {
		got := run(t, "run", sourceFile(t, tc.source), "--format", "json")
		if got.status != 1 || got.stdout != "" {
			t.Fatalf("recoverable failure: %+v", got)
		}
		var out struct {
			Diagnostics []struct {
				Code  string
				Error runtimeValue
			}
		}
		if err := json.Unmarshal([]byte(got.stderr), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != "runtime.unhandled-error" || out.Diagnostics[0].Error.Kind != tc.kind {
			t.Fatalf("error: %s", got.stderr)
		}
	}
	for _, op := range []struct{ name, want string }{{"Math.div", "-2"}, {"Math.mod", "-1"}} {
		v := runValue(t, fmt.Sprintf("return %s(-7, 3)", op.name))
		if v.Status != "ok" {
			t.Fatalf("fallible success: %+v", v)
		}
		valueJSON(t, v, fmt.Sprintf(`{"type":"Int","value":%q}`, op.want))
	}
	v := runValue(t, `flow Handle(error: Error) -> Error { return error }; flow Divide(x: Int) -> Int! { return Math.div(8, x) }; flow Outer() -> Error onError Handle { unused = Divide(0); return Error.new("missed", "", Record.new()) }; return Outer()`)
	if v.Type != "Error" || v.Kind != "Math.DivideByZero" || len(v.Trace) != 2 {
		t.Fatalf("trace: %+v", v)
	}
	for i, want := range []string{"Divide", "Outer"} {
		var fields map[string]runtimeValue
		if err := json.Unmarshal(v.Trace[i].Value, &fields); err != nil {
			t.Fatal(err)
		}
		valueJSON(t, fields["flow"], fmt.Sprintf("%q", want))
	}
	v = runValue(t, `flow Divide(x: Int) -> Int! { return Math.div(8, x) }; flow Handle(error: Error) -> Error { return error }; flow Outer() -> Error onError Handle { xs = List.map(List.new(2, 0, 4), Divide); return Error.new("missed", "", Record.new()) }; return Outer()`)
	if v.Kind != "Math.DivideByZero" || len(v.Trace) != 3 {
		t.Fatalf("list trace: %+v", v)
	}
	var frame map[string]runtimeValue
	if err := json.Unmarshal(v.Trace[1].Value, &frame); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, frame["operation"], `"List.map"`)
	var input map[string]runtimeValue
	if err := json.Unmarshal(frame["input"].Value, &input); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, input["index"], `{"type":{"type":"String","value":"Int"}}`)
	got := run(t, "run", sourceFile(t, `flow Divide(x: Int) -> Int! { return Math.div(8, x) }; return Divide(0)`))
	if got.status != 1 || !strings.Contains(got.stderr, "Divide") || !strings.Contains(got.stderr, "Math.div") {
		t.Fatalf("text trace: %+v", got)
	}
}

func TestRunFileReads(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("aé😀"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`flow Read() -> String! { bytes = File.read(File.Path.new(%q)); return UTF8.String.new(bytes) }; return Read()`, file.Name())
	v := runValue(t, source, "--read-root", dir)
	valueJSON(t, v, `{"type":"String","value":"aé😀"}`)
	denied := run(t, "run", sourceFile(t, source), "--format", "json")
	if denied.status != 2 || diagnostics(t, denied.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("authorization: %+v", denied)
	}
	lazy := fmt.Sprintf(`return Match.new(true, true: Bytes.new("safe"), _: File.read(File.Path.new(%q)))`, file.Name())
	v = runValue(t, lazy)
	valueJSON(t, v, `{"type":"Bytes","value":"c2FmZQ=="}`)
	missing := fmt.Sprintf(`return File.read(File.Path.new(%q))`, filepath.Join(dir, "missing"))
	got := run(t, "run", sourceFile(t, missing), "--read-root", dir, "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"File.ReadFailed"`) {
		t.Fatalf("missing file: %+v", got)
	}
	invalid, err := os.Create(filepath.Join(dir, "invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.Write([]byte{0xff}); err != nil {
		t.Fatal(err)
	}
	if err := invalid.Close(); err != nil {
		t.Fatal(err)
	}
	bad := fmt.Sprintf(`flow Decode() -> String! { bytes = File.read(File.Path.new(%q)); return UTF8.String.new(bytes) }; return Decode()`, invalid.Name())
	got = run(t, "run", sourceFile(t, bad), "--read-root", dir, "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"UTF8.InvalidBytes"`) {
		t.Fatalf("UTF8 boundary: %+v", got)
	}
}

func TestRunStoredFallibility(t *testing.T) {
	v := runValue(t, `flow Make() -> List<Int!> { return List.new(Math.div(1, 0), 2) }; return Make()`)
	var items []runtimeValue
	if err := json.Unmarshal(v.Value, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != "error" || items[0].Error.Kind != "Math.DivideByZero" || len(items[0].Error.Trace) != 0 || items[1].Status != "ok" {
		t.Fatalf("stored list: %s", v.Value)
	}
	v = runValue(t, `flow Make() -> Map<String, Int!> { return Map.new(Record.new(key: "bad", value: Math.div(1, 0))) }; return Make()`)
	var entries []struct{ Key, Value runtimeValue }
	if err := json.Unmarshal(v.Value, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Value.Status != "error" {
		t.Fatalf("stored map: %s", v.Value)
	}
	got := run(t, "run", sourceFile(t, `flow Make() -> Map<String, Int!> { return Map.new(Record.new(key: "bad", value: Math.div(1, 0))) }; return Map.get(Make(), "bad")`), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"Math.DivideByZero"`) {
		t.Fatalf("stored map lookup: %+v", got)
	}
	v = runValue(t, `flow Make() -> Error! { return Error.new("data", "", Record.new()) }; return Make()`)
	if v.Type != "Error!" || v.Status != "ok" {
		t.Fatalf("Error success: %+v", v)
	}
	v = runValue(t, `flow Inner() -> Record{x: Int}! { return Record.new(x: 3) }; flow Outer() -> Record! { return Inner() }; return Outer()`)
	valueJSON(t, v, `{"type":"Record","value":{"x":{"type":"Int","value":"3"}}}`)
}

func TestRunHostLimitsAndAuthorization(t *testing.T) {
	for _, source := range []string{
		`return 1`,
		`flow H(e: Error) -> Int { return 9 }; flow F() -> Int onError H { return Math.add(1, 2) }; return F()`,
	} {
		got := run(t, "run", sourceFile(t, source), "--format", "json", "--max-steps", "1")
		if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
			t.Fatalf("execution limit: %+v", got)
		}
	}
	source := `flow H(e: Error) -> Bytes { return Bytes.new("caught") }; flow F() -> Bytes onError H { return File.read(File.Path.new("not-authorized")) }; return F()`
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("host denial caught by Lo: %+v", got)
	}
	dir, outside := t.TempDir(), t.TempDir()
	file, err := os.Create(filepath.Join(outside, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("outside"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(file.Name(), link); err != nil {
		t.Fatal(err)
	}
	got = run(t, "run", sourceFile(t, fmt.Sprintf(`return File.read(File.Path.new(%q))`, link)), "--read-root", dir, "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"File.ReadFailed"`) || strings.Contains(got.stdout, "outside") {
		t.Fatalf("root escape: %+v", got)
	}
	for _, args := range [][]string{
		{"run", "--max-steps", "0"}, {"run", "--max-steps", "-1"}, {"run", "--max-steps", "1", "--max-steps", "2"},
		{"check", "--read-root", dir}, {"inspect", "--max-steps", "1"},
		{"check", "--write-root", dir}, {"run", "--listen", "0"},
		{"serve", "--listen", "-1"}, {"serve", "--listen", "65536"}, {"serve", "--listen", "0", "--listen", "0"},
		{"run", "--read-root", dir, "--read-root", dir + "/."},
		{"run", "--write-root", dir, "--write-root", dir + "/."},
	} {
		got := run(t, append(args, sourceFile(t, "return 1"))...)
		if got.status != 2 || got.stdout != "" {
			t.Fatalf("runtime options: %+v", got)
		}
	}
}

func TestRunExpandedValueLimit(t *testing.T) {
	var program strings.Builder
	fmt.Fprintf(&program, "v0 = %q\n", strings.Repeat("a", 4096))
	for i := 1; i <= 13; i++ {
		fmt.Fprintf(&program, "v%d = List.new(v%d, v%d)\n", i, i-1, i-1)
	}
	program.WriteString("return v13\n")
	got := run(t, "run", sourceFile(t, program.String()), "--format", "json")
	if got.status != 2 {
		t.Fatalf("expanded value limit: status %d, stdout bytes %d", got.status, len(got.stdout))
	}
	if diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" || got.stdout != "" {
		t.Fatal("expanded value must fail before result output")
	}
}

func TestRunResourceLimits(t *testing.T) {
	var integer, depth, weight strings.Builder
	integer.WriteString("v0 = 2\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&integer, "v%d = Math.mul(v%d, v%d)\n", i, i-1, i-1)
	}
	integer.WriteString("return v20")
	for i := 0; i < 260; i++ {
		fmt.Fprintf(&depth, "flow F%d() -> Int { return F%d() }\n", i, i+1)
	}
	depth.WriteString("flow F260() -> Int { return 1 }\nreturn F0()")
	fmt.Fprintf(&weight, "a = %q\n", strings.Repeat("x", 1<<20))
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&weight, "v%d = String.concat(a, a)\n", i)
	}
	weight.WriteString("return 1")
	for _, tc := range []struct {
		name, source string
		options      []string
	}{
		{"integer-magnitude", integer.String(), nil},
		{"flow-depth", depth.String(), nil},
		{"accumulated-values", weight.String(), []string{"--max-live-bytes", "33554432"}},
		{"unfold-count", `flow Step(n: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: n, next_state: n) }; return List.unfold(0, 1000001, Step)`, []string{"--max-list-items", "1000000"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, append([]string{"run", sourceFile(t, tc.source), "--format", "json"}, tc.options...)...)
			if got.status != 2 || got.stdout != "" || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
				t.Fatalf("resource limit: %+v", got)
			}
		})
	}
	t.Run("file-bytes", func(t *testing.T) {
		dir := t.TempDir()
		file, err := os.Create(filepath.Join(dir, "large"))
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate((16 << 20) + 1); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		program := fmt.Sprintf(`return File.read(File.Path.new(%q))`, file.Name())
		got := run(t, "run", sourceFile(t, program), "--read-root", dir, "--format", "json")
		if got.status != 2 || got.stdout != "" || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
			t.Fatalf("file limit: %+v", got)
		}
	})
}

func TestRunCountLetterExample(t *testing.T) {
	got := run(t, "run", "examples/count_letter.lo", "--read-root", "examples")
	if got.status != 0 || got.stdout != "3\n" || got.stderr != "" {
		t.Fatalf("count letter: %+v", got)
	}
}

func TestRunRangeOrder(t *testing.T) {
	for _, tc := range []struct {
		start, end                      string
		includeStart, includeEnd, empty bool
	}{
		{"2", "1", true, true, true}, {"1", "1", true, true, false}, {"1", "1", true, false, true}, {"1", "1", false, true, true},
		{"1", "2", false, false, true}, {"1", "3", false, false, false}, {"1", "2", true, false, false}, {"1", "2", false, true, false},
		{`Scalar.new("a")`, `Scalar.new("b")`, false, false, true}, {`Scalar.new("a")`, `Scalar.new("c")`, false, false, false},
		{`Scalar.new("\uD7FF")`, `Scalar.new("\uE000")`, false, false, true},
		{`"a"`, `"a\u0000"`, false, false, true}, {`"a"`, `"a\u0000\u0000"`, false, false, false}, {`"a"`, `"b"`, false, false, false},
	} {
		source := fmt.Sprintf("return Range.is_empty(Range.new(%s,%s,%t,%t))", tc.start, tc.end, tc.includeStart, tc.includeEnd)
		v := runValue(t, source)
		valueJSON(t, v, fmt.Sprintf("%t", tc.empty))
	}
	for _, tc := range []struct {
		op          string
		left, right int
		result      int
	}{
		{"Math.div", 7, 3, 2}, {"Math.div", -7, 3, -2}, {"Math.div", 7, -3, -2}, {"Math.div", -7, -3, 2},
		{"Math.mod", 7, 3, 1}, {"Math.mod", -7, 3, -1}, {"Math.mod", 7, -3, 1}, {"Math.mod", -7, -3, -1},
	} {
		v := runValue(t, fmt.Sprintf("return %s(%d,%d)", tc.op, tc.left, tc.right))
		valueJSON(t, v, fmt.Sprintf(`{"type":"Int","value":"%d"}`, tc.result))
	}
	for _, tc := range []struct{ op, result string }{{"Math.min", "-4"}, {"Math.max", "8"}, {"Math.product", "-64"}} {
		v := runValue(t, fmt.Sprintf("return %s(List.new(2, -4, 8))", tc.op))
		if v.Status == "ok" {
			var inner runtimeValue
			if err := json.Unmarshal(v.Value, &inner); err != nil {
				t.Fatal(err)
			}
			v = inner
		}
		valueJSON(t, v, fmt.Sprintf("%q", tc.result))
	}
}

func TestRunFallibleListVariants(t *testing.T) {
	for _, tc := range []struct{ name, flow, call string }{
		{"map", `flow F(x: Int) -> Int! { return Math.div(8, x) }`, `List.map(List.new(2, 0, 4), F)`},
		{"filter", `flow F(x: Int) -> Bool! { n = Math.div(8, x); return Math.gt(n, 0) }`, `List.filter(List.new(2, 0, 4), F)`},
		{"fold", `flow F(state: Int, x: Int) -> Int! { return Math.div(state, x) }`, `List.fold(List.new(2, 0, 4), 8, F)`},
		{"flat_map", `flow F(x: Int) -> List<Int>! { n = Math.div(8, x); return List.new(n) }`, `List.flat_map(List.new(2, 0, 4), F)`},
		{"unfold", `flow F(state: Int) -> Record{value: Int, next_state: Int}! { n = Math.div(8, state); return Record.new(value: n, next_state: Math.sub(state, 1)) }`, `List.unfold(1, 3, F)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.flow + `; flow H(e: Error) -> Error { return e }; flow Main() -> Error onError H { unused = ` + tc.call + `; return Error.new("missed", "", Record.new()) }; return Main()`
			v := runValue(t, source)
			if v.Kind != "Math.DivideByZero" || len(v.Trace) != 3 {
				t.Fatalf("fallible list: %+v", v)
			}
			var frame map[string]runtimeValue
			if err := json.Unmarshal(v.Trace[1].Value, &frame); err != nil {
				t.Fatal(err)
			}
			valueJSON(t, frame["operation"], fmt.Sprintf("%q", "List."+tc.name))
		})
	}
}
