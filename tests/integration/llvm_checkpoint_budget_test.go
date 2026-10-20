package integration_test

import (
	"fmt"
	"strconv"
	"testing"
)

func TestLLVMCheckpointDecodeBudget(t *testing.T) {
	llvmCheckpointLane(t)
	setup, typ := checkpointSetup("Parameters")
	for _, fixture := range []struct {
		name, data string
		status     int
	}{
		{"canonical", llvmCheckpointParameters, 0},
		{"invalid-json", "!", 1},
		{"null-envelope", "null", 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			input := sourceFile(t, fmt.Sprintf(`flow Run()->%s!{%s return Checkpoint.decode(Bytes.new(%q),original)};return Run()`, typ, setup, fixture.data))
			artifact, built := buildForTest(t, input)
			if built.status != 0 {
				t.Fatalf("build checkpoint budget fixture: %+v", built)
			}
			invoke := func(steps uint64) result {
				return runCheckpointBounded(t, artifact, "run", "--format", "json", "--max-steps", strconv.FormatUint(steps, 10))
			}
			completed := func(got result) bool {
				t.Helper()
				if got.status == fixture.status {
					return true
				}
				if got.status != 2 {
					t.Fatalf("unexpected checkpoint budget result: %+v", got)
				}
				report := diagnostics(t, got.stderr)
				if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.limit" {
					t.Fatalf("unexpected checkpoint budget diagnostic: %+v", got)
				}
				return false
			}
			lower, upper := uint64(1), uint64(10000)
			if !completed(invoke(upper)) {
				t.Fatal("checkpoint fixture exceeds its declared budget search range")
			}
			for lower < upper {
				middle := lower + (upper-lower)/2
				if completed(invoke(middle)) {
					upper = middle
				} else {
					lower = middle + 1
				}
			}
			for _, steps := range []uint64{lower - 1, lower, lower + 1} {
				budget := strconv.FormatUint(steps, 10)
				expected := runCheckpointBounded(t, binary, "run", input, "--format", "json", "--max-steps", budget)
				actual := invoke(steps)
				if actual != expected {
					t.Fatalf("checkpoint budget %d differs: native %+v evaluator %+v", steps, actual, expected)
				}
				if completed(actual) != (steps >= lower) {
					t.Fatalf("inconsistent checkpoint boundary at %d", steps)
				}
			}
			t.Logf("minimum steps to checkpoint outcome: %d", lower)
		})
	}
}
