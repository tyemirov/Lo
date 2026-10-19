package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestFlowFoldErrorProgressValueBudget(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "state", strings.Repeat("x", 8<<20))
	program := fmt.Sprintf(`flow Inner(state: Bytes, item: Int) -> Bytes! {
		unused = Math.div(1, 0); return state
	}; flow Outer(state: Bytes, item: Int) -> Bytes! {
		return Flow.fold(List.new(1), state, Inner)
	}; flow Run() -> Bytes! {
		state = File.read(File.Path.new(%q)); return Flow.fold(List.new(1, 2), state, Outer)
	}; return Run()`, path)
	got := run(t, "run", sourceFile(t, program), "--read-root", root, "--format", "json")
	if got.status != 2 {
		t.Fatalf("expected host value limit, status %d", got.status)
	}
	var document struct {
		Diagnostics []struct {
			Code     string         `json:"code"`
			Message  string         `json:"message"`
			Progress []runtimeValue `json:"flow_progress"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Diagnostics) != 1 {
		t.Fatalf("diagnostic count %d", len(document.Diagnostics))
	}
	diagnostic := document.Diagnostics[0]
	if diagnostic.Code != "runtime.limit" || !strings.Contains(diagnostic.Message, "expanded value bytes") {
		t.Fatalf("expected returned-error value limit, got %s: %s", diagnostic.Code, diagnostic.Message)
	}
	if len(diagnostic.Progress) != 2 {
		t.Fatalf("value-limit failure lost nested fold progress: got %d records, want 2", len(diagnostic.Progress))
	}
	for index, total := range []string{`"2"`, `"1"`} {
		fields := foldProgressFields(t, diagnostic.Progress[index])
		valueJSON(t, fields["total_count"], total)
		valueJSON(t, fields["completed_count"], `"0"`)
		valueJSON(t, fields["next_index"], `"0"`)
		if fields["state"].Type != "Bytes" {
			t.Fatalf("lost accumulator type: %s", fields["state"].Type)
		}
	}
}
