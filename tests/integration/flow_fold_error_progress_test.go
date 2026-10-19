package integration_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

const recoverableFoldStep = `flow Step(state: Int, item: Int) -> Int! { return Math.add(state, Math.div(10, item)) }; `

type foldErrorProgressValue struct {
	Kind     string         `json:"kind"`
	Input    *runtimeValue  `json:"input"`
	Progress []runtimeValue `json:"flow_progress"`
}

type expectedFoldProgress struct {
	total, completed, next int
	state                  string
}

func TestFlowFoldRecoverableErrorProgress(t *testing.T) {
	for _, test := range []struct {
		name, source string
		progress     []expectedFoldProgress
	}{
		{"second-item", recoverableFoldStep + `return Flow.fold(List.new(2, 0, 5), 0, Step)`, []expectedFoldProgress{{3, 1, 1, `"5"`}}},
		{"nested-order", recoverableFoldStep + `flow Outer(state: Int, item: Int) -> Int! { return Flow.fold(List.new(2, 0, 3), state, Step) }; return Flow.fold(List.new(9, 8), 5, Outer)`, []expectedFoldProgress{{2, 0, 0, `"5"`}, {3, 1, 1, `"10"`}}},
		{"equal-nested-records", recoverableFoldStep + `flow Outer(state: Int, item: Int) -> Int! { return Flow.fold(List.new(0), state, Step) }; return Flow.fold(List.new(0), 0, Outer)`, []expectedFoldProgress{{1, 0, 0, `"0"`}, {1, 0, 0, `"0"`}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "run", sourceFile(t, test.source), "--format", "json")
			if got.status != 1 || got.stdout != "" {
				t.Fatalf("expected recoverable fold failure: %+v", got)
			}
			var document struct {
				Diagnostics []struct {
					Error    json.RawMessage `json:"error"`
					Progress []runtimeValue  `json:"flow_progress"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Diagnostics) != 1 {
				t.Fatalf("missing error diagnostic: %s", got.stderr)
			}
			actual := decodeFoldErrorProgress(t, document.Diagnostics[0].Error)
			if actual.Kind != "Math.DivideByZero" {
				t.Fatalf("changed primary error: %+v", actual)
			}
			assertFoldErrorProgress(t, actual.Progress, test.progress)
			assertFoldErrorProgress(t, document.Diagnostics[0].Progress, test.progress)
			if test.name == "equal-nested-records" && !reflect.DeepEqual(foldProgressFields(t, actual.Progress[0]), foldProgressFields(t, actual.Progress[1])) {
				t.Fatal("nested equal-content fixture is not equal")
			}
		})
	}
}

func TestFlowFoldErrorProgressHandlers(t *testing.T) {
	t.Run("handler-inspects-and-preserves", func(t *testing.T) {
		source := recoverableFoldStep + `flow Keep(error: Error) -> Error { return Match.new(List.length(error.flow_progress), 1: error, _: Error.new("MissingProgress", "", Record.new())) }; flow Run() -> Error onError Keep { unused = Flow.fold(List.new(2, 0, 5), 0, Step); return Error.new("MissedFailure", "", Record.new()) }; return Run()`
		actual := returnedFoldErrorProgress(t, source)
		if actual.Kind != "Math.DivideByZero" {
			t.Fatalf("handler did not inspect original progress: %+v", actual)
		}
		assertFoldErrorProgress(t, actual.Progress, []expectedFoldProgress{{3, 1, 1, `"5"`}})
	})
	t.Run("fresh-error-clears-progress", func(t *testing.T) {
		source := recoverableFoldStep + `flow Replace(error: Error) -> Error { return Error.new("Replacement", "", Record.new(progress_count: List.length(error.flow_progress))) }; flow Run() -> Error onError Replace { unused = Flow.fold(List.new(2, 0), 0, Step); return Error.new("MissedFailure", "", Record.new()) }; return Run()`
		actual := returnedFoldErrorProgress(t, source)
		if actual.Kind != "Replacement" || len(actual.Progress) != 0 || actual.Input == nil {
			t.Fatalf("fresh error retained fold progress: %+v", actual)
		}
		valueJSON(t, foldProgressFields(t, *actual.Input)["progress_count"], `"1"`)
	})
	t.Run("handled-fold-does-not-contaminate-later-error", func(t *testing.T) {
		source := recoverableFoldStep + `flow Count(error: Error) -> Int { return List.length(error.flow_progress) }; flow Handled() -> Int onError Count { return Flow.fold(List.new(2, 0), 0, Step) }; flow Main() -> Int! { count = Handled(); return Math.div(count, 0) }; return Main()`
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 {
			t.Fatalf("expected later recoverable failure: %+v", got)
		}
		var document struct {
			Diagnostics []struct {
				Error    json.RawMessage `json:"error"`
				Progress json.RawMessage `json:"flow_progress"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
			t.Fatal(err)
		}
		if len(document.Diagnostics) != 1 {
			t.Fatalf("missing later error: %s", got.stderr)
		}
		if len(document.Diagnostics[0].Progress) != 0 {
			t.Fatalf("top-level diagnostic retained handled fold progress: %s", got.stderr)
		}
		actual := decodeFoldErrorProgress(t, document.Diagnostics[0].Error)
		if actual.Kind != "Math.DivideByZero" || len(actual.Progress) != 0 || actual.Input == nil {
			t.Fatalf("stale progress on unrelated error: %+v", actual)
		}
		valueJSON(t, foldProgressFields(t, *actual.Input)["left_value"], `"1"`)
	})
	t.Run("fresh-error-default-empty", func(t *testing.T) {
		valueJSON(t, runValue(t, `error = Error.new("Fresh", "", Record.new()); return List.length(error.flow_progress)`), `"0"`)
		actual := returnedFoldErrorProgress(t, `return Error.new("Fresh", "", Record.new())`)
		if actual.Kind != "Fresh" || len(actual.Progress) != 0 {
			t.Fatalf("fresh error progress: %+v", actual)
		}
	})
}

func returnedFoldErrorProgress(t *testing.T, source string) foldErrorProgressValue {
	t.Helper()
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("expected returned Error value: %+v", got)
	}
	var document struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &document); err != nil {
		t.Fatal(err)
	}
	return decodeFoldErrorProgress(t, document.Result)
}

func decodeFoldErrorProgress(t *testing.T, raw json.RawMessage) foldErrorProgressValue {
	t.Helper()
	var actual foldErrorProgressValue
	if err := json.Unmarshal(raw, &actual); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	_, published := fields["flow_progress"]
	if published != (len(actual.Progress) > 0) {
		t.Fatalf("flow_progress must be present only when nonempty: %s", raw)
	}
	return actual
}

func assertFoldErrorProgress(t *testing.T, actual []runtimeValue, expected []expectedFoldProgress) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("fold progress length %d, want %d: %+v", len(actual), len(expected), actual)
	}
	for index, want := range expected {
		fields := foldProgressFields(t, actual[index])
		valueJSON(t, fields["operation"], `"Flow.fold"`)
		valueJSON(t, fields["total_count"], fmt.Sprintf("%q", fmt.Sprint(want.total)))
		valueJSON(t, fields["completed_count"], fmt.Sprintf("%q", fmt.Sprint(want.completed)))
		valueJSON(t, fields["next_index"], fmt.Sprintf("%q", fmt.Sprint(want.next)))
		valueJSON(t, fields["state"], want.state)
	}
}

func foldProgressFields(t *testing.T, value runtimeValue) map[string]runtimeValue {
	t.Helper()
	var fields map[string]runtimeValue
	if err := json.Unmarshal(value.Value, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
