package runtimeops

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"math/big"
)

func ParseInt(text string) (value.Value, error) {
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("invalid compiled integer")
	}
	return Integer(n)
}
func Commit(s execution.Session, target value.Target, span source.Span) error {
	if err := s.Boundary().Commit(s.Context(), target); err != nil {
		if host := execution.BoundaryHostError(err); host != nil {
			return host
		}
		return execution.Failed(ActivationError(target, err, span))
	}
	return nil
}
func Propagate(v value.Value) (value.Value, error) {
	result, cause := v.(value.Fallible).Outcome()
	if cause != nil {
		return nil, execution.Failed(cause)
	}
	return result, nil
}
func ScalarList(t types.List, v value.Text) (value.Value, error) {
	items := []value.Value{}
	for _, r := range v.Text() {
		if len(items) == execution.MaxItems {
			return nil, execution.Limit("list elements")
		}
		items = append(items, must(value.NewText(types.Scalar(), string(r))))
	}
	return value.NewList(t, items)
}
func FallibleRecord(t types.Fallible, v value.Fallible) (value.Value, error) {
	r, cause := v.Outcome()
	if cause != nil {
		return value.Failure(t, cause)
	}
	return value.Success(t, value.RecordView(r.(value.Record)))
}

// Cause preserves the flow boundary semantics for compiled calls.
func Cause(result value.Value, err error) (value.Error, error) {
	var cause value.Error
	if err != nil {
		if f, ok := err.(execution.Failure); ok {
			cause = f.Value()
		} else {
			return nil, err
		}
	}
	if f, ok := result.(value.Fallible); ok {
		_, cause = f.Outcome()
	}
	return cause, nil
}
func Match(selector, head value.Value) bool {
	if r, ok := head.(value.Range); ok {
		return r.Contains(selector)
	}
	return value.Equal(selector, head)
}
func CallbackFailure(call Call, cause value.Error, name string, index int, element, state value.Value) value.Value {
	fields := []value.Field{{Name: "index", Value: value.Int64(int64(index))}}
	if element != nil {
		fields = append(fields, value.Field{Name: "element", Value: element})
	}
	if state != nil {
		fields = append(fields, value.Field{Name: "state", Value: state})
	}
	cause = value.WithFrame(cause, name, call.Operation().Name(), must(value.NewRecord(fields)))
	return must(value.Failure(call.Type().(types.Fallible), value.At(cause, call.Operation().Name(), call.Span())))
}
