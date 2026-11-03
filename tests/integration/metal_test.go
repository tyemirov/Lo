package integration_test

import (
	"fmt"

	"strings"
	"testing"
)

// metalTestFactory models the Metal storage boundary without a GPU dependency.

func TestMetalSelection(t *testing.T) {
	path := sourceFile(t, deviceSource("metal:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`))
	for _, tc := range []struct {
		name   string
		args   []string
		status int
		kind   string
	}{
		{"denied", nil, 2, "runtime.capability-denied"},
		{"unavailable build", []string{"--allow-device", "metal:0"}, 1, "Device.Unavailable"},
		{"different grant", []string{"--allow-device", "cuda-sim:0"}, 2, "runtime.capability-denied"},
		{"duplicate", []string{"--allow-device", "metal:0", "--allow-device", "metal:0"}, 2, "cli.invalid-invocation"},
		{"multiple", []string{"--allow-device", "metal:0", "--allow-device", "cuda:0"}, 2, "cli.invalid-invocation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runProgram(t, path, "run", append([]string{"--format", "json"}, tc.args...)...)
			if got.status != tc.status || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" {
				t.Fatalf("want %s/%d: %+v", tc.kind, tc.status, got)
			}
		})
	}
	for _, selector := range []string{"metal", "metal:1", "Metal:0", "mps:0", "auto"} {
		got := runProgram(t, sourceFile(t, fmt.Sprintf(`return Device.new(%q)`, selector)), "run", "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "Device.InvalidSelector") {
			t.Fatalf("invalid selector %s: %+v", selector, got)
		}
	}
}

func TestMetalStaticInspection(t *testing.T) {
	path := sourceFile(t, deviceSource("metal:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`))
	for _, command := range []string{"parse", "check", "inspect"} {
		got := run(t, command, path, "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("static %s: %+v", command, got)
		}
		if command == "inspect" {
			for _, fragment := range []string{`"metal:0"`, `"storage_bytes":16`, `"devices":true`, `metal-fp32`, `Device.UnsupportedProfile`, `Device.LibraryFailed`, `Device.Lost`} {
				if !strings.Contains(got.stdout, fragment) {
					t.Fatalf("inspection lacks %s: %s", fragment, got.stdout)
				}
			}
		}
	}
}

// A transfer failure must prevent the following effect.
