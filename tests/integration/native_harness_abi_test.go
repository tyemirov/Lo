package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func TestNativeHarnessCLIABI(t *testing.T) {
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if address := os.Getenv("LO_TEST_SESSION"); address != "" {
		inputs, err := testsession.Inputs(context.Background(), address)
		if err != nil {
			t.Fatal(err)
		}
		manifest = inputs.Manifest
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var distribution struct {
		Compiler struct{ Path string }
	}
	if err := json.Unmarshal(data, &distribution); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsLocal(distribution.Compiler.Path) {
		t.Fatal("harness compiler requires a local distribution member")
	}
	compiler, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(manifest), distribution.Compiler.Path))
	if err != nil {
		t.Fatal(err)
	}
	if relative, err := filepath.Rel(repository, compiler); err != nil || !filepath.IsLocal(relative) {
		t.Fatal("harness compiler leaves Lo")
	}
	triple := map[string]string{"darwin": "aarch64-apple-darwin", "linux": "aarch64-unknown-linux-gnu"}[runtime.GOOS]
	for _, fixture := range []struct {
		name, source, diagnostic string
	}{
		{"three-arguments", `int call(void){const char *args[]={"fixture","metadata",0};return lo_cli_main(2,args,0);}`, ""},
		{"reject-two-arguments", `int call(void){const char *args[]={"fixture","metadata",0};return lo_cli_main(2,args);}`, "too few arguments"},
		{"reject-two-argument-pointer", `int (*entry)(int,const char *const*)=lo_cli_main;`, "incompatible function pointer"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if address := os.Getenv("LO_TEST_SESSION"); address != "" {
				lease, err := testsession.Acquire(context.Background(), address)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := lease.Close(); err != nil {
						t.Error(err)
					}
				}()
			}
			input := sourceFile(t, fixture.source)
			got := runExecutable(t, compiler, "-target", triple, "-fsyntax-only", "-std=c11", "-Werror", "-nostdinc",
				"-include", filepath.Join(repository, "testdata/native-runtime/cli-entry.h"), "-x", "c", input)
			if fixture.diagnostic == "" {
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("canonical CLI ABI rejected: %+v", got)
				}
			} else if got.status == 0 || !strings.Contains(got.stderr, fixture.diagnostic) {
				t.Fatalf("invalid CLI ABI accepted or misdiagnosed: %+v", got)
			}
		})
	}
	harness := locationMetadataHarness(t, `
#include "cli-entry.h"
int main(void){const char *args[]={"fixture","run","--format","json",0};return lo_cli_main(4,args,0);}
`)
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{
		name: "native-cli-execution", source: "return 1", harness: harness,
		verify: func(t *testing.T, got result) {
			value := nativeValue(t, got)
			if value.Type != "Int" {
				t.Fatalf("canonical CLI ABI returned %s", value.Type)
			}
			valueJSON(t, value, `"1"`)
		},
	}})
}
