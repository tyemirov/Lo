package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type llvmRuntimeScenario struct {
	name, source   string
	expectedStdout string
	metal          bool
	harness        string
	number         int
	transform      func(*testing.T, []byte) []byte
}

func TestLLVMRuntimeLifetime(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmLifetimeScenarios())
}

func llvmLifetimeScenarios() []llvmRuntimeScenario {
	success := `flow Step(state: Int, item: Int) -> Int { return Match.new(item, 2: Math.add(state, 20), _: Math.add(state, item)) }; return Flow.fold(List.new(1, 2, 3), 0, Step)`
	return []llvmRuntimeScenario{
		{name: "success", source: success, number: 0},
		{name: "recoverable", source: `flow Step(item: Int) -> Int! { return Math.div(10, item) }; return List.map(List.new(2, 0, 3), Step)`, number: 1},
		{name: "budget", source: success, number: 2},
		{name: "cancelled", source: success, number: 3},
		{name: "thread-owner", source: success, number: 4},
	}
}

func runLLVMRuntimeScenarios(t *testing.T, scenarios []llvmRuntimeScenario) {
	t.Helper()
	runLLVMRuntimeTargetScenarios(t, runtime.GOOS+"/"+runtime.GOARCH, scenarios)
}

func runLLVMRuntimeTargetScenarios(t *testing.T, selectedTarget string, scenarios []llvmRuntimeScenario) {
	t.Helper()
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if manifest == "" {
		t.Fatal("LLVM lane requires LO_LLVM_TOOLCHAIN")
	}
	manifest, err := filepath.Abs(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = filepath.EvalSymlinks(manifest)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, manifest)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("toolchain manifest leaves repository")
	}
	type fileIdentity struct{ Path, SHA256 string }
	type targetManifest struct {
		Triple          string
		Linker, Runtime fileIdentity
		Inputs          map[string]fileIdentity
		LinkArgs        []string `json:"link_args"`
	}
	var distribution struct {
		ABI, LLVM, Host string
		Compiler        fileIdentity
		Targets         map[string]targetManifest
		Metal           *targetManifest
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &distribution); err != nil {
		t.Fatal(err)
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	if distribution.ABI != "lo-native-1" || distribution.LLVM != "21.1.8" || distribution.Host != host {
		t.Fatal("toolchain identity mismatch")
	}
	defaultTarget, exists := distribution.Targets[selectedTarget]
	if !exists {
		t.Fatalf("toolchain lacks target %s", selectedTarget)
	}
	triple := map[string]string{"darwin/arm64": "aarch64-apple-darwin", "linux/arm64": "aarch64-unknown-linux-gnu"}[selectedTarget]
	if triple == "" || defaultTarget.Triple != triple {
		t.Fatal("toolchain target mismatch")
	}
	resolve := func(identity fileIdentity) string {
		t.Helper()
		if !filepath.IsLocal(identity.Path) {
			t.Fatal("toolchain path must be relative")
		}
		path, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(manifest), identity.Path))
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(repository, path)
		if err != nil || !filepath.IsLocal(relative) {
			t.Fatal("toolchain input leaves repository")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != identity.SHA256 {
			t.Fatalf("toolchain digest mismatch: %s", identity.Path)
		}
		return path
	}
	compiler := resolve(distribution.Compiler)
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			target := defaultTarget
			if scenario.metal {
				if selectedTarget != "darwin/arm64" || distribution.Metal == nil {
					t.Fatal("Metal fixture requires the explicit Darwin Metal runtime")
				}
				target = *distribution.Metal
				if target.Triple != triple {
					t.Fatal("Metal fixture triple mismatch")
				}
			}
			linker, archive := resolve(target.Linker), resolve(target.Runtime)
			inputs := map[string]string{}
			for name, identity := range target.Inputs {
				inputs[name] = resolve(identity)
			}
			work := t.TempDir()
			relative, err := filepath.Rel(repository, work)
			if err != nil || !filepath.IsLocal(relative) {
				t.Fatal("test directory leaves repository")
			}
			module, object, harness, executable := filepath.Join(work, "program.ll"), filepath.Join(work, "program.o"), filepath.Join(work, "lifetime.o"), filepath.Join(work, "lifetime")
			emitArguments := []string{"build", sourceFile(t, scenario.source), "--emit", "llvm", "--target", selectedTarget, "--output", module, "--format", "json"}
			if scenario.metal {
				emitArguments = append(emitArguments, "--device-backend", "metal")
			}
			emitted := runExecutable(t, binary, emitArguments...)
			if emitted.status != 0 {
				t.Fatalf("emit LLVM: %+v", emitted)
			}
			if scenario.transform != nil {
				contents, err := os.ReadFile(module)
				if err != nil {
					t.Fatal(err)
				}
				contents = scenario.transform(t, contents)
				file, err := os.OpenFile(module, os.O_WRONLY|os.O_TRUNC, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := file.Write(contents)
				closeErr := file.Close()
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			runTool := func(tool string, arguments ...string) {
				t.Helper()
				command := exec.Command(tool, arguments...)
				command.Dir = work
				command.Env = []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + work, "TMP=" + work, "TEMP=" + work, "LC_ALL=C", "LANG=C", "ZERO_AR_DATE=1"}
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", filepath.Base(tool), err, output)
				}
			}
			flags := []string{"-target", target.Triple, "-O0", "-g", "-fPIC", "-ffp-contract=off", "-I", filepath.Join(repository, "runtime/native/include"), "-I", filepath.Join(repository, "testdata/native-runtime")}
			if selectedTarget == "darwin/arm64" {
				flags = append(flags, "-mmacosx-version-min=27.0")
			}
			runTool(compiler, append(append([]string{}, flags...), "-c", module, "-o", object)...)
			harnessSource := scenario.harness
			if harnessSource == "" {
				harnessSource = "testdata/native-runtime/lifetime.c"
			}
			if !filepath.IsLocal(harnessSource) {
				t.Fatal("harness leaves repository")
			}
			runTool(compiler, append(append([]string{}, flags...), fmt.Sprintf("-DLO_CASE=%d", scenario.number), "-fno-stack-protector", "-nostdinc", "-c", filepath.Join(repository, harnessSource), "-o", harness)...)
			var arguments []string
			for _, argument := range target.LinkArgs {
				switch argument {
				case "{object}":
					arguments = append(arguments, object, harness)
				case "{runtime}":
					arguments = append(arguments, archive)
				case "{output}":
					arguments = append(arguments, executable)
				default:
					if strings.HasPrefix(argument, "{input:") && strings.HasSuffix(argument, "}") {
						name := strings.TrimSuffix(strings.TrimPrefix(argument, "{input:"), "}")
						input, exists := inputs[name]
						if !exists {
							t.Fatalf("missing link input %s", name)
						}
						arguments = append(arguments, input)
					} else {
						arguments = append(arguments, argument)
					}
				}
			}
			runTool(linker, arguments...)
			if selectedTarget == "linux/arm64" {
				got := runLinuxLLVMExecutable(t, executable)
				if got.status != 0 || got.stdout != scenario.expectedStdout || got.stderr != "" {
					t.Fatalf("Linux lifetime: %+v", got)
				}
			} else if scenario.expectedStdout != "" {
				command := exec.Command(executable)
				command.Dir = work
				command.Env = []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + work, "TMP=" + work, "TEMP=" + work, "LC_ALL=C", "LANG=C"}
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				if err := command.Run(); err != nil {
					if _, ok := err.(*exec.ExitError); !ok {
						t.Fatal(err)
					}
				}
				got := result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
				if got.status != 0 || got.stdout != scenario.expectedStdout || got.stderr != "" {
					t.Fatalf("native runtime output: %+v, expected %q", got, scenario.expectedStdout)
				}
			} else {
				runTool(executable)
			}
		})
	}
}
