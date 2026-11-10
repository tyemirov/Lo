package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestF036FinalReceiptStartsCleanupDeadline(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	fixture := newF036Fixture(t)
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "held-eof", number: 0,
		source:  strings.Replace(fixture.source("finish"), "Main()->Int onError Recover", "Main()->Int!", 1),
		harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
		transform: func(t *testing.T, module []byte) []byte {
			const entry = "define void @lo_entry("
			if strings.Count(string(module), entry) != 1 {
				t.Fatal("missing compiled entry")
			}
			return []byte(strings.Replace(string(module), entry, "define void @lo_eof_entry(", 1))
		},
		harnessTransform: func(t *testing.T, source []byte) []byte {
			text := string(fixture.transform(t, source))
			const entry = "int lo_test_host_init("
			if strings.Count(text, entry) != 1 {
				t.Fatal("missing provider host entry")
			}
			text = strings.Replace(text, entry, "int eof_provider_init(", 1)
			appendix, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/f036-receipt-eof.c"))
			if err != nil {
				t.Fatal(err)
			}
			return []byte(text + "\n" + string(appendix))
		},
		run: func(t *testing.T, artifact, work string) {
			actual := f036RunPolicyFixture(t, fixture, artifact, "--process-kernel-limit", fixture.kernelArgument())
			nativeFailure(t, actual, 2, "runtime.host-failure", "")
			macCleanupPresent(t, fixture.ready, true)
			if !fixture.logged("C\n") || !fixture.logged("final-receipt-before-held-eof\n") {
				t.Fatal("EOF hold did not follow completed command ownership and final receipt")
			}
			log, err := os.ReadFile(fixture.log)
			returned := strings.Index(string(log), "compiled-entry-returned\n")
			released := strings.Index(string(log), "held-eof-released\n")
			if err != nil || returned < 0 || (released >= 0 && returned > released) {
				t.Fatalf("Process waited through delayed EOF before returning to its compiled caller: %v; %q", err, log)
			}
			var report struct {
				Diagnostics []struct {
					Progress runtimeValue `json:"process_progress"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(actual.stderr), &report); err != nil || len(report.Diagnostics) != 1 {
				t.Fatalf("held EOF diagnostic: %v; %+v", err, actual)
			}
			fields := processFields(t, report.Diagnostics[0].Progress)
			valueJSON(t, fields["cleanup_complete"], `false`)
			valueJSON(t, fields["start_confirmed"], `true`)
			valueJSON(t, fields["stage"], `"release"`)
		}}})
}
