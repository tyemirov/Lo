package integration_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestLLVMProcessorOwnedListStateAllocationScaling(t *testing.T) {
	processorOwnedListStateAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxProcessorOwnedListStateAllocationScaling(t *testing.T) {
	processorOwnedListStateAllocationScaling(t, "linux/arm64")
}

func TestLLVMProcessorOwnedRecordStateAllocationScaling(t *testing.T) {
	processorOwnedRecordStateAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxProcessorOwnedRecordStateAllocationScaling(t *testing.T) {
	processorOwnedRecordStateAllocationScaling(t, "linux/arm64")
}

func TestLLVMOwnedFragmentAnchorsReleaseOmittedState(t *testing.T) {
	ownedFragmentAnchorsReleaseOmittedState(t, semanticNativeTarget())
}

func TestLLVMLinuxOwnedFragmentAnchorsReleaseOmittedState(t *testing.T) {
	ownedFragmentAnchorsReleaseOmittedState(t, "linux/arm64")
}

func TestLLVMProcessorOwnedFiveListRecordStateAllocationScaling(t *testing.T) {
	processorOwnedFiveListRecordStateAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxProcessorOwnedFiveListRecordStateAllocationScaling(t *testing.T) {
	processorOwnedFiveListRecordStateAllocationScaling(t, "linux/arm64")
}

func processorOwnedListStateAllocationScaling(t *testing.T, target string) {
	t.Helper()
	for _, quota := range []bool{false, true} {
		t.Run(fmt.Sprintf("list_quota_%t", quota), func(t *testing.T) {
			borrowedGraphFixtureAllocationScaling(t, target, quota, func(count int) string {
				// The lazy match creates the state once inside the first callback.
				// Later callbacks return the same state owned by the fold scope.
				return fmt.Sprintf(`
flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }
flow Initial(state: List<Int>) -> Bool { return Math.eq(List.length(state), 1) }
flow Initialize(state: List<Int>) -> List<Int> { return List.unfold(0, %[1]d, Next) }
flow Keep(state: List<Int>) -> List<Int> { return state }
flow Hold(matcher: Match<List<Int>, List<Int>>, state: List<Int>, item: Int) -> List<Int> {
    return state |> Match.apply(matcher)
}
matcher = Match.new(List.new(Match.Case.new(Initial, Initialize)), Keep)
items = List.unfold(0, %[1]d, Next)
state = List.fold(items, List.new(0), matcher, Hold)
return Math.add(List.length(state), Math.sum(state))
`, count)
			}, func(count int) int { return count * (count + 1) / 2 }, 48, 4096)
		})
	}
}

func processorOwnedRecordStateAllocationScaling(t *testing.T, target string) {
	t.Helper()
	for _, quota := range []bool{false, true} {
		t.Run(fmt.Sprintf("list_quota_%t", quota), func(t *testing.T) {
			borrowedGraphFixtureAllocationScaling(t, target, quota, func(count int) string {
				// The list stays shared while each callback returns a new record.
				return fmt.Sprintf(`
flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }
flow Initial(state: Record{items: List<Int>, count: Int}) -> Bool { return Math.eq(state.count, 0) }
flow Initialize(state: Record{items: List<Int>, count: Int}) -> List<Int> { return List.unfold(0, %[1]d, Next) }
flow Keep(state: Record{items: List<Int>, count: Int}) -> List<Int> { return state.items }
flow Hold(matcher: Match<Record{items: List<Int>, count: Int}, List<Int>>, state: Record{items: List<Int>, count: Int}, item: Int) -> Record{items: List<Int>, count: Int} {
    items = state |> Match.apply(matcher)
    return Record.new(items: items, count: Math.add(state.count, 1))
}
matcher = Match.new(List.new(Match.Case.new(Initial, Initialize)), Keep)
items = List.unfold(0, %[1]d, Next)
state = List.fold(items, Record.new(items: List.new(0), count: 0), matcher, Hold)
return Math.add(state.count, Math.add(List.length(state.items), Math.sum(state.items)))
`, count)
			}, func(count int) int { return count * (count + 3) / 2 }, 64, 4096)
		})
	}
}

func processorOwnedFiveListRecordStateAllocationScaling(t *testing.T, target string) {
	t.Helper()
	for _, quota := range []bool{false, true} {
		t.Run(fmt.Sprintf("list_quota_%t", quota), func(t *testing.T) {
			borrowedGraphFixtureAllocationScaling(t, target, quota, func(count int) string {
				var schema, initialized, carried, initial, sums []string
				for index, name := range []string{"a", "b", "c", "d", "e"} {
					schema = append(schema, name+": List<Int>")
					initialized = append(initialized, fmt.Sprintf("%s: List.unfold(%d, %d, Next)", name, index*count, count))
					carried = append(carried, name+": state."+name)
					initial = append(initial, name+": List.new(0)")
					sums = append(sums, "Math.sum(state."+name+")")
				}
				stateType := "Record{" + strings.Join(schema, ", ") + ", count: Int}"
				return fmt.Sprintf(`
flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }
flow Initial(state: %[1]s) -> Bool { return Math.eq(state.count, 0) }
flow Initialize(state: %[1]s) -> %[1]s { return Record.new(%[2]s, count: 1) }
flow Keep(state: %[1]s) -> %[1]s { return Record.new(%[3]s, count: Math.add(state.count, 1)) }
flow Hold(matcher: Match<%[1]s, %[1]s>, state: %[1]s, item: Int) -> %[1]s {
    return state |> Match.apply(matcher)
}
matcher = Match.new(List.new(Match.Case.new(Initial, Initialize)), Keep)
items = List.unfold(0, %[4]d, Next)
state = List.fold(items, Record.new(%[5]s, count: 0), matcher, Hold)
return Math.add(state.count, Math.sum(List.new(%[6]s)))
`, stateType, strings.Join(initialized, ", "), strings.Join(carried, ", "), count, strings.Join(initial, ", "), strings.Join(sums, ", "))
			}, func(count int) int { return (25*count*count - 3*count) / 2 }, 160, 16384)
		})
	}
}

func ownedFragmentAnchorsReleaseOmittedState(t *testing.T, target string) {
	t.Helper()
	const count = 128
	text := fmt.Sprintf(`
flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }
flow First(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Bool { return Math.eq(state.count, 0) }
flow Second(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Bool { return Math.eq(state.count, 1) }
flow Drop(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Bool { return Math.eq(state.count, 3) }
flow ProbeReady(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Bool { return Math.eq(state.count, 4) }
flow InitializeA(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Record{a: List<Int>, b: List<Int>, count: Int} { return Record.new(a: List.unfold(0, %[1]d, Next), b: state.b, count: Math.add(state.count, 1)) }
flow InitializeB(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Record{a: List<Int>, b: List<Int>, count: Int} { return Record.new(a: state.a, b: List.unfold(0, %[1]d, Next), count: Math.add(state.count, 1)) }
flow DropB(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Record{a: List<Int>, b: List<Int>, count: Int} { return Record.new(a: state.a, b: List.new(), count: Math.add(state.count, 1)) }
flow Keep(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Record{a: List<Int>, b: List<Int>, count: Int} { return Record.new(a: state.a, b: state.b, count: Math.add(state.count, 1)) }
flow Probe(state: Record{a: List<Int>, b: List<Int>, count: Int}) -> Record{a: List<Int>, b: List<Int>, count: Int} {
    unused = List.unfold(0, %[2]d, Next)
    return Keep(state)
}
flow Hold(matcher: Match<Record{a: List<Int>, b: List<Int>, count: Int}, Record{a: List<Int>, b: List<Int>, count: Int}>, state: Record{a: List<Int>, b: List<Int>, count: Int}, item: Int) -> Record{a: List<Int>, b: List<Int>, count: Int} { return state |> Match.apply(matcher) }
matcher = Match.new(List.new(Match.Case.new(First, InitializeA), Match.Case.new(Second, InitializeB), Match.Case.new(Drop, DropB), Match.Case.new(ProbeReady, Probe)), Keep)
items = List.unfold(0, 5, Next)
state = List.fold(items, Record.new(a: List.new(0), b: List.new(0), count: 0), matcher, Hold)
return Math.add(state.count, Math.add(List.length(state.a), Math.sum(state.a)))
`, count, 2*count)
	runLLVMRuntimeTargetScenarios(t, target, []llvmRuntimeScenario{{
		name: "promote-second-fragment-then-omit", source: text,
		harness: "testdata/native-runtime/receipt-quota.c",
		verify: func(t *testing.T, got result) {
			t.Helper()
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("owned-fragment quota fixture: %+v", got)
			}
			lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
			if len(lines) != 2 {
				t.Fatalf("expected value and quota record: %q", got.stdout)
			}
			valueJSON(t, fileOutput(t, result{stdout: lines[0]}), strconv.Quote(strconv.Itoa(5+count*(count+1)/2)))
			var observed struct{ Live, Data uint64 }
			if err := json.Unmarshal([]byte(lines[1]), &observed); err != nil || observed.Live == 0 || observed.Data == 0 {
				t.Fatalf("owned-fragment quota record: %s: %v", lines[1], err)
			}
			// The final probe fits after b is omitted. Keeping b's receipts
			// would add another expanded list and exceed this live budget.
			if observed.Live > 160*count+1024 {
				t.Fatalf("omitted fragment retained receipt weight: live=%d data=%d", observed.Live, observed.Data)
			}
			t.Logf("live=%d data=%d", observed.Live, observed.Data)
		},
	}})
}
