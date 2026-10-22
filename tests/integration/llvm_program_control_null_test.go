package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestLLVMProgramControlValueLifetime(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for index, name := range []string{"retained-across-contexts", "late-admission-rejection"} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: `return Math.add(1,2)`, number: index, harness: "testdata/native-qualification/program-control-null.c", transform: func(t *testing.T, module []byte) []byte {
			if index == 1 {
				module = rewriteLLVMMetadata(t, module, func(metadata map[string]json.RawMessage) { metadata["has_result"] = json.RawMessage(`null`) })
			}
			var metadata struct {
				Locations []struct {
					ID     uint64
					Action string
				}
			}
			if err := json.Unmarshal(locationMetadataBytes(t, module), &metadata); err != nil {
				t.Fatal(err)
			}
			for _, site := range metadata.Locations {
				if site.Action == "statement_step" {
					return append(module, []byte(fmt.Sprintf("\n@lo_test_step_site = constant i64 %d\n", site.ID))...)
				}
			}
			t.Fatal("statement step metadata missing")
			return nil
		}})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
