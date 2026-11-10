package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestF036LockCaptureCleanupFreeze(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin descriptor and process fixtures")
	}
	for _, scenario := range []struct {
		name   string
		number int
	}{{"closed-duplicates-release-freeze", 0}, {"uncertain-close-retains-freeze", 1}} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			second, later := filepath.Join(fixture.root, "second-lock"), filepath.Join(fixture.root, "later-lock")
			writeLLVMFixture(t, second, nil)
			writeLLVMFixture(t, later, nil)
			source := fmt.Sprintf(`
flow ProbeLater()->Bool onError Denied{target=Lock.File.new(File.Path.new(%q));lease=commit target;return false}
flow ProbeSame()->Bool onError Denied{target=Lock.File.new(File.Path.new(%q));lease=commit target;return false}
flow Denied(error:Error)->Bool{return Match.new(String.eq(error.kind,"Lock.Failed"),true:String.starts_with(error.message,"file lock acquisition is frozen by active remote owner "),_:false)}
flow Main()->Int!{first=Lock.File.new(File.Path.new(%q));one=commit first;second=Lock.File.new(File.Path.new(%q));two=commit second;context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));command=Process.Command.new(context,executable,List.new(%q,"finish",%q,%q,"initial"),Bytes.new(""));result=commit command;return 0};return Main()
`, later, fixture.lock, fixture.lock, second, fixture.work, fixture.command, fixture.socket, fixture.ready, fixture.release)
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "shared-session-after-capture-failure", number: scenario.number,
				source: source, harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
				transform: func(t *testing.T, input []byte) []byte {
					return []byte(strings.Replace(string(input), "define void @lo_entry(", "define void @lo_f036_capture_entry(", 1))
				},
				harnessTransform: func(t *testing.T, input []byte) []byte {
					extra, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/f036-lock-capture.c"))
					if err != nil {
						t.Fatal(err)
					}
					provider := strings.Replace(string(fixture.transform(t, input)), "int main(int argc,char **argv,char **environment)", "int f036_provider_main(int argc,char **argv,char **environment)", 1)
					if !strings.Contains(provider, "int f036_provider_main(") {
						t.Fatal("lock capture harness did not bind the provider entry")
					}
					return []byte(provider + "\n" + strings.NewReplacer("@F036_SECOND_LOCK@", strconv.Quote(second), "@F036_LATER_LOCK@", strconv.Quote(later)).Replace(string(extra)))
				},
				run: func(t *testing.T, artifact, work string) {
					got := f036RunPolicyFixture(t, fixture, artifact, "--lock-file", second, "--lock-file", later)
					nativeFailure(t, got, 0, "runtime.host-failure", "")
					if !fixture.logged("capture-probed\n") || fixture.logged("O\n") || !strings.Contains(got.stderr, "duplicate remote lock lease") {
						t.Fatalf("capture condition or shared-session probe did not execute: %+v", got)
					}
					if scenario.number == 1 && !strings.Contains(got.stderr, "close duplicate lock lease") {
						t.Fatalf("compound cleanup evidence was lost: %+v", got)
					}
				},
			}})
		})
	}
}
