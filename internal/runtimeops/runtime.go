package runtimeops

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"math/big"
)

// Call contains runtime metadata, without compiler graph nodes.
type Call interface {
	Type() types.Type
	Span() source.Span
	Operation() catalog.Operation
	Labels() []string
}
type call struct {
	out    types.Type
	span   source.Span
	op     catalog.Operation
	labels []string
}

func (c call) Type() types.Type             { return c.out }
func (c call) Span() source.Span            { return c.span }
func (c call) Operation() catalog.Operation { return c.op }
func (c call) Labels() []string             { return c.labels }
func NewCall(out types.Type, span source.Span, op catalog.Operation, labels []string) (Call, error) {
	if out == nil || span == nil || op == nil {
		return nil, fmt.Errorf("operation requires type, span, and identity")
	}
	return call{out, span, op, append([]string(nil), labels...)}, nil
}

type machine struct{ execution.Session }

func Integer(n *big.Int) (value.Value, error) {
	if n.BitLen() > execution.MaxIntegerBits {
		return nil, execution.Limit("integer bits")
	}
	return value.NewInt(n)
}
func List(session execution.Session, t types.List, items []value.Value) (value.Value, error) {
	if limit := session.Limits().ListItems; limit != 0 && uint64(len(items)) > limit {
		return nil, execution.Limit("list elements")
	}
	return value.NewList(t, items)
}
func (m *machine) integer(n *big.Int) (value.Value, error) { return Integer(n) }
func (m *machine) list(t types.List, items []value.Value) (value.Value, error) {
	return List(m.Session, t, items)
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
