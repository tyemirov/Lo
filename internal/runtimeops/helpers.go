package runtimeops

import (
	"errors"
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
func ActivationError(target value.Target, err error, span source.Span) (value.Error, error) {
	var operation, kind string
	var input value.Record
	switch v := target.(type) {
	case value.ProcessCommand:
		progress := execution.ProcessProgress(err)
		if progress == nil {
			return nil, fmt.Errorf("no recoverable Process failure evidence")
		}
		operation, kind = catalog.ProcessRun, catalog.ProcessExecutionFailed
		fields := []value.Field{{Name: "progress", Value: progress}}
		fields = activationCleanup(fields, err)
		input = must(value.NewRecord(fields))
	case value.ClientRequest:
		operation, kind = catalog.HTTPExchange, catalog.HTTPExchangeFailed
		fields := []value.Field{{Name: "method", Value: v.Method()}, {Name: "origin", Value: must(value.NewText(types.String(), v.URL().Origin()))}}
		progress, progressErr := execution.HTTPProgress(err)
		if progressErr != nil {
			return nil, progressErr
		}
		if progress != nil {
			fields = append(fields, value.Field{Name: "progress", Value: progress})
		}
		fields = activationCleanup(fields, err)
		input = must(value.NewRecord(fields))
	case value.RemovalEffect:
		operation, kind = v.Operation().Capability(), catalog.RemoveFailed
		fields := []value.Field{{Name: catalog.ParamEntries, Value: v.Entries()}}
		var file execution.FileError
		if errors.As(err, &file) {
			kind = file.Kind()
		}
		if _, progress, ok := execution.RemovalDetails(err); ok {
			fields = append(fields, value.Field{Name: "progress", Value: progress})
		}
		input = must(value.NewRecord(fields))
	case value.ContentEffect:
		operation = v.Operation().Capability()
		kind = v.Operation().ActivationKinds()[0]
		fields := []value.Field{{Name: catalog.ParamPath, Value: v.Path()}, {Name: catalog.ParamContent, Value: v.Content()}}
		if confirmed, stage, ok := execution.WriteProgress(err); ok {
			progress := must(value.NewRecord([]value.Field{{Name: "bytes_written", Value: value.Int64(int64(confirmed))}, {Name: "stage", Value: must(value.NewText(types.String(), stage))}}))
			fields = append(fields, value.Field{Name: "progress", Value: progress})
		}
		fields = activationCleanup(fields, err)
		input = must(value.NewRecord(fields))
	case value.Server:
		operation, kind = catalog.HTTPListen, catalog.ServerStartFailed
		input = must(value.NewRecord([]value.Field{{Name: "port", Value: v.Port()}}))
	default:
		return nil, fmt.Errorf("no recoverable activation failure contract for %s", target.Type())
	}
	return value.At(must(value.NewError(kind, err.Error(), input)), operation, span), nil
}

func activationCleanup(fields []value.Field, err error) []value.Field {
	if cleanup := execution.CleanupDetails(err); len(cleanup) > 0 {
		items := make([]value.Value, len(cleanup))
		for i, message := range cleanup {
			items[i] = must(value.NewText(types.String(), message))
		}
		fields = append(fields, value.Field{Name: "cleanup_errors", Value: must(value.NewList(must(types.NewList(types.String())), items))})
	}
	return fields
}
