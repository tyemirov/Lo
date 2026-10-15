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

type optimizerCalculation struct {
	arithmetic tensorArithmetic
	err        error
}

func (c *optimizerCalculation) binary(op string, a, b tensor.Data) tensor.Data {
	if c.err != nil {
		return nil
	}
	v, err := c.arithmetic.Apply(op, a, b, nil)
	c.err = err
	return v
}
func (c *optimizerCalculation) scalar(v float32) tensor.Data {
	if c.err != nil {
		return nil
	}
	x, err := c.arithmetic.Constant(must(tensor.NewShape(nil)), v)
	c.err = err
	return x
}
func (c *optimizerCalculation) sqrt(v tensor.Data) tensor.Data {
	if c.err != nil {
		return nil
	}
	x, err := c.arithmetic.Apply(catalog.TensorSqrt, v, nil, nil)
	c.err = err
	return x
}

func (m *machine) optimizerOperation(call Call, args []value.Value) (output value.Value, resultErr error) {
	scope := m.Scope()
	defer func() { scope.Close(output) }()
	defer func() {
		if resultErr != nil || output == nil {
			return
		}
		v := output
		if wrapped, ok := v.(value.Fallible); ok {
			v, _ = wrapped.Outcome()
		}
		if state, ok := v.(value.AdamWState); ok {
			err := m.Resources().Classify(value.Allocations(state.FirstMoment(), state.SecondMoment(), state.Beta1Power(), state.Beta2Power()), device.OptimizerStorage)
			if err != nil {
				output = nil
				resultErr = execution.BoundaryHostError(err)
			}
		}
	}()
	name := call.Operation().Name()
	fail := func(err error) (value.Value, error) {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		var f tensor.Fault
		if errors.As(err, &f) {
			return OperationFailure(call, args, f.Kind(), f.Error()), nil
		}
		var backend device.Fault
		if errors.As(err, &backend) {
			return OperationFailure(call, args, backend.Kind(), backend.Error()), nil
		}
		return nil, execution.Limit(name + ": " + err.Error())
	}
	if name == catalog.SGDConfigNew || name == catalog.AdamWConfigNew {
		var config value.Value
		var err error
		if name == catalog.SGDConfigNew {
			config, err = value.NewSGDConfig(args[0].(value.Float).Number())
		} else {
			config, err = value.NewAdamWConfig(args[0].(value.Float).Number(), args[1].(value.Float).Number(), args[2].(value.Float).Number(), args[3].(value.Float).Number(), args[4].(value.Float).Number())
		}
		if err != nil {
			return OperationFailure(call, args, catalog.InvalidOptimizerConfig, err.Error()), nil
		}
		return Success(call.Type(), config), nil
	}
	if name == catalog.SGDStateNew {
		return Success(call.Type(), must(value.NewSGDState(args[0].(value.TensorBundle), value.Int64(0), args[1].(value.SGDConfig)))), nil
	}
	if !value.HostPublishable(args[0]) {
		return OperationFailure(call, args, device.UnsupportedOperation, "optimizer arithmetic requires the selected device backend"), nil
	}
	if name == catalog.AdamWStateNew {
		p := args[0].(value.TensorBundle)
		var bytes uint64 = 4
		for _, f := range p.Contents().Fields() {
			bytes += 8 * f.Value.(value.Tensor).Data().Shape().Count()
		}
		if err := m.Resources().CheckBudget(device.OptimizerStorage, bytes); err != nil {
			return nil, execution.BoundaryHostError(err)
		}
		maths := tensorArithmetic{m}
		first, second := p.Contents().Fields(), p.Contents().Fields()
		for i, f := range first {
			shape := f.Value.(value.Tensor).Data().Shape()
			a, err := maths.Constant(shape, 0)
			if err != nil {
				return fail(err)
			}
			b, err := maths.Constant(shape, 0)
			if err != nil {
				return fail(err)
			}
			first[i].Value = must(value.NewTensor(a))
			second[i].Value = must(value.NewTensor(b))
		}
		one, err := maths.Constant(must(tensor.NewShape(nil)), 1)
		if err != nil {
			return fail(err)
		}
		power := must(value.NewTensor(one))
		state := must(value.NewAdamWState(p, value.Int64(0), args[1].(value.AdamWConfig), must(value.NewRecord(first)), must(value.NewRecord(second)), power, power))
		return Success(call.Type(), state), nil
	}
	state := args[0].(value.OptimizerState)
	gradients := args[1].(value.TensorBundle).Contents()
	if err := value.MatchTensorSchema(state.Parameters().Contents(), gradients); err != nil {
		return OperationFailure(call, args, catalog.GradientSchemaMismatch, err.Error()), nil
	}
	c := &optimizerCalculation{arithmetic: tensorArithmetic{m}}
	rate := c.scalar(state.Config().Rate())
	fields := state.Parameters().Contents().Fields()
	if name == catalog.SGDStep {
		for i, f := range fields {
			g := gradients.Field(f.Name).(value.Tensor).Data()
			update := c.binary(catalog.TensorMul, rate, g)
			p := c.binary(catalog.TensorSub, f.Value.(value.Tensor).Data(), update)
			if c.err != nil {
				return fail(c.err)
			}
			fields[i].Value = must(value.NewTensor(p))
		}
		step, err := m.integer(new(big.Int).Add(state.Step().Big(), big.NewInt(1)))
		if err != nil {
			return nil, err
		}
		p := must(value.NewTensorBundle(state.Parameters().Type().(types.TensorBundle), must(value.NewRecord(fields))))
		return Success(call.Type(), must(value.NewSGDState(p, step.(value.Integer), state.Config().(value.SGDConfig)))), nil
	}
	var additional uint64 = 8
	for _, f := range fields {
		additional += 8 * f.Value.(value.Tensor).Data().Shape().Count()
	}
	if err := m.Resources().CheckBudget(device.OptimizerStorage, additional); err != nil {
		return nil, execution.BoundaryHostError(err)
	}
	adam := state.(value.AdamWState)
	config := adam.Config().(value.AdamWConfig)
	first, second := adam.FirstMoment().Fields(), adam.SecondMoment().Fields()
	beta1, beta2, epsilon, decay, one := c.scalar(config.Beta1()), c.scalar(config.Beta2()), c.scalar(config.Epsilon()), c.scalar(config.Decay()), c.scalar(1)
	var power1, power2 tensor.Data
	for i, f := range fields {
		p, g := f.Value.(value.Tensor).Data(), gradients.Field(f.Name).(value.Tensor).Data()
		m0, v0 := first[i].Value.(value.Tensor).Data(), second[i].Value.(value.Tensor).Data()
		mBeta := c.binary(catalog.TensorMul, beta1, m0)
		mFactor := c.binary(catalog.TensorSub, one, beta1)
		mGradient := c.binary(catalog.TensorMul, mFactor, g)
		mNext := c.binary(catalog.TensorAdd, mBeta, mGradient)
		vBeta := c.binary(catalog.TensorMul, beta2, v0)
		vFactor := c.binary(catalog.TensorSub, one, beta2)
		gSquared := c.binary(catalog.TensorMul, g, g)
		vGradient := c.binary(catalog.TensorMul, vFactor, gSquared)
		vNext := c.binary(catalog.TensorAdd, vBeta, vGradient)
		if power1 == nil {
			power1 = c.binary(catalog.TensorMul, adam.Beta1Power().Data(), beta1)
			power2 = c.binary(catalog.TensorMul, adam.Beta2Power().Data(), beta2)
		}
		mDenominator := c.binary(catalog.TensorSub, one, power1)
		mHat := c.binary(catalog.TensorDiv, mNext, mDenominator)
		vDenominator := c.binary(catalog.TensorSub, one, power2)
		vHat := c.binary(catalog.TensorDiv, vNext, vDenominator)
		decayFactor := c.binary(catalog.TensorMul, rate, decay)
		decayUpdate := c.binary(catalog.TensorMul, decayFactor, p)
		decayed := c.binary(catalog.TensorSub, p, decayUpdate)
		root := c.sqrt(vHat)
		denominator := c.binary(catalog.TensorAdd, root, epsilon)
		quotient := c.binary(catalog.TensorDiv, mHat, denominator)
		update := c.binary(catalog.TensorMul, rate, quotient)
		pNext := c.binary(catalog.TensorSub, decayed, update)
		if c.err != nil {
			return fail(c.err)
		}
		fields[i].Value = must(value.NewTensor(pNext))
		first[i].Value = must(value.NewTensor(mNext))
		second[i].Value = must(value.NewTensor(vNext))
	}
	step, err := m.integer(new(big.Int).Add(state.Step().Big(), big.NewInt(1)))
	if err != nil {
		return nil, err
	}
	p := must(value.NewTensorBundle(state.Parameters().Type().(types.TensorBundle), must(value.NewRecord(fields))))
	next := must(value.NewAdamWState(p, step.(value.Integer), config, must(value.NewRecord(first)), must(value.NewRecord(second)), must(value.NewTensor(power1)), must(value.NewTensor(power2))))
	return Success(call.Type(), next), nil
}
