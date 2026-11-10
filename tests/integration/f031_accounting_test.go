package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestF031JSONAccounting(t *testing.T) {
	runLLVMRuntimeScenarios(t, f031AccountingScenarios())
}

func TestF031LinuxJSONAccounting(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f031AccountingScenarios())
}

func f031AccountingScenarios() []llvmRuntimeScenario {
	items := strings.TrimSuffix(strings.Repeat("0,", 64), ",")
	document := "[" + strings.TrimSuffix(strings.Repeat("0,", 1024), ",") + "]"
	broken := strings.TrimSuffix(document, "]")
	success := fmt.Sprintf(`flow Visit(previous:Int,item:Int)->Int!{value=%s;return Math.add(previous,JSON.length(value))};flow Main()->Bool!{return Math.eq(List.fold(List.new(%s),0,Visit),65536)};return Main()`, f031Decode(document), items)
	failure := fmt.Sprintf(`flow Failed(error:Error)->Int{return Match.new(String.eq(error.kind,"JSON.InvalidData"),true:1,_:0)};flow Parse()->Int onError Failed{value=%s;return 0};flow Visit(previous:Int,item:Int)->Int{return Math.add(previous,Parse())};return Math.eq(List.fold(List.new(%s),0,Visit),64)`, f031Decode(broken), items)
	large := strings.Repeat("x", 262144)
	detached := func(document, extraction string) string {
		return fmt.Sprintf(`flow Select(index:Int)->Record{next_state:Int,value:JSON.Value}!{document=%s;selected=%s;return Record.new(next_state:Math.add(index,1),value:selected)};flow Main()->Bool!{values=List.unfold(0,16,Select);return Math.eq(List.length(values),16)};return Main()`, f031Decode(document), extraction)
	}
	const deep = 65536
	deepDocument := strings.Repeat("[", deep) + "0" + strings.Repeat("]", deep)
	setup, typ := checkpointSetup("Parameters")
	checkpoint := fmt.Sprintf(`flow Failed(error:Error)->Bool{return String.eq(error.kind,"Checkpoint.InvalidData")};flow Main()->Bool onError Failed{%s value=Checkpoint.decode(Bytes.new(%q),original);return false};return Main()`, setup, strings.Repeat("[", 16384)+"0"+strings.Repeat("]", 16384))
	canonical := fmt.Sprintf(`flow Main()->%s!{%s return Checkpoint.decode(Bytes.new(%q),original)};return Main()`, typ, setup, llvmCheckpointParameters)
	fixtures := []struct{ name, source string }{
		{"repeated-success-releases-scratch", success},
		{"repeated-failure-releases-scratch", failure},
		{"object-subvalue-detaches-large-sibling", detached(`{"tiny":true,"large":"`+large+`"}`, `JSON.get(document,"tiny")`)},
		{"array-subvalue-detaches-large-sibling", detached(`[true,"`+large+`"]`, `JSON.at(document,0)`)},
		{"cancellation-after-parser-allocation", `flow Caught(error:Error)->String{return "caught"};flow Main()->String onError Caught{return JSON.kind(` + f031Decode(document) + `)};return Main()`},
		{"deep-native-diagnostic-budget", `flow Fail(value:JSON.Value)->Int!{return Error.new("Fixture.DeepJSON","deep",Record.new(value:value))};flow Main()->Int!{return Fail(` + f031Decode(deepDocument) + `)};return Main()`},
		{"deep-checkpoint-error-and-release", checkpoint},
		{"checkpoint-depth-denied", canonical},
		{"checkpoint-depth-admitted", canonical},
		{"strict-checkpoint-decoded-keys", f031StrictCheckpointSource()},
		{"deep-guard-native-diagnostic-budget", `flow Eligible(context:JSON.Value,original:File.Entry,current:File.Entry)->Bool{return true};flow Fail(value:File.Remove)->Int!{return Error.new("Fixture.GuardJSON","deep",Record.new(value:value))};flow Main()->Int!{document=` + f031Decode(deepDocument) + `;target=File.Remove.checked(List.new(),document,Eligible);return Fail(target)};return Main()`},
		{"invalid-bytes-native-diagnostic-budget", `return ` + f031Decode("!"+strings.Repeat(" ", 2<<20))},
		{"wide-json-list-native-diagnostic-budget", `flow Item(value:JSON.Value,index:Int)->Record{next_state:Int,value:JSON.Value}{return Record.new(next_state:Math.add(index,1),value:value)};flow Main()->Int!{value=JSON.decode(Bytes.new("null"));values=List.unfold(0,65536,value,Item);return Error.new("Fixture.WideJSON","wide",Record.new(values:values))};return Main()`},
	}
	scenarios := make([]llvmRuntimeScenario, 0, len(fixtures))
	for number, fixture := range fixtures {
		if number == 0 || number == 2 || number == 3 || number == 9 {
			fixture.source = `flow UnexpectedFailure(error:Error)->Bool{return false};` + strings.Replace(fixture.source, "flow Main()->Bool!{", "flow Main()->Bool onError UnexpectedFailure{", 1)
		}
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: fixture.name, number: number, source: fixture.source,
			harness: "testdata/native-runtime/f031-json-accounting.c",
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				if (number <= 5 || number >= 10) && !strings.Contains(string(module), "@lo_op_JSON_2Edecode(") {
					t.Fatal("missing public JSON.decode boundary")
				}
				return []byte(strings.ReplaceAll(string(module), "@lo_op_JSON_2Edecode(", "@lo_f031_accounted_decode("))
			},
			verify: func(t *testing.T, actual result) {
				t.Helper()
				if actual.status != 0 || actual.stderr != "" || actual.stdout != "" {
					t.Fatalf("JSON accounting scenario %s: %+v", fixture.name, actual)
				}
			},
		})
	}
	return scenarios
}

func f031StrictCheckpointSource() string {
	setup, typ := checkpointSetup("Parameters")
	fixtures := []string{
		strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"dtype":"fp32","d\u0074ype":"fp16"`, 1),
		strings.Replace(llvmCheckpointParameters, `"type":`, `"TYPE":`, 1),
		strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"DTYPE":"fp16"`, 1),
		strings.Replace(llvmCheckpointParameters, `"dtype":"fp32"`, `"dtype":"\ud800"`, 1),
		strings.Replace(llvmCheckpointParameters, `"device":"cpu"`, `"device":"\udc00"`, 1),
	}
	var flows, checks []string
	for index, document := range fixtures {
		flows = append(flows, fmt.Sprintf(`flow Failed%d(error:Error)->Bool{return String.eq(error.kind,"Checkpoint.InvalidData")};flow Check%d()->Bool onError Failed%d{%s value=Checkpoint.decode(Bytes.new(%q),original);return false}`, index, index, index, setup, document))
		checks = append(checks, fmt.Sprintf("Check%d()", index))
	}
	flows = append(flows, fmt.Sprintf(`flow Canonical()->%s!{%s return Checkpoint.decode(Bytes.new(%q),original)}`, typ, setup, llvmCheckpointParameters))
	return strings.Join(flows, "\n") + `
flow Both(previous:Bool,value:Bool)->Bool{return Match.new(previous,true:value,_:false)}
flow Main()->Bool!{control=Canonical();return List.fold(List.new(` + strings.Join(checks, ",") + `),true,Both)};return Main()`
}
