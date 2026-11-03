package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func semanticNativeTarget() string {
	if target := os.Getenv("LO_TEST_TARGET"); target != "" {
		return target
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func semanticNativeScenarios(t *testing.T, scenarios []llvmRuntimeScenario) {
	t.Helper()
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func semanticNativeObservation(source string, number int) llvmRuntimeScenario {
	return llvmRuntimeScenario{name: "native-runtime", source: source, number: number, captureOutput: number >= 2, harness: "testdata/native-qualification/semantic-effects.c", transform: func(t *testing.T, module []byte) []byte {
		t.Helper()
		contents := strings.ReplaceAll(string(module), "call void @lo_op_File_2Eread(", "call void @lo_semantic_read(")
		contents = strings.ReplaceAll(contents, "call void @lo_commit(", "call void @lo_semantic_commit(")
		contents = strings.Replace(contents, "define void @lo_entry(", "define void @lo_semantic_entry(", 1)
		return []byte(contents + "\ndeclare void @lo_semantic_read(ptr,ptr,i64,i64,ptr)\ndeclare void @lo_semantic_commit(ptr,ptr,i64,i64,ptr)\n")
	}}
}

func TestSemanticNativeReadOrder(t *testing.T) {
	const text = `flow Read(path: String) -> String! { bytes=File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
flow Join(a: String,b: String) -> String { return String.concat(a,b) }
flow Main() -> String! { unused=Read("unused");first=Read("repeat");second=Read("repeat");ordered=Join(b:Read("b"),a:Read("a"));return String.concat(first,second,ordered) };return Main()`
	scenario := semanticNativeObservation(text, 0)
	scenario.verify = nativeValueCheck("String", `"repeat:2;repeat:3;a:5;b:4;"`)
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeMatchRegions(t *testing.T) {
	const text = `flow Handle(error: Error) -> String { return "failed" }
flow Read(path: String) -> String onError Handle { bytes=File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
return Match.new(Read("selector"),"miss":Read("unselected"),"selector":Read("selected"),"unreached":Read("unreached-body"),_:Read("default"))`
	scenario := semanticNativeObservation(text, 1)
	scenario.verify = nativeValueCheck("String", `"selected"`)
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeRepeatedFrames(t *testing.T) {
	const text = `flow Recover(error:Error)->Error{return error};flow Divide(seed:Int,divisor:Int)->Int!{numerator=Math.add(seed,1);result=Math.div(numerator,divisor);return Math.add(result,seed)};flow Capture(seed:Int,divisor:Int)->Error onError Recover{result=Divide(seed,divisor);return Error.new("success",Int.to_string(result),Record.new(seed:seed,divisor:divisor))};return List.new(Capture(10,0),Capture(20,2),Capture(30,0),Capture(40,4))`
	value := nativeValue(t, runProgram(t, sourceFile(t, text), "run", "--format", "json"))
	var items []runtimeValue
	if err := json.Unmarshal(value.Value, &items); err != nil || len(items) != 4 {
		t.Fatalf("native frame results: %s (%v)", value.Value, err)
	}
	for index, numerator := range map[int]string{0: `"11"`, 2: `"31"`} {
		failure := items[index]
		if failure.Kind != "Math.DivideByZero" || failure.Input == nil || len(failure.Trace) != 2 {
			t.Fatalf("native frame failure %d: %+v", index, failure)
		}
		valueJSON(t, recordField(t, *failure.Input, "left_value"), numerator)
		for frame, name := range []string{`"Divide"`, `"Capture"`} {
			valueJSON(t, recordField(t, failure.Trace[frame], "flow"), name)
		}
	}
	for index, message := range map[int]string{1: "30", 3: "50"} {
		if items[index].Kind != "success" || items[index].Message != message || len(items[index].Trace) != 0 {
			t.Fatalf("native frame success %d reused prior failure: %+v", index, items[index])
		}
	}
}

func TestSemanticNativeErrorPrecedence(t *testing.T) {
	const text = `flow Handle(error: Error) -> Error { return error }
flow Main() -> Error onError Handle { first=File.read(File.Path.new("first"));later=File.read(File.Path.new("later"));return Error.new("missed","",Record.new()) };return Main()`
	scenario := semanticNativeObservation(text, 2)
	scenario.verify = nativeValuesCheck(1, func(t *testing.T, failure runtimeValue) {
		if failure.Type != "Error" || failure.Kind != "File.ReadFailed" || failure.Input == nil || len(failure.Trace) != 1 {
			t.Fatalf("first native error: %+v", failure)
		}
		valueJSON(t, recordField(t, *failure.Input, "file_path"), `"first"`)
		valueJSON(t, recordField(t, failure.Trace[0], "flow"), `"Main"`)
	})
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeCaptureLargeOutput(t *testing.T) {
	// The fixture must consume the complete output, including the final marker.
	text := fmt.Sprintf("return %q", strings.Repeat("A", 2<<20)+"CAPTURE_TRAILER_B005")
	scenario := semanticNativeObservation(text, 4)
	scenario.expectedStdout = "capture-complete\n"
	scenario.timeout = 5 * time.Second
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeCancellation(t *testing.T) {
	const text = `flow Handle(error: Error)->Int{return 99}
flow Main()->Int onError Handle { bytes=File.read(File.Path.new("unread"));target=File.Write.new(File.Path.new("unwritten"),bytes);commit target;return 1 };return Main()`
	semanticNativeScenarios(t, []llvmRuntimeScenario{semanticNativeObservation(text, 3)})
}

func TestSemanticNativeMetalProfile(t *testing.T) {
	const text = `flow Main()->Record{arithmetic:Tensor<FP32>,layout:Tensor<FP32>,reduction:Tensor<FP32>}!{
 metal=Device.new("metal:0");cpu=Device.new("cpu")
 a=Tensor.to(Tensor.new(List.new(-0.0,1.401298464324817e-45,-1.401298464324817e-45),List.new(3)),metal)
 b=Tensor.to(Tensor.new(List.new(0.0),List.new()),metal)
 arithmetic=Tensor.add(a,b);layout=Tensor.transpose(a,List.new(0))
 values=Tensor.to(Tensor.new(List.new(16777216.0,1.0,-16777216.0),List.new(3)),metal);reduction=Tensor.sum(values,List.new(0))
 return Record.new(arithmetic:Tensor.to(arithmetic,cpu),layout:Tensor.to(layout,cpu),reduction:Tensor.to(reduction,cpu)) };return Main()`
	scenario := llvmRuntimeScenario{name: "profile", source: text, harness: "testdata/native-qualification/semantic-metal.c", number: 0}
	scenario.verify = nativeValuesCheck(1, func(t *testing.T, result runtimeValue) {
		checkNativeTensorBits(t, recordField(t, result, "arithmetic"), []string{"3"}, []uint32{0, 0, 0})
		checkNativeTensorBits(t, recordField(t, result, "layout"), []string{"3"}, []uint32{0x80000000, 1, 0x80000001})
		checkNativeTensorBits(t, recordField(t, result, "reduction"), nil, []uint32{0})
	})
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeMetalCheckpointResume(t *testing.T) {
	for index, fixture := range []struct {
		optimizer, config, first, second string
	}{
		{"SGD", `Optimizer.SGD.Config.new(0.25)`, semanticOptimizerCheckpoint("SGD", "1.5", "1", "", "", ""), semanticOptimizerCheckpoint("SGD", "1", "2", "", "", "")},
		{"AdamW", `Optimizer.AdamW.Config.new(0.25,0.5,0.5,2.0,0.5)`, semanticOptimizerCheckpoint("AdamW", "1.625", "1", "1", "2", "0.5"), semanticOptimizerCheckpoint("AdamW", "1.296875", "2", "1.5", "3", "0.25")},
	} {
		t.Run(fixture.optimizer, func(t *testing.T) {
			text := fmt.Sprintf(`flow Main()->Bytes!{
 metal=Device.new("metal:0");cpu=Device.new("cpu");x=Tensor.new(List.new(2.0),List.new());p=Parameters.new(Record.new(x:x))
 mp=Parameters.to(p,metal);cp=Parameters.to(mp,cpu);template=Optimizer.%s.State.new(cp,%s)
 restored=Checkpoint.decode(Bytes.new(%q),template);moved=Optimizer.%s.State.to(restored,metal)
 gradient=Tensor.to(x,metal);g=Gradients.new(moved.parameters,Record.new(x:gradient));resumed=Optimizer.%s.step(moved,g)
 host=Optimizer.%s.State.to(resumed,cpu);return Checkpoint.encode(host) };return Main()`, fixture.optimizer, fixture.config, fixture.first, fixture.optimizer, fixture.optimizer, fixture.optimizer)
			scenario := llvmRuntimeScenario{name: "resume", source: text, harness: "testdata/native-qualification/semantic-metal.c", number: index + 1}
			scenario.verify = nativeValuesCheck(1, func(t *testing.T, result runtimeValue) {
				assertSemanticCheckpoint(t, result, fixture.second)
			})
			semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
		})
	}
}

func TestSemanticNativeMetalFailureCleanup(t *testing.T) {
	const text = `flow Handle(e:Error)->String{return e.kind};flow Main()->String onError Handle {a=Tensor.to(Tensor.new(List.new(-1.0),List.new()),Device.new("metal:0"));unused=Tensor.sqrt(a);target=File.Write.new(File.Path.new("later"),Bytes.new("bad"));commit target;return "unexpected"};return Main()`
	scenario := llvmRuntimeScenario{name: "numeric-error", source: text, harness: "testdata/native-runtime/metal-executor.c"}
	scenario.verify = nativeValuesCheck(3, func(t *testing.T, result runtimeValue) {
		valueJSON(t, result, `"Numeric.Domain"`)
	})
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
	semanticNativeScenarios(t, llvmMetalCancellationScenarios())
}
