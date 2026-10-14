package integration_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCPUTrainingExample(t *testing.T) {
	path := filepath.Join(repository, "examples", "training_cpu.lo")
	for _, command := range []string{"parse", "check", "inspect"} {
		got := run(t, command, path, "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("training %s: %+v", command, got)
		}
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := successfulValue(t, runValue(t, string(source)))
	second := successfulValue(t, runValue(t, string(source)))
	if string(first.Value) != string(second.Value) {
		t.Fatal("CPU training is not bitwise repeatable")
	}
	valueJSON(t, recordField(t, first, "initial_loss"), `"2.46875"`)
	lossField := recordField(t, first, "loss")
	text, err := strconv.Unquote(string(lossField.Value))
	if err != nil {
		t.Fatal(err)
	}
	loss, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatal(err)
	}
	checkNumbers(t, []float64{loss}, []float64{.00013215476064942777}, 1e-8, 1e-5)
	if loss >= .001 {
		t.Fatalf("training loss=%g", loss)
	}
	parameters := recordField(t, first, "parameters")
	for _, name := range []string{"w1", "b1", "w2", "b2"} {
		if len(tensorNumbers(t, recordField(t, parameters, name))) == 0 {
			t.Fatalf("missing final parameter %s", name)
		}
	}
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
}
