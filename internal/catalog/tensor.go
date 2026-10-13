package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	DeviceNew       = "Device.new"
	TensorNew       = "Tensor.new"
	TensorShape     = "Tensor.shape"
	TensorRank      = "Tensor.rank"
	TensorDtype     = "Tensor.dtype"
	TensorDevice    = "Tensor.device"
	TensorToFloat   = "Tensor.to_float"
	TensorValues    = "Tensor.values"
	TensorAdd       = "Tensor.add"
	TensorSub       = "Tensor.sub"
	TensorMul       = "Tensor.mul"
	TensorDiv       = "Tensor.div"
	TensorSqrt      = "Tensor.sqrt"
	TensorRelu      = "Tensor.relu"
	TensorMatmul    = "Tensor.matmul"
	TensorBroadcast = "Tensor.broadcast"
	TensorReshape   = "Tensor.reshape"
	TensorTranspose = "Tensor.transpose"
	TensorSum       = "Tensor.sum"
	TensorMean      = "Tensor.mean"
	InvalidSelector = "Device.InvalidSelector"
	InvalidShape    = "Tensor.InvalidShape"
	ShapeMismatch   = "Tensor.ShapeMismatch"
	EmptyReduction  = "Tensor.EmptyReduction"
	ParamShape      = "shape"
	ParamAxes       = "axes"
	ParamValues     = "values"
	ParamSelector   = "selector"
)

func addTensors(m map[string]Operation) {
	t := types.Tensor()
	ft := must(types.NewFallible(t))
	ints := must(types.NewList(types.Int()))
	floats := must(types.NewList(types.Float()))
	add := func(name string, params []types.Parameter, out types.Type, errors ...string) {
		m[name] = operation{name: name, rule: Fixed, parameters: params, output: out, errors: errors}
	}
	one := []types.Parameter{{Name: ParamValue, Type: t}}
	add(DeviceNew, []types.Parameter{{Name: ParamSelector, Type: types.String()}}, must(types.NewFallible(types.Device())), InvalidSelector)
	add(TensorNew, []types.Parameter{{Name: ParamValues, Type: floats}, {Name: ParamShape, Type: ints}}, ft, InvalidShape, NumericNonFinite)
	add(TensorShape, one, ints)
	add(TensorRank, one, types.Int())
	add(TensorDtype, one, types.String())
	add(TensorDevice, one, types.Device())
	add(TensorToFloat, one, must(types.NewFallible(types.Float())), ShapeMismatch)
	add(TensorValues, one, must(types.NewFallible(floats)))
	for _, name := range []string{TensorAdd, TensorSub, TensorMul, TensorDiv, TensorMatmul} {
		errors := []string{ShapeMismatch, NumericNonFinite}
		if name == TensorDiv {
			errors = append(errors, NumericZero)
		}
		add(name, []types.Parameter{{Name: ParamLeftOperand, Type: t}, {Name: ParamRightOperand, Type: t}}, ft, errors...)
	}
	add(TensorSqrt, one, ft, NumericDomain, NumericNonFinite)
	add(TensorRelu, one, ft)
	for _, name := range []string{TensorBroadcast, TensorReshape, TensorTranspose, TensorSum, TensorMean} {
		param := ParamShape
		if name == TensorTranspose || name == TensorSum || name == TensorMean {
			param = ParamAxes
		}
		errors := []string{InvalidShape, ShapeMismatch}
		if name == TensorSum || name == TensorMean {
			errors = append(errors, NumericNonFinite)
		}
		if name == TensorMean {
			errors = append(errors, EmptyReduction)
		}
		add(name, []types.Parameter{{Name: ParamValue, Type: t}, {Name: param, Type: ints}}, ft, errors...)
	}
}
