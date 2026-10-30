package integration_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestLLVMReceiptQuotaBoundaries(t *testing.T)      { receiptQuotaBoundaries(t, semanticNativeTarget()) }
func TestLLVMLinuxReceiptQuotaBoundaries(t *testing.T) { receiptQuotaBoundaries(t, "linux/arm64") }

func receiptQuotaBoundaries(t *testing.T, target string) {
	t.Helper()
	const shared = `flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Scale(item: Int) -> Int { return Math.mul(item, 3) }; flow Sum(state: Int, item: Int) -> Int { return Math.add(state, item) }; items = List.unfold(0, 128, Next); `
	var scenarios []llvmRuntimeScenario
	for _, fixture := range []struct {
		name, expression string
		result           int
		live, data       uint64
	}{
		{"literal-map", `return Math.sum(List.map(items, Scale))`, 24384, 8851, 21539},
		{"ancestor-fold", `return List.fold(items, 0, Sum)`, 8128, 4435, 17191},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{
			name: fixture.name, source: shared + fixture.expression, harness: "testdata/native-runtime/receipt-quota.c",
			verify: func(t *testing.T, got result) {
				t.Helper()
				if got.status != 0 || got.stderr != "" {
					t.Fatalf("quota fixture: %+v", got)
				}
				lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
				if len(lines) != 2 {
					t.Fatalf("quota output: %q", got.stdout)
				}
				valueJSON(t, fileOutput(t, result{stdout: lines[0]}), strconv.Quote(strconv.Itoa(fixture.result)))
				var observed struct{ Live, Data uint64 }
				if err := json.Unmarshal([]byte(lines[1]), &observed); err != nil || observed.Live == 0 || observed.Data == 0 {
					t.Fatalf("quota record: %s: %v", lines[1], err)
				}
				if observed.Live != fixture.live || observed.Data != fixture.data {
					t.Errorf("receipt optimization changed quota boundaries: got live=%d data=%d; want live=%d data=%d", observed.Live, observed.Data, fixture.live, fixture.data)
				}
				t.Logf("%s live=%d data=%d", fixture.name, observed.Live, observed.Data)
			},
		})
	}
	runLLVMRuntimeTargetScenarios(t, target, scenarios)
}
