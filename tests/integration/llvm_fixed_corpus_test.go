package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
)

func TestLLVMFixedCorpusModuleLimit(t *testing.T) {
	for _, fixture := range corpus.Cases() {
		if fixture.Name != "map_10000" {
			continue
		}
		for _, target := range []string{"darwin/arm64", "linux/arm64"} {
			t.Run(target, func(t *testing.T) {
				output := filepath.Join(t.TempDir(), "program.ll")
				got := runExecutable(t, binary, "build", sourceFile(t, fixture.Source), "--emit", "llvm", "--target", target, "--output", output, "--format", "json")
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("fixed performance corpus emission: %+v", got)
				}
				data, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if len(data) > 64<<20 {
					t.Fatalf("module exceeds construction budget: %d", len(data))
				}
			})
		}
	}
}

func TestLLVMFixedCorpusExecutable(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires LLVM runtime inputs")
	}
	target := os.Getenv("LO_TEST_TARGET")
	if target == "" {
		target = runtime.GOOS + "/" + runtime.GOARCH
	}
	for _, fixture := range corpus.Cases() {
		if fixture.Name != "map_10000" {
			continue
		}
		declaration, body, ok := strings.Cut(fixture.Source, "\n")
		if !ok {
			t.Fatal("missing callback declaration")
		}
		for _, source := range []struct{ name, text string }{{"entry", fixture.Source}, {"large-flow", declaration + "\nflow Main() -> Int {\n" + body + "}\nreturn Main()\n"}} {
			t.Run(source.name, func(t *testing.T) {
				artifact := filepath.Join(t.TempDir(), "program")
				got := runExecutable(t, binary, "build", sourceFile(t, source.text), "--toolchain", os.Getenv("LO_LLVM_TOOLCHAIN"), "--target", target, "--output", artifact, "--format", "json")
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("fixed corpus compile and link: %+v", got)
				}
				if target == "linux/arm64" {
					got = runLinuxLLVMExecutable(t, artifact, "run", "--max-steps", "10000000")
				} else {
					got = runExecutable(t, artifact, "run", "--max-steps", "10000000")
				}
				if got.status != 0 || got.stdout != fixture.Expected+"\n" || got.stderr != "" {
					t.Fatalf("fixed corpus result: %+v, want %s", got, fixture.Expected)
				}
			})
		}
	}
}
