package nativebuild

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type loweringCancellationProgram struct {
	ir.Program
	cancel context.CancelFunc
}

func (program loweringCancellationProgram) Body() []ir.Statement {
	program.cancel()
	return program.Program.Body()
}

type generationLimitSpan struct {
	source.Span
	path string
}

func (span generationLimitSpan) File() string { return span.path }

func TestLLVMExecutableGenerationDiagnostics(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LLVM input admission requires a Darwin host")
	}
	for _, scenario := range []string{"cancelled", "lowering-limit", "emission-limit"} {
		t.Run(scenario, func(t *testing.T) {
			root := snapshotTestDirectory(t)
			manifest := generationTestManifest(t, root)
			file, err := source.New(filepath.Join(root, "input.lo"), "return 1")
			if err != nil {
				t.Fatal(err)
			}
			program, failure := compiler.Check(file)
			if failure != nil {
				t.Fatal(failure)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := "build.cancelled"
			if scenario == "cancelled" {
				program = loweringCancellationProgram{program, cancel}
			} else {
				want = "build.limit"
				var span source.Span = file.Span(0, len(file.Text()))
				literalType, text := program.ResultType(), "1"
				if scenario == "lowering-limit" {
					span = generationLimitSpan{span, strings.Repeat("x", nativeplan.MaxModuleBytes+1)}
				} else {
					literalType, text = types.String(), strings.Repeat("\x00", nativeplan.MaxModuleBytes/18+1)
				}
				literal, err := ir.NewLiteral(literalType, text, span)
				if err != nil {
					t.Fatal(err)
				}
				returned, err := ir.NewReturn(literal, span)
				if err != nil {
					t.Fatal(err)
				}
				program, err = ir.NewProgram(file, []ir.Statement{returned}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(root, "program")
			if err := writeFile(output, []byte("previous artifact")); err != nil {
				t.Fatal(err)
			}
			_, failure = BuildLLVM(ctx, program, output, "darwin/arm64", device.SoftwareBuild, manifest)
			if failure == nil || string(failure.Data().Code) != want {
				t.Fatalf("generation diagnostic: got %v, want %s", failure, want)
			}
			if scenario == "emission-limit" && !strings.Contains(failure.Data().Message, "native module exceeds") {
				t.Fatalf("limit did not occur during emission: %v", failure)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "previous artifact" {
				t.Fatalf("generation failure changed output: %q, %v", data, err)
			}
		})
	}
}

func generationTestManifest(t *testing.T, root string) string {
	t.Helper()
	input := filepath.Join(root, "input")
	if err := writeFile(input, []byte("generation must fail before tool execution")); err != nil {
		t.Fatal(err)
	}
	pin := llvmFile{Path: "input", SHA256: snapshotTestDigest(t, input)}
	arguments, _ := llvmLinkRecipe("darwin/arm64", device.SoftwareBuild)
	record := llvmDistribution{ABI: nativeplan.ABIIdentity, LLVM: llvmVersion, Host: runtime.GOOS + "/" + runtime.GOARCH,
		Compiler: pin, Targets: map[string]llvmTarget{"darwin/arm64": {
			Triple: "aarch64-apple-darwin", Linker: pin, Runtime: pin,
			Inputs: map[string]llvmFile{"system": pin, "security": pin, "corefoundation": pin}, LinkArgs: arguments,
		}}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "toolchain.json")
	if err := writeFile(manifest, data); err != nil {
		t.Fatal(err)
	}
	return manifest
}
