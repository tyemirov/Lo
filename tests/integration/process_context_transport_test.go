package integration_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProcessContextInheritedTransport(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the selected Darwin Process profile")
	}
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "context-helper")
	code := filepath.Join(root, "context-helper.c")
	writeLLVMFixture(t, code, []byte(`extern char *getenv(const char*); extern unsigned long strlen(const char*); extern int strcmp(const char*,const char*); extern char *getcwd(char*,unsigned long); extern long read(int,void*,unsigned long); extern long write(int,const void*,unsigned long);
int main(int argc,char **argv,char **env){char cwd[4096],byte;int count=0;while(env[count])count++;char *value=getenv("LO_F024_LONG");if(argc!=33||count<40||!value||strlen(value)!=16384||!getcwd(cwd,sizeof(cwd))||strcmp(cwd,argv[1])||!getenv("CI")||strcmp(getenv("CI"),"1")||read(0,&byte,1)!=0)return 73;for(int i=2;i<argc;i++)if(strcmp(argv[i],"literal argument"))return 74;return write(1,"context-ok",10)==10?0:75;}`))
	command := exec.Command("cc", code, "-o", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build context helper: %v: %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	t.Setenv("LO_F024_LONG", strings.Repeat("x", 16384))
	for index := 0; index < 40; index++ {
		t.Setenv(fmt.Sprintf("LO_F024_ENTRY_%02d", index), "inherited")
	}
	arguments := []string{processLiteral(t, root)}
	for index := 0; index < 31; index++ {
		arguments = append(arguments, `"literal argument"`)
	}
	source := processContextProgram("String", fmt.Sprintf(`initial = Process.Context.current(); context = Process.Context.with_directory(initial, File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new(%s), Bytes.new("")); result = commit command; code = Process.Termination.exit_code(result.termination); return Match.new(code, 0: UTF8.String.new(result.stdout), _: "helper-failed")`, root, path, strings.Join(arguments, ",")))
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--process-allow", fmt.Sprintf("%x,%s", digest, path), "--process-read-root", root, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"), "--format", "json"))
	valueJSON(t, got, `"context-ok"`)
}

func TestProcessContextTransportBounds(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the selected Darwin Process profile")
	}
	root := fileFixtureRoot(t)
	path, code := filepath.Join(root, "bounded-helper"), filepath.Join(root, "bounded-helper.c")
	writeLLVMFixture(t, code, []byte(`extern int atoi(const char*);
int main(int argc,char **argv,char **env){if(argc!=2)return 0;int count=0;while(env[count])count++;return count==atoi(argv[1])?0:73;}`))
	command := exec.Command("cc", code, "-o", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build bounded helper: %v: %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	const environmentLimit = 4096
	for _, test := range []struct {
		name                         string
		arguments, variables, length int
		accepted                     bool
	}{
		{"arguments-at-limit", 255, 0, 0, true},
		{"arguments-over-limit", 256, 0, 0, false},
		{"environment-at-limit", 0, environmentLimit, 1, true},
		{"environment-over-limit", 0, environmentLimit + 1, 1, false},
		{"entry-at-limit", 0, 1, 65530, true},
		{"entry-over-limit", 0, 1, 65531, false},
		{"body-over-limit", 0, 20, 60000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments := make([]string, test.arguments)
			for index := range arguments {
				arguments[index] = `"argument"`
			}
			contextSource := ""
			flows := ""
			if test.variables >= environmentLimit {
				// Build the environment at runtime without a large, unrolled LLVM function.
				const batchSize = 64
				entries := make([]string, batchSize)
				for index := range entries {
					entries[index] = fmt.Sprintf(`Record.new(key: String.concat("V", Int.to_string(Math.add(offset, %d))), value: "x")`, index)
				}
				flows = fmt.Sprintf(`flow Offset(offset: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: offset, next_state: Math.add(offset, %d)) }; flow Extend(context: Process.Context, offset: Int) -> Process.Context! { return Process.Context.override_environment(context, Map.new(%s)) }; `, batchSize, strings.Join(entries, ","))
				contextSource = fmt.Sprintf(`initial = Process.Context.new(Map.new(), File.Path.new(%q)); offsets = List.unfold(0, %d, Offset); batched = List.fold(offsets, initial, Extend); `, root, environmentLimit/batchSize)
				if test.variables > environmentLimit {
					contextSource += fmt.Sprintf(`context = Process.Context.override_environment(batched, Map.new(Record.new(key: "V%d", value: "x"))); `, environmentLimit)
				} else {
					contextSource += `context = batched; `
				}
				arguments = []string{fmt.Sprintf(`"%d"`, test.variables)}
			} else {
				environment := make([]string, test.variables)
				for index := range environment {
					environment[index] = fmt.Sprintf(`Record.new(key: "V%04d", value: %q)`, index, strings.Repeat("x", test.length))
				}
				contextSource = fmt.Sprintf(`context = Process.Context.new(Map.new(%s), File.Path.new(%q)); `, strings.Join(environment, ","), root)
			}
			source := flows + processContextProgram("Int", contextSource+fmt.Sprintf(`executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new(%s), Bytes.new("")); result = commit command; return Process.Termination.exit_code(result.termination)`, path, strings.Join(arguments, ",")))
			got := run(t, "run", sourceFile(t, source), "--process-allow", fmt.Sprintf("%x,%s", digest, path), "--process-read-root", root, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"), "--format", "json")
			if test.accepted {
				valueJSON(t, fileOutput(t, got), `"0"`)
				return
			}
			diagnostic := receiptHostDiagnostic(t, got)
			if got.status != 1 || diagnostic.Error == nil || diagnostic.Error.Kind != "Process.ExecutionFailed" || diagnostic.Error.Input == nil {
				t.Fatalf("transport admission: %+v", got)
			}
			fields := processFields(t, *diagnostic.Error.Input)
			progress := processFields(t, fields["progress"])
			valueJSON(t, progress["start_confirmed"], `false`)
			valueJSON(t, progress["cleanup_complete"], `true`)
		})
	}
}
