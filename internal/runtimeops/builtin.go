package runtimeops

import (
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"strings"
	"unicode/utf8"
)

func SGDConfigNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.SGDConfigNew
	_ = op
	return m.optimizerOperation(call, args)
}
func AdamWConfigNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.AdamWConfigNew
	_ = op
	return m.optimizerOperation(call, args)
}
func SGDStateNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.SGDStateNew
	_ = op
	return m.optimizerOperation(call, args)
}
func AdamWStateNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.AdamWStateNew
	_ = op
	return m.optimizerOperation(call, args)
}
func SGDStep(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.SGDStep
	_ = op
	return m.optimizerOperation(call, args)
}
func AdamWStep(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.AdamWStep
	_ = op
	return m.optimizerOperation(call, args)
}
func AutodiffStop(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.AutodiffStop
	_ = op
	return args[0], nil
}
func ParametersNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ParametersNew
	_ = op
	return m.parameterOperation(call, args)
}
func ParametersValues(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ParametersValues
	_ = op
	return m.parameterOperation(call, args)
}
func GradientsNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.GradientsNew
	_ = op
	return m.parameterOperation(call, args)
}
func GradientsValues(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.GradientsValues
	_ = op
	return m.parameterOperation(call, args)
}
func GradientsZero(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.GradientsZero
	_ = op
	return m.parameterOperation(call, args)
}
func GradientsAdd(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.GradientsAdd
	_ = op
	return m.parameterOperation(call, args)
}
func GradientsScale(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.GradientsScale
	_ = op
	return m.parameterOperation(call, args)
}
func DeviceNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.DeviceNew
	_ = op
	return m.tensorOperation(call, args)
}
func TensorNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorNew
	_ = op
	return m.tensorOperation(call, args)
}
func TensorShape(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorShape
	_ = op
	return m.tensorOperation(call, args)
}
func TensorRank(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorRank
	_ = op
	return m.tensorOperation(call, args)
}
func TensorDtype(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorDtype
	_ = op
	return m.tensorOperation(call, args)
}
func TensorDevice(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorDevice
	_ = op
	return m.tensorOperation(call, args)
}
func TensorToFloat(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorToFloat
	_ = op
	return m.tensorOperation(call, args)
}
func TensorValues(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorValues
	_ = op
	return m.tensorOperation(call, args)
}
func TensorAdd(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorAdd
	_ = op
	return m.tensorOperation(call, args)
}
func TensorSub(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorSub
	_ = op
	return m.tensorOperation(call, args)
}
func TensorMul(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorMul
	_ = op
	return m.tensorOperation(call, args)
}
func TensorDiv(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorDiv
	_ = op
	return m.tensorOperation(call, args)
}
func TensorSqrt(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorSqrt
	_ = op
	return m.tensorOperation(call, args)
}
func TensorRelu(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorRelu
	_ = op
	return m.tensorOperation(call, args)
}
func TensorMatmul(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorMatmul
	_ = op
	return m.tensorOperation(call, args)
}
func TensorBroadcast(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorBroadcast
	_ = op
	return m.tensorOperation(call, args)
}
func TensorReshape(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorReshape
	_ = op
	return m.tensorOperation(call, args)
}
func TensorTranspose(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorTranspose
	_ = op
	return m.tensorOperation(call, args)
}
func TensorSum(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorSum
	_ = op
	return m.tensorOperation(call, args)
}
func TensorMean(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.TensorMean
	_ = op
	return m.tensorOperation(call, args)
}
func IntNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.IntNew
	_ = op
	return args[0], nil
}
func StringNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.StringNew
	_ = op
	return args[0], nil
}
func BoolNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.BoolNew
	_ = op
	return args[0], nil
}
func FloatNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatNew
	_ = op
	return args[0], nil
}
func FloatAdd(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatAdd
	_ = op
	return m.floatOperation(call, args)
}
func FloatSub(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatSub
	_ = op
	return m.floatOperation(call, args)
}
func FloatMul(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatMul
	_ = op
	return m.floatOperation(call, args)
}
func FloatDiv(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatDiv
	_ = op
	return m.floatOperation(call, args)
}
func FloatSqrt(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatSqrt
	_ = op
	return m.floatOperation(call, args)
}
func FloatEqual(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatEqual
	_ = op
	return m.floatOperation(call, args)
}
func FloatLT(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatLT
	_ = op
	return m.floatOperation(call, args)
}
func FloatLTE(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatLTE
	_ = op
	return m.floatOperation(call, args)
}
func FloatGT(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatGT
	_ = op
	return m.floatOperation(call, args)
}
func FloatGTE(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatGTE
	_ = op
	return m.floatOperation(call, args)
}
func IntToFloat(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.IntToFloat
	_ = op
	return m.floatOperation(call, args)
}
func FloatToInt(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatToInt
	_ = op
	return m.floatOperation(call, args)
}
func FloatToString(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FloatToString
	_ = op
	return m.floatOperation(call, args)
}
func ScalarNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ScalarNew
	_ = op
	return must(value.NewText(types.Scalar(), args[0].(value.Text).Text())), nil
}
func PathNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.PathNew
	_ = op
	return must(value.NewText(types.Path(), args[0].(value.Text).Text())), nil
}
func BytesNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.BytesNew
	_ = op
	return value.NewBytes([]byte(args[0].(value.Text).Text())), nil
}
func ListNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ListNew
	_ = op
	return m.list(call.Type().(types.List), args)
}
func RecordNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.RecordNew
	_ = op
	fields := make([]value.Field, len(args))
	for i, name := range call.Labels() {
		fields[i] = value.Field{Name: name, Value: args[i]}
	}
	return must(value.NewRecord(fields)), nil
}
func MapNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MapNew
	_ = op
	entries := make([]value.Entry, len(args))
	for i, v := range args {
		r := v.(value.Record)
		entries[i] = value.Entry{Key: r.Field("key"), Value: r.Field("value")}
	}
	return must(value.NewMap(call.Type().(types.Map), entries)), nil
}
func RangeNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.RangeNew
	_ = op
	return must(value.NewRange(call.Type().(types.Range), args[0], args[1], args[2].(value.Boolean).Bool(), args[3].(value.Boolean).Bool())), nil
}
func Concat(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Concat
	_ = op
	var b strings.Builder
	for _, v := range args {
		s := v.(value.Text).Text()
		if limit := session.Limits().ValueBytes; limit != 0 && uint64(len(s))+uint64(b.Len())+16 > limit {
			return nil, execution.Limit("string bytes")
		}
		b.WriteString(s)
	}
	return must(value.NewText(types.String(), b.String())), nil
}
func Upper(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Upper
	_ = op
	s := args[0].(value.Text).Text()
	if op == catalog.Upper {
		s = strings.ToUpper(s)
	} else {
		s = strings.ToLower(s)
	}
	if limit := session.Limits().ValueBytes; limit != 0 && uint64(len(s))+16 > limit {
		return nil, execution.Limit("string bytes")
	}
	return must(value.NewText(types.String(), s)), nil
}
func Lower(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Lower
	_ = op
	s := args[0].(value.Text).Text()
	if op == catalog.Upper {
		s = strings.ToUpper(s)
	} else {
		s = strings.ToLower(s)
	}
	if limit := session.Limits().ValueBytes; limit != 0 && uint64(len(s))+16 > limit {
		return nil, execution.Limit("string bytes")
	}
	return must(value.NewText(types.String(), s)), nil
}
func StringLength(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.StringLength
	_ = op
	return value.Int64(int64(utf8.RuneCountInString(args[0].(value.Text).Text()))), nil
}
func StringEqual(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.StringEqual
	_ = op
	return value.Bool(value.Equal(args[0], args[1])), nil
}
func ScalarEqual(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ScalarEqual
	_ = op
	return value.Bool(value.Equal(args[0], args[1])), nil
}
func IntToString(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.IntToString
	_ = op
	return must(value.NewText(types.String(), args[0].(value.Integer).Big().String())), nil
}
func FromScalarsNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FromScalarsNew
	_ = op
	var b strings.Builder
	for _, v := range args[0].(value.List).Items() {
		b.WriteString(v.(value.Text).Text())
	}
	return must(value.NewText(types.String(), b.String())), nil
}
func MathAdd(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathAdd
	_ = op
	return m.binaryMath(call, args)
}
func MathSub(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathSub
	_ = op
	return m.binaryMath(call, args)
}
func MathMul(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathMul
	_ = op
	return m.binaryMath(call, args)
}
func MathDiv(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathDiv
	_ = op
	return m.binaryMath(call, args)
}
func MathMod(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathMod
	_ = op
	return m.binaryMath(call, args)
}
func MathLT(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathLT
	_ = op
	return m.binaryMath(call, args)
}
func MathLTE(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathLTE
	_ = op
	return m.binaryMath(call, args)
}
func MathGT(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathGT
	_ = op
	return m.binaryMath(call, args)
}
func MathGTE(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathGTE
	_ = op
	return m.binaryMath(call, args)
}
func MathEqual(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathEqual
	_ = op
	return m.binaryMath(call, args)
}
func MathSum(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathSum
	_ = op
	return m.aggregate(call, args)
}
func MathProduct(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathProduct
	_ = op
	return m.aggregate(call, args)
}
func MathMin(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathMin
	_ = op
	return m.aggregate(call, args)
}
func MathMax(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.MathMax
	_ = op
	return m.aggregate(call, args)
}
func Length(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Length
	_ = op
	return value.Int64(int64(args[0].(value.List).Len())), nil
}
func Contains(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Contains
	_ = op
	return value.Bool(args[0].(value.Range).Contains(args[1])), nil
}
func IsEmpty(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.IsEmpty
	_ = op
	return value.Bool(args[0].(value.Range).Empty()), nil
}
func Merge(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Merge
	_ = op
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
}
func HasKey(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.HasKey
	_ = op
	v, found := args[0].(value.Map).Get(args[1])
	if op == catalog.HasKey {
		return value.Bool(found), nil
	}
	if !found {
		return OperationFailure(call, args, catalog.MissingKey, "map key is absent"), nil
	}
	if v.Type().Kind() == types.FallibleKind {
		return v, nil
	}
	return Success(call.Type(), v), nil
}
func Get(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.Get
	_ = op
	v, found := args[0].(value.Map).Get(args[1])
	if op == catalog.HasKey {
		return value.Bool(found), nil
	}
	if !found {
		return OperationFailure(call, args, catalog.MissingKey, "map key is absent"), nil
	}
	if v.Type().Kind() == types.FallibleKind {
		return v, nil
	}
	return Success(call.Type(), v), nil
}
func ErrorNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ErrorNew
	_ = op
	return value.At(must(value.NewError(args[0].(value.Text).Text(), args[1].(value.Text).Text(), args[2].(value.Record))), op, call.Span()), nil
}
func UTF8New(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.UTF8New
	_ = op
	data := args[0].(value.Bytes).Bytes()
	if !utf8.Valid(data) {
		return OperationFailure(call, args, catalog.InvalidUTF8, "bytes are not valid UTF-8"), nil
	}
	return Success(call.Type(), must(value.NewText(types.String(), string(data)))), nil
}
func FileRead(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FileRead
	_ = op
	data, err := m.Boundary().Read(m.Context(), args[0].(value.Text))
	if err != nil {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		return OperationFailure(call, args, catalog.ReadFailed, err.Error()), nil
	}
	if limit := session.Limits().ValueBytes; limit != 0 && data.Weight() > limit {
		return nil, execution.Limit("file read bytes")
	}
	return Success(call.Type(), data), nil
}
func FileAppend(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FileAppend
	_ = op
	return must(value.NewEffect(call.Operation(), args[0].(value.Text), args[1])), nil
}
func FileWrite(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.FileWrite
	_ = op
	return must(value.NewEffect(call.Operation(), args[0].(value.Text), args[1])), nil
}
func ServerNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ServerNew
	_ = op
	return must(value.NewServer(args[0].(value.Integer), flows[1], flows[2])), nil
}
func ResponseTextNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ResponseTextNew
	_ = op
	body := args[1]
	if op == catalog.ResponseTextNew {
		body = value.NewBytes([]byte(args[1].(value.Text).Text()))
	}
	headers := must(value.NewHeaders(must(value.NewList(types.HeaderEntries(), nil))))
	fields := must(value.NewRecord([]value.Field{{Name: "status", Value: args[0]}, {Name: "headers", Value: headers}, {Name: "body", Value: body}}))
	return must(value.NewHTTP(types.Response(), fields)), nil
}
func ResponseBytesNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	m := &machine{session}
	_ = m
	op := catalog.ResponseBytesNew
	_ = op
	body := args[1]
	if op == catalog.ResponseTextNew {
		body = value.NewBytes([]byte(args[1].(value.Text).Text()))
	}
	headers := must(value.NewHeaders(must(value.NewList(types.HeaderEntries(), nil))))
	fields := must(value.NewRecord([]value.Field{{Name: "status", Value: args[0]}, {Name: "headers", Value: headers}, {Name: "body", Value: body}}))
	return must(value.NewHTTP(types.Response(), fields)), nil
}

func TensorTo(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	return (&machine{session}).tensorOperation(call, args)
}

func FileList(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	entries, err := session.Boundary().List(session.Context(), args[0].(value.Text))
	if err != nil {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		kind := catalog.ListFailed
		var file execution.FileError
		if errors.As(err, &file) {
			kind = file.Kind()
		}
		return OperationFailure(call, args, kind, err.Error()), nil
	}
	return Success(call.Type(), entries), nil
}
func FileRemove(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	entries := args[0].(value.List)
	count := entries.Len()
	if limit := session.Limits().FileEntries; limit != 0 && uint64(count) > limit {
		return nil, execution.Limit("file removal entries")
	}
	weight := entries.Weight() + 32 + uint64(len(catalog.FileRemove))
	if err := session.ReserveFile(uint64(count), weight); err != nil {
		return nil, err
	}
	effect, err := value.NewRemovalEffect(session.FileSession(), entries)
	if err != nil {
		return OperationFailure(call, args, catalog.InvalidSelection, err.Error()), nil
	}
	return Success(call.Type(), effect), nil
}

func FileOpenPaths(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	paths, err := session.Boundary().OpenPaths(session.Context(), args[0].(value.Text))
	if err != nil {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		return OperationFailure(call, args, catalog.OpenFilesFailed, err.Error()), nil
	}
	return Success(call.Type(), paths), nil
}

func PathEqual(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	return value.Bool(args[0].(value.Text).Text() == args[1].(value.Text).Text()), nil
}

func UTF8BytesNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	return value.NewBytes([]byte(args[0].(value.Text).Text())), nil
}

func HeadersNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	headers, err := value.NewHeaders(args[0].(value.List))
	if err != nil {
		return OperationFailure(call, nil, catalog.InvalidHeaders, err.Error()), nil
	}
	return Success(call.Type(), headers), nil
}
func HeaderValues(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	name, err := value.CanonicalHeaderName(args[1].(value.Text).Text())
	if err != nil {
		return OperationFailure(call, nil, catalog.InvalidHeaders, err.Error()), nil
	}
	matches := []value.Value{}
	for _, entry := range args[0].(value.Headers).Entries().Items() {
		fields := entry.(value.Record)
		if fields.Field("name").(value.Text).Text() == name {
			matches = append(matches, fields.Field("value"))
		}
	}
	return Success(call.Type(), must(value.NewList(must(types.NewList(types.Bytes())), matches))), nil
}
func ResponseNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	fields := must(value.NewRecord([]value.Field{{Name: "status", Value: args[0]}, {Name: "headers", Value: args[1]}, {Name: "body", Value: args[2]}}))
	return value.NewHTTP(types.Response(), fields)
}
