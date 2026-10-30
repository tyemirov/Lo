package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
)

func TestInterpreterInheritedGraphAllocations(t *testing.T) {
	for _, size := range []int{128, 256, 512} {
		for _, test := range []struct {
			name, output, local string
			listLimit           bool
		}{
			{"fallible", "List<Int>!", "", false},
			{"unused_local", "List<Int>", "unused = Math.add(item, 1);", false},
			{"fallible_list_limit", "List<Int>!", "", true},
			{"unused_local_list_limit", "List<Int>", "unused = Math.add(item, 1);", true},
		} {
			t.Run(fmt.Sprintf("%s/%d", test.name, size), func(t *testing.T) {
				input := execution.DefaultBudgetInput()
				if test.listLimit {
					input.Limits.ListItems = uint64(size)
				}
				budget, err := execution.NewBudget(input)
				if err != nil {
					t.Fatal(err)
				}
				text := allocationSequence + fmt.Sprintf(`flow Same(state: List<Int>, item: Int) -> %s { %s return state }; flow Main() -> Int! { xs = List.unfold(0, %d, Next); return List.length(List.fold(xs, xs, Same)) }; return Main()`, test.output, test.local, size)
				requests, bytes := executionAllocationsWithBudget(t, text, fmt.Sprint(size), budget)
				t.Logf("items=%d allocation_count=%d allocation_bytes=%d", size, requests, bytes)
				if requests > uint64(size)*100+2048 || bytes > uint64(size)*4000+65536 {
					t.Fatalf("inherited graph allocation cost exceeds linear budget: requests=%d bytes=%d", requests, bytes)
				}
			})
		}
	}
}

func TestInterpreterFreshRecordBorrowedGraphAllocations(t *testing.T) {
	for _, size := range []int{128, 256, 512} {
		for _, limited := range []bool{false, true} {
			t.Run(fmt.Sprintf("size_%d/list_limit_%t", size, limited), func(t *testing.T) {
				input := execution.DefaultBudgetInput()
				if limited {
					input.Limits.ListItems = uint64(size)
				}
				budget, err := execution.NewBudget(input)
				if err != nil {
					t.Fatal(err)
				}
				text := allocationSequence + fmt.Sprintf(`flow Advance(state: Record{first: List<Int>, second: List<Int>, count: Int}, item: Int) -> Record{first: List<Int>, second: List<Int>, count: Int}! { return Record.new(first: state.first, second: state.second, count: Math.add(state.count, 1)) }; flow Main() -> Int! { first = List.unfold(0, %d, Next); second = List.unfold(0, %d, Next); initial = Record.new(first: first, second: second, count: 0); result = List.fold(first, initial, Advance); return result.count }; return Main()`, size, size)
				requests, bytes := executionAllocationsWithBudget(t, text, fmt.Sprint(size), budget)
				t.Logf("items=%d allocation_count=%d allocation_bytes=%d", size, requests, bytes)
				if requests > uint64(size)*120+2048 || bytes > uint64(size)*6000+65536 {
					t.Fatalf("fresh record borrowed graph cost exceeds linear budget: requests=%d bytes=%d", requests, bytes)
				}
			})
		}
	}
}

func TestInterpreterFiveBorrowedGraphAllocations(t *testing.T) {
	fields := []string{"first", "second", "third", "fourth", "fifth"}
	var schema, carried, initial []string
	for _, field := range fields {
		schema = append(schema, field+": List<Int>")
		carried = append(carried, field+": state."+field)
		initial = append(initial, field+": "+field)
	}
	schema = append(schema, "count: Int")
	carried = append(carried, "count: Math.add(state.count, 1)")
	initial = append(initial, "count: 0")
	for _, size := range []int{128, 256, 512} {
		for _, limited := range []bool{false, true} {
			t.Run(fmt.Sprintf("size_%d/list_limit_%t", size, limited), func(t *testing.T) {
				input := execution.DefaultBudgetInput()
				if limited {
					input.Limits.ListItems = uint64(size)
				}
				budget, err := execution.NewBudget(input)
				if err != nil {
					t.Fatal(err)
				}
				var setup strings.Builder
				for _, field := range fields {
					fmt.Fprintf(&setup, "%s = List.unfold(0, %d, Next); ", field, size)
				}
				text := allocationSequence + fmt.Sprintf(`flow Advance(state: Record{%s}, item: Int) -> Record{%s}! { return Record.new(%s) }; flow Main() -> Int! { %s initial = Record.new(%s); result = List.fold(first, initial, Advance); return result.count }; return Main()`, strings.Join(schema, ", "), strings.Join(schema, ", "), strings.Join(carried, ", "), setup.String(), strings.Join(initial, ", "))
				requests, bytes := executionAllocationsWithBudget(t, text, fmt.Sprint(size), budget)
				t.Logf("items=%d allocation_count=%d allocation_bytes=%d", size, requests, bytes)
				if requests > uint64(size)*250+4096 || bytes > uint64(size)*15000+131072 {
					t.Fatalf("five independent borrowed graphs exceed linear allocation budget: requests=%d bytes=%d", requests, bytes)
				}
			})
		}
	}
}
