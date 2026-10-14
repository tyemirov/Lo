package eval

import (
	"errors"
	"math"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (m *machine) parameterOperation(call ir.BuiltinCall, args []value.Value) (value.Value, error) {
	name := call.Operation().Name()
	wrap := func(r value.Record) value.Value {
		t := call.Type().(types.Fallible).Base().(types.TensorBundle)
		return success(call.Type(), must(value.NewTensorBundle(t, r)))
	}
	if name == catalog.ParametersNew {
		return wrap(args[0].(value.Record)), nil
	}
	left := args[0].(value.TensorBundle).Contents()
	if name == catalog.ParametersValues || name == catalog.GradientsValues {
		return left, nil
	}
	var right value.Record
	if name == catalog.GradientsNew {
		right = args[1].(value.Record)
	}
	if name == catalog.GradientsAdd {
		right = args[1].(value.TensorBundle).Contents()
	}
	if right != nil {
		if err := value.MatchTensorSchema(left, right); err != nil {
			return operationFailure(call, args, catalog.GradientSchemaMismatch, err.Error()), nil
		}
	}
	if name == catalog.GradientsNew {
		return wrap(right), nil
	}
	maths := tensorArithmetic{m}
	fail := func(err error) (value.Value, error) {
		if host := boundaryHostError(err); host != nil {
			return nil, host
		}
		var f tensor.Fault
		if errors.As(err, &f) {
			return operationFailure(call, args, f.Kind(), f.Error()), nil
		}
		return nil, Limit(name + ": " + err.Error())
	}
	var factor tensor.Data
	if name == catalog.GradientsScale {
		f := float32(args[1].(value.Float).Number())
		if math.IsInf(float64(f), 0) {
			return operationFailure(call, args, catalog.NumericNonFinite, "gradient scale factor exceeds finite FP32"), nil
		}
		var err error
		factor, err = maths.Constant(must(tensor.NewShape(nil)), f)
		if err != nil {
			return fail(err)
		}
	}
	fields := left.Fields()
	for i, f := range fields {
		a := f.Value.(value.Tensor).Data()
		var result tensor.Data
		var err error
		switch name {
		case catalog.GradientsZero:
			result, err = maths.Constant(a.Shape(), 0)
		case catalog.GradientsAdd:
			result, err = maths.Apply(catalog.TensorAdd, a, right.Field(f.Name).(value.Tensor).Data(), nil)
		case catalog.GradientsScale:
			result, err = maths.Apply(catalog.TensorMul, a, factor, nil)
		default:
			panic("unknown gradient lifecycle operation")
		}
		if err != nil {
			return fail(err)
		}
		fields[i].Value = must(value.NewTensor(result))
	}
	return wrap(must(value.NewRecord(fields))), nil
}
