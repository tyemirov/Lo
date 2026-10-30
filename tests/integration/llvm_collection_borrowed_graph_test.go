package integration_test

import (
	encodingbinary "encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestLLVMBorrowedGraphResourcePolicy(t *testing.T) {
	borrowedGraphResourcePolicy(t, semanticNativeTarget())
}

func TestLLVMLinuxBorrowedGraphResourcePolicy(t *testing.T) {
	borrowedGraphResourcePolicy(t, "linux/arm64")
}

func borrowedGraphResourcePolicy(t *testing.T, target string) {
	t.Helper()
	const shared = `flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Same(state: List<Int>, item: Int) -> List<Int> { unused = Math.add(item, 1); return state }; flow Warm(state: Host.Invocation) -> Host.Invocation { unused = Math.add(0, 1); return state }; flow Borrow(state: Host.Invocation, item: Int) -> Host.Invocation { count = List.length(state.arguments); unused = Math.add(count, item); return state }; flow Failed(error: Error) -> Bool { return false }; `
	const fold = shared + `flow Run() -> Bool onError Failed { initial = Warm(Host.Invocation.current()); numbers = List.unfold(0, 64, Next); kept = List.fold(numbers, numbers, Same); result = List.fold(kept, initial, Borrow); return Math.eq(List.length(result.arguments), 16) }; return Run()`
	const pins = shared + `flow Children(count: Int, item: Host.Invocation) -> List<Host.Invocation> { return Match.new(count, 0: List.new(item, item), _: List.new()) }; flow Step(state: Record{count: Int, host: Host.Invocation}, item: Host.Invocation) -> Record{state: Record{count: Int, host: Host.Invocation}, items: List<Host.Invocation>, stop: Bool} { count = List.length(item.arguments); children = Children(state.count, item); return Record.new(state: Record.new(count: Math.add(state.count, 1), host: item), items: children, stop: Math.eq(state.count, 2)) }; flow Run() -> Bool onError Failed { initial = Warm(Host.Invocation.current()); result = Flow.worklist(List.new(initial), Record.new(count: 0, host: initial), Step); return Match.new(result.count, 3: Math.eq(List.length(result.host.arguments), 16), _: false) }; return Run()`
	const eviction = shared + `flow Run() -> Bool onError Failed { initial = Host.Invocation.current(); a = List.unfold(0, 16, Next); b = List.unfold(0, 17, Next); c = List.unfold(0, 18, Next); d = List.unfold(0, 19, Next); e = List.unfold(0, 20, Next); aa = List.fold(a, a, Same); bb = List.fold(b, b, Same); cc = List.fold(c, c, Same); dd = List.fold(d, d, Same); ee = List.fold(e, e, Same); again = List.fold(a, a, Same); lengths = List.new(List.length(aa), List.length(bb), List.length(cc), List.length(dd), List.length(ee), List.length(again)); return Math.eq(Math.sum(lengths), 106) }; return Run()`
	const queued = shared + `flow Item(host: Host.Invocation, state: Int) -> Record{value: Record{host: Host.Invocation, index: Int}, next_state: Int} { return Record.new(value: Record.new(host: host, index: state), next_state: Math.add(state, 1)) }; flow Children(host: Host.Invocation, count: Int) -> List<Record{host: Host.Invocation, index: Int}> { return Match.new(count, 0: List.unfold(1, 32, host, Item), _: List.new()) }; flow Step(state: Record{count: Int, host: Host.Invocation}, item: Record{host: Host.Invocation, index: Int}) -> Record{state: Record{count: Int, host: Host.Invocation}, items: List<Record{host: Host.Invocation, index: Int}>, stop: Bool} { observed = List.length(item.host.arguments); children = Children(item.host, state.count); return Record.new(state: Record.new(count: Math.add(state.count, 1), host: item.host), items: children, stop: false) }; flow Run() -> Bool onError Failed { initial = Warm(Host.Invocation.current()); result = Flow.worklist(List.new(Record.new(host: initial, index: 0)), Record.new(count: 0, host: initial), Step); return Match.new(result.count, 33: Math.eq(List.length(result.host.arguments), 16), _: false) }; return Run()`
	encode := func(count int) string {
		body := encodingbinary.BigEndian.AppendUint32(nil, uint32(count))
		for index := 0; index < count; index++ {
			argument := strings.Repeat("a", 32)
			body = encodingbinary.BigEndian.AppendUint32(body, uint32(len(argument)+1))
			body = append(body, argument...)
			body = append(body, 0)
		}
		body = encodingbinary.BigEndian.AppendUint32(body, 2)
		body = append(body, '/', 0)
		body = encodingbinary.BigEndian.AppendUint32(body, 0)
		body = encodingbinary.BigEndian.AppendUint32(body, 0)
		body = encodingbinary.BigEndian.AppendUint64(body, 0)
		encoded := make([]string, len(body))
		for index, octet := range body {
			encoded[index] = strconv.Itoa(int(octet))
		}
		return strings.Join(encoded, ",")
	}
	var scenarios []llvmRuntimeScenario
	for number, fixture := range []struct{ name, source string }{{"late-descendant-and-expired-roots", fold}, {"overlapping-cached-subtree-pins", pins}, {"five-live-roots-and-revisit-after-eviction", eviction}, {"small-cached-graph-amid-distinct-queued-pins", queued}} {
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: fixture.name, source: fixture.source, number: number,
			harness: "testdata/native-runtime/borrowed-graph-policy.c",
			harnessTransform: func(t *testing.T, data []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(data), "@HOST_BODY@", encode(16))
				return []byte(strings.ReplaceAll(text, "@LARGE_HOST_BODY@", encode(128)))
			},
		})
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}

func TestLLVMFallibleListAliasFoldAllocationScaling(t *testing.T) {
	borrowedGraphFoldAllocationScaling(t, semanticNativeTarget(), `flow Same(state: List<Int>, item: Int) -> List<Int>! { return state }`)
}

func TestLLVMLinuxFallibleListAliasFoldAllocationScaling(t *testing.T) {
	borrowedGraphFoldAllocationScaling(t, "linux/arm64", `flow Same(state: List<Int>, item: Int) -> List<Int>! { return state }`)
}

func TestLLVMListAliasFoldUnusedLocalAllocationScaling(t *testing.T) {
	borrowedGraphFoldAllocationScaling(t, semanticNativeTarget(), `flow Same(state: List<Int>, item: Int) -> List<Int> { unused = Math.add(item, 1); return state }`)
}

func TestLLVMLinuxListAliasFoldUnusedLocalAllocationScaling(t *testing.T) {
	borrowedGraphFoldAllocationScaling(t, "linux/arm64", `flow Same(state: List<Int>, item: Int) -> List<Int> { unused = Math.add(item, 1); return state }`)
}

func TestLLVMFallibleListAliasFoldListQuotaAllocationScaling(t *testing.T) {
	borrowedGraphFoldAllocationScalingWithQuota(t, semanticNativeTarget(), `flow Same(state: List<Int>, item: Int) -> List<Int>! { return state }`, true)
}

func TestLLVMLinuxFallibleListAliasFoldListQuotaAllocationScaling(t *testing.T) {
	borrowedGraphFoldAllocationScalingWithQuota(t, "linux/arm64", `flow Same(state: List<Int>, item: Int) -> List<Int>! { return state }`, true)
}

func borrowedGraphFoldAllocationScaling(t *testing.T, target, callback string) {
	borrowedGraphFoldAllocationScalingWithQuota(t, target, callback, false)
}

func borrowedGraphFoldAllocationScalingWithQuota(t *testing.T, target, callback string, quota bool) {
	borrowedGraphFixtureAllocationScaling(t, target, quota, func(count int) string {
		return fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; %s; flow Run() -> Int! { items = List.unfold(0, %d, Next); return List.length(List.fold(items, items, Same)) }; return Run()`, callback, count)
	}, func(count int) int { return count }, 16, 1024)
}

func TestLLVMRecordListStateAllocationScaling(t *testing.T) {
	recordListStateAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxRecordListStateAllocationScaling(t *testing.T) {
	recordListStateAllocationScaling(t, "linux/arm64")
}

func recordListStateAllocationScaling(t *testing.T, target string) {
	borrowedGraphFixtureAllocationScaling(t, target, true, func(count int) string {
		return fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Same(state: Record{left: List<Int>, right: List<Int>, count: Int}, item: Int) -> Record{left: List<Int>, right: List<Int>, count: Int} { return Record.new(left: state.left, right: state.right, count: Math.add(state.count, 1)) }; flow Run() -> Int { left = List.unfold(0, %d, Next); right = List.unfold(%d, %d, Next); state = List.fold(left, Record.new(left: left, right: right, count: 0), Same); return Math.add(state.count, Math.add(Math.sum(state.left), Math.sum(state.right))) }; return Run()`, count, count, count)
	}, func(count int) int { return 2 * count * count }, 48, 3072)
}

func TestLLVMFiveListRecordStateAllocationScaling(t *testing.T) {
	fiveListRecordStateAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxFiveListRecordStateAllocationScaling(t *testing.T) {
	fiveListRecordStateAllocationScaling(t, "linux/arm64")
}

func fiveListRecordStateAllocationScaling(t *testing.T, target string) {
	borrowedGraphFixtureAllocationScaling(t, target, true, func(count int) string {
		var fields, updated, bindings, initial, sums []string
		for index, name := range []string{"a", "b", "c", "d", "e"} {
			fields = append(fields, name+": List<Int>")
			updated = append(updated, name+": state."+name)
			bindings = append(bindings, fmt.Sprintf("%s = List.unfold(%d, %d, Next)", name, index*count, count))
			initial = append(initial, name+": "+name)
			sums = append(sums, "Math.sum(state."+name+")")
		}
		typ := "Record{" + strings.Join(fields, ", ") + ", count: Int}"
		return fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Same(state: %s, item: Int) -> %s { return Record.new(%s, count: Math.add(state.count, 1)) }; flow Run() -> Int { %s; state = List.fold(a, Record.new(%s, count: 0), Same); return Math.add(state.count, Math.sum(List.new(%s))) }; return Run()`, typ, typ, strings.Join(updated, ", "), strings.Join(bindings, "; "), strings.Join(initial, ", "), strings.Join(sums, ", "))
	}, func(count int) int { return (25*count*count - 3*count) / 2 }, 80, 5120)
}

func borrowedGraphFixtureAllocationScaling(t *testing.T, target string, quota bool, sourceForCount func(int) string, expectedForCount func(int) int, requestsPerItem, bytesPerItem uint64) {
	t.Helper()
	counts := []int{128, 256, 512}
	samples := make(map[int]collectionAllocationSample, len(counts))
	var scenarios []llvmRuntimeScenario
	for _, count := range counts {
		source := sourceForCount(count)
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: strconv.Itoa(count), source: source, number: count,
			harness: "testdata/native-runtime/collection-scope-allocations.c",
			harnessTransform: func(t *testing.T, data []byte) []byte {
				t.Helper()
				if !quota {
					return data
				}
				text := strings.ReplaceAll(string(data), `"--max-steps", "10000000"`, fmt.Sprintf(`"--max-steps", "10000000", "--max-list-items", "%d"`, count))
				return []byte(strings.ReplaceAll(text, "lo_cli_main(6, arguments, 0)", "lo_cli_main(8, arguments, 0)"))
			},
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(module), "call void @lo_op_List_2Efold(", "call void @lo_profile_fold(")
				return []byte(text + "\ndeclare void @lo_profile_fold(ptr,ptr,i64,i64,ptr)\n")
			},
			verify: func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("borrowed-list allocation fixture: %+v", got)
				}
				lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("expected scalar result and allocation record: %q", got.stdout)
				}
				valueJSON(t, fileOutput(t, result{stdout: lines[0]}), strconv.Quote(strconv.Itoa(expectedForCount(count))))
				var sample collectionAllocationSample
				if err := json.Unmarshal([]byte(lines[1]), &sample); err != nil || sample.Bytes == 0 || sample.Allocations == 0 {
					t.Fatalf("allocation record: %s: %v", lines[1], err)
				}
				samples[count] = sample
				t.Logf("items=%d allocations=%d bytes=%d", count, sample.Allocations, sample.Bytes)
				if sample.Allocations > requestsPerItem*uint64(count)+64 || sample.Bytes > bytesPerItem*uint64(count)+4096 {
					t.Errorf("inherited-list callback exceeds linear allocation budget at %d callbacks: %+v", count, sample)
				}
			},
		})
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
	for index := 1; index < len(counts); index++ {
		previous, current := samples[counts[index-1]], samples[counts[index]]
		if previous.Allocations == 0 || current.Allocations == 0 {
			continue
		}
		if current.Allocations > 3*previous.Allocations || current.Bytes > 3*previous.Bytes {
			t.Errorf("doubling inherited-list length and callback count from %d to %d exceeds linear allocation growth: %+v -> %+v", counts[index-1], counts[index], previous, current)
		}
	}
}
