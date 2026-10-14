package value

import (
	"fmt"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type TensorBundle interface {
	Value
	Contents() Record
	tensorBundle()
}
type tensorBundle struct {
	base
	contents Record
}

func (tensorBundle) tensorBundle()      {}
func (v tensorBundle) Contents() Record { return v.contents }
func (v tensorBundle) MarshalJSON() ([]byte, error) {
	fields := map[string]Value{}
	for _, f := range v.contents.Fields() {
		fields[f.Name] = f.Value
	}
	return encode(v, fields)
}
func NewTensorBundle(t types.TensorBundle, contents Record) (TensorBundle, error) {
	if t == nil || contents == nil || !types.Equal(t.Schema(), contents.Type()) {
		return nil, fmt.Errorf("tensor record contents do not match the declared schema")
	}
	return tensorBundle{newBase(t, contents.Weight()), contents}, nil
}
func MatchTensorSchema(expected, actual Record) error {
	if !types.Equal(expected.Type(), actual.Type()) {
		return fmt.Errorf("gradient fields require %s, got %s", expected.Type(), actual.Type())
	}
	for _, f := range expected.Fields() {
		a := f.Value.(Tensor).Data().Shape()
		b := actual.Field(f.Name).(Tensor).Data().Shape()
		if !slices.Equal(a.Dims(), b.Dims()) {
			return fmt.Errorf("gradient field %s requires shape %v, got %v", f.Name, a.Dims(), b.Dims())
		}
	}
	return nil
}
