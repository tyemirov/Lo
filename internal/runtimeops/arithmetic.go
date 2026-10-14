package runtimeops

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/derivative"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"math/big"
)

func Arithmetic(session execution.Session) derivative.Arithmetic {
	return tensorArithmetic{&machine{session}}
}

type tensorArithmetic struct{ machine *machine }

func (d tensorArithmetic) finish(data tensor.Data, err error) (tensor.Data, error) {
	if err != nil {
		return nil, err
	}
	d.machine.AddWeight(data.Shape().Weight())
	return data, nil
}
func (d tensorArithmetic) Apply(op string, a, b tensor.Data, indices []*big.Int) (tensor.Data, error) {
	var bs tensor.Shape
	if b != nil {
		bs = b.Shape()
	}
	p, err := tensor.Prepare(op, a.Shape(), bs, indices)
	if err != nil {
		return nil, err
	}
	if err = d.machine.tensorBudget(p.Steps(), p.Shape().Weight()); err != nil {
		return nil, err
	}
	return d.finish(tensor.Execute(d.machine.Context(), p, a, b))
}
func (d tensorArithmetic) Constant(shape tensor.Shape, v float32) (tensor.Data, error) {
	if err := d.machine.tensorBudget(shape.Count(), shape.Weight()); err != nil {
		return nil, err
	}
	return d.finish(tensor.Construct(d.machine.Context(), shape, func(int) float64 { return float64(v) }))
}
func (d tensorArithmetic) Relu(g, a tensor.Data) (tensor.Data, error) {
	if err := d.machine.tensorBudget(a.Shape().Count(), a.Shape().Weight()); err != nil {
		return nil, err
	}
	return d.finish(tensor.ReluDerivative(d.machine.Context(), g, a))
}
func (d tensorArithmetic) CheckSqrt(a tensor.Data) error {
	return tensor.CheckSqrtDerivative(d.machine.Context(), a)
}
