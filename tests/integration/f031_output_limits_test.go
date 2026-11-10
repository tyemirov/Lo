package integration_test

import "testing"

func TestF031FixedOutputQuota(t *testing.T) {
	source := `return Float.format_fixed(1.5,2)`
	got := runProgram(t, sourceFile(t, source), "run", "--max-value-bytes", "40")
	if got.status != 0 || got.stdout != "1.50\n" || got.stderr != "" {
		t.Fatalf("small fixed result and fallible wrapper must fit their actual value weight: %+v", got)
	}
}
