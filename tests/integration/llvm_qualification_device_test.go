package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

const nativeDeviceQualificationHarness = "testdata/native-qualification/device-qualification.c"

func nativeDeviceQualification(t *testing.T, scenarios []llvmRuntimeScenario) {
	t.Helper()
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
	scenario := llvmRuntimeScenario{name: "100-fold-iterations", source: text, number: 110}
	scenario.verify = nativeValuesCheck(1, func(t *testing.T, value runtimeValue) {
		checkNativeTensor(t, value, []string{"4"}, []float64{1, 2, 3, 4}, 0, 0)
	})
	nativeDeviceQualification(t, []llvmRuntimeScenario{scenario})
}

func TestLLVMDeviceRetainedCallbackValues(t *testing.T) {
	const text = `flow Step(state:Tensor<FP32>)->Record{value:Tensor<FP32>,next_state:Tensor<FP32>}! {
 host=Tensor.to(state,Device.new("cpu"));next=Tensor.to(host,Device.new("cuda-sim:0"));return Record.new(value:state,next_state:next)
}
flow Read(value:Tensor<FP32>)->Tensor<FP32>!{return Tensor.to(value,Device.new("cpu"))}
flow Main()->List<Tensor<FP32>>!{
 host=Tensor.new(List.new(-0.0),List.new());initial=Tensor.to(host,Device.new("cuda-sim:0"));retained=List.unfold(initial,20,Step);return List.map(retained,Read)
};return Main()`
	scenario := llvmRuntimeScenario{name: "20-retained-unfold-values", source: text, number: 111}
	scenario.verify = nativeValuesCheck(1, func(t *testing.T, value runtimeValue) {
		var retained []runtimeValue
		if err := json.Unmarshal(value.Value, &retained); err != nil || len(retained) != 20 {
			t.Fatalf("retained tensors: %d (%v)", len(retained), err)
		}
		for _, tensor := range retained {
			checkNativeTensorBits(t, tensor, nil, []uint32{0x80000000})
		}
	})
	nativeDeviceQualification(t, []llvmRuntimeScenario{scenario})
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
		shape := []string{"2"}
		if fixture.name == "empty-matmul" {
			shape = []string{"2", "3"}
		}
		if fixture.name == "empty-broadcast" {
			shape = []string{"0", "3"}
		}
		scenario := llvmRuntimeScenario{name: fixture.name, source: text, number: 130 + index}
		scenario.verify = nativeValuesCheck(1, func(t *testing.T, value runtimeValue) { checkNativeTensor(t, value, shape, fixture.want, 0, 0) })
		scenarios = append(scenarios, scenario)
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
	text := metalTrainingGradientSource(t)
	firstSource := strings.Replace(metalTrainingSource(t), "List.unfold(0, 200, Counter)", "List.unfold(0, 1, Counter)", 1)
	nativeDeviceQualification(t, []llvmRuntimeScenario{
		{name: "200-observed-gradient-steps", source: text, number: 140, verify: nativeValuesCheck(1, checkNativeTrainingGradients)},
		{name: "first-parameters", source: firstSource, number: 141, verify: nativeValuesCheck(1, func(t *testing.T, value runtimeValue) {
			for _, reference := range trainingReferences(t) {
				checkNumbers(t, trainingParameters(t, recordField(t, value, "parameters")), reference.FirstParameters, 1e-6, 1e-5)
			}
		})},
	})
}

func TestLLVMRetainedImmutableValues(t *testing.T) {
	const text = `flow Failure(error:Error)->Bool{return false}
flow Add(item:Int)->Int{return Math.add(item,1)}
flow Verify(original:Record{n:Int,bytes:Bytes,items:List<Int>,table:Map<String,Int>})->Bool onError Failure {
 derived=Record.merge(original,Record.new(n:Math.add(original.n,1),bytes:Bytes.new("xyz"),items:List.map(original.items,Add),table:Map.new(Record.new(key:"a",value:9))))
 text=String.FromBytes.new(original.bytes);entry=Map.get(original.table,"a");newentry=Map.get(derived.table,"a")
 flow VerifyChoiceTest1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: Int, text: String}) -> Bool {
     return Math.eq(request.selector, 18446744073709551616)
 }

 flow VerifyChoice2Test1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: Int, text: String}) -> Bool {
     return Math.eq(request.selector, 18446744073709551617)
 }

 flow VerifyChoice3Test1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: String}) -> Bool {
     return String.eq(request.selector, "abc")
 }

 flow VerifyChoice4Test1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, selector: Int}) -> Bool {
     return Math.eq(request.selector, 3)
 }

 flow VerifyChoice5Test1(request: Record{entry: Int, newentry: Int, selector: Int}) -> Bool {
     return Math.eq(request.selector, 5)
 }

 flow VerifyChoice6Test1(request: Record{newentry: Int, selector: Int}) -> Bool {
     return Math.eq(request.selector, 1)
 }

 flow VerifyChoice7Test1(value: Int) -> Bool {
     return Math.eq(value, 9)
 }

 flow VerifyChoice7Result1(value: Int) -> Bool {
     return true
 }

 flow VerifyChoice7Default(value: Int) -> Bool {
     return false
 }

 flow VerifyChoice6Result1(request: Record{newentry: Int, selector: Int}) -> Bool {
     return request.newentry |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoice7Test1, VerifyChoice7Result1)), VerifyChoice7Default))
 }

 flow VerifyChoice6Default(request: Record{newentry: Int, selector: Int}) -> Bool {
     return false
 }

 flow VerifyChoice5Result1(request: Record{entry: Int, newentry: Int, selector: Int}) -> Bool {
     return Record.new(selector: request.entry, newentry: request.newentry) |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoice6Test1, VerifyChoice6Result1)), VerifyChoice6Default))
 }

 flow VerifyChoice5Default(request: Record{entry: Int, newentry: Int, selector: Int}) -> Bool {
     return false
 }

 flow VerifyChoice4Result1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, selector: Int}) -> Bool {
     return Record.new(selector: Math.sum(request.derived.items), entry: request.entry, newentry: request.newentry) |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoice5Test1, VerifyChoice5Result1)), VerifyChoice5Default))
 }

 flow VerifyChoice4Default(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, selector: Int}) -> Bool {
     return false
 }

 flow VerifyChoice3Result1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: String}) -> Bool {
     return Record.new(selector: Math.sum(request.original.items), derived: request.derived, entry: request.entry, newentry: request.newentry) |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoice4Test1, VerifyChoice4Result1)), VerifyChoice4Default))
 }

 flow VerifyChoice3Default(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: String}) -> Bool {
     return false
 }

 flow VerifyChoice2Result1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: Int, text: String}) -> Bool {
     return Record.new(selector: request.text, derived: request.derived, entry: request.entry, newentry: request.newentry, original: request.original) |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoice3Test1, VerifyChoice3Result1)), VerifyChoice3Default))
 }

 flow VerifyChoice2Default(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: Int, text: String}) -> Bool {
     return false
 }

 flow VerifyChoiceResult1(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: Int, text: String}) -> Bool {
     return Record.new(selector: request.derived.n, derived: request.derived, entry: request.entry, newentry: request.newentry, original: request.original, text: request.text) |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoice2Test1, VerifyChoice2Result1)), VerifyChoice2Default))
 }

 flow VerifyChoiceDefault(request: Record{derived: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, entry: Int, newentry: Int, original: Record{bytes: Bytes, items: List<Int>, n: Int, table: Map<String, Int>}, selector: Int, text: String}) -> Bool {
     return false
 }

 return Record.new(selector: original.n, derived: derived, entry: entry, newentry: newentry, original: original, text: text) |> Match.apply(Match.new(List.new(Match.Case.new(VerifyChoiceTest1, VerifyChoiceResult1)), VerifyChoiceDefault))
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
