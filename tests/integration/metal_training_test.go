package integration_test

import (
	"encoding/json"

	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/targetconfig"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestMetalTrainingCLI(t *testing.T) {
	path := filepath.Join(repository, "examples", "training_metal.lo")
	for _, command := range []string{"parse", "check", "inspect"} {
		got := run(t, command, path, "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("training %s: %+v", command, got)
		}
		if command == "inspect" {
			for _, fragment := range []string{`"metal:0"`, `"profile":"metal-fp32"`, `"execution":"synchronous"`, "Autodiff.grad", "Optimizer.SGD.step", "Device.Unavailable", "Numeric.NonFinite"} {
				if !strings.Contains(got.stdout, fragment) {
					t.Fatalf("missing training inspection %s: %s", fragment, got.stdout)
				}
			}
		}
	}
	for _, tc := range []struct {
		name, diagnostic string
		status           int
		args             []string
	}{
		{"denied", "runtime.capability-denied", 2, nil},
		{"unavailable", "Device.Unavailable", 1, []string{"--allow-device", "metal:0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"run", path, "--format", "json"}, tc.args...)
			got := runProgram(t, args[1], args[0], args[2:]...)
			if got.status != tc.status || got.stdout != "" || !strings.Contains(got.stderr, tc.diagnostic) {
				t.Fatalf("training admission: %+v", got)
			}
		})
	}
}

const trainingParameterType = "Record{b1: Tensor<FP32>, b2: Tensor<FP32>, w1: Tensor<FP32>, w2: Tensor<FP32>}"

type trainingReference struct {
	InitialLoss      float64   `json:"initial_loss"`
	InitialGradients []float64 `json:"initial_gradients_w1_b1_w2_b2"`
	FirstParameters  []float64 `json:"first_parameters_w1_b1_w2_b2"`
	FinalLoss        float64   `json:"final_loss"`
	FinalParameters  []float64 `json:"final_parameters_w1_b1_w2_b2"`
}

func trainingReferences(t *testing.T) []trainingReference {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "docs", "evidence", "training-reference-results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var references struct {
		Binary64 trainingReference `json:"binary64_reference"`
		FP32     trainingReference `json:"fp32_reference"`
	}
	if err := json.Unmarshal(data, &references); err != nil {
		t.Fatal(err)
	}
	return []trainingReference{references.Binary64, references.FP32}
}

func metalTrainingSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "examples", "training_metal.lo"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func trainingFloat(t *testing.T, value runtimeValue) float64 {
	t.Helper()
	var text string
	if err := json.Unmarshal(value.Value, &text); err != nil {
		t.Fatal(err)
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		t.Fatalf("nonfinite training value: %s (%v)", text, err)
	}
	return number
}

func trainingParameters(t *testing.T, value runtimeValue) []float64 {
	t.Helper()
	var fields map[string]runtimeValue
	if err := json.Unmarshal(value.Value, &fields); err != nil || len(fields) != 4 {
		t.Fatalf("training fields: %s (%v)", value.Value, err)
	}
	var numbers []float64
	for _, field := range []struct {
		name  string
		shape []string
	}{{"w1", []string{"2", "2"}}, {"b1", []string{"2"}}, {"w2", []string{"2", "1"}}, {"b2", []string{"1"}}} {
		tensor := recordField(t, value, field.name)
		var metadata struct {
			Dtype, Device string
			Shape         []string
		}
		if err := json.Unmarshal(tensor.Value, &metadata); err != nil || metadata.Dtype != "fp32" || metadata.Device != targetconfig.CPU || !slices.Equal(metadata.Shape, field.shape) {
			t.Fatalf("training tensor metadata: %s (%v)", tensor.Value, err)
		}
		for _, number := range tensorNumbers(t, tensor) {
			if math.IsNaN(number) || math.IsInf(number, 0) {
				t.Fatalf("nonfinite training tensor: %s", tensor.Value)
			}
		}
		numbers = append(numbers, tensorNumbers(t, tensor)...)
	}
	if len(numbers) != 9 {
		t.Fatalf("training parameter count: %d", len(numbers))
	}
	return numbers
}

func assertMetalTrainingResult(t *testing.T, value runtimeValue) float64 {
	t.Helper()
	loss := trainingFloat(t, recordField(t, value, "loss"))
	initial := trainingFloat(t, recordField(t, value, "initial_loss"))
	parameters := trainingParameters(t, recordField(t, value, "parameters"))
	for _, reference := range trainingReferences(t) {
		checkNumbers(t, []float64{initial}, []float64{reference.InitialLoss}, 0, 0)
		checkNumbers(t, []float64{loss}, []float64{reference.FinalLoss}, 1e-8, 1e-5)
		checkNumbers(t, parameters, reference.FinalParameters, 1e-6, 1e-5)
	}
	if loss >= .001 {
		t.Fatalf("training loss %g", loss)
	}
	return loss
}

func trainingResult(t *testing.T, got result) runtimeValue {
	t.Helper()
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("training result: %+v", got)
	}
	var envelope struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	return successfulValue(t, envelope.Result)
}

// This diagnostic variant observes gradients explicitly. It is separate from
// the maintained example whose transfer and storage contract is tested above.
func metalTrainingGradientSource(t *testing.T) string {
	t.Helper()
	text := metalTrainingSource(t)
	stateType := "Optimizer.SGD.State<" + trainingParameterType + ">"
	observationType := "Record{loss: Float, gradients: " + trainingParameterType + "}"
	stepStart := strings.Index(text, "flow TrainStep(")
	mainStart := strings.Index(text, "flow Main()")
	if stepStart < 0 || mainStart < stepStart {
		t.Fatal("training fixture flow boundaries changed")
	}
	mainBody := strings.Index(text[mainStart:], "  device =") + mainStart
	observationStart := strings.Index(text, "  first_loss =")
	if mainBody < mainStart || observationStart < mainBody {
		t.Fatal("training fixture boundaries changed")
	}
	step := fmt.Sprintf(`flow ObserveStep(inputs: Tensor<FP32>, targets: Tensor<FP32>, state: %s) -> Record{value: %s, next_state: %s}! {
  values = Parameters.values(state.parameters)
  differentiated = Autodiff.grad(values, inputs, targets, Loss)
  gradients = Gradients.new(state.parameters, differentiated.gradients)
  next = Optimizer.SGD.step(state, gradients)
  cpu = Device.new("cpu")
  b1 = Tensor.to(differentiated.gradients.b1, cpu)
  b2 = Tensor.to(differentiated.gradients.b2, cpu)
  w1 = Tensor.to(differentiated.gradients.w1, cpu)
  w2 = Tensor.to(differentiated.gradients.w2, cpu)
  loss = Tensor.to_float(Tensor.to(differentiated.value, cpu))
  observed = Record.new(b1: b1, b2: b2, w1: w1, w2: w2)
  value = Record.new(loss: loss, gradients: observed)
  return Record.new(value: value, next_state: next)
}
`, stateType, observationType, stateType)
	return text[:stepStart] + step + "flow Main() -> List<" + observationType + ">! {\n" + text[mainBody:observationStart] + "  return List.unfold(initial_state, 200, inputs, targets, ObserveStep)\n}\nreturn Main()\n"
}
