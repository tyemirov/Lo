package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

func checkpointXORSource(t *testing.T, stage, backend, directory string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "examples", "xor_"+stage+"_"+backend+".lo"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, name := range []string{"xor-state.json", "xor-parameters.json"} {
		text = strings.ReplaceAll(text, fmt.Sprintf("%q", "bin/"+name), fmt.Sprintf("%q", filepath.Join(directory, name)))
	}
	return text
}

func TestCheckpointXOR(t *testing.T) {
	baseline := trainingResult(t, run(t, "run", filepath.Join(repository, "examples", "xor_cpu.lo"), "--format", "json"))
	for _, backend := range []string{"cpu", "metal"} {
		t.Run(backend, func(t *testing.T) {
			directory := t.TempDir()
			for _, stage := range []string{"save", "resume", "infer"} {
				text := checkpointXORSource(t, stage, backend, directory)
				file := sourceFile(t, text)
				for _, command := range []string{"parse", "check", "inspect"} {
					got := run(t, command, file, "--format", "json")
					if got.status != 0 || got.stderr != "" {
						t.Fatalf("%s %s: %+v", stage, command, got)
					}
					if stage == "infer" && command == "inspect" && (strings.Contains(got.stdout, "Autodiff.grad") || strings.Contains(got.stdout, "Optimizer.SGD.step")) {
						t.Fatal("inference contains training")
					}
				}
				options := []string{"--read-root", directory, "--write-root", directory}
				var got result
				if backend == "cpu" {
					got = run(t, append([]string{"run", file, "--format", "json"}, options...)...)
				} else {
					var receipts []device.Receipt
					got, receipts = runWithDevices(t, context.Background(), text, metalComputeFactory(), append(options, "--allow-device", "metal:0")...)
					assertReleased(t, receipts)
					uploads, downloads := 0, 0
					var peak uint64
					for _, receipt := range receipts {
						peak = max(peak, receipt.Live)
						if receipt.Event == device.EventTransfer {
							if receipt.Device == device.Metal {
								uploads++
							} else {
								downloads++
							}
						}
					}
					wantUp, wantDown := 6, 6
					if stage == "infer" {
						wantUp, wantDown = 5, 1
					}
					if uploads != wantUp || downloads != wantDown || peak > 8192 {
						t.Fatalf("%s storage: uploads=%d downloads=%d peak=%d", stage, uploads, downloads, peak)
					}
				}
				actual := trainingResult(t, got)
				switch stage {
				case "save":
					valueJSON(t, recordField(t, actual, "steps"), `"250"`)
				case "resume":
					assertXORResult(t, actual)
					if string(actual.Value) != string(baseline.Value) {
						t.Fatalf("resumed %s differs from 500 uninterrupted updates", backend)
					}
				case "infer":
					expected := recordField(t, baseline, "predictions")
					if string(actual.Value) != string(expected.Value) {
						t.Fatalf("loaded %s inference differs: %s", backend, actual.Value)
					}
				}
			}
		})
	}
}
