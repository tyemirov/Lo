package integration_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

func assertXORResult(t *testing.T, result runtimeValue) {
	t.Helper()
	initial := trainingFloat(t, recordField(t, result, "initial_loss"))
	loss := trainingFloat(t, recordField(t, result, "loss"))
	if initial <= .1 || loss >= .001 || loss >= initial/100 {
		t.Fatalf("XOR did not learn: initial=%g final=%g", initial, loss)
	}
	valueJSON(t, recordField(t, result, "steps"), `"500"`)
	prediction := recordField(t, result, "predictions")
	var metadata struct {
		Dtype, Device string
		Shape         []string
	}
	if err := json.Unmarshal(prediction.Value, &metadata); err != nil || metadata.Dtype != "fp32" || metadata.Device != device.CPU || !slices.Equal(metadata.Shape, []string{"4", "1"}) {
		t.Fatalf("XOR prediction metadata: %s (%v)", prediction.Value, err)
	}
	predictions := tensorNumbers(t, prediction)
	targets := []float64{0, 1, 1, 0}
	checkNumbers(t, predictions, targets, .05, 0)
	var squaredError float64
	for index, predicted := range predictions {
		if (predicted >= .5) != (targets[index] == 1) {
			t.Fatalf("XOR row %d classified incorrectly: %g", index, predicted)
		}
		squaredError += math.Pow(predicted-targets[index], 2)
	}
	checkNumbers(t, []float64{loss}, []float64{squaredError / 4}, 1e-8, 1e-5)
	trainingParameters(t, recordField(t, result, "parameters"))
	t.Logf("XOR steps=500 initial_loss=%.10g loss=%.10g predictions=%v", initial, loss, predictions)
}

func TestXORExamples(t *testing.T) {
	for _, name := range []string{"xor_cpu.lo", "xor_metal.lo"} {
		for _, command := range []string{"parse", "check", "inspect"} {
			got := run(t, command, filepath.Join(repository, "examples", name), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("%s %s: %+v", command, name, got)
			}
		}
	}
	var previous string
	for repeat := 0; repeat < 3; repeat++ {
		got := run(t, "run", filepath.Join(repository, "examples", "xor_cpu.lo"), "--format", "json")
		assertXORResult(t, trainingResult(t, got))
		if repeat > 0 && got.stdout != previous {
			t.Fatal("XOR CPU results changed between runs")
		}
		previous = got.stdout
	}
}

func TestXORMetalModel(t *testing.T) {
	text, err := os.ReadFile(filepath.Join(repository, "examples", "xor_metal.lo"))
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	for repeat := 0; repeat < 3; repeat++ {
		value, receipts, serialized := metalResult(t, string(text), metalComputeFactory())
		assertXORResult(t, value)
		if repeat > 0 && serialized != previous {
			t.Fatal("XOR Metal model results changed between runs")
		}
		previous = serialized
		uploads, downloads := 0, 0
		var peak uint64
		for _, receipt := range receipts {
			peak = max(peak, receipt.Live)
			if receipt.Event == device.EventTransfer {
				if receipt.Device == device.Metal {
					uploads++
				} else if receipt.Device == device.CPU {
					downloads++
				}
			}
		}
		if uploads != 6 || downloads != 7 || peak > 4096 {
			t.Fatalf("XOR storage: uploads=%d downloads=%d peak=%d", uploads, downloads, peak)
		}
		assertReleased(t, receipts)
	}
}
