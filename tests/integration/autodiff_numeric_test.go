package integration_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

type derivativeFixture struct {
	name, body string
	a, b       []float64
	as, bs     []int
	ga, gb     []float64
}

func derivativeFixtures() []derivativeFixture {
	return []derivativeFixture{
		{"add-broadcast", `return Tensor.sum(Tensor.add(p.a,p.b),List.new(0,1))`, []float64{1, 2, 3, 4}, []float64{2, 3}, []int{2, 2}, []int{2}, []float64{1, 1, 1, 1}, []float64{2, 2}},
		{"subtract-broadcast", `return Tensor.sum(Tensor.sub(p.a,p.b),List.new(0,1))`, []float64{1, 2, 3, 4}, []float64{2, 3}, []int{2, 2}, []int{2}, []float64{1, 1, 1, 1}, []float64{-2, -2}},
		{"multiply-broadcast", `return Tensor.mean(Tensor.mul(p.a,p.b),List.new(0,1))`, []float64{1, 2, 3, 4}, []float64{2, 3}, []int{2, 2}, []int{2}, []float64{.5, .75, .5, .75}, []float64{1, 1.5}},
		{"divide-broadcast", `return Tensor.sum(Tensor.div(p.a,p.b),List.new(0,1))`, []float64{1, 2, 3, 4}, []float64{2, 4}, []int{2, 2}, []int{2}, []float64{.5, .25, .5, .25}, []float64{-1, -.375}},
		{"sqrt", `return Tensor.sum(Tensor.sqrt(p.a),List.new(0))`, []float64{1, 4, 9}, []float64{1}, []int{3}, []int{}, []float64{.5, .25, 1.0 / 6}, []float64{0}},
		{"relu", `return Tensor.sum(Tensor.relu(p.a),List.new(0))`, []float64{-2, 1, 3}, []float64{1}, []int{3}, []int{}, []float64{0, 1, 1}, []float64{0}},
		{"matmul", `return Tensor.sum(Tensor.matmul(p.a,p.b),List.new(0,1))`, []float64{1, 2, 3, 4, 5, 6}, []float64{.5, 1, .75, 1.25, 1.5, 2}, []int{2, 3}, []int{3, 2}, []float64{1.5, 2, 3.5, 1.5, 2, 3.5}, []float64{5, 5, 7, 7, 9, 9}},
		{"broadcast", `return Tensor.sum(Tensor.broadcast(p.a,List.new(2,3)),List.new(0,1))`, []float64{1, 2, 3}, []float64{1}, []int{1, 3}, []int{}, []float64{2, 2, 2}, []float64{0}},
		{"reshape", `r=Tensor.reshape(p.a,List.new(2,3)); return Tensor.sum(Tensor.mul(r,p.b),List.new(0,1))`, []float64{1, 2, 3, 4, 5, 6}, []float64{.5, 1, 1.5}, []int{6}, []int{3}, []float64{.5, 1, 1.5, .5, 1, 1.5}, []float64{5, 7, 9}},
		{"transpose", `r=Tensor.transpose(p.a,List.new(1,0)); return Tensor.sum(Tensor.mul(r,p.b),List.new(0,1))`, []float64{1, 2, 3, 4, 5, 6}, []float64{.5, 1.5}, []int{2, 3}, []int{2}, []float64{.5, .5, .5, 1.5, 1.5, 1.5}, []float64{6, 15}},
		{"sum", `r=Tensor.sum(p.a,List.new(0)); return Tensor.sum(Tensor.mul(r,p.b),List.new(0))`, []float64{1, 2, 3, 4, 5, 6}, []float64{.5, 1, 1.5}, []int{2, 3}, []int{3}, []float64{.5, 1, 1.5, .5, 1, 1.5}, []float64{5, 7, 9}},
		{"mean", `r=Tensor.mean(p.a,List.new(1)); return Tensor.sum(Tensor.mul(r,p.b),List.new(0))`, []float64{1, 2, 3, 4, 5, 6}, []float64{1.5, 3}, []int{2, 3}, []int{2}, []float64{.5, .5, .5, 1, 1, 1}, []float64{2, 5}},
		{"metadata", `r=Tensor.reshape(p.a,Tensor.shape(p.a)); return Tensor.sum(r,List.new(Math.sub(Tensor.rank(r),1)))`, []float64{2, 3}, []float64{1}, []int{2}, []int{}, []float64{1, 1}, []float64{0}},
	}
}
func floatList(v []float64) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.FormatFloat(n, 'g', -1, 64)
		if !strings.ContainsAny(parts[i], ".eE") {
			parts[i] += ".0"
		}
	}
	return "List.new(" + strings.Join(parts, ",") + ")"
}
func dimensionList(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return "List.new(" + strings.Join(parts, ",") + ")"
}
func (f derivativeFixture) source(expression string, a, b []float64) string {
	if strings.HasPrefix(expression, "Autodiff.grad(p,Loss).") {
		expression = "result.gradients." + strings.TrimPrefix(expression, "Autodiff.grad(p,Loss).gradients.")
		expression = "Autodiff.grad(p,Loss); return " + expression
	} else {
		expression = expression + "; return result"
	}
	return fmt.Sprintf(`flow Loss(p: Record{a: Tensor<FP32>, b: Tensor<FP32>}) -> Tensor<FP32>! { %s }
flow Run() -> Tensor<FP32>! {
 a=Tensor.new(%s,%s); b=Tensor.new(%s,%s); p=Record.new(a:a,b:b)
 result=%s
}; return Run()`, f.body, floatList(a), dimensionList(f.as), floatList(b), dimensionList(f.bs), expression)
}
func tensorNumbers(t *testing.T, v runtimeValue) []float64 {
	t.Helper()
	if v.Type != "Tensor<FP32>" {
		t.Fatalf("expected tensor, got %s", v.Type)
	}
	var tensor struct{ Values []string }
	if err := json.Unmarshal(v.Value, &tensor); err != nil {
		t.Fatal(err)
	}
	out := make([]float64, len(tensor.Values))
	for i, s := range tensor.Values {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = n
	}
	return out
}
func checkNumbers(t *testing.T, actual, expected []float64, atol, rtol float64) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("values %v, expected %v", actual, expected)
	}
	for i, x := range actual {
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x-expected[i]) > atol+rtol*math.Abs(expected[i]) {
			t.Fatalf("element %d = %.10g, expected %.10g (atol=%g, rtol=%g)", i, x, expected[i], atol, rtol)
		}
	}
}
func TestAutodiffAnalyticalAndFiniteDifferences(t *testing.T) {
	for _, fixture := range derivativeFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			for _, field := range []string{"a", "b"} {
				v := successfulValue(t, runValue(t, fixture.source("Autodiff.grad(p,Loss).gradients."+field, fixture.a, fixture.b)))
				actual := tensorNumbers(t, v)
				expected, inputs := fixture.ga, fixture.a
				if field == "b" {
					expected, inputs = fixture.gb, fixture.b
				}
				checkNumbers(t, actual, expected, 1e-6, 1e-5)
				finite := make([]float64, len(inputs))
				for i := range inputs {
					loss := func(delta float64) float64 {
						a, b := append([]float64{}, fixture.a...), append([]float64{}, fixture.b...)
						if field == "a" {
							a[i] += delta
						} else {
							b[i] += delta
						}
						return tensorNumbers(t, successfulValue(t, runValue(t, fixture.source("Loss(p)", a, b))))[0]
					}
					finite[i] = (loss(1e-3) - loss(-1e-3)) / (2e-3)
				}
				checkNumbers(t, actual, finite, 2e-3, 2e-2)
			}
		})
	}
}

func TestAutodiffEmptyAndNonsmoothRules(t *testing.T) {
	for _, tc := range []struct {
		source        string
		shape, values []string
	}{
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(Tensor.relu(p.x),List.new(0)) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(-0.0,0.0,-1.0),List.new(3)); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, []string{"3"}, []string{"0", "0", "0"}},
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(Tensor.broadcast(p.x,List.new(0)),List.new(0)) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new(1)); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, []string{"1"}, []string{"0"}},
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(p.x,List.new(0)) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(),List.new(0)); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, []string{"0"}, []string{}},
		{`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32> { return p.x }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, []string{}, []string{"1"}},
		{`flow Loss(c: Tensor<FP32>, p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { unused=Tensor.sqrt(p.x); return c }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(0.0),List.new()); r=Autodiff.grad(Record.new(x:x),x,Loss); return r.gradients.x }; return Run()`, []string{}, []string{"0"}},
	} {
		tensorResult(t, tc.source, tc.shape, tc.values)
	}
}

func TestAutodiffFailures(t *testing.T) {
	for _, tc := range []struct{ body, values, kind string }{
		{`return Tensor.sqrt(p.x)`, `0.0`, "Autodiff.UndefinedDerivative"},
		{`return Tensor.sqrt(p.x)`, `-1.0`, "Numeric.Domain"},
		{`a=Tensor.add(p.x,p.x); return Tensor.add(a,a)`, `3e38`, "Numeric.NonFinite"},
		{`return Tensor.div(p.x,p.x)`, `0.0`, "Numeric.DivideByZero"},
	} {
		source := fmt.Sprintf(`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { %s }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(%s),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, tc.body, tc.values)
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"`+tc.kind+`"`) || !strings.Contains(got.stderr, `"value":"Loss"`) {
			t.Fatalf("derivative failure %s: %+v", tc.kind, got)
		}
	}
}
