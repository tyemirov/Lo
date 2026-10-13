package eval

import (
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (m *machine) builtin(call ir.BuiltinCall, args []value.Value, flows map[int]ir.FlowID, f frame) (value.Value, error) {
	op := call.Operation().Name()
	switch op {
	case catalog.IntNew, catalog.StringNew, catalog.BoolNew, catalog.FloatNew:
		return args[0], nil
	case catalog.ScalarNew:
		return must(value.NewText(types.Scalar(), args[0].(value.Text).Text())), nil
	case catalog.PathNew:
		return must(value.NewText(types.Path(), args[0].(value.Text).Text())), nil
	case catalog.BytesNew:
		return value.NewBytes([]byte(args[0].(value.Text).Text())), nil
	case catalog.ListNew:
		return m.list(call.Type().(types.List), args)
	case catalog.RecordNew:
		fields := make([]value.Field, len(args))
		for i, name := range call.Labels() {
			fields[i] = value.Field{Name: name, Value: args[i]}
		}
		return must(value.NewRecord(fields)), nil
	case catalog.MapNew:
		entries := make([]value.Entry, len(args))
		for i, v := range args {
			r := v.(value.Record)
			entries[i] = value.Entry{Key: r.Field("key"), Value: r.Field("value")}
		}
		return must(value.NewMap(call.Type().(types.Map), entries)), nil
	case catalog.RangeNew:
		return must(value.NewRange(call.Type().(types.Range), args[0], args[1], args[2].(value.Boolean).Bool(), args[3].(value.Boolean).Bool())), nil
	case catalog.Concat:
		var b strings.Builder
		for _, v := range args {
			s := v.(value.Text).Text()
			if len(s) > MaxBytes-b.Len() {
				return nil, Limit("string bytes")
			}
			b.WriteString(s)
		}
		return must(value.NewText(types.String(), b.String())), nil
	case catalog.Upper, catalog.Lower:
		s := args[0].(value.Text).Text()
		if op == catalog.Upper {
			s = strings.ToUpper(s)
		} else {
			s = strings.ToLower(s)
		}
		if len(s) > MaxBytes {
			return nil, Limit("string bytes")
		}
		return must(value.NewText(types.String(), s)), nil
	case catalog.StringLength:
		return value.Int64(int64(utf8.RuneCountInString(args[0].(value.Text).Text()))), nil
	case catalog.StringEqual, catalog.ScalarEqual:
		return value.Bool(value.Equal(args[0], args[1])), nil
	case catalog.IntToString:
		return must(value.NewText(types.String(), args[0].(value.Integer).Big().String())), nil
	case catalog.FromScalarsNew:
		var b strings.Builder
		for _, v := range args[0].(value.List).Items() {
			b.WriteString(v.(value.Text).Text())
		}
		return must(value.NewText(types.String(), b.String())), nil
	case catalog.MathAdd, catalog.MathSub, catalog.MathMul, catalog.MathDiv, catalog.MathMod, catalog.MathLT, catalog.MathLTE, catalog.MathGT, catalog.MathGTE, catalog.MathEqual:
		return m.binaryMath(call, args)
	case catalog.MathSum, catalog.MathProduct, catalog.MathMin, catalog.MathMax:
		return m.aggregate(call, args)
	case catalog.Length:
		return value.Int64(int64(len(args[0].(value.List).Items()))), nil
	case catalog.Contains:
		return value.Bool(args[0].(value.Range).Contains(args[1])), nil
	case catalog.IsEmpty:
		return value.Bool(args[0].(value.Range).Empty()), nil
	case catalog.Merge:
		fields := map[string]value.Value{}
		for _, arg := range args {
			for _, field := range arg.(value.Record).Fields() {
				fields[field.Name] = field.Value
			}
		}
		merged := make([]value.Field, 0, len(fields))
		for name, v := range fields {
			merged = append(merged, value.Field{Name: name, Value: v})
		}
		return must(value.NewRecord(merged)), nil
	case catalog.HasKey, catalog.Get:
		v, found := args[0].(value.Map).Get(args[1])
		if op == catalog.HasKey {
			return value.Bool(found), nil
		}
		if !found {
			return operationFailure(call, args, catalog.MissingKey, "map key is absent"), nil
		}
		if v.Type().Kind() == types.FallibleKind {
			return v, nil
		}
		return success(call.Type(), v), nil
	case catalog.ErrorNew:
		return value.At(must(value.NewError(args[0].(value.Text).Text(), args[1].(value.Text).Text(), args[2].(value.Record))), op, call.Span()), nil
	case catalog.UTF8New:
		data := args[0].(value.Bytes).Bytes()
		if !utf8.Valid(data) {
			return operationFailure(call, args, catalog.InvalidUTF8, "bytes are not valid UTF-8"), nil
		}
		return success(call.Type(), must(value.NewText(types.String(), string(data)))), nil
	case catalog.FileRead:
		data, err := m.boundary.Read(m.ctx, args[0].(value.Text))
		if err != nil {
			if host := boundaryHostError(err); host != nil {
				return nil, host
			}
			return operationFailure(call, args, catalog.ReadFailed, err.Error()), nil
		}
		if len(data.Bytes()) > MaxBytes {
			return nil, Limit("file read bytes")
		}
		return success(call.Type(), data), nil
	case catalog.FileAppend, catalog.FileWrite:
		return must(value.NewEffect(call.Operation(), args[0].(value.Text), args[1])), nil
	case catalog.ServerNew:
		return must(value.NewServer(args[0].(value.Integer), flows[1], flows[2])), nil
	case catalog.ResponseTextNew, catalog.ResponseBytesNew:
		body := args[1]
		if op == catalog.ResponseTextNew {
			body = value.NewBytes([]byte(args[1].(value.Text).Text()))
		}
		headers := must(value.NewMap(must(types.NewMap(types.String(), types.String())), nil))
		fields := must(value.NewRecord([]value.Field{{Name: "status", Value: args[0]}, {Name: "headers", Value: headers}, {Name: "body", Value: body}}))
		return must(value.NewHTTP(types.Response(), fields)), nil
	case catalog.ListMap, catalog.ListFilter, catalog.ListFold, catalog.ListFlatMap, catalog.ListUnfold:
		return m.listProcessor(call, args, flows[len(args)-1], f)
	default:
		panic("missing checked builtin implementation: " + op)
	}
}

func (m *machine) binaryMath(call ir.BuiltinCall, args []value.Value) (value.Value, error) {
	a, b := args[0].(value.Integer).Big(), args[1].(value.Integer).Big()
	n := new(big.Int)
	switch call.Operation().Name() {
	case catalog.MathAdd:
		n.Add(a, b)
	case catalog.MathSub:
		n.Sub(a, b)
	case catalog.MathMul:
		if a.Sign() != 0 && b.Sign() != 0 && a.BitLen()+b.BitLen()-1 > MaxIntegerBits {
			return nil, Limit("integer bits")
		}
		n.Mul(a, b)
	case catalog.MathDiv:
		if b.Sign() == 0 {
			return operationFailure(call, args, catalog.DivideByZero, "division by zero"), nil
		}
		n.Quo(a, b)
	case catalog.MathMod:
		if b.Sign() == 0 {
			return operationFailure(call, args, catalog.ModuloByZero, "remainder by zero"), nil
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
	return success(call.Type(), v), nil
}
func (m *machine) aggregate(call ir.BuiltinCall, args []value.Value) (value.Value, error) {
	items := args[0].(value.List).Items()
	op := call.Operation().Name()
	if len(items) == 0 && (op == catalog.MathMin || op == catalog.MathMax) {
		return operationFailure(call, args, catalog.EmptyList, "empty number list"), nil
	}
	n := new(big.Int)
	if op == catalog.MathProduct {
		n.SetInt64(1)
	}
	for i, v := range items {
		if err := m.step(); err != nil {
			return nil, err
		}
		x := v.(value.Integer).Big()
		switch op {
		case catalog.MathSum:
			n.Add(n, x)
		case catalog.MathProduct:
			if n.Sign() != 0 && x.Sign() != 0 && n.BitLen()+x.BitLen()-1 > MaxIntegerBits {
				return nil, Limit("integer bits")
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
		if n.BitLen() > MaxIntegerBits {
			return nil, Limit("integer bits")
		}
	}
	return success(call.Type(), must(value.NewInt(n))), nil
}
func success(t types.Type, v value.Value) value.Value {
	if out, ok := t.(types.Fallible); ok {
		return must(value.Success(out, v))
	}
	return v
}
func operationFailure(call ir.BuiltinCall, args []value.Value, kind, message string) value.Fallible {
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
func activationError(target value.Target, err error, span source.Span) value.Error {
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
