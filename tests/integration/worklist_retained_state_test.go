package integration_test

import "testing"

const worklistWideBatchPreservedState = `flow Child(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: Math.add(state, 1), next_state: Math.add(state, 1)) }; flow Children(item: Int) -> List<Int> { return Match.new(item, 0: List.unfold(0, 4096, Child), _: List.new()) }; flow Step(state: Record{count: Int, preserved: Int}, item: Int) -> Record{state: Record{count: Int, preserved: Int}, items: List<Int>, stop: Bool} { children = Children(item); preserved = Match.new(item, 0: Math.add(state.preserved, 1), _: state.preserved); next = Record.new(count: Math.add(state.count, 1), preserved: preserved); return Record.new(state: next, items: children, stop: false) }; flow Run() -> Int { result = Flow.worklist(List.new(0), Record.new(count: 0, preserved: 0), Step); return Math.add(result.count, result.preserved) }; return Run()`

func TestWorklistWideBatchPreservesScalarState(t *testing.T) {
	valueJSON(t, runValue(t, worklistWideBatchPreservedState), `"4098"`)
}

func TestLLVMLinuxWideBatchPreservesScalarState(t *testing.T) {
	artifact := linuxLLVMArtifact(t, worklistWideBatchPreservedState)
	got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
	valueJSON(t, fileOutput(t, got), `"4098"`)
}
