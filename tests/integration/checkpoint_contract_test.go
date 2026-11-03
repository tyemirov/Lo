package integration_test

import (
	"encoding/base64"
	"fmt"

	"strings"
	"testing"
)

func checkpointBuiltinExamples() []builtinExample {
	var examples []builtinExample
	for _, tc := range []struct{ name, expression, typ string }{
		{"Checkpoint.encode", "Checkpoint.encode(p)", "Bytes"},
		{"Checkpoint.decode", "Checkpoint.decode(Checkpoint.encode(p),p)", "Parameters<Record{x: Tensor<FP32>}>"},
		{"Parameters.to", `Parameters.to(p,Device.new("cpu"))`, "Parameters<Record{x: Tensor<FP32>}>"},
		{"Optimizer.SGD.State.to", `Optimizer.SGD.State.to(sgd,Device.new("cpu"))`, "Optimizer.SGD.State<Record{x: Tensor<FP32>}>"},
		{"Optimizer.AdamW.State.to", `Optimizer.AdamW.State.to(adam,Device.new("cpu"))`, "Optimizer.AdamW.State<Record{x: Tensor<FP32>}>"},
	} {
		text := fmt.Sprintf(`flow Example() -> %s! { x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); sgd=Optimizer.SGD.State.new(p,Optimizer.SGD.Config.new(0.25)); adam=Optimizer.AdamW.State.new(p,Optimizer.AdamW.Config.new(0.25,0.5,0.5,0.5,0.5)); return %s }; return Example()`, tc.typ, tc.expression)
		examples = append(examples, builtinExample{tc.name, text, tc.typ + "!"})
	}
	return examples
}

func checkpointCatalogResults() map[string]string {
	const parameters = `{"type":"Parameters\u003cRecord{x: Tensor\u003cFP32\u003e}\u003e","value":{"x":{"type":"Tensor\u003cFP32\u003e","value":{"dtype":"fp32","shape":[],"device":"cpu","values":["2"]}}}}`
	optimizers := optimizerCatalogResults()
	return map[string]string{
		"Checkpoint.encode":        fmt.Sprintf(`{"type":"Bytes","value":%q}`, base64.StdEncoding.EncodeToString([]byte(parameters))),
		"Checkpoint.decode":        parameters,
		"Parameters.to":            parameters,
		"Optimizer.SGD.State.to":   optimizers["Optimizer.SGD.State.new"],
		"Optimizer.AdamW.State.to": optimizers["Optimizer.AdamW.State.new"],
	}
}

func TestCheckpointTypeAndInspection(t *testing.T) {
	for _, text := range []string{
		`return Checkpoint.encode(1)`,
		`return Checkpoint.decode(Bytes.new("{}"),"wrong")`,
		`flow F() -> Bytes! { x=Tensor.new(List.new(1.0),List.new()); return Checkpoint.encode(x) }; return F()`,
		`flow F() -> Parameters<Record{x:Tensor<FP32>}>! { x=Tensor.new(List.new(1.0),List.new()); p=Parameters.new(Record.new(x:x)); return Parameters.to(p,1) }; return F()`,
	} {
		got := run(t, "check", sourceFile(t, text), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "check.") {
			t.Fatalf("checkpoint type accepted: %+v", got)
		}
	}
	setup, typ := checkpointSetup("AdamW")
	text := fmt.Sprintf(`flow Run() -> %s! { %s saved=Checkpoint.encode(original); loaded=Checkpoint.decode(saved,original); moved=Optimizer.AdamW.State.to(loaded,Device.new("metal:0")); return Optimizer.AdamW.State.to(moved,cpu) }; return Run()`, typ, setup)
	for _, command := range []string{"parse", "check", "inspect"} {
		got := run(t, command, sourceFile(t, text), "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("%s: %+v", command, got)
		}
		if command == "inspect" {
			for _, fragment := range []string{"Checkpoint.InvalidData", "Checkpoint.SchemaMismatch", "Device.TransferFailed", `"metal:0"`, "Optimizer.AdamW.State.to"} {
				if !strings.Contains(got.stdout, fragment) {
					t.Fatalf("inspection missing %s", fragment)
				}
			}
		}
	}
}

func checkpointTransferSource(kind string) string {
	setup, _ := checkpointSetup(kind)
	transfer := kind + ".to"
	if kind != "Parameters" {
		transfer = "Optimizer." + kind + ".State.to"
	}
	return fmt.Sprintf(`flow Run() -> Record{before: Bytes, after: Bytes}! { %s before=Checkpoint.encode(original); moved=%s(original,Device.new("metal:0")); restored=%s(moved,cpu); after=Checkpoint.encode(restored); return Record.new(before:before,after:after) }; return Run()`, setup, transfer, transfer)
}

const checkpointAdamWResume = `flow Run()->Record{expected:Bytes,actual:Bytes}!{
 cpu=Device.new("cpu"); device=Device.new("metal:0");
 x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x));
 template=Optimizer.AdamW.State.new(p,Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1));
 state=Optimizer.AdamW.State.to(template,device);
 g1=Tensor.to(Tensor.new(List.new(0.1,-0.2),List.new(2)),device);
 g2=Tensor.to(Tensor.new(List.new(0.0,0.3),List.new(2)),device);
 first=Optimizer.AdamW.step(state,Gradients.new(state.parameters,Record.new(x:g1)));
 second=Optimizer.AdamW.step(first,Gradients.new(first.parameters,Record.new(x:g2)));
 saved=Checkpoint.encode(Optimizer.AdamW.State.to(first,cpu));
 imported=Checkpoint.decode(saved,template);
 restored=Optimizer.AdamW.State.to(imported,device);
 resumed=Optimizer.AdamW.step(restored,Gradients.new(restored.parameters,Record.new(x:g2)));
 expected=Checkpoint.encode(Optimizer.AdamW.State.to(second,cpu));
 actual=Checkpoint.encode(Optimizer.AdamW.State.to(resumed,cpu));
 return Record.new(expected:expected,actual:actual)
};return Run()`

func assertCheckpointAdamW(t *testing.T, result runtimeValue) {
	t.Helper()
	if string(recordField(t, result, "expected").Value) != string(recordField(t, result, "actual").Value) {
		t.Fatal("AdamW resume changed complete state")
	}
}
