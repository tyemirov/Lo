package integration_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestLLVMFileRemovalAllocationScaling(t *testing.T) {
	fileRemovalAllocationScaling(t, semanticNativeTarget())
}

func TestLLVMLinuxFileRemovalAllocationScaling(t *testing.T) {
	fileRemovalAllocationScaling(t, "linux/arm64")
}

type removalAllocationSample struct {
	Bytes       uint64 `json:"bytes"`
	Allocations uint64 `json:"allocations"`
}

func fileRemovalAllocationScaling(t *testing.T, target string) {
	t.Helper()
	for _, checked := range []bool{false, true} {
		t.Run(fmt.Sprintf("checked=%t", checked), func(t *testing.T) {
			counts := []int{64, 128, 256}
			samples := make(map[int]removalAllocationSample, len(counts))
			var scenarios []llvmRuntimeScenario
			for _, count := range counts {
				indices := make([]string, count)
				for index := range indices {
					indices[index] = strconv.Itoa(index)
				}
				constructor := "File.Remove.new(selected)"
				if checked {
					constructor = "File.Remove.checked(selected, Accept)"
				}
				source := fmt.Sprintf(`flow Create(total: Int, index: Int) -> Int! { path = File.Path.new(String.concat("selected-", Int.to_string(index))); write = File.Write.new(path, Bytes.new("x")); commit write; return Math.add(total, 1) }; flow Select(entry: File.Entry) -> Bool { return Math.eq(entry.size_bytes, 1) }; flow Accept(original: File.Entry, current: File.Entry) -> Bool { return true }; flow Main() -> Int! { created = Flow.fold(List.new(%s), 0, Create); entries = File.list(File.Path.new(".")); selected = List.filter(entries, Select); removal = %s; receipt = commit removal; return receipt.completed_count }; return Main()`, strings.Join(indices, ","), constructor)
				scenarios = append(scenarios, llvmRuntimeScenario{
					name:    strconv.Itoa(count),
					source:  source,
					number:  count,
					harness: "testdata/native-runtime/file-removal-allocations.c",
					transform: func(t *testing.T, module []byte) []byte {
						t.Helper()
						text := strings.ReplaceAll(string(module), "call void @lo_commit(", "call void @lo_profile_commit(")
						text = strings.Replace(text, "define void @lo_entry(", "define void @lo_profiled_entry(", 1)
						return []byte(text + "\ndeclare void @lo_profile_commit(ptr,ptr,i64,i64,ptr)\n")
					},
					verify: func(t *testing.T, got result) {
						t.Helper()
						if got.status != 0 || got.stderr != "" {
							t.Fatalf("removal allocation fixture: %+v", got)
						}
						lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
						if len(lines) != 2 {
							t.Fatalf("expected result and allocation record: %q", got.stdout)
						}
						valueJSON(t, fileOutput(t, result{stdout: lines[0]}), strconv.Quote(strconv.Itoa(count)))
						var sample removalAllocationSample
						if err := json.Unmarshal([]byte(lines[1]), &sample); err != nil || sample.Bytes == 0 || sample.Allocations == 0 {
							t.Fatalf("allocation record: %s: %v", lines[1], err)
						}
						samples[count] = sample
						t.Logf("entries=%d allocations=%d bytes=%d", count, sample.Allocations, sample.Bytes)
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
					t.Errorf("doubling removal batch from %d to %d exceeds linear allocation growth: %+v -> %+v", counts[index-1], counts[index], previous, current)
				}
			}
		})
	}
}
