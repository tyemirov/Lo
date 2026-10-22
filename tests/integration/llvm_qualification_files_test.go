package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func qualificationFileScenario(name, source string, number int) llvmRuntimeScenario {
	return llvmRuntimeScenario{name: name, source: source, number: number, harness: "testdata/native-qualification/file-failures.c", transform: func(t *testing.T, module []byte) []byte {
		t.Helper()
		text := strings.Replace(string(module), "define void @lo_entry(", "define void @lo_qualified_entry(", 1)
		text = strings.ReplaceAll(text, "call void @lo_commit(", "call void @lo_qualified_commit(")
		return []byte(text + "\ndeclare void @lo_qualified_commit(ptr,ptr,i64,i64,ptr)\n")
	}}
}

func TestLLVMFileDiscoveryFailures(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for number, name := range []string{"invalid-name", "entry-limit", "missing-child"} {
		scenario := qualificationFileScenario(name, `return File.list(File.Path.new("grant"))`, number)
		scenario.verify = func(t *testing.T, got result) {
			want := []string{"File.InvalidName", "runtime.limit", "File.ListFailed"}[number]
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, want) {
				t.Fatalf("discovery %s: %+v", name, got)
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMFileRemovalFailures(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for index, fixture := range []struct {
		name, kind, stage string
		completed         int
	}{{"preflight-change", "File.EntryChanged", "preflight", 0}, {"per-entry-change", "File.EntryChanged", "remove", 1}, {"remove-failure", "File.RemoveFailed", "remove", 2}, {"cancel-after-remove", "", "remove", 1}, {"cleanup-failure", "", "finalize", 4}, {"failure-and-cleanup", "File.RemoveFailed", "remove", 2}} {
		text := `flow Clean()->Int!{entries=File.list(File.Path.new("grant"));removal=File.Remove.new(entries);commit removal;return 7};return Clean()`
		scenario := qualificationFileScenario(fixture.name, text, index+3)
		scenario.verify = func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("removal fixture: %+v", got)
			}
			progressFields(t, result{stderr: got.stdout}, fixture.completed, 4, fixture.stage, fixture.kind)
			if fixture.name == "cancel-after-remove" && !strings.Contains(got.stdout, "runtime.cancelled") {
				t.Fatal("cancellation lost host class")
			}
			if strings.Contains(fixture.name, "cleanup") && !strings.Contains(got.stdout, "cleanup") {
				t.Fatal("cleanup evidence missing")
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMNestedFoldCancellationProgress(t *testing.T) {
	text := `flow Step(state:Int,entry:File.Entry)->Int!{target=File.Remove.new(List.new(entry));commit target;return Math.add(state,1)};flow Outer(entries:List<File.Entry>,state:Int,item:Int)->Int!{return Flow.fold(entries,state,Step)};flow Clean()->Int!{entries=File.list(File.Path.new("grant"));return Flow.fold(List.new(1),0,entries,Outer)};return Clean()`
	scenario := qualificationFileScenario("third-removal", text, 9)
	scenario.verify = func(t *testing.T, got result) {
		if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "runtime.cancelled") {
			t.Fatalf("nested cancellation: %+v", got)
		}
		var report struct {
			Diagnostics []struct {
				Progress []runtimeValue `json:"flow_progress"`
			}
		}
		if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Diagnostics) != 1 || len(report.Diagnostics[0].Progress) != 2 {
			t.Fatalf("missing nested progress: %s", got.stdout)
		}
		for index, expected := range []string{`"0"`, `"2"`} {
			var fields map[string]runtimeValue
			if err := json.Unmarshal(report.Diagnostics[0].Progress[index].Value, &fields); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"completed_count", "next_index", "state"} {
				valueJSON(t, fields[field], expected)
			}
		}
		progressFields(t, result{stderr: got.stdout}, 1, 1, "remove", "")
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{scenario})
}

func TestLLVMReceiptSessionCleanupDiagnostic(t *testing.T) {
	scenario := qualificationFileScenario("completed-write", `flow Write()->File.WriteReceipt!{target=File.Write.new(File.Path.new("grant/written"),Bytes.new("abc"));receipt=commit target;return receipt};return Write()`, 10)
	scenario.verify = func(t *testing.T, got result) {
		if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "runtime.host-failure") {
			t.Fatalf("session cleanup: %+v", got)
		}
		diagnostic := receiptHostDiagnostic(t, result{stderr: got.stdout})
		if diagnostic.LastCompleted == nil || diagnostic.LastCompleted.Operation != "File.write" || diagnostic.LastCompleted.Receipt.Type != "File.WriteReceipt" {
			t.Fatalf("missing completed activation: %s", got.stdout)
		}
		var span struct {
			File       string
			Start, End struct{ Offset int }
		}
		if err := json.Unmarshal(diagnostic.LastCompleted.Span, &span); err != nil {
			t.Fatal(err)
		}
		if span.File == "" || span.Start.Offset != 104 || span.End.Offset != 125 {
			t.Fatalf("completed activation location: %s", diagnostic.LastCompleted.Span)
		}
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{scenario})
}

func TestLLVMFileLateFailureProgress(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for number, name := range []string{"steps", "value"} {
		tail := `steps=List.unfold(0,1000,Count);return 0`
		if name == "value" {
			var body strings.Builder
			fmt.Fprintf(&body, "text0=%q;", strings.Repeat("x", 1024))
			for index := 1; index < 16; index++ {
				fmt.Fprintf(&body, "text%d=String.concat(text%d,text%d);", index, index-1, index-1)
			}
			tail = body.String() + "return String.length(text15)"
		}
		text := `flow Count(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.add(state,1))};flow Clean()->Int!{entries=File.list(File.Path.new("grant"));removal=File.Remove.new(entries);commit removal;` + tail + `};return Clean()`
		scenario := qualificationFileScenario(name, text, 11+number)
		scenario.verify = func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "runtime.limit") {
				t.Fatalf("late failure: %+v", got)
			}
			progressFields(t, result{stderr: got.stdout}, 1, 1, "finalize", "")
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
