package integration_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

func minimumLLVMWorkSteps(t *testing.T, text string) uint64 {
	t.Helper()
	path := sourceFile(t, text)
	artifact, built := buildForTest(t, path)
	if built.status != 0 || built.stderr != "" {
		t.Fatalf("build LLVM work fixture: %+v", built)
	}
	invoke := func(steps uint64) result {
		return runExecutable(t, artifact, "run", "--format", "json", "--max-steps", strconv.FormatUint(steps, 10))
	}
	admitted := func(got result) bool {
		t.Helper()
		if got.status == 0 {
			if got.stderr != "" {
				t.Fatalf("successful LLVM work has diagnostics: %+v", got)
			}
			return true
		}
		if got.status != 2 || got.stdout != "" {
			t.Fatalf("unexpected LLVM work outcome: %+v", got)
		}
		report := diagnostics(t, got.stderr)
		if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.limit" {
			t.Fatalf("unexpected LLVM work denial: %+v", got)
		}
		return false
	}
	lower, upper := uint64(1), uint64(10000)
	if !admitted(invoke(upper)) {
		t.Fatal("LLVM work fixture exceeds its declared search range")
	}
	for lower < upper {
		middle := lower + (upper-lower)/2
		if admitted(invoke(middle)) {
			upper = middle
		} else {
			lower = middle + 1
		}
	}
	for _, steps := range []uint64{lower - 1, lower, lower + 1} {
		budget := strconv.FormatUint(steps, 10)
		expected := runExecutable(t, binary, "run", path, "--format", "json", "--max-steps", budget)
		actual := invoke(steps)
		if actual != expected {
			t.Fatalf("public work boundary %d: LLVM %+v, evaluator %+v", steps, actual, expected)
		}
		if admitted(actual) != (steps >= lower) {
			t.Fatalf("compiled work minimum %d has inconsistent boundary %d", lower, steps)
		}
	}
	return lower
}

func TestLLVMNumericalWorkCharges(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	type workFixture struct {
		name    string
		program func(int) string
		delta   uint64
	}
	fixtures := []workFixture{}
	for _, operation := range []struct {
		name, expression, output string
		delta                    uint64
	}{
		{"relu", "Tensor.relu(a)", "Tensor<FP32>", 18},
		{"reshape", "Tensor.reshape(a,List.new(n))", "Tensor<FP32>", 9},
		{"sum", "Tensor.sum(a,List.new(0))", "Tensor<FP32>", 18},
		{"mean", "Tensor.mean(a,List.new(0))", "Tensor<FP32>", 18},
		{"sum-no-axes", "Tensor.sum(a,List.new())", "Tensor<FP32>", 18},
		{"mean-no-axes", "Tensor.mean(a,List.new())", "Tensor<FP32>", 27},
		{"values", "Tensor.values(a)", "List<Float>", 18},
	} {
		fixtures = append(fixtures, workFixture{operation.name, func(size int) string {
			return fmt.Sprintf(`flow Work(n: Int) -> %s! { seed=Tensor.new(List.new(1.0),List.new()); a=Tensor.broadcast(seed,List.new(n)); return %s }; return Work(%d)`, operation.output, operation.expression, size)
		}, operation.delta})
	}
	fixtures = append(fixtures, workFixture{"matmul", func(size int) string {
		return fmt.Sprintf(`flow Work(n: Int) -> Tensor<FP32>! { seed=Tensor.new(List.new(1.0),List.new()); a=Tensor.broadcast(seed,List.new(2,n)); b=Tensor.broadcast(seed,List.new(n,3)); return Tensor.matmul(a,b) }; return Work(%d)`, size)
	}, 153}, workFixture{"derivative-shared-edges", func(size int) string {
		return fmt.Sprintf(`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(Tensor.mul(p.x,p.x),List.new(0)) }; flow Run() -> Tensor<FP32>! { one=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(one,List.new(%d)); result=Autodiff.grad(Record.new(x:x),Loss); return result.gradients.x }; return Run()`, size)
	}, 81})
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			small := minimumLLVMWorkSteps(t, fixture.program(1))
			large := minimumLLVMWorkSteps(t, fixture.program(10))
			if large < small || large-small != fixture.delta {
				t.Fatalf("compiled work delta: small=%d large=%d, want delta %d", small, large, fixture.delta)
			}
			t.Logf("compiled minimum steps: n=1:%d n=10:%d delta:%d", small, large, large-small)
		})
	}
}
