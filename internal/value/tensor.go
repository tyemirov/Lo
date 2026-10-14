package value

import (
	"fmt"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type Tensor interface {
	Value
	Data() tensor.Data
}
type tensorValue struct {
	base
	data tensor.Data
}

func (v tensorValue) Data() tensor.Data { return v.data }
func NewTensor(d tensor.Data) (Tensor, error) {
	if d == nil {
		return nil, fmt.Errorf("absent tensor data")
	}
	return tensorValue{base{types.Tensor(), d.Shape().Weight()}, d}, nil
}
func (v tensorValue) MarshalJSON() ([]byte, error) {
	shape := make([]string, v.data.Shape().Rank())
	for i, d := range v.data.Shape().Dims() {
		shape[i] = strconv.FormatInt(d, 10)
	}
	elements := make([]string, int(v.data.Shape().Count()))
	for i := range elements {
		elements[i] = strconv.FormatFloat(float64(v.data.At(i)), 'g', -1, 32)
	}
	return encode(v, struct {
		Dtype  string   `json:"dtype"`
		Shape  []string `json:"shape"`
		Device string   `json:"device"`
		Values []string `json:"values"`
	}{tensor.Dtype, shape, tensor.CPU, elements})
}

type Device interface {
	Value
	Selector() string
}
type device struct {
	base
	selector string
}

func (v device) Selector() string             { return v.selector }
func (v device) MarshalJSON() ([]byte, error) { return encode(v, v.selector) }
func NewDevice(selector string) (Device, error) {
	switch selector {
	case tensor.CPU, tensor.CUDA, tensor.Simulation:
		return device{newBase(types.Device(), uint64(len(selector))), selector}, nil
	default:
		return nil, fmt.Errorf("invalid device selector %q", selector)
	}
}

// Snapshot removes tensor storage from diagnostic inputs, including nested data.
func Snapshot(v Value) Value {
	switch x := v.(type) {
	case OptimizerState:
		return Snapshot(x.Contents())
	case OptimizerConfig:
		return Snapshot(x.Contents())
	case TensorBundle:
		return Snapshot(x.Contents())
	case Tensor:
		shape := make([]Value, x.Data().Shape().Rank())
		for i, d := range x.Data().Shape().Dims() {
			shape[i] = Int64(d)
		}
		return must(NewRecord([]Field{
			{"tensor_type", must(NewText(types.String(), types.Tensor().String()))},
			{"dtype", must(NewText(types.String(), tensor.Dtype))},
			{"shape", must(NewList(must(types.NewList(types.Int())), shape))},
			{"device", must(NewText(types.String(), tensor.CPU))},
		}))
	case Record:
		fields := x.Fields()
		for i := range fields {
			fields[i].Value = Snapshot(fields[i].Value)
		}
		r := must(NewRecord(fields))
		if v.Type().Kind() == types.AnyRecordKind {
			return RecordView(r)
		}
		return r
	case List:
		items := x.Items()
		for i := range items {
			items[i] = Snapshot(items[i])
		}
		return must(NewList(snapshotType(v.Type()).(types.List), items))
	case Map:
		entries := x.Entries()
		for i := range entries {
			entries[i].Value = Snapshot(entries[i].Value)
		}
		return must(NewMap(snapshotType(v.Type()).(types.Map), entries))
	case Fallible:
		result, cause := x.Outcome()
		if cause != nil {
			return must(Failure(snapshotType(v.Type()).(types.Fallible), cause))
		}
		result = Snapshot(result)
		return must(Success(must(types.NewFallible(result.Type())), result))
	default:
		return v
	}
}

func snapshotType(t types.Type) types.Type {
	switch x := t.(type) {
	case types.OptimizerState:
		return snapshotType(types.StateContents(x))
	case types.TensorBundle:
		return snapshotType(x.Schema())
	case types.List:
		return must(types.NewList(snapshotType(x.Element())))
	case types.Map:
		return must(types.NewMap(x.Key(), snapshotType(x.Value())))
	case types.Record:
		fields := x.Fields()
		for i := range fields {
			fields[i].Type = snapshotType(fields[i].Type)
		}
		return must(types.NewRecord(fields))
	case types.Fallible:
		return must(types.NewFallible(snapshotType(x.Base())))
	}
	if t.Kind() == types.TensorKind {
		return must(types.NewRecord([]types.Field{{Name: "tensor_type", Type: types.String()}, {Name: "dtype", Type: types.String()}, {Name: "shape", Type: must(types.NewList(types.Int()))}, {Name: "device", Type: types.String()}}))
	}
	if t.Kind() == types.SGDConfigKind || t.Kind() == types.AdamWConfigKind {
		return types.ConfigContents(t.Kind())
	}
	return t
}
