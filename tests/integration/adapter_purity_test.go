package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdapterPurity(t *testing.T) {
	const read = `flow Recover(e: Error) -> Int { return 0 }; flow Observe() -> Int onError Recover { data = File.read(File.Path.new("not-observed")); return 1 }; `
	const commit = `flow Recover(e: Error) -> Int { return 0 }; flow Activate() -> Int onError Recover { target = File.Write.new(File.Path.new("not-activated"), Bytes.new("x")); commit target; return 1 }; flow Observe() -> Int onError Recover { return Activate() }; `
	for _, tc := range []struct{ name, callback, call string }{
		{"map", `flow Callback(x: Int) -> Int { return Observe() }`, `List.map(List.new(1), Callback)`},
		{"filter", `flow Callback(x: Int) -> Bool { return Math.eq(Observe(), x) }`, `List.filter(List.new(1), Callback)`},
		{"fold", `flow Callback(state: Int, x: Int) -> Int { return Math.add(state, Observe()) }`, `List.fold(List.new(1), 0, Callback)`},
		{"flat-map", `flow Callback(x: Int) -> List<Int> { return List.new(Observe()) }`, `List.flat_map(List.new(1), Callback)`},
		{"unfold", `flow Callback(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: Observe(), next_state: state) }`, `List.unfold(0, 1, Callback)`},
		{"predicate", `flow Callback(x: Int) -> Bool { return Math.eq(Observe(), x) }`, `Match.new(1, when Callback: 1, _: 0)`},
	} {
		t.Run(tc.name+"-transitive-read", func(t *testing.T) {
			adapterPurityRejected(t, read+tc.callback+`; return `+tc.call, "check.effectful-callback")
		})
		t.Run(tc.name+"-transitive-commit", func(t *testing.T) {
			recovery := map[string]string{
				"map":       `flow CallbackError(e: Error) -> Int { return 0 }; `,
				"filter":    `flow CallbackError(e: Error) -> Bool { return false }; `,
				"fold":      `flow CallbackError(e: Error) -> Int { return 0 }; `,
				"flat-map":  `flow CallbackError(e: Error) -> List<Int> { return List.new(0) }; `,
				"unfold":    `flow CallbackError(e: Error) -> Record{value: Int, next_state: Int} { return Record.new(value: 0, next_state: 0) }; `,
				"predicate": `flow CallbackError(e: Error) -> Bool { return false }; `,
			}[tc.name]
			callback := strings.Replace(tc.callback, " { return ", " onError CallbackError { return ", 1)
			adapterPurityRejected(t, commit+recovery+callback+`; return `+tc.call, "check.effectful-callback")
		})
	}
	for _, tc := range []struct{ name, expression string }{
		{"selector", `Match.new(Observe(), 1: 1, _: 0)`},
		{"branch", `Match.new(1, 1: Observe(), _: 0)`},
		{"case-head", `Match.new(1, Observe(): 1, _: 0)`},
	} {
		t.Run(tc.name+"-transitive-commit", func(t *testing.T) {
			adapterPurityRejected(t, commit+`return `+tc.expression, "check.effectful-match")
		})
	}
	t.Run("case-head-transitive-read", func(t *testing.T) {
		adapterPurityRejected(t, read+`return Match.new(1, Observe(): 1, _: 0)`, "check.effectful-match")
	})
	t.Run("self-handler-transitive-read", func(t *testing.T) {
		adapterPurityRejected(t, read+`flow Handler(e: Error) -> Int { return Observe() }; flow Main() -> Int onError Handler { return Math.div(1, 0) }; return Main()`, "check.effectful-callback")
	})
	t.Run("http-error-handler-transitive-read", func(t *testing.T) {
		adapterPurityRejected(t, read+`flow Handler(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow Boundary(e: Error) -> HTTP.Response { data = Observe(); return HTTP.Response.Text.new(500, "error") }; return HTTP.Server.new(0, Handler, Boundary)`, "check.effectful-callback")
	})
	t.Run("unselected-observations", func(t *testing.T) {
		root := fileFixtureRoot(t)
		selected := filepath.Join(root, "selected")
		if err := os.WriteFile(selected, []byte("chosen"), 0600); err != nil {
			t.Fatal(err)
		}
		program := fmt.Sprintf(`flow Read(path: String) -> String! { return UTF8.String.new(File.read(File.Path.new(path))) }; flow Main() -> String! { return Match.new(Read(%q), "chosen": Read(%q), _: Read("not-authorized")) }; return Main()`, selected, selected)
		valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, program), "--read-root", root, "--format", "json")), `"chosen"`)
	})
	t.Run("conditional-observation-contaminates-callback", func(t *testing.T) {
		adapterPurityRejected(t, read+`flow Callback(x: Int) -> Int { return Match.new(x, 1: 1, _: Observe()) }; return List.map(List.new(1), Callback)`, "check.effectful-callback")
	})
	t.Run("unused-nested-flow", func(t *testing.T) {
		valueJSON(t, runValue(t, read+`flow Callback(x: Int) -> Int { flow Unused() -> Int { return Observe() }; return x }; return List.length(List.map(List.new(1), Callback))`), `"1"`)
	})
	t.Run("stored-http-handler", func(t *testing.T) {
		source := read + `flow Handler(r: HTTP.Request) -> HTTP.Response { data = Observe(); return HTTP.Response.Text.new(200, "ok") }; flow Boundary(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; flow Callback(x: Int) -> Int { server = HTTP.Server.new(0, Handler, Boundary); return x }; return List.length(List.map(List.new(1), Callback))`
		valueJSON(t, runValue(t, source), `"1"`)
	})
	t.Run("device-requirements-independent", func(t *testing.T) {
		source := `flow Callback(x: Int) -> Tensor<FP32>! { value = Tensor.new(List.new(1.0), List.new(1)); remote = Tensor.to(value, Device.new("cuda-sim:0")); return Tensor.to(remote, Device.new("cpu")) }; return List.map(List.new(1), Callback)`
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 0 {
			t.Fatalf("pure tensor callback: %+v", got)
		}
	})
	t.Run("ordered-fold-observation-and-commit", func(t *testing.T) {
		root := fileFixtureRoot(t)
		output := filepath.Join(root, "fold")
		source := fmt.Sprintf(`flow Step(path: File.Path, state: Int, x: Int) -> Int! { previous = File.read(path); target = File.Write.new(path, previous); commit target; return Math.add(state, x) }; flow Main() -> Int! { return Flow.fold(List.new(1, 2), 0, File.Path.new(%q), Step) }; return Main()`, output)
		if err := os.WriteFile(output, []byte("stable"), 0600); err != nil {
			t.Fatal(err)
		}
		valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, source), "--read-root", root, "--write-root", root, "--format", "json")), `"3"`)
	})
}

func adapterPurityRejected(t *testing.T, source, code string) {
	t.Helper()
	got := run(t, "check", sourceFile(t, source), "--format", "json")
	if got.status != 1 {
		t.Fatalf("expected %s: %+v", code, got)
	}
	report := diagnostics(t, got.stderr)
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != code || (!strings.Contains(report.Diagnostics[0].Message, "read") && !strings.Contains(report.Diagnostics[0].Message, "commit")) {
		t.Fatalf("expected informative %s: %s", code, got.stderr)
	}
}
