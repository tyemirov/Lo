package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMFileAdapters(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	t.Run("append-create", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "output")
		source := sourceFile(t, fmt.Sprintf(`path=File.Path.new(%q); first=File.Append.new(path,UTF8.Bytes.new("hé")); a=commit first; second=File.Append.new(path,UTF8.Bytes.new("!")); b=commit second; return Record.new(first:a,second:b)`, path))
		result := run(t, "run", source, "--write-root", root, "--format", "json")
		if result.status != 0 {
			t.Fatalf("append: %+v", result)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "hé!" {
			t.Fatalf("content %q: %v", data, err)
		}
	})
	t.Run("ordered-remove", func(t *testing.T) {
		root := t.TempDir()
		writeLLVMFixture(t, filepath.Join(root, "b"), []byte("two"))
		writeLLVMFixture(t, filepath.Join(root, "a"), []byte("one"))
		source := sourceFile(t, fmt.Sprintf(`flow Main()->File.RemoveReceipt! {entries=File.list(File.Path.new(%q)); target=File.Remove.new(entries); receipt=commit target; return receipt}; return Main()`, root))
		result := run(t, "run", source, "--list-root", root, "--remove-root", root, "--format", "json")
		if result.status != 0 {
			t.Fatalf("remove: %+v", result)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("remaining entries %v: %v", entries, err)
		}
	})
}

func llvmFileNoReaderScenarios() []llvmRuntimeScenario {
	return []llvmRuntimeScenario{{name: "cancel-after-no-reader-open", number: 0, harness: "testdata/native-runtime/files-no-reader.c", source: `flow Handle(e:Error)->Int{return 99};flow Main()->Int onError Handle {target=File.Write.new(File.Path.new("pipe"),Bytes.new("x"));commit target;later=File.Write.new(File.Path.new("later"),Bytes.new("wrong"));commit later;return 0};return Main()`}}
}
func TestLLVMFileNoReaderCancellation(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmFileNoReaderScenarios())
}
func TestLLVMLinuxFileNoReaderCancellation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmFileNoReaderScenarios())
}

func TestLLVMLinuxFileAdapters(t *testing.T) {
	artifact := linuxLLVMArtifact(t, `flow Keep(entry:File.Entry)->Bool{return String.eq(entry.name,"lo-native-file")};flow Main()->Record{appended:Int,content:Bytes,created:Int,remaining:Int,removed:Int}! {path=File.Path.new("/lo-native-file");first=File.Write.new(path,UTF8.Bytes.new("abc"));a=commit first;second=File.Append.new(path,UTF8.Bytes.new("dé"));b=commit second;content=File.read(path);entries=File.list(File.Path.new("/"));selected=List.filter(entries,Keep);removal=File.Remove.new(selected);r=commit removal;after=File.list(File.Path.new("/"));remaining=List.filter(after,Keep);return Record.new(created:a.bytes_written,appended:b.bytes_written,content:content,removed:r.observed_bytes,remaining:List.length(remaining))};return Main()`)
	got := runLinuxLLVMExecutable(t, artifact, "run", "--read-root", "/", "--write-root", "/", "--list-root", "/", "--remove-root", "/", "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("Linux file execution: %+v", got)
	}
	value := trainingResult(t, got)
	valueJSON(t, recordField(t, value, "created"), `"3"`)
	valueJSON(t, recordField(t, value, "appended"), `"3"`)
	valueJSON(t, recordField(t, value, "content"), `"YWJjZMOp"`)
	valueJSON(t, recordField(t, value, "removed"), `"6"`)
	valueJSON(t, recordField(t, value, "remaining"), `"0"`)
	denied := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
	if denied.status != 2 || !strings.Contains(denied.stderr, `"code":"runtime.capability-denied"`) {
		t.Fatalf("Linux absent authority: %+v", denied)
	}
}

func llvmFileObservationScenarios() []llvmRuntimeScenario {
	source := `flow Keep(entry:File.Entry)->Bool{return String.eq(entry.name,"selected")};flow Handle(e:Error)->String{return e.kind};flow Main()->String onError Handle {entries=File.list(File.Path.new("grant"));selected=List.filter(entries,Keep);target=File.Remove.new(selected);PLACEHOLDER;return "accepted"};return Main()`
	scenarios := []llvmRuntimeScenario{
		{name: "foreign-session-entries", number: 0, source: strings.Replace(source, "PLACEHOLDER", `unused=List.length(selected)`, 1)},
		{name: "changed-entry-preflight", number: 1, source: strings.Replace(source, "PLACEHOLDER", `commit target;later=File.Write.new(File.Path.new("grant/later"),Bytes.new("wrong"));commit later`, 1)},
		{name: "replaced-grant-directory", number: 2, source: strings.Replace(source, "PLACEHOLDER", `commit target;later=File.Write.new(File.Path.new("grant/later"),Bytes.new("wrong"));commit later`, 1)},
	}
	for index := range scenarios {
		scenarios[index].harness = "testdata/native-runtime/files-observation.c"
		scenarios[index].transform = func(t *testing.T, module []byte) []byte {
			t.Helper()
			contents := string(module)
			contents = strings.ReplaceAll(contents, "call void @lo_op_File_2Elist(", "call void @lo_files_list_bridge(")
			contents = strings.ReplaceAll(contents, "call void @lo_op_File_2ERemove_2Enew(", "call void @lo_files_selection_bridge(")
			return []byte(contents + "\ndeclare void @lo_files_list_bridge(ptr,ptr,i64,i64,ptr)\ndeclare void @lo_files_selection_bridge(ptr,ptr,i64,i64,ptr)\n")
		}
	}
	return scenarios
}
func TestLLVMFileObservationIsolation(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmFileObservationScenarios())
}
func TestLLVMLinuxFileObservationIsolation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmFileObservationScenarios())
}

func TestLLVMFileUnicodeDiagnosticParity(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	root := t.TempDir()
	path := filepath.Join(root, "line\u2028separator")
	source := sourceFile(t, fmt.Sprintf(`flow Handle(e:Error)->String{return e.message};flow Main()->String onError Handle {path=File.Path.new(%q);first=File.Write.new(path,Bytes.new("original"));commit first;entries=File.list(File.Path.new(%q));changed=File.Write.new(path,Bytes.new("x"));commit changed;target=File.Remove.new(entries);commit target;return "unexpected"};return Main()`, path, root))
	arguments := []string{"--write-root", root, "--list-root", root, "--remove-root", root, "--format", "json"}
	expected := runExecutable(t, binary, append([]string{"run", source}, arguments...)...)
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build: %+v", built)
	}
	actual := runExecutable(t, artifact, append([]string{"run"}, arguments...)...)
	if actual != expected {
		t.Fatalf("Unicode file diagnostic differs: native %+v evaluator %+v", actual, expected)
	}
}

func llvmFileProbeCancellationScenarios() []llvmRuntimeScenario {
	return []llvmRuntimeScenario{{name: "cancel-during-probe-executable-hash", number: 0, harness: "testdata/native-runtime/files-probe-cancellation.c", source: `flow Handle(e:Error)->Int{return 99};flow Main()->Int onError Handle {paths=File.open_paths(File.Path.new("."));target=File.Write.new(File.Path.new("later"),Bytes.new("wrong"));commit target;return 0};return Main()`, transform: func(t *testing.T, module []byte) []byte {
		t.Helper()
		contents := strings.ReplaceAll(string(module), "call void @lo_op_File_2Eopen_5Fpaths(", "call void @lo_files_probe_bridge(")
		return []byte(contents + "\ndeclare void @lo_files_probe_bridge(ptr,ptr,i64,i64,ptr)\n")
	}}}
}
func TestLLVMFileProbeHashCancellation(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmFileProbeCancellationScenarios())
}
func TestLLVMLinuxFileProbeHashCancellation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmFileProbeCancellationScenarios())
}
