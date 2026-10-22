package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLLVMExecutableReproducibility(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	compiler, _ := controlledLLVMTools(t)
	source := sourceFile(t, `return String.concat("stable ", "é")`)
	for _, target := range []string{"darwin/arm64", "linux/arm64"} {
		t.Run(strings.ReplaceAll(target, "/", "-"), func(t *testing.T) {
			var artifacts []string
			for _, name := range []string{"first", "second artifact"} {
				output := filepath.Join(t.TempDir(), name)
				got := runExecutable(t, compiler, "build", source, "--toolchain", os.Getenv("LO_LLVM_TOOLCHAIN"), "--target", target, "--output", output, "--format", "json")
				if got.status != 0 {
					t.Fatalf("LLVM build: %+v", got)
				}
				assertNativeObjectTarget(t, output, strings.Split(target, "/")[0])
				artifacts = append(artifacts, output)
			}
			first, err := os.ReadFile(artifacts[0])
			if err != nil {
				t.Fatal(err)
			}
			second, err := os.ReadFile(artifacts[1])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatal("identical source and target produced different artifact bytes")
			}
			if target == runtime.GOOS+"/"+runtime.GOARCH {
				for _, artifact := range artifacts {
					got := runExecutable(t, artifact, "run")
					if got.status != 0 || got.stdout != "stable é\n" || got.stderr != "" {
						t.Fatalf("reproducible artifact result: %+v", got)
					}
				}
			}
		})
	}
}
