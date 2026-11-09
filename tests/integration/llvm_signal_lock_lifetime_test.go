package integration_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLLVMNativeSignalLockLifetime(t *testing.T) {
	runNativeSignalLockLifetime(t, semanticNativeTarget())
}
func TestLLVMLinuxNativeSignalLockLifetime(t *testing.T) {
	runNativeSignalLockLifetime(t, "linux/arm64")
}
func runNativeSignalLockLifetime(t *testing.T, target string) {
	t.Helper()
	var grant, root, launcher, command string
	if target == "darwin/arm64" {
		root = t.TempDir()
		code := filepath.Join(root, "command.c")
		command = filepath.Join(root, "command")
		writeLLVMFixture(t, code, []byte(`extern long write(int,const void*,__SIZE_TYPE__);
extern int poll(void*,unsigned long,int);
extern unsigned alarm(unsigned);
int main(void){alarm(12);if(write(1,"~",1)!=1)return 1;for(;;)poll(0,0,1000);}
`))
		compiler := exec.Command("cc", code, "-o", command)
		compiler.Dir = root
		if output, err := compiler.CombinedOutput(); err != nil {
			t.Fatalf("build admitted transport fixture: %v: %s", err, output)
		}
		bytes, err := os.ReadFile(command)
		if err != nil {
			t.Fatal(err)
		}
		grant = fmt.Sprintf("%x,%s", sha256.Sum256(bytes), command)
		launcher = filepath.Join(repository, "bin/lo-process-launcher")
	}
	var scenarios []llvmRuntimeScenario
	for phase, name := range []string{"startup-console", "http-active-console", "delayed-process-cleanup"} {
		if phase == 2 && target != "darwin/arm64" {
			continue
		}
		for variant, delivery := range []string{"sigint-pid", "sigint-group", "sigterm-pid", "sigterm-group"} {
			source := `flow Main()->Int!{lock=Lock.File.new(Program.path());lease=commit lock;line=Console.Line.new("stderr","active",true);receipt=commit line;late=Console.Line.new("stdout","late",true);commit late;return 7};return Main()`
			if phase == 1 {
				source = `flow Handle(request:HTTP.Request)->HTTP.Response!{line=Console.Line.new("stderr","active",true);receipt=commit line;return HTTP.Response.Text.new(200,"ok")};flow Failed(error:Error)->HTTP.Response{return HTTP.Response.Text.new(500,error.kind)};flow Main()->Int!{lock=Lock.File.new(Program.path());lease=commit lock;server=HTTP.Server.new(0,Handle,Failed);commit server;return 0};return Main()`
			}
			if phase == 2 {
				source = fmt.Sprintf(`flow Main()->Process.Result!{lock=Lock.File.new(Program.path());lease=commit lock;executable=Process.Executable.new(File.Path.new(%q));command=Process.Command.new(executable:executable,arguments:List.new(),context:Process.Context.new(Map.new(),File.Path.new(%q)),stdin:Bytes.new(""));result=commit command;return result};return Main()`, command, root)
			}
			scenario := llvmRuntimeScenario{name: name + "/" + delivery, source: source, number: phase*4 + variant, harness: "testdata/native-runtime/signal-lock-lifetime.c", captureOutput: true, timeout: 18 * time.Second}
			scenario.harnessTransform = func(t *testing.T, source []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(source), "@PROCESS_GRANT@", fmt.Sprintf("%q", grant))
				text = strings.ReplaceAll(text, "@PROCESS_ROOT@", fmt.Sprintf("%q", root))
				text = strings.ReplaceAll(text, "@PROCESS_LAUNCHER@", fmt.Sprintf("%q", launcher))
				return []byte(text)
			}
			scenario.transform = func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.Replace(string(module), "define void @lo_entry(", "define void @lo_signal_entry(", 1)
				text = strings.ReplaceAll(text, "call void @lo_commit(", "call void @lo_signal_commit(")
				return []byte(text + "\ndeclare void @lo_signal_commit(ptr,ptr,i64,i64,ptr)\n")
			}
			selectedPhase, selectedVariant := phase, variant
			scenario.verify = func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 {
					t.Fatalf("signal ownership and lock lifetime: %+v", got)
				}
				if selectedPhase == 1 && selectedVariant%2 == 0 {
					if !strings.Contains(got.stdout, `"status":"ready"`) || !strings.Contains(got.stdout, `"status":"stopped"`) {
						t.Fatalf("request did not drain through shutdown: %+v", got)
					}
				} else if !strings.Contains(got.stderr, `"code":"runtime.cancelled"`) && !strings.Contains(got.stderr, `"code":"runtime.host-failure"`) {
					t.Fatalf("missing terminal signal/output diagnostic: %+v", got)
				}
			}
			scenarios = append(scenarios, scenario)
		}
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}
