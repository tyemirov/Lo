package integration_test

import "testing"

const worklistWideBatchPreservedState = `flow Child(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: Math.add(state, 1), next_state: Math.add(state, 1)) }; flow Children(item: Int) -> List<Int> {
    flow ChildrenChoiceTest1(value: Int) -> Bool {
        return Math.eq(value, 0)
    }

    flow ChildrenChoiceResult1(value: Int) -> List<Int> {
        return List.unfold(0, 4096, Child)
    }

    flow ChildrenChoiceDefault(value: Int) -> List<Int> {
        return List.new()
    }

    return item |> Match.apply(Match.new(List.new(Match.Case.new(ChildrenChoiceTest1, ChildrenChoiceResult1)), ChildrenChoiceDefault)) }; flow Step(state: Record{count: Int, preserved: Int}, item: Int) -> Record{state: Record{count: Int, preserved: Int}, items: List<Int>, stop: Bool} { children = Children(item); preserved = Record.new(selector: item, state: state) |> Match.apply(Match.new(List.new(Match.Case.new(StepPreservedChoiceTest1, StepPreservedChoiceResult1)), StepPreservedChoiceDefault)); next = Record.new(count: Math.add(state.count, 1), preserved: preserved);
    flow StepPreservedChoiceTest1(request: Record{selector: Int, state: Record{count: Int, preserved: Int}}) -> Bool {
        return Math.eq(request.selector, 0)
    }

    flow StepPreservedChoiceResult1(request: Record{selector: Int, state: Record{count: Int, preserved: Int}}) -> Int {
        return Math.add(request.state.preserved, 1)
    }

    flow StepPreservedChoiceDefault(request: Record{selector: Int, state: Record{count: Int, preserved: Int}}) -> Int {
        return request.state.preserved
    }

    return Record.new(state: next, items: children, stop: false) }; flow Run() -> Int { result = Flow.worklist(List.new(0), Record.new(count: 0, preserved: 0), Step); return Math.add(result.count, result.preserved) }; return Run()`

func TestWorklistWideBatchPreservesScalarState(t *testing.T) {
	valueJSON(t, runValue(t, worklistWideBatchPreservedState), `"4098"`)
}

func TestLLVMLinuxWideBatchPreservesScalarState(t *testing.T) {
	artifact := linuxLLVMArtifact(t, worklistWideBatchPreservedState)
	got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
	valueJSON(t, fileOutput(t, got), `"4098"`)
}
