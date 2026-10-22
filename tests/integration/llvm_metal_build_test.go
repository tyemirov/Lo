package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMMetalBuildSelection(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	source := sourceFile(t, "return 1")
	compiler := developmentToolchainCompiler(t)
	for _, scenario := range []struct {
		name, target, code string
	}{
		{"unsupported-target", "linux/arm64", "build.unsupported-target"},
		{"missing-metal-inputs", "darwin/arm64", "build.toolchain"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// The software manifest deliberately contains no selected Metal runtime.
			manifest, err := os.ReadFile(os.Getenv("LO_LLVM_TOOLCHAIN"))
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal(manifest, &record); err != nil {
				t.Fatal(err)
			}
			delete(record, "metal")
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(t.TempDir(), "toolchain.json")
			writeLLVMFixture(t, selected, data)
			got := runExecutable(t, compiler, "build", source, "--toolchain", selected, "--device-backend", "metal", "--target", scenario.target, "--output", filepath.Join(t.TempDir(), "program"), "--format", "json")
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != scenario.code {
				t.Fatalf("Metal selection: %+v, want %s", got, scenario.code)
			}
		})
	}
}

func llvmMetalManifest(t *testing.T) (string, map[string]any) {
	t.Helper()
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record["metal"] == nil {
		t.Skip("requires the explicit Metal runtime distribution")
	}
	return manifest, record
}

func llvmInspectionTool(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("LO_NATIVE_PREPARATION"), "tools.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Tools map[string]struct{ Path, SHA256 string }
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	pin := record.Tools["llvm-objdump"]
	path := filepath.Join(repository, pin.Path)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("inspection tool leaves Lo")
	}
	contents, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	if hex.EncodeToString(sum[:]) != pin.SHA256 {
		t.Fatal("inspection tool digest differs")
	}
	return resolved
}

func TestLLVMMetalBuildStandalone(t *testing.T) {
	manifest, record := llvmMetalManifest(t)
	metal := record["metal"].(map[string]any)
	work := t.TempDir()
	source := sourceFile(t, "return 17")
	original := filepath.Join(work, "original")
	built := runExecutable(t, binary, "build", source, "--toolchain", manifest, "--device-backend", "metal", "--output", original, "--format", "json")
	if built.status != 0 {
		t.Fatalf("Metal build: %+v", built)
	}
	var receipt struct {
		DeviceBackend string `json:"device_backend"`
		Metal         map[string]any
	}
	if err := json.Unmarshal([]byte(built.stdout), &receipt); err != nil || receipt.DeviceBackend != "metal" || receipt.Metal["kernel_sha256"] != metal["identity"].(map[string]any)["kernel_sha256"] {
		t.Fatalf("Metal build receipt: %+v, %v", built, err)
	}
	artifactBytes, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := os.ReadFile(filepath.Join(filepath.Dir(manifest), metal["kernel"].(map[string]any)["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(artifactBytes, kernel) {
		t.Fatal("Metal artifact does not embed the selected precompiled kernel")
	}
	copyPath := filepath.Join(work, "copied")
	if err := os.Link(original, copyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"metadata", "run"} {
		arguments := []string{command}
		if command == "run" {
			arguments = append(arguments, "--format", "json")
		}
		execCommand := exec.Command(copyPath, arguments...)
		execCommand.Dir = work
		execCommand.Env = []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + work}
		output, err := execCommand.CombinedOutput()
		if err != nil {
			t.Fatalf("standalone %s: %v: %s", command, err, output)
		}
		if command == "metadata" {
			var metadata map[string]any
			if err := json.Unmarshal(output, &metadata); err != nil || metadata["device_backend"] != "metal" || metadata["metal"].(map[string]any)["kernel_sha256"] != receipt.Metal["kernel_sha256"] {
				t.Fatalf("Metal metadata: %s, %v", output, err)
			}
		} else if !strings.Contains(string(output), `"value":"17"`) {
			t.Fatalf("standalone result: %s", output)
		}
	}
	imports, err := exec.Command(llvmInspectionTool(t), "--macho", "--dylibs-used", copyPath).CombinedOutput()
	if err != nil {
		t.Fatalf("Metal imports: %v: %s", err, imports)
	}
	for _, library := range []string{"/Metal.framework", "/Foundation.framework", "/usr/lib/libobjc.A.dylib"} {
		if !strings.Contains(string(imports), library) {
			t.Fatalf("missing %s: %s", library, imports)
		}
	}
}

func TestLLVMMetalBroadcastStandalone(t *testing.T) {
	manifest, _ := llvmMetalManifest(t)
	work := t.TempDir()
	source := sourceFile(t, `flow Main() -> Tensor<FP32>! { seed = Tensor.new(List.new(1.25), List.new()); return Tensor.broadcast(seed, List.new(2, 2)) }; return Main()`)
	artifact := filepath.Join(work, "broadcast")
	built := runExecutable(t, binary, "build", source, "--toolchain", manifest, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 {
		t.Fatalf("Metal broadcast build: %+v", built)
	}
	var receipt struct {
		DeviceBackend string `json:"device_backend"`
	}
	if err := json.Unmarshal([]byte(built.stdout), &receipt); err != nil || receipt.DeviceBackend != "metal" {
		t.Fatalf("Metal broadcast receipt: %+v, %v", built, err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(artifact, "run", "--format", "json")
	command.Dir = work
	command.Env = []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + work}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("standalone Metal broadcast: %v: %s", err, &stderr)
	}
	var envelope struct {
		Command, Status string
		Result          runtimeValue
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || stderr.Len() != 0 || envelope.Command != "run" || envelope.Status != "ok" {
		t.Fatalf("broadcast result: %s, %s, %v", &stdout, &stderr, err)
	}
	value := successfulValue(t, envelope.Result)
	if value.Type != "Tensor<FP32>" {
		t.Fatalf("broadcast type: %s", value.Type)
	}
	valueJSON(t, value, `{"dtype":"fp32","device":"cpu","shape":["2","2"],"values":["1.25","1.25","1.25","1.25"]}`)
}

func TestLLVMMetalBuildAdmission(t *testing.T) {
	manifest, base := llvmMetalManifest(t)
	compiler := developmentToolchainCompiler(t)
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing-metal-link", "extra-link-input", "numeric-profile", "kernel-flags", "kernel-digest", "kernel-bytes", "coherent-kernel-substitution", "software-runtime"} {
		t.Run(scenario, func(t *testing.T) {
			var record map[string]any
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			metal := record["metal"].(map[string]any)
			root := t.TempDir()
			link := func(pin map[string]any) {
				name := pin["path"].(string)
				target := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(target), os.ModePerm); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(filepath.Join(filepath.Dir(manifest), name), target); err != nil && !os.IsExist(err) {
					t.Fatal(err)
				}
			}
			link(record["compiler"].(map[string]any))
			link(metal["linker"].(map[string]any))
			link(metal["runtime"].(map[string]any))
			link(metal["kernel"].(map[string]any))
			for _, pin := range metal["inputs"].(map[string]any) {
				link(pin.(map[string]any))
			}
			switch scenario {
			case "missing-metal-link":
				delete(metal["inputs"].(map[string]any), "metal")
			case "extra-link-input":
				metal["inputs"].(map[string]any)["unexpected"] = metal["kernel"]
			case "numeric-profile":
				metal["identity"].(map[string]any)["profile"] = "cpu-fp32"
			case "kernel-flags":
				metal["identity"].(map[string]any)["kernel_flags"] = []string{"-ffast-math"}
			case "kernel-digest":
				metal["identity"].(map[string]any)["kernel_sha256"] = strings.Repeat("0", 64)
			case "kernel-bytes":
				metal["kernel"].(map[string]any)["path"] = "corrupt.metallib"
				writeLLVMFixture(t, filepath.Join(root, "corrupt.metallib"), []byte("corrupt"))
			case "coherent-kernel-substitution":
				kernel := []byte("coherently substituted precompiled kernel")
				sum := sha256.Sum256(kernel)
				digest := hex.EncodeToString(sum[:])
				metal["kernel"].(map[string]any)["path"] = "substituted.metallib"
				metal["kernel"].(map[string]any)["sha256"] = digest
				metal["identity"].(map[string]any)["kernel_sha256"] = digest
				writeLLVMFixture(t, filepath.Join(root, "substituted.metallib"), kernel)
			case "software-runtime":
				metal["runtime"] = record["targets"].(map[string]any)["darwin/arm64"].(map[string]any)["runtime"]
				link(metal["runtime"].(map[string]any))
			}
			changed, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(root, "toolchain.json")
			writeLLVMFixture(t, selected, changed)
			output := filepath.Join(root, "program")
			writeLLVMFixture(t, output, []byte("previous artifact"))
			got := runExecutable(t, compiler, "build", sourceFile(t, "return 1"), "--toolchain", selected, "--device-backend", "metal", "--output", output, "--format", "json")
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "build.toolchain" {
				t.Fatalf("Metal admission: %+v", got)
			}
			if scenario == "coherent-kernel-substitution" && !strings.Contains(got.stderr, "embedded kernel differs") {
				t.Fatalf("kernel substitution did not reach archive identity binding: %+v", got)
			}
			remaining, err := os.ReadFile(output)
			if err != nil || string(remaining) != "previous artifact" {
				t.Fatalf("failed build changed artifact: %q, %v", remaining, err)
			}
		})
	}
}

func TestLLVMMetalSoftwareIsolation(t *testing.T) {
	manifest, record := llvmMetalManifest(t)
	software := record["targets"].(map[string]any)["darwin/arm64"].(map[string]any)["inputs"].(map[string]any)
	metal := record["metal"].(map[string]any)["inputs"].(map[string]any)
	for name, pin := range software {
		if pin.(map[string]any)["path"] == metal[name].(map[string]any)["path"] {
			t.Fatalf("software and Metal share mutable producer description %s", name)
		}
	}
	output := filepath.Join(t.TempDir(), "software")
	got := runExecutable(t, binary, "build", sourceFile(t, "return 1"), "--toolchain", manifest, "--device-backend", "software", "--output", output, "--format", "json")
	if got.status != 0 {
		t.Fatalf("software build: %+v", got)
	}
	imports, err := exec.Command(llvmInspectionTool(t), "--macho", "--dylibs-used", output).CombinedOutput()
	if err != nil {
		t.Fatalf("software imports: %v: %s", err, imports)
	}
	for _, library := range []string{"/Metal.framework", "/Foundation.framework", "/usr/lib/libobjc.A.dylib"} {
		if strings.Contains(string(imports), library) {
			t.Fatalf("software artifact links %s: %s", library, imports)
		}
	}
}

func TestLLVMMetalProductionUnavailable(t *testing.T) {
	llvmMetalManifest(t)
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "production-bridge-no-device", metal: true, captureOutput: true,
		harness: "testdata/native-runtime/metal-unavailable.c",
		source:  `flow Main()->Int!{a=Tensor.new(List.new(1.0),List.new(1));b=Tensor.to(a,Device.new("metal:0"));target=File.Write.new(File.Path.new("later"),Bytes.new("wrong"));commit target;return 0};return Main()`}})
}
