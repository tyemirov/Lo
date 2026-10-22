package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(tests *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--verify" {
		output := os.Getenv("LO_TEST_NATIVE_VERIFICATION")
		if output == "child-failure" {
			os.Exit(3)
		}
		fmt.Println(output)
		return
	}
	os.Exit(tests.Run())
}

func TestActualBenchmarkBinaryVerification(t *testing.T) {
	work, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := fixture{Name: "verification", Native: os.Args[0], Work: work}
	for _, scenario := range []struct{ name, output, expected, diagnostic string }{
		{"matching output", "7", "7", ""},
		{"wrong value", "8", "7", "native benchmark result differs"},
		{"failed executable", "child-failure", "7", "verify native benchmark"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("LO_TEST_NATIVE_VERIFICATION", scenario.output)
			err := verifyNativeBenchmark(fixture, scenario.expected)
			if scenario.diagnostic == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), scenario.diagnostic) {
				t.Fatalf("verification error = %v, expected %q", err, scenario.diagnostic)
			}
		})
	}
}

func TestRunRecordsExternalInventoryWithoutAdmission(t *testing.T) {
	work := t.TempDir()
	output := work + "/report.json"
	calls := 0
	inventory := func(config) ([]string, error) {
		calls++
		if calls == 1 {
			return []string{"before-background"}, nil
		}
		return []string{"after-background"}, nil
	}
	environment := func() (map[string]string, error) { return map[string]string{"execution": "controlled-guest"}, nil }
	load := func() ([]float64, error) { return []float64{8, 7, 6}, nil }
	report, err := runWithObservations(config{Samples: 10, Output: output}, environment, inventory, load)
	if err != nil {
		t.Fatal(err)
	}
	if report["measurement_complete"] != true {
		t.Fatalf("measurement incomplete: %v", report)
	}
	jobs := report["other_benchmark_jobs"].([]string)
	if strings.Join(jobs, ",") != "before-background,after-background" {
		t.Fatalf("lost inventory: %v", jobs)
	}
	snapshots := report["benchmark_context"].(map[string]any)
	if snapshots["before"].(map[string]any)["other_benchmark_jobs"].([]string)[0] != "before-background" || snapshots["after"].(map[string]any)["other_benchmark_jobs"].([]string)[0] != "after-background" {
		t.Fatalf("lost snapshots: %v", snapshots)
	}
}
