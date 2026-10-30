package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const worklistIntegerChildren = `flow Children(item: Int) -> List<Int> { return Match.new(item, 1: List.new(11, 12), 2: List.new(21), _: List.new()) };`

func TestWorklistDynamicDiscovery(t *testing.T) {
	source := worklistIntegerChildren + `flow Step(state: String, item: Int) -> Record{state: String, items: List<Int>, stop: Bool} { return Record.new(state: String.concat(state, Int.to_string(item), ","), items: Children(item), stop: false) }; return Flow.worklist(List.new(1, 2), "", Step)`
	valueJSON(t, runValue(t, source), `"1,11,12,2,21,"`)
}

func TestWorklistEmptyAndEarlyStop(t *testing.T) {
	for _, test := range []struct{ name, source, expected string }{
		{"empty", `flow Empty() -> List<Int> { return List.new() }; flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, item), items: Empty(), stop: false) }; return Flow.worklist(Empty(), 7, Step)`, `"7"`},
		{"fallible-context", `flow Step(number: Int!, state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { captured = List.new(number); return Record.new(state: Math.add(state, List.length(captured)), items: List.new(), stop: false) }; return Flow.worklist(List.new(1), 0, 7, Step)`, `"1"`},
		{"stop-discards-new-and-pending", `flow Children(item: Int) -> List<Int> { return Match.new(item, 1: List.new(2, 3), _: List.new(4)) }; flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, item), items: Children(item), stop: Math.eq(item, 2)) }; return Flow.worklist(List.new(1), 0, Step)`, `"3"`},
	} {
		t.Run(test.name, func(t *testing.T) { valueJSON(t, runValue(t, test.source), test.expected) })
	}
}

const worklistGrowableQueueSource = `flow Child(index: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: Math.add(index, 1), next_state: Math.add(index, 1)) }; flow Children(item: Int) -> List<Int> { return Match.new(item, 0: List.unfold(0, 12000, Child), _: List.new()) }; flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, 1), items: Children(item), stop: false) }; return Flow.worklist(List.new(0), 0, Step)`

func TestWorklistGrowableQueue(t *testing.T) {
	source := worklistGrowableQueueSource
	valueJSON(t, runValue(t, source), `"12001"`)
}

const worklistPendingTensorSource = `flow Children(item: Tensor<FP32>) -> List<Tensor<FP32>>! { marker = Tensor.to_float(item); return Match.new(Float.eq(marker, 0.0), true: List.new(Tensor.new(List.new(1.0), List.new()), Tensor.new(List.new(2.0), List.new())), _: List.new()) }; flow Step(context: Tensor<FP32>, state: Float, item: Tensor<FP32>) -> Record{state: Float, items: List<Tensor<FP32>>, stop: Bool}! { next = Float.add(state, Float.add(Tensor.to_float(context), Tensor.to_float(item))); return Record.new(state: next, items: Children(item), stop: false) }; flow Run() -> Float! { return Flow.worklist(List.new(Tensor.new(List.new(0.0), List.new())), 0.0, Tensor.new(List.new(10.0), List.new()), Step) }; return Run()`

func TestWorklistPendingTensorLifetime(t *testing.T) {
	source := worklistPendingTensorSource
	valueJSON(t, successfulValue(t, runValue(t, source)), `"33"`)
}

func TestWorklistOrderedActivation(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "events")
	source := worklistIntegerChildren + fmt.Sprintf(`flow Step(path: File.Path, state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool}! { target = File.Append.new(path, UTF8.Bytes.new(String.concat(Int.to_string(item), ","))); commit target; return Record.new(state: Math.add(state, 1), items: Children(item), stop: false) }; flow Run() -> Int! { return Flow.worklist(List.new(1, 2), 0, File.Path.new(%q), Step) }; return Run()`, path)
	output := fileOutput(t, run(t, "run", sourceFile(t, source), "--write-root", root, "--format", "json"))
	valueJSON(t, output, `"5"`)
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "1,11,12,2,21," {
		t.Fatalf("worklist effect order: %q %v", contents, err)
	}
}

func TestWorklistStaticContracts(t *testing.T) {
	for _, test := range []struct{ name, source, code string }{
		{"missing-stop", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>} { return Record.new(state: state, items: List.new()) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"wrong-state", `flow Step(state: Int, item: Int) -> Record{state: String, items: List<Int>, stop: Bool} { return Record.new(state: "bad", items: List.new(), stop: false) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"wrong-items", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<String>, stop: Bool} { return Record.new(state: state, items: List.new(), stop: false) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"wrong-stop", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Int} { return Record.new(state: state, items: List.new(), stop: 0) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"extra-field", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool, extra: Int} { return Record.new(state: state, items: List.new(), stop: false, extra: 1) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"items-require-list", `flow Step(state: Int, item: Scalar) -> Record{state: Int, items: List<Scalar>, stop: Bool} { return Record.new(state: state, items: List.new(), stop: true) }; return Flow.worklist("abc", 0, Step)`, "check.type-mismatch"},
		{"fallible-state-total-callback", `flow Step(state: Int!, item: Int) -> Record{state: Int!, items: List<Int>, stop: Bool} { return Record.new(state: state, items: List.new(), stop: true) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"fallible-state-fallible-callback", `flow Step(state: Int!, item: Int) -> Record{state: Int!, items: List<Int>, stop: Bool}! { return Record.new(state: state, items: List.new(), stop: true) }; return Flow.worklist(List.new(1), 0, Step)`, "check.type-mismatch"},
		{"list-stays-pure", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool}! { target = File.Append.new(File.Path.new("unused"), Bytes.new("x")); commit target; return Record.new(state: state, items: List.new(), stop: true) }; flow Nested(state: Int, item: Int) -> Int! { return Flow.worklist(List.new(item), state, Step) }; flow Run() -> Int! { return List.fold(List.new(1), 0, Nested) }; return Run()`, "check.effectful-callback"},
		{"match-stays-pure", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool}! { target = File.Append.new(File.Path.new("unused"), Bytes.new("x")); commit target; return Record.new(state: state, items: List.new(), stop: true) }; flow Run() -> Int! { return Match.new(true, true: Flow.worklist(List.new(1), 0, Step), _: 0) }; return Run()`, "check.effectful-match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, test.source), "--format", "json")
			if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != test.code {
				t.Fatalf("worklist static contract: %+v, want %s", got, test.code)
			}
		})
	}
}

func TestWorklistInspection(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "untouched")
	source := fmt.Sprintf(`flow Step(path: File.Path, state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool}! { target = File.Append.new(path, Bytes.new("x")); commit target; return Record.new(state: state, items: List.new(), stop: true) }; flow Run() -> Int! { return Flow.worklist(List.new(1), 0, File.Path.new(%q), Step) }; return Run()`, path)
	got := run(t, "inspect", sourceFile(t, source), "--format", "json")
	if got.status != 0 || !strings.Contains(got.stdout, `"commits":true`) || !strings.Contains(got.stdout, "File.append") {
		t.Fatalf("worklist effect inspection: %+v", got)
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("inspection activated worklist: %v", err)
	}
}

func TestWorklistFailureProgress(t *testing.T) {
	for _, test := range []struct {
		name, source string
		args         []string
		status       int
		completed    string
		pending      string
		state        string
	}{
		{"recoverable", `flow Children(item: Int) -> List<Int> { return Match.new(item, 2: List.new(0, 5), _: List.new()) }; flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool}! { next = Math.add(state, Math.div(10, item)); return Record.new(state: next, items: Children(item), stop: false) }; return Flow.worklist(List.new(2, 3), 0, Step)`, nil, 1, `"1"`, `"3"`, `"5"`},
		{"terminal-step-limit", `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, 1), items: List.new(item), stop: false) }; return Flow.worklist(List.new(1), 0, Step)`, []string{"--max-steps", "150"}, 2, "", `"1"`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"run", sourceFile(t, test.source), "--format", "json"}, test.args...)
			got := run(t, args...)
			if got.status != test.status {
				t.Fatalf("worklist failure: %+v", got)
			}
			var document struct {
				Diagnostics []struct {
					Code     string         `json:"code"`
					Progress []runtimeValue `json:"flow_progress"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Diagnostics) != 1 || len(document.Diagnostics[0].Progress) != 1 {
				t.Fatalf("missing worklist failure progress: %+v", got)
			}
			fields := foldProgressFields(t, document.Diagnostics[0].Progress[0])
			valueJSON(t, fields["operation"], `"Flow.worklist"`)
			valueJSON(t, fields["pending_count"], test.pending)
			if test.completed != "" {
				valueJSON(t, fields["completed_count"], test.completed)
				valueJSON(t, fields["state"], test.state)
			} else {
				if document.Diagnostics[0].Code != "runtime.limit" || string(fields["completed_count"].Value) == `"0"` || string(fields["completed_count"].Value) != string(fields["state"].Value) {
					t.Fatalf("last completed state was not preserved: %+v", got)
				}
			}
		})
	}
}

func TestWorklistSuccessorAdmissionProgress(t *testing.T) {
	root := fileFixtureRoot(t)
	first := fileFixture(t, root, "first", "x")
	next := fileFixture(t, root, "next", strings.Repeat("x", 8<<20))
	program := fmt.Sprintf(`flow Step(children: List<File.Path>, state: Int, item: File.Path) -> Record{state: Int, items: List<File.Path>, stop: Bool}! { bytes = File.read(item); return Record.new(state: Math.add(state, 1), items: children, stop: false) }; flow Run() -> Int! { return Flow.worklist(List.new(File.Path.new(%q)), 0, List.new(File.Path.new(%q)), Step) }; return Run()`, first, next)
	got := run(t, "run", sourceFile(t, program), "--read-root", root, "--max-live-bytes", "4194304", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("successor I/O admission escaped its policy: %+v", got)
	}
	var document struct {
		Diagnostics []struct {
			Progress []runtimeValue `json:"flow_progress"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Diagnostics) != 1 || len(document.Diagnostics[0].Progress) != 1 {
		t.Fatalf("successor admission discarded confirmed progress: %+v", got)
	}
	fields := foldProgressFields(t, document.Diagnostics[0].Progress[0])
	valueJSON(t, fields["completed_count"], `"1"`)
	valueJSON(t, fields["state"], `"1"`)
	valueJSON(t, fields["pending_count"], `"1"`)
}

func TestWorklistCancellationProgress(t *testing.T) {
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
	program := fmt.Sprintf(`flow Step(state: Int, item: File.Entry) -> Record{state: Int, items: List<File.Entry>, stop: Bool}! { target = File.Remove.new(List.new(item)); commit target; return Record.new(state: Math.add(state, 1), items: List.new(), stop: false) }; flow Run() -> Int! { return Flow.worklist(File.list(File.Path.new(%q)), 0, Step) }; return Run()`, root)
	_, err, got := injectedFileProgram(t, ctx, root, program, filesystem)
	if err == nil || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.cancelled" {
		t.Fatalf("worklist did not preserve cancellation: %+v", got)
	}
	var document struct {
		Diagnostics []struct {
			Progress []runtimeValue `json:"flow_progress"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Diagnostics) != 1 || len(document.Diagnostics[0].Progress) != 1 {
		t.Fatalf("cancellation discarded confirmed worklist state: %+v", got)
	}
	fields := foldProgressFields(t, document.Diagnostics[0].Progress[0])
	valueJSON(t, fields["completed_count"], `"2"`)
	valueJSON(t, fields["state"], `"2"`)
	valueJSON(t, fields["pending_count"], `"2"`)
	if len(filesystem.removals) != 3 {
		t.Fatalf("cancelled worklist visited its suffix: %v", filesystem.removals)
	}
}
