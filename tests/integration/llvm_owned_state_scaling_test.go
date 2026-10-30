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
				return fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Hold(state: List<Int>, item: Int) -> List<Int> { return Match.new(item, 0: List.unfold(0, %d, Next), _: state) }; flow Run() -> Int { items = List.unfold(0, %d, Next); state = List.fold(items, List.new(0), Hold); return Math.add(List.length(state), Math.sum(state)) }; return Run()`, count, count)
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
				return fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Hold(state: Record{items: List<Int>, count: Int}, item: Int) -> Record{items: List<Int>, count: Int} { items = Match.new(item, 0: List.unfold(0, %d, Next), _: state.items); return Record.new(items: items, count: Math.add(state.count, 1)) }; flow Run() -> Int { items = List.unfold(0, %d, Next); state = List.fold(items, Record.new(items: List.new(0), count: 0), Hold); return Math.add(state.count, Math.add(List.length(state.items), Math.sum(state.items))) }; return Run()`, count, count)
			}, func(count int) int { return count * (count + 3) / 2 }, 64, 4096)
		})
	}
}

func processorOwnedFiveListRecordStateAllocationScaling(t *testing.T, target string) {
	t.Helper()
	for _, quota := range []bool{false, true} {
		t.Run(fmt.Sprintf("list_quota_%t", quota), func(t *testing.T) {
			borrowedGraphFixtureAllocationScaling(t, target, quota, func(count int) string {
				var schema, bindings, carried, initial, sums []string
				for index, name := range []string{"a", "b", "c", "d", "e"} {
					schema = append(schema, name+": List<Int>")
					bindings = append(bindings, fmt.Sprintf("%s = Match.new(item, 0: List.unfold(%d, %d, Next), _: state.%s)", name, index*count, count, name))
					carried = append(carried, name+": "+name)
					initial = append(initial, name+": List.new(0)")
					sums = append(sums, "Math.sum(state."+name+")")
				}
				stateType := "Record{" + strings.Join(schema, ", ") + ", count: Int}"
				return fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Hold(state: %s, item: Int) -> %s { %s; return Record.new(%s, count: Math.add(state.count, 1)) }; flow Run() -> Int { items = List.unfold(0, %d, Next); state = List.fold(items, Record.new(%s, count: 0), Hold); return Math.add(state.count, Math.sum(List.new(%s))) }; return Run()`, stateType, stateType, strings.Join(bindings, "; "), strings.Join(carried, ", "), count, strings.Join(initial, ", "), strings.Join(sums, ", "))
			}, func(count int) int { return (25*count*count - 3*count) / 2 }, 160, 16384)
		})
	}
}

func ownedFragmentAnchorsReleaseOmittedState(t *testing.T, target string) {
	t.Helper()
	const count = 128
	text := fmt.Sprintf(`flow Empty() -> List<Int> { return List.new() }; flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Hold(state: Record{a: List<Int>, b: List<Int>, count: Int}, item: Int) -> Record{a: List<Int>, b: List<Int>, count: Int} { a = Match.new(item, 0: List.unfold(0, %[1]d, Next), _: state.a); b = Match.new(item, 1: List.unfold(0, %[1]d, Next), 3: Empty(), _: state.b); probe = Match.new(item, 4: List.unfold(0, %[2]d, Next), _: Empty()); return Record.new(a: a, b: b, count: Math.add(state.count, 1)) }; flow Run() -> Int { items = List.unfold(0, 5, Next); state = List.fold(items, Record.new(a: List.new(0), b: List.new(0), count: 0), Hold); return Math.add(state.count, Math.add(List.length(state.a), Math.sum(state.a))) }; return Run()`, count, 2*count)
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
