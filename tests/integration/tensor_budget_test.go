package integration_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func checkedTensor(t *testing.T, text string) ir.Program {
	t.Helper()
	s, err := source.New("tensor-budget.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	p, d := compiler.Check(s)
	if d != nil {
		t.Fatal(d)
	}
	return p
}
func minimumTensorSteps(t *testing.T, text string) uint64 {
	t.Helper()
	p := checkedTensor(t, text)
	lo, hi := uint64(1), uint64(10000)
	for lo < hi {
		mid := (lo + hi) / 2
		budget, err := execution.NewBudget(mid)
		if err != nil {
			t.Fatal(err)
		}
		_, err = eval.Run(context.Background(), p, &observedBoundary{}, budget, device.DefaultConfig())
		if err == nil {
			hi = mid
			continue
		}
		var host execution.HostError
		if !errors.As(err, &host) || host.Code() != "runtime.limit" {
			t.Fatal(err)
		}
		lo = mid + 1
	}
	return lo
}
func TestTensorWorkCharges(t *testing.T) {
	for _, tc := range []struct {
		operation string
		delta     uint64
	}{
		{"Tensor.relu(a)", 18}, // Broadcast plus elementwise work.
		{"Tensor.reshape(a,List.new(n))", 9},
		{"Tensor.sum(a,List.new(0))", 18},
		{"Tensor.mean(a,List.new(0))", 18},
		{"Tensor.sum(a,List.new())", 18},
		{"Tensor.mean(a,List.new())", 27},
		{"Tensor.values(a)", 18},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			typ := "Tensor<FP32>"
			if tc.operation == "Tensor.values(a)" {
				typ = "List<Float>"
			}
			program := func(n int) string {
				return fmt.Sprintf(`flow Work(n: Int) -> %s! { seed=Tensor.new(List.new(1.0),List.new()); a=Tensor.broadcast(seed,List.new(n)); return %s }; return Work(%d)`, typ, tc.operation, n)
			}
			delta := minimumTensorSteps(t, program(10)) - minimumTensorSteps(t, program(1))
			if delta != tc.delta {
				t.Fatalf("work delta=%d, want %d", delta, tc.delta)
			}
		})
	}
	program := func(n int) string {
		return fmt.Sprintf(`flow Work(n: Int) -> Tensor<FP32>! { seed=Tensor.new(List.new(1.0),List.new()); a=Tensor.broadcast(seed,List.new(2,n)); b=Tensor.broadcast(seed,List.new(n,3)); return Tensor.matmul(a,b) }; return Work(%d)`, n)
	}
	// Two broadcasts add 5 steps per k; the six dot products add 12 per k.
	if delta := minimumTensorSteps(t, program(10)) - minimumTensorSteps(t, program(1)); delta != 153 {
		t.Fatalf("matmul work delta=%d, want 153", delta)
	}
}

type tensorCancellation struct {
	context.Context
	calls  atomic.Int64
	cancel context.CancelFunc
}

func (c *tensorCancellation) Err() error {
	if c.calls.Add(1) == 256 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestTensorKernelCancellation(t *testing.T) {
	p := checkedTensor(t, `flow Handle(e: Error) -> Int { return 99 }
flow Work() -> Int onError Handle { seed=Tensor.new(List.new(1.0),List.new()); a=Tensor.broadcast(seed,List.new(128,128)); product=Tensor.matmul(a,a); return 0 }; return Work()`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &tensorCancellation{Context: ctx, cancel: cancel}
	budget, err := execution.NewBudget(10000000)
	if err != nil {
		t.Fatal(err)
	}
	v, err := eval.Run(controlled, p, &observedBoundary{}, budget, device.DefaultConfig())
	var host execution.HostError
	if v != nil || !errors.As(err, &host) || host.Code() != "runtime.cancelled" || !errors.Is(err, context.Canceled) {
		t.Fatalf("kernel cancellation entered handler: value=%v, error=%v", v, err)
	}
}

func TestTensorFailureStopsCommit(t *testing.T) {
	b := &observedBoundary{}
	_, err := evaluate(t, `flow Fail() -> Tensor<FP32>! { a=Tensor.new(List.new(-1.0),List.new()); b=Tensor.sqrt(a); target=File.write(File.Path.new("after"),Bytes.new("wrong")); commit target; return b }; return Fail()`, b)
	var failure execution.Failure
	if !errors.As(err, &failure) || failure.Value().Kind() != "Numeric.Domain" || b.commits != 0 {
		t.Fatalf("failure/commit order: error=%v commits=%d", err, b.commits)
	}
}
