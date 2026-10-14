package runtimeops

import (
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"strconv"
)

const MaxTapeBytes uint64 = 32 << 20

type GradientFrame struct {
	Invocation int
	Name       string
}
type Tape interface {
	Retain(value.Value) error
	Snapshot(int, []value.Field) error
	Fail(error, types.Fallible, string, source.Span, []value.Value, []GradientFrame) (value.Value, error)
	tape()
}
type tape struct {
	session   execution.Session
	snapshots map[int]value.Record
	storage   map[tensor.Storage]bool
	bytes     uint64
}

func (*tape) tape() {}
func NewTape(session execution.Session) Tape {
	return &tape{session: session, snapshots: map[int]value.Record{}, storage: map[tensor.Storage]bool{}}
}
func (t *tape) Snapshot(id int, fields []value.Field) error {
	for i, f := range fields {
		fields[i].Value = value.Snapshot(f.Value)
	}
	snapshot, err := value.NewRecord(fields)
	if err != nil {
		return err
	}
	if err := t.session.ChargeValue(snapshot); err != nil {
		return err
	}
	t.snapshots[id] = snapshot
	return nil
}
func (t *tape) Retain(v value.Value) error {
	if wrapped, ok := v.(value.Fallible); ok {
		v, _ = wrapped.Outcome()
	}
	if v, ok := v.(value.Tensor); ok {
		storage := v.Data().Storage()
		if !t.storage[storage] {
			if storage.Bytes() > MaxTapeBytes-t.bytes {
				return execution.Limit("derivative tape bytes")
			}
			t.storage[storage] = true
			t.bytes += storage.Bytes()
		}
	}
	return nil
}
func (t *tape) Fail(err error, out types.Fallible, op string, span source.Span, operands []value.Value, frames []GradientFrame) (value.Value, error) {
	if host := execution.BoundaryHostError(err); host != nil {
		return nil, host
	}
	var cause value.Error
	var runtime execution.Failure
	var numeric tensor.Fault
	switch {
	case errors.As(err, &runtime):
		cause = runtime.Value()
	case errors.As(err, &numeric):
		fields := make([]value.Field, 0, len(operands))
		for i, v := range operands {
			if v != nil {
				fields = append(fields, value.Field{Name: "argument" + strconv.Itoa(i), Value: v})
			}
		}
		cause = value.At(must(value.NewError(numeric.Kind(), numeric.Error(), must(value.NewRecord(fields)))), op, span)
	default:
		return nil, execution.Limit("derivative operation: " + err.Error())
	}
	for i := len(frames) - 1; i >= 0; i-- {
		frame := frames[i]
		cause = value.WithFrame(cause, frame.Name, cause.Operation(), t.snapshots[frame.Invocation])
	}
	return value.Failure(out, cause)
}
