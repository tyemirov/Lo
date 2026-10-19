//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/metalconfig"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativebuild"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimebundle"
)

func TestNativeMetalLiveStandalone(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository, "examples", "metal_arithmetic.lo"))
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFile(t, string(data))
	compiler := filepath.Join(repository, "bin", "lo-metal")
	artifact := filepath.Join(t.TempDir(), "program")
	built := runExecutable(t, compiler, "build", source, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 {
		t.Fatalf("Metal artifact build: %+v", built)
	}
	metadata := runExecutable(t, artifact, "metadata")
	var identity nativebuild.Metadata
	if metadata.status != 0 || json.Unmarshal([]byte(metadata.stdout), &identity) != nil {
		t.Fatalf("metadata: %+v", metadata)
	}
	archive, manifest, err := runtimebundle.Metal()
	if err != nil {
		t.Fatal(err)
	}
	if identity.DeviceBackend != device.MetalBuild || identity.Target != metalconfig.Target || identity.Toolchain != "go1.27.1" || identity.Compiler != nativebuild.CompilerIdentity || identity.Bundle != runtimebundle.Digest() || identity.Metal == nil || identity.Metal.Profile != "metal-fp32" || identity.Metal.SDK != "27.0" || identity.Metal.Clang == "" || identity.Metal.Metal == "" || identity.Metal.Metallib == "" || identity.Source != source || identity.SourceDigest != hashText(data) || identity.MetalBundle != hashText(archive) || identity.MetalManifest != hashText(manifest) {
		t.Fatalf("artifact identity: %s", metadata.stdout)
	}
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
	info, err := buildinfo.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["CGO_ENABLED"] != "1" || settings["-tags"] != "metal" {
		t.Fatalf("Metal build settings: %+v", settings)
	}
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
			built := runExecutable(t, compiler, "build", source, "--device-backend", "metal", "--output", artifact, "--format", "json")
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
			built := runExecutable(t, compiler, "build", source, "--device-backend", "metal", "--output", artifact, "--format", "json")
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
	nativeToolchainFailures(t, filepath.Join(repository, "bin", "lo-metal"), []string{"--device-backend", "metal"})
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
	built := runExecutable(t, compiler, "build", source, "--device-backend", "metal", "--output", artifact, "--format", "json")
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
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	for _, output := range []string{first, second} {
		command := exec.Command(isolatedCompiler, "build", source, "--device-backend", "metal", "--output", output, "--format", "json")
		command.Dir = t.TempDir()
		command.Env = append(os.Environ(), "GOFLAGS=-toolexec=must-not-execute", "GOWORK=/absent", "GOPROXY=off", "CGO_ENABLED=0", "GOOS=invalid", "GOARCH=invalid", "CC=/absent", "SDKROOT=/absent", "CGO_CFLAGS=-invalid", "CGO_CPPFLAGS=-invalid", "CGO_LDFLAGS=-invalid", "MACOSX_DEPLOYMENT_TARGET=invalid")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated build: %v %s", err, output)
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
	got := runExecutable(t, first, "run")
	if got.status != 0 || got.stdout != "7\n" {
		t.Fatalf("unused Metal requires no grant: %+v", got)
	}
	for _, target := range []string{"darwin/arm64", "linux/arm64"} {
		artifact := filepath.Join(t.TempDir(), "software")
		got := runExecutable(t, compiler, "build", source, "--target", target, "--output", artifact, "--format", "json")
		if got.status != 0 {
			t.Fatalf("default software build: %+v", got)
		}
		info, err := buildinfo.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		for _, setting := range info.Settings {
			if setting.Key == "CGO_ENABLED" && setting.Value != "0" {
				t.Fatalf("default artifact enables cgo: %+v", setting)
			}
		}
	}
	command := exec.Command(compiler, "build", source, "--device-backend", "metal", "--target", "linux/arm64", "--output", first, "--format", "json")
	command.Env = []string{"PATH=" + t.TempDir(), "LO_BUILD_ROOT=" + os.Getenv("LO_BUILD_ROOT")}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err == nil || diagnostics(t, stderr.String()).Diagnostics[0].Code != "build.unsupported-target" {
		t.Fatalf("target preflight: %v %s", err, &stderr)
	}
	command = exec.Command(compiler, "build", source, "--device-backend", "metal", "--output", first, "--format", "json")
	command.Env = append(os.Environ(), "DEVELOPER_DIR=/absent-metal-toolchain")
	stderr.Reset()
	command.Stderr = &stderr
	if err := command.Run(); err == nil || diagnostics(t, stderr.String()).Diagnostics[0].Code != "build.toolchain" {
		t.Fatalf("toolchain preflight: %v %s", err, &stderr)
	}
	unchanged, err := os.ReadFile(first)
	if err != nil || !bytes.Equal(a, unchanged) {
		t.Fatalf("failed preflight replaced artifact: %v", err)
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
			built := runExecutable(t, compiler, "build", tc.source, "--device-backend", "metal", "--output", artifact, "--format", "json")
			if built.status != 0 {
				t.Fatalf("training build: %+v", built)
			}
			metadata := runExecutable(t, artifact, "metadata")
			var identity nativebuild.Metadata
			data, err := os.ReadFile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.status != 0 || json.Unmarshal([]byte(metadata.stdout), &identity) != nil || identity.DeviceBackend != device.MetalBuild || identity.SourceDigest != hashText(data) || identity.Metal == nil || identity.Metal.Xcode != "Xcode 27.1\nBuild version 27A9275" || identity.Metal.Profile != device.MetalProfile {
				t.Fatalf("training metadata: %+v", metadata)
			}
			t.Logf("training artifact: %s", metadata.stdout)
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
