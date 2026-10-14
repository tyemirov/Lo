package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestParametersGradientLifecycle(t *testing.T) {
	const program = `flow Run() -> Tensor<FP32>! {
 x=Tensor.new(List.new(2.0,3.0),List.new(2))
 parameters=Parameters.new(Record.new(x:x))
 original=Gradients.new(parameters,Record.new(x:x))
 zero=Gradients.zero(parameters)
 added=Gradients.add(original,original)
 scaled=Gradients.scale(added,0.25)
 reset=Gradients.zero(parameters)
 values=%s
 return values.x
}; return Run()`
	for _, tc := range []struct {
		expression string
		expected   []string
	}{
		{"Parameters.values(parameters)", []string{"2", "3"}},
		{"Gradients.values(original)", []string{"2", "3"}},
		{"Gradients.values(zero)", []string{"0", "0"}},
		{"Gradients.values(added)", []string{"4", "6"}},
		{"Gradients.values(scaled)", []string{"1", "1.5"}},
		{"Gradients.values(reset)", []string{"0", "0"}},
	} {
		tensorResult(t, fmt.Sprintf(program, tc.expression), []string{"2"}, tc.expected)
	}
}

func TestParametersStopAndIdentity(t *testing.T) {
	const program = `flow Loss(p: Record{a: Tensor<FP32>, b: Tensor<FP32>}) -> Tensor<FP32>! {
 tied=Tensor.mul(p.a,p.a)
 stopped=Autodiff.stop(p.b)
 return Tensor.sum(Tensor.add(tied,stopped),List.new(0))
}
flow Run() -> Tensor<FP32>! {
 x=Tensor.new(List.new(2.0,3.0),List.new(2))
 parameters=Parameters.new(Record.new(a:x,b:x))
 values=Parameters.values(parameters)
 result=Autodiff.grad(values,Loss)
 return %s
}; return Run()`
	for _, tc := range []struct {
		expression      string
		shape, expected []string
	}{
		{"result.value", []string{}, []string{"18"}},
		{"result.gradients.a", []string{"2"}, []string{"4", "6"}},
		{"result.gradients.b", []string{"2"}, []string{"0", "0"}},
	} {
		tensorResult(t, fmt.Sprintf(program, tc.expression), tc.shape, tc.expected)
	}
	tensorResult(t, `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sqrt(Autodiff.stop(p.x)) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(0.0),List.new()); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`, []string{}, []string{"0"})
}

func TestParametersSchemaFailures(t *testing.T) {
	for _, source := range []string{
		`return Parameters.new(Record.new())`,
		`return Parameters.new(Record.new(x:1))`,
		`flow Bad(p: Parameters<Record{x: Int}>) -> Int { return 0 }`,
		`flow Bad(p: Parameters) -> Int { return 0 }`,
		`flow Run() -> Int! { x=Tensor.new(List.new(1.0),List.new()); p=Parameters.new(Record.new(x:x)); g=Gradients.new(p,Record.new(y:x)); return 0 }; return Run()`,
		`flow Run() -> Int! { x=Tensor.new(List.new(1.0),List.new()); p=Parameters.new(Record.new(x:x,x:x)); return 0 }; return Run()`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 {
			t.Fatalf("invalid parameter schema: %+v", got)
		}
	}
	for _, body := range []string{
		`p=Parameters.new(Record.new(x:a)); return Gradients.new(p,Record.new(x:b))`,
		`p=Parameters.new(Record.new(x:a)); q=Parameters.new(Record.new(x:b)); g=Gradients.zero(p); h=Gradients.zero(q); return Gradients.add(g,h)`,
	} {
		source := `flow Run() -> Gradients<Record{x: Tensor<FP32>}>! { a=Tensor.new(List.new(1.0),List.new()); b=Tensor.new(List.new(1.0),List.new(1)); ` + body + ` }; return Run()`
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"kind":"Gradient.SchemaMismatch"`) || got.stdout != "" {
			t.Fatalf("gradient metadata mismatch: %+v", got)
		}
	}
}

func TestParametersWeightedMicrobatchesAndFold(t *testing.T) {
	const program = `flow Loss(batch: Tensor<FP32>, p: Record{weight: Tensor<FP32>}) -> Tensor<FP32>! {
 predictions=Tensor.mul(batch,p.weight)
 return Tensor.mean(Tensor.mul(predictions,predictions),List.new(0))
}
flow Accumulate(p: Parameters<Record{weight: Tensor<FP32>}>, count: Float, prior: Gradients<Record{weight: Tensor<FP32>}>, batch: Tensor<FP32>) -> Gradients<Record{weight: Tensor<FP32>}>! {
 values=Parameters.values(p)
 result=Autodiff.grad(values,batch,Loss)
 gradients=Gradients.new(p,result.gradients)
 elements=Tensor.values(batch)
 size=Int.to_float(List.length(elements))
 factor=Float.div(size,count)
 weighted=Gradients.scale(gradients,factor)
 return Gradients.add(prior,weighted)
}
flow Run() -> Tensor<FP32>! {
 weight=Tensor.new(List.new(2.0),List.new())
 parameters=Parameters.new(Record.new(weight:weight))
 first=Tensor.new(List.new(1.0),List.new(1))
 second=Tensor.new(List.new(2.0,4.0),List.new(2))
 complete=Tensor.new(List.new(1.0,2.0,4.0),List.new(3))
 zero=Gradients.zero(parameters)
 accumulated=List.fold(List.new(first,second),zero,parameters,3.0,Accumulate)
 accumulated_values=Gradients.values(accumulated)
 old_values=Gradients.values(zero)
 values=Parameters.values(parameters)
 combined=Autodiff.grad(values,complete,Loss)
 return %s
}; return Run()`
	accumulated := tensorNumbers(t, successfulValue(t, runValue(t, fmt.Sprintf(program, "accumulated_values.weight"))))
	combined := tensorNumbers(t, successfulValue(t, runValue(t, fmt.Sprintf(program, "combined.gradients.weight"))))
	checkNumbers(t, combined, []float64{28}, 1e-6, 1e-5)
	checkNumbers(t, accumulated, combined, 1e-6, 1e-5)
	tensorResult(t, fmt.Sprintf(program, "old_values.weight"), []string{}, []string{"0"})
}

func TestParametersScaleFailuresAndMetadataSnapshots(t *testing.T) {
	for _, factor := range []string{"1e40", "2.0"} {
		source := fmt.Sprintf(`flow Run() -> Gradients<Record{x: Tensor<FP32>}>! { x=Tensor.new(List.new(3e38),List.new()); p=Parameters.new(Record.new(x:x)); g=Gradients.new(p,Record.new(x:x)); return Gradients.scale(g,%s) }; return Run()`, factor)
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"Numeric.NonFinite"`) || !strings.Contains(got.stderr, `"tensor_type"`) || strings.Contains(got.stderr, `"values"`) {
			t.Fatalf("gradient failure snapshot: %+v", got)
		}
	}
}
