package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestF036CleanupBoundaryOwnership(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and descriptor fixtures")
	}
	for number, scenario := range []struct {
		name, injection, cleanup string
		complete                 bool
	}{
		{"malformed-receipt-retains-freeze", "boundary-receipt-corrupted", "", false},
		{"channel-setup-close-failure-retains-freeze", "boundary-setup-failed", "Process pipe release failed", false},
		{"owner-thread-failure-releases-capture", "boundary-thread-failed", "", true},
		{"owner-thread-close-failure-retains-freeze", "boundary-thread-failed", "Process resource release failed", false},
		{"channel-setup-clean-release", "boundary-setup-failed", "", true},
		{"inheritance-close-failure-retains-freeze", "boundary-setup-failed", "Process pipe release failed", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			later := filepath.Join(fixture.root, "later-lock")
			writeLLVMFixture(t, later, nil)
			source := fmt.Sprintf(`
flow ProbeLater()->Bool onError Denied{target=Lock.File.new(File.Path.new(%q));lease=commit target;return false}
flow ProbeSame()->Bool onError Denied{target=Lock.File.new(File.Path.new(%q));lease=commit target;return false}
flow Denied(error:Error)->Bool{return Match.new(String.eq(error.kind,"Lock.Failed"),true:String.starts_with(error.message,"file lock acquisition is frozen by active remote owner "),_:false)}
flow Main()->Int!{lock=Lock.File.new(File.Path.new(%q));lease=commit lock;context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));command=Process.Command.new(context,executable,List.new(%q,"finish",%q,%q,"initial"),Bytes.new(""));result=commit command;return 0};return Main()
`, later, fixture.lock, fixture.lock, fixture.work, fixture.command, fixture.socket, fixture.ready, fixture.release)
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-shared-session", number: 0,
				source: source, harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
				transform: func(t *testing.T, module []byte) []byte {
					return []byte(strings.Replace(string(module), "define void @lo_entry(", "define void @lo_f036_boundary_entry(", 1))
				},
				harnessTransform: func(t *testing.T, provider []byte) []byte {
					extra, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/f036-cleanup-boundaries.c"))
					if err != nil {
						t.Fatal(err)
					}
					text := string(fixture.transform(t, provider))
					for _, replacement := range [][2]string{
						{"int main(int argc,char **argv,char **environment)", "int f036_boundary_provider_main(int argc,char **argv,char **environment)"},
						{"int lo_test_host_init(int argc,char **argv,char **environment)", "int f036_boundary_provider_init(int argc,char **argv,char **environment)"},
					} {
						if strings.Count(text, replacement[0]) != 1 {
							t.Fatal("cleanup boundary fixture did not bind its public host entry")
						}
						text = strings.Replace(text, replacement[0], replacement[1], 1)
					}
					return []byte(text + "\n" + strings.NewReplacer("@F036_BOUNDARY_CASE@", strconv.Itoa(number), "@F036_LATER_LOCK@", strconv.Quote(later)).Replace(string(extra)))
				},
				run: func(t *testing.T, artifact, work string) {
					got := f036RunPolicyFixture(t, fixture, artifact, "--process-kernel-limit", "4096", "--lock-file", later)
					nativeFailure(t, got, 0, "runtime.host-failure", "")
					if !fixture.logged(scenario.injection+"\n") || !fixture.logged("boundary-probed\n") {
						t.Fatalf("boundary injection or shared-session probes did not execute: %+v", got)
					}
					var report struct {
						Diagnostics []struct {
							Progress runtimeValue `json:"process_progress"`
							Cleanup  []string     `json:"cleanup_errors"`
						}
					}
					if err := json.Unmarshal([]byte(got.stderr), &report); err != nil || len(report.Diagnostics) != 1 {
						t.Fatalf("cleanup boundary diagnostic: %v; %+v", err, got)
					}
					progress := processFields(t, report.Diagnostics[0].Progress)
					valueJSON(t, progress["cleanup_complete"], strconv.FormatBool(scenario.complete))
					valueJSON(t, progress["stdout_bytes"], `"0"`)
					valueJSON(t, progress["stderr_bytes"], `"0"`)
					if scenario.cleanup != "" && !strings.Contains(strings.Join(report.Diagnostics[0].Cleanup, "\n"), scenario.cleanup) {
						t.Fatalf("compound cleanup evidence was lost: %+v", got)
					}
					if number == 0 {
						if !fixture.logged("C\n") {
							t.Fatal("receipt corruption did not follow real provider cleanup")
						}
						macCleanupPresent(t, fixture.ready, true)
					} else {
						macCleanupPresent(t, fixture.ready, false)
						for _, record := range fixture.tokenRecords(t) {
							if record[0] != "caller" {
								t.Fatalf("pre-spawn failure created a remote owner: %v", record)
							}
						}
					}
				},
			}})
		})
	}
}
