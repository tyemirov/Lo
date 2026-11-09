package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This lane compiles the exact native SDK adapter into a public host. Only SDK
// calls, callback delivery, and the AUTH clock are injected. The controller, command
// transport, real process lifecycle, authority, and lock transfer remain active.
func TestF036NativeSDKAdapter(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the Darwin SDK and audit-token process fixture")
	}
	for _, scenario := range []struct {
		name, operation string
		number          int
		admission       bool
		quarantine      bool
		required        []string
	}{
		{name: "uncached-borrowed-auth-and-late-reply", operation: "fork", number: 0,
			required: []string{"sdk-admitted\n", "sdk-subscribe\n", "sdk-allow-uncached\n", "sdk-late-response-rejected\n", "sdk-library-closed\n"}},
		{name: "subscription-failure", operation: "finish", number: 1, admission: true,
			required: []string{"sdk-subscribe\n", "sdk-delete\n", "sdk-library-closed\n"}},
		{name: "fail-closed-setup-failure", operation: "finish", number: 2, admission: true,
			required: []string{"sdk-set-deadline\n", "sdk-delete\n", "sdk-library-closed\n"}},
		{name: "client-admission-failure", operation: "finish", number: 3, admission: true,
			required: []string{"sdk-new\n", "sdk-library-closed\n"}},
		{name: "required-sdk-symbol-absent", operation: "finish", number: 4, admission: true,
			required: []string{"sdk-library-closed\n"}},
		{name: "inverted-muting-rejected", operation: "finish", number: 5, admission: true,
			required: []string{"sdk-unmute-paths\n", "sdk-unmute-targets\n", "sdk-delete\n", "sdk-library-closed\n"}},
		{name: "auth-deadline-close-busy-and-late-reply", operation: "pause", number: 6, quarantine: true,
			required: []string{"sdk-close-auth-busy\n", "sdk-deadline-denial\n", "sdk-borrow-ended\n", "sdk-late-response-rejected\n"}},
		{name: "queued-auth-during-delete", operation: "finish", number: 7, quarantine: true,
			required: []string{"sdk-delete\n", "sdk-deny-uncached\n", "sdk-close-callback-joined\n", "sdk-library-closed\n"}},
		{name: "delete-failure-retains-owner", operation: "finish", number: 8, quarantine: true,
			required: []string{"sdk-delete\n"}},
		{name: "sdk-auth-response-failure", operation: "finish", number: 9, quarantine: true,
			required: []string{"sdk-admitted\n", "sdk-late-response-rejected\n"}},
		{name: "synchronization-admission-failure", operation: "finish", number: 10, quarantine: true,
			required: []string{"sdk-admitted\n"}},
		{name: "deadline-readback-mismatch", operation: "finish", number: 11, admission: true,
			required: []string{"sdk-set-deadline\n", "sdk-delete\n", "sdk-library-closed\n"}},
		{name: "muted-inventory-failure", operation: "finish", number: 12, admission: true,
			required: []string{"sdk-delete\n", "sdk-library-closed\n"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			events, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/process-ownership-provider.c"))
			if err != nil {
				t.Fatal(err)
			}
			const registration = "lo_process_ownership_provider(fixture_open,36)"
			if strings.Count(string(events), registration) != 1 {
				t.Fatal("controlled SDK event-source registration changed")
			}
			events = []byte(strings.Replace(string(events), registration, "lo_process_ownership_provider(sdk_native_open,36)", 1))
			transform := func(t *testing.T, source []byte) []byte {
				t.Helper()
				source = []byte(strings.NewReplacer(
					"@F036_EVENT_SOURCE@", string(events),
					"@F036_SDK_CASE@", strconv.Itoa(scenario.number),
					"@F036_COMMAND@", strconv.Quote(fixture.command),
					"@F036_ADAPTER_SOURCE@", strconv.Quote(filepath.Join(repository, "runtime/native/src/process_es.c")),
				).Replace(string(source)))
				return fixture.transform(t, source)
			}
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "compiled-host", source: fixture.source(scenario.operation),
				number: 0, harness: "testdata/native-runtime/process-es-sdk.c", hostBootstrap: "lo_test_host_init",
				sdkHarness: true, harnessTransform: transform,
				run: func(t *testing.T, artifact, work string) {
					actual := f036RunPolicyFixture(t, fixture, artifact, "--process-kernel-limit", fixture.kernelArgument())
					if scenario.number == 0 {
						nativeValueCheck("Int", `"0"`)(t, actual)
					} else if actual.status != 2 || !strings.Contains(actual.stderr, "runtime.") {
						t.Fatalf("SDK boundary failure was not terminal: %+v", actual)
					}
					if scenario.admission && !strings.Contains(actual.stderr, "runtime.process-unavailable") {
						t.Fatalf("failed SDK setup did not report unavailable admission: %+v", actual)
					}
					for _, required := range scenario.required {
						fixture.wait(t, required, func() bool { return fixture.logged(required) })
					}
					log, err := os.ReadFile(fixture.log)
					if err != nil {
						t.Fatal(err)
					}
					if scenario.admission {
						if strings.Contains(string(log), "sdk-admitted\n") || strings.Contains(string(log), "sdk-allow-uncached\n") {
							t.Fatalf("failed SDK setup admitted a command: %s", log)
						}
						macCleanupPresent(t, fixture.ready, false)
						for _, record := range fixture.tokenRecords(t) {
							if record[0] == "command" {
								t.Fatalf("failed admission launched a command: %s", log)
							}
						}
					}
					if scenario.number == 10 {
						macCleanupPresent(t, fixture.ready, false)
						for _, record := range fixture.tokenRecords(t) {
							if record[0] == "command" {
								t.Fatal("failed SDK synchronization launched a command")
							}
						}
					}
					if scenario.quarantine {
						if !fixture.lockBusy(t) {
							t.Fatal("unproven SDK lifecycle released the transferred lock")
						}
						if scenario.number == 8 && (strings.Count(string(log), "sdk-delete\n") != 1 || strings.Contains(string(log), "sdk-library-closed\n")) {
							t.Fatalf("failed SDK deletion was retried or library released: %s", log)
						}
					} else {
						fixture.wait(t, "caller lock release", func() bool { return !fixture.lockBusy(t) })
					}
					if scenario.number == 0 {
						fixture.wait(t, "all observed processes exit", func() bool {
							for _, record := range fixture.tokenRecords(t) {
								if fixture.alive(record[1]) {
									return false
								}
							}
							return true
						})
					}
				}}})
		})
	}
}
