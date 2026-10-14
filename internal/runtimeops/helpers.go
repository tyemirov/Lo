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

func (m *machine) binaryMath(call Call, args []value.Value) (value.Value, error) {
	a, b := args[0].(value.Integer).Big(), args[1].(value.Integer).Big()
	n := new(big.Int)
	switch call.Operation().Name() {
	case catalog.MathAdd:
		n.Add(a, b)
	case catalog.MathSub:
		n.Sub(a, b)
	case catalog.MathMul:
		if a.Sign() != 0 && b.Sign() != 0 && a.BitLen()+b.BitLen()-1 > execution.MaxIntegerBits {
			return nil, execution.Limit("integer bits")
		}
		n.Mul(a, b)
	case catalog.MathDiv:
		if b.Sign() == 0 {
			return OperationFailure(call, args, catalog.DivideByZero, "division by zero"), nil
		}
		n.Quo(a, b)
	case catalog.MathMod:
		if b.Sign() == 0 {
			return OperationFailure(call, args, catalog.ModuloByZero, "remainder by zero"), nil
		}
		n.Rem(a, b)
	case catalog.MathLT:
		return value.Bool(a.Cmp(b) < 0), nil
	case catalog.MathLTE:
		return value.Bool(a.Cmp(b) <= 0), nil
	case catalog.MathGT:
		return value.Bool(a.Cmp(b) > 0), nil
	case catalog.MathGTE:
		return value.Bool(a.Cmp(b) >= 0), nil
	case catalog.MathEqual:
		return value.Bool(a.Cmp(b) == 0), nil
	default:
		panic("unknown math operation")
	}
	v, err := m.integer(n)
	if err != nil {
		return nil, err
	}
	return Success(call.Type(), v), nil
}
func (m *machine) aggregate(call Call, args []value.Value) (value.Value, error) {
	items := args[0].(value.List).Items()
	op := call.Operation().Name()
	if len(items) == 0 && (op == catalog.MathMin || op == catalog.MathMax) {
		return OperationFailure(call, args, catalog.EmptyList, "empty number list"), nil
	}
	n := new(big.Int)
	if op == catalog.MathProduct {
		n.SetInt64(1)
	}
	for i, v := range items {
		if err := m.Step(); err != nil {
			return nil, err
		}
		x := v.(value.Integer).Big()
		switch op {
		case catalog.MathSum:
			n.Add(n, x)
		case catalog.MathProduct:
			if n.Sign() != 0 && x.Sign() != 0 && n.BitLen()+x.BitLen()-1 > execution.MaxIntegerBits {
				return nil, execution.Limit("integer bits")
			}
			n.Mul(n, x)
		case catalog.MathMin:
			if i == 0 || x.Cmp(n) < 0 {
				n.Set(x)
			}
		case catalog.MathMax:
			if i == 0 || x.Cmp(n) > 0 {
				n.Set(x)
			}
		}
		if n.BitLen() > execution.MaxIntegerBits {
			return nil, execution.Limit("integer bits")
		}
	}
	return Success(call.Type(), must(value.NewInt(n))), nil
}
func Success(t types.Type, v value.Value) value.Value {
	if out, ok := t.(types.Fallible); ok {
		return must(value.Success(out, v))
	}
	return v
}
func OperationFailure(call Call, args []value.Value, kind, message string) value.Fallible {
	params := call.Operation().Parameters()
	fields := make([]value.Field, 0, len(args))
	for i, arg := range args {
		if arg == nil {
			continue
		}
		name := fmt.Sprintf("argument_%d", i)
		if i < len(params) {
			name = params[i].Name
		}
		fields = append(fields, value.Field{Name: name, Value: arg})
	}
	e := must(value.NewError(kind, message, must(value.NewRecord(fields))))
	return must(value.Failure(call.Type().(types.Fallible), value.At(e, call.Operation().Name(), call.Span())))
}
func ActivationError(target value.Target, err error, span source.Span) value.Error {
	var operation, kind string
	var input value.Record
	switch v := target.(type) {
	case value.Effect:
		operation = v.Operation().Name()
		kind = v.Operation().ActivationKinds()[0]
		input = must(value.NewRecord([]value.Field{{Name: catalog.ParamPath, Value: v.Path()}, {Name: catalog.ParamContent, Value: v.Content()}}))
	case value.Server:
		operation, kind = catalog.HTTPListen, catalog.ServerStartFailed
		input = must(value.NewRecord([]value.Field{{Name: "port", Value: v.Port()}}))
	default:
		panic("unknown commit target")
	}
	return value.At(must(value.NewError(kind, err.Error(), input)), operation, span)
}
