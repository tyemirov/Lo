package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLLVMF034TreeFailureEvidence(t *testing.T) {
	runLLVMRuntimeScenarios(t, f034FailureEvidenceScenarios())
}

func TestLLVMLinuxF034TreeFailureEvidence(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f034FailureEvidenceScenarios())
}

func f034FailureEvidenceScenarios() []llvmRuntimeScenario {
	var scenarios []llvmRuntimeScenario
	for number, fixture := range []struct{ name, kind string }{
		{"handled-prefix-message", ""},
		{"preflight-leaf-io-progress", "File.RemoveFailed"},
		{"preflight-leaf-absence-progress", "File.EntryChanged"},
		{"nested-cleanup-errors", "File.RemoveFailed"},
		{"changed-grant-keeps-cleanup-errors", ""},
		{"protection-lookup-io-progress", "File.RemoveFailed"},
		{"protection-lookup-absence-progress", "File.EntryChanged"},
	} {
		source := f034RemoveSource("parent", "", "")
		if number == 0 {
			body := f034Program("parent", "String", "", `target = File.Tree.Remove.checked(tree, Eligible); receipt = commit target; return "unexpected-success"`)
			body = strings.Replace(body, "flow Main() -> String!", "flow Main() -> String onError Recover", 1)
			source = f034Predicates + `flow Recover(error: Error) -> String { return error.message }; ` + body
		}
		scenario := llvmRuntimeScenario{name: fixture.name, source: source, number: number, harness: "testdata/native-runtime/f034-tree-failure-evidence.c", transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			text := strings.Replace(string(module), "define void @lo_entry(", "define void @lo_f034_evidence_entry(", 1)
			if !strings.Contains(text, "define void @lo_flow_1(") {
				t.Fatal("missing eligibility callback definition")
			}
			text = strings.Replace(text, "define void @lo_flow_1(", "define void @lo_f034_evidence_guard(", 1)
			text += "\ndeclare void @lo_flow_1(ptr, ptr, i64, i64, ptr)\n"
			text = strings.ReplaceAll(text, "@lo_commit(", "@lo_f034_evidence_commit(")
			return []byte(text)
		}, verify: func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("tree failure evidence %s: %+v", fixture.name, got)
			}
			if number == 0 {
				value := fileOutput(t, got)
				var message string
				if value.Type != "String" || json.Unmarshal(value.Value, &message) != nil {
					t.Fatalf("handled tree error did not return a String: %s", got.stdout)
				}
				const summary = `tree removal: completed 1 of 7 entries, 3 logical bytes; stage remove; entry "a-dir/b"`
				if !strings.Contains(message, summary) || !strings.Contains(message, "input/output error") {
					t.Fatalf("handled tree failure omitted confirmed progress or native cause: %q", message)
				}
				return
			}
			index, relative := 2, "a-dir/a"
			if number == 3 || number == 4 {
				index, relative = 0, "."
			}
			if number == 3 || number == 4 {
				var report struct {
					Diagnostics []struct {
						Code    string
						Error   *runtimeValue
						Cleanup []string `json:"cleanup_errors"`
					}
				}
				if err := json.Unmarshal([]byte(got.stdout), &report); err != nil || len(report.Diagnostics) != 1 {
					t.Fatalf("missing compound failure: %s", got.stdout)
				}
				diagnostic := report.Diagnostics[0]
				descriptions := diagnostic.Cleanup
				if number == 4 {
					if diagnostic.Code != "runtime.capability-denied" || diagnostic.Error != nil {
						t.Fatalf("changed removal grant must remain terminal: %s", got.stdout)
					}
				} else {
					if diagnostic.Error == nil || diagnostic.Error.Input == nil {
						t.Fatalf("missing recoverable compound failure: %s", got.stdout)
					}
					cleanup := recordField(t, *diagnostic.Error.Input, "cleanup_errors")
					var failures []runtimeValue
					if err := json.Unmarshal(cleanup.Value, &failures); err != nil {
						t.Fatalf("invalid cleanup list: %s", got.stdout)
					}
					descriptions = nil
					for _, failure := range failures {
						var message string
						if failure.Type != "String" || json.Unmarshal(failure.Value, &message) != nil {
							t.Fatalf("invalid cleanup evidence: %s", cleanup.Value)
						}
						descriptions = append(descriptions, message)
					}
				}
				if len(descriptions) != 2 || !strings.Contains(descriptions[0], "bad file descriptor") || !strings.Contains(descriptions[1], "no space left on device") {
					t.Fatalf("cleanup cause order changed: %v", descriptions)
				}
			}
			fields := f034Progress(t, got.stdout, 0, 7, index, relative, "preflight", fixture.kind)
			valueJSON(t, fields["completed_bytes"], `"0"`)
		}}
		scenarios = append(scenarios, scenario)
	}
	return scenarios
}
