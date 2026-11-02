package integration_test

import (
	"runtime"
	"testing"
)

func TestLLVMProgramEntryReplacementBeforeCapture(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("checks the Darwin running-image witness")
	}
	source := `flow Main() -> File.Path! { target = Lock.File.new(Program.path()); lease = commit target; return Program.path() }; return Main()`
	runLLVMRuntimeTargetScenarios(t, "darwin/arm64", []llvmRuntimeScenario{{
		name:          "replaced-before-cli-capture",
		source:        source,
		harness:       "testdata/native-runtime/program-entry-replacement.c",
		captureOutput: true,
	}})
}
