package integration_test

import (
	"testing"
	"time"
)

func TestLLVMConsoleReadinessCancellation(t *testing.T) {
	source := `flow ServeHandle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200,"ok") }; flow ServeError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500,error.kind) }; server=HTTP.Server.new(0,ServeHandle,ServeError);commit server`
	verify := func(t *testing.T, got result) {
		t.Helper()
		if got.status != 0 || got.stdout != "" || got.stderr != "" {
			t.Fatalf("serve readiness completion: %+v", got)
		}
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{
		{name: "stop-before-ready-acknowledgment", source: source, harness: "testdata/native-runtime/console-ready-cancellation.c", number: 0, captureOutput: true, timeout: 8 * time.Second, verify: verify},
		{name: "fresh-stopped-event-deadline", source: source, harness: "testdata/native-runtime/console-ready-cancellation.c", number: 1, captureOutput: true, timeout: 8 * time.Second, verify: verify},
	})
}
