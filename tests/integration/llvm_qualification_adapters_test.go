package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const nativeProcessLifecycleSource = `flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new("/command"));target=Process.Command.new(executable: executable, arguments: List.new("one two","$VALUE"), context: Process.Context.new(Map.new(Record.new(key:"ONLY",value:"explicit")), File.Path.new("/work")), stdin: Bytes.new("input"));result=commit target;return result};return Main()`

func nativeProcessLifecycleScenarios(t *testing.T, cases []int) {
	t.Helper()
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	names := []string{"exited", "signaled", "exact-stream-capacity", "partial-stdout", "early-stdin-close", "partial-start", "release-host-failure", "stdout-over-limit", "stderr-over-limit", "cancel-joins-owner"}
	var scenarios []llvmRuntimeScenario
	for _, number := range cases {
		scenarios = append(scenarios, llvmRuntimeScenario{name: names[number], source: nativeProcessLifecycleSource, number: number, captureOutput: number < 6, harness: "testdata/native-qualification/process-lifecycle.c", transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			return []byte(strings.Replace(string(module), "define void @lo_entry(", "define void @lo_process_lifecycle_entry(", 1))
		}})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMProcessLifecycleCompleted(t *testing.T) { nativeProcessLifecycleScenarios(t, []int{0, 1}) }
func TestLLVMProcessLifecycleExactOutputLimit(t *testing.T) {
	nativeProcessLifecycleScenarios(t, []int{2, 7, 8})
}
func TestLLVMProcessLifecycleFailures(t *testing.T) {
	nativeProcessLifecycleScenarios(t, []int{3, 4, 5, 6})
}
func TestLLVMProcessLifecycleCancellation(t *testing.T) { nativeProcessLifecycleScenarios(t, []int{9}) }

func TestLLVMHTTPClientCleanupEvidence(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	var scenarios []llvmRuntimeScenario
	for _, number := range []int{2, 3} {
		name := "ordinary-read-failure"
		if number == 3 {
			name = "cancelled-read-failure"
		}
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: httpClientSource(fmt.Sprintf("http://127.0.0.1:%d/", 34446+number), "GET", `List.new()`, "", `response=commit target;return response`), number: number, harness: "testdata/native-qualification/http-cancel-cleanup.c"})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMProcessTransportCompleted(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the Darwin process transport")
	}
	for _, signaled := range []bool{false, true} {
		t.Run(fmt.Sprintf("signaled-%t", signaled), func(t *testing.T) {
			directory := t.TempDir()
			code := filepath.Join(directory, "command.c")
			child := filepath.Join(directory, "command")
			termination := "return 73;"
			if signaled {
				termination = "kill(getpid(),15);return 99;"
			}
			writeLLVMFixture(t, code, []byte(`extern long read(int,void*,unsigned long),write(int,const void*,unsigned long);extern int strcmp(const char*,const char*),kill(int,int),getpid(void);extern char *getcwd(char*,unsigned long);int main(int argc,char**argv,char**env){char input[16],cwd[4096];long count=read(0,input,sizeof(input));if(argc!=3||strcmp(argv[1],"one two")||strcmp(argv[2],"$VALUE")||!env[0]||strcmp(env[0],"ONLY=explicit")||env[1]||count!=5||!getcwd(cwd,sizeof(cwd))||strcmp(cwd,`+fmt.Sprintf("%q", directory)+`))return 77;const unsigned char binary[]={0,255,'o'};write(1,binary,3);write(2,"error-output",12);`+termination+`}`))
			command := exec.Command("cc", code, "-o", child)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("compile process dependency: %v: %s", err, output)
			}
			data, err := os.ReadFile(child)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			source := sourceFile(t, fmt.Sprintf(`flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(executable: executable, arguments: List.new("one two","$VALUE"), context: Process.Context.new(Map.new(Record.new(key:"ONLY",value:"explicit")), File.Path.new(%q)), stdin: Bytes.new("input"));result=commit target;return result};return Main()`, child, directory))
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build process program: %+v", built)
			}
			args := []string{"--format", "json", "--process-allow", fmt.Sprintf("%x,%s", digest, child), "--process-read-root", directory, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher")}
			reference := runExecutable(t, binary, append([]string{"run", source}, args...)...)
			actual := runExecutable(t, artifact, append([]string{"run"}, args...)...)
			if reference.status != 0 || actual != reference {
				t.Fatalf("native process transport differs: native=%+v reference=%+v", actual, reference)
			}
			var envelope struct{ Result runtimeValue }
			if err := json.Unmarshal([]byte(actual.stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			fields := processFields(t, successfulValue(t, envelope.Result))
			valueJSON(t, fields["stdout"], `"AP9v"`)
			valueJSON(t, fields["stderr"], `"ZXJyb3Itb3V0cHV0"`)
			term := processFields(t, fields["termination"])
			kind, payload, expected := "exited", "exit_code", `"73"`
			if signaled {
				kind, payload, expected = "signaled", "signal", `"15"`
			}
			valueJSON(t, term["kind"], fmt.Sprintf("%q", kind))
			valueJSON(t, term[payload], expected)
		})
	}
}

func TestLLVMProcessTransportCancellation(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the Darwin process transport")
	}
	directory := t.TempDir()
	childSource := filepath.Join(directory, "blocked.c")
	child := filepath.Join(directory, "blocked")
	writeLLVMFixture(t, childSource, []byte(`extern long write(int,const void*,unsigned long);extern int pause(void);int main(void){write(1,"partial",7);for(;;)pause();}`))
	command := exec.Command("cc", childSource, "-o", child)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile blocked dependency: %v: %s", err, output)
	}
	data, err := os.ReadFile(child)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	source := sourceFile(t, fmt.Sprintf(`flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(executable: executable, arguments: List.new(), context: Process.Context.new(Map.new(), File.Path.new(%q)), stdin: Bytes.new("input"));result=commit target;return result};return Main()`, child, directory))
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build cancellation program: %+v", built)
	}
	process := exec.Command(artifact, "run", "--format", "json", "--process-allow", fmt.Sprintf("%x,%s", digest, child), "--process-read-root", directory, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"))
	process.Dir = directory
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	joined, childPID := false, 0
	defer func() {
		if !joined {
			_ = process.Process.Kill()
			_ = process.Wait()
			if childPID != 0 {
				_ = syscall.Kill(childPID, syscall.SIGKILL)
			}
		}
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for childPID == 0 {
		observation, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").CombinedOutput()
		if err != nil {
			t.Fatalf("observe native child: %v: %s", err, observation)
		}
		for _, line := range strings.Split(string(observation), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[1] != strconv.Itoa(process.Process.Pid) || filepath.Base(fields[len(fields)-1]) != filepath.Base(child) {
				continue
			}
			childPID, err = strconv.Atoi(fields[0])
			if err != nil {
				t.Fatal(err)
			}
		}
		if childPID != 0 {
			break
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatal("native child did not reach execution")
		}
	}
	if err := process.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
		joined = true
	case <-time.After(5 * time.Second):
		_ = process.Process.Kill()
		t.Fatal("cancelled transport did not join its process owner")
	}
	if process.ProcessState.ExitCode() != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "runtime.cancelled") || !strings.Contains(stderr.String(), `"cleanup_complete":{"type":"Bool","value":true}`) || !strings.Contains(stderr.String(), `"start_confirmed":{"type":"Bool","value":true}`) || strings.Contains(stderr.String(), "cGFydGlhbA==") {
		t.Fatalf("cancellation result: state=%s wait=%v out=%s err=%s", process.ProcessState, waitErr, stdout.String(), stderr.String())
	}
	if err := syscall.Kill(childPID, 0); err != syscall.ESRCH {
		t.Fatalf("native child remains after cancellation: %v", err)
	}
}

func TestLLVMProcessTransportOutputLimits(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the Darwin process transport")
	}
	directory := t.TempDir()
	code := filepath.Join(directory, "output.c")
	child := filepath.Join(directory, "output")
	writeLLVMFixture(t, code, []byte(`extern long write(int,const void*,unsigned long),read(int,void*,unsigned long);extern int strcmp(const char*,const char*);static void output(int fd,int extra){char data[4096];for(int i=0;i<4096;++i)data[i]='x';for(int i=0;i<256;++i){unsigned long offset=0;while(offset<sizeof(data)){long count=write(fd,data+offset,sizeof(data)-offset);if(count<=0)return;offset+=(unsigned long)count;}}if(extra)write(fd,"x",1);}int main(int argc,char**argv){char input[16];if(argc!=2||read(0,input,sizeof(input))!=5)return 77;if(!strcmp(argv[1],"exact")){output(1,0);output(2,0);}else output(!strcmp(argv[1],"stdout")?1:2,1);return 0;}`))
	command := exec.Command("cc", code, "-o", child)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile output dependency: %v: %s", err, output)
	}
	data, err := os.ReadFile(child)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	for _, name := range []string{"exact", "stdout", "stderr"} {
		t.Run(name, func(t *testing.T) {
			source := sourceFile(t, fmt.Sprintf(`flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(executable: executable, arguments: List.new(%q), context: Process.Context.new(Map.new(), File.Path.new(%q)), stdin: Bytes.new("input"));result=commit target;return result};return Main()`, child, name, directory))
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build output contract: %+v", built)
			}
			args := []string{"--format", "json", "--process-allow", fmt.Sprintf("%x,%s", digest, child), "--process-read-root", directory, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"), "--max-steps", "10000000"}
			reference := runExecutable(t, binary, append([]string{"run", source}, args...)...)
			actual := runExecutable(t, artifact, append([]string{"run"}, args...)...)
			if name == "exact" {
				if actual.status != 0 || actual != reference {
					t.Fatalf("exact transport output differs: native status=%d reference status=%d", actual.status, reference.status)
				}
				var envelope struct{ Result runtimeValue }
				if err := json.Unmarshal([]byte(actual.stdout), &envelope); err != nil {
					t.Fatal(err)
				}
				fields := processFields(t, successfulValue(t, envelope.Result))
				for _, field := range []string{"stdout", "stderr"} {
					var encoded string
					if err := json.Unmarshal(fields[field].Value, &encoded); err != nil {
						t.Fatal(err)
					}
					decoded, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil || !bytes.Equal(decoded, bytes.Repeat([]byte{'x'}, 1<<20)) {
						t.Fatalf("exact %s lost bytes: length=%d error=%v", field, len(decoded), err)
					}
				}
			} else {
				for lane, result := range map[string]result{"native": actual, "interpreter": reference} {
					if result.status != 2 || result.stdout != "" || !strings.Contains(result.stderr, "runtime.limit") || !strings.Contains(result.stderr, "process_progress") || !strings.Contains(result.stderr, name) || !strings.Contains(result.stderr, "1048576") {
						t.Fatalf("%s %s over-limit diagnostics: %+v", lane, name, result)
					}
				}
			}
		})
	}
}
