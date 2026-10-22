package integration_test

import (
	"encoding/json"
	"testing"
)

const collectionScalarContexts = `flow Inner(a:Int,b:Int,c:Int,item:Int)->Int{return Math.add(Math.add(Math.add(a,b),c),item)};flow Outer(seed:Int,item:Int)->List<Int>{return List.map(List.new(item,Math.add(item,1)),seed,2,3,Inner)};return List.map(List.new(1,2,3),10,Outer)`
const collectionTensorContexts = `flow Inner(a:Tensor<FP32>,b:Tensor<FP32>,item:Int)->Tensor<FP32>!{return Tensor.reshape(a,List.new(1))};flow Outer(seed:Tensor<FP32>,item:Int)->List<Tensor<FP32>>!{return List.map(List.new(item,Math.add(item,1)),seed,seed,Inner)};flow Main()->List<List<Tensor<FP32>>>!{initial=Tensor.new(List.new(-0.0),List.new());return List.map(List.new(1,2,3),initial,Outer)};return Main()`
const collectionTensorFailure = `flow Inner(seed:Tensor<FP32>,item:Int)->Tensor<FP32>!{value=Math.div(1,item);return Tensor.reshape(seed,List.new(1))};flow Outer(seed:Tensor<FP32>,item:Int)->List<Tensor<FP32>>!{return List.map(List.new(1,0,2),seed,Inner)};flow Main()->List<List<Tensor<FP32>>>!{initial=Tensor.new(List.new(-0.0),List.new());return List.map(List.new(1,2),initial,Outer)};return Main()`
const collectionTensorCancellation = `flow Step(seed:Tensor<FP32>,state:Tensor<FP32>,item:Int)->Tensor<FP32>!{return Tensor.reshape(seed,List.new(1))};flow Main()->Tensor<FP32>!{initial=Tensor.new(List.new(-0.0),List.new());steps=List.new(1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24);return List.fold(steps,initial,initial,Step)};return Main()`

func TestCollectionArgumentResults(t *testing.T) {
	valueJSON(t, runValue(t, collectionScalarContexts), `[{"type":"List<Int>","value":[{"type":"Int","value":"16"},{"type":"Int","value":"17"}]},{"type":"List<Int>","value":[{"type":"Int","value":"17"},{"type":"Int","value":"18"}]},{"type":"List<Int>","value":[{"type":"Int","value":"18"},{"type":"Int","value":"19"}]}]`)
	value := successfulValue(t, runValue(t, collectionTensorContexts))
	var outer []runtimeValue
	if err := json.Unmarshal(value.Value, &outer); err != nil {
		t.Fatal(err)
	}
	if len(outer) != 3 {
		t.Fatal("nested callback result count")
	}
	for _, list := range outer {
		var inner []runtimeValue
		if err := json.Unmarshal(list.Value, &inner); err != nil {
			t.Fatal(err)
		}
		if len(inner) != 2 {
			t.Fatal("inner callback result count")
		}
		for _, tensor := range inner {
			checkNumbers(t, tensorNumbers(t, tensor), []float64{0}, 0, 0)
		}
	}
}

func TestLLVMCollectionArgumentLifetime(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for number, fixture := range []struct {
		name, source string
		steps, peak  uint64
	}{{"nested-scalar-context", collectionScalarContexts, 94, 0}, {"retained-aliased-tensor-context", collectionTensorContexts, 82, 4}, {"nested-recoverable-error", collectionTensorFailure, 40, 4}, {"terminal-cancellation", collectionTensorCancellation, 105, 4}} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: fixture.source, number: number, harness: "testdata/native-qualification/collection-arguments.c", verify: func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("collection argument lifetime: %+v", got)
			}
			var observation struct{ Steps, Peak uint64 }
			if err := json.Unmarshal([]byte(got.stdout), &observation); err != nil || observation.Steps != fixture.steps || observation.Peak != fixture.peak {
				t.Fatalf("collection charge observation: %+v %v", got, err)
			}
			t.Logf("collection arguments %s: %s", fixture.name, got.stdout)
		}})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
