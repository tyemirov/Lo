package integration_test

import (
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMMetalTransferOnlyArithmetic(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
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

func llvmMetalScenario(t *testing.T, name, text string, view bool) llvmRuntimeScenario {
	t.Helper()
	_, _, expected := metalResult(t, text, metalComputeFactory())
	number := 0
	if view {
		number = 1
	}
	return llvmRuntimeScenario{name: name, source: text, expectedStdout: strings.Repeat(expected, 3), harness: "testdata/native-runtime/metal-executor.c", number: number}
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
		value, _, _ := metalResult(t, text, metalComputeFactory())
		checkNumbers(t, tensorNumbers(t, value), fixture.expected, 0, 0)
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, fixture.view))
	}
	return scenarios
}
func TestLLVMMetalPrimitives(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalPrimitives(t))
}
func TestLLVMLinuxMetalPrimitives(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
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
		value, _, _ := metalResult(t, text, metalComputeFactory())
		for i, number := range tensorNumbers(t, value) {
			if math.Float32bits(float32(number)) != fixture.bits[i] {
				t.Fatalf("%s oracle bits differ", fixture.name)
			}
		}
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, false))
	}
	return scenarios
}
func TestLLVMMetalNumericProfile(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalProfiles(t))
}
func TestLLVMLinuxMetalNumericProfile(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
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
			value, _, _ := metalResult(t, text, metalComputeFactory())
			expected := fixture.ga
			if field == "b" {
				expected = fixture.gb
			}
			checkNumbers(t, tensorNumbers(t, value), expected, 1e-6, 1e-5)
			scenarios = append(scenarios, llvmMetalScenario(t, fixture.name+"-"+field, text, false))
		}
	}
	return scenarios
}
func TestLLVMMetalDerivatives(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalDerivatives(t))
}
func TestLLVMLinuxMetalDerivatives(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
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
		value, _, _ := metalResult(t, text, metalComputeFactory())
		checkNumbers(t, tensorNumbers(t, recordField(t, value, "first")), fixture.first, 1e-6, 1e-5)
		checkNumbers(t, tensorNumbers(t, recordField(t, value, "second")), fixture.second, 1e-6, 1e-5)
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, false))
	}
	for _, kind := range []string{"Parameters", "SGD", "AdamW"} {
		text := checkpointTransferSource(kind)
		value, _, _ := metalResult(t, text, metalComputeFactory())
		if string(recordField(t, value, "before").Value) != string(recordField(t, value, "after").Value) {
			t.Fatal("checkpoint changed through device transfer")
		}
		scenarios = append(scenarios, llvmMetalScenario(t, "checkpoint-transfer-"+kind, text, true))
	}
	value, _, _ := metalResult(t, checkpointAdamWResume, metalComputeFactory())
	assertCheckpointAdamW(t, value)
	scenarios = append(scenarios, llvmMetalScenario(t, "checkpoint-resumed-AdamW", checkpointAdamWResume, false))
	return scenarios
}
func TestLLVMMetalOptimizersAndCheckpoints(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalOptimizers(t))
}
func TestLLVMLinuxMetalOptimizersAndCheckpoints(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalOptimizers(t))
}

func llvmMetalTraining(t *testing.T) []llvmRuntimeScenario {
	text := metalTrainingSource(t)
	value, _, _ := metalResult(t, text, metalComputeFactory())
	assertMetalTrainingResult(t, value)
	scenario := llvmMetalScenario(t, "200-step-training", text, false)
	scenario.number = 2
	return []llvmRuntimeScenario{scenario}
}
func TestLLVMMetalTraining(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalTraining(t))
}
func TestLLVMLinuxMetalTraining(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalTraining(t))
}

func llvmMetalFailures(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, fixture := range []struct{ name, setup, expression string }{
		{"divide-zero", `a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(0.0),List.new()),Device.new("metal:0"));`, `Tensor.div(a,b)`},
		{"normalized-zero-divisor", `a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(1e-45),List.new()),Device.new("metal:0"));`, `Tensor.div(a,b)`},
		{"sqrt-domain", `a=Tensor.to(Tensor.new(List.new(-1.0),List.new()),Device.new("metal:0"));`, `Tensor.sqrt(a)`},
		{"overflow", `a=Tensor.to(Tensor.new(List.new(1e20),List.new()),Device.new("metal:0"));`, `Tensor.mul(a,a)`},
	} {
		text := fmt.Sprintf(`flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {%s;unused=%s;return Record.new(kind:"unexpected",message:"")};return Main()`, fixture.setup, fixture.expression)
		scenarios = append(scenarios, llvmMetalScenario(t, fixture.name, text, false))
	}
	text := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {a=Tensor.to(Tensor.new(List.new(0.0),List.new()),Device.new("metal:0"));unused=Autodiff.grad(Record.new(x:a),Loss);return Record.new(kind:"unexpected",message:"")};return Main()`
	scenarios = append(scenarios, llvmMetalScenario(t, "undefined-derivative", text, false))
	return scenarios
}
func TestLLVMMetalNumericFailures(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalFailures(t))
}
func TestLLVMLinuxMetalNumericFailures(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalFailures(t))
}

func llvmMetalXOR(t *testing.T) []llvmRuntimeScenario {
	text, err := os.ReadFile(filepath.Join(repository, "examples", "xor_metal.lo"))
	if err != nil {
		t.Fatal(err)
	}
	value, _, _ := metalResult(t, string(text), metalComputeFactory())
	assertXORResult(t, value)
	scenario := llvmMetalScenario(t, "500-step-XOR", string(text), false)
	scenario.number = 3
	return []llvmRuntimeScenario{scenario}
}
func TestLLVMMetalXOR(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalXOR(t))
}
func TestLLVMLinuxMetalXOR(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalXOR(t))
}

func llvmMetalBoundaryFailures(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	body := `flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);effect=File.Write.new(File.Path.new("later"),Bytes.new("unexpected"));commit effect;return Record.new(kind:"unexpected",message:"")};return Main()`
	for _, fixture := range []struct {
		name   string
		number int
		model  *metalModel
	}{
		{"unsupported-before-allocation", 4, &metalModel{support: func(device.Operation) error { return errors.New("controlled compute admission failure") }}},
		{"profile-before-allocation", 5, &metalModel{profile: "wrong-profile"}},
		{"compute-command-cleanup", 6, &metalModel{before: func(device.Operation) error { return errors.New("controlled compute command failure") }}},
	} {
		_, _, expected := metalResult(t, body, fixture.model)
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: body, expectedStdout: strings.Repeat(expected, 3), harness: "testdata/native-runtime/metal-executor.c", number: fixture.number})
	}
	lost := `flow Stop(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Retry(e:Error)->Record{kind:String,message:String} onError Stop {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);return Record.new(kind:"unexpected",message:"")};flow Main()->Record{kind:String,message:String} onError Retry {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);return Record.new(kind:"unexpected",message:"")};return Main()`
	lostModel := &metalModel{before: func(device.Operation) error {
		fault, err := device.NewFault(device.Lost, errors.New("lost during computation"))
		if err != nil {
			t.Fatal(err)
		}
		return fault
	}}
	_, _, expected := metalResult(t, lost, lostModel)
	scenarios = append(scenarios, llvmRuntimeScenario{name: "sticky-compute-loss", source: lost, expectedStdout: strings.Repeat(expected, 3), harness: "testdata/native-runtime/metal-executor.c", number: 7})
	reverse := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Handle(e:Error)->Record{kind:String,message:String}{return Record.new(kind:e.kind,message:e.message)};flow Main()->Record{kind:String,message:String} onError Handle {a=Tensor.to(Tensor.new(List.new(4.0),List.new()),Device.new("metal:0"));unused=Autodiff.grad(Record.new(x:a),Loss);effect=File.Write.new(File.Path.new("later"),Bytes.new("unexpected"));commit effect;return Record.new(kind:"unexpected",message:"")};return Main()`
	reverseModel := &metalModel{before: func(operation device.Operation) error {
		if operation.Kind() == device.CheckSqrtGradient {
			return errors.New("reverse command failed")
		}
		return nil
	}}
	_, _, expected = metalResult(t, reverse, reverseModel)
	scenarios = append(scenarios, llvmRuntimeScenario{name: "reverse-command-cleanup", source: reverse, expectedStdout: strings.Repeat(expected, 3), harness: "testdata/native-runtime/metal-executor.c", number: 8})
	return scenarios
}
func TestLLVMMetalBoundaryFailures(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeScenarios(t, llvmMetalBoundaryFailures(t))
}
func TestLLVMLinuxMetalBoundaryFailures(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalBoundaryFailures(t))
}

func llvmMetalCancellationScenarios() []llvmRuntimeScenario {
	const source = `flow Handle(e:Error)->Int{return 99};flow Main()->Int onError Handle {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));unused=Tensor.mul(a,a);effect=File.Write.new(File.Path.new("later"),Bytes.new("unexpected"));commit effect;return 0};return Main()`
	return []llvmRuntimeScenario{{name: "cancel-during-compute", number: 9, source: source, harness: "testdata/native-runtime/metal-executor.c"}, {name: "lost-and-cancellation-during-compute", number: 10, source: source, harness: "testdata/native-runtime/metal-executor.c"}}
}
func TestLLVMMetalComputeCancellation(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmMetalCancellationScenarios())
}
func TestLLVMLinuxMetalComputeCancellation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmMetalCancellationScenarios())
}
