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
	const prefix = `flow Keep(entry:File.Entry)->Bool{
    flow KeepChoiceTest1(value: String) -> Bool {
        return String.eq(value, "a")
    }

    flow KeepChoiceResult1(value: String) -> Bool {
        return true
    }

    flow KeepChoiceTest2(value: String) -> Bool {
        return String.eq(value, "b")
    }

    flow KeepChoiceDefault(value: String) -> Bool {
        return false
    }

    return entry.name |> Match.apply(Match.new(List.new(Match.Case.new(KeepChoiceTest1, KeepChoiceResult1), Match.Case.new(KeepChoiceTest2, KeepChoiceResult1)), KeepChoiceDefault))};flow Name(entry:File.Entry)->String{return entry.name};`
	const writes = `first=File.Write.new(File.Path.new("a"),Bytes.new("first"));commit first;second=File.Write.new(File.Path.new("b"),Bytes.new("second"));commit second;`
	const scan = `scan=File.scan(File.Path.new("."));selected=List.filter(scan.entries,Keep);`
	const guard = `flow Eligible(original:File.Entry,current:File.Entry)->Bool{
    flow EligibleChoiceTest1(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return Map.has_key(Map.new(Record.new(key: false, value: true)), request.selector)
    }

    flow EligibleChoiceResult1(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return false
    }

    flow EligibleChoice2Test1(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return Map.has_key(Map.new(Record.new(key: false, value: true)), request.selector)
    }

    flow EligibleChoice2Result1(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return false
    }

    flow EligibleChoice3Test1(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return Map.has_key(Map.new(Record.new(key: false, value: true)), request.selector)
    }

    flow EligibleChoice3Result1(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return false
    }

    flow EligibleChoice3Default(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return Math.eq(request.original.link_count,request.current.link_count)
    }

    flow EligibleChoice2Default(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return Record.new(selector: Math.eq(request.original.changed_unix_ns,request.current.changed_unix_ns), current: request.current, original: request.original) |> Match.apply(Match.new(List.new(Match.Case.new(EligibleChoice3Test1, EligibleChoice3Result1)), EligibleChoice3Default))
    }

    flow EligibleChoiceDefault(request: Record{current: File.Entry, original: File.Entry, selector: Bool}) -> Bool {
        return Record.new(selector: Math.eq(request.original.accessed_unix_ns,request.current.accessed_unix_ns), current: request.current, original: request.original) |> Match.apply(Match.new(List.new(Match.Case.new(EligibleChoice2Test1, EligibleChoice2Result1)), EligibleChoice2Default))
    }

    return Record.new(selector: Math.eq(original.owner_uid,current.owner_uid), current: current, original: original) |> Match.apply(Match.new(List.new(Match.Case.new(EligibleChoiceTest1, EligibleChoiceResult1)), EligibleChoiceDefault))};`
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
				valueJSON(t, fields, `[{"type":"Record{access: Int, change: Int, owner: Int}","value":{"access":{"type":"Int","value":"-876543211"},"change":{"type":"Int","value":"-1000000000"},"owner":{"type":"Int","value":"1001"}}},{"type":"Record{access: Int, change: Int, owner: Int}","value":{"access":{"type":"Int","value":"-876543211"},"change":{"type":"Int","value":"-1000000000"},"owner":{"type":"Int","value":"1001"}}}]`)
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
