package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const llvmCheckpointParameters = `{"type":"Parameters\u003cRecord{x: Tensor\u003cFP32\u003e}\u003e","value":{"x":{"type":"Tensor\u003cFP32\u003e","value":{"dtype":"fp32","shape":["2"],"device":"cpu","values":["1","-2"]}}}}`

func llvmCheckpointLane(t *testing.T) {
	t.Helper()
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
}

func checkpointResultBytes(t *testing.T, output result) []byte {
	t.Helper()
	if output.status != 0 || output.stderr != "" {
		t.Fatalf("checkpoint encode: %+v", output)
	}
	var envelope struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(output.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	value := successfulValue(t, envelope.Result)
	if value.Type != "Bytes" {
		t.Fatalf("checkpoint result type: %s", value.Type)
	}
	var text string
	if err := json.Unmarshal(value.Value, &text); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLLVMCheckpointCanonicalBytes(t *testing.T) {
	llvmCheckpointLane(t)
	input := sourceFile(t, `flow Main()->Bytes!{x=Tensor.new(List.new(1.0,-2.0),List.new(2));p=Parameters.new(Record.new(x:x));return Checkpoint.encode(p)};return Main()`)
	expected := runExecutable(t, binary, "run", input, "--format", "json")
	actual := run(t, "run", input, "--format", "json")
	for _, output := range []result{expected, actual} {
		if got := string(checkpointResultBytes(t, output)); got != llvmCheckpointParameters {
			t.Fatalf("canonical checkpoint bytes: %s, want %s", got, llvmCheckpointParameters)
		}
	}
}

// A killed child is a test failure, not an accepted codec diagnostic. The
// deadline also keeps malformed-input regressions from hanging acceptance.
func runCheckpointBounded(t *testing.T, executable string, arguments ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = repository
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("checkpoint command exceeded bounded completion: %v", ctx.Err())
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatal(err)
		}
	}
	return result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
}

func assertLLVMCheckpointRejected(t *testing.T, setup, typ, data, kind string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "must-not-exist")
	text := fmt.Sprintf(`flow Main()->%s!{
%s
restored=Checkpoint.decode(
Bytes.new(
%q
),original);
effect=File.Write.new(File.Path.new(%q),Bytes.new("unexpected"));
commit effect;
return restored
};return Main()`, typ, setup, data, path)
	input := sourceFile(t, text)
	arguments := []string{"run", input, "--format", "json", "--write-root", directory, "--max-steps", "100000000"}
	expected := runCheckpointBounded(t, binary, arguments...)
	artifact, built := buildForTest(t, input)
	if built.status != 0 {
		t.Fatalf("build checkpoint rejection fixture: %+v", built)
	}
	_, runtimeArguments, ok := nativeInvocation(arguments)
	if !ok {
		t.Fatal("checkpoint runtime invocation not admitted")
	}
	actual := runCheckpointBounded(t, artifact, runtimeArguments...)
	for _, output := range []result{expected, actual} {
		if output.status != 1 || output.stdout != "" {
			t.Fatalf("malformed checkpoint did not stop evaluation: %+v", output)
		}
		var envelope struct {
			Diagnostics []struct {
				Code, Stage string
				Error       runtimeValue
				Span        struct{ File string }
			}
		}
		if err := json.Unmarshal([]byte(output.stderr), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Diagnostics) != 1 {
			t.Fatalf("checkpoint diagnostic count: %s", output.stderr)
		}
		diagnostic := envelope.Diagnostics[0]
		if diagnostic.Code != "runtime.unhandled-error" || diagnostic.Stage != "runtime" || diagnostic.Error.Kind != kind || diagnostic.Error.Type != "Error" || diagnostic.Span.File != input || diagnostic.Error.Input == nil || len(diagnostic.Error.Trace) == 0 {
			t.Fatalf("checkpoint typed diagnostic lost source or error context: %s", output.stderr)
		}
	}
	var expectedDiagnostic, actualDiagnostic any
	if err := json.Unmarshal([]byte(expected.stderr), &expectedDiagnostic); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(actual.stderr), &actualDiagnostic); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualDiagnostic, expectedDiagnostic) {
		t.Fatalf("checkpoint diagnostic differs: native %s; interpreter %s", actual.stderr, expected.stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed checkpoint activated file output: %v", err)
	}
}

func TestLLVMCheckpointMalformedPublicDecode(t *testing.T) {
	llvmCheckpointLane(t)
	setup, typ := checkpointSetup("Parameters")
	for _, test := range []struct{ name, data, kind string }{
		{"truncated", llvmCheckpointParameters[:len(llvmCheckpointParameters)-1], "Checkpoint.InvalidData"},
		{"trailing", llvmCheckpointParameters + "{}", "Checkpoint.InvalidData"},
		{"whitespace", " " + llvmCheckpointParameters, "Checkpoint.InvalidData"},
		{"duplicate", strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"dtype":"fp32","dtype":"fp32"`, 1), "Checkpoint.InvalidData"},
		{"unknown", strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"extra":true,"dtype":"fp32"`, 1), "Checkpoint.InvalidData"},
		{"dtype", strings.Replace(llvmCheckpointParameters, `"fp32"`, `"fp16"`, 1), "Checkpoint.SchemaMismatch"},
		{"shape", strings.Replace(llvmCheckpointParameters, `"shape":["2"]`, `"shape":["1","2"]`, 1), "Checkpoint.SchemaMismatch"},
		{"field", strings.Replace(llvmCheckpointParameters, `"x":`, `"y":`, 1), "Checkpoint.SchemaMismatch"},
		{"nonfinite", strings.Replace(llvmCheckpointParameters, `"values":["1","-2"]`, `"values":["NaN","-2"]`, 1), "Checkpoint.InvalidData"},
		{"device", strings.Replace(llvmCheckpointParameters, `"cpu"`, `"metal:0"`, 1), "Checkpoint.SchemaMismatch"},
	} {
		t.Run(test.name, func(t *testing.T) { assertLLVMCheckpointRejected(t, setup, typ, test.data, test.kind) })
	}
}

func TestLLVMCheckpointOptimizerStatePublicAdmission(t *testing.T) {
	llvmCheckpointLane(t)
	for _, optimizer := range []string{"SGD", "AdamW"} {
		t.Run(optimizer, func(t *testing.T) {
			setup, typ := checkpointSetup(optimizer)
			input := sourceFile(t, fmt.Sprintf(`flow Main()->Bytes!{%s return Checkpoint.encode(original)};return Main()`, setup))
			expected := runExecutable(t, binary, "run", input, "--format", "json")
			actual := run(t, "run", input, "--format", "json")
			valid := string(checkpointResultBytes(t, expected))
			if string(checkpointResultBytes(t, actual)) != valid {
				t.Fatalf("%s canonical optimizer checkpoint differs", optimizer)
			}
			t.Run("arbitrary-precision-step-resume", func(t *testing.T) {
				const previousStep = "184467440737095516160000000000000000000"
				const nextStep = "184467440737095516160000000000000000001"
				encoded := strings.Replace(valid, `"step":{"type":"Int","value":"1"}`, `"step":{"type":"Int","value":"`+previousStep+`"}`, 1)
				if encoded == valid {
					t.Fatal("missing optimizer step fixture")
				}
				input := sourceFile(t, fmt.Sprintf(`flow Main()->%s!{%s restored=Checkpoint.decode(Bytes.new(%q),original);return Optimizer.%s.step(restored,gradients)};return Main()`, typ, setup, encoded, optimizer))
				expected := runExecutable(t, binary, "run", input, "--format", "json")
				actual := run(t, "run", input, "--format", "json")
				for _, output := range []result{expected, actual} {
					if output.status != 0 || output.stderr != "" {
						t.Fatalf("large optimizer step: %+v", output)
					}
					var envelope struct{ Result runtimeValue }
					if err := json.Unmarshal([]byte(output.stdout), &envelope); err != nil {
						t.Fatal(err)
					}
					valueJSON(t, recordField(t, successfulValue(t, envelope.Result), "step"), fmt.Sprintf("%q", nextStep))
				}
				var expectedValue, actualValue any
				if err := json.Unmarshal([]byte(expected.stdout), &expectedValue); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(actual.stdout), &actualValue); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actualValue, expectedValue) {
					t.Fatalf("large-step resumed optimizer differs: native %s; interpreter %s", actual.stdout, expected.stdout)
				}
			})
			mutations := []struct{ name, old, new string }{
				{"negative-step", `"step":{"type":"Int","value":"1"}`, `"step":{"type":"Int","value":"-1"}`},
			}
			if optimizer == "AdamW" {
				mutations = append(mutations, []struct{ name, old, new string }{
					{"invalid-rate", `"learning_rate":{"type":"Float","value":"0.009999999776482582"}`, `"learning_rate":{"type":"Float","value":"0"}`},
					{"beta-power", `"values":["0.9"]`, `"values":["2"]`},
					{"negative-second-moment", `"values":["9.9998715e-06","3.9999486e-05"]`, `"values":["-1","3.9999486e-05"]`},
				}...)
			}
			for _, mutation := range mutations {
				t.Run(mutation.name, func(t *testing.T) {
					bad := strings.Replace(valid, mutation.old, mutation.new, 1)
					if bad == valid {
						t.Fatalf("missing mutation subject %s", mutation.old)
					}
					assertLLVMCheckpointRejected(t, setup, typ, bad, "Checkpoint.InvalidData")
				})
			}
			if optimizer == "AdamW" {
				sgdSetup, sgdType := checkpointSetup("SGD")
				assertLLVMCheckpointRejected(t, sgdSetup, sgdType, valid, "Checkpoint.SchemaMismatch")
			}
		})
	}
}

func TestLLVMCheckpointJSONBoundaryParity(t *testing.T) {
	llvmCheckpointLane(t)
	setup, typ := checkpointSetup("Parameters")
	for _, test := range []struct{ name, data, kind string }{
		{"null-envelope", "null", "Checkpoint.SchemaMismatch"},
		{"absent-value", fmt.Sprintf(`{"type":%q}`, typ), "Checkpoint.InvalidData"},
		{"number-type", `{"type":1,"value":{}}`, "Checkpoint.InvalidData"},
		{"bool-dtype", strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"dtype":true`, 1), "Checkpoint.InvalidData"},
		{"object-shape", strings.Replace(llvmCheckpointParameters, `"shape":["2"]`, `"shape":{}`, 1), "Checkpoint.InvalidData"},
		{"number-dimension", strings.Replace(llvmCheckpointParameters, `"shape":["2"]`, `"shape":[2]`, 1), "Checkpoint.InvalidData"},
		{"bool-element", strings.Replace(llvmCheckpointParameters, `"values":["1","-2"]`, `"values":[true,"-2"]`, 1), "Checkpoint.InvalidData"},
		{"null-tensor", strings.Replace(llvmCheckpointParameters, `{"dtype":"fp32","shape":["2"],"device":"cpu","values":["1","-2"]}`, `null`, 1), "Checkpoint.SchemaMismatch"},
		{"array-envelope", `[]`, "Checkpoint.InvalidData"},
		{"number-envelope", `1e99999`, "Checkpoint.InvalidData"},
		{"null-record", fmt.Sprintf(`{"type":%q,"value":null}`, typ), "Checkpoint.SchemaMismatch"},
		{"array-record", fmt.Sprintf(`{"type":%q,"value":[]}`, typ), "Checkpoint.InvalidData"},
		{"array-tensor", strings.Replace(llvmCheckpointParameters, `{"dtype":"fp32","shape":["2"],"device":"cpu","values":["1","-2"]}`, `[]`, 1), "Checkpoint.InvalidData"},
		{"typed-before-trailing", `{"type":1} !`, "Checkpoint.InvalidData"},
		{"typed-before-unknown", `{"type":1,"unknown":true}`, "Checkpoint.InvalidData"},
		{"null-dtype", strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"dtype":null`, 1), "Checkpoint.SchemaMismatch"},
		{"null-element", strings.Replace(llvmCheckpointParameters, `"values":["1","-2"]`, `"values":[null,"-2"]`, 1), "Checkpoint.InvalidData"},
		{"invalid-array-separator", `[1 !]`, "Checkpoint.InvalidData"},
		{"invalid-object-key", `{!}`, "Checkpoint.InvalidData"},
		{"invalid-object-value-separator", `{"type":"x" !}`, "Checkpoint.InvalidData"},
		{"invalid-string-escape", `"\q"`, "Checkpoint.InvalidData"},
		{"invalid-unicode-escape", `"\u12!4"`, "Checkpoint.InvalidData"},
		{"invalid-literal", `tru!`, "Checkpoint.InvalidData"},
		{"invalid-number-fraction", `1.!`, "Checkpoint.InvalidData"},
		{"invalid-number-exponent", `1e!`, "Checkpoint.InvalidData"},
		{"uppercase-type", strings.Replace(llvmCheckpointParameters, `"type":`, `"TYPE":`, 1), "Checkpoint.InvalidData"},
		{"uppercase-dtype", strings.Replace(llvmCheckpointParameters, `"dtype":`, `"DTYPE":`, 1), "Checkpoint.InvalidData"},
		{"folded-shape", strings.Replace(llvmCheckpointParameters, `"shape":`, `"ſhape":`, 1), "Checkpoint.InvalidData"},
		{"uppercase-shape-wrong-type", strings.Replace(llvmCheckpointParameters, `"shape":["2"]`, `"SHAPE":true`, 1), "Checkpoint.InvalidData"},
		{"folded-shape-wrong-element", strings.Replace(llvmCheckpointParameters, `"shape":["2"]`, `"ſhape":[1]`, 1), "Checkpoint.InvalidData"},
		{"unknown-control-field", `{"\u0001":true}`, "Checkpoint.InvalidData"},
		{"unknown-line-separator", `{"\u2028":true}`, "Checkpoint.InvalidData"},
		{"unknown-supplementary-format", `{"\udb40\udc01":true}`, "Checkpoint.InvalidData"},
		{"unknown-printable-non-ascii", `{"λ🙂":true}`, "Checkpoint.InvalidData"},
		{"invalid-string-control", "\"line\n\"", "Checkpoint.InvalidData"},
		{"invalid-token", "!", "Checkpoint.InvalidData"},
		{"invalid-object-separator", `{"type" !}`, "Checkpoint.InvalidData"},
	} {
		t.Run(test.name, func(t *testing.T) { assertLLVMCheckpointRejected(t, setup, typ, test.data, test.kind) })
	}
}

func TestLLVMCheckpointLongMultibyteInput(t *testing.T) {
	llvmCheckpointLane(t)
	setup, typ := checkpointSetup("Parameters")
	// The missing closing quote forces the entire multibyte span through the
	// lexical scanner, without printing that span in the error message.
	data := `{"type":"` + strings.Repeat("λ", 1<<19)
	assertLLVMCheckpointRejected(t, setup, typ, data, "Checkpoint.InvalidData")
}

func TestLLVMCheckpointNumericStringParity(t *testing.T) {
	llvmCheckpointLane(t)
	for _, component := range []string{"tensor", "config"} {
		t.Run(component, func(t *testing.T) {
			setup, typ := checkpointSetup("Parameters")
			valid, prefix := llvmCheckpointParameters, `"values":["`
			if component == "config" {
				setup, typ = checkpointSetup("SGD")
				input := sourceFile(t, fmt.Sprintf(`flow Main()->Bytes!{%s return Checkpoint.encode(original)};return Main()`, setup))
				valid = string(checkpointResultBytes(t, runCheckpointBounded(t, binary, "run", input, "--format", "json")))
				prefix = `"learning_rate":{"type":"Float","value":"`
			}
			start := strings.Index(valid, prefix)
			if start < 0 {
				t.Fatal("missing numeric checkpoint fixture")
			}
			start += len(prefix)
			end := strings.IndexByte(valid[start:], '"') + start
			for _, item := range []struct{ name, text string }{
				{"hexadecimal", "0x1p0"},
				{"underscores", "1_0"},
				{"prefix-underscore", "0X_1P0"},
				{"fraction-underscore", "1.0_1"},
				{"exponent-underscore", "1e0_1"},
				{"malformed-double-underscore", "1__0"},
				{"malformed-hex-underscore", "0x1_p0"},
				{"malformed-exponent-underscore", "0x1p_0"},
				{"malformed-missing-exponent", "0x1"},
				{"malformed-empty-mantissa", "0x.p0"},
				{"nonfinite", "NaN"},
				{"decimal-overflow", "1e10000"},
				{"hex-overflow", "0x1p1024"},
				{"smallest-f32", "0x1p-149"},
				{"half-smallest-f32", "0x1p-150"},
				{"above-half-smallest-f32", "0x1.000002p-150"},
				{"f32-even-tie", "0x1.000001p0"},
				{"f32-odd-tie", "0x1.000003p0"},
				{"f32-max-finite", "0x1.fffffep127"},
				{"f32-below-overflow-double-round", "0x1.fffffeffffffffffffffffp127"},
				{"f32-rounded-overflow", "0x1.ffffffp127"},
				{"smallest-f64", "0x1p-1074"},
				{"half-smallest-f64", "0x1p-1075"},
				{"above-half-smallest-f64", "0x1.00000000000008p-1075"},
				{"f64-below-overflow", "0x1.fffffffffffff7p1023"},
				{"f64-overflow-tie", "0x1.fffffffffffff8p1023"},
				{"zero-huge-exponent", "-0x0p999999999999999999999999"},
				{"huge-negative-exponent", "0x1p-999999999999999999999999"},
				{"malformed-huge-exponent", "0x0p999999999999999999999999_"},
			} {
				t.Run(item.name, func(t *testing.T) {
					data := valid[:start] + item.text + valid[end:]
					assertLLVMCheckpointRejected(t, setup, typ, data, "Checkpoint.InvalidData")
				})
			}
		})
	}
}

func TestLLVMCheckpointIntegerStringParity(t *testing.T) {
	llvmCheckpointLane(t)
	setup, typ := checkpointSetup("SGD")
	input := sourceFile(t, fmt.Sprintf(`flow Main()->Bytes!{%s return Checkpoint.encode(original)};return Main()`, setup))
	valid := string(checkpointResultBytes(t, runCheckpointBounded(t, binary, "run", input, "--format", "json")))
	for _, text := range []string{"1_0", "1_", "_1", "+1", "-0", "+", "", "١"} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			data := strings.Replace(valid, `"step":{"type":"Int","value":"1"}`, `"step":{"type":"Int","value":"`+text+`"}`, 1)
			if data == valid {
				t.Fatal("missing integer fixture")
			}
			assertLLVMCheckpointRejected(t, setup, typ, data, "Checkpoint.InvalidData")
		})
	}
}
