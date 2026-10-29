package value

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// Allocations walks immutable values and counts each backing allocation once.
func Allocations(values ...Value) []device.Allocation {
	seen := map[device.Allocation]bool{}
	var result []device.Allocation
	var walk func(Value)
	walk = func(v Value) {
		if v == nil || !resourceType(v.Type()) {
			return
		}
		switch x := v.(type) {
		case Tensor:
			a := x.Data().Storage().Allocation()
			if !seen[a] {
				seen[a] = true
				result = append(result, a)
			}
		case OptimizerState:
			walk(x.Contents())
		case OptimizerConfig:
			walk(x.Contents())
		case TensorBundle:
			walk(x.Contents())
		case RemovalEffect:
			if guard, ok := x.Guard(); ok {
				for _, context := range guard.Context() {
					walk(context)
				}
			}
		case Record:
			for _, f := range x.Fields() {
				walk(f.Value)
			}
		case List:
			for _, item := range x.Items() {
				walk(item)
			}
		case Map:
			for _, entry := range x.Entries() {
				walk(entry.Value)
			}
		case Fallible:
			output, _ := x.Outcome()
			walk(output)
		}
	}
	for _, v := range values {
		walk(v)
	}
	return result
}

// HostPublishable rejects implicit downloads in nested result values.
func HostPublishable(v Value) bool {
	for _, a := range Allocations(v) {
		if a.Location() != device.CPU {
			return false
		}
	}
	return true
}

func resourceType(t types.Type) bool {
	switch x := t.(type) {
	case types.Fallible:
		return resourceType(x.Base())
	case types.List:
		return resourceType(x.Element())
	case types.Map:
		return resourceType(x.Value())
	case types.Record:
		for _, f := range x.Fields() {
			if resourceType(f.Type) {
				return true
			}
		}
		return false
	}
	switch t.Kind() {
	case types.TensorKind, types.ParametersKind, types.GradientsKind, types.SGDStateKind, types.AdamWStateKind, types.AnyRecordKind, types.FileRemoveKind:
		return true
	}
	return false
}
