package integration_test

import (
	"strings"
	"testing"
)

func TestLLVMFileObservationDescriptorLifetime(t *testing.T) {
	runFileObservationDescriptorLifetime(t, semanticNativeTarget())
}
func TestLLVMLinuxFileObservationDescriptorLifetime(t *testing.T) {
	runFileObservationDescriptorLifetime(t, "linux/arm64")
}

func runFileObservationDescriptorLifetime(t *testing.T, target string) {
	t.Helper()
	const makeItems = `flow Next(state:Int)->Record{value:File.Path,next_state:Int}{return Record.new(value:File.Path.new("."),next_state:Math.add(state,1))};items=List.unfold(0,512,Next);`
	var scenarios []llvmRuntimeScenario
	for number, name := range []string{"fold-success", "fold-cancelled", "worklist-success", "worklist-cancelled"} {
		body := `flow Observe(state:Int,path:File.Path)->Int!{directory=File.directory(path);return Math.add(state,1)};`
		expression := `Flow.fold(items,0,Observe)`
		if number >= 2 {
			body = `flow Observe(state:Int,path:File.Path)->Record{state:Int,items:List<File.Path>,stop:Bool}!{directory=File.directory(path);return Record.new(state:Math.add(state,1),items:List.new(),stop:false)};`
			expression = `Flow.worklist(items,0,Observe)`
		}
		scenario := llvmRuntimeScenario{name: name, number: number}
		scenario.source = body + makeItems + "return " + expression
		scenario.harness = "testdata/native-runtime/file-observation-lifetime.c"
		scenario.captureOutput = true
		scenario.transform = func(t *testing.T, module []byte) []byte {
			t.Helper()
			return []byte(strings.Replace(string(module), "define void @lo_entry(", "define void @lo_observed_entry(", 1))
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}
