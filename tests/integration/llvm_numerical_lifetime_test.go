package integration_test

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestLLVMNumericalLifetime(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmNumericalLifetimeScenarios())
}

func TestLLVMNumericalLifetimeLinux(t *testing.T) {
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		t.Skip("covered by host lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmNumericalLifetimeScenarios())
}

func llvmNumericalLifetimeScenarios() []llvmRuntimeScenario {
	const square = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.mul(p.x,p.x) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`
	const undefined = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sqrt(Tensor.mul(p.x,p.x)) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(0.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`
	const cancelled = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(Tensor.matmul(p.x,p.x),List.new(0,1)) }; flow Run() -> Tensor<FP32>! { scalar=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(scalar,List.new(128,128)); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`
	const optimizer = `flow Run() -> Optimizer.AdamW.State<Record{x: Tensor<FP32>}>! { x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x)); config=Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1); state=Optimizer.AdamW.State.new(p,config); return Optimizer.AdamW.step(state,Gradients.zero(p)) }; return Run()`
	const shared = `flow Run() -> Record{a: Tensor<FP32>, b: Tensor<FP32>}! { x=Tensor.new(List.new(1.0),List.new()); y=Tensor.reshape(x,List.new(1)); return Record.new(a:x,b:y) }; return Run()`
	cases := []llvmRuntimeScenario{
		{name: "success-retained-alias", source: square, number: 0},
		{name: "recoverable-after-tape", source: undefined, number: 1},
		{name: "budget-after-tape", source: square, number: 2},
		{name: "cancellation-after-tape", source: cancelled, number: 3},
		{name: "optimizer-classes", source: optimizer, number: 4},
		{name: "shared-storage", source: shared, number: 5},
	}
	for i := range cases {
		cases[i].harness = "testdata/native-runtime/numerical.c"
	}
	return cases
}

func TestLLVMRejectsForeignTensorStorage(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmForeignTensorStorageScenarios())
}
func TestLLVMRejectsForeignTensorStorageLinux(t *testing.T) {
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		t.Skip("covered by host lane")
	}
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmForeignTensorStorageScenarios())
}
func llvmForeignTensorStorageScenarios() []llvmRuntimeScenario {
	scenarios := []llvmRuntimeScenario{
		{name: "record-parameter-adoption", number: 6, source: `flow Run() -> Record{x: Tensor<FP32>}! { x=Tensor.new(List.new(1.0),List.new()); r=Record.new(x:x); p=Parameters.new(r); return r }; return Run()`, transform: foreignTensorBridge("lo_op_Parameters_2Enew")},
		{name: "nominal-checkpoint-input", number: 7, source: `flow Run() -> Parameters<Record{x: Tensor<FP32>}>! { x=Tensor.new(List.new(1.0),List.new()); p=Parameters.new(Record.new(x:x)); unused=Checkpoint.encode(p); return p }; return Run()`, transform: foreignTensorBridge("lo_op_Checkpoint_2Eencode")},
	}
	for i := range scenarios {
		scenarios[i].harness = "testdata/native-runtime/numerical.c"
	}
	return scenarios
}
func foreignTensorBridge(symbol string) func(*testing.T, []byte) []byte {
	return func(t *testing.T, module []byte) []byte {
		t.Helper()
		pattern := regexp.MustCompile(`call void @` + symbol + `\(ptr [^,]+, ptr [^,]+, i64 1, i64 ([0-9]+), ptr [^)]+\)`)
		match := pattern.FindSubmatch(module)
		if match == nil {
			t.Fatalf("missing %s invocation", symbol)
		}
		unwrappers := regexp.MustCompile(`call void @lo_convert_propagate\(ptr [^,]+, ptr [^,]+, i64 1, i64 ([0-9]+), ptr [^)]+\)`).FindAllSubmatch(module, -1)
		if len(unwrappers) == 0 {
			t.Fatal("missing fallible return normalization")
		}
		unwrapSite := unwrappers[len(unwrappers)-1][1]
		bridge := fmt.Sprintf(`
define void @lo_numerical_foreign(ptr %%c, ptr %%args, ptr %%out) {
 %%temporary = alloca { i32, i32, ptr }
 call void @lo_convert_propagate(ptr %%c, ptr %%args, i64 1, i64 %s, ptr %%temporary)
 %%status = load i32, ptr %%temporary
 %%ok = icmp eq i32 %%status, 0
 br i1 %%ok, label %%dispatch, label %%failure
dispatch:
 %%vp = getelementptr { i32, i32, ptr }, ptr %%temporary, i32 0, i32 2
 %%v = load ptr, ptr %%vp
 %%one = alloca ptr
 store ptr %%v, ptr %%one
 call void @%s(ptr %%c, ptr %%one, i64 1, i64 %s, ptr %%out)
 call void @lo_value_release(ptr %%v)
 ret void
failure:
 %%result = load { i32, i32, ptr }, ptr %%temporary
 store { i32, i32, ptr } %%result, ptr %%out
 ret void
}
`, unwrapSite, symbol, match[1])
		return append(module, []byte(bridge)...)

	}
}

func llvmTapeBudgetSource() string {
	var body strings.Builder
	body.WriteString(`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { `)
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&body, "a%d=Tensor.broadcast(p.x,List.new(1000000)); ", i)
	}
	body.WriteString(`b0=Tensor.mul(a0,a1); b1=Tensor.mul(a2,a3); b2=Tensor.mul(a4,a5); b3=Tensor.mul(a6,a7); b4=Tensor.mul(a8,p.x); return Tensor.sum(Tensor.add(Tensor.add(b0,b1),Tensor.add(Tensor.add(b2,b3),b4)),List.new(0)) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(1.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`)
	return body.String()
}

func TestLLVMNumericalTapeBudgetBoundary(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires explicit LLVM lane")
	}
	source := sourceFile(t, llvmTapeBudgetSource())
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build: %+v", built)
	}
	expected := runExecutable(t, binary, "run", source, "--format", "json", "--max-steps", "100000000")
	actual := runExecutable(t, artifact, "run", "--format", "json", "--max-steps", "100000000")
	if expected.status != 2 || !strings.Contains(expected.stderr, "boundary operation failed: execution limit: evaluation value bytes") {
		t.Fatalf("evaluator tape boundary: %+v", expected)
	}
	if actual != expected {
		t.Fatalf("tape boundary native %+v evaluator %+v", actual, expected)
	}
	scenarios := []llvmRuntimeScenario{{name: "tape-class-logical-boundary-cleanup", source: llvmTapeBudgetSource(), number: 8, harness: "testdata/native-runtime/numerical.c"}}
	runLLVMRuntimeScenarios(t, scenarios)
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		runLLVMRuntimeTargetScenarios(t, "linux/arm64", scenarios)
	}
}
