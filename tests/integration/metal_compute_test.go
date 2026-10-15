package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

func metalComputeFactory() device.Factory { return &metalModel{} }

func metalResult(t *testing.T, text string, factory device.Factory) (runtimeValue, []device.Receipt, string) {
	t.Helper()
	got, receipts := runWithDevices(t, context.Background(), text, factory, "--allow-device", "metal:0")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("Metal execution: %+v", got)
	}
	var envelope struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	assertReleased(t, receipts)
	return successfulValue(t, envelope.Result), receipts, got.stdout
}

func TestMetalComputePrimitives(t *testing.T) { metalPrimitives(t, metalComputeFactory) }
func metalPrimitives(t *testing.T, factory func() device.Factory) {
	for _, tc := range []struct {
		name, body string
		expected   []float64
	}{
		{"broadcast add", `return Tensor.add(a,b)`, []float64{3, 6, 5, 8, 7, 10}},
		{"subtract", `return Tensor.sub(a,b)`, []float64{-1, -2, 1, 0, 3, 2}},
		{"multiply", `return Tensor.mul(a,b)`, []float64{2, 8, 6, 16, 10, 24}},
		{"divide", `return Tensor.div(a,b)`, []float64{.5, .5, 1.5, 1, 2.5, 1.5}},
		{"sqrt", `return Tensor.sqrt(Tensor.mul(a,a))`, []float64{1, 2, 3, 4, 5, 6}},
		{"relu", `return Tensor.relu(Tensor.sub(a,b))`, []float64{0, 0, 1, 0, 3, 2}},
		{"transpose", `return Tensor.transpose(a,List.new(1,0))`, []float64{1, 3, 5, 2, 4, 6}},
		{"reshape", `return Tensor.reshape(a,List.new(6))`, []float64{1, 2, 3, 4, 5, 6}},
		{"broadcast", `return Tensor.broadcast(b,List.new(3,2))`, []float64{2, 4, 2, 4, 2, 4}},
		{"sum", `return Tensor.sum(a,List.new(0))`, []float64{9, 12}},
		{"mean", `return Tensor.mean(a,List.new(1))`, []float64{1.5, 3.5, 5.5}},
		{"matmul non-square", `return Tensor.matmul(a,Tensor.reshape(a,List.new(2,3)))`, []float64{9, 12, 15, 19, 26, 33, 29, 40, 51}},
		{"matmul rectangular output", `return Tensor.matmul(a,Tensor.reshape(b,List.new(2,1)))`, []float64{10, 22, 34}},
		{"nonadjacent reduction axes", `return Tensor.sum(Tensor.reshape(a,List.new(1,3,2)),List.new(0,2))`, []float64{3, 7, 11}},
		{"rank-three transpose", `return Tensor.transpose(Tensor.reshape(a,List.new(1,3,2)),List.new(2,0,1))`, []float64{1, 3, 5, 2, 4, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := fmt.Sprintf(`flow Compute(a:Tensor<FP32>,b:Tensor<FP32>)->Tensor<FP32>!{%s}
flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0),List.new(3,2)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(2.0,4.0),List.new(2)),Device.new("metal:0"));return Tensor.to(Compute(a,b),Device.new("cpu"))};return Main()`, tc.body)
			var previous string
			for repeat := 0; repeat < 3; repeat++ {
				v, receipts, serialized := metalResult(t, text, factory())
				checkNumbers(t, tensorNumbers(t, v), tc.expected, 1e-6, 1e-5)
				if repeat > 0 && serialized != previous {
					t.Fatal("numeric bits changed between runs")
				}
				previous = serialized
				transfers := 0
				for _, r := range receipts {
					if r.Event == device.EventTransfer {
						transfers++
					}
				}
				if transfers != 3 {
					t.Fatalf("implicit transfer: %d", transfers)
				}
			}
		})
	}
}

func TestMetalComputeDerivatives(t *testing.T) { metalDerivatives(t, metalComputeFactory) }
func metalDerivatives(t *testing.T, factory func() device.Factory) {
	fixtures := append(derivativeFixtures(), derivativeFixture{"shared and stopped", `r=Tensor.mul(p.a,p.a);twice=Tensor.add(r,r);stopped=Autodiff.stop(p.b);return Tensor.sum(Tensor.add(twice,stopped),List.new(0))`, []float64{1, 2}, []float64{3, 4}, []int{2}, []int{2}, []float64{4, 8}, []float64{0, 0}})
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			for _, field := range []string{"a", "b"} {
				text := fixture.source("Autodiff.grad(p,Loss).gradients."+field, fixture.a, fixture.b)
				text = strings.Replace(text, "p=Record.new(a:a,b:b)", `ma=Tensor.to(a,Device.new("metal:0"));mb=Tensor.to(b,Device.new("metal:0"));p=Record.new(a:ma,b:mb)`, 1)
				text = strings.Replace(text, "return result.gradients."+field, `return Tensor.to(result.gradients.`+field+`,Device.new("cpu"))`, 1)
				v, receipts, _ := metalResult(t, text, factory())
				expected := fixture.ga
				if field == "b" {
					expected = fixture.gb
				}
				checkNumbers(t, tensorNumbers(t, v), expected, 1e-6, 1e-5)
				transfers := 0
				for _, r := range receipts {
					if r.Event == device.EventTransfer {
						transfers++
					}
				}
				if transfers != 3 {
					t.Fatalf("implicit derivative transfer: %d", transfers)
				}
			}
		})
	}
}

func TestMetalComputeOptimizers(t *testing.T) { metalOptimizers(t, metalComputeFactory) }
func metalOptimizers(t *testing.T, factory func() device.Factory) {
	for _, tc := range []struct {
		name, config  string
		first, second []float64
	}{
		{"SGD", "Optimizer.SGD.Config.new(0.1)", []float64{.99, -1.98}, []float64{.99, -2.01}},
		{"AdamW", "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)", []float64{.9890000224113464, -1.9880000352859497}, []float64{.9813104271888733, -1.988489031791687}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := fmt.Sprintf(`flow Main()->Record{old:Tensor<FP32>,first:Tensor<FP32>,second:Tensor<FP32>,step:Int}!{
   x=Tensor.to(Tensor.new(List.new(1.0,-2.0),List.new(2)),Device.new("metal:0"));p=Parameters.new(Record.new(x:x));original=Optimizer.%s.State.new(p,%s)
   g1=Tensor.to(Tensor.new(List.new(0.1,-0.2),List.new(2)),Device.new("metal:0"));g2=Tensor.to(Tensor.new(List.new(0.0,0.3),List.new(2)),Device.new("metal:0"))
   first=Optimizer.%s.step(original,Gradients.new(p,Record.new(x:g1)));second=Optimizer.%s.step(first,Gradients.new(p,Record.new(x:g2)))
   old_values=Parameters.values(original.parameters);first_values=Parameters.values(first.parameters);second_values=Parameters.values(second.parameters)
   return Record.new(old:Tensor.to(old_values.x,Device.new("cpu")),first:Tensor.to(first_values.x,Device.new("cpu")),second:Tensor.to(second_values.x,Device.new("cpu")),step:second.step)
  };return Main()`, tc.name, tc.config, tc.name, tc.name)
			var previous string
			for repeat := 0; repeat < 3; repeat++ {
				v, receipts, serialized := metalResult(t, text, factory())
				checkNumbers(t, tensorNumbers(t, recordField(t, v, "old")), []float64{1, -2}, 0, 0)
				checkNumbers(t, tensorNumbers(t, recordField(t, v, "first")), tc.first, 1e-6, 1e-5)
				checkNumbers(t, tensorNumbers(t, recordField(t, v, "second")), tc.second, 1e-6, 1e-5)
				valueJSON(t, recordField(t, v, "step"), `"2"`)
				if repeat > 0 && previous != serialized {
					t.Fatal("optimizer bits changed between runs")
				}
				previous = serialized
				transfers := 0
				for _, r := range receipts {
					if r.Event == device.EventTransfer {
						transfers++
					}
				}
				if transfers != 6 {
					t.Fatalf("implicit optimizer transfer: %d", transfers)
				}
			}
		})
	}
	text := `flow Main()->Record{old:Tensor<FP32>,scaled:Tensor<FP32>}!{x=Tensor.to(Tensor.new(List.new(2.0,-4.0),List.new(2)),Device.new("metal:0"));p=Parameters.new(Record.new(x:x));g=Gradients.new(p,Record.new(x:x));zero=Gradients.zero(p);added=Gradients.add(g,zero);scaled=Gradients.scale(added,0.5);v=Gradients.values(scaled);return Record.new(old:Tensor.to(x,Device.new("cpu")),scaled:Tensor.to(v.x,Device.new("cpu")))};return Main()`
	v, _, _ := metalResult(t, text, factory())
	checkNumbers(t, tensorNumbers(t, recordField(t, v, "old")), []float64{2, -4}, 0, 0)
	checkNumbers(t, tensorNumbers(t, recordField(t, v, "scaled")), []float64{1, -2}, 0, 0)
}

func TestMetalComputeProfile(t *testing.T) { metalNumericProfile(t, metalComputeFactory) }
func metalNumericProfile(t *testing.T, factory func() device.Factory) {
	for _, tc := range []struct {
		name, a, b, op string
		bits           []uint32
	}{
		{"normalization add", "-0.0,1e-45,-1e-45", "0.0", "Tensor.add(a,b)", []uint32{0, 0, 0}},
		{"underflow multiply", "1.1754943508222875e-38,-1.1754943508222875e-38", "0.5", "Tensor.mul(a,b)", []uint32{0, 0}},
		{"signed zero divide", "-0.0,1e-45,-1e-45", "-1.0", "Tensor.div(a,b)", []uint32{0, 0, 0}},
		{"sqrt subnormals", "-0.0,1e-45,-1e-45", "1.0", "Tensor.sqrt(a)", []uint32{0, 0, 0}},
		{"layout keeps bits", "-0.0,1.401298464324817e-45,-1.401298464324817e-45", "1.0", "Tensor.transpose(a,List.new(0))", []uint32{0x80000000, 1, 0x80000001}},
		{"fixed reduction", "16777216.0,1.0,-16777216.0", "1.0", "Tensor.sum(a,List.new(0))", []uint32{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := strings.Count(tc.a, ",") + 1
			text := fmt.Sprintf(`flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(%s),List.new(%d)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(%s),List.new()),Device.new("metal:0"));return Tensor.to(%s,Device.new("cpu"))};return Main()`, tc.a, count, tc.b, tc.op)
			var previous string
			for repeat := 0; repeat < 3; repeat++ {
				v, _, serialized := metalResult(t, text, factory())
				numbers := tensorNumbers(t, v)
				for i, n := range numbers {
					if actual := math.Float32bits(float32(n)); actual != tc.bits[i] {
						t.Fatalf("bits[%d]=%08x want %08x", i, actual, tc.bits[i])
					}
				}
				if repeat > 0 && previous != serialized {
					t.Fatal("profile changed between runs")
				}
				previous = serialized
			}
		})
	}
	for _, tc := range []struct {
		name, source string
		want         []float64
	}{
		{"empty sum", `a=Tensor.to(Tensor.new(List.new(),List.new(2,0)),Device.new("metal:0"));return Tensor.to(Tensor.sum(a,List.new(1)),Device.new("cpu"))`, []float64{0, 0}},
		{"empty matmul", `a=Tensor.to(Tensor.new(List.new(),List.new(2,0)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(),List.new(0,3)),Device.new("metal:0"));return Tensor.to(Tensor.matmul(a,b),Device.new("cpu"))`, []float64{0, 0, 0, 0, 0, 0}},
		{"empty broadcast", `a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));return Tensor.to(Tensor.broadcast(a,List.new(0,3)),Device.new("cpu"))`, []float64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, _, _ := metalResult(t, `flow Main()->Tensor<FP32>!{`+tc.source+`};return Main()`, factory())
			checkNumbers(t, tensorNumbers(t, v), tc.want, 0, 0)
		})
	}
}

func metalTrainingSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "examples", "training_cpu.lo"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, name := range []string{"inputs", "targets", "w1", "b1", "w2", "b2"} {
		pattern := regexp.MustCompile(`(?m)^  ` + name + ` = (Tensor.new\([^\n]+\))$`)
		text = pattern.ReplaceAllString(text, `  `+name+` = Tensor.to($1, Device.new("metal:0"))`)
	}
	text = strings.ReplaceAll(text, "Tensor.to_float(first_loss)", `Tensor.to_float(Tensor.to(first_loss,Device.new("cpu")))`)
	text = strings.ReplaceAll(text, "Tensor.to_float(final_loss)", `Tensor.to_float(Tensor.to(final_loss,Device.new("cpu")))`)
	var observations strings.Builder
	for _, name := range []string{"b1", "b2", "w1", "w2"} {
		fmt.Fprintf(&observations, "  observed_%s = Tensor.to(final_values.%s,Device.new(\"cpu\"))\n", name, name)
		text = strings.ReplaceAll(text, name+": final_values."+name, name+": observed_"+name)
	}
	text = strings.Replace(text, "  observed = Record.new", observations.String()+"  observed = Record.new", 1)
	return text
}
func TestMetalComputeTraining(t *testing.T) { metalTraining(t, metalComputeFactory) }
func metalTraining(t *testing.T, factory func() device.Factory) {
	source := metalTrainingSource(t)
	var previous string
	for repeat := 0; repeat < 3; repeat++ {
		v, receipts, serialized := metalResult(t, source, factory())
		var lossText string
		if err := json.Unmarshal(recordField(t, v, "loss").Value, &lossText); err != nil {
			t.Fatal(err)
		}
		loss, err := strconv.ParseFloat(lossText, 64)
		if err != nil {
			t.Fatal(err)
		}
		checkNumbers(t, []float64{loss}, []float64{.00013215476064942777}, 1e-8, 1e-5)
		if loss >= .001 {
			t.Fatalf("training loss %g", loss)
		}
		parameters := recordField(t, v, "parameters")
		for _, tc := range []struct {
			name     string
			expected []float64
		}{
			{"w1", []float64{1.1966239213943481, -.045796893537044525, .08913600444793701, 1.388328194618225}},
			{"b1", []float64{.19662371277809143, .38832828402519226}},
			{"w2", []float64{.7451153993606567, 1.0836396217346191}},
			{"b2", []float64{.021668776869773865}},
		} {
			checkNumbers(t, tensorNumbers(t, recordField(t, parameters, tc.name)), tc.expected, 1e-6, 1e-5)
		}

		if repeat > 0 && serialized != previous {
			t.Fatal("training bits changed between runs")
		}
		previous = serialized
		transfers, computes := 0, 0
		var peak uint64
		for _, r := range receipts {
			peak = max(peak, r.Live)
			if r.Event == device.EventTransfer {
				transfers++
			}
			if r.Event == device.EventCompute {
				computes++
			}
		}
		if transfers != 12 || computes < 200 || peak > 65536 {
			t.Fatalf("training lifetime: transfers=%d computes=%d peak=%d", transfers, computes, peak)
		}
		t.Logf("run=%d steps=200 loss=%.12g transfers=%d computes=%d peak_reserved_bytes=%d", repeat+1, loss, transfers, computes, peak)
	}
}

func TestMetalComputeNumericFailures(t *testing.T) { metalNumericFailures(t, metalComputeFactory) }
func metalNumericFailures(t *testing.T, factory func() device.Factory) {
	for _, tc := range []struct{ name, a, b, expression, kind string }{
		{"division subnormal divisor", "1.0", "1e-45", "Tensor.div(a,b)", "Numeric.DivideByZero"},
		{"negative square root", "-1.0", "1.0", "Tensor.sqrt(a)", "Numeric.Domain"},
		{"overflow", "3e38", "3e38", "Tensor.mul(a,b)", "Numeric.NonFinite"},
		{"ordered element failure", "1.0,3e38", "0.0,1e-38", "Tensor.div(a,b)", "Numeric.DivideByZero"},
		{"empty mean", "", "", "Tensor.mean(a,List.new(0))", "Tensor.EmptyReduction"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			if tc.a != "" {
				count = strings.Count(tc.a, ",") + 1
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "must-not-exist")
			text := fmt.Sprintf(`flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(%s),List.new(%d)),Device.new("metal:0"));b=Tensor.to(Tensor.new(List.new(%s),List.new(%d)),Device.new("metal:0"));result=%s;effect=File.write(File.Path.new(%q),Bytes.new("bad"));commit effect;return Tensor.to(result,Device.new("cpu"))};return Main()`, tc.a, count, tc.b, count, tc.expression, path)
			got, receipts := runWithDevices(t, context.Background(), text, factory(), "--allow-device", "metal:0", "--write-root", dir)
			if got.status != 1 || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" {
				t.Fatalf("numeric failure: %+v", got)
			}
			if _, err := os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("later commit: %v", err)
			}
			assertReleased(t, receipts)
		})
	}
	for _, input := range []string{"0.0", "1e-45", "-1e-45"} {
		text := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Main()->Tensor<FP32>!{x=Tensor.to(Tensor.new(List.new(` + input + `),List.new()),Device.new("metal:0"));r=Autodiff.grad(Record.new(x:x),Loss);return Tensor.to(r.gradients.x,Device.new("cpu"))};return Main()`
		got, receipts := runWithDevices(t, context.Background(), text, factory(), "--allow-device", "metal:0")
		if got.status != 1 || !strings.Contains(got.stderr, "Autodiff.UndefinedDerivative") || !strings.Contains(got.stderr, `"value":"Loss"`) {
			t.Fatalf("sqrt derivative: %+v", got)
		}
		assertReleased(t, receipts)
	}
}

func TestMetalComputeControlledFailures(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		status     int
	}{
		{"command", device.ExecutionFailed, 1}, {"lost", device.Lost, 1}, {"cancel", "runtime.cancelled", 2}, {"profile", device.UnsupportedProfile, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			model := &metalModel{}
			if tc.name == "profile" {
				model.profile = "unsupported-profile"
			} else {
				model.before = func(device.Operation) error {
					if tc.name == "cancel" {
						cancel()
						return errors.New("command failed during cancellation")
					}
					failure, err := device.NewFault(tc.kind, errors.New("injected command failure"))
					if err != nil {
						t.Fatal(err)
					}
					return failure
				}
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "must-not-exist")
			text := fmt.Sprintf(`flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(2.0),List.new()),Device.new("metal:0"));b=Tensor.mul(a,a);effect=File.write(File.Path.new(%q),Bytes.new("bad"));commit effect;return Tensor.to(b,Device.new("cpu"))};return Main()`, path)
			got, receipts := runWithDevices(t, ctx, text, model, "--allow-device", "metal:0", "--write-root", dir)
			if got.status != tc.status || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" {
				t.Fatalf("compute failure: %+v", got)
			}
			if _, err := os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("later commit: %v", err)
			}
			if tc.name == "profile" && len(model.operations) != 0 {
				t.Fatal("unsupported profile submitted work")
			}
			if tc.status == 1 && !strings.Contains(got.stderr, `"value":"Tensor.mul"`) {
				t.Fatalf("source operation missing: %s", got.stderr)
			}
			transfers := 0
			for _, r := range receipts {
				if r.Event == device.EventTransfer {
					transfers++
				}
			}
			if transfers != 1 {
				t.Fatalf("diagnostic downloaded tensor: %d", transfers)
			}
			assertReleased(t, receipts)
		})
	}
}

func TestMetalComputeReverseFailureTrace(t *testing.T) {
	model := &metalModel{before: func(op device.Operation) error {
		if op.Kind() == device.CheckSqrtGradient {
			return errors.New("reverse command failed")
		}
		return nil
	}}
	text := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Main()->Tensor<FP32>!{x=Tensor.to(Tensor.new(List.new(4.0),List.new()),Device.new("metal:0"));r=Autodiff.grad(Record.new(x:x),Loss);return Tensor.to(r.gradients.x,Device.new("cpu"))};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, model, "--allow-device", "metal:0")
	for _, fragment := range []string{device.ExecutionFailed, `"value":"Tensor.sqrt"`, `"value":"Loss"`, `"span"`} {
		if !strings.Contains(got.stderr, fragment) {
			t.Fatalf("missing reverse trace %s: %+v", fragment, got)
		}
	}
	if got.status != 1 || got.stdout != "" {
		t.Fatalf("reverse failure: %+v", got)
	}
	assertReleased(t, receipts)
}

func TestMetalComputeLostSession(t *testing.T) {
	model := &metalModel{before: func(device.Operation) error {
		fault, err := device.NewFault(device.Lost, errors.New("lost during computation"))
		if err != nil {
			t.Fatal(err)
		}
		return fault
	}}
	text := `flow Stop(error:Error)->String{return error.kind}
flow Retry(error:Error)->String onError Stop {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));b=Tensor.mul(a,a);return "unexpected"}
flow Main()->String onError Retry {a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));b=Tensor.mul(a,a);return "unexpected"};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, model, "--allow-device", "metal:0")
	if got.status != 0 || !strings.Contains(got.stdout, device.Lost) || len(model.operations) != 1 {
		t.Fatalf("lost session resubmitted: %+v operations=%v", got, model.operations)
	}
	transfers := 0
	for _, r := range receipts {
		if r.Event == device.EventTransfer {
			transfers++
		}
	}
	if transfers != 1 {
		t.Fatalf("lost session transferred again: %d", transfers)
	}
	assertReleased(t, receipts)
}

func TestMetalComputeStaticInspection(t *testing.T) {
	text := `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));return Tensor.mul(a,a)};return Main()`
	got := run(t, "inspect", sourceFile(t, text), "--format", "json")
	if got.status != 0 {
		t.Fatalf("inspect: %+v", got)
	}
	for _, fragment := range []string{`"profile":"metal-fp32"`, `"execution":"synchronous"`, device.ExecutionFailed, device.UnsupportedProfile, device.Lost} {
		if !strings.Contains(got.stdout, fragment) {
			t.Fatalf("missing static compute metadata %s: %s", fragment, got.stdout)
		}
	}
}

func TestMetalComputeViewTapeLifetime(t *testing.T) {
	var body strings.Builder
	body.WriteString(`flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{v0=p.x;`)
	for i := 1; i <= 10; i++ {
		shape := "25,20"
		if i%2 == 0 {
			shape = "500"
		}
		fmt.Fprintf(&body, "v%d=Tensor.reshape(v%d,List.new(%s));", i, i-1, shape)
	}
	body.WriteString(`return Tensor.sum(v10,List.new(0))};flow Main()->Tensor<FP32>!{seed=Tensor.new(List.new(1.0),List.new());source=Tensor.broadcast(seed,List.new(500));x=Tensor.to(source,Device.new("metal:0"));r=Autodiff.grad(Record.new(x:x),Loss);total=Tensor.sum(r.gradients.x,List.new(0));return Tensor.to(total,Device.new("cpu"))};return Main()`)
	v, receipts, _ := metalResult(t, body.String(), &metalModel{reservation: func(index int, bytes uint64) uint64 {
		if index == 1 {
			return 4 << 20
		}
		return bytes * 2
	}})
	checkNumbers(t, tensorNumbers(t, v), []float64{500}, 0, 0)
	var peak uint64
	for _, r := range receipts {
		peak = max(peak, r.Live)
	}
	if peak > 5<<20 {
		t.Fatalf("views duplicated storage: %d", peak)
	}
}

func TestMetalComputeAdmissionAndBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		status     int
		configure  func(*metalModel)
	}{
		{"unsupported operation", device.UnsupportedOperation, 1, func(m *metalModel) {
			m.support = func(device.Operation) error { return errors.New("unsupported primitive") }
		}},
		{"output allocation", device.AllocationFailed, 1, func(m *metalModel) {
			m.allocationFailure = func(index int, _ uint64) error {
				if index == 2 {
					return errors.New("output allocation failed")
				}
				return nil
			}
		}},
		{"status allocation", device.AllocationFailed, 1, func(m *metalModel) {
			m.allocationFailure = func(index int, _ uint64) error {
				if index == 3 {
					return errors.New("status allocation failed")
				}
				return nil
			}
		}},
		{"workspace reservation", "runtime.limit", 2, func(m *metalModel) {
			m.reservation = func(index int, bytes uint64) uint64 {
				if index == 3 {
					return device.MaxWorkspace + 4
				}
				return bytes * 2
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &metalModel{}
			tc.configure(model)
			text := `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));return Tensor.to(Tensor.mul(a,a),Device.new("cpu"))};return Main()`
			got, receipts := runWithDevices(t, context.Background(), text, model, "--allow-device", "metal:0")
			if got.status != tc.status || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" || len(model.operations) != 0 {
				t.Fatalf("admission failed: %+v submitted=%v", got, model.operations)
			}
			if tc.name == "unsupported operation" && model.allocations != 1 {
				t.Fatal("unsupported operation allocated output")
			}
			assertReleased(t, receipts)
		})
	}
}

func TestMetalComputeExample(t *testing.T) {
	text, err := os.ReadFile(filepath.Join(repository, "examples", "metal_arithmetic.lo"))
	if err != nil {
		t.Fatal(err)
	}
	v, _, _ := metalResult(t, string(text), metalComputeFactory())
	checkNumbers(t, tensorNumbers(t, recordField(t, v, "before")), []float64{1, -2}, 0, 0)
	checkNumbers(t, tensorNumbers(t, recordField(t, v, "after")), []float64{.8, -1.6}, 1e-6, 1e-5)
	valueJSON(t, recordField(t, v, "loss"), `"5"`)
}
