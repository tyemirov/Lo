package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeCanonicalBuild(t *testing.T) {
	compiler, _ := controlledLLVMTools(t)
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if manifest == "" {
		t.Fatal("canonical compiler qualification requires LO_LLVM_TOOLCHAIN")
	}
	input := sourceFile(t, "return Math.add(123456789012345678901234567890,1)")
	output := filepath.Join(t.TempDir(), "program")
	command := exec.Command(compiler, "build", input, "--toolchain", manifest, "--output", output, "--format", "json")
	command.Env = []string{"PATH=", "LO_BUILD_ROOT=" + os.Getenv("LO_BUILD_ROOT"), "CI=1"}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("canonical build without Go or backend selection: %v %s", err, &stderr)
	}
	var receipt struct{ Codegen, ABI string }
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil || receipt.Codegen != "llvm" || receipt.ABI != "lo-native-1" {
		t.Fatalf("canonical receipt: %s %v", &stdout, err)
	}
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	result := runExecutable(t, output, "run")
	if result.status != 0 || result.stdout != "123456789012345678901234567891\n" || result.stderr != "" {
		t.Fatalf("independent artifact result: %+v", result)
	}
}

func TestNativeRejectsBackendSelection(t *testing.T) {
	compiler, _ := controlledLLVMTools(t)
	input := sourceFile(t, "return 7")
	for _, backend := range []string{"llvm", "go"} {
		t.Run(backend, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "program")
			got := runExecutable(t, compiler, "build", input, "--output", output, "--format", "json", "--codegen", backend, "--toolchain", os.Getenv("LO_LLVM_TOOLCHAIN"))
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
				t.Fatalf("removed backend selector: %+v", got)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("rejected backend published artifact: %v", err)
			}
		})
	}
}
