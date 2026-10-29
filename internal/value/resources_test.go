package value

import (
	"context"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func TestRemovalGuardAllocations(t *testing.T) {
	resources := must(device.NewSession(device.DefaultConfig()))
	t.Cleanup(func() {
		if err := resources.Close(); err != nil {
			t.Fatal(err)
		}
	})
	shape := must(tensor.NewShape(nil))
	data := must(tensor.Construct(context.Background(), resources, shape, func(int) float64 { return 1 }))
	captured := must(NewTensor(data))
	aliases := must(NewList(must(types.NewList(types.Tensor())), []Value{captured, captured}))
	nested := must(NewRecord([]Field{{Name: "aliases", Value: aliases}}))
	entries := must(NewList(must(types.NewList(types.FileEntry())), nil))
	session := NewFileSession()
	guarded := must(NewCheckedRemovalEffect(session, entries, "Guard", []Value{nested, captured}))
	wrapped := must(Success(must(types.NewFallible(types.FileRemove())), guarded))
	contained := must(NewList(must(types.NewList(wrapped.Type())), []Value{wrapped, wrapped}))

	for _, input := range []Value{guarded, wrapped, contained} {
		allocations := Allocations(input)
		if len(allocations) != 1 || allocations[0] != data.Storage().Allocation() {
			t.Fatalf("%s guard allocations = %v, want captured allocation once", input.Type(), allocations)
		}
	}
	for _, input := range []Value{must(NewRemovalEffect(session, entries)), Snapshot(guarded)} {
		if allocations := Allocations(input); len(allocations) != 0 {
			t.Fatalf("%s without captured storage has allocations %v", input.Type(), allocations)
		}
	}
}
