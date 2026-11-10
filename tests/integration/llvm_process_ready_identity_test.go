package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLLVMProcessReadyIdentity(t *testing.T) {
	for number, scenario := range []struct {
		name string
		word int
	}{
		{"current-token", -1}, {"changed-execution-version", 7}, {"changed-pid", 5},
		{"changed-audit-user", 0}, {"changed-effective-user", 1}, {"changed-effective-group", 2},
		{"changed-real-user", 3}, {"changed-real-group", 4}, {"changed-audit-session", 6},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			marker := fileFixture(t, fixture.writes, "command-effect", "preserved")
			evidence := fileFixture(t, fixture.writes, "ready-evidence", "")
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "default-transport", number: number,
				source: fixture.source(t, "write", marker), harness: "testdata/native-runtime/process-ready-identity.c",
				hostBootstrap: "lo_test_host_init", sdkHarness: true,
				harnessTransform: func(t *testing.T, source []byte) []byte {
					return []byte(strings.NewReplacer(
						"@READY_PROTOCOL@", strconv.Quote(filepath.Join(repository, "runtime/native/host/process/protocol.h")),
						"@READY_EVIDENCE@", strconv.Quote(evidence)).Replace(string(source)))
				},
				run: func(t *testing.T, artifact, work string) {
					deadline, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					command := exec.CommandContext(deadline, artifact, "run", "--format", "json",
						"--process-allow", fixture.grant, "--process-launcher", artifact,
						"--process-read-root", fixture.root,
						"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes),
						"--max-process-ms", "1500", "--max-process-cleanup-ms", "500")
					command.Dir = fixture.root
					command.Env = []string{"CI=1", "PATH=", "TMPDIR=" + work, "TMP=" + work, "TEMP=" + work, "LC_ALL=C", "LANG=C"}
					var stdout, stderr bytes.Buffer
					command.Stdout, command.Stderr = &stdout, &stderr
					if err := command.Run(); err != nil {
						if _, exited := err.(*exec.ExitError); !exited {
							t.Fatal(err)
						}
					}
					if deadline.Err() != nil {
						t.Fatalf("Ready identity fixture exceeded its deadline: %s", stderr.String())
					}
					got := result{stdout: stdout.String(), stderr: stderr.String(), status: command.ProcessState.ExitCode()}
					wire, err := os.ReadFile(evidence)
					if err != nil || len(wire) != 76 {
						t.Fatalf("real launcher did not publish exactly one Ready condition: %x %v; %+v", wire, err, got)
					}
					want := append([]byte(nil), wire[:38]...)
					if scenario.word >= 0 {
						want[6+4*scenario.word+3] ^= 1
					}
					if !bytes.Equal(wire[38:], want) {
						t.Fatalf("Ready fixture changed the wrong token field: %x", wire)
					}
					contents, err := os.ReadFile(marker)
					if err != nil {
						t.Fatal(err)
					}
					if scenario.word < 0 {
						nativeValueCheck("Int", `"0"`)(t, got)
						if string(contents) != "changed" {
							t.Fatalf("verified Ready did not permit the real command: %q", contents)
						}
						return
					}
					nativeFailure(t, got, 2, "runtime.host-failure", "")
					if string(contents) != "preserved" {
						t.Fatalf("invalid Ready permitted command effects: %q", contents)
					}
					var diagnostic struct {
						Diagnostics []struct {
							Progress runtimeValue `json:"process_progress"`
						}
					}
					if err := json.Unmarshal([]byte(got.stderr), &diagnostic); err != nil || len(diagnostic.Diagnostics) != 1 {
						t.Fatalf("invalid Ready diagnostic: %v; %+v", err, got)
					}
					progress := processFields(t, diagnostic.Diagnostics[0].Progress)
					valueJSON(t, progress["stage"], `"start"`)
					valueJSON(t, progress["cleanup_complete"], `true`)
					valueJSON(t, progress["stdout_bytes"], `"0"`)
					valueJSON(t, progress["stderr_bytes"], `"0"`)
				},
			}})
		})
	}
}
