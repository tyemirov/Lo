package runtimeops

import (
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"math/big"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"

	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (m *machine) tensorOperation(call Call, args []value.Value) (value.Value, error) {
	op := call.Operation().Name()
	fail := func(err error) (value.Value, error) {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		var f tensor.Fault
		if errors.As(err, &f) {
			return OperationFailure(call, args, f.Kind(), op+": "+f.Error()), nil
		}
		var backend device.Fault
		if errors.As(err, &backend) {
			return OperationFailure(call, args, backend.Kind(), op+": "+backend.Error()), nil
		}
		return nil, execution.Limit(op + ": " + err.Error())
	}
	if op == catalog.DeviceNew {
		d, err := value.NewDevice(args[0].(value.Text).Text())
		if err != nil {
			return OperationFailure(call, args, catalog.InvalidSelector, err.Error()), nil
		}
		return Success(call.Type(), d), nil
	}
	if op == catalog.TensorNew {
		s, err := tensor.NewShape(tensorIntegers(args[1]))
		if err != nil {
			return fail(err)
		}
		if err = s.Limits(); err != nil {
			return fail(err)
		}
		items := args[0].(value.List).Items()
		if err = tensor.ValidateCount(s, len(items)); err != nil {
			return fail(err)
		}
		if err = m.tensorBudget(s.Count(), s.Weight()+16); err != nil {
			return nil, err
		}
		d, err := tensor.Construct(m.Context(), m.Resources(), s, func(i int) float64 { return items[i].(value.Float).Number() })
		if err != nil {
			return fail(err)
		}
		return Success(call.Type(), must(value.NewTensor(d))), nil
	}
	a := args[0].(value.Tensor).Data()
	s := a.Shape()
	if op == catalog.TensorTo {
		location := args[1].(value.Device).Selector()
		for _, selector := range []string{a.Location(), location} {
			if !m.Resources().Granted(selector) {
				return nil, execution.Denied(op, selector)
			}
		}
		if a.Location() != device.CPU && location != device.CPU && a.Location() != location {
			return OperationFailure(call, args, device.Mismatch, "transfers require CPU and one compute location"), nil
		}
		steps := uint64(0)
		if a.Location() != location {
			steps = s.Count()
		}
		if err := m.tensorBudget(steps, tensor.Weight(s, location)+16); err != nil {
			return nil, err
		}
		result, err := tensor.Transfer(m.Context(), m.Resources(), a, location)
		if err != nil {
			return fail(err)
		}
		return Success(call.Type(), must(value.NewTensor(result))), nil
	}
	switch op {
	case catalog.TensorShape:
		items := make([]value.Value, s.Rank())
		for i, d := range s.Dims() {
			items[i] = value.Int64(d)
		}
		return m.list(call.Type().(types.List), items)
	case catalog.TensorRank:
		return value.Int64(int64(s.Rank())), nil
	case catalog.TensorDtype:
		return must(value.NewText(types.String(), tensor.Dtype)), nil
	case catalog.TensorDevice:
		return must(value.NewDevice(a.Location())), nil
	case catalog.TensorToFloat:
		if a.Location() != device.CPU {
			return OperationFailure(call, args, catalog.HostObservationRequired, "transfer tensor to CPU before numeric observation"), nil
		}
		if err := tensor.ValidateScalar(s); err != nil {
			return fail(err)
		}
		if err := m.tensorBudget(1, 40); err != nil {
			return nil, err
		}
		return Success(call.Type(), must(value.NewFloat(float64(a.(tensor.HostData).At(0))))), nil
	case catalog.TensorValues:
		if a.Location() != device.CPU {
			return OperationFailure(call, args, catalog.HostObservationRequired, "transfer tensor to CPU before numeric observation"), nil
		}
		if err := m.tensorBudget(s.Count(), 32+24*s.Count()); err != nil {
			return nil, err
		}
		items := make([]value.Value, int(s.Count()))
		for i := range items {
			if i%1024 == 0 {
				if err := m.Context().Err(); err != nil {
					return nil, execution.BoundaryHostError(err)
				}
			}
			items[i] = must(value.NewFloat(float64(a.(tensor.HostData).At(i))))
		}
		return Success(call.Type(), must(value.NewList(call.Type().(types.Fallible).Base().(types.List), items))), nil
	}
	var b tensor.Data
	var bs tensor.Shape
	var indices []*big.Int
	if len(args) == 2 {
		if t, ok := args[1].(value.Tensor); ok {
			b = t.Data()
			bs = b.Shape()
		} else {
			indices = tensorIntegers(args[1])
		}
	}
	if b != nil && a.Location() != b.Location() {
		return OperationFailure(call, args, device.Mismatch, "tensor input locations differ"), nil
	}
	if a.Location() != device.CPU {
		return OperationFailure(call, args, device.UnsupportedOperation, "device arithmetic requires the NVIDIA backend"), nil
	}
	p, err := tensor.Prepare(op, s, bs, indices)
	if err != nil {
		return fail(err)
	}
	if err = m.tensorBudget(p.Steps(), p.Shape().Weight()+16); err != nil {
		return nil, err
	}
	d, err := tensor.Execute(m.Context(), m.Resources(), p, a, b)
	if err != nil {
		return fail(err)
	}
	return Success(call.Type(), must(value.NewTensor(d))), nil
}
func tensorIntegers(v value.Value) []*big.Int {
	items := v.(value.List).Items()
	n := make([]*big.Int, len(items))
	for i, item := range items {
		n[i] = item.(value.Integer).Big()
	}
	return n
}
func (m *machine) tensorBudget(steps, weight uint64) error { return m.TensorBudget(steps, weight) }
