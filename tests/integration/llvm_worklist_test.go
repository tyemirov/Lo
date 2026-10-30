package integration_test

import "testing"

func TestLLVMResourcePolicyOptions(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{{
		name: "explicit-host-policy", source: `return List.new(1, 2)`,
		harness: "testdata/native-runtime/resource-policy.c",
	}})
}
