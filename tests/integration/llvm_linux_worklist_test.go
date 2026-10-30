package integration_test

import (
	"encoding/json"
	"testing"
)

func TestLLVMLinuxWorklist(t *testing.T) {
	for _, fixture := range []struct{ name, source, expected string }{
		{"growable-queue", worklistGrowableQueueSource, `"12001"`},
		{"pending-tensors", worklistPendingTensorSource, `"33"`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			artifact := linuxLLVMArtifact(t, fixture.source)
			got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
			valueJSON(t, fileOutput(t, got), fixture.expected)
		})
	}
	t.Run("confirmed-failure-progress", func(t *testing.T) {
		artifact := linuxLLVMArtifact(t, `flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, 1), items: List.new(item), stop: false) }; return Flow.worklist(List.new(1), 0, Step)`)
		for _, quota := range []struct{ name, option, amount string }{
			{"steps", "--max-steps", "150"},
			{"data", "--max-data-bytes", "1000"},
		} {
			t.Run(quota.name, func(t *testing.T) {
				got := runLinuxLLVMExecutable(t, artifact, "run", quota.option, quota.amount, "--format", "json")
				if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
					t.Fatalf("Linux worklist quota: %+v", got)
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
					t.Fatalf("Linux worklist lost progress: %+v", got)
				}
				fields := foldProgressFields(t, document.Diagnostics[0].Progress[0])
				valueJSON(t, fields["operation"], `"Flow.worklist"`)
				valueJSON(t, fields["pending_count"], `"1"`)
				if string(fields["completed_count"].Value) == `"0"` || string(fields["completed_count"].Value) != string(fields["state"].Value) {
					t.Fatalf("Linux worklist lost last completed state: %+v", got)
				}
			})
		}
	})
}
