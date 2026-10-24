package integration_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestLLVMLiteralSequenceCheckedStatus(t *testing.T) {
	scenario := literalSequenceScenario([]string{"0", "1", "256"}, 0)
	originalTransform := scenario.transform
	scenario.name = "trusted-ABI-checked-status"
	scenario.harness = "testdata/native-qualification/literal-sequence-status.c"
	scenario.expectedStdout = "literal-sequence-status-ok\n"
	scenario.transform = func(t *testing.T, module []byte) []byte {
		t.Helper()
		module = originalTransform(t, module)
		text := string(module)
		invalidCalls := regexp.MustCompile(`call void @lo_invalid_status\(ptr %context, ptr null, i64 0, i64 ([0-9]+), ptr %r[0-9]+\)`).FindAllStringSubmatch(text, -1)
		if len(invalidCalls) != 2 {
			t.Fatalf("expected entry and shared body invalid sites, got %d", len(invalidCalls))
		}
		// Keep the original shared invalid-site identity; wrappers observe it at runtime.
		text = strings.ReplaceAll(text, "call void @lo_invalid_status(", "call void @lo_literal_status_invalid(")
		text = strings.ReplaceAll(text, "call void @lo_value_retain(", "call void @lo_literal_status_retain(")
		text += fmt.Sprintf("\n@lo_literal_status_invalid_site = constant i64 %s\ndeclare void @lo_literal_status_invalid(ptr,ptr,i64,i64,ptr)\ndeclare void @lo_literal_status_retain(ptr)\n", invalidCalls[1][1])
		return []byte(text)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{scenario})
}
