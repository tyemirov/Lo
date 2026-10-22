//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/metalconfig"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativebuild"
)

type nativeMetalArtifactIdentity struct {
	SourceDigest                        string `json:"source_sha256"`
	Source, Compiler, Toolchain, Target string
	DeviceBackend                       device.BuildBackend `json:"device_backend"`
	Runtime                             string              `json:"runtime_sha256"`
	Metal                               *metalconfig.Identity
}

type nativeMetalSelectedInputs struct {
	Targets map[string]struct{ Runtime struct{ SHA256 string } }
	Metal   *struct {
		Runtime  struct{ SHA256 string }
		Identity metalconfig.Identity
	}
}

func nativeMetalManifest(t *testing.T) (string, nativeMetalSelectedInputs) {
	t.Helper()
	path := os.Getenv("LO_LLVM_TOOLCHAIN")
	if path == "" {
		t.Fatal("native Metal tests require LO_LLVM_TOOLCHAIN")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, path)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("native Metal manifest leaves Lo")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var selected nativeMetalSelectedInputs
	if err := json.Unmarshal(data, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.Metal == nil {
		t.Fatal("native Metal tests require an explicit Metal runtime")
	}
	return path, selected
}

func nativeMetalBuildArgsForManifest(source, output, backend, manifest string) []string {
	return []string{"build", source, "--toolchain", manifest, "--device-backend", backend, "--output", output, "--format", "json"}
}
func nativeMetalBuildArgs(t *testing.T, source, output, backend string) []string {
	t.Helper()
	manifest, _ := nativeMetalManifest(t)
	return nativeMetalBuildArgsForManifest(source, output, backend, manifest)
}
func nativeMetalMetadata(t *testing.T, artifact, source string, backend device.BuildBackend) nativeMetalArtifactIdentity {
	t.Helper()
	got := runExecutable(t, artifact, "metadata")
	var identity nativeMetalArtifactIdentity
	if got.status != 0 || json.Unmarshal([]byte(got.stdout), &identity) != nil {
		t.Fatalf("LLVM artifact metadata: %+v", got)
	}
	_, selected := nativeMetalManifest(t)
	runtime := selected.Targets[metalconfig.Target].Runtime.SHA256
	if backend == device.MetalBuild {
		runtime = selected.Metal.Runtime.SHA256
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if identity.DeviceBackend != backend || identity.Target != metalconfig.Target || identity.Compiler != nativebuild.LLVMCompilerIdentity || identity.Toolchain != "LLVM 21.1.8" || identity.Runtime != runtime || identity.Source != source || identity.SourceDigest != hashText(data) {
		t.Fatalf("LLVM artifact identity: %s", got.stdout)
	}
	if backend == device.MetalBuild && (identity.Metal == nil || !reflect.DeepEqual(*identity.Metal, selected.Metal.Identity)) {
		t.Fatalf("Metal manifest identity differs: %s", got.stdout)
	}
	if backend == device.SoftwareBuild && identity.Metal != nil {
		t.Fatalf("software artifact contains Metal identity: %s", got.stdout)
	}
	return identity
}

func TestNativeMetalLiveStandalone(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository, "examples", "metal_arithmetic.lo"))
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFile(t, string(data))
	compiler := filepath.Join(repository, "bin", "lo-metal")
	artifact := filepath.Join(t.TempDir(), "program")
	built := runExecutable(t, compiler, nativeMetalBuildArgs(t, source, artifact, "metal")...)
	if built.status != 0 {
		t.Fatalf("Metal artifact build: %+v", built)
	}
	identity := nativeMetalMetadata(t, artifact, source, device.MetalBuild)
	metadata := runExecutable(t, artifact, "metadata")
	if strings.Contains(metadata.stdout, "grant") || strings.Contains(metadata.stdout, "allow-device") {
		t.Fatalf("metadata contains authorization: %s", metadata.stdout)
	}
	_, receipts := runWithDevices(t, context.Background(), string(data), device.DefaultFactory(), "--allow-device", "metal:0")
	opened := false
	for _, receipt := range receipts {
		if receipt.Event == device.EventOpen {
			opened = true
			if receipt.Backend.KernelSHA256 != identity.Metal.KernelSHA256 || receipt.Backend.Profile != identity.Metal.Profile {
				t.Fatalf("artifact and interpreter identities differ: %+v %+v", receipt.Backend, identity.Metal)
			}
			t.Logf("runtime identity: %+v", receipt.Backend)
		}
	}
	if !opened {
		t.Fatal("Metal session did not open")
	}
	t.Logf("artifact metadata: %s", metadata.stdout)
	assertNativeObjectTarget(t, artifact, "darwin")
	expected := runExecutable(t, compiler, "run", source, "--allow-device", "metal:0", "--format", "json")
	if expected.status != 0 {
		t.Fatalf("interpreter: %+v", expected)
	}
	denied := runExecutable(t, artifact, "run", "--format", "json")
	if denied.status != 2 || !strings.Contains(denied.stderr, "runtime.capability-denied") {
		t.Fatalf("missing runtime grant: %+v", denied)
	}
	wrong := runExecutable(t, artifact, "run", "--allow-device", "cuda-sim:0", "--format", "json")
	if wrong.status != 2 || !strings.Contains(wrong.stderr, "runtime.capability-denied") {
		t.Fatalf("wrong runtime grant: %+v", wrong)
	}
	for _, args := range [][]string{{"run", "--device-backend", "metal"}, {"build", "--output", "ignored"}} {
		got := runExecutable(t, artifact, args...)
		if got.status != 2 || !strings.Contains(got.stderr, "cli.invalid-invocation") {
			t.Fatalf("artifact command rejection: %+v", got)
		}
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(artifact, "run", "--allow-device", "metal:0", "--format", "json")
	command.Dir = t.TempDir()
	command.Env = []string{"PATH=" + t.TempDir(), "LO_BUILD_ROOT=" + os.Getenv("LO_BUILD_ROOT")}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil || stdout.String() != expected.stdout || stderr.Len() != 0 {
		t.Fatalf("standalone: %v stdout=%s stderr=%s expected=%+v", err, &stdout, &stderr, expected)
	}
}

func hashText(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func TestNativeMetalLiveDifferential(t *testing.T) {
	compiler := filepath.Join(repository, "bin", "lo-metal")
	for _, tc := range []struct {
		name, source string
		status       int
		fragment     string
	}{
		{"profile", `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(-0.0,1e-45,-1e-45),List.new(3)),Device.new("metal:0"));return Tensor.to(Tensor.add(a,a),Device.new("cpu"))};return Main()`, 0, `"values":["0","0","0"]`},
		{"fixed-reduction", `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(16777216.0,1.0,-16777216.0),List.new(3)),Device.new("metal:0"));return Tensor.to(Tensor.sum(a,List.new(0)),Device.new("cpu"))};return Main()`, 0, `"values":["0"]`},
		{"rectangular-matmul", `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0),List.new(3,2)),Device.new("metal:0"));b=Tensor.reshape(a,List.new(2,3));return Tensor.to(Tensor.matmul(a,b),Device.new("cpu"))};return Main()`, 0, `"values":["9","12","15","19","26","33","29","40","51"]`},
		{"adamw", `flow Main()->Tensor<FP32>!{x=Tensor.to(Tensor.new(List.new(1.0,-2.0),List.new(2)),Device.new("metal:0"));p=Parameters.new(Record.new(x:x));state=Optimizer.AdamW.State.new(p,Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1));g1=Tensor.to(Tensor.new(List.new(0.1,-0.2),List.new(2)),Device.new("metal:0"));g2=Tensor.to(Tensor.new(List.new(0.0,0.3),List.new(2)),Device.new("metal:0"));first=Optimizer.AdamW.step(state,Gradients.new(p,Record.new(x:g1)));second=Optimizer.AdamW.step(first,Gradients.new(p,Record.new(x:g2)));v=Parameters.values(second.parameters);return Tensor.to(v.x,Device.new("cpu"))};return Main()`, 0, `"values":["0.9813104","-1.988489"]`},
		{"reverse-error", `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Main()->Tensor<FP32>!{x=Tensor.to(Tensor.new(List.new(0.0),List.new()),Device.new("metal:0"));r=Autodiff.grad(Record.new(x:x),Loss);return Tensor.to(r.gradients.x,Device.new("cpu"))};return Main()`, 1, `Autodiff.UndefinedDerivative`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := sourceFile(t, tc.source)
			artifact := filepath.Join(t.TempDir(), "program")
			built := runExecutable(t, compiler, nativeMetalBuildArgs(t, source, artifact, "metal")...)
			if built.status != 0 {
				t.Fatalf("build: %+v", built)
			}
			for _, budget := range []string{"10000000", "1", "30"} {
				expected := runExecutable(t, compiler, "run", source, "--allow-device", "metal:0", "--max-steps", budget, "--format", "json")
				for repeat := 0; repeat < 3; repeat++ {
					actual := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--max-steps", budget, "--format", "json")
					if actual != expected {
						t.Fatalf("budget=%s repeat=%d native=%+v interpreter=%+v", budget, repeat, actual, expected)
					}
					if budget == "10000000" && (actual.status != tc.status || !strings.Contains(actual.stdout+actual.stderr, tc.fragment)) {
						t.Fatalf("independent result: %+v", actual)
					}
				}
			}
		})
	}
}

func TestNativeMetalLiveFailurePreventsCommit(t *testing.T) {
	for _, selector := range []string{"metal:0", "cuda:0"} {
		t.Run(selector, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "must-not-exist")
			text := fmt.Sprintf(`flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new(%q));b=Tensor.to(Tensor.new(List.new(1e-45),List.new()),Device.new(%q));r=Tensor.div(a,b);effect=File.Write.new(File.Path.new(%q),Bytes.new("bad"));commit effect;return Tensor.to(r,Device.new("cpu"))};return Main()`, selector, selector, path)
			source := sourceFile(t, text)
			compiler := filepath.Join(repository, "bin", "lo-metal")
			artifact := filepath.Join(t.TempDir(), "program")
			built := runExecutable(t, compiler, nativeMetalBuildArgs(t, source, artifact, "metal")...)
			if built.status != 0 {
				t.Fatalf("build: %+v", built)
			}
			expected := runExecutable(t, compiler, "run", source, "--allow-device", selector, "--write-root", dir, "--format", "json")
			actual := runExecutable(t, artifact, "run", "--allow-device", selector, "--write-root", dir, "--format", "json")
			kind := "Numeric.DivideByZero"
			if selector == "cuda:0" {
				kind = "Device.Unavailable"
			}
			if actual != expected || actual.status != 1 || actual.stdout != "" || !strings.Contains(actual.stderr, kind) {
				t.Fatalf("failure: native=%+v interpreter=%+v", actual, expected)
			}
			if _, err := os.ReadFile(path); !os.IsNotExist(err) {
				t.Fatalf("later commit: %v", err)
			}
		})
	}
}

func TestNativeMetalLiveBuildFailures(t *testing.T) {
	nativeToolchainFailures(t)
}

func TestNativeMetalLiveUnavailable(t *testing.T) {
	library := filepath.Join(t.TempDir(), "unavailable.dylib")
	command := exec.Command("xcrun", "--sdk", "macosx", "clang", "-dynamiclib", "-framework", "Metal", "-mmacosx-version-min=27.0", filepath.Join(repository, "testdata", "metal_unavailable.c"), "-o", library)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile controlled framework boundary: %v %s", err, output)
	}
	source := filepath.Join(repository, "examples", "metal_arithmetic.lo")
	compiler := filepath.Join(repository, "bin", "lo-metal")
	artifact := filepath.Join(t.TempDir(), "program")
	built := runExecutable(t, compiler, nativeMetalBuildArgs(t, source, artifact, "metal")...)
	if built.status != 0 {
		t.Fatalf("build: %+v", built)
	}
	var expected string
	for _, executable := range []string{compiler, artifact} {
		args := []string{"run"}
		if executable == compiler {
			args = append(args, source)
		}
		args = append(args, "--allow-device", "metal:0", "--format", "json")
		command := exec.Command(executable, args...)
		command.Env = append(os.Environ(), "DYLD_INSERT_LIBRARIES="+library)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err == nil || command.ProcessState.ExitCode() != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Device.Unavailable") {
			t.Fatalf("unavailable framework device: %v %s %s", err, &stdout, &stderr)
		}
		if executable == compiler {
			expected = stderr.String()
		} else if stderr.String() != expected {
			t.Fatalf("unavailable trace differs: native=%s interpreter=%s", &stderr, expected)
		}
	}
}

func TestNativeMetalLiveBuildIsolation(t *testing.T) {
	source := sourceFile(t, `return 7`)
	compiler := filepath.Join(repository, "bin", "lo-metal")
	isolatedCompiler := filepath.Join(t.TempDir(), "lo-metal")
	if err := os.Link(compiler, isolatedCompiler); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(t.TempDir(), "first"), filepath.Join(t.TempDir(), "second")
	for _, output := range []string{first, second} {
		command := exec.Command(isolatedCompiler, nativeMetalBuildArgs(t, source, output, "metal")...)
		command.Dir = t.TempDir()
		command.Env = append(os.Environ(), "PATH=", "CC=/absent", "CXX=/absent", "AR=/absent", "SDKROOT=/absent", "CFLAGS=-invalid", "CPPFLAGS=-invalid", "LDFLAGS=-invalid", "MACOSX_DEPLOYMENT_TARGET=invalid", "DEVELOPER_DIR=/absent-metal-toolchain")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated LLVM build: %v %s", err, output)
		}
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
		t.Fatalf("identical inputs produced different artifacts: %s %s", hashText(a), hashText(b))
	}
	assertNativeObjectTarget(t, first, "darwin")
	nativeMetalMetadata(t, first, source, device.MetalBuild)
	got := runExecutable(t, first, "run")
	if got.status != 0 || got.stdout != "7\n" {
		t.Fatalf("unused Metal requires no grant: %+v", got)
	}
	for _, target := range []string{"darwin/arm64", "linux/arm64"} {
		artifact := filepath.Join(t.TempDir(), "software")
		args := append(nativeMetalBuildArgs(t, source, artifact, "software"), "--target", target)
		got := runExecutable(t, compiler, args...)
		if got.status != 0 {
			t.Fatalf("LLVM software build: %+v", got)
		}
		assertNativeObjectTarget(t, artifact, strings.Split(target, "/")[0])
		if target == metalconfig.Target {
			nativeMetalMetadata(t, artifact, source, device.SoftwareBuild)
		}
	}
	args := append(nativeMetalBuildArgs(t, source, first, "metal"), "--target", "linux/arm64")
	got = runExecutable(t, compiler, args...)
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "build.unsupported-target" {
		t.Fatalf("target preflight: %+v", got)
	}
	manifest, _ := nativeMetalManifest(t)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var selected map[string]any
	if err := json.Unmarshal(data, &selected); err != nil {
		t.Fatal(err)
	}
	selected["abi"] = "invalid-abi"
	data, err = json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(t.TempDir(), "toolchain.json")
	writeLLVMFixture(t, corrupt, data)
	got = runExecutable(t, compiler, nativeMetalBuildArgsForManifest(source, first, "metal", corrupt)...)
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "build.toolchain" || !strings.Contains(got.stderr, "toolchain ABI, LLVM version, or host identity does not match") {
		t.Fatalf("toolchain admission: %+v", got)
	}
	unchanged, err := os.ReadFile(first)
	if err != nil || !bytes.Equal(a, unchanged) {
		t.Fatalf("failed admission replaced artifact: %v", err)
	}
}

func TestNativeMetalLiveTraining(t *testing.T) {
	compiler := filepath.Join(repository, "bin", "lo-metal")
	for _, tc := range []struct {
		name, source string
		repeats      int
	}{
		{"200-steps", filepath.Join(repository, "examples", "training_metal.lo"), 3},
		{"finite-gradients", sourceFile(t, metalTrainingGradientSource(t)), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := filepath.Join(t.TempDir(), "training")
			built := runExecutable(t, compiler, nativeMetalBuildArgs(t, tc.source, artifact, "metal")...)
			if built.status != 0 {
				t.Fatalf("training build: %+v", built)
			}
			identity := nativeMetalMetadata(t, artifact, tc.source, device.MetalBuild)
			if identity.Metal.Profile != device.MetalProfile {
				t.Fatal("training artifact profile differs")
			}
			t.Logf("training artifact: %+v", identity)
			for _, budget := range []string{"1", "30"} {
				expected := runExecutable(t, compiler, "run", tc.source, "--allow-device", "metal:0", "--max-steps", budget, "--format", "json")
				actual := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--max-steps", budget, "--format", "json")
				if actual != expected || actual.status != 2 || actual.stdout != "" || !strings.Contains(actual.stderr, "runtime.limit") {
					t.Fatalf("training budget=%s: native=%+v interpreter=%+v", budget, actual, expected)
				}
			}
			expected := runExecutable(t, compiler, "run", tc.source, "--allow-device", "metal:0", "--format", "json")
			trainingResult(t, expected)
			for repeat := 0; repeat < tc.repeats; repeat++ {
				got := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--format", "json")
				if got != expected {
					t.Fatalf("training parity: native=%+v interpreter=%+v", got, expected)
				}
				if tc.name == "200-steps" {
					loss := assertMetalTrainingResult(t, trainingResult(t, got))
					t.Logf("native run=%d loss=%.17g", repeat+1, loss)
				} else {
					var steps []runtimeValue
					if err := json.Unmarshal(trainingResult(t, got).Value, &steps); err != nil || len(steps) != 200 {
						t.Fatalf("native gradient steps=%d (%v)", len(steps), err)
					}
					for _, step := range steps {
						trainingFloat(t, recordField(t, step, "loss"))
						trainingParameters(t, recordField(t, step, "gradients"))
					}
					t.Log("native: 200 finite losses and 1800 finite gradient coordinates match the interpreter exactly")
				}
			}
		})
	}
}
