package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestLLVMF025FileObserver(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmF025FileObserverScenarios())
}

func TestLLVMLinuxF025FileObserver(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmF025FileObserverScenarios())
}

func llvmF025FileObserverScenarios() []llvmRuntimeScenario {
	const prefix = `flow Keep(entry:File.Entry)->Bool{return Match.new(entry.name,"a":true,"b":true,_:false)};flow Name(entry:File.Entry)->String{return entry.name};`
	const writes = `first=File.Write.new(File.Path.new("a"),Bytes.new("first"));commit first;second=File.Write.new(File.Path.new("b"),Bytes.new("second"));commit second;`
	const scan = `scan=File.scan(File.Path.new("."));selected=List.filter(scan.entries,Keep);`
	const guard = `flow Eligible(original:File.Entry,current:File.Entry)->Bool{return Match.new(Math.eq(original.owner_uid,current.owner_uid),false:false,_:Match.new(Math.eq(original.accessed_unix_ns,current.accessed_unix_ns),false:false,_:Match.new(Math.eq(original.changed_unix_ns,current.changed_unix_ns),false:false,_:Math.eq(original.link_count,current.link_count))))};`
	const checked = `target=File.Remove.checked(selected,Eligible);receipt=commit target;return receipt.completed_count`
	metadata := prefix + `flow Metadata(entry:File.Entry)->Record{owner:Int,access:Int,change:Int}{return Record.new(owner:entry.owner_uid,access:entry.accessed_unix_ns,change:entry.changed_unix_ns)};flow Main()->Record{directory_owner:Int,scan_owner:Int,entries:List<Record{owner:Int,access:Int,change:Int}>}!{` + writes + `directory=File.directory(File.Path.new("."));` + scan + `return Record.new(directory_owner:directory.owner_uid,scan_owner:scan.directory.owner_uid,entries:List.map(selected,Metadata))};return Main()`
	discovery := prefix + `flow Main()->Record{names:List<String>,vanished:List<String>}!{` + writes + scan + `return Record.new(names:List.map(selected,Name),vanished:scan.vanished_names)};return Main()`
	removal := prefix + guard + `flow Main()->Int!{` + writes + scan + checked + `};return Main()`
	budgetGuard := `flow Count(index:Int)->Record{value:Int,next_state:Int}{return Record.new(value:index,next_state:Math.add(index,1))};flow Eligible(original:File.Entry,current:File.Entry)->Bool{steps=List.unfold(0,10000,Count);return Math.eq(List.length(steps),10000)};`
	budget := prefix + budgetGuard + `flow Main()->Int!{` + writes + scan + checked + `};return Main()`
	var scenarios []llvmRuntimeScenario
	for index, test := range []struct{ name, source, expected string }{
		{"metadata", metadata, ""},
		{"vanished-child", discovery, ""},
		{"lookup-error", discovery, "File.ListFailed"},
		{"preflight-rejection", removal, "File.EntryChanged"},
		{"remove-rejection-prefix", removal, "File.EntryChanged"},
		{"guard-budget", budget, "runtime.limit"},
		{"remove-cancellation", removal, "runtime.cancelled"},
		{"invalid-provider", discovery, "runtime.host-failure"},
		{"remove-access-change-prefix", removal, "File.EntryChanged"},
		{"remove-ctime-change-prefix", removal, "File.EntryChanged"},
		{"remove-link-change-prefix", removal, "File.EntryChanged"},
		{"strict-list-missing-child", prefix + `flow Main()->Int!{` + writes + `entries=File.list(File.Path.new("."));return List.length(entries)};return Main()`, "File.ListFailed"},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: test.name, source: test.source, number: index, harness: "testdata/native-runtime/file-metadata-observer.c", transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			return []byte(strings.Replace(string(module), "define void @lo_entry(", "define void @lo_qualified_entry(", 1))
		}, verify: func(t *testing.T, actual result) {
			if actual.status != 0 || actual.stderr != "" {
				t.Fatalf("native File observer %s: %+v", test.name, actual)
			}
			if test.expected != "" && !strings.Contains(actual.stdout, test.expected) {
				t.Fatalf("native File observer %s omitted %s: %s", test.name, test.expected, actual.stdout)
			}
			switch test.name {
			case "metadata":
				output := fileOutput(t, actual)
				valueJSON(t, recordField(t, output, "directory_owner"), `"1001"`)
				valueJSON(t, recordField(t, output, "scan_owner"), `"1001"`)
				fields := recordField(t, output, "entries")
				valueJSON(t, fields, `[{"type":"Record{access: Int, change: Int, owner: Int}","value":{"access":{"type":"Int","value":"-3000000000"},"change":{"type":"Int","value":"-1000000000"},"owner":{"type":"Int","value":"1001"}}},{"type":"Record{access: Int, change: Int, owner: Int}","value":{"access":{"type":"Int","value":"-3000000000"},"change":{"type":"Int","value":"-1000000000"},"owner":{"type":"Int","value":"1001"}}}]`)
			case "vanished-child":
				output := fileOutput(t, actual)
				valueJSON(t, recordField(t, output, "names"), `[{"type":"String","value":"b"}]`)
				valueJSON(t, recordField(t, output, "vanished"), `[{"type":"String","value":"a"}]`)
			case "preflight-rejection", "remove-rejection-prefix", "remove-access-change-prefix", "remove-ctime-change-prefix", "remove-link-change-prefix":
				failure := receiptHostDiagnostic(t, result{stderr: actual.stdout})
				if failure.Error == nil || failure.Error.Input == nil {
					t.Fatalf("missing failure progress: %s", actual.stdout)
				}
				input := processFields(t, *failure.Error.Input)
				progress := processFields(t, input["progress"])
				completed := 0
				if test.name != "preflight-rejection" {
					completed = 1
				}
				valueJSON(t, progress["completed_count"], fmt.Sprintf(`"%d"`, completed))
				guard := processFields(t, input["guard"])
				valueJSON(t, guard["condition"], `"predicate-false"`)
			}
		}})
	}
	return scenarios
}
