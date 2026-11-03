package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestLLVMRetainedABI(t *testing.T)      { runRetainedABI(t, semanticNativeTarget()) }
func TestLLVMLinuxRetainedABI(t *testing.T) { runRetainedABI(t, "linux/arm64") }

func runRetainedABI(t *testing.T, target string) {
	t.Helper()
	names := []string{"cli-signal-isolation", "simultaneous-owner-cancellation", "success-lifetime", "recoverable-diagnostic", "terminal-diagnostic", "completion-diagnostic", "owner-thread", "repeated-error-frames", "diagnostic-size-bound"}
	scenarios := make([]llvmRuntimeScenario, 0, len(names))
	for number, name := range names {
		source := `return Math.add(1,2)`
		if number <= 1 {
			source = `flow Join(state:String,item:String)->String{return String.concat(state,item)};flow Main()->Int!{initial=Host.Invocation.current();text=List.fold(initial.arguments,"",Join);target=Console.Line.new("stdout",text,false);receipt=commit target;return 7};return Main()`
		}
		if number == 3 {
			source = `flow Main()->Int!{return Math.div(1,0)};return Main()`
		}
		if number == 5 {
			source = cliStackProgram("Program.Exit", `return Program.Exit.new(0,Bytes.new("out"),Bytes.new(""))`)
		}
		if number == 7 {
			source = `flow Inner(x:Int)->Int!{return Math.div(7,x)};flow Outer()->Int!{return Inner(0)};return Outer()`
		}
		if number == 8 {
			var text strings.Builder
			text.WriteString(`flow Main()->Int!{s0="x";`)
			for index := 1; index <= 20; index++ {
				fmt.Fprintf(&text, "s%d=String.concat(s%d,s%d);", index, index-1, index-1)
			}
			text.WriteString(`return Map.get(Map.new(Record.new(key:"present",value:1)),s20)};return Main()`)
			source = text.String()
		}
		scenario := llvmRuntimeScenario{name: name, source: source, number: number, harness: "testdata/native-runtime/retained-abi.c"}
		if number == 0 {
			scenario.verify = func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
					t.Fatalf("CLI signal state crossed the embedding boundary: %+v", got)
				}
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}
