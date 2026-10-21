package integration_test

import "testing"

func TestLLVMDeviceHTTPRequestLifetime(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmDeviceHTTPRequestScenarios())
}
func TestLLVMLinuxDeviceHTTPRequestLifetime(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmDeviceHTTPRequestScenarios())
}
func llvmDeviceHTTPRequestScenarios() []llvmRuntimeScenario {
	return []llvmRuntimeScenario{{name: "1000-device-requests", source: `flow Handle(r:HTTP.Request)->HTTP.Response! {
 source=Tensor.new(List.new(-0.0),List.new())
 remote=Tensor.to(source,Device.new("cuda-sim:0"))
 host=Tensor.to(remote,Device.new("cpu"))
 scalar=Tensor.to_float(host)
 return HTTP.Response.Text.new(200,Float.to_string(scalar))
 };flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)};s=HTTP.Server.new(34445,Handle,OnError);commit s`, harness: "testdata/native-runtime/device-http-lifetime.c"}}
}
