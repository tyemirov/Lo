package integration_test

import (
	"os"
	"strings"
	"testing"
)

func TestLLVMDeviceArtifactGrantArguments(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	input := sourceFile(t, "return 1")
	artifact, built := buildForTest(t, input)
	if built.status != 0 {
		t.Fatalf("build grant fixture: %+v", built)
	}
	for _, scenario := range []struct {
		name    string
		args    []string
		message string
	}{
		{"missing", []string{"--allow-device"}, "requires one selector with run or serve"},
		{"cpu", []string{"--allow-device", "cpu"}, "requires a non-CPU selector"},
		{"unknown", []string{"--allow-device", "cuda:1"}, "invalid device grant"},
		{"duplicate", []string{"--allow-device", "metal:0", "--allow-device", "metal:0"}, "requires one selector with run or serve"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			got := runExecutable(t, artifact, append([]string{"run", "--format", "json"}, scenario.args...)...)
			if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "cli.invalid-invocation") || !strings.Contains(got.stderr, scenario.message) {
				t.Fatalf("grant diagnostic: %+v", got)
			}
		})
	}
	got := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"value":"1"`) {
		t.Fatalf("unused device grant initializes no backend: %+v", got)
	}
}
