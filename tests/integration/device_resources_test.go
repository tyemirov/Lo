package integration_test

import (
	"strings"
	"testing"
)

func TestDeviceParameterLocations(t *testing.T) {
	text := `flow Main()->Parameters<Record{a:Tensor<FP32>,b:Tensor<FP32>}>! {
 a=Tensor.new(List.new(1.0),List.new())
 b=Tensor.to(a,Device.new("cuda-sim:0"))
 return Parameters.new(Record.new(a:a,b:b))
 };return Main()`
	got := runProgram(t, sourceFile(t, text), "run", "--allow-device", "cuda-sim:0", "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "Gradient.SchemaMismatch") {
		t.Fatalf("mixed parameter locations: %+v", got)
	}
}
