package integration_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLLVMAdmittedProgramSessions(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{{
		name: "fresh-session-lifetime", source: `return Math.add(1, 2)`,
		harness: "testdata/native-qualification/program-sessions.c",
	}, {
		name: "metadata-rejection", source: `return Math.add(1, 2)`, number: 1,
		harness: "testdata/native-qualification/program-sessions.c",
		transform: func(t *testing.T, module []byte) []byte {
			return rewriteLLVMMetadata(t, module, func(metadata map[string]json.RawMessage) {
				metadata["abi"] = json.RawMessage(`"invalid-program-abi"`)
			})
		},
	}})
}

func TestLLVMAdmittedLiteralFailureOrder(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires LLVM runtime inputs")
	}
	large := "1" + strings.Repeat("0", 315654)
	for _, fixture := range []struct {
		name, source, code string
		status             int
	}{
		{"unreached", "flow Unused()->Int{return " + large + "};return 1", "", 0},
		{"reached", "return " + large, "runtime.limit", 2},
		{"earlier-host-failure", "flow Main()->Int!{data=File.read(File.Path.new(\"ungranted\"));return " + large + "};return Main()", "runtime.capability-denied", 2},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := sourceFile(t, fixture.source)
			reference := runExecutable(t, binary, "run", path, "--format", "json")
			artifact, built := buildForTest(t, path)
			if built.status != 0 {
				t.Fatalf("literal fixture build: %+v", built)
			}
			got := runExecutable(t, artifact, "run", "--format", "json")
			for name, observed := range map[string]result{"interpreter": reference, "native": got} {
				if observed.status != fixture.status {
					t.Fatalf("%s literal failure order: %+v", name, observed)
				}
				if fixture.code != "" {
					if diagnostics(t, observed.stderr).Diagnostics[0].Code != fixture.code {
						t.Fatalf("%s literal failure code: %+v", name, observed)
					}
				} else {
					var output struct{ Result runtimeValue }
					if err := json.Unmarshal([]byte(observed.stdout), &output); err != nil {
						t.Fatal(err)
					}
					valueJSON(t, output.Result, `"1"`)
				}
			}
		})
	}
}
