package integration_test

import (
	"fmt"
	"testing"
)

func TestInterpreterScalarStateTemporaryTensorAllocations(t *testing.T) {
	for _, state := range []struct{ name, typ, initial, length string }{
		{"list", "List<Int>", "items", "result"},
		{"record", "Record{items: List<Int>}", "Record.new(items: items)", "result.items"},
	} {
		for _, size := range []int{128, 256, 512} {
			t.Run(fmt.Sprintf("%s/%d", state.name, size), func(t *testing.T) {
				text := allocationSequence + fmt.Sprintf(`flow Same(state: %s, item: Int) -> %s! { temporary = Tensor.new(List.new(1.0), List.new()); return state }; flow Main() -> Int! { items = List.unfold(0, %d, Next); result = List.fold(items, %s, Same); return List.length(%s) }; return Main()`, state.typ, state.typ, size, state.initial, state.length)
				requests, bytes := executionAllocations(t, text, fmt.Sprint(size))
				t.Logf("items=%d allocations=%d bytes=%d", size, requests, bytes)
				if requests > uint64(size)*160+4096 || bytes > uint64(size)*8000+131072 {
					t.Errorf("scalar state cleanup exceeds linear allocation budget: allocations=%d bytes=%d", requests, bytes)
				}
			})
		}
	}
}
