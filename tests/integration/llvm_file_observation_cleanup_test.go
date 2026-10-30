package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLLVMFileObservationCallbackCleanupProgress(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for _, operation := range []string{"fold", "worklist"} {
		const state = `Record{count:Int,directories:List<File.Directory>}`
		const initial = `Record.new(count:0,directories:List.new())`
		const paths = `List.new(File.Path.new("first"),File.Path.new("second"),File.Path.new("third"))`
		text := fmt.Sprintf(`flow Step(state:%s,path:File.Path)->%s!{directory=File.directory(path);return Record.new(count:Math.add(state.count,1),directories:List.new(directory))};return Flow.fold(%s,%s,Step)`, state, state, paths, initial)
		if operation == "worklist" {
			text = fmt.Sprintf(`flow Step(state:%s,path:File.Path)->Record{state:%s,items:List<File.Path>,stop:Bool}!{directory=File.directory(path);return Record.new(state:Record.new(count:Math.add(state.count,1),directories:List.new(directory)),items:List.new(),stop:false)};return Flow.worklist(%s,%s,Step)`, state, state, paths, initial)
		}
		scenario := llvmRuntimeScenario{name: operation, source: text, harness: "testdata/native-runtime/files-observation-cleanup.c", transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			text := strings.ReplaceAll(string(module), "call void @lo_op_File_2Edirectory(", "call void @lo_observation_directory_bridge(")
			return []byte(text + "\ndeclare void @lo_observation_directory_bridge(ptr,ptr,i64,i64,ptr)\n")
		}}
		scenario.verify = func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "runtime.host-failure") {
				t.Fatalf("callback cleanup: %+v", got)
			}
			var document struct {
				Diagnostics []struct {
					Progress []runtimeValue `json:"flow_progress"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Diagnostics) != 1 || len(document.Diagnostics[0].Progress) != 1 {
				t.Fatalf("missing completion progress: %+v", got)
			}
			fields := foldProgressFields(t, document.Diagnostics[0].Progress[0])
			valueJSON(t, fields["completed_count"], `"2"`)
			valueJSON(t, foldProgressFields(t, fields["state"])["count"], `"2"`)
			if operation == "worklist" {
				valueJSON(t, fields["pending_count"], `"1"`)
			} else {
				valueJSON(t, fields["next_index"], `"2"`)
				valueJSON(t, fields["total_count"], `"3"`)
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
