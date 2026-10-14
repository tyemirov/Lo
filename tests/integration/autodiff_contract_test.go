package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
)

func TestAutodiffInspection(t *testing.T) {
	const source = `flow Square(x: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.mul(x,x) }
flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { a=Square(p.x); b=Square(p.x); return Tensor.add(a,b) }
flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`
	got := run(t, "inspect", sourceFile(t, source), "--format", "json")
	if got.status != 0 {
		t.Fatalf("inspect: %+v", got)
	}
	var report struct {
		Nodes []struct {
			Operation  string
			Derivative *struct {
				Nodes []struct {
					ID          int
					Operation   string
					Reverse     bool
					Span        json.RawMessage
					Invocations []int
					Edges       []struct{ Operand, Node int }
				}
			}
		}
		UnusedFlows []string `json:"unused_flows"`
	}
	var envelope struct{ Report json.RawMessage }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Report, &report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range report.Nodes {
		if node.Operation != "Autodiff.grad" {
			continue
		}
		found = true
		if node.Derivative == nil {
			t.Fatal("missing derivative plan")
		}
		seen := map[int]bool{}
		for _, n := range node.Derivative.Nodes {
			if n.Operation != "Tensor.mul" {
				continue
			}
			if !n.Reverse || len(n.Edges) != 2 || n.Edges[0].Operand != 0 || n.Edges[1].Operand != 1 || n.Edges[0].Node != n.Edges[1].Node || len(n.Invocations) != 2 || string(n.Span) == "null" {
				t.Fatalf("invalid shared-node source map: %+v", n)
			}
			seen[n.Invocations[1]] = true
		}
		if len(seen) != 2 {
			t.Fatalf("helper invocations collapsed: %v", seen)
		}
	}
	if !found || len(report.UnusedFlows) != 0 {
		t.Fatalf("gradient call or flow use absent: %+v", report)
	}
}

func TestAutodiffLossValidation(t *testing.T) {
	for _, tc := range []struct{ source, code string }{
		{`flow Loss(p: Record{x: Int}) -> Int { return p.x }; return Autodiff.grad(Record.new(x:1),Loss)`, "check.type-constraint"},
		{`flow Loss(p: Record{}) -> Int { return 1 }; return Autodiff.grad(Record.new(),Loss)`, "check.type-constraint"},
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Int { return 1 }; flow Run() -> Int! { x=Tensor.new(List.new(1.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return 0 }; return Run()`, "check.autodiff-loss"},
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32> { return p.x }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(1.0,2.0),List.new(2)); r=Autodiff.grad(Record.new(x:x),Loss); return r.value }; return Run()`, "check.autodiff-loss"},
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32> { return p.x }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(1.0),List.new()); r=Autodiff.grad(Record.new(y:x),Loss); return r.value }; return Run()`, "check.type-mismatch"},
	} {
		got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"code":"`+tc.code+`"`) {
			t.Fatalf("invalid loss: %+v", got)
		}
	}
	source := `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32> { return p.x }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(1.0,2.0),List.new(Math.add(1,1))); r=Autodiff.grad(Record.new(x:x),Loss); return r.value }; return Run()`
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"Autodiff.InvalidLoss"`) {
		t.Fatalf("dynamic loss rank: %+v", got)
	}
}

func TestAutodiffTransitiveRejections(t *testing.T) {
	for _, helper := range []string{
		`flow Helper(x: Tensor<FP32>) -> Tensor<FP32>! { data=File.read(File.Path.new("never")); return x }`,
		`flow Helper(x: Tensor<FP32>) -> Tensor<FP32>! { effect=File.write(File.Path.new("never"),Bytes.new("bad")); commit effect; return x }`,
		`flow Item(x: Int) -> Int { return x }; flow Helper(x: Tensor<FP32>) -> Tensor<FP32> { ignored=List.map(List.new(1),Item); return x }`,
		`flow Inner(p: Record{x: Tensor<FP32>}) -> Tensor<FP32> { return p.x }; flow Helper(x: Tensor<FP32>) -> Tensor<FP32>! { r=Autodiff.grad(Record.new(x:x),Inner); return r.value }`,
		`flow Handle(e: Error) -> Int { return 0 }; flow Helper(x: Tensor<FP32>) -> Int onError Handle { root=Tensor.sqrt(x); return 0 }`,
	} {
		source := helper + `; flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { unused=Helper(p.x); return p.x }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(1.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.value }; return Run()`
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"code":"check.autodiff-unsupported"`) {
			t.Fatalf("transitive rejection: %+v", got)
		}
	}
}

func TestAutodiffFailureStopsCommitAndCanBeHandled(t *testing.T) {
	const prelude = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sqrt(p.x) }
flow Handle(e: Error) -> String { return e.kind }
flow Work() -> String onError Handle {
 x=Tensor.new(List.new(0.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss)
 effect=File.write(File.Path.new("after"),Bytes.new("bad")); commit effect
 return "unexpected"
}; return Work()`
	boundary := &observedBoundary{}
	v, err := evaluate(t, prelude, boundary)
	if err != nil || boundary.commits != 0 {
		t.Fatalf("gradient failure order: %v, commits=%d", err, boundary.commits)
	}
	encoded, err := json.Marshal(v)
	if err != nil || !strings.Contains(string(encoded), "Autodiff.UndefinedDerivative") {
		t.Fatalf("outer handler: %s %v", encoded, err)
	}
}

func TestAutodiffWorkAndCancellation(t *testing.T) {
	program := func(n int) string {
		return fmt.Sprintf(`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(Tensor.mul(p.x,p.x),List.new(0)) }; flow Run() -> Tensor<FP32>! { one=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(one,List.new(%d)); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, n)
	}
	// Nine extra elements: input broadcast, forward mul/sum, zero accumulator,
	// reduction broadcast, two VJP multiplies, and two accumulator additions.
	if delta := minimumTensorSteps(t, program(10)) - minimumTensorSteps(t, program(1)); delta != 81 {
		t.Fatalf("derivative work delta=%d, want 81", delta)
	}
	p := checkedTensor(t, program(100000))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &tensorCancellation{Context: ctx, cancel: cancel}
	budget, err := eval.NewBudget(10000000)
	if err != nil {
		t.Fatal(err)
	}
	v, err := eval.Run(controlled, p, &observedBoundary{}, budget)
	var host eval.HostError
	if v != nil || !errors.As(err, &host) || host.Code() != "runtime.cancelled" {
		t.Fatalf("derivative cancellation: %v %v", v, err)
	}
	// Reuse the same checked plan after cancellation with a fresh invocation.
	if _, err = eval.Run(context.Background(), p, &observedBoundary{}, budget); err != nil {
		t.Fatal(err)
	}
}

func TestAutodiffExpansionLimit(t *testing.T) {
	var source strings.Builder
	source.WriteString(`flow Level0(x: Tensor<FP32>) -> Tensor<FP32> { return x };`)
	for i := 1; i <= 18; i++ {
		fmt.Fprintf(&source, `flow Level%d(x: Tensor<FP32>) -> Tensor<FP32> { a=Level%d(x); return Level%d(a) };`, i, i-1, i-1)
	}
	source.WriteString(`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32> { return Level18(p.x) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(1.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.value }; return Run()`)
	got := run(t, "check", sourceFile(t, source.String()), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"code":"check.autodiff-limit"`) {
		t.Fatalf("unbounded derivative expansion: %+v", got)
	}
}

func TestAutodiffReverseFailureSource(t *testing.T) {
	const source = `flow Divide(c: Tensor<FP32>, x: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.div(x,c) }
flow Loss(c: Tensor<FP32>, p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Divide(c,p.x) }
flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(0.0),List.new()); c=Tensor.new(List.new(1e-40),List.new()); r=Autodiff.grad(Record.new(x:x),c,Loss); return r.gradients.x }; return Run()`
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"Numeric.NonFinite"`) || !strings.Contains(got.stderr, `"value":"Divide"`) || !strings.Contains(got.stderr, `"value":"Tensor.div"`) {
		t.Fatalf("reverse failure provenance: %+v", got)
	}
}

func TestAutodiffDiagnosticContextBudget(t *testing.T) {
	var source strings.Builder
	source.WriteString(`flow Counter(x: Int) -> Record{value: Int, next_state: Int} { return Record.new(value:x,next_state:Math.add(x,1)) };
flow Helper(context: List<Int>, x: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.relu(x) };
flow Loss(context: List<Int>, p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { v0=p.x;`)
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&source, "v%d=Helper(context,v%d);", i, i-1)
	}
	source.WriteString(`return v200 }; flow Run() -> Tensor<FP32>! { context=List.unfold(0,20000,Counter); x=Tensor.new(List.new(1.0),List.new()); r=Autodiff.grad(Record.new(x:x),context,Loss); return r.value }; return Run()`)
	got := run(t, "run", sourceFile(t, source.String()), "--format", "json", "--max-steps", "10000000")
	if got.status != 2 || !strings.Contains(got.stderr, "evaluation value bytes") || got.stdout != "" {
		t.Fatalf("unbounded derivative context: status=%d, stdout bytes=%d, stderr bytes=%d", got.status, len(got.stdout), len(got.stderr))
	}
}
