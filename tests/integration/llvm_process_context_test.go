package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLLVMProcessContextInjectedInputs(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmProcessContextInputScenarios(t))
}

func TestLLVMLinuxProcessContextInjectedInputs(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmProcessContextInputScenarios(t))
}

func llvmProcessContextInputScenarios(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	initial, err := json.Marshal(struct {
		Environment [][2]string `json:"environment"`
		Directory   string      `json:"directory"`
	}{[][2]string{{"LO_F024_INITIAL", "injected"}}, repository})
	if err != nil {
		t.Fatal(err)
	}
	encoded := processLiteral(t, string(initial))
	return []llvmRuntimeScenario{{
		name:    "injected-process-context",
		source:  `flow Failed(error: Error) -> Bool { return false }; flow Main() -> Bool onError Failed { initial = Process.Context.current(); text = Process.Context.environment_value(initial, "LO_F024_INITIAL"); return String.eq(text, "injected") }; return Main()`,
		harness: "testdata/native-runtime/process-context-input.c",
		harnessTransform: func(t *testing.T, source []byte) []byte {
			return []byte(strings.ReplaceAll(string(source), "@CONTEXT_JSON@", encoded))
		},
	}}
}
