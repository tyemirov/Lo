package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runLinuxLLVMExecutable(t *testing.T, executable string, arguments ...string) result {
	t.Helper()
	preparation := os.Getenv("LO_NATIVE_PREPARATION")
	if preparation == "" {
		t.Fatal("Linux LLVM qualification requires LO_NATIVE_PREPARATION")
	}
	preparation, err := filepath.Abs(preparation)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err = filepath.EvalSymlinks(preparation)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Join(repository, "_scratch"), preparation)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("guest preparation leaves repository scratch directory")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	args := append([]string{"-B", filepath.Join(repository, "tools/nativeruntime/run.py"), "--preparation", preparation, executable}, arguments...)
	command := exec.CommandContext(ctx, python, args...)
	command.Dir = repository
	command.Env = append(os.Environ(), "CI=1", "PYTHONDONTWRITEBYTECODE=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("Linux software guest: %v", ctx.Err())
		}
		if _, exited := err.(*exec.ExitError); !exited {
			t.Fatal(err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), status: command.ProcessState.ExitCode()}
}

func linuxLLVMArtifact(t *testing.T, source string) string {
	t.Helper()
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if manifest == "" {
		t.Fatal("LLVM lane requires LO_LLVM_TOOLCHAIN")
	}
	artifact := filepath.Join(t.TempDir(), "linux-program")
	got := runExecutable(t, binary, "build", sourceFile(t, source), "--codegen", "llvm", "--toolchain", manifest, "--target", "linux/arm64", "--output", artifact, "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("public Linux LLVM build: %+v", got)
	}
	return artifact
}

func TestLLVMLinuxCore(t *testing.T) {
	for _, fixture := range []struct{ name, source, expected string }{
		{"big-integer", `return Math.mul(18446744073709551616, 18446744073709551616)`, `"340282366920938463463374607431768211456"`},
		{"unicode", `return String.concat(String.upper("é"), String.FromScalars.new(List.new(Scalar.new("😀"))))`, `"É😀"`},
		{"callbacks", `flow Pair(item: Int) -> List<Int> { return List.new(item, Math.add(item, 1)) }; return List.flat_map(List.new(1, 3), Pair)`, `[{"type":"Int","value":"1"},{"type":"Int","value":"2"},{"type":"Int","value":"3"},{"type":"Int","value":"4"}]`},
		{"fold", `flow Step(scale: Int, state: Int, item: Int) -> Int { return Math.add(Math.mul(state, scale), item) }; return List.fold(List.new(1, 2, 3), 0, 10, Step)`, `"123"`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			artifact := linuxLLVMArtifact(t, fixture.source)
			got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("Linux core result: %+v", got)
			}
			var envelope struct {
				Command, Status string
				Result          runtimeValue
			}
			if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Command != "run" || envelope.Status != "ok" {
				t.Fatalf("Linux result envelope: %s", got.stdout)
			}
			valueJSON(t, successfulValue(t, envelope.Result), fixture.expected)
		})
	}
}

func TestLLVMLinuxErrorAndBudget(t *testing.T) {
	t.Run("recoverable", func(t *testing.T) {
		artifact := linuxLLVMArtifact(t, `flow Step(item: Int) -> Int! { return Math.div(10, item) }; return List.map(List.new(2, 0, 3), Step)`)
		got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
		if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"Math.DivideByZero"`) || !strings.Contains(got.stderr, `"operation":{"type":"String","value":"List.map"}`) {
			t.Fatalf("Linux recoverable callback failure: %+v", got)
		}
	})
	t.Run("exact-budget", func(t *testing.T) {
		artifact := linuxLLVMArtifact(t, `return Math.add(1, 2)`)
		for _, boundary := range []struct {
			steps  string
			status int
		}{{"3", 2}, {"4", 0}, {"5", 0}} {
			t.Run(boundary.steps, func(t *testing.T) {
				got := runLinuxLLVMExecutable(t, artifact, "run", "--max-steps", boundary.steps)
				if got.status != boundary.status {
					t.Fatalf("Linux exact budget: %+v", got)
				}
				if boundary.status == 0 {
					if got.stdout != "3\n" || got.stderr != "" {
						t.Fatalf("Linux arithmetic result: %+v", got)
					}
				} else if got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
					t.Fatalf("Linux budget rejection: %+v", got)
				}
			})
		}
	})
}

func TestLLVMLinuxLifetime(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmLifetimeScenarios())
}
