package integration_test

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestLLVMRuntimeToolchainAdmission(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	compiler := developmentToolchainCompiler(t)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"response-file", func(target map[string]any) {
			target["link_args"] = append(target["link_args"].([]any), "@unadmitted.rsp")
		}},
		{"raw-archive", func(target map[string]any) { target["link_args"] = append(target["link_args"].([]any), "unadmitted.a") }},
		{"library-search", func(target map[string]any) { target["link_args"] = append(target["link_args"].([]any), "-Lunadmitted") }},
		{"output-override", func(target map[string]any) {
			target["link_args"] = append(target["link_args"].([]any), "-o", "unadmitted")
		}},
		{"extra-input", func(target map[string]any) {
			inputs := target["inputs"].(map[string]any)
			for _, input := range inputs {
				inputs["unexpected"] = input
				break
			}
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			var distribution map[string]any
			if err := json.Unmarshal(data, &distribution); err != nil {
				t.Fatal(err)
			}
			// Hardlinks give this fixture its own directory without changing inputs.
			root := t.TempDir()
			link := func(identity map[string]any) {
				name := identity["path"].(string)
				destination := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(filepath.Join(filepath.Dir(manifest), name), destination); err != nil {
					t.Fatal(err)
				}
			}
			link(distribution["compiler"].(map[string]any))
			target := distribution["targets"].(map[string]any)[runtime.GOOS+"/"+runtime.GOARCH].(map[string]any)
			link(target["linker"].(map[string]any))
			link(target["runtime"].(map[string]any))
			for _, input := range target["inputs"].(map[string]any) {
				link(input.(map[string]any))
			}
			mutation.edit(target)
			changed, err := json.Marshal(distribution)
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(root, "toolchain.json")
			writeLLVMFixture(t, selected, changed)
			input := sourceFile(t, "return 1")
			output := filepath.Join(root, "program")
			writeLLVMFixture(t, output, []byte("previous artifact"))
			before := sha256.Sum256([]byte("previous artifact"))
			got := runExecutable(t, compiler, "build", input, "--format", "json", "--toolchain", selected, "--output", output)
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "build.toolchain" {
				t.Fatalf("unadmitted linker input must fail admission: %+v", got)
			}
			after, err := os.ReadFile(output)
			if err != nil || sha256.Sum256(after) != before {
				t.Fatalf("failed build changed previous artifact: %q, %v", after, err)
			}
		})
	}
}

func writeLLVMFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write fixture: %v, %v", writeErr, closeErr)
	}
}

func TestLLVMRuntimeStandalone(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	input := sourceFile(t, `flow Add(a:Int,b:Int)->Int{return Math.add(a,b)}
return List.fold(List.new(1,2,3),123456789012345678901234567890,Add)`)
	work := t.TempDir()
	output := filepath.Join(work, "program")
	built := runExecutable(t, binary, "build", input, "--format", "json", "--toolchain", os.Getenv("LO_LLVM_TOOLCHAIN"), "--output", output)
	if built.status != 0 {
		t.Fatalf("build standalone LLVM executable: %+v", built)
	}
	var receipt struct {
		Codegen string
		ABI     string
		Runtime string `json:"runtime_sha256"`
	}
	if err := json.Unmarshal([]byte(built.stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Codegen != "llvm" || receipt.ABI != "lo-native-1" || len(receipt.Runtime) != 64 {
		t.Fatalf("LLVM build identity: %+v", receipt)
	}
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(output, "run", "--format", "json")
	command.Dir = work
	command.Env = []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + work}
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("execute with source removed and empty PATH: %v\n%s", err, data)
	}
	var result struct{ Result runtimeValue }
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, result.Result, `"123456789012345678901234567896"`)
}

func TestLLVMRuntimeArgumentMetadata(t *testing.T) {
	input := sourceFile(t, `flow Twice(n:Int)->Int{return Math.mul(n,2)}
return List.map(List.new(1,2),Twice)`)
	output := filepath.Join(t.TempDir(), "program.ll")
	got := runExecutable(t, binary, "build", input, "--emit", "llvm", "--output", output, "--format", "json")
	if got.status != 0 {
		t.Fatalf("emit operation metadata: %+v", got)
	}
	module, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2}|[^"\\])*)"`).FindSubmatch(module)
	if match == nil {
		t.Fatal("missing runtime metadata")
	}
	data, err := decodeLLVMBytes(match[1])
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Types []struct{ Name string }
		Flows []struct{ Name string }
		Sites []struct {
			Operation string
			Arguments []struct {
				Kind string
				Type uint64
				Flow uint64
			}
		} `json:"locations"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	for _, site := range metadata.Sites {
		if site.Operation != "List.map" {
			continue
		}
		if len(site.Arguments) != 2 {
			t.Fatalf("callback argument metadata absent: %+v", site)
		}
		value, callback := site.Arguments[0], site.Arguments[1]
		if value.Kind != "value" || int(value.Type) >= len(metadata.Types) || metadata.Types[value.Type].Name != "List<Int>" ||
			callback.Kind != "flow" || int(callback.Flow) >= len(metadata.Flows) || metadata.Flows[callback.Flow].Name != "Twice" {
			t.Fatalf("argument metadata does not identify declared values and callbacks: %+v", site)
		}
		return
	}
	t.Fatal("missing List.map call site")
}

// The compiler must reject unavailable native inputs at the build boundary.
func TestLLVMRuntimeRequiresToolchain(t *testing.T) {
	compiler, _ := controlledLLVMTools(t)
	input := sourceFile(t, "return 123456789012345678901234567890\n")
	output := filepath.Join(t.TempDir(), "program")
	got := runExecutable(t, compiler, "build", input, "--format", "json", "--toolchain", filepath.Join(t.TempDir(), "missing.json"),
		"--output", output)
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "build.toolchain" {
		t.Fatalf("missing LLVM distribution must be a build failure: %+v", got)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed build published an executable: %v", err)
	}
}

func TestLLVMRuntimeCore(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	for _, fixture := range []struct {
		name, source, expected string
	}{
		{"integer", `return Math.mul(123456789012345678901234567890,9)`, `"1111111101111111110111111111010"`},
		{"unicode", `return String.length("λ🙂")`, `"2"`},
		{"callback", `flow Twice(n:Int)->Int{return Math.mul(n,2)}; return List.map(List.new(1,2,3),Twice)`,
			`[{"type":"Int","value":"2"},{"type":"Int","value":"4"},{"type":"Int","value":"6"}]`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got := successfulValue(t, runValue(t, fixture.source))
			valueJSON(t, got, fixture.expected)
		})
	}
}
