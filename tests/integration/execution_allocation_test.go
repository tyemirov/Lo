package integration_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const allocationSequence = `flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; `

func TestInterpreterCollectionAllocations(t *testing.T) {
	const size = 8192
	for _, test := range []struct {
		name, text, expected string
		requests, bytes      uint64
	}{
		{"unfold", allocationSequence + fmt.Sprintf(`return List.length(List.unfold(0, %d, Next))`, size), "8192", 30, 1500},
		{"map", allocationSequence + fmt.Sprintf(`flow Scale(item: Int) -> Int { return Math.mul(item, 3) }; xs = List.unfold(0, %d, Next); return Math.sum(List.map(xs, Scale))`, size), "100651008", 50, 2500},
		{"fold", allocationSequence + fmt.Sprintf(`flow Sum(state: Int, item: Int) -> Int { return Math.add(state, item) }; xs = List.unfold(0, %d, Next); return List.fold(xs, 0, Sum)`, size), "33550336", 45, 2000},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests, bytes := executionAllocations(t, test.text, test.expected)
			t.Logf("items=%d allocation_count=%d allocation_bytes=%d", size, requests, bytes)
			if requests > size*test.requests+2048 || bytes > size*test.bytes+65536 {
				t.Fatalf("collection allocation cost exceeds budget: requests=%d bytes=%d", requests, bytes)
			}
		})
	}
}

func TestInterpreterWorklistAllocations(t *testing.T) {
	const size = 2048
	text := allocationSequence + fmt.Sprintf(`flow Step(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, 1), items: List.new(), stop: false) }; return Flow.worklist(List.unfold(0, %d, Next), 0, Step)`, size)
	requests, bytes := executionAllocations(t, text, "2048")
	t.Logf("items=%d allocation_count=%d allocation_bytes=%d", size, requests, bytes)
	if requests > size*85+2048 || bytes > size*4300+65536 {
		t.Fatalf("queued scalar allocation cost exceeds budget: requests=%d bytes=%d", requests, bytes)
	}
}

func executionAllocations(t *testing.T, text, expected string) (uint64, uint64) {
	t.Helper()
	return executionAllocationsWithBudget(t, text, expected, execution.DefaultBudget())
}

func executionAllocationsWithBudget(t *testing.T, text, expected string, budget execution.Budget) (uint64, uint64) {
	t.Helper()
	input, err := source.New("execution-allocation.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	checked, problem := compiler.Check(input)
	if problem != nil {
		t.Fatal(problem)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	requests, bytes := ^uint64(0), ^uint64(0)
	for range 3 {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		result, err := program.Run(context.Background(), &observedBoundary{}, budget, device.DefaultConfig())
		runtime.ReadMemStats(&after)
		if err != nil || value.TextResult(result) != expected {
			t.Fatalf("allocation workload result=%v error=%v, want %s", result, err, expected)
		}
		requests = min(requests, after.Mallocs-before.Mallocs)
		bytes = min(bytes, after.TotalAlloc-before.TotalAlloc)
	}
	return requests, bytes
}
