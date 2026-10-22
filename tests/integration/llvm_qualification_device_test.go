package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

const nativeDeviceQualificationHarness = "testdata/native-qualification/device-qualification.c"

func nativeDeviceQualification(t *testing.T, scenarios []llvmRuntimeScenario) {
	t.Helper()
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	for index := range scenarios {
		scenarios[index].harness = nativeDeviceQualificationHarness
		number := scenarios[index].number
		scenarios[index].captureOutput = (number >= 100 && number <= 103) || (number >= 120 && number <= 124)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMCheckpointTransferFailureCleanup(t *testing.T) {
	const text = `flow Main()->Bytes!{
 x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x));
 state=Optimizer.AdamW.State.new(p,Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1));
 moved=Optimizer.AdamW.State.to(state,Device.new("metal:0"));
 restored=Optimizer.AdamW.State.to(moved,Device.new("cpu"));
 data=Checkpoint.encode(restored); effect=File.Write.new(File.Path.new("never"),data); commit effect; return data
 }; return Main()`
	var scenarios []llvmRuntimeScenario
	for number, name := range []string{"third-upload-failure", "download-failure", "third-upload-cancellation", "reserved-allocation-budget"} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: text, number: 100 + number})
	}
	nativeDeviceQualification(t, scenarios)
}

func TestLLVMDeviceCallbackLifetime(t *testing.T) {
	const text = `flow Counter(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.add(state,1))}
flow Step(state:Tensor<FP32>,index:Int)->Tensor<FP32>! {
 transferred=Tensor.to(state,Device.new("cuda-sim:0"));return Tensor.to(transferred,Device.new("cpu"))
}
flow Main()->Tensor<FP32>! {
 initial=Tensor.new(List.new(1.0,2.0,3.0,4.0),List.new(4));steps=List.unfold(0,100,Counter);return List.fold(steps,initial,Step)
};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, &observedDeviceFactory{}, "--allow-device", "cuda-sim:0")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("reference callback: %+v", got)
	}
	var envelope struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	result, expected := successfulValue(t, envelope.Result), got.stdout
	checkNumbers(t, tensorNumbers(t, result), []float64{1, 2, 3, 4}, 0, 0)
	semanticTransferCount(t, receipts, 200)
	nativeDeviceQualification(t, []llvmRuntimeScenario{{name: "100-fold-iterations", source: text, expectedStdout: expected, number: 110}})
}

func TestLLVMDeviceRetainedCallbackValues(t *testing.T) {
	const text = `flow Step(state:Tensor<FP32>)->Record{value:Tensor<FP32>,next_state:Tensor<FP32>}! {
 host=Tensor.to(state,Device.new("cpu"));next=Tensor.to(host,Device.new("cuda-sim:0"));return Record.new(value:state,next_state:next)
}
flow Read(value:Tensor<FP32>)->Tensor<FP32>!{return Tensor.to(value,Device.new("cpu"))}
flow Main()->List<Tensor<FP32>>!{
 host=Tensor.new(List.new(-0.0),List.new());initial=Tensor.to(host,Device.new("cuda-sim:0"));retained=List.unfold(initial,20,Step);return List.map(retained,Read)
};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, &observedDeviceFactory{}, "--allow-device", "cuda-sim:0")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("reference retained callback: %+v", got)
	}
	expected := got.stdout
	if strings.Count(expected, `"values":["-0"]`) != 20 {
		t.Fatal("retained values lost signed-zero bits")
	}
	semanticTransferCount(t, receipts, 61)
	nativeDeviceQualification(t, []llvmRuntimeScenario{{name: "20-retained-unfold-values", source: text, expectedStdout: expected, number: 111}})
}

func TestLLVMMetalEmptyProfiles(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for index, fixture := range []struct {
		name, body string
		want       []float64
	}{
		{"empty-sum", `a=Tensor.to(Tensor.new(List.new(),List.new(2,0)),Device.new("metal:0"));return Tensor.to(Tensor.sum(a,List.new(1)),Device.new("cpu"))`, []float64{0, 0}},
		{"empty-matmul", `a=Tensor.to(Tensor.new(List.new(),List.new(2,0)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(),List.new(0,3)),Device.new("metal:0"));return Tensor.to(Tensor.matmul(a,b),Device.new("cpu"))`, []float64{0, 0, 0, 0, 0, 0}},
		{"empty-broadcast", `a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));return Tensor.to(Tensor.broadcast(a,List.new(0,3)),Device.new("cpu"))`, []float64{}},
	} {
		text := `flow Main()->Tensor<FP32>!{` + fixture.body + `};return Main()`
		result, _, expected := metalResult(t, text, metalComputeFactory())
		checkNumbers(t, tensorNumbers(t, result), fixture.want, 0, 0)
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: text, expectedStdout: expected, number: 130 + index})
	}
	nativeDeviceQualification(t, scenarios)
}

func TestLLVMMetalTensorBoundaries(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for index, fixture := range []struct{ name, typ, expression string }{
		{"host-values", "List<Float>", "Tensor.values(transferred)"},
		{"publication", "Tensor<FP32>", "transferred"},
		{"storage-only-backend", "Tensor<FP32>", "Tensor.add(transferred,transferred)"},
		{"mismatched-locations", "Tensor<FP32>", "Tensor.add(source,transferred)"},
		{"host-scalar", "Float", "Tensor.to_float(transferred)"},
	} {
		text := deviceSource("metal:0", fixture.typ, fixture.expression)
		if index == 4 {
			text = strings.Replace(text, "List.new(1.25, -0.0, 1.401298464324817e-45, -2.5), List.new(2, 2)", "List.new(1.25), List.new()", 1)
		}
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: text, number: 120 + index})
	}
	nativeDeviceQualification(t, scenarios)
}

func TestLLVMMetalTrainingGradients(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	text := metalTrainingGradientSource(t)
	actual, _, expected := metalResult(t, text, metalComputeFactory())
	cpu, _ := runWithDevices(t, context.Background(), strings.ReplaceAll(text, "metal:0", "cpu"), device.SoftwareFactory{})
	var steps, cpuSteps []runtimeValue
	if err := json.Unmarshal(actual.Value, &steps); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(trainingResult(t, cpu).Value, &cpuSteps); err != nil || len(steps) != 200 || len(cpuSteps) != 200 {
		t.Fatalf("gradient observations native=%d cpu=%d error=%v", len(steps), len(cpuSteps), err)
	}
	for index, step := range steps {
		loss := trainingFloat(t, recordField(t, step, "loss"))
		gradients := trainingParameters(t, recordField(t, step, "gradients"))
		checkNumbers(t, []float64{loss}, []float64{trainingFloat(t, recordField(t, cpuSteps[index], "loss"))}, 1e-5, 1e-4)
		checkNumbers(t, gradients, trainingParameters(t, recordField(t, cpuSteps[index], "gradients")), 1e-5, 1e-4)
		if index == 0 {
			for _, reference := range trainingReferences(t) {
				checkNumbers(t, gradients, reference.InitialGradients, 1e-6, 1e-5)
				checkNumbers(t, []float64{loss}, []float64{reference.InitialLoss}, 0, 0)
			}
		}
	}
	firstSource := strings.Replace(metalTrainingSource(t), "List.unfold(0, 200, Counter)", "List.unfold(0, 1, Counter)", 1)
	first, _, firstExpected := metalResult(t, firstSource, metalComputeFactory())
	for _, reference := range trainingReferences(t) {
		checkNumbers(t, trainingParameters(t, recordField(t, first, "parameters")), reference.FirstParameters, 1e-6, 1e-5)
	}
	nativeDeviceQualification(t, []llvmRuntimeScenario{{name: "200-observed-gradient-steps", source: text, expectedStdout: expected, number: 140}, {name: "first-parameters", source: firstSource, expectedStdout: firstExpected, number: 141}})
	t.Log("200 losses and 1800 gradient coordinates match the CPU reference; initial gradients and first parameters match independent references")
}

func TestLLVMRetainedImmutableValues(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	const text = `flow Failure(error:Error)->Bool{return false}
flow Add(item:Int)->Int{return Math.add(item,1)}
flow Verify(original:Record{n:Int,bytes:Bytes,items:List<Int>,table:Map<String,Int>})->Bool onError Failure {
 derived=Record.merge(original,Record.new(n:Math.add(original.n,1),bytes:Bytes.new("xyz"),items:List.map(original.items,Add),table:Map.new(Record.new(key:"a",value:9))))
 text=UTF8.String.new(original.bytes);entry=Map.get(original.table,"a");newentry=Map.get(derived.table,"a")
 return Match.new(original.n,18446744073709551616:Match.new(derived.n,18446744073709551617:Match.new(text,"abc":Match.new(Math.sum(original.items),3:Match.new(Math.sum(derived.items),5:Match.new(entry,1:Match.new(newentry,9:true,_:false),_:false),_:false),_:false),_:false),_:false),_:false)
}
return Record.new(n:18446744073709551616,bytes:Bytes.new("abc"),items:List.new(1,2),table:Map.new(Record.new(key:"a",value:1)))`
	scenario := llvmRuntimeScenario{name: "retained-record-and-derived-values", source: text, harness: "testdata/native-qualification/device-immutable.c", transform: func(t *testing.T, module []byte) []byte {
		t.Helper()
		// Verify is named explicitly because flow identifiers follow source order.
		contents := string(module)
		if !strings.Contains(contents, "define void @lo_flow_2(") {
			t.Fatal("missing Verify flow")
		}
		return []byte(contents)
	}}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{scenario})
}
