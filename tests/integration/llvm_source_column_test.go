package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLLVMSourceColumnOverflow(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM executable lane")
	}
	prefix := `padding="` + strings.Repeat("界", 1<<16) + `"; return `
	column := utf8.RuneCountInString(prefix) + 1
	path := sourceFile(t, prefix+"7")
	artifact, built := buildForTest(t, path)
	if built.status != 0 || built.stderr != "" {
		t.Fatalf("build long single-line source: %+v", built)
	}
	got := runExecutable(t, artifact, "run", "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("run long single-line source: %+v", got)
	}
	var envelope struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, successfulValue(t, envelope.Result), `"7"`)

	output := filepath.Join(t.TempDir(), "program.ll")
	emitted := runExecutable(t, binary, "build", path, "--emit", "llvm", "--output", output, "--format", "json")
	if emitted.status != 0 || emitted.stderr != "" {
		t.Fatalf("emit long single-line source: %+v", emitted)
	}
	module, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	constant := regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2}|[^"\\])*)"`).FindSubmatch(module)
	if constant == nil {
		t.Fatal("missing runtime metadata")
	}
	metadata, err := decodeLLVMBytes(constant[1])
	if err != nil {
		t.Fatal(err)
	}
	locations := metadataLocations(t, metadata)
	for _, location := range locations {
		if location.Action == "literal" && location.Text == "7" {
			if location.Start.Line != 1 || location.Start.Column != column {
				t.Fatalf("runtime source position lost: %+v, want column %d", location, column)
			}
			return
		}
	}
	t.Fatal("runtime metadata omits the final integer literal")
}
