package value

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// Allocations walks immutable values and counts each backing allocation once.
func Allocations(values ...Value) []device.Allocation {
	seen := map[device.Allocation]bool{}
	var result []device.Allocation
	VisitWhile(values, func(current Value) bool {
		if !mayContainAllocations(current.Type()) {
			return false
		}
		if tensor, ok := current.(Tensor); ok {
			allocation := tensor.Data().Storage().Allocation()
			if !seen[allocation] {
				seen[allocation] = true
				result = append(result, allocation)
			}
		}
		return true
	})
	return result
}

func mayContainAllocations(t types.Type) bool {
	switch t.Kind() {
	case types.IntKind, types.FloatKind, types.StringKind, types.BoolKind,
		types.NullKind, types.ScalarKind, types.BytesKind, types.PathKind,
		types.URLKind, types.MethodKind, types.DeviceKind:
		return false
	case types.ListKind:
		return mayContainAllocations(t.(types.List).Element())
	case types.MapKind:
		mapping := t.(types.Map)
		return mayContainAllocations(mapping.Key()) || mayContainAllocations(mapping.Value())
	case types.RangeKind:
		return mayContainAllocations(t.(types.Range).Element())
	case types.RecordKind:
		for _, field := range t.(types.Record).Fields() {
			if mayContainAllocations(field.Type) {
				return true
			}
		}
		return false
	default:
		// Opaque types and failures can retain resources outside their public fields.
		return true
	}
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
