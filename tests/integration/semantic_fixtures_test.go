package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticMigrationArithmeticSuccess(t *testing.T) {
	for _, fixture := range []struct{ operation, arguments, expected string }{
		{"Math.div", "-7, 3", "-2"},
		{"Math.mod", "-7, 3", "-1"},
		{"Math.min", "List.new(4, -7, 2)", "-7"},
		{"Math.max", "List.new(4, -7, 2)", "4"},
	} {
		t.Run(fixture.operation, func(t *testing.T) {
			result := successfulValue(t, runValue(t, fmt.Sprintf("return %s(%s)", fixture.operation, fixture.arguments)))
			if result.Type != "Int" {
				t.Fatalf("result type: %s", result.Type)
			}
			valueJSON(t, result, fmt.Sprintf("%q", fixture.expected))
		})
	}
}

func semanticScalarCheckpoint(number string) string {
	return fmt.Sprintf(`{"type":"Tensor\u003cFP32\u003e","value":{"dtype":"fp32","shape":[],"device":"cpu","values":[%q]}}`, number)
}

func semanticParametersCheckpoint(number string) string {
	return `{"type":"Parameters\u003cRecord{x: Tensor\u003cFP32\u003e}\u003e","value":{"x":` + semanticScalarCheckpoint(number) + `}}`
}

func semanticOptimizerCheckpoint(optimizer, parameter, step, moment1, moment2, power string) string {
	const sgdConfig = `{"type":"Optimizer.SGD.Config","value":{"learning_rate":{"type":"Float","value":"0.25"}}}`
	const adamConfig = `{"type":"Optimizer.AdamW.Config","value":{"beta1":{"type":"Float","value":"0.5"},"beta2":{"type":"Float","value":"0.5"},"epsilon":{"type":"Float","value":"2"},"learning_rate":{"type":"Float","value":"0.25"},"weight_decay":{"type":"Float","value":"0.5"}}}`
	if optimizer == "SGD" {
		return `{"type":"Optimizer.SGD.State\u003cRecord{x: Tensor\u003cFP32\u003e}\u003e","value":{"config":` + sgdConfig + `,"parameters":` + semanticParametersCheckpoint(parameter) + `,"step":{"type":"Int","value":` + fmt.Sprintf("%q", step) + `}}}`
	}
	record := func(number string) string {
		return `{"type":"Record{x: Tensor\u003cFP32\u003e}","value":{"x":` + semanticScalarCheckpoint(number) + `}}`
	}
	return `{"type":"Optimizer.AdamW.State\u003cRecord{x: Tensor\u003cFP32\u003e}\u003e","value":{"beta1_power":` + semanticScalarCheckpoint(power) + `,"beta2_power":` + semanticScalarCheckpoint(power) + `,"config":` + adamConfig + `,"first_moment":` + record(moment1) + `,"parameters":` + semanticParametersCheckpoint(parameter) + `,"second_moment":` + record(moment2) + `,"step":{"type":"Int","value":` + fmt.Sprintf("%q", step) + `}}}`
}

func assertSemanticCheckpoint(t *testing.T, result runtimeValue, expected string) {
	t.Helper()
	bytesValue := successfulValue(t, result)
	if bytesValue.Type != "Bytes" {
		t.Fatalf("checkpoint result type: %s", bytesValue.Type)
	}
	var encoded string
	if err := json.Unmarshal(bytesValue.Value, &encoded); err != nil {
		t.Fatal(err)
	}
	actual, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != expected {
		t.Fatalf("canonical checkpoint:\n%s\nwant:\n%s", actual, expected)
	}
}

func TestSemanticMigrationCheckpointResume(t *testing.T) {
	for _, fixture := range []struct{ optimizer, config, first, second string }{
		{"SGD", `Optimizer.SGD.Config.new(0.25)`, semanticOptimizerCheckpoint("SGD", "1.5", "1", "", "", ""), semanticOptimizerCheckpoint("SGD", "1", "2", "", "", "")},
		{"AdamW", `Optimizer.AdamW.Config.new(0.25,0.5,0.5,2.0,0.5)`, semanticOptimizerCheckpoint("AdamW", "1.625", "1", "1", "2", "0.5"), semanticOptimizerCheckpoint("AdamW", "1.296875", "2", "1.5", "3", "0.25")},
	} {
		t.Run(fixture.optimizer, func(t *testing.T) {
			setup := fmt.Sprintf(`x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); template=Optimizer.%s.State.new(p,%s); gradient=Gradients.new(p,Record.new(x:x)); `, fixture.optimizer, fixture.config)
			t.Run("encode-first", func(t *testing.T) {
				source := fmt.Sprintf(`flow Main() -> Bytes! { %s first=Optimizer.%s.step(template,gradient); return Checkpoint.encode(first) }; return Main()`, setup, fixture.optimizer)
				assertSemanticCheckpoint(t, runValue(t, source), fixture.first)
			})
			t.Run("resume-independent-bytes", func(t *testing.T) {
				source := fmt.Sprintf(`flow Main() -> Bytes! { %s restored=Checkpoint.decode(Bytes.new(%q),template); resumed=Optimizer.%s.step(restored,gradient); return Checkpoint.encode(resumed) }; return Main()`, setup, fixture.first, fixture.optimizer)
				assertSemanticCheckpoint(t, runValue(t, source), fixture.second)
			})
		})
	}
}

func TestSemanticMigrationFailurePreventsEffects(t *testing.T) {
	for _, fixture := range []struct{ name, setup, expression, kind string }{
		{"division", "", "Math.div(1,0)", "Math.DivideByZero"},
		{"checkpoint", `x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); `, `Checkpoint.decode(Bytes.new("{"),p)`, "Checkpoint.InvalidData"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			directory := t.TempDir()
			later := filepath.Join(directory, "later")
			source := fmt.Sprintf(`flow Main() -> Int! { %s failure=%s; target=File.Write.new(File.Path.new(%q),Bytes.new("later")); commit target; return 7 }; return Main()`, fixture.setup, fixture.expression, later)
			got := run(t, "run", sourceFile(t, source), "--write-root", directory, "--format", "json")
			if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, fixture.kind) {
				t.Fatalf("expected %s: %+v", fixture.kind, got)
			}
			if _, err := os.ReadFile(later); !os.IsNotExist(err) {
				t.Fatalf("effect after failure: %v", err)
			}
		})
	}
}

func TestSemanticMigrationStepBoundary(t *testing.T) {
	source := sourceFile(t, "return Math.add(1,2)")
	for _, fixture := range []struct {
		steps  string
		status int
	}{
		{"3", 2},
		{"4", 0},
		{"5", 0},
	} {
		t.Run(fixture.steps, func(t *testing.T) {
			got := run(t, "run", source, "--max-steps", fixture.steps)
			if got.status != fixture.status {
				t.Fatalf("step boundary: %+v", got)
			}
			if fixture.status == 0 {
				if got.stdout != "3\n" || got.stderr != "" {
					t.Fatalf("arithmetic result: %+v", got)
				}
			} else if got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
				t.Fatalf("step limit: %+v", got)
			}
		})
	}
}
