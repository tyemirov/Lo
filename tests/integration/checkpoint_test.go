package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const checkpointSchema = "Record{x: Tensor<FP32>}"

func checkpointSetup(kind string) (string, string) {
	setup := `x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x)); cpu=Device.new("cpu"); `
	if kind == "Parameters" {
		return setup + `original=Parameters.to(p,cpu); `, "Parameters<" + checkpointSchema + ">"
	}
	config := "Optimizer.SGD.Config.new(0.1)"
	if kind == "AdamW" {
		config = "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)"
	}
	setup += fmt.Sprintf(`config=%s; initial=Optimizer.%s.State.new(p,config); g=Tensor.new(List.new(0.1,-0.2),List.new(2)); gradients=Gradients.new(p,Record.new(x:g)); first=Optimizer.%s.step(initial,gradients); original=Optimizer.%s.State.to(first,cpu); `, config, kind, kind, kind)
	return setup, "Optimizer." + kind + ".State<" + checkpointSchema + ">"
}

func checkpointWriteSource(kind, path string) string {
	setup, typ := checkpointSetup(kind)
	return fmt.Sprintf(`flow Run() -> %s! { %s data=Checkpoint.encode(original); effect=File.write(File.Path.new(%q),data); commit effect; return original }; return Run()`, typ, setup, path)
}

func TestCheckpointFreshProcess(t *testing.T) {
	for _, kind := range []string{"Parameters", "SGD", "AdamW"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "model.json")
			original := successfulValue(t, runValue(t, checkpointWriteSource(kind, path), "--write-root", dir))
			data, err := os.ReadFile(path)
			if err != nil || !json.Valid(data) {
				t.Fatalf("checkpoint bytes: %v %s", err, data)
			}
			setup, typ := checkpointSetup(kind)
			// Different template values must not replace the saved data or configuration.
			setup = strings.ReplaceAll(setup, "1.0,-2.0", "9.0,8.0")
			setup = strings.ReplaceAll(setup, "Config.new(0.1)", "Config.new(0.25)")
			setup = strings.ReplaceAll(setup, "Config.new(0.01,", "Config.new(0.02,")
			load := fmt.Sprintf(`flow Run() -> %s! { %s data=File.read(File.Path.new(%q)); return Checkpoint.decode(data,original) }; return Run()`, typ, setup, path)
			loaded := successfulValue(t, runValue(t, load, "--read-root", dir))
			if original.Type != loaded.Type || string(original.Value) != string(loaded.Value) {
				t.Fatalf("checkpoint changed across processes: saved=%s loaded=%s", original.Value, loaded.Value)
			}
			if kind == "Parameters" {
				return
			}
			baselineSetup, _ := checkpointSetup(kind)
			baseline := fmt.Sprintf(`flow Run() -> %s! { %s return Optimizer.%s.step(original,gradients) }; return Run()`, typ, baselineSetup, kind)
			expected := successfulValue(t, runValue(t, baseline))
			resume := fmt.Sprintf(`flow Run() -> %s! { %s data=File.read(File.Path.new(%q)); restored=Checkpoint.decode(data,original); return Optimizer.%s.step(restored,gradients) }; return Run()`, typ, setup, path, kind)
			actual := successfulValue(t, runValue(t, resume, "--read-root", dir))
			if string(expected.Value) != string(actual.Value) {
				t.Fatalf("resumed %s differs: expected=%s actual=%s", kind, expected.Value, actual.Value)
			}
			valueJSON(t, recordField(t, actual, "step"), `"2"`)
		})
	}
}

func TestCheckpointRejectsInvalidData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.json")
	successfulValue(t, runValue(t, checkpointWriteSource("Parameters", path), "--write-root", dir))
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, data, kind string }{
		{"truncated", string(valid[:len(valid)-1]), "Checkpoint.InvalidData"},
		{"trailing", string(valid) + "{}", "Checkpoint.InvalidData"},
		{"whitespace", " " + string(valid), "Checkpoint.InvalidData"},
		{"duplicate", strings.Replace(string(valid), `"dtype":"fp32"`, `"dtype":"fp32","dtype":"fp32"`, 1), "Checkpoint.InvalidData"},
		{"extra", strings.Replace(string(valid), `"dtype":"fp32"`, `"extra":true,"dtype":"fp32"`, 1), "Checkpoint.InvalidData"},
		{"dtype", strings.Replace(string(valid), `"fp32"`, `"fp16"`, 1), "Checkpoint.SchemaMismatch"},
		{"shape", strings.Replace(string(valid), `"shape":["2"]`, `"shape":["1","2"]`, 1), "Checkpoint.SchemaMismatch"},
		{"field", strings.ReplaceAll(string(valid), `"x":`, `"y":`), "Checkpoint.SchemaMismatch"},
		{"nonfinite", strings.Replace(string(valid), `"values":["1","-2"]`, `"values":["NaN","-2"]`, 1), "Checkpoint.InvalidData"},
		{"device", strings.Replace(string(valid), `"cpu"`, `"metal:0"`, 1), "Checkpoint.SchemaMismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.data == string(valid) {
				t.Fatal("invalid-data fixture did not change")
			}
			setup, typ := checkpointSetup("Parameters")
			source := fmt.Sprintf(`flow Run() -> %s! { %s restored=Checkpoint.decode(Bytes.new(%q),original); effect=File.write(File.Path.new("never"),Bytes.new("bad")); commit effect; return restored }; return Run()`, typ, setup, tc.data)
			boundary := &observedBoundary{}
			_, err := evaluate(t, source, boundary)
			if err == nil || !strings.Contains(err.Error(), tc.kind) || boundary.commits != 0 {
				t.Fatalf("invalid checkpoint: %v commits=%d", err, boundary.commits)
			}
		})
	}
}
