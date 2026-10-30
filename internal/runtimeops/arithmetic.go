package runtimeops

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/derivative"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"math/big"
)

func Arithmetic(session execution.Session) derivative.Arithmetic {
	return tensorArithmetic{&machine{session}}
}

type tensorArithmetic struct{ machine *machine }

func (d tensorArithmetic) Apply(op string, a, b tensor.Data, indices []*big.Int) (tensor.Data, error) {
	if b != nil && a.Location() != b.Location() {
		return nil, must(device.NewFault(device.Mismatch, fmt.Errorf("numeric operation %s requires matching input locations", op)))
	}
	var bs tensor.Shape
	if b != nil {
		bs = b.Shape()
	}
	p, err := tensor.Prepare(op, a.Shape(), bs, indices)
	if err != nil {
		return nil, err
	}
	if err = d.machine.tensorBudget(p.Steps(), tensor.Weight(p.Shape(), a.Location())); err != nil {
		return nil, err
	}
	return tensor.Execute(d.machine.Context(), d.machine.Resources(), p, a, b)
}
func (d tensorArithmetic) Constant(shape tensor.Shape, location string, v float32) (tensor.Data, error) {
	if err := d.machine.tensorBudget(shape.Count(), tensor.Weight(shape, location)); err != nil {
		return nil, err
	}
	return tensor.Constant(d.machine.Context(), d.machine.Resources(), shape, location, v)
}
func (d tensorArithmetic) Relu(g, a tensor.Data) (tensor.Data, error) {
	if a.Location() != g.Location() {
		return nil, must(device.NewFault(device.Mismatch, fmt.Errorf("derivative inputs require matching locations")))
	}
	if err := d.machine.tensorBudget(a.Shape().Count(), tensor.Weight(a.Shape(), a.Location())); err != nil {
		return nil, err
	}
	return tensor.ReluDerivative(d.machine.Context(), d.machine.Resources(), g, a)
}
func (d tensorArithmetic) CheckSqrt(a tensor.Data) error {
	return tensor.CheckSqrtDerivative(d.machine.Context(), d.machine.Resources(), a)
}
