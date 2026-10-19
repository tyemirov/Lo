package value

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// Receipt is an immutable nominal activation result.
type Receipt interface {
	Value
	FieldsRecord() Record
	receipt()
}
type receipt struct {
	base
	fields Record
}

func (receipt) receipt()               {}
func (v receipt) FieldsRecord() Record { return v.fields }
func (v receipt) MarshalJSON() ([]byte, error) {
	fields := map[string]Value{}
	for _, field := range v.fields.Fields() {
		fields[field.Name] = field.Value
	}
	return encode(v, fields)
}

// NewReceipt validates the exact public shape of an adapter result.
func NewReceipt(t types.Type, fields Record) (Receipt, error) {
	if t == nil || fields == nil {
		return nil, fmt.Errorf("incomplete receipt")
	}
	var count int
	switch t.Kind() {
	case types.FileWriteReceiptKind, types.FileAppendReceiptKind, types.ServerReceiptKind:
		count = 2
	case types.FileRemoveReceiptKind:
		count = 3
	default:
		return nil, fmt.Errorf("invalid receipt type %s", t)
	}
	if len(fields.Fields()) != count {
		return nil, fmt.Errorf("invalid receipt field count")
	}
	for _, field := range fields.Fields() {
		expected, ok := types.FieldType(t, field.Name)
		if !ok || !types.Equal(expected, field.Value.Type()) {
			return nil, fmt.Errorf("invalid receipt field %s", field.Name)
		}
	}
	return receipt{newBase(t, fields.Weight()), fields}, nil
}
