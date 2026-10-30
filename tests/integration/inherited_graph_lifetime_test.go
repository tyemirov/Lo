package integration_test

import (
	"context"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestInheritedGraphRetainsLateChargedDescendants(t *testing.T) {
	for _, operation := range []string{"close", "keep_overlap", "retain_overlapping_pins"} {
		t.Run(operation, func(t *testing.T) {
			items := make([]value.Value, 32)
			for index := range items {
				items[index] = value.Int64(int64(index))
			}
			listType, err := types.NewList(types.Int())
			if err != nil {
				t.Fatal(err)
			}
			root, err := value.NewList(listType, items)
			if err != nil {
				t.Fatal(err)
			}
			child := items[0]
			input := execution.DefaultBudgetInput()
			input.Limits.LiveBytes = root.Weight() + child.Weight()
			budget, err := execution.NewBudget(input)
			if err != nil {
				t.Fatal(err)
			}
			session, err := execution.NewSession(context.Background(), &observedBoundary{}, budget, device.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.ChargeValue(root); err != nil {
				t.Fatal(err)
			}
			// Warm graph reuse before the descendant receives its own receipt.
			warm := session.Scope()
			if err := session.ChargeValue(value.Int64(0)); err != nil {
				t.Fatal(err)
			}
			warm.Close(root)
			scope := session.Scope()
			if operation == "retain_overlapping_pins" {
				first, err := scope.Pin(child)
				if err != nil {
					t.Fatal(err)
				}
				second, err := scope.Pin(child)
				if err != nil {
					t.Fatal(err)
				}
				scope.Retain(root)
				scope.Close(nil)
				first()
				second()
			} else {
				if err := session.ChargeValue(child); err != nil {
					t.Fatal(err)
				}
				if operation == "keep_overlap" {
					scope.Keep(root, child)
				}
				scope.Close(root)
			}
			if err := session.ChargeValue(value.Int64(0)); err == nil {
				t.Fatal("reachable descendant receipt was released; an extra value incorrectly fits the live budget")
			}
		})
	}
}

func TestInheritedGraphReuseChecksFreshListWidths(t *testing.T) {
	input := execution.DefaultBudgetInput()
	input.Limits.ListItems = 16
	budget, err := execution.NewBudget(input)
	if err != nil {
		t.Fatal(err)
	}
	session, err := execution.NewSession(context.Background(), &observedBoundary{}, budget, device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listType, err := types.NewList(types.Int())
	if err != nil {
		t.Fatal(err)
	}
	items := make([]value.Value, 17)
	for index := range items {
		items[index] = value.Int64(int64(index))
	}
	root, err := value.NewList(listType, items[:16])
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ChargeValue(root); err != nil {
		t.Fatal(err)
	}
	warm := session.Scope()
	if err := session.ChargeValue(value.Int64(0)); err != nil {
		t.Fatal(err)
	}
	warm.Close(root)
	oversized, err := value.NewList(listType, items)
	if err != nil {
		t.Fatal(err)
	}
	combined, err := value.NewRecord([]value.Field{{Name: "cached", Value: root}, {Name: "fresh", Value: oversized}})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ChargeValue(combined); err == nil {
		t.Fatal("cached sibling bypassed the fresh nested list quota")
	}
}

func TestInheritedGraphRepeatedScopesAndProgramsRespectLiveBudget(t *testing.T) {
	listType, err := types.NewList(types.Int())
	if err != nil {
		t.Fatal(err)
	}
	input := execution.DefaultBudgetInput()
	input.Limits.LiveBytes = 1024
	input.Limits.ListItems = 32
	budget, err := execution.NewBudget(input)
	if err != nil {
		t.Fatal(err)
	}
	session, err := execution.NewSession(context.Background(), &observedBoundary{}, budget, device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 100; iteration++ {
		scope := session.Scope()
		items := make([]value.Value, 32)
		for index := range items {
			items[index] = value.Int64(int64(index))
		}
		root, err := value.NewList(listType, items)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ChargeValue(root); err != nil {
			t.Fatalf("scope %d: %v", iteration, err)
		}
		child := session.Scope()
		if err := session.ChargeValue(value.Int64(0)); err != nil {
			t.Fatal(err)
		}
		child.Close(root)
		scope.Close(nil)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	input.Limits.LiveBytes = 4096
	budget, err = execution.NewBudget(input)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := source.New("inherited-graph-repeat.lo", allocationSequence+`flow Same(state: List<Int>, item: Int) -> List<Int>! { return state }; flow Main() -> Int! { xs = List.unfold(0, 32, Next); return List.length(List.fold(xs, xs, Same)) }; return Main()`)
	if err != nil {
		t.Fatal(err)
	}
	checked, problem := compiler.Check(unit)
	if problem != nil {
		t.Fatal(problem)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 100; iteration++ {
		result, err := program.Run(context.Background(), &observedBoundary{}, budget, device.DefaultConfig())
		if err != nil || value.TextResult(result) != "32" {
			t.Fatalf("program run %d: result=%v error=%v", iteration, result, err)
		}
	}
}
