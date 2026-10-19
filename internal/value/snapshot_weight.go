package value

import (
	"math/bits"

	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// SnapshotWeight calculates diagnostic value weight before snapshot allocation.
func SnapshotWeight(v Value) uint64 {
	switch x := v.(type) {
	case ProcessResult:
		return addWeight(48, x.Termination().Weight())
	case ProcessCommand:
		return addWeight(64, addWeight(x.Executable().Weight(), x.Directory().Weight()))
	case URL:
		return uint64(16 + len(x.Origin()) + 1)
	case ClientRequest:
		weight := addWeight(16, x.Method().Weight())
		weight = addWeight(weight, uint64(16+len(x.URL().Origin())+1))
		weight = addWeight(weight, SnapshotWeight(x.Headers()))
		return addWeight(weight, 16)
	case Headers:
		weight := uint64(32)
		for _, entry := range x.Entries().Items() {
			weight = addWeight(weight, 41+entry.(Record).Field("name").Weight())
		}
		return weight
	case HTTPValue:
		return addWeight(16, SnapshotWeight(x.Fields()))
	case Receipt:
		return addWeight(16, SnapshotWeight(x.FieldsRecord()))
	case FileEntry:
		return addWeight(16, x.FieldsRecord().Weight())
	case RemovalEffect:
		return addWeight(16, x.Weight())
	case OptimizerState:
		return SnapshotWeight(x.Contents())
	case OptimizerConfig:
		return SnapshotWeight(x.Contents())
	case TensorBundle:
		return SnapshotWeight(x.Contents())
	case Tensor:
		shapeWeight := uint64(16)
		for _, dimension := range x.Data().Shape().Dims() {
			shapeWeight = addWeight(shapeWeight, 16+uint64((bits.Len64(uint64(dimension))+7)/8))
		}
		weight := uint64(16 + len("tensor_type") + 16 + len(types.Tensor().String()))
		weight = addWeight(weight, uint64(len("dtype")+16+len(tensor.Dtype)))
		weight = addWeight(weight, uint64(len("shape"))+shapeWeight)
		return addWeight(weight, uint64(len("device")+16+len(x.Data().Location())))
	case Record:
		weight := uint64(16)
		for _, field := range x.(record).fields {
			weight = addWeight(weight, uint64(len(field.Name)))
			weight = addWeight(weight, SnapshotWeight(field.Value))
		}
		return weight
	case List:
		weight := uint64(16)
		for _, item := range x.(list).items {
			weight = addWeight(weight, SnapshotWeight(item))
		}
		return weight
	case Map:
		weight := uint64(16)
		for _, entry := range x.(mapValue).entries {
			weight = addWeight(weight, entry.Key.Weight())
			weight = addWeight(weight, SnapshotWeight(entry.Value))
		}
		return weight
	case Fallible:
		result, cause := x.Outcome()
		if cause != nil {
			return addWeight(16, cause.Weight())
		}
		return addWeight(16, SnapshotWeight(result))
	default:
		return v.Weight()
	}
}
