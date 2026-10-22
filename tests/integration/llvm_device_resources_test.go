package integration_test

import "testing"

func llvmDeviceResourceScenarios() []llvmRuntimeScenario {
	source := `flow Main()->Tensor<FP32>! {a=Tensor.new(List.new(1.0,2.0,3.0),List.new(3));b=Tensor.to(a,Device.new("cuda-sim:0"));c=Tensor.to(b,Device.new("cpu"));next=File.Write.new(File.Path.new("later"),Bytes.new("ran"));commit next;return c};return Main()`
	retry := `flow Stop(error:Error)->String{return error.kind};flow Retry(error:Error)->String onError Stop {a=Tensor.new(List.new(1.0),List.new());b=Tensor.to(a,Device.new("cuda-sim:0"));return "unexpected"};flow Main()->String onError Retry {a=Tensor.new(List.new(1.0),List.new());b=Tensor.to(a,Device.new("cuda-sim:0"));return "unexpected"};return Main()`
	names := []string{"reserved-transfer-receipts", "open-failure", "allocation-failure", "upload-failure", "download-failure", "release-cleanup", "backend-cleanup", "cancel-during-upload", "reserved-budget", "undersized-reservation", "sticky-lost", "lost-and-cancellation"}
	var scenarios []llvmRuntimeScenario
	for number, name := range names {
		text := source
		if number == 10 {
			text = retry
		}
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, number: number, source: text, captureOutput: true, harness: "testdata/native-runtime/device-resources.c"})
	}
	return scenarios
}

func TestLLVMDeviceResources(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmDeviceResourceScenarios())
}

func TestLLVMLinuxDeviceResources(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmDeviceResourceScenarios())
}
