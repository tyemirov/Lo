package integration_test

import (
	"strings"
	"testing"
)

func TestLLVMProbeAdmissionBinding(t *testing.T) {
	runLLVMRuntimeScenarios(t, probeAdmissionBindingScenarios())
}

func TestLLVMLinuxProbeAdmissionBinding(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", probeAdmissionBindingScenarios())
}

func probeAdmissionBindingScenarios() []llvmRuntimeScenario {
	const source = `flow Failed(error:Error)->String{return error.kind};flow Main()->String onError Failed{paths=File.open_paths(File.Path.new("grant"));return Int.to_string(List.length(paths))};return Main()`
	var scenarios []llvmRuntimeScenario
	for number, name := range []string{"bound-executable-after-PATH-change", "absent-executable-after-PATH-change", "retained-root-after-replacement"} {
		payload := `"1"`
		if number == 1 {
			payload = `"File.OpenFilesFailed"`
		}
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: name, source: source, number: number,
			harness: "testdata/native-runtime/probe-admission-binding.c",
			verify:  nativeValueCheck("String", payload),
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(module), "call void @lo_op_File_2Eopen_5Fpaths(", "call void @lo_probe_admission(")
				return []byte(text + "\ndeclare void @lo_probe_admission(ptr,ptr,i64,i64,ptr)\n")
			},
		})
	}
	return scenarios
}
