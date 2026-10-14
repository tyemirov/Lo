package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func recordField(t *testing.T, v runtimeValue, name string) runtimeValue {
	t.Helper()
	var fields map[string]runtimeValue
	if err := json.Unmarshal(v.Value, &fields); err != nil {
		t.Fatal(err)
	}
	f, ok := fields[name]
	if !ok {
		t.Fatalf("field %s absent in %s", name, v.Value)
	}
	return f
}
func TestOptimizerSGDTransitions(t *testing.T) {
	const program = `flow Run() -> Record{old: Optimizer.SGD.State<Record{x: Tensor<FP32>}>, first: Optimizer.SGD.State<Record{x: Tensor<FP32>}>, second: Optimizer.SGD.State<Record{x: Tensor<FP32>}>}! {
 x=Tensor.new(List.new(1.0,-2.0),List.new(2))
 p=Parameters.new(Record.new(x:x)); config=Optimizer.SGD.Config.new(0.1)
 original=Optimizer.SGD.State.new(p,config)
 g=Tensor.new(List.new(0.5,-1.0),List.new(2))
 gradients=Gradients.new(p,Record.new(x:g))
 first=Optimizer.SGD.step(original,gradients)
 second=Optimizer.SGD.step(first,gradients)
 return Record.new(old:original,first:first,second:second)
}; return Run()`
	v := successfulValue(t, runValue(t, program))
	for i, tc := range []struct {
		field    string
		expected []float64
	}{{"old", []float64{1, -2}}, {"first", []float64{.95, -1.9}}, {"second", []float64{.9, -1.8}}} {
		state := recordField(t, v, tc.field)
		p := recordField(t, state, "parameters")
		checkNumbers(t, tensorNumbers(t, recordField(t, p, "x")), tc.expected, 1e-6, 1e-5)
		valueJSON(t, recordField(t, state, "step"), fmt.Sprintf("%q", fmt.Sprint(i)))
	}
}

func TestOptimizerAdamWTransitions(t *testing.T) {
	const program = `flow Run() -> Record{old: Optimizer.AdamW.State<Record{x: Tensor<FP32>}>, first: Optimizer.AdamW.State<Record{x: Tensor<FP32>}>, second: Optimizer.AdamW.State<Record{x: Tensor<FP32>}>}! {
 x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x))
 config=Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)
 original=Optimizer.AdamW.State.new(p,config)
 g1=Tensor.new(List.new(0.1,-0.2),List.new(2)); g2=Tensor.new(List.new(0.0,0.3),List.new(2))
 first=Optimizer.AdamW.step(original,Gradients.new(p,Record.new(x:g1)))
 second=Optimizer.AdamW.step(first,Gradients.new(p,Record.new(x:g2)))
 return Record.new(old:original,first:first,second:second)
}; return Run()`
	v := successfulValue(t, runValue(t, program))
	for i, tc := range []struct {
		field    string
		expected []float64
	}{
		{"old", []float64{1, -2}},
		{"first", []float64{.9890000224113464, -1.9880000352859497}},
		{"second", []float64{.9813104271888733, -1.988489031791687}},
	} {
		state := recordField(t, v, tc.field)
		checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, state, "parameters"), "x")), tc.expected, 1e-6, 1e-5)
		valueJSON(t, recordField(t, state, "step"), fmt.Sprintf("%q", fmt.Sprint(i)))
	}
	old := recordField(t, v, "old")
	checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, old, "first_moment"), "x")), []float64{0, 0}, 0, 0)
	checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, old, "second_moment"), "x")), []float64{0, 0}, 0, 0)
	checkNumbers(t, tensorNumbers(t, recordField(t, old, "beta1_power")), []float64{1}, 0, 0)
	checkNumbers(t, tensorNumbers(t, recordField(t, old, "beta2_power")), []float64{1}, 0, 0)
	for _, tc := range []struct {
		field         string
		first, second []float64
	}{
		{"first", []float64{.010000002570450306, -.020000005140900612}, []float64{9.999871508625802e-6, 3.9999486034503207e-5}},
		{"second", []float64{.009000002406537533, .012000003829598427}, []float64{9.989871614379808e-6, .00012995832366868854}},
	} {
		s := recordField(t, v, tc.field)
		checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, s, "first_moment"), "x")), tc.first, 1e-6, 1e-5)
		checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, s, "second_moment"), "x")), tc.second, 1e-6, 1e-5)
	}
}

func TestOptimizerFoldAndZeroGradients(t *testing.T) {
	const program = `flow Update(g: Gradients<Record{x: Tensor<FP32>}>, state: Optimizer.SGD.State<Record{x: Tensor<FP32>}>, index: Int) -> Optimizer.SGD.State<Record{x: Tensor<FP32>}>! { return Optimizer.SGD.step(state,g) }
flow Run() -> Optimizer.SGD.State<Record{x: Tensor<FP32>}>! {
 x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); config=Optimizer.SGD.Config.new(0.25); state=Optimizer.SGD.State.new(p,config)
 one=Tensor.new(List.new(1.0),List.new()); g=Gradients.new(p,Record.new(x:one))
 return List.fold(List.new(0,1,2),state,g,Update)
}; return Run()`
	v := successfulValue(t, runValue(t, program))
	checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, v, "parameters"), "x")), []float64{1.25}, 0, 0)
	valueJSON(t, recordField(t, v, "step"), `"3"`)
	for _, betas := range []string{"0.0,0.0", "0.9,0.999"} {
		source := fmt.Sprintf(`flow Run() -> Optimizer.AdamW.State<Record{x: Tensor<FP32>}>! { x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x)); config=Optimizer.AdamW.Config.new(0.01,%s,1e-8,0.1); state=Optimizer.AdamW.State.new(p,config); return Optimizer.AdamW.step(state,Gradients.zero(p)) }; return Run()`, betas)
		s := successfulValue(t, runValue(t, source))
		checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, s, "parameters"), "x")), []float64{.999, -1.998}, 1e-6, 1e-5)
		checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, s, "first_moment"), "x")), []float64{0, 0}, 0, 0)
		checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, s, "second_moment"), "x")), []float64{0, 0}, 0, 0)
	}
}

func TestOptimizerSchemasAndFailureOrder(t *testing.T) {
	for _, optimizer := range []string{"SGD", "AdamW"} {
		config := "Optimizer.SGD.Config.new(2.0)"
		if optimizer == "AdamW" {
			config = "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)"
		}
		for _, tc := range []struct{ shape, gradient, kind string }{
			{"List.new(1)", "1.0", "Gradient.SchemaMismatch"},
			{"List.new()", "3e38", "Numeric.NonFinite"},
		} {
			source := fmt.Sprintf(`flow Run() -> Optimizer.%s.State<Record{x: Tensor<FP32>}>! { x=Tensor.new(List.new(1.0),List.new()); p=Parameters.new(Record.new(x:x)); state=Optimizer.%s.State.new(p,%s); g=Tensor.new(List.new(%s),%s); gp=Parameters.new(Record.new(x:g)); gradients=Gradients.new(gp,Record.new(x:g)); next=Optimizer.%s.step(state,gradients); effect=File.write(File.Path.new("never"),Bytes.new("bad")); commit effect; return next }; return Run()`, optimizer, optimizer, config, tc.gradient, tc.shape, optimizer)
			boundary := &observedBoundary{}
			_, err := evaluate(t, source, boundary)
			if err == nil || !strings.Contains(err.Error(), tc.kind) || boundary.commits != 0 {
				t.Fatalf("optimizer failure: %v commits=%d", err, boundary.commits)
			}
		}
	}
	for _, field := range []string{"config", "first_moment", "beta1_power"} {
		source := fmt.Sprintf(`flow Private(s: Optimizer.AdamW.State<Record{x: Tensor<FP32>}>) -> Int { hidden=s.%s; return 0 }`, field)
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "check.unknown-field") {
			t.Fatalf("private state field: %+v", got)
		}
	}
}

func TestOptimizerPrimitiveWorkCharges(t *testing.T) {
	for _, tc := range []struct {
		optimizer, config string
		delta             uint64
	}{
		{"SGD", "Optimizer.SGD.Config.new(0.1)", 36},
		{"AdamW", "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)", 180},
	} {
		program := func(n int) string {
			return fmt.Sprintf(`flow Run() -> Optimizer.%s.State<Record{x: Tensor<FP32>}>! { one=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(one,List.new(%d)); p=Parameters.new(Record.new(x:x)); state=Optimizer.%s.State.new(p,%s); return Optimizer.%s.step(state,Gradients.zero(p)) }; return Run()`, tc.optimizer, n, tc.optimizer, tc.config, tc.optimizer)
		}
		if delta := minimumTensorSteps(t, program(10)) - minimumTensorSteps(t, program(1)); delta != tc.delta {
			t.Fatalf("%s work delta=%d, want %d", tc.optimizer, delta, tc.delta)
		}
	}
}

func TestOptimizerInvalidConfigurations(t *testing.T) {
	for _, expression := range []string{
		`Optimizer.SGD.Config.new(0.0)`, `Optimizer.SGD.Config.new(-1.0)`, `Optimizer.SGD.Config.new(1e-60)`, `Optimizer.SGD.Config.new(1e40)`,
		`Optimizer.AdamW.Config.new(0.01,1.0,0.9,1e-8,0.0)`,
		`Optimizer.AdamW.Config.new(0.01,0.999999999,0.9,1e-8,0.0)`,
		`Optimizer.AdamW.Config.new(0.01,0.9,-0.1,1e-8,0.0)`,
		`Optimizer.AdamW.Config.new(0.01,0.9,0.9,0.0,0.0)`,
		`Optimizer.AdamW.Config.new(0.01,0.9,0.9,1e-60,0.0)`,
		`Optimizer.AdamW.Config.new(0.01,0.9,0.9,1e-8,-0.1)`,
		`Optimizer.AdamW.Config.new(1e30,0.9,0.9,1e-8,1e30)`,
	} {
		got := run(t, "run", sourceFile(t, "return "+expression), "--format", "json")
		if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"Optimizer.InvalidConfig"`) {
			t.Fatalf("invalid optimizer config: %+v", got)
		}
	}
}
