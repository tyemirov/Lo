package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeMetalSelection(t *testing.T) {
	source := sourceFile(t, "return 1")
	unavailable := "build.backend-unavailable"
	if runtime.GOOS+"/"+runtime.GOARCH != "darwin/arm64" {
		unavailable = "build.unsupported-target"
	}
	for _, tc := range []struct {
		name string
		args []string
		code string
	}{
		{"cross-target", []string{"--device-backend", "metal", "--target", "linux/arm64"}, "build.unsupported-target"},
		{"compiler-unavailable", []string{"--device-backend", "metal", "--target", "darwin/arm64"}, unavailable},
		{"unknown", []string{"--device-backend", "cuda"}, "cli.invalid-invocation"},
		{"duplicate", []string{"--device-backend", "software", "--device-backend", "metal"}, "cli.invalid-invocation"},
		{"grant", []string{"--device-backend", "metal", "--allow-device", "metal:0"}, "cli.invalid-invocation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"build", source, "--output", filepath.Join(t.TempDir(), "program"), "--format", "json"}, tc.args...)
			command := exec.Command(binary, args...)
			command.Env = []string{"PATH=" + t.TempDir(), "LO_BUILD_ROOT=" + os.Getenv("LO_BUILD_ROOT")}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Run(); err == nil || diagnostics(t, stderr.String()).Diagnostics[0].Code != tc.code {
				t.Fatalf("preflight rejection: %v %s", err, &stderr)
			}
		})
	}
}

func TestNativeMetalSoftwareArtifact(t *testing.T) {
	source := sourceFile(t, `flow Main()->Tensor<FP32>!{a=Tensor.new(List.new(1.0),List.new(1));return Tensor.to(a,Device.new("metal:0"))};return Main()`)
	artifact := filepath.Join(t.TempDir(), "program")
	built := runExecutable(t, binary, "build", source, "--output", artifact, "--device-backend", "software", "--format", "json")
	if built.status != 0 {
		t.Fatalf("software build: %+v", built)
	}
	var metadata map[string]string
	got := runExecutable(t, artifact, "metadata")
	if got.status != 0 || json.Unmarshal([]byte(got.stdout), &metadata) != nil || metadata["device_backend"] != "software" {
		t.Fatalf("metadata: %+v", got)
	}
	expected := runExecutable(t, binary, "run", source, "--allow-device", "metal:0", "--format", "json")
	actual := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--format", "json")
	if actual != expected || actual.status != 1 {
		t.Fatalf("unavailable device: actual=%+v expected=%+v", actual, expected)
	}
}
