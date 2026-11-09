package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestF036LaunchEvidenceOrder(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	for _, scenario := range []struct {
		name   string
		number int
	}{{"failed-before-ready-after-lifecycle", 24}, {"ready-before-exec-notification", 25}, {"spawn-failed-before-child-exists", 26}} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			readPath, writePath := filepath.Join(fixture.work, "ready-read"), filepath.Join(fixture.work, "ready-write")
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", number: scenario.number,
				source: strings.Replace(fixture.source("finish"), "Main()->Int onError Recover", "Main()->Int!", 1), harness: "testdata/native-runtime/process-ownership-provider.c",
				hostBootstrap: "lo_test_host_init", sdkHarness: true,
				harnessTransform: func(t *testing.T, source []byte) []byte {
					text := string(fixture.transform(t, source))
					for before, after := range map[string]string{
						"int result=original(pid,path,actions,attributes,arguments,environment);": "int result=order_spawn_denied()?EAGAIN:original(pid,path,actions,attributes,arguments,environment);",
						"int lo_test_host_init(":                                       "int order_provider_init(",
						"fixture_emit(owner,&event,1);return 1;":                       "if(packet->kind==FIXTURE_LAUNCHER)order_defer(owner,&event);else fixture_emit(owner,&event,1);return 1;",
						"while(!__atomic_load_n(&owner->stopping,__ATOMIC_SEQ_CST)) {": "while(!__atomic_load_n(&owner->stopping,__ATOMIC_SEQ_CST)) { order_release(owner);",
					} {
						if strings.Count(text, before) != 1 {
							t.Fatalf("missing unique launch-order seam %q", before)
						}
						text = strings.Replace(text, before, after, 1)
					}
					appendix, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/f036-launch-order.c"))
					if err != nil {
						t.Fatal(err)
					}
					appendix = []byte(strings.NewReplacer("@ORDER_PROTOCOL@", strconv.Quote(filepath.Join(repository, "runtime/native/host/process/protocol.h")),
						"@ORDER_READY_READ@", strconv.Quote(readPath), "@ORDER_READY_WRITE@", strconv.Quote(writePath)).Replace(string(appendix)))
					return []byte("#include \"lo_process_ownership.h\"\nstruct fixture_owner;\nstatic int order_spawn_denied(void);\nstatic void order_defer(struct fixture_owner *,LoProcessOwnershipEvent *);\nstatic void order_release(struct fixture_owner *);\n" + text + "\n" + string(appendix))
				},
				run: func(t *testing.T, artifact, work string) {
					stdoutPath, stderrPath := filepath.Join(work, "stdout"), filepath.Join(work, "stderr")
					stdout, err := os.Create(stdoutPath)
					if err != nil {
						t.Fatal(err)
					}
					defer stdout.Close()
					stderr, err := os.Create(stderrPath)
					if err != nil {
						t.Fatal(err)
					}
					defer stderr.Close()
					command := exec.Command(artifact, append(fixture.flags(t, artifact), "--process-kernel-limit", "4096")...)
					command.Dir, command.Env = fixture.work, fixture.environment()
					command.Stdout, command.Stderr = stdout, stderr
					if err := command.Start(); err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() { done <- command.Wait() }()
					waited := false
					defer func() {
						fixture.teardown(t)
						if !waited {
							select {
							case <-done:
							case <-time.After(2 * time.Second):
								t.Error("launch fixture caller did not exit")
							}
						}
					}()
					if scenario.number == 25 {
						fixture.wait(t, "Ready received before EXEC", func() bool { _, err := os.Stat(readPath); return err == nil })
						if !fixture.logged("E-pending\n") || fixture.logged("E-delayed\n") {
							t.Fatal("EXEC was not held across Ready receipt")
						}
						macCleanupPresent(t, fixture.ready, false)
						if !fixture.lockBusy(t) {
							t.Fatal("Ready wait released transferred lock")
						}
						select {
						case err := <-done:
							waited = true
							t.Fatalf("Ready completed without EXEC: %v", err)
						default:
						}
						writeLLVMFixture(t, fixture.release, []byte("release"))
					}
					select {
					case <-done:
						waited = true
					case <-time.After(5 * time.Second):
						t.Fatal("launch fixture exceeded completion bound")
					}
					out, _ := os.ReadFile(stdoutPath)
					diagnostic, _ := os.ReadFile(stderrPath)
					actual := result{string(out), string(diagnostic), command.ProcessState.ExitCode()}
					macCleanupPresent(t, writePath, scenario.number != 26)
					if scenario.number == 25 {
						nativeValueCheck("Int", `"0"`)(t, actual)
						macCleanupPresent(t, fixture.ready, true)
						if !fixture.logged("E-delayed\n") {
							t.Fatal("delayed EXEC never reached controller")
						}
					} else {
						macCleanupPresent(t, fixture.ready, false)
						if scenario.number == 24 && (!fixture.logged("E-launcher\n") || !fixture.logged("X\n")) {
							t.Fatal("failed launcher lacked confirmed lifecycle evidence")
						}
						if scenario.number == 26 {
							if !fixture.logged("spawn-result=35\n") {
								t.Fatal("controlled spawn did not fail with EAGAIN")
							}
							for _, record := range fixture.tokenRecords(t) {
								if record[0] == "command" {
									t.Fatal("failed spawn created a command child")
								}
							}
						}
						var fields map[string]runtimeValue
						if scenario.number == 24 {
							report := receiptHostDiagnostic(t, actual)
							if actual.status != 1 || report.Error == nil || report.Error.Kind != "Process.ExecutionFailed" || report.Error.Input == nil {
								t.Fatalf("clean failed launcher did not preserve typed execution failure: %+v", actual)
							}
							input := processFields(t, *report.Error.Input)
							fields = processFields(t, input["progress"])
						} else {
							nativeFailure(t, actual, 2, "runtime.host-failure", "")
							var report struct {
								Diagnostics []struct {
									Progress runtimeValue `json:"process_progress"`
								} `json:"diagnostics"`
							}
							if err := json.Unmarshal(diagnostic, &report); err != nil || len(report.Diagnostics) != 1 {
								t.Fatalf("spawn failure progress: %v; %+v", err, actual)
							}
							fields = processFields(t, report.Diagnostics[0].Progress)
						}
						valueJSON(t, fields["stage"], `"start"`)
						valueJSON(t, fields["start_confirmed"], strconv.FormatBool(scenario.number == 24))
						valueJSON(t, fields["cleanup_complete"], `true`)
					}
					fixture.wait(t, "proven launch owner release", func() bool { return fixture.logged("C\n") && !fixture.lockBusy(t) })
				}}})
		})
	}
}
