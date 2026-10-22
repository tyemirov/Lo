package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func captureRegressionHarness(t *testing.T, source []byte, status int) []byte {
	t.Helper()
	text := string(source)
	// Keep the fixture's real capture and CLI invocation; replace only device-specific expectations.
	start := strings.Index(text, " int expected=")
	if start < 0 {
		t.Fatal("capture regression expectation boundary missing")
	}
	text = text[:start] + fmt.Sprintf(" if(status!=%d)return 96; if(write(1,contents,(Size)length)!=length)return 97; return 0;\n}\n", status)
	text = strings.Replace(text, `"--write-root","."`, `"--write-root",".","--read-root","."`, 1)
	text = strings.Replace(text, "lo_cli_main(8,args,0)", "lo_cli_main(10,args,0)", 1)
	return []byte(text)
}

func TestLLVMFixtureCaptureLargeStreams(t *testing.T) {
	const tail = "CAPTURE_FINAL_TRAILER"
	payload := strings.Repeat("A", 2<<20) + tail
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			payload := payload
			source := fmt.Sprintf("return %q", payload)
			status := 0
			if stream == "stderr" {
				payload = strings.Repeat("A", 512<<10) + tail
				source = fmt.Sprintf("flow Main()->Bytes!{return File.read(File.Path.new(%q))};return Main()", payload)
				status = 1
			}
			scenario := llvmRuntimeScenario{name: stream, source: source, number: 0, harness: "testdata/native-runtime/device-resources.c", captureOutput: true, timeout: 5 * time.Second,
				harnessTransform: func(t *testing.T, module []byte) []byte { return captureRegressionHarness(t, module, status) },
				verify: func(t *testing.T, got result) {
					if got.status != 0 || got.stderr != "" {
						t.Fatalf("capture exit=%d stderr=%q", got.status, got.stderr)
					}
					t.Logf("complete %s capture bytes=%d", stream, len(got.stdout))
					if stream == "stdout" {
						expected := `{"command":"run","status":"ok","result":{"type":"String","value":"` + payload + "\"}}\n"
						if got.stdout != expected {
							t.Fatalf("complete stdout bytes=%d want=%d", len(got.stdout), len(expected))
						}
					} else {
						var output struct {
							Diagnostics []struct {
								Error struct {
									Kind  string `json:"kind"`
									Input struct {
										Value map[string]struct {
											Value string `json:"value"`
										} `json:"value"`
									} `json:"input"`
								} `json:"error"`
							} `json:"diagnostics"`
						}
						if err := json.Unmarshal([]byte(got.stdout), &output); err != nil {
							t.Fatal(err)
						}
						if len(output.Diagnostics) != 1 || output.Diagnostics[0].Error.Kind != "File.ReadFailed" || output.Diagnostics[0].Error.Input.Value["file_path"].Value != payload || len(got.stdout) <= 2<<20 {
							t.Fatalf("incomplete stderr diagnostic bytes=%d", len(got.stdout))
						}
					}
				}}
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
		})
	}
}

func TestLLVMFixtureCaptureOverflow(t *testing.T) {
	payload := strings.Repeat("A", 2<<20) + "OVERFLOW_TRAILER"
	source := fmt.Sprintf("payload=%q;return List.new(payload,payload,payload)", payload)
	scenario := llvmRuntimeScenario{name: "explicit-overflow", source: source, number: 0, harness: "testdata/native-runtime/device-resources.c", captureOutput: true, timeout: 5 * time.Second,
		harnessTransform: func(t *testing.T, body []byte) []byte { return captureRegressionHarness(t, body, 0) },
		verify: func(t *testing.T, got result) {
			if got.status != 15 || got.stdout != "" || got.stderr != "" {
				t.Fatalf("overflow: exit=%d stdout bytes=%d stderr=%q", got.status, len(got.stdout), got.stderr)
			}
		}}
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

// The host-to-Linux route must stage the opt-in file without a package change.
func TestLLVMLinuxFixtureCaptureFile(t *testing.T) {
	scenarios := llvmDeviceResourceScenarios()
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", []llvmRuntimeScenario{scenarios[1]})
}
