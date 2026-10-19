package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestFlowFold(t *testing.T) {
	t.Run("ordered-activation", func(t *testing.T) {
		root := fileFixtureRoot(t)
		output := filepath.Join(root, "order")
		source := fmt.Sprintf(`flow Step(path: File.Path, state: Int, item: String) -> Int! {
			target = File.Append.new(path, UTF8.Bytes.new(item)); commit target; return Math.add(state, 1)
		}; flow Run() -> Int! { return Flow.fold(List.new("a", "b", "c"), 0, File.Path.new(%q), Step) }; return Run()`, output)
		v := fileOutput(t, run(t, "run", sourceFile(t, source), "--write-root", root, "--format", "json"))
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
		got := run(t, "run", sourceFile(t, program), "--write-root", root, "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "Math.DivideByZero") || !strings.Contains(got.stderr, `"value":"Flow.fold"`) {
			t.Fatalf("callback failure trace: %+v", got)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "xx" {
			t.Fatalf("callback suffix executed: %q: %v", data, err)
		}
	})
}

func TestFlowFoldNestedCancellationProgress(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		fileFixture(t, root, name, name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.remove = func(directory *os.Root, name string, count int) error {
		if err := directory.Remove(name); err != nil {
			return err
		}
		if count == 3 {
			cancel()
		}
		return nil
	}
	program := fmt.Sprintf(`flow Step(state: Int, entry: File.Entry) -> Int! {
		target = File.Remove.new(List.new(entry)); commit target; return Math.add(state, 1)
	}; flow Outer(entries: List<File.Entry>, state: Int, item: Int) -> Int! {
		return Flow.fold(entries, state, Step)
	}; flow Clean() -> Int! {
		entries = File.list(File.Path.new(%q)); return Flow.fold(List.new(1), 0, entries, Outer)
	}; return Clean()`, root)
	_, err, got := injectedFileProgram(t, ctx, root, program, filesystem)
	if err == nil || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.cancelled" {
		t.Fatalf("expected nested cancellation: %+v", got)
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
		t.Fatalf("missing outer and inner flow progress: %+v", got)
	}
	for index, completed := range []string{`"0"`, `"2"`} {
		var fields map[string]runtimeValue
		if err := json.Unmarshal(report.Diagnostics[0].Progress[index].Value, &fields); err != nil {
			t.Fatal(err)
		}
		valueJSON(t, fields["completed_count"], completed)
		valueJSON(t, fields["next_index"], completed)
		valueJSON(t, fields["state"], completed)
	}
	progressFields(t, got, 1, 1, "remove", "")
	if len(filesystem.removals) != 3 {
		t.Fatalf("removal attempts %v", filesystem.removals)
	}
}

func TestFlowFoldCleanupFailureProgress(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, name := range []string{"a", "b", "c"} {
		fileFixture(t, root, name, name)
	}
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.close = func(directory *os.Root) error {
		return errors.Join(directory.Close(), errors.New("injected fold cleanup failure"))
	}
	program := fmt.Sprintf(`flow Step(state: Int, entry: File.Entry) -> Int! {
		target = File.Remove.new(List.new(entry)); commit target; return Math.add(state, 1)
	}; flow Clean() -> Int! { entries = File.list(File.Path.new(%q)); return Flow.fold(entries, 0, Step) }; return Clean()`, root)
	_, err, got := injectedFileProgram(t, context.Background(), root, program, filesystem)
	if err == nil || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.host-failure" {
		t.Fatalf("expected cleanup failure: %+v", got)
	}
	var report struct {
		Diagnostics []struct {
			Progress []runtimeValue `json:"flow_progress"`
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 || len(report.Diagnostics[0].Progress) != 1 {
		t.Fatalf("missing completed flow progress: %+v", got)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(report.Diagnostics[0].Progress[0].Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["completed_count"], `"3"`)
	valueJSON(t, fields["state"], `"3"`)
	progressFields(t, got, 1, 1, "finalize", "")
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
	got := run(t, "run", sourceFile(t, program), "--list-root", root, "--remove-root", root, "--max-steps", "500", "--format", "json")
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

func TestFlowFoldGrantCloseProgress(t *testing.T) {
	for _, scenario := range []string{"successful-fold", "prior-cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			root := fileFixtureRoot(t)
			for _, name := range []string{"a", "b", "c"} {
				fileFixture(t, root, name, name)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
			closes := 0
			filesystem.close = func(directory *os.Root) error {
				closes++
				closed := directory.Close()
				if closes == 2 {
					return errors.Join(closed, errors.New("injected grant close failure"))
				}
				return closed
			}
			if scenario == "prior-cancellation" {
				filesystem.remove = func(directory *os.Root, name string, count int) error {
					if err := directory.Remove(name); err != nil {
						return err
					}
					if count == 2 {
						cancel()
					}
					return nil
				}
			}
			files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}}, filesystem)
			if err != nil {
				t.Fatal(err)
			}
			text := fmt.Sprintf(`flow Step(state: Int, entry: File.Entry) -> Int! {
				target = File.Remove.new(List.new(entry)); commit target; return Math.add(state, 1)
			}; flow Clean() -> Int! { entries = File.list(File.Path.new(%q)); return Flow.fold(entries, 0, Step) }; return Clean()`, root)
			fixture, err := source.New("grant-close.lo", text)
			if err != nil {
				t.Fatal(err)
			}
			graph, diagnostic := compiler.Check(fixture)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			program, err := eval.Program(graph)
			if err != nil {
				t.Fatal(err)
			}
			ctx, capture, err := execution.CaptureFailure(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, runErr := program.Run(ctx, files, execution.DefaultBudget(), device.DefaultConfig())
			if closes != 1 {
				t.Fatalf("retained directory closes %d, expected one before grant cleanup", closes)
			}
			closeErr := files.Close()
			if closeErr == nil || closes != 3 {
				t.Fatalf("grant cleanup: %v; closes=%d", closeErr, closes)
			}
			err = capture.Preserve(execution.PreserveCleanup(runErr, closeErr))
			var stderr bytes.Buffer
			got := result{status: runner.ReportRuntime(&stderr, runner.JsonFormat, err), stderr: stderr.String()}
			wantCode, wantCompleted := "runtime.host-failure", int64(3)
			if scenario == "prior-cancellation" {
				wantCode, wantCompleted = "runtime.cancelled", 1
			}
			if diagnostics(t, got.stderr).Diagnostics[0].Code != wantCode || !strings.Contains(got.stderr, "injected grant close failure") {
				t.Fatalf("grant close failure class: %+v", got)
			}
			progress := execution.FlowDetails(err)
			if len(progress) != 1 || progress[0].Field("completed_count").(value.Integer).Big().Int64() != wantCompleted {
				t.Fatalf("grant close lost cumulative prefix: %+v", got)
			}
		})
	}
}
