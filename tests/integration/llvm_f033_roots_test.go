package integration_test

import (
	"strings"
	"testing"
)

func TestLLVMF033ExactRootWitnesses(t *testing.T) {
	runLLVMRuntimeScenarios(t, f033ExactRootScenarios())
}

func TestLLVMLinuxF033ExactRootWitnesses(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f033ExactRootScenarios())
}

func f033ExactRootScenarios() []llvmRuntimeScenario {
	var scenarios []llvmRuntimeScenario
	for index, test := range []struct{ name, operation, expected string }{
		{"File.directory", "File.directory", "runtime.capability-denied"},
		{"File.list", "File.list", "runtime.capability-denied"},
		{"File.scan", "File.scan", "runtime.capability-denied"},
		{"File.System.usage", "File.System.usage", "runtime.capability-denied"},
		{"inert-root-replaced-ancestor", "File.directory", "runtime.capability-denied"},
		{"vanished-root-replaced-ancestor", "File.directory", "runtime.capability-denied"},
		{"inert-root-unchanged-ancestor", "File.directory", "File.DirectoryAbsent"},
		{"vanished-root-unchanged-ancestor", "File.directory", "File.DirectoryAbsent"},
		{"inert-root-symlinked-ancestor", "File.directory", "runtime.capability-denied"},
		{"directory-error-replaced-ancestor", "File.directory", "runtime.capability-denied"},
		{"list-error-replaced-ancestor", "File.list", "runtime.capability-denied"},
		{"scan-error-replaced-ancestor", "File.scan", "runtime.capability-denied"},
		{"usage-error-replaced-ancestor", "File.System.usage", "runtime.capability-denied"},
		{"invalid-provider-replaced-ancestor", "File.directory", "runtime.host-failure"},
		{"cancelled-replaced-ancestor", "File.directory", "runtime.cancelled"},
		{"list-replacement-restored-by-next-callback", "File.list", "runtime.capability-denied"},
		{"scan-directory-replacement-restored-by-entry", "File.scan", "runtime.capability-denied"},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: test.name, source: `return ` + test.operation + `(File.Path.new("ancestor/root"))`,
			number: index, harness: "testdata/native-runtime/f033-exact-roots.c",
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				const definition = "define void @lo_entry("
				if !strings.Contains(string(module), definition) {
					t.Fatal("missing program entry")
				}
				return []byte(strings.Replace(string(module), definition, "define void @lo_f033_entry(", 1))
			},
			verify: func(t *testing.T, actual result) {
				if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, test.expected) {
					t.Fatalf("%s: %+v; expected %s", test.name, actual, test.expected)
				}
			},
		})
	}
	return scenarios
}

func TestLLVMF033MetadataSessionRegistration(t *testing.T) {
	runLLVMRuntimeScenarios(t, f033MetadataSessionScenarios())
}

func TestLLVMLinuxF033MetadataSessionRegistration(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f033MetadataSessionScenarios())
}

func f033MetadataSessionScenarios() []llvmRuntimeScenario {
	var scenarios []llvmRuntimeScenario
	for index, test := range []struct{ name, source string }{
		{"first-hook-after-peer-evaluation", "return true"},
		{"first-hook-after-peer-creation", `flow Main() -> Int! { directory = File.directory(File.Path.new(".")); return directory.owner_uid }; return Main()`},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: test.name, source: test.source, number: index,
			harness: "testdata/native-runtime/f033-metadata-session.c",
			transform: func(t *testing.T, module []byte) []byte {
				return []byte(strings.Replace(string(module), "define void @lo_entry(", "define void @lo_f033_entry(", 1))
			},
			verify: func(t *testing.T, actual result) {
				if actual.status != 0 || actual.stderr != "" || (index == 1 && !strings.Contains(actual.stdout, `"value":"777"`)) {
					t.Fatalf("metadata session %s: %+v", test.name, actual)
				}
			},
		})
	}
	return scenarios
}
