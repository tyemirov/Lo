package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMMetalTransferOnlyArithmetic(t *testing.T) {
	input := sourceFile(t, `flow Handle(e:Error)->String{return e.kind};flow Main()->String onError Handle {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("cuda-sim:0"));b=Tensor.add(a,a);return "unexpected"};return Main()`)
	artifact, built := buildForTest(t, input)
	if built.status != 0 {
		t.Fatalf("build: %+v", built)
	}
	got := runExecutable(t, artifact, "run", "--allow-device", "cuda-sim:0", "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"value":"Device.UnsupportedOperation"`) {
		t.Fatalf("transfer-only arithmetic: %+v", got)
	}
}

func llvmMetalScenario(t *testing.T, name, text string, view bool, check func(*testing.T, runtimeValue)) llvmRuntimeScenario {
	t.Helper()
	number := 0
	if view {
		number = 1
	}
	return llvmRuntimeScenario{name: name, source: text, verify: nativeValuesCheck(3, check), harness: "testdata/native-runtime/metal-executor.c", number: number}
}

func llvmMetalPrimitives(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, fixture := range []struct {
		name, body string
		expected   []float64
		view       bool
	}{
		{"add", `return Tensor.add(a,b)`, []float64{3, 6, 5, 8, 7, 10}, false},
		{"subtract", `return Tensor.sub(a,b)`, []float64{-1, -2, 1, 0, 3, 2}, false},
		{"multiply", `return Tensor.mul(a,b)`, []float64{2, 8, 6, 16, 10, 24}, false},
		{"divide", `return Tensor.div(a,b)`, []float64{.5, .5, 1.5, 1, 2.5, 1.5}, false},
		{"sqrt", `return Tensor.sqrt(Tensor.mul(a,a))`, []float64{1, 2, 3, 4, 5, 6}, false},
		{"relu", `return Tensor.relu(Tensor.sub(a,b))`, []float64{0, 0, 1, 0, 3, 2}, false},
		{"transpose", `return Tensor.transpose(a,List.new(1,0))`, []float64{1, 3, 5, 2, 4, 6}, false},
		{"reshape", `return Tensor.reshape(a,List.new(6))`, []float64{1, 2, 3, 4, 5, 6}, true},
		{"broadcast", `return Tensor.broadcast(b,List.new(3,2))`, []float64{2, 4, 2, 4, 2, 4}, false},
		{"sum", `return Tensor.sum(a,List.new(0))`, []float64{9, 12}, false},
		{"mean", `return Tensor.mean(a,List.new(1))`, []float64{1.5, 3.5, 5.5}, false},
		{"matmul", `return Tensor.matmul(a,Tensor.reshape(a,List.new(2,3)))`, []float64{9, 12, 15, 19, 26, 33, 29, 40, 51}, false},
		{"rectangular-matmul", `return Tensor.matmul(a,Tensor.reshape(b,List.new(2,1)))`, []float64{10, 22, 34}, false},
		{"nonadjacent-reduction", `return Tensor.sum(Tensor.reshape(a,List.new(1,3,2)),List.new(0,2))`, []float64{3, 7, 11}, false},
		{"rank-three-transpose", `return Tensor.transpose(Tensor.reshape(a,List.new(1,3,2)),List.new(2,0,1))`, []float64{1, 3, 5, 2, 4, 6}, false},
		{"empty-axes-sum", `return Tensor.sum(a,List.new())`, []float64{1, 2, 3, 4, 5, 6}, true},
		{"empty-axes-mean", `return Tensor.mean(a,List.new())`, []float64{1, 2, 3, 4, 5, 6}, true},
	} {
		text := fmt.Sprintf(`flow Compute(a:Tensor<FP32>,b:Tensor<FP32>)->Tensor<FP32>!{%s};flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0),List.new(3,2)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(2.0,4.0),List.new(2)),Device.new("metal:0"));return Tensor.to(Compute(a,b),Device.new("cpu"))};return Main()`, fixture.body)
		shapes := map[string][]string{
			"transpose": {"2", "3"}, "reshape": {"6"}, "sum": {"2"}, "mean": {"3"},
			"matmul": {"3", "3"}, "rectangular-matmul": {"3", "1"},
			"nonadjacent-reduction": {"3"}, "rank-three-transpose": {"2", "1", "3"},
		}
		shape := []string{"3", "2"}
		if selected, exists := shapes[fixture.name]; exists {
			shape = selected
		}
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, fixture.view, func(t *testing.T, value runtimeValue) {
			checkNativeTensor(t, value, shape, fixture.expected, 0, 0)
		}))
	}
	return scenarios
}
func TestLLVMMetalPrimitives(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalPrimitives(t))
}
func TestLLVMLinuxMetalPrimitives(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalPrimitives(t))
}

func llvmMetalProfiles(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, fixture := range []struct {
		name, a, b, operation string
		bits                  []uint32
	}{
		{"normalization-add", "-0.0,1e-45,-1e-45", "0.0", "Tensor.add(a,b)", []uint32{0, 0, 0}},
		{"underflow-multiply", "1.1754943508222875e-38,-1.1754943508222875e-38", "0.5", "Tensor.mul(a,b)", []uint32{0, 0}},
		{"signed-zero-divide", "-0.0,1e-45,-1e-45", "-1.0", "Tensor.div(a,b)", []uint32{0, 0, 0}},
		{"sqrt-subnormals", "-0.0,1e-45,-1e-45", "1.0", "Tensor.sqrt(a)", []uint32{0, 0, 0}},
		{"layout-bit-preservation", "-0.0,1.401298464324817e-45,-1.401298464324817e-45", "1.0", "Tensor.transpose(a,List.new(0))", []uint32{0x80000000, 1, 0x80000001}},
		{"ordered-reduction", "16777216.0,1.0,-16777216.0", "1.0", "Tensor.sum(a,List.new(0))", []uint32{0}},
	} {
		text := fmt.Sprintf(`flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(%s),List.new(%d)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(%s),List.new()),Device.new("metal:0"));return Tensor.to(%s,Device.new("cpu"))};return Main()`, fixture.a, strings.Count(fixture.a, ",")+1, fixture.b, fixture.operation)
		shape := []string{fmt.Sprint(len(fixture.bits))}
		if fixture.name == "ordered-reduction" {
			shape = nil
		}
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, false, func(t *testing.T, value runtimeValue) {
			checkNativeTensorBits(t, value, shape, fixture.bits)
		}))
	}
	return scenarios
}
func TestLLVMMetalNumericProfile(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalProfiles(t))
}
func TestLLVMLinuxMetalNumericProfile(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalProfiles(t))
}

func llvmMetalDerivatives(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	fixtures := append(derivativeFixtures(), derivativeFixture{"shared-and-stopped", `r=Tensor.mul(p.a,p.a);twice=Tensor.add(r,r);stopped=Autodiff.stop(p.b);return Tensor.sum(Tensor.add(twice,stopped),List.new(0))`, []float64{1, 2}, []float64{3, 4}, []int{2}, []int{2}, []float64{4, 8}, []float64{0, 0}})
	for _, fixture := range fixtures {
		for _, field := range []string{"a", "b"} {
			text := fixture.source("Autodiff.grad(p,Loss).gradients."+field, fixture.a, fixture.b)
			text = strings.Replace(text, "p=Record.new(a:a,b:b)", `ma=Tensor.to(a,Device.new("metal:0"));mb=Tensor.to(b,Device.new("metal:0"));p=Record.new(a:ma,b:mb)`, 1)
			text = strings.Replace(text, "return result.gradients."+field, `return Tensor.to(result.gradients.`+field+`,Device.new("cpu"))`, 1)
			expected, shape := fixture.ga, fixture.as
			if field == "b" {
				expected, shape = fixture.gb, fixture.bs
			}
			scenarios = append(scenarios, llvmMetalScenario(t, fixture.name+"-"+field, text, false, func(t *testing.T, value runtimeValue) {
				checkNativeTensor(t, value, nativeShape(shape), expected, 1e-6, 1e-5)
			}))
		}
	}
	return scenarios
}
func TestLLVMMetalDerivatives(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalDerivatives(t))
}
func TestLLVMLinuxMetalDerivatives(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalDerivatives(t))
}

func llvmMetalOptimizers(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, fixture := range []struct {
		name, config  string
		first, second []float64
	}{
		{"SGD", "Optimizer.SGD.Config.new(0.1)", []float64{.99, -1.98}, []float64{.99, -2.01}},
		{"AdamW", "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)", []float64{.9890000224113464, -1.9880000352859497}, []float64{.9813104271888733, -1.988489031791687}},
	} {
		text := fmt.Sprintf(`flow Main()->Record{old:Tensor<FP32>,first:Tensor<FP32>,second:Tensor<FP32>,step:Int}!{x=Tensor.to(Tensor.new(List.new(1.0,-2.0),List.new(2)),Device.new("metal:0"));p=Parameters.new(Record.new(x:x));original=Optimizer.%s.State.new(p,%s);g1=Tensor.to(Tensor.new(List.new(0.1,-0.2),List.new(2)),Device.new("metal:0"));g2=Tensor.to(Tensor.new(List.new(0.0,0.3),List.new(2)),Device.new("metal:0"));first=Optimizer.%s.step(original,Gradients.new(p,Record.new(x:g1)));second=Optimizer.%s.step(first,Gradients.new(p,Record.new(x:g2)));old_values=Parameters.values(original.parameters);first_values=Parameters.values(first.parameters);second_values=Parameters.values(second.parameters);return Record.new(old:Tensor.to(old_values.x,Device.new("cpu")),first:Tensor.to(first_values.x,Device.new("cpu")),second:Tensor.to(second_values.x,Device.new("cpu")),step:second.step)};return Main()`, fixture.name, fixture.config, fixture.name, fixture.name)
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, false, func(t *testing.T, value runtimeValue) {
			checkNativeTensor(t, recordField(t, value, "old"), []string{"2"}, []float64{1, -2}, 0, 0)
			checkNativeTensor(t, recordField(t, value, "first"), []string{"2"}, fixture.first, 1e-6, 1e-5)
			checkNativeTensor(t, recordField(t, value, "second"), []string{"2"}, fixture.second, 1e-6, 1e-5)
			valueJSON(t, recordField(t, value, "step"), `"2"`)
		}))
	}
	for _, kind := range []string{"Parameters", "SGD", "AdamW"} {
		text := checkpointTransferSource(kind)
		scenarios = append(scenarios, llvmMetalScenario(t, "checkpoint-transfer-"+kind, text, true, func(t *testing.T, value runtimeValue) {
			checkNativeCheckpointTransfer(t, value, kind)
		}))
	}
	scenarios = append(scenarios, llvmMetalScenario(t, "checkpoint-resumed-AdamW", checkpointAdamWResume, false, func(t *testing.T, value runtimeValue) {
		assertCheckpointAdamW(t, value)
		state := nativeCheckpoint(t, recordField(t, value, "actual"))
		checkNativeTensor(t, recordField(t, recordField(t, state, "parameters"), "x"), []string{"2"}, []float64{.9813104271888733, -1.988489031791687}, 1e-6, 1e-5)
		checkNativeTensor(t, recordField(t, recordField(t, state, "first_moment"), "x"), []string{"2"}, []float64{.009000002406537533, .012000003829598427}, 1e-8, 1e-5)
		checkNativeTensor(t, recordField(t, recordField(t, state, "second_moment"), "x"), []string{"2"}, []float64{9.989871614379808e-6, .00012995832366868854}, 1e-10, 1e-5)
		valueJSON(t, recordField(t, state, "step"), `"2"`)
	}))
	return scenarios
}
func TestLLVMMetalOptimizersAndCheckpoints(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalOptimizers(t))
}
func TestLLVMLinuxMetalOptimizersAndCheckpoints(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalOptimizers(t))
}

func llvmMetalTraining(t *testing.T) []llvmRuntimeScenario {
	text := metalTrainingSource(t)
	scenario := llvmMetalScenario(t, "200-step-training", text, false, func(t *testing.T, value runtimeValue) { assertMetalTrainingResult(t, value) })
	scenario.number = 2
	return []llvmRuntimeScenario{scenario}
}
func TestLLVMMetalTraining(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalTraining(t))
}
func TestLLVMLinuxMetalTraining(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalTraining(t))
}

func llvmMetalFailures(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, fixture := range []struct{ name, setup, expression, kind string }{
		{"divide-zero", `a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(0.0),List.new()),Device.new("metal:0"));`, `Tensor.div(a,b)`, "Numeric.DivideByZero"},
		{"normalized-zero-divisor", `a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(1e-45),List.new()),Device.new("metal:0"));`, `Tensor.div(a,b)`, "Numeric.DivideByZero"},
		{"sqrt-domain", `a=Tensor.to(Tensor.new(List.new(-1.0),List.new()),Device.new("metal:0"));`, `Tensor.sqrt(a)`, "Numeric.Domain"},
		{"overflow", `a=Tensor.to(Tensor.new(List.new(1e20),List.new()),Device.new("metal:0"));`, `Tensor.mul(a,a)`, "Numeric.NonFinite"},
	} {
		text := fmt.Sprintf(`flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {%s;unused=%s;return Record.new(kind:"unexpected",message:"")};return Main()`, fixture.setup, fixture.expression)
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, false, nativeErrorKind(fixture.kind, strings.Split(fixture.expression, "(")[0]+": expected valid finite metal-fp32 arithmetic, got invalid value at row-major element 0")))
	}
	text := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {a=Tensor.to(Tensor.new(List.new(0.0),List.new()),Device.new("metal:0"));unused=Autodiff.grad(Record.new(x:a),Loss);return Record.new(kind:"unexpected",message:"")};return Main()`
	scenarios = append(scenarios, llvmMetalScenario(t, "undefined-derivative", text, false, nativeErrorKind("Autodiff.UndefinedDerivative", "expected valid finite metal-fp32 arithmetic, got invalid value at row-major element 0")))
	return scenarios
}
func TestLLVMMetalNumericFailures(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalFailures(t))
}
func TestLLVMLinuxMetalNumericFailures(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalFailures(t))
}

func llvmMetalXOR(t *testing.T) []llvmRuntimeScenario {
	text, err := os.ReadFile(filepath.Join(repository, "examples", "xor_metal.lo"))
	if err != nil {
		t.Fatal(err)
	}
	scenario := llvmMetalScenario(t, "500-step-XOR", string(text), false, assertXORResult)
	scenario.number = 3
	return []llvmRuntimeScenario{scenario}
}
func TestLLVMMetalXOR(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalXOR(t))
}
func TestLLVMLinuxMetalXOR(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalXOR(t))
}

func llvmMetalBoundaryFailures(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	body := `flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);effect=File.Write.new(File.Path.new("later"),Bytes.new("unexpected"));commit effect;return Record.new(kind:"unexpected",message:"")};return Main()`
	for _, fixture := range []struct {
		name, kind, message string
		number              int
	}{
		{"unsupported-before-allocation", "Device.UnsupportedOperation", "controlled compute admission failure", 4},
		{"profile-before-allocation", "Device.UnsupportedProfile", "Metal computation requires metal-fp32", 5},
		{"compute-command-cleanup", "Device.ExecutionFailed", "controlled compute command failure", 6},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: body, verify: nativeValuesCheck(3, nativeErrorKind(fixture.kind, "Tensor.mul: "+fixture.kind+": "+fixture.message)), harness: "testdata/native-runtime/metal-executor.c", number: fixture.number})
	}
	lost := `flow Stop(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Retry(e:Error)->Record{kind:String,message:String} onError Stop {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);return Record.new(kind:"unexpected",message:"")};flow Main()->Record{kind:String,message:String} onError Retry {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);return Record.new(kind:"unexpected",message:"")};return Main()`
	scenarios = append(scenarios, llvmRuntimeScenario{name: "sticky-compute-loss", source: lost, verify: nativeValuesCheck(3, nativeErrorKind("Device.Lost", "Tensor.to: Device.Lost: lost during computation")), harness: "testdata/native-runtime/metal-executor.c", number: 7})
	reverse := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {a=Tensor.to(Tensor.new(List.new(4.0),List.new()),Device.new("metal:0"));unused=Autodiff.grad(Record.new(x:a),Loss);effect=File.Write.new(File.Path.new("later"),Bytes.new("unexpected"));commit effect;return Record.new(kind:"unexpected",message:"")};return Main()`
	scenarios = append(scenarios, llvmRuntimeScenario{name: "reverse-command-cleanup", source: reverse, verify: nativeValuesCheck(3, nativeErrorKind("Device.ExecutionFailed", "Device.ExecutionFailed: reverse command failed")), harness: "testdata/native-runtime/metal-executor.c", number: 8})
	return scenarios
}
func TestLLVMMetalBoundaryFailures(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalBoundaryFailures(t))
}
func TestLLVMLinuxMetalBoundaryFailures(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalBoundaryFailures(t))
}

func llvmMetalCancellationScenarios() []llvmRuntimeScenario {
	const source = `flow Handle(e:Error)->Int{return 99};flow Main()->Int onError Handle {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);effect=File.Write.new(File.Path.new("later"),Bytes.new("unexpected"));commit effect;return 0};return Main()`
	return []llvmRuntimeScenario{{name: "cancel-during-compute", captureOutput: true, number: 9, source: source, harness: "testdata/native-runtime/metal-executor.c"}, {name: "lost-and-cancellation-during-compute", captureOutput: true, number: 10, source: source, harness: "testdata/native-runtime/metal-executor.c"}}
}
func TestLLVMMetalComputeCancellation(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalCancellationScenarios())
}
func TestLLVMLinuxMetalComputeCancellation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalCancellationScenarios())
}
