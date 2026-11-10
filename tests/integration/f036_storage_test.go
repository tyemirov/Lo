package integration_test

import (
	"runtime"
	"testing"
)

func TestF036StorageAdmissionPrecedesLaunch(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	for _, scenario := range []struct{ name, value, live string }{
		{"value-budget", "1048576", "0"}, {"live-budget", "0", "1048576"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", number: 0, source: fixture.source("finish"),
				harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true, harnessTransform: fixture.transform,
				run: func(t *testing.T, artifact, work string) {
					actual := f036RunPolicyFixture(t, fixture, artifact, "--max-process-tokens", "4294967295",
						"--max-value-bytes", scenario.value, "--max-live-bytes", scenario.live, "--process-kernel-limit", fixture.kernelArgument())
					nativeFailure(t, actual, 2, "runtime.limit", "")
					macCleanupPresent(t, fixture.ready, false)
					if fixture.logged("O\n") || fixture.lockBusy(t) {
						t.Fatal("failed storage admission launched or retained a remote owner")
					}
					for _, record := range fixture.tokenRecords(t) {
						if record[0] != "caller" {
							t.Fatalf("storage rejection created a process: %s", record[0])
						}
					}
				}}})
		})
	}
}
