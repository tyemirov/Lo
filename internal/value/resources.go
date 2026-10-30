package value

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

// Allocations walks immutable values and counts each backing allocation once.
func Allocations(values ...Value) []device.Allocation {
	seen := map[device.Allocation]bool{}
	var result []device.Allocation
	Visit(values, func(current Value) {
		if tensor, ok := current.(Tensor); ok {
			allocation := tensor.Data().Storage().Allocation()
			if !seen[allocation] {
				seen[allocation] = true
				result = append(result, allocation)
			}
		}
	})
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
