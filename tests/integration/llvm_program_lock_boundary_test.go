package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMProgramLockEmbeddingBoundary(t *testing.T) {
	runProgramLockEmbeddingBoundary(t, semanticNativeTarget())
}
func TestLLVMLinuxProgramLockEmbeddingBoundary(t *testing.T) {
	runProgramLockEmbeddingBoundary(t, "linux/arm64")
}
func runProgramLockEmbeddingBoundary(t *testing.T, target string) {
	t.Helper()
	names := []string{"missing-identity", "injected-identity", "discarded-lease", "recoverable-finalize", "cancelled-finalize", "close-failure-before-terminal", "failed-finalize-close-error", "no-exec-inheritance", "destructor-release", "injected-alias", "failed-attempt-close-handler", "replaced-entry-registration", "replaced-entry-explicit-dedup", "retained-child-cleanup", "unknown-owner-quarantine", "replaced-entry-explicit-only", "shared-session-registration-gate", "empty-evaluated-registration-gate", "repeated-entry-observation-identity", "failed-attempt-close-cli-classification"}
	var scenarios []llvmRuntimeScenario
	for number, name := range names {
		source := `flow Recover(e:Error)->Bool{return false};flow Main()->Bool onError Recover {a=Program.path();b=Program.path();return File.Path.eq(a,b)};return Main()`
		if number == 0 {
			source = `flow Recover(e:Error)->Bool{return String.eq(e.kind,"Program.EntryUnavailable")};flow Main()->Bool onError Recover {path=Program.path();return false};return Main()`
		}
		if number >= 2 {
			source = `flow Recover(e:Error)->Bool{return false};flow Acquire()->Bool onError Recover {target=Lock.File.new(Program.path());lease=commit target;return true};return Acquire()`
		}
		if number == 3 || number == 6 {
			source = `flow Main()->Int!{target=Lock.File.new(Program.path());lease=commit target;return Math.div(1,0)};return Main()`
		}
		if number == 5 {
			source = cliStackProgram("Program.Exit", `target=Lock.File.new(Program.path());lease=commit target;return Program.Exit.new(0,Bytes.new("out"),Bytes.new("err"))`)
		}
		if number == 10 || number == 19 {
			source = `flow Recover(e:Error)->Bool{return true};flow Again(path:File.Path)->Bool onError Recover {target=Lock.File.new(path);lease=commit target;return false};flow Main()->Bool!{path=Program.path();target=Lock.File.new(path);lease=commit target;return Again(path)};return Main()`
		}
		if number == 13 || number == 14 {
			source = `flow Main()->Process.Result!{target=Lock.File.new(Program.path());lease=commit target;executable=Process.Executable.new(File.Path.new("/command"));command=Process.Command.new(executable:executable,arguments:List.new("argument"),context:Process.Context.new(Map.new(Record.new(key:"DECLARED",value:"value")),File.Path.new("/work")),stdin:Bytes.new("input"));result=commit command;return result};return Main()`
		}
		if number == 17 {
			source = ""
		}
		if number == 18 {
			source = cliStackProgram("File.Path", `return Program.path()`)
		}
		scenario := llvmRuntimeScenario{name: name, source: source, number: number, harness: "testdata/native-runtime/program-lock-boundary.c"}
		if number == 19 {
			scenario.verify = func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.host-failure") {
					t.Fatalf("failed lock candidate cleanup must remain terminal: %+v", got)
				}
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}

func TestLLVMProgramLockMetalEntry(t *testing.T) {
	manifest, _ := llvmMetalManifest(t)
	artifact := filepath.Join(t.TempDir(), "program-entry-metal")
	source := sourceFile(t, `flow Main()->Lock.Lease!{target=Lock.File.new(Program.path());lease=commit target;return lease};return Main()`)
	built := runExecutable(t, binary, "build", source, "--toolchain", manifest, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 || built.stderr != "" {
		t.Fatalf("build Metal entry lock: %+v", built)
	}
	got := runExecutable(t, artifact, "run", "--lock-entry", "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"type":"Lock.Lease"`) {
		t.Fatalf("Metal actual entry lock: %+v", got)
	}
}
