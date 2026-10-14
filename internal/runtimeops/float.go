package runtimeops

import (
	"math"
	"math/big"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (m *machine) floatOperation(call Call, args []value.Value) (value.Value, error) {
	op := call.Operation().Name()
	if op == catalog.IntToFloat {
		n, _ := args[0].(value.Integer).Big().Float64()
		return floatResult(call, args, n), nil
	}
	a := args[0].(value.Float).Number()
	switch op {
	case catalog.FloatToInt:
		n, _ := new(big.Float).SetFloat64(a).Int(nil)
		return m.integer(n)
	case catalog.FloatToString:
		return must(value.NewText(types.String(), strconv.FormatFloat(a, 'g', -1, 64))), nil
	case catalog.FloatSqrt:
		if a < 0 {
			return OperationFailure(call, args, catalog.NumericDomain, "square root requires a nonnegative input"), nil
		}
		return floatResult(call, args, math.Sqrt(a)), nil
	}
	b := args[1].(value.Float).Number()
	var result float64
	switch op {
	case catalog.FloatAdd:
		result = float64(a + b)
	case catalog.FloatSub:
		result = float64(a - b)
	case catalog.FloatMul:
		result = float64(a * b)
	case catalog.FloatDiv:
		if b == 0 {
			return OperationFailure(call, args, catalog.NumericZero, "division by zero"), nil
		}
		result = float64(a / b)
	case catalog.FloatEqual:
		return value.Bool(a == b), nil
	case catalog.FloatLT:
		return value.Bool(a < b), nil
	case catalog.FloatLTE:
		return value.Bool(a <= b), nil
	case catalog.FloatGT:
		return value.Bool(a > b), nil
	case catalog.FloatGTE:
		return value.Bool(a >= b), nil
	default:
		panic("unknown checked floating-point operation")
	}
	return floatResult(call, args, result), nil
}

func floatResult(call Call, args []value.Value, n float64) value.Value {
	result, err := value.NewFloat(n)
	if err != nil {
		return OperationFailure(call, args, catalog.NumericNonFinite, "numeric result is not finite")
	}
	return Success(call.Type(), result)
}
