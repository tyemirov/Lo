package integration_test

import (
	"fmt"
	"testing"
)

func TestListLengthContextAllocationGrowth(t *testing.T) {
	type measurement struct {
		items    int
		requests uint64
		bytes    uint64
	}
	var measurements []measurement
	for _, size := range []int{128, 256, 512} {
		text := allocationSequence + fmt.Sprintf(`flow Length(context: List<Int>, item: Int) -> Int { return List.length(context) }; xs = List.unfold(0, %d, Next); return Math.sum(List.map(xs, xs, Length))`, size)
		requests, bytes := executionAllocations(t, text, fmt.Sprint(size*size))
		t.Logf("items=%d allocation_count=%d allocation_bytes=%d", size, requests, bytes)
		measurements = append(measurements, measurement{size, requests, bytes})
	}
	for index := 1; index < len(measurements); index++ {
		previous, current := measurements[index-1], measurements[index]
		if current.bytes > 3*previous.bytes {
			t.Errorf("List.length context allocation grows faster than linear: %d items use %d bytes; %d items use %d bytes", previous.items, previous.bytes, current.items, current.bytes)
		}
	}
}
