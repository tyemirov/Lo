package integration_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativebuild"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestLLVMLoweringBuild(t *testing.T) {
	for target, triple := range map[string]string{
		"darwin/arm64": "aarch64-apple-darwin",
		"linux/arm64":  "aarch64-unknown-linux-gnu",
	} {
		t.Run(target, func(t *testing.T) {
			input := sourceFile(t, "value = 123456789012345678901234567890\nreturn value\n")
			output := filepath.Join(t.TempDir(), "program.ll")
			command := exec.Command(binary, "build", input, "--emit", "llvm", "--target", target, "--output", output, "--format", "json")
			command.Dir = repository
			command.Env = append(os.Environ(), "PATH="+t.TempDir())
			data, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("emit LLVM without external tools: %v\n%s", err, data)
			}
			var receipt struct{ Command, Status, Artifact, Target, Emit, ABI string }
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Command != "build" || receipt.Status != "ok" || receipt.Artifact != output || receipt.Target != target {
				t.Fatalf("LLVM build receipt: %+v", receipt)
			}
			if receipt.Emit != "llvm" || receipt.ABI != "lo-native-1" {
				t.Fatalf("module receipt omits artifact kind or ABI: %+v", receipt)
			}
			module, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{triple, "define void @lo_entry(", "!DIFile(", "!DILocation("} {
				if !strings.Contains(string(module), required) {
					t.Fatalf("LLVM module omits %q", required)
				}
			}
			metadataConstant := regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2})+)", align 1$`).FindSubmatch(module)
			if metadataConstant == nil {
				t.Fatal("LLVM module omits its program metadata")
			}
			metadata, err := hex.DecodeString(strings.ReplaceAll(string(metadataConstant[1]), `\`, ""))
			if err != nil {
				t.Fatal(err)
			}
			var description struct {
				ABI       string
				Locations []struct{ Action, Text string }
			}
			if err := json.Unmarshal(metadata, &description); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, location := range description.Locations {
				if location.Action == "literal" && location.Text == "123456789012345678901234567890" {
					found = true
				}
			}
			if description.ABI != "lo-native-1" || !found {
				t.Fatalf("metadata lost the ABI or full integer literal: %+v", description)
			}
		})
	}
}

func TestLLVMLoweringInvocation(t *testing.T) {
	input := sourceFile(t, "return 1\n")
	for _, args := range [][]string{
		{"run", input, "--emit", "llvm"},
		{"inspect", input, "--emit", "llvm"},
		{"build", input, "--output", filepath.Join(t.TempDir(), "program"), "--emit", "unknown"},
		{"build", input, "--output", filepath.Join(t.TempDir(), "program"), "--emit", "llvm", "--emit", "llvm"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			invocation := append([]string{args[0], "--format", "json"}, args[1:]...)
			result := runExecutable(t, binary, invocation...)
			if result.status != 2 || diagnostics(t, result.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
				t.Fatalf("invalid emit invocation accepted: %+v", result)
			}
		})
	}
}

func TestLLVMLoweringPreservesOutput(t *testing.T) {
	input := sourceFile(t, "return 1\n")
	for _, extra := range [][]string{{"--target", "windows/arm64"}, {"--device-backend", "metal"}} {
		output := filepath.Join(t.TempDir(), "existing.ll")
		file, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("original artifact\n"); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"build", input, "--emit", "llvm", "--output", output, "--format", "json"}, extra...)
		result := runExecutable(t, binary, args...)
		if result.status != 2 || !strings.HasPrefix(diagnostics(t, result.stderr).Diagnostics[0].Code, "build.") {
			t.Fatalf("unsupported LLVM build: %+v", result)
		}
		got, err := os.ReadFile(output)
		if err != nil || string(got) != "original artifact\n" {
			t.Fatalf("failed build changed output: %q %v", got, err)
		}
	}
	result := runExecutable(t, binary, "build", input, "--emit", "llvm", "--output", input, "--format", "json")
	if result.status != 2 || diagnostics(t, result.stderr).Diagnostics[0].Code != "build.output" {
		t.Fatalf("source overwrite accepted: %+v", result)
	}
	got, err := os.ReadFile(input)
	if err != nil || string(got) != "return 1\n" {
		t.Fatalf("source changed: %q %v", got, err)
	}
}

func TestLLVMLoweringRejectsUnsupportedGraph(t *testing.T) {
	input := sourceFile(t, `
flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.sum(p.x,List.new(0)) }
flow Run() -> Tensor<FP32>! {
  x = Tensor.new(List.new(2.0),List.new(1))
  result = Autodiff.grad(Record.new(x:x),Loss)
  return result.value
}
return Run()
`)
	inspected := runExecutable(t, binary, "inspect", input, "--format", "json")
	if inspected.status != 0 {
		t.Fatalf("inspection must remain independent of native lowering: %+v", inspected)
	}
	output := filepath.Join(t.TempDir(), "unsupported.ll")
	result := runExecutable(t, binary, "build", input, "--format", "json", "--emit", "llvm", "--output", output)
	if result.status != 2 || diagnostics(t, result.stderr).Diagnostics[0].Code != "build.generation" ||
		!strings.Contains(strings.ToLower(result.stderr), "autodiff") {
		t.Fatalf("unsupported specialized lowering must fail explicitly: %+v", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("unsupported lowering published output: %v", err)
	}
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("original artifact\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result = runExecutable(t, binary, "build", input, "--format", "json", "--emit", "llvm", "--output", output)
	if result.status != 2 || diagnostics(t, result.stderr).Diagnostics[0].Code != "build.generation" {
		t.Fatalf("unsupported lowering with existing output: %+v", result)
	}
	got, err := os.ReadFile(output)
	if err != nil || string(got) != "original artifact\n" {
		t.Fatalf("unsupported lowering changed output: %q %v", got, err)
	}
}

func TestLLVMLoweringCancellation(t *testing.T) {
	input := sourceFile(t, "return 1\n")
	file, err := source.New(input, "return 1\n")
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	output := filepath.Join(t.TempDir(), "cancelled.ll")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, diagnostic = nativebuild.EmitLLVM(ctx, program, output, "darwin/arm64", device.SoftwareBuild)
	if diagnostic == nil || diagnostic.Data().Code != "build.cancelled" {
		t.Fatalf("cancelled build result: %v", diagnostic)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("cancelled build published output: %v", err)
	}
}

func TestLLVMLoweringSourceAliases(t *testing.T) {
	input := sourceFile(t, "return 1\n")
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "alias.ll")
			link := os.Link
			if kind == "symlink" {
				link = os.Symlink
			}
			if err := link(input, output); err != nil {
				t.Fatal(err)
			}
			result := runExecutable(t, binary, "build", input, "--format", "json", "--emit", "llvm", "--output", output)
			if result.status != 2 || diagnostics(t, result.stderr).Diagnostics[0].Code != "build.output" {
				t.Fatalf("source alias accepted: %+v", result)
			}
			got, err := os.ReadFile(input)
			if err != nil || string(got) != "return 1\n" {
				t.Fatalf("source alias changed input: %q %v", got, err)
			}
		})
	}
}
