package integration_test

import "testing"

func llvmFileProbeOutputScenarios() []llvmRuntimeScenario {
	const source = `flow Handle(e:Error)->Int{return 99};flow Main()->Int onError Handle {paths=File.open_paths(File.Path.new("."));later=File.Write.new(File.Path.new("later"),Bytes.new("wrong"));commit later;return List.length(paths)};return Main()`
	var scenarios []llvmRuntimeScenario
	for index, name := range []string{"stdout-exact-boundary", "stdout-overflow", "stderr-exact-boundary", "stderr-overflow", "independent-stream-boundaries"} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: source, number: index, harness: "testdata/native-runtime/files-probe-output.c"})
	}
	return scenarios
}

func TestLLVMFileProbeOutputTransportLimit(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmFileProbeOutputScenarios())
}

func TestLLVMLinuxFileProbeOutputTransportLimit(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmFileProbeOutputScenarios())
}
