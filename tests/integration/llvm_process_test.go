package integration_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLLVMProcessConstructors(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires explicit LLVM runtime lane")
	}
	for index, source := range []string{
		`return Process.Executable.new(File.Path.new("/absolute/command"))`,
		`return Process.Executable.new(File.Path.new("relative"))`,
		`flow Main()->Process.Command!{ executable=Process.Executable.new(File.Path.new("/command")); return Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"NAME",value:"value")), File.Path.new("/work")), stdin: Bytes.new("input")) }; return Main()`,
		`flow Main()->Process.Result!{ executable=Process.Executable.new(File.Path.new("/command")); command=Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"NAME",value:"value")), File.Path.new("/work")), stdin: Bytes.new("input")); result=commit command; return result }; return Main()`,
		`flow Main()->Process.Command!{ executable=Process.Executable.new(File.Path.new("/command")); return Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"NAME",value:"value")), File.Path.new("relative")), stdin: Bytes.new("input")) }; return Main()`,
		`flow Main()->Process.Command!{ executable=Process.Executable.new(File.Path.new("/command")); return Process.Command.new(executable: executable, arguments: List.new("\u0000"), context: Process.Context.new(Map.new(Record.new(key:"NAME",value:"value")), File.Path.new("/work")), stdin: Bytes.new("input")) }; return Main()`,
		`flow Main()->Process.Command!{ executable=Process.Executable.new(File.Path.new("/command")); return Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"",value:"value")), File.Path.new("/work")), stdin: Bytes.new("input")) }; return Main()`,
		`flow Main()->Process.Command!{ executable=Process.Executable.new(File.Path.new("/command")); return Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"NAME",value:"\u0000")), File.Path.new("/work")), stdin: Bytes.new("input")) }; return Main()`,
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			input := sourceFile(t, source)
			artifact, built := buildForTest(t, input)
			if built.status != 0 {
				t.Fatalf("build process fixture: %+v", built)
			}
			expected := runExecutable(t, binary, "run", input, "--format", "json")
			actual := runExecutable(t, artifact, "run", "--format", "json")
			if actual != expected {
				t.Fatalf("process contract differs: native %+v evaluator %+v", actual, expected)
			}
		})
	}
}

func TestLLVMProcessExecution(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires explicit LLVM runtime lane")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires qualified Darwin arm64 process host")
	}
	directory := t.TempDir()
	code := filepath.Join(directory, "command.c")
	executable := filepath.Join(directory, "command")
	writeLLVMFixture(t, code, []byte(`extern long read(int,void*,unsigned long); extern long write(int,const void*,unsigned long); extern int strcmp(const char*,const char*);extern int fcntl(int,int,...); int main(int argc,char **argv,char **env){char input[16];for(int fd=3;fd<64;fd++)if(fcntl(fd,1)>=0)return 78;long count=read(0,input,16);if(argc!=2||strcmp(argv[1],"argument")||!env[0]||strcmp(env[0],"DECLARED=value")||env[1]||count!=5)return 77;write(1,input,count);write(2,"diagnostic",10);return 7;}`))
	command := exec.Command("cc", code, "-o", executable)
	command.Stdin = nil
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build command fixture: %v: %s", err, output)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	input := sourceFile(t, fmt.Sprintf(`flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(executable: executable, arguments: List.new("argument"), context: Process.Context.new(Map.new(Record.new(key:"DECLARED",value:"value")), File.Path.new(%q)), stdin: Bytes.new("input"));result=commit target;return result};return Main()`, executable, directory))
	artifact, built := buildForTest(t, input)
	if built.status != 0 {
		t.Fatalf("build process fixture: %+v", built)
	}
	grant := fmt.Sprintf("%x,%s", digest, executable)
	external := filepath.Join(repository, "bin", "lo-process-launcher")
	arguments := []string{"--process-allow", grant, "--process-read-root", directory, "--process-launcher", external, "--format", "json"}
	expected := runExecutable(t, binary, append([]string{"run", input}, arguments...)...)
	if expected.status != 0 {
		t.Fatalf("evaluate qualified process: %+v", expected)
	}
	copied := filepath.Join(directory, "standalone")
	copyCommand := exec.Command("cp", artifact, copied)
	if output, err := copyCommand.CombinedOutput(); err != nil {
		t.Fatalf("copy standalone fixture: %v: %s", err, output)
	}
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(artifact); err != nil {
		t.Fatal(err)
	}
	artifact = copied

	for _, launcher := range []string{external, artifact} {
		t.Run(filepath.Base(launcher), func(t *testing.T) {
			args := append([]string{}, arguments...)
			args[5] = launcher
			actual := runExecutable(t, artifact, append([]string{"run"}, args...)...)
			if actual != expected {
				t.Fatalf("process execution differs: native %+v evaluator %+v", actual, expected)
			}
		})
	}
	t.Run("foreign-descriptors", func(t *testing.T) {
		descriptor, err := os.Open(code)
		if err != nil {
			t.Fatal(err)
		}
		defer descriptor.Close()
		args := append([]string{}, arguments...)
		args[5] = artifact
		command := exec.Command(artifact, append([]string{"run"}, args...)...)
		command.Dir = repository
		command.Env = []string{"CI=1", "PATH=", "HOME=" + directory, "TMPDIR=" + directory, "TMP=" + directory, "TEMP=" + directory, "LC_ALL=C", "LANG=C"}
		command.ExtraFiles = []*os.File{descriptor, descriptor, descriptor, descriptor, descriptor, descriptor, descriptor, descriptor}
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			t.Fatalf("execute with foreign descriptors: %v: %s", err, stderr.String())
		}
		actual := result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
		if actual != expected {
			t.Fatalf("child inherited undeclared descriptors: native %+v evaluator %+v", actual, expected)
		}
	})

	t.Run("post-commit-budget-safe-progress", func(t *testing.T) {
		later := filepath.Join(directory, "later")
		source := sourceFile(t, fmt.Sprintf(`flow Add(state:Int,item:Int)->Int{return Math.add(state,item)}; flow Main()->Int!{executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(executable: executable, arguments: List.new("argument"), context: Process.Context.new(Map.new(Record.new(key:"DECLARED",value:"value")), File.Path.new(%q)), stdin: Bytes.new("input"));result=commit target;items=List.new(1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20);total=Flow.fold(items,0,Add);write=File.Write.new(File.Path.new(%q),Bytes.new("wrong"));commit write;return total};return Main()`, executable, directory, later))
		builtArtifact, built := buildForTest(t, source)
		if built.status != 0 {
			t.Fatalf("build postcommit fixture: %+v", built)
		}

		for _, format := range []string{"json", "text"} {
			t.Run(format, func(t *testing.T) {
				args := append(append([]string{}, arguments...), "--max-steps", "100", "--write-root", directory)
				args[7] = format
				expected := runExecutable(t, binary, append([]string{"run", source}, args...)...)
				if expected.status != 2 {
					t.Fatalf("evaluator must reach postcommit budget: %+v", expected)
				}
				if format == "json" && (!strings.Contains(expected.stderr, `"operation":"Process.run"`) || !strings.Contains(expected.stderr, `"code":"runtime.limit"`)) {
					t.Fatalf("evaluator must retain completed activation: %+v", expected)
				}
				args[5] = builtArtifact
				actual := runExecutable(t, builtArtifact, append([]string{"run"}, args...)...)
				if actual != expected {
					t.Fatalf("postcommit failure differs: native %+v evaluator %+v", actual, expected)
				}
				if strings.Contains(actual.stderr, "aW5wdXQ=") || strings.Contains(actual.stderr, "ZGlhZ25vc3RpYw==") {
					t.Fatal("captured process bytes entered safe progress")
				}
			})
		}

		if _, err := os.Stat(later); !os.IsNotExist(err) {
			t.Fatalf("later commit executed: %v", err)
		}
	})

}

func llvmProcessDriverScenarios() []llvmRuntimeScenario {
	source := `flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new("/command"));target=Process.Command.new(executable: executable, arguments: List.new("argument"), context: Process.Context.new(Map.new(Record.new(key:"DECLARED",value:"value")), File.Path.new("/work")), stdin: Bytes.new("input"));result=commit target;return result};return Main()`
	names := []string{"success", "late-start-busy-retained-cleanup", "unresolved-start", "release-failure", "malformed-termination", "thread-creation-failure", "contradictory-success", "unconfirmed-termination"}
	cases := make([]llvmRuntimeScenario, len(names))
	for index, name := range names {
		cases[index] = llvmRuntimeScenario{name: name, source: source, number: index, harness: "testdata/native-runtime/process-driver.c"}
	}
	return cases
}
func TestLLVMProcessDriverOwnership(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmProcessDriverScenarios())
}
func TestLLVMLinuxProcessDriverOwnership(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmProcessDriverScenarios())
}

func TestLLVMLinuxProcessUnavailable(t *testing.T) {
	source := `flow Main()->Process.Result!{executable=Process.Executable.new(File.Path.new("/command"));target=Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"NAME",value:"value")), File.Path.new("/work")), stdin: Bytes.new("input"));result=commit target;return result};return Main()`
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", []llvmRuntimeScenario{
		{name: "absent-authority", source: source, number: 0, harness: "testdata/native-runtime/process-unavailable.c"},
		{name: "configured-unavailable-before-file-access", source: source, number: 1, harness: "testdata/native-runtime/process-unavailable.c"},
	})
}

func TestLLVMProcessTransportOwnership(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "darwin/arm64", []llvmRuntimeScenario{
		{name: "observer-error-stops-owned-primary", source: `return 1`, number: 0, harness: "testdata/native-runtime/process-transport.c"},
		{name: "observer-event-error-stops-owned-primary", source: `return 1`, number: 1, harness: "testdata/native-runtime/process-transport.c"},
		{name: "ambiguous-close-never-retries-reused-descriptor", source: `return 1`, number: 2, harness: "testdata/native-runtime/process-transport.c"},
	})
}

func TestLLVMProcessConfigurationDiagnostics(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires explicit LLVM runtime lane")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires qualified Darwin process host")
	}
	directory := t.TempDir()
	command := filepath.Join(directory, "reviewed")
	corrupt := filepath.Join(directory, "corrupt-helper")
	writeLLVMFixture(t, command, []byte("reviewed bytes"))
	writeLLVMFixture(t, corrupt, []byte("unreviewed helper"))
	digest := sha256.Sum256([]byte("reviewed bytes"))
	grant := fmt.Sprintf("%x,%s", digest, command)
	helper := filepath.Join(repository, "bin", "lo-process-launcher")
	input := sourceFile(t, `return 1`)
	artifact, built := buildForTest(t, input)
	if built.status != 0 {
		t.Fatalf("build configuration fixture: %+v", built)
	}
	cases := []struct{ name, grant, helper, root string }{
		{"absent-helper", grant, filepath.Join(directory, "absent"), directory},
		{"directory-helper", grant, directory, directory},
		{"corrupt-helper", grant, corrupt, directory},
		{"command-digest", strings.Repeat("0", 64) + "," + command, helper, directory},
		{"nondirectory-read-root", grant, helper, command},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"--process-allow", test.grant, "--process-launcher", test.helper, "--process-read-root", test.root, "--format", "json"}
			expected := runExecutable(t, binary, append([]string{"run", input}, args...)...)
			actual := runExecutable(t, artifact, append([]string{"run"}, args...)...)
			if expected.status != 2 || actual != expected {
				t.Fatalf("configuration diagnostic differs: native %+v evaluator %+v", actual, expected)
			}
		})
	}
}

func TestLLVMProcessAdmissionHashCancellation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "darwin/arm64", []llvmRuntimeScenario{{name: "cancel-between-admission-hash-chunks", source: `flow Main()->Process.Result!{raw=File.read(File.Path.new("command-path"));path=UTF8.String.new(raw);directoryBytes=File.read(File.Path.new("directory-path"));directory=UTF8.String.new(directoryBytes);executable=Process.Executable.new(File.Path.new(path));target=Process.Command.new(executable: executable, arguments: List.new("arg"), context: Process.Context.new(Map.new(Record.new(key:"DECLARED",value:"value")), File.Path.new(directory)), stdin: Bytes.new("input"));result=commit target;return result};return Main()`, number: 0, harness: "testdata/native-runtime/process-hash-cancel.c"}})
}

func TestLLVMProcessProtectedFileComposition(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires explicit LLVM runtime lane")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires qualified Darwin process host")
	}
	directory := t.TempDir()
	command := filepath.Join(directory, "reviewed")
	alias := filepath.Join(directory, "alias")
	content := []byte("reviewed bytes")
	writeLLVMFixture(t, command, content)
	if err := os.Link(command, alias); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	args := []string{"--process-allow", fmt.Sprintf("%x,%s", digest, command), "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"), "--process-read-root", directory, "--write-root", directory, "--format", "json"}
	for _, path := range []string{command, alias} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			source := sourceFile(t, fmt.Sprintf(`target=File.Write.new(File.Path.new(%q),Bytes.new("changed"));commit target`, path))
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build protected file fixture: %+v", built)
			}
			expected := runExecutable(t, binary, append([]string{"run", source}, args...)...)
			actual := runExecutable(t, artifact, append([]string{"run"}, args...)...)
			if expected.status != 2 || actual != expected {
				t.Fatalf("registered file protection differs: native %+v evaluator %+v", actual, expected)
			}
			observed, err := os.ReadFile(command)
			if err != nil || !bytes.Equal(observed, content) {
				t.Fatalf("registered bytes changed: %q: %v", observed, err)
			}
		})
	}
}
