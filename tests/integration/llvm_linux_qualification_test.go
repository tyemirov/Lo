package integration_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func runLinuxLLVMExecutable(t *testing.T, executable string, arguments ...string) result {
	t.Helper()
	return runLinuxLLVMExecutableWithCapture(t, executable, "", arguments...)
}

func runLinuxLLVMExecutableWithCapture(t *testing.T, executable, capture string, arguments ...string) result {
	t.Helper()
	return runLinuxLLVMExecutableWithInputs(t, executable, capture, "", arguments...)
}

func runLinuxLLVMExecutableWithCompanion(t *testing.T, executable, companion string, arguments ...string) result {
	t.Helper()
	if companion == "" {
		t.Fatal("Linux companion executable is required")
	}
	return runLinuxLLVMExecutableWithInputs(t, executable, "", companion, arguments...)
}

func runLinuxLLVMExecutableWithInputs(t *testing.T, executable, capture, companion string, arguments ...string) result {
	t.Helper()
	if companion != "" {
		selected, err := filepath.Abs(companion)
		if err != nil {
			t.Fatal(err)
		}
		selected, err = filepath.EvalSymlinks(selected)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(repository, selected)
		if err != nil || !filepath.IsLocal(relative) {
			t.Fatal("Linux companion executable leaves repository")
		}
		companion = selected
	}
	if companion == "" && runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		return runExecutableAt(t, filepath.Dir(executable), executable, arguments...)
	}
	if address := os.Getenv("LO_TEST_GUEST_SOCKET"); address != "" {
		return runLinuxGuestSession(t, address, executable, capture, companion, arguments)
	}
	t.Fatal("Linux execution requires the shared guest session; run make test-session-group for the selected tests")
	return result{}
}

func runLinuxGuestSession(t *testing.T, address, executable, capture, companion string, arguments []string) result {
	t.Helper()
	started := time.Now()
	resolved, err := filepath.EvalSymlinks(address)
	if err != nil {
		t.Fatalf("resolve Linux guest session: %v", err)
	}
	if relative, err := filepath.Rel(repository, resolved); err != nil || !filepath.IsLocal(relative) {
		t.Fatal("Linux guest session socket leaves repository")
	}
	connection, err := net.DialTimeout("unix", resolved, 5*time.Second)
	if err != nil {
		t.Fatalf("connect Linux guest session: %v", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(150 * time.Second)); err != nil {
		t.Fatalf("set Linux guest session deadline: %v", err)
	}
	request := struct {
		Executable string   `json:"executable"`
		Arguments  []string `json:"arguments"`
		Capture    string   `json:"capture_file"`
		Companion  string   `json:"companion"`
		Stdin      string   `json:"stdin"`
		TimeoutMS  int      `json:"timeout_ms"`
	}{executable, append([]string{}, arguments...), capture, companion, "", 120000}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		t.Fatalf("submit Linux guest case: %v", err)
	}
	var response struct {
		Status    *int   `json:"status"`
		Stdout    string `json:"stdout"`
		Stderr    string `json:"stderr"`
		ElapsedNS *int64 `json:"elapsed_ns"`
		Error     string `json:"error"`
	}
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(connection, 180<<20)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode Linux guest result: %v", err)
	}
	if response.Error != "" {
		t.Fatalf("execute Linux guest case %s: %s", t.Name(), response.Error)
	}
	stdout, stdoutErr := base64.StdEncoding.Strict().DecodeString(response.Stdout)
	stderr, stderrErr := base64.StdEncoding.Strict().DecodeString(response.Stderr)
	if stdoutErr != nil || stderrErr != nil || response.Status == nil || *response.Status < 0 || *response.Status > 255 || response.ElapsedNS == nil || *response.ElapsedNS <= 0 {
		t.Fatalf("invalid Linux guest result: status=%v stdout=%v stderr=%v", response.Status, stdoutErr, stderrErr)
	}
	t.Logf("LO_GUEST_CASE_NS=%d LO_GUEST_TOTAL_NS=%d", *response.ElapsedNS, time.Since(started).Nanoseconds())
	return result{stdout: string(stdout), stderr: string(stderr), status: *response.Status}
}

func linuxLLVMArtifact(t *testing.T, source string) string {
	t.Helper()
	if address := os.Getenv("LO_TEST_SESSION"); address != "" {
		input, err := testsession.Source(t.Context(), address, source)
		if err != nil {
			t.Fatalf("prepare immutable Linux source: %v", err)
		}
		compiled, lease, err := testsession.CompileRetained(t.Context(), address, testsession.CompileRequest{Source: input, Target: "linux/arm64"})
		if err != nil {
			t.Fatalf("prepare Linux runtime fixture: %v", err)
		}
		t.Cleanup(func() {
			if err := lease.Close(); err != nil {
				t.Errorf("release Linux runtime fixture: %v", err)
			}
		})
		t.Logf("LO_NATIVE_RUNTIME=%s", compiled.Runtime)
		return compiled.Artifact
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if manifest == "" {
		t.Fatal("LLVM lane requires LO_LLVM_TOOLCHAIN")
	}
	artifact := filepath.Join(t.TempDir(), "linux-program")
	got := runExecutable(t, binary, "build", sourceFile(t, source), "--toolchain", manifest, "--target", "linux/arm64", "--output", artifact, "--format", "json")
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
