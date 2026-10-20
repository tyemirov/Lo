package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSemanticRuntimeRejections(t *testing.T) {
	const scalar = `x=Tensor.new(List.new(1.0),List.new()); `
	const simulated = scalar + `remote=Tensor.to(x,Device.new("cuda-sim:0")); `
	const parameters = scalar + `p=Parameters.new(Record.new(x:x)); `
	const simulatedParameters = simulated + `p=Parameters.new(Record.new(x:remote)); `
	const dynamicShape = `flow Dimension(n:Int)->Int{return n}
flow Subtract(a:Tensor<FP32>,b:Tensor<FP32>)->Tensor<FP32>!{return Tensor.sub(a,b)}
flow Matmul(a:Tensor<FP32>,b:Tensor<FP32>)->Tensor<FP32>!{return Tensor.matmul(a,b)}
`
	for _, fixture := range []struct{ operation, prelude, setup, expression, grant, kind string }{
		{"Tensor.sub", dynamicShape, `dims=List.map(List.new(2),Dimension);a=Tensor.new(List.new(1.0,2.0),dims);b=Tensor.new(List.new(1.0,2.0,3.0),List.new(3)); `, "Subtract(a,b)", "", "Tensor.ShapeMismatch"},
		{"Tensor.matmul", dynamicShape, `dims=List.map(List.new(2),Dimension);a=Tensor.new(List.new(1.0,2.0),dims); `, "Matmul(a,a)", "", "Tensor.ShapeMismatch"},
		{"Checkpoint.encode", "", simulatedParameters, "Checkpoint.encode(p)", "cuda-sim:0", "Tensor.HostObservationRequired"},
		{"Tensor.relu", "", simulated, "Tensor.relu(remote)", "cuda-sim:0", "Device.UnsupportedOperation"},
		{"Gradients.zero", "", simulatedParameters, "Gradients.zero(p)", "cuda-sim:0", "Device.UnsupportedOperation"},
		{"Optimizer.AdamW.State.new", "", simulatedParameters + `config=Optimizer.AdamW.Config.new(0.25,0.5,0.5,2.0,0.5); `, "Optimizer.AdamW.State.new(p,config)", "cuda-sim:0", "Device.UnsupportedOperation"},
		{"Tensor.to", "", scalar, `Tensor.to(x,Device.new("cuda:0"))`, "cuda:0", "Device.Unavailable"},
		{"Parameters.to", "", parameters, `Parameters.to(p,Device.new("cuda:0"))`, "cuda:0", "Device.Unavailable"},
		{"Optimizer.SGD.State.to", "", parameters + `config=Optimizer.SGD.Config.new(0.25);state=Optimizer.SGD.State.new(p,config); `, `Optimizer.SGD.State.to(state,Device.new("cuda:0"))`, "cuda:0", "Device.Unavailable"},
		{"Optimizer.AdamW.State.to", "", parameters + `config=Optimizer.AdamW.Config.new(0.25,0.5,0.5,2.0,0.5);state=Optimizer.AdamW.State.new(p,config); `, `Optimizer.AdamW.State.to(state,Device.new("cuda:0"))`, "cuda:0", "Device.Unavailable"},
	} {
		t.Run(fixture.operation, func(t *testing.T) {
			directory := fileFixtureRoot(t)
			later := filepath.Join(directory, "later")
			text := fmt.Sprintf(`%s flow Main()->Int!{%s unused=%s;target=File.Write.new(File.Path.new(%q),Bytes.new("later"));commit target;return 7};return Main()`, fixture.prelude, fixture.setup, fixture.expression, later)
			arguments := []string{"run", sourceFile(t, text), "--write-root", directory, "--format", "json"}
			if fixture.grant != "" {
				arguments = append(arguments, "--allow-device", fixture.grant)
			}
			got := run(t, arguments...)
			if got.status != 1 || got.stdout != "" {
				t.Fatalf("runtime rejection: %+v", got)
			}
			diagnostic := receiptHostDiagnostic(t, got)
			if diagnostic.Code != "runtime.unhandled-error" || diagnostic.Error == nil || diagnostic.Error.Kind != fixture.kind {
				t.Fatalf("want %s from %s: %+v", fixture.kind, fixture.operation, got)
			}
			executed := false
			for _, frame := range diagnostic.Error.Trace {
				var operation string
				if err := json.Unmarshal(recordField(t, frame, "operation").Value, &operation); err != nil {
					t.Fatal(err)
				}
				if operation == fixture.operation {
					executed = true
				}
			}
			if !executed {
				t.Fatalf("failure did not execute %s: %s", fixture.operation, got.stderr)
			}
			if _, err := os.ReadFile(later); !os.IsNotExist(err) {
				t.Fatalf("effect after %s failure: %v", fixture.operation, err)
			}
		})
	}
}
