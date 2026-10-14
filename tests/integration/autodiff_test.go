package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestAutodiffSharedEdgesAndContext(t *testing.T) {
	const program = `
flow Square(x: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.mul(x,x) }
flow Loss(context: Tensor<FP32>, p: Record{x: Tensor<FP32>, unused: Tensor<FP32>}) -> Tensor<FP32>! {
  first = Square(p.x)
  second = Square(p.x)
  return Tensor.sum(Tensor.add(Tensor.add(first,second),context), List.new(0))
}
flow Run() -> Tensor<FP32>! {
  x = Tensor.new(List.new(2.0,3.0),List.new(2))
  p = Record.new(x:x, unused:x)
  first = Autodiff.grad(p,x,Loss)
  second = Autodiff.grad(p,x,Loss)
  return %s
}
return Run()`
	for _, tc := range []struct {
		expr          string
		shape, values []string
	}{
		{"first.value", []string{}, []string{"31"}},
		{"first.gradients.x", []string{"2"}, []string{"8", "12"}},
		{"first.gradients.unused", []string{"2"}, []string{"0", "0"}},
		{"second.gradients.x", []string{"2"}, []string{"8", "12"}},
		{"p.x", []string{"2"}, []string{"2", "3"}},
	} {
		t.Run(tc.expr, func(t *testing.T) { tensorResult(t, fmt.Sprintf(program, tc.expr), tc.shape, tc.values) })
	}
}

func TestAutodiffRejectsUnsupportedGraphs(t *testing.T) {
	for _, body := range []string{
		`unused = Tensor.values(p.x); return Tensor.sum(p.x,List.new(0))`,
		`unused = Tensor.new(List.new(1.0),List.new()); return Tensor.sum(p.x,List.new(0))`,
		`return Match.new(true, true: Tensor.sum(p.x,List.new(0)), _: Tensor.sum(p.x,List.new(0)))`,
		`unused = File.read(File.Path.new("never-read")); return Tensor.sum(p.x,List.new(0))`,
		`unused = Float.add(1.0,2.0); return Tensor.sum(p.x,List.new(0))`,
	} {
		source := `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { ` + body + ` }
flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new(1)); r=Autodiff.grad(Record.new(x:x),Loss); return r.value }; return Run()`
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"code":"check.autodiff-unsupported"`) {
			t.Fatalf("unsupported loss graph: %+v", got)
		}
		if diagnostics(t, got.stderr).Diagnostics[0].Span == nil {
			t.Fatal("missing source span")
		}
	}
}
