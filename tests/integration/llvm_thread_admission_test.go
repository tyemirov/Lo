package integration_test

import (
	"fmt"
	"testing"
)

func TestLLVMThreadAdmission(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for index, name := range []string{"helper-foreign-thread", "operation-foreign-thread", "helper-prefaulted-thread", "operation-prefaulted-thread"} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: `return Math.add(1,2)`, number: index, harness: "testdata/native-qualification/thread-admission.c", transform: func(t *testing.T, module []byte) []byte {
			locations := metadataLocations(t, locationMetadataBytes(t, module))
			for _, site := range locations {
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
