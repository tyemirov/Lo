package integration_test

import (
	"encoding/json"

	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowFold(t *testing.T) {
	t.Run("ordered-activation", func(t *testing.T) {
		root := fileFixtureRoot(t)
		output := filepath.Join(root, "order")
		source := fmt.Sprintf(`flow Step(path: File.Path, state: Int, item: String) -> Int! {
			target = File.Append.new(path, UTF8.Bytes.new(item)); commit target; return Math.add(state, 1)
		}; flow Run() -> Int! { return Flow.fold(List.new("a", "b", "c"), 0, File.Path.new(%q), Step) }; return Run()`, output)
		v := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--write-root", root, "--format", "json"))
		valueJSON(t, v, `"3"`)
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "abc" {
			t.Fatalf("ordered callbacks: %q: %v", data, err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		source := `flow Empty() -> List<Int> { return List.new() }; flow Step(state: Int, item: Int) -> Int { return Math.add(state, item) }; return Flow.fold(Empty(), 7, Step)`
		v := runValue(t, source)
		valueJSON(t, v, `"7"`)
	})
	t.Run("list-remains-pure", func(t *testing.T) {
		source := `flow Step(state: Int, item: Int) -> Int! { target = File.Append.new(File.Path.new("unused"), UTF8.Bytes.new("x")); commit target; return state }; flow Run() -> Int! { return List.fold(List.new(1), 0, Step) }; return Run()`
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != "check.effectful-callback" {
			t.Fatalf("List callback purity: %+v", got)
		}
	})
	t.Run("total-rejects-activation", func(t *testing.T) {
		source := `flow Step(state: Int, item: Int) -> Int! { target = File.Append.new(File.Path.new("unused"), UTF8.Bytes.new("x")); commit target; return state }; flow Run() -> Int { return Flow.fold(List.new(1), 0, Step) }; return Run()`
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 {
			t.Fatalf("total effectful flow accepted: %+v", got)
		}
	})
	t.Run("inspection", func(t *testing.T) {
		root := fileFixtureRoot(t)
		output := filepath.Join(root, "untouched")
		source := fmt.Sprintf(`flow Step(path: File.Path, state: Int, item: String) -> Int! { target = File.Append.new(path, UTF8.Bytes.new(item)); commit target; return Math.add(state, 1) }; flow Run() -> Int! { return Flow.fold(List.new("a"), 0, File.Path.new(%q), Step) }; return Run()`, output)
		got := run(t, "inspect", sourceFile(t, source), "--format", "json")
		if got.status != 0 || !strings.Contains(got.stdout, `"commits":true`) || !strings.Contains(got.stdout, `File.append`) {
			t.Fatalf("fold activation inspection: %+v", got)
		}
		var report map[string]json.RawMessage
		if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("inspection mutated fixture: %v", err)
		}
	})
	t.Run("callback-failure-stops-suffix", func(t *testing.T) {
		root := fileFixtureRoot(t)
		output := filepath.Join(root, "attempts")
		program := fmt.Sprintf(`flow Step(path: File.Path, state: Int, item: Int) -> Int! {
			target = File.Append.new(path, UTF8.Bytes.new("x")); commit target; return Math.add(state, Math.div(10, item))
		}; flow Run() -> Int! { return Flow.fold(List.new(2, 0, 5), 0, File.Path.new(%q), Step) }; return Run()`, output)
		got := runProgram(t, sourceFile(t, program), "run", "--write-root", root, "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "Math.DivideByZero") || !strings.Contains(got.stderr, `"value":"Flow.fold"`) {
			t.Fatalf("callback failure trace: %+v", got)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "xx" {
			t.Fatalf("callback suffix executed: %q: %v", data, err)
		}
	})
}

func TestFlowFoldStepLimitProgress(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		fileFixture(t, root, name, name)
	}
	program := fmt.Sprintf(`flow Counter(state: Int) -> Record{value: Int, next_state: Int} {
		return Record.new(value: state, next_state: Math.add(state, 1))
	}; flow Delay(entry: File.Entry) -> Int {
		return Match.new(entry.name, "c": List.length(List.unfold(0, 1000, Counter)), _: 0)
	}; flow Step(state: Int, entry: File.Entry) -> Int! {
		target = File.Remove.new(List.new(entry)); commit target; delay = Delay(entry); return Math.add(state, Math.add(delay, 1))
	}; flow Outer(entries: List<File.Entry>, state: Int, item: Int) -> Int! {
		return Flow.fold(entries, state, Step)
	}; flow Clean() -> Int! {
		entries = File.list(File.Path.new(%q)); return Flow.fold(List.new(1), 0, entries, Outer)
	}; return Clean()`, root)
	got := runProgram(t, sourceFile(t, program), "run", "--list-root", root, "--remove-root", root, "--max-steps", "500", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("expected nested step limit: %+v", got)
	}
	var report struct {
		Diagnostics []struct {
			Progress []runtimeValue `json:"flow_progress"`
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 || len(report.Diagnostics[0].Progress) != 2 {
		t.Fatalf("missing nested limit progress: %+v", got)
	}
	for index, completed := range []string{`"0"`, `"2"`} {
		var fields map[string]runtimeValue
		if err := json.Unmarshal(report.Diagnostics[0].Progress[index].Value, &fields); err != nil {
			t.Fatal(err)
		}
		valueJSON(t, fields["completed_count"], completed)
		valueJSON(t, fields["state"], completed)
	}
	progressFields(t, got, 1, 1, "finalize", "")
	remaining, err := os.ReadDir(root)
	if err != nil || len(remaining) != 1 || remaining[0].Name() != "d" {
		t.Fatalf("step limit suffix: %v: %v", remaining, err)
	}
}
