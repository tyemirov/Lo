package integration_test

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
)

func TestNativeStandalone(t *testing.T) {
	source := sourceFile(t, "numbers = List.new(1, 2, 3, 4)\nreturn Math.sum(numbers)\n")
	artifact := filepath.Join(t.TempDir(), "native program")
	built := run(t, "build", source, "--output", artifact, "--format", "json")
	if built.status != 0 || built.stderr != "" {
		t.Fatalf("build native program: %+v", built)
	}
	var receipt struct{ Command, Status, Artifact, Target string }
	if err := json.Unmarshal([]byte(built.stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Command != "build" || receipt.Status != "ok" || receipt.Artifact != artifact || receipt.Target != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("build receipt: %+v", receipt)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(artifact, "run")
	command.Dir = t.TempDir()
	command.Env = []string{"PATH=" + t.TempDir(), "LO_BUILD_ROOT=" + os.Getenv("LO_BUILD_ROOT")}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil || stdout.String() != "10\n" || stderr.Len() != 0 {
		t.Fatalf("standalone result: %v stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	symbols, err := exec.Command("go", "tool", "nm", artifact).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"eval", "ir", "compiler", "check", "syntax", "resolve", "autodiff", "codegen"} {
		if bytes.Contains(symbols, []byte("github.com/MarcoPoloResearchLab/Lo/internal/"+forbidden+".")) {
			t.Fatalf("artifact contains forbidden package %s", forbidden)
		}
	}
}

func TestNativeCrossTarget(t *testing.T) {
	target := "linux"
	if runtime.GOOS == target {
		target = "darwin"
	}
	artifact := filepath.Join(t.TempDir(), "program")
	built := runExecutable(t, binary, "build", sourceFile(t, "return 1"), "--output", artifact, "--target", target+"/arm64", "--format", "json")
	if built.status != 0 {
		t.Fatalf("cross build: %+v", built)
	}
	info, err := buildinfo.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if info.GoVersion != "go1.27.1" || settings["GOOS"] != target || settings["GOARCH"] != "arm64" || settings["CGO_ENABLED"] != "0" {
		t.Fatalf("artifact build settings: %s %+v", info.GoVersion, settings)
	}
}

var nativeArtifacts = struct {
	sync.Mutex
	paths map[string]string
}{paths: map[string]string{}}

func buildForTest(t *testing.T, path string) (string, result) {
	t.Helper()
	input := path
	if !filepath.IsAbs(input) {
		input = filepath.Join(repository, input)
	}
	data, err := os.ReadFile(input)
	if err != nil {
		artifact := filepath.Join(t.TempDir(), "program")
		return artifact, runExecutable(t, nativeCompiler, "build", path, "--output", artifact, "--format", "json")
	}
	key := fmt.Sprintf("%x", sha256.Sum256(append([]byte(path+"\x00"), data...)))
	nativeArtifacts.Lock()
	defer nativeArtifacts.Unlock()
	if artifact, ok := nativeArtifacts.paths[key]; ok {
		return artifact, result{status: 0}
	}
	artifact := filepath.Join(filepath.Dir(binary), "fixture-"+key)
	r := runExecutable(t, nativeCompiler, "build", path, "--output", artifact, "--format", "json")
	if r.status == 0 {
		nativeArtifacts.paths[key] = artifact
	}
	return artifact, r
}
func runtimeCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	if path, runtimeArgs, ok := nativeInvocation(args); ok {
		artifact, r := buildForTest(t, path)
		if r.status != 0 {
			t.Fatalf("build runtime fixture: %+v", r)
		}
		return exec.Command(artifact, runtimeArgs...)
	}
	return exec.Command(binary, args...)
}

func nativeInvocation(args []string) (string, []string, bool) {
	if os.Getenv("LO_TEST_BACKEND") != "native" || len(args) == 0 || args[0] != "run" && args[0] != "serve" {
		return "", nil, false
	}
	inv, err := runner.ParseCompiler(args)
	if err != nil {
		// Invalid compiler invocations test the public compiler CLI parser.
		return "", nil, false
	}
	for i := 1; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			i++
			continue
		}
		return inv.Path(), append(append([]string{}, args[:i]...), args[i+1:]...), true
	}
	panic("validated invocation has no source argument")
}

func TestNativeBuildBoundary(t *testing.T) {
	source := sourceFile(t, "return 41\n")
	artifact := filepath.Join(t.TempDir(), "artifact")
	for _, tc := range []struct {
		name string
		args []string
		code string
	}{
		{"target", []string{"build", source, "--output", artifact, "--target", "linux/amd64", "--format", "json"}, "build.unsupported-target"},
		{"source-output", []string{"build", source, "--output", source, "--format", "json"}, "build.output"},
		{"runtime-grant", []string{"build", source, "--output", artifact, "--format", "json", "--read-root", "."}, "cli.invalid-invocation"},
		{"missing-output", []string{"build", source, "--format", "json"}, "cli.invalid-invocation"},
		{"missing-directory", []string{"build", source, "--format", "json", "--output", filepath.Join(t.TempDir(), "absent", "artifact")}, "build.output"},
		{"directory-output", []string{"build", source, "--format", "json", "--output", t.TempDir()}, "build.output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runExecutable(t, binary, tc.args...)
			if r.status != 2 || diagnostics(t, r.stderr).Diagnostics[0].Code != tc.code {
				t.Fatalf("build rejection: %+v", r)
			}
		})
	}
	file, err := os.Create(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("previous artifact"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	invalid := sourceFile(t, "return missing\n")
	r := runExecutable(t, binary, "build", invalid, "--output", artifact, "--format", "json")
	if r.status != 1 || diagnostics(t, r.stderr).Diagnostics[0].Code != "resolve.unknown-name" {
		t.Fatalf("source rejection: %+v", r)
	}
	data, err := os.ReadFile(artifact)
	if err != nil || string(data) != "previous artifact" {
		t.Fatalf("previous artifact changed: %q %v", data, err)
	}
	command := exec.Command(binary, "build", source, "--output", artifact, "--format", "json")
	command.Env = []string{"PATH=" + t.TempDir(), "LO_BUILD_ROOT=" + os.Getenv("LO_BUILD_ROOT")}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Run(); err == nil || diagnostics(t, stderr.String()).Diagnostics[0].Code != "build.toolchain" {
		t.Fatalf("missing toolchain: %v %s", err, &stderr)
	}
	data, err = os.ReadFile(artifact)
	if err != nil || string(data) != "previous artifact" {
		t.Fatal("toolchain failure replaced output")
	}
}

func TestNativeReproducibleAndIsolated(t *testing.T) {
	source := sourceFile(t, "return String.new(\"quotes \\\" $() `literal` é\")\n")
	first := filepath.Join(t.TempDir(), "first artifact")
	r := runExecutable(t, binary, "build", source, "--output", first, "--format", "json")
	if r.status != 0 {
		t.Fatalf("first build: %+v", r)
	}
	second := filepath.Join(t.TempDir(), "second artifact")
	command := exec.Command(binary, "build", source, "--output", second, "--format", "json")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "GOFLAGS=-toolexec=must-not-execute", "GOWORK=/absent/work", "GOPROXY=off", "GOMODCACHE="+t.TempDir(), "CGO_ENABLED=1", "GOOS=invalid", "GOARCH=invalid")
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated build: %v %s", err, data)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("identical inputs produced different artifact bytes")
	}
	metadata := runExecutable(t, second, "metadata")
	var record map[string]string
	if metadata.status != 0 || json.Unmarshal([]byte(metadata.stdout), &record) != nil || record["source"] != source || record["toolchain"] != "go1.27.1" || len(record["runtime_bundle_sha256"]) != 64 || len(record["source_sha256"]) != 64 {
		t.Fatalf("metadata: %+v", metadata)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	got := runExecutable(t, second, "run")
	if got.status != 0 || got.stdout != "quotes \" $() `literal` é\n" {
		t.Fatalf("escaped literal: %+v", got)
	}
}

func TestNativeLargeArguments(t *testing.T) {
	fixtures := corpus.Cases()
	fixtures = append(fixtures, corpus.Case{Name: "bindings", Source: `flow F(x: Int) -> Int { return Math.sum(List.new(` + strings.TrimSuffix(strings.Repeat("x,", 129), ",") + `)) }; return F(3)`, Expected: "387"})
	for _, fixture := range fixtures {
		if fixture.Name != "map_10000" && fixture.Name != "bindings" {
			continue
		}
		source := sourceFile(t, fixture.Source)
		artifact, built := buildForTest(t, source)
		if built.status != 0 {
			t.Fatalf("build large arguments: %+v", built)
		}
		got := runExecutable(t, artifact, "run", "--max-steps", "10000000")
		if got.status != 0 || got.stdout != fixture.Expected+"\n" {
			t.Fatalf("large argument result: %+v", got)
		}
	}
}

func TestNativeLargeCallbackContexts(t *testing.T) {
	for _, count := range []int{62, 63, 64, 127, 128} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			parameters := make([]string, count)
			arguments := make([]string, count)
			terms := make([]string, count)
			for i := range parameters {
				parameters[i] = fmt.Sprintf("context%d: Int", i)
				arguments[i] = fmt.Sprintf("Math.add(seed, %d)", i+1)
				terms[i] = fmt.Sprintf("Math.mul(context%d, %d)", i, i+1)
			}
			text := fmt.Sprintf(`flow Add(%s, item: Int) -> Int { return Math.sum(List.new(%s, item)) }; seed = 0; return Math.sum(List.map(List.new(1, 2), %s, Add))`, strings.Join(parameters, ","), strings.Join(terms, ","), strings.Join(arguments, ","))
			source := sourceFile(t, text)
			checked := runExecutable(t, binary, "check", source, "--format", "json")
			if checked.status != 0 || checked.stderr != "" {
				t.Fatalf("check callback contexts: %+v", checked)
			}
			want := strconv.Itoa(count*(count+1)*(2*count+1)/3+3) + "\n"
			expected := runExecutable(t, binary, "run", source)
			if expected.status != 0 || expected.stdout != want || expected.stderr != "" {
				t.Fatalf("interpreter callback result: %+v, want %q", expected, want)
			}
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build callback contexts: %+v", built)
			}
			actual := runExecutable(t, artifact, "run")
			if actual != expected {
				t.Fatalf("callback result: native %+v interpreter %+v", actual, expected)
			}
			for _, steps := range []string{"1", "100", "500", "1000", "100000"} {
				expected := runExecutable(t, binary, "run", source, "--max-steps", steps, "--format", "json")
				actual := runExecutable(t, artifact, "run", "--max-steps", steps, "--format", "json")
				if actual != expected {
					t.Fatalf("budget %s: native %+v interpreter %+v", steps, actual, expected)
				}
			}
		})
	}
}

func TestNativeRemovedSourceDiagnostics(t *testing.T) {
	source := sourceFile(t, `flow F(x: Int) -> Int! { return Math.div(7,x) }; return F(0)`)
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build: %+v", built)
	}
	expected := map[string]result{}
	for _, format := range []string{"text", "json"} {
		expected[format] = runExecutable(t, binary, "run", source, "--format", format)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "json"} {
		actual := runExecutable(t, artifact, "run", "--format", format)
		if actual != expected[format] || actual.status != 1 {
			t.Fatalf("%s diagnostics: native %+v interpreter %+v", format, actual, expected[format])
		}
	}
	invalid := runExecutable(t, artifact, "run", "--format", "json", source)
	if invalid.status != 2 || diagnostics(t, invalid.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
		t.Fatalf("artifact source argument: %+v", invalid)
	}
}

func TestNativeRuntimeDifferential(t *testing.T) {
	cases := []string{
		`return -0.0`,
		`flow H(e: Error) -> Error {return e}; flow F(x: Int) -> Int! {return Math.div(8,x)}; flow G() -> Error onError H {unused=F(0);return Error.new("missed","",Record.new())};return G()`,
		`flow F(x: Int) -> Int {return Math.mul(x,3)};return List.map(List.new(1,2,3),F)`,
		`flow P(x: Int) -> Bool {return Math.gt(x,2)};return Match.new(3,when P: 7,_: 9)`,
	}
	for _, text := range cases {
		source := sourceFile(t, text)
		artifact, r := buildForTest(t, source)
		if r.status != 0 {
			t.Fatalf("build differential fixture: %+v", r)
		}
		for _, steps := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "20", "30", "100"} {
			expected := runExecutable(t, binary, "run", source, "--max-steps", steps, "--format", "json")
			actual := runExecutable(t, artifact, "run", "--max-steps", steps, "--format", "json")
			if actual != expected {
				t.Fatalf("budget %s: native %+v interpreter %+v", steps, actual, expected)
			}
		}
	}
}

func TestNativeTensorBudgetDifferential(t *testing.T) {
	cases := []string{
		`flow Work() -> Tensor<FP32>! { seed=Tensor.new(List.new(1.0),List.new()); a=Tensor.broadcast(seed,List.new(2,3)); b=Tensor.broadcast(seed,List.new(3,2)); return Tensor.matmul(a,b) }; return Work()`,
		`flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(Tensor.mul(p.x,p.x),List.new(0)) }; flow Run() -> Tensor<FP32>! { one=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(one,List.new(10)); r=Autodiff.grad(Record.new(x:x),Loss); return r.gradients.x }; return Run()`,
	}
	for _, optimizer := range []struct{ name, config string }{
		{"SGD", "Optimizer.SGD.Config.new(0.1)"},
		{"AdamW", "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)"},
	} {
		cases = append(cases, fmt.Sprintf(`flow Run() -> Optimizer.%s.State<Record{x: Tensor<FP32>}>! { one=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(one,List.new(10)); p=Parameters.new(Record.new(x:x)); state=Optimizer.%s.State.new(p,%s); return Optimizer.%s.step(state,Gradients.zero(p)) }; return Run()`, optimizer.name, optimizer.name, optimizer.config, optimizer.name))
	}
	for i, text := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			minimum := minimumTensorSteps(t, text)
			source := sourceFile(t, text)
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build: %+v", built)
			}
			for _, steps := range []uint64{1, minimum / 4, minimum / 2, minimum - 1, minimum, minimum + 1} {
				budget := strconv.FormatUint(steps, 10)
				expected := runExecutable(t, binary, "run", source, "--format", "json", "--max-steps", budget)
				actual := runExecutable(t, artifact, "run", "--format", "json", "--max-steps", budget)
				if actual != expected {
					t.Fatalf("budget %d: native %+v interpreter %+v", steps, actual, expected)
				}
				if steps >= minimum && actual.status != 0 {
					t.Fatalf("completion: %+v", actual)
				}
			}
		})
	}
}

func TestNativeEffectBudgetDifferential(t *testing.T) {
	const prelude = `flow Failed(e: Error) -> Int { return -100 }
flow Read(label: String) -> Int onError Failed { path=File.Path.new("order"); target=File.Append.new(path, UTF8.Bytes.new(label)); commit target; text=UTF8.String.new(File.read(path)); return String.length(text) }
flow Head(path: String) -> Int onError Failed { text=UTF8.String.new(File.read(File.Path.new(path))); return String.length(text) }
flow Pair(first: Int, second: Int) -> Int { return Math.add(Math.mul(first,10),second) }
`
	for _, fixture := range []struct{ name, body, output, effects string }{
		{"named-order", `return Pair(second:Read("b"),first:Read("a"))`, "21\n", "ba"},
		{"match-heads", `unused=Read("a"); return Match.new(Head("order"),1:Math.add(Head("order"),6),2:Head("absent"),_:9)`, "7\n", "a"},
		{"unused-binding", `unused=Read("a"); return 7`, "7\n", "a"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := sourceFile(t, prelude+fixture.body)
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build: %+v", built)
			}
			invoke := func(executable string, steps int) (result, string) {
				directory := t.TempDir()
				args := []string{"run"}
				if executable == binary {
					args = append(args, source)
				}
				args = append(args, "--read-root", ".", "--write-root", ".", "--max-steps", strconv.Itoa(steps))
				command := exec.Command(executable, args...)
				command.Dir = directory
				var stdout, stderr bytes.Buffer
				command.Stdout = &stdout
				command.Stderr = &stderr
				if err := command.Run(); err != nil {
					if _, ok := err.(*exec.ExitError); !ok {
						t.Fatal(err)
					}
				}
				contents, err := os.ReadFile(filepath.Join(directory, "order"))
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				return result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}, string(contents)
			}
			baseline, effects := invoke(binary, 1000)
			if baseline.status != 0 || baseline.stdout != fixture.output || effects != fixture.effects {
				t.Fatalf("independent result: %+v effects %q", baseline, effects)
			}
			low, high := 1, 1000
			for low < high {
				middle := (low + high) / 2
				r, _ := invoke(binary, middle)
				if r.status == 0 {
					high = middle
				} else {
					low = middle + 1
				}
			}
			for _, steps := range []int{1, low / 4, low / 2, low - 1, low, low + 1} {
				expected, expectedEffects := invoke(binary, steps)
				actual, actualEffects := invoke(artifact, steps)
				if actual != expected || actualEffects != expectedEffects {
					t.Fatalf("steps %d: native %+v %q, interpreter %+v %q", steps, actual, actualEffects, expected, expectedEffects)
				}
			}
		})
	}
}

func TestNativeToolchainCancellationAndOutputLimit(t *testing.T) {
	nativeToolchainFailures(t, binary, nil)
}

func nativeToolchainFailures(t *testing.T, compiler string, buildArgs []string) {
	t.Helper()
	helper := filepath.Join(t.TempDir(), "go")
	command := exec.Command("go", "build", "-o", helper, "./testdata/native_toolchain")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build controlled toolchain: %v %s", err, output)
	}
	for _, action := range []string{"cancel", "overflow", "version-overflow", "failure", "artifact-overflow"} {
		t.Run(action, func(t *testing.T) {
			state := t.TempDir()
			artifact := filepath.Join(state, "artifact")
			file, err := os.Create(artifact)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = file.WriteString("previous"); err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"build", sourceFile(t, "return 1"), "--output", artifact, "--format", "json"}, buildArgs...)
			command := exec.Command(compiler, args...)
			command.Env = append(os.Environ(), "PATH="+filepath.Dir(helper)+string(os.PathListSeparator)+os.Getenv("PATH"), "LO_TEST_TOOL_STATE="+state, "LO_TEST_TOOL_ACTION="+action)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			if action == "cancel" {
				deadline := time.After(5 * time.Second)
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
			ready:
				for {
					select {
					case err := <-done:
						t.Fatalf("toolchain exited before cancellation: %v %s", err, &stderr)
					case <-deadline:
						_ = command.Process.Kill()
						t.Fatal("controlled toolchain did not start")
					case <-ticker.C:
						if data, err := os.ReadFile(filepath.Join(state, "ready")); err == nil && string(data) == "ready" {
							break ready
						}
					}
				}
				if err := command.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("build unexpectedly succeeded")
				}
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				t.Fatal("build did not stop")
			}
			code := "build.cancelled"
			if action == "failure" {
				code = "build.failed"
			}
			if strings.HasSuffix(action, "overflow") {
				code = "build.limit"
			}
			if d := diagnostics(t, stderr.String()).Diagnostics[0]; d.Code != code {
				t.Fatalf("diagnostic code %s, want %s", d.Code, code)
			}
			data, err := os.ReadFile(artifact)
			if err != nil || string(data) != "previous" {
				t.Fatal("failed build replaced artifact")
			}
			work, err := os.ReadFile(filepath.Join(state, "work"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.ReadDir(string(work)); !os.IsNotExist(err) {
				t.Fatalf("build directory remained: %s %v", work, err)
			}
			entries, err := os.ReadDir(state)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".lo-artifact-") {
					t.Fatal("staged artifact remained")
				}
			}
			if action == "cancel" {
				time.Sleep(800 * time.Millisecond)
				if _, err := os.ReadFile(filepath.Join(state, "child-survived")); !os.IsNotExist(err) {
					t.Fatal("compiler child survived group cancellation")
				}
			}
		})
	}
}
