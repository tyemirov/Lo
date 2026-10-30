package integration_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestLLVMLinuxResourcePolicyOptions(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", []llvmRuntimeScenario{{
		name: "explicit-host-policy", source: `return List.new(1, 2)`,
		harness: "testdata/native-runtime/resource-policy.c",
	}})
}

func TestLLVMListAliasFoldAllocationScaling(t *testing.T) {
	listAliasFoldAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxListAliasFoldAllocationScaling(t *testing.T) {
	listAliasFoldAllocationScaling(t, "linux/arm64")
}

type collectionAllocationSample struct {
	Bytes       uint64 `json:"bytes"`
	Allocations uint64 `json:"allocations"`
}

func listAliasFoldAllocationScaling(t *testing.T, target string) {
	t.Helper()
	counts := []int{128, 256, 512}
	samples := make(map[int]collectionAllocationSample, len(counts))
	var scenarios []llvmRuntimeScenario
	for _, count := range counts {
		source := fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Same(state: List<Int>, item: Int) -> List<Int> { return state }; items = List.unfold(0, %d, Next); return List.length(List.fold(items, items, Same))`, count)
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: strconv.Itoa(count), source: source, number: count,
			harness: "testdata/native-runtime/collection-scope-allocations.c",
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(module), "call void @lo_op_List_2Efold(", "call void @lo_profile_fold(")
				return []byte(text + "\ndeclare void @lo_profile_fold(ptr,ptr,i64,i64,ptr)\n")
			},
			verify: func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("list-state allocation fixture: %+v", got)
				}
				lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("expected scalar result and allocation record: %q", got.stdout)
				}
				valueJSON(t, fileOutput(t, result{stdout: lines[0]}), strconv.Quote(strconv.Itoa(count)))
				var sample collectionAllocationSample
				if err := json.Unmarshal([]byte(lines[1]), &sample); err != nil || sample.Bytes == 0 || sample.Allocations == 0 {
					t.Fatalf("allocation record: %s: %v", lines[1], err)
				}
				if sample.Allocations > 4 || sample.Bytes > 128 {
					t.Errorf("returning an inherited list requires constant allocation work: %+v", sample)
				}
				samples[count] = sample
				t.Logf("items=%d allocations=%d bytes=%d", count, sample.Allocations, sample.Bytes)
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
			t.Errorf("doubling aliased list and callback count from %d to %d exceeds linear allocation growth: %+v -> %+v", counts[index-1], counts[index], previous, current)
		}
	}
}

func TestLLVMScalarFoldAllocationBudget(t *testing.T) {
	scalarFoldAllocationBudget(t, semanticNativeTarget())
}

func TestLLVMLinuxScalarFoldAllocationBudget(t *testing.T) {
	scalarFoldAllocationBudget(t, "linux/arm64")
}

func scalarFoldAllocationBudget(t *testing.T, target string) {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, count := range []int{128, 512, 2048} {
		source := fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Sum(state: Int, item: Int) -> Int { return Math.add(state, item) }; items = List.unfold(0, %d, Next); return List.fold(items, 0, Sum)`, count)
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: strconv.Itoa(count), source: source, number: count,
			harness: "testdata/native-runtime/collection-scope-allocations.c",
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(module), "call void @lo_op_List_2Efold(", "call void @lo_profile_fold(")
				return []byte(text + "\ndeclare void @lo_profile_fold(ptr,ptr,i64,i64,ptr)\n")
			},
			verify: func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("scalar fold allocation fixture: %+v", got)
				}
				lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("expected scalar result and allocation record: %q", got.stdout)
				}
				actual := fileOutput(t, result{stdout: lines[0]})
				if actual.Type != "Int" {
					t.Fatalf("scalar fold result type: %s", actual.Type)
				}
				valueJSON(t, actual, strconv.Quote(strconv.Itoa(count*(count-1)/2)))
				var sample collectionAllocationSample
				if err := json.Unmarshal([]byte(lines[1]), &sample); err != nil || sample.Bytes == 0 || sample.Allocations == 0 {
					t.Fatalf("allocation record: %s: %v", lines[1], err)
				}
				t.Logf("items=%d allocations=%d bytes=%d", count, sample.Allocations, sample.Bytes)
				if sample.Allocations > uint64(2*count+32) || sample.Bytes > uint64(208*count+1024) {
					t.Errorf("scalar fold exceeds per-item allocation budget at %d callbacks: %+v", count, sample)
				}
			},
		})
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}

func TestLLVMWorklistPinAllocationBudget(t *testing.T) {
	worklistPinAllocationBudget(t, semanticNativeTarget())
}

func TestLLVMLinuxWorklistPinAllocationBudget(t *testing.T) {
	worklistPinAllocationBudget(t, "linux/arm64")
}

func worklistPinAllocationBudget(t *testing.T, target string) {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, count := range []int{128, 512, 2048} {
		source := fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Children(item: Int) -> List<Int> { return Match.new(item, 0: List.unfold(1, %d, Next), _: List.new()) }; flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, 1), items: Children(item), stop: false) }; return Flow.worklist(List.new(0), 0, Step)`, count)
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: strconv.Itoa(count), source: source, number: count,
			harness: "testdata/native-runtime/collection-scope-allocations.c",
			harnessTransform: func(t *testing.T, data []byte) []byte {
				t.Helper()
				return []byte(strings.ReplaceAll(string(data), "lo_op_List_2Efold", "lo_op_Flow_2Eworklist"))
			},
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(module), "call void @lo_op_Flow_2Eworklist(", "call void @lo_profile_fold(")
				return []byte(text + "\ndeclare void @lo_profile_fold(ptr,ptr,i64,i64,ptr)\n")
			},
			verify: func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("worklist pin allocation fixture: %+v", got)
				}
				lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("expected scalar result and allocation record: %q", got.stdout)
				}
				valueJSON(t, fileOutput(t, result{stdout: lines[0]}), strconv.Quote(strconv.Itoa(count+1)))
				var sample collectionAllocationSample
				if err := json.Unmarshal([]byte(lines[1]), &sample); err != nil || sample.Bytes == 0 || sample.Allocations == 0 {
					t.Fatalf("allocation record: %s: %v", lines[1], err)
				}
				t.Logf("items=%d allocations=%d bytes=%d", count, sample.Allocations, sample.Bytes)
				if sample.Allocations > uint64(51*count+192) || sample.Bytes > uint64(5264*count+8192) {
					t.Errorf("worklist exceeds discovered-item pin allocation budget at %d children: %+v", count, sample)
				}
			},
		})
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}

func TestLLVMUnfoldTraversalAllocationBudget(t *testing.T) {
	unfoldTraversalAllocationBudget(t, semanticNativeTarget())
}

func TestLLVMLinuxUnfoldTraversalAllocationBudget(t *testing.T) {
	unfoldTraversalAllocationBudget(t, "linux/arm64")
}

func unfoldTraversalAllocationBudget(t *testing.T, target string) {
	t.Helper()
	var scenarios []llvmRuntimeScenario
	for _, count := range []int{128, 512, 2048} {
		source := fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; return List.length(List.unfold(0, %d, Next))`, count)
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: strconv.Itoa(count), source: source, number: count,
			harness: "testdata/native-runtime/collection-scope-allocations.c",
			harnessTransform: func(t *testing.T, data []byte) []byte {
				t.Helper()
				return []byte(strings.ReplaceAll(string(data), "lo_op_List_2Efold", "lo_op_List_2Eunfold"))
			},
			transform: func(t *testing.T, module []byte) []byte {
				t.Helper()
				text := strings.ReplaceAll(string(module), "call void @lo_op_List_2Eunfold(", "call void @lo_profile_fold(")
				return []byte(text + "\ndeclare void @lo_profile_fold(ptr,ptr,i64,i64,ptr)\n")
			},
			verify: func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("unfold allocation fixture: %+v", got)
				}
				lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("expected scalar result and allocation record: %q", got.stdout)
				}
				actual := fileOutput(t, result{stdout: lines[0]})
				if actual.Type != "Int" {
					t.Fatalf("unfold result type: %s", actual.Type)
				}
				valueJSON(t, actual, strconv.Quote(strconv.Itoa(count)))
				var sample collectionAllocationSample
				if err := json.Unmarshal([]byte(lines[1]), &sample); err != nil || sample.Bytes == 0 || sample.Allocations == 0 {
					t.Fatalf("allocation record: %s: %v", lines[1], err)
				}
				t.Logf("items=%d allocations=%d bytes=%d", count, sample.Allocations, sample.Bytes)
				if sample.Allocations > uint64(11*count+64) || sample.Bytes > uint64(1408*count+4096) {
					t.Errorf("unfold exceeds per-item traversal allocation budget at %d callbacks: %+v", count, sample)
				}
			},
		})
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}
