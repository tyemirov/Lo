package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestF036DescendantOutputFailure(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	for _, scenario := range []string{"capture-quota", "forward-closed-pipe"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newF036Fixture(t)
			compiler, flags := llvmDarwinSDKHarness(t, fixture.work)
			build := exec.Command(compiler, append(flags, filepath.Join(repository, "testdata/native-runtime/f036-output-command.c"), "-o", fixture.command)...)
			build.Dir, build.Env = fixture.work, fixture.environment()
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build output child: %v\n%s", err, output)
			}
			binary, err := os.ReadFile(fixture.command)
			if err != nil {
				t.Fatal(err)
			}
			fixture.grant = fmt.Sprintf("%x,%s", sha256.Sum256(binary), fixture.command)
			source := strings.Replace(fixture.source("output-child"), "Main()->Int onError Recover", "Main()->Int!", 1)
			if scenario == "forward-closed-pipe" {
				source = strings.Replace(source, "result=commit command", `forwarded=Process.Command.with_output(command,"forward");result=commit forwarded`, 1)
			}
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", number: 0, source: source,
				harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true, harnessTransform: fixture.transform,
				run: func(t *testing.T, artifact, work string) {
					arguments := append(fixture.flags(t, artifact), "--process-kernel-limit", "4096")
					if scenario == "capture-quota" {
						arguments = append(arguments, "--max-process-output-bytes", "4")
					} else {
						arguments = append(arguments, "--process-forward-output")
					}
					command := exec.Command(artifact, arguments...)
					command.Dir, command.Env = fixture.work, fixture.environment()
					var stdout, stderr bytes.Buffer
					command.Stdout, command.Stderr = &stdout, &stderr
					if scenario == "forward-closed-pipe" {
						reader, writer, err := os.Pipe()
						if err != nil {
							t.Fatal(err)
						}
						if err := reader.Close(); err != nil {
							t.Fatal(err)
						}
						defer writer.Close()
						command.Stdout = writer
					}
					if err := command.Run(); err != nil {
						if _, exited := err.(*exec.ExitError); !exited {
							t.Fatal(err)
						}
					}
					actual := result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
					if actual.status != 2 || !strings.Contains(actual.stderr, "runtime.") {
						t.Fatalf("output failure was not terminal: %+v", actual)
					}
					if scenario == "capture-quota" && !strings.Contains(actual.stderr, "runtime.limit") {
						t.Fatalf("output quota classification: %+v", actual)
					}
					if scenario == "forward-closed-pipe" && !strings.Contains(actual.stderr, "runtime.host-failure") {
						t.Fatalf("closed output pipe classification: %+v", actual)
					}
					macCleanupPresent(t, fixture.ready, true)
					pids := map[string]bool{}
					for _, record := range fixture.tokenRecords(t) {
						if record[0] == "command" {
							pids[record[1][40:48]] = true
						}
					}
					if len(pids) < 2 {
						t.Fatal("output failure did not involve a real descendant")
					}
					var document struct {
						Diagnostics []struct {
							Progress runtimeValue `json:"process_progress"`
						} `json:"diagnostics"`
					}
					if err := json.Unmarshal([]byte(actual.stderr), &document); err != nil || len(document.Diagnostics) != 1 {
						t.Fatalf("output failure progress: %v; %+v", err, actual)
					}
					fields := processFields(t, document.Diagnostics[0].Progress)
					valueJSON(t, fields["cleanup_complete"], `true`)
					valueJSON(t, fields["stage"], `"stdout"`)
					valueJSON(t, fields["start_confirmed"], `true`)
					class := `"host"`
					if scenario == "capture-quota" {
						class = `"limit"`
					}
					valueJSON(t, fields["primary_class"], class)
					for _, field := range []string{"stdin_written", "stderr_bytes"} {
						valueJSON(t, fields[field], `"0"`)
					}
					stdoutCount := `"0"`
					if scenario == "capture-quota" {
						stdoutCount = `"4"`
					}
					valueJSON(t, fields["stdout_bytes"], stdoutCount)
					fixture.wait(t, "output-failure owner release", func() bool { return fixture.logged("C\n") && !fixture.lockBusy(t) })
					fixture.wait(t, "output-failure descendant termination", func() bool {
						for _, record := range fixture.tokenRecords(t) {
							if fixture.alive(record[1]) {
								return false
							}
						}
						return true
					})
				}}})
		})
	}
}
