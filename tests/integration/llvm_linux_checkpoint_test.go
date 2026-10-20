package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func linuxCheckpointBytes(t *testing.T, source string) []byte {
	t.Helper()
	artifact := linuxLLVMArtifact(t, source)
	value := trainingResult(t, runLinuxLLVMExecutable(t, artifact, "run", "--format", "json"))
	if value.Type != "Bytes" {
		t.Fatalf("checkpoint output type %q", value.Type)
	}
	var encoded string
	if err := json.Unmarshal(value.Value, &encoded); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !json.Valid(data) {
		t.Fatalf("checkpoint output is not JSON bytes: %v", err)
	}
	return data
}

func TestLLVMLinuxCheckpointFreshProcesses(t *testing.T) {
	t.Run("Parameters-canonical", func(t *testing.T) {
		setup, _ := checkpointSetup("Parameters")
		saved := linuxCheckpointBytes(t, fmt.Sprintf(`flow Run() -> Bytes! { %s return Checkpoint.encode(original) }; return Run()`, setup))
		const expected = `{"type":"Parameters\u003cRecord{x: Tensor\u003cFP32\u003e}\u003e","value":{"x":{"type":"Tensor\u003cFP32\u003e","value":{"dtype":"fp32","shape":["2"],"device":"cpu","values":["1","-2"]}}}}`
		if !bytes.Equal(saved, []byte(expected)) {
			t.Fatalf("Linux canonical checkpoint: %s, expected %s", saved, expected)
		}
		setup = strings.ReplaceAll(setup, "1.0,-2.0", "9.0,8.0")
		loaded := linuxCheckpointBytes(t, fmt.Sprintf(`flow Run() -> Bytes! { %s loaded=Checkpoint.decode(Bytes.new(%q),original); return Checkpoint.encode(loaded) }; return Run()`, setup, saved))
		if !bytes.Equal(loaded, saved) {
			t.Fatalf("Linux checkpoint changed in a fresh process: saved=%s loaded=%s", saved, loaded)
		}
	})
	for _, kind := range []string{"SGD", "AdamW"} {
		t.Run(kind+"-resume", func(t *testing.T) {
			setup, typ := checkpointSetup(kind)
			saved := linuxCheckpointBytes(t, fmt.Sprintf(`flow Run() -> Bytes! { %s return Checkpoint.encode(original) }; return Run()`, setup))
			secondStep := fmt.Sprintf(`g2=Tensor.new(List.new(0.0,0.3),List.new(2)); second=Optimizer.%s.step(original,Gradients.new(original.parameters,Record.new(x:g2))); return Checkpoint.encode(second)`, kind)
			expected := linuxCheckpointBytes(t, fmt.Sprintf(`flow Run() -> Bytes! { %s %s }; return Run()`, setup, secondStep))
			changed := strings.ReplaceAll(setup, "1.0,-2.0", "9.0,8.0")
			changed = strings.ReplaceAll(changed, "Config.new(0.1)", "Config.new(0.25)")
			changed = strings.ReplaceAll(changed, "Config.new(0.01,", "Config.new(0.02,")
			resume := fmt.Sprintf(`flow Run() -> Bytes! { %s restored=Checkpoint.decode(Bytes.new(%q),original); g2=Tensor.new(List.new(0.0,0.3),List.new(2)); second=Optimizer.%s.step(restored,Gradients.new(restored.parameters,Record.new(x:g2))); return Checkpoint.encode(second) }; return Run()`, changed, saved, kind)
			actual := linuxCheckpointBytes(t, resume)
			if !bytes.Equal(actual, expected) {
				t.Fatalf("Linux %s complete resumed state differs: uninterrupted=%s resumed=%s", kind, expected, actual)
			}
			var state runtimeValue
			if err := json.Unmarshal(actual, &state); err != nil {
				t.Fatal(err)
			}
			if state.Type != typ {
				t.Fatalf("Linux resumed state type %s, expected %s", state.Type, typ)
			}
			valueJSON(t, recordField(t, state, "step"), `"2"`)
			parameters := recordField(t, recordField(t, state, "parameters"), "x")
			if kind == "SGD" {
				checkNumbers(t, tensorNumbers(t, parameters), []float64{.99, -2.01}, 1e-6, 1e-5)
				checkNumbers(t, []float64{trainingFloat(t, recordField(t, recordField(t, state, "config"), "learning_rate"))}, []float64{float64(float32(0.1))}, 0, 0)
			} else {
				checkNumbers(t, tensorNumbers(t, parameters), []float64{.9813104271888733, -1.988489031791687}, 1e-6, 1e-5)
				checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, state, "first_moment"), "x")), []float64{.009000002406537533, .012000003829598427}, 1e-6, 1e-5)
				checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, state, "second_moment"), "x")), []float64{9.989871614379808e-6, .00012995832366868854}, 1e-6, 1e-5)
				checkNumbers(t, tensorNumbers(t, recordField(t, state, "beta1_power")), []float64{.81}, 1e-6, 1e-5)
				checkNumbers(t, tensorNumbers(t, recordField(t, state, "beta2_power")), []float64{.998001}, 1e-6, 1e-5)
				checkNumbers(t, []float64{trainingFloat(t, recordField(t, recordField(t, state, "config"), "learning_rate"))}, []float64{float64(float32(0.01))}, 0, 0)
			}
		})
	}
}
