package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestF031JSONDeepLifetime(t *testing.T) {
	runLLVMRuntimeScenarios(t, f031LifetimeScenarios())
}

func TestF031LinuxJSONDeepLifetime(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f031LifetimeScenarios())
}

func f031LifetimeScenarios() []llvmRuntimeScenario {
	const depth = 16384
	document := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
	wantTree := strings.Repeat(`{"kind":"array","value":[`, depth) + `{"kind":"number","value":"0"}` + strings.Repeat(`]}`, depth)
	decode := f031Decode(document)
	var scenarios []llvmRuntimeScenario
	for index, source := range []string{
		`return ` + decode,
		`flow Fail(value:JSON.Value)->Int!{return Error.new("Fixture.DeepJSON","deep",Record.new(value:value))};flow Main()->Int!{return Fail(` + decode + `)};return Main()`,
		`flow Caught(error:Error)->String{return "caught"};flow Main()->String onError Caught{return JSON.kind(` + decode + `)};return Main()`,
	} {
		name := []string{"raw-output-and-release", "diagnostic-snapshot-and-release", "cancelled-decode-and-release"}[index]
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: name, source: source, number: index, harness: "testdata/native-runtime/f031-json-lifetime.c",
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				const operation = "@lo_op_JSON_2Edecode("
				if !strings.Contains(string(module), operation) {
					t.Fatal("missing JSON.decode boundary")
				}
				return []byte(strings.ReplaceAll(string(module), operation, "@lo_f031_decode("))
			},
			verify: func(t *testing.T, actual result) {
				if actual.status != 0 || actual.stderr != "" {
					t.Fatalf("deep JSON lifecycle: status=%d stderr=%s", actual.status, actual.stderr)
				}
				if index == 2 {
					if !json.Valid([]byte(actual.stdout)) || !strings.Contains(actual.stdout, "runtime.cancelled") || strings.Contains(actual.stdout, `"value":"caught"`) {
						t.Fatalf("JSON cancellation became recoverable: %+v", actual)
					}
					return
				}
				if !strings.Contains(actual.stdout, wantTree) {
					t.Fatalf("deep JSON output was truncated at %d arrays", strings.Count(actual.stdout, `"kind":"array"`))
				}
				if index == 1 && !strings.Contains(actual.stdout, "Fixture.DeepJSON") {
					t.Fatalf("deep snapshot lost operation error: %+v", actual)
				}
			},
		})
	}
	return scenarios
}
