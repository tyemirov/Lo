package eval

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (m *machine) builtin(call ir.BuiltinCall, args []value.Value, flows map[int]identity.FlowID, f frame) (value.Value, error) {
	switch call.Operation().Name() {
	case catalog.CheckpointEncode, catalog.CheckpointDecode, catalog.ParametersTo, catalog.SGDStateTo, catalog.AdamWStateTo:
		return runtimeops.Checkpoint(m.Session, call, args, flows)
	case catalog.TensorTo:
		return runtimeops.TensorTo(m.Session, call, args, flows)
	case catalog.SGDConfigNew:
		return runtimeops.SGDConfigNew(m.Session, call, args, flows)
	case catalog.AdamWConfigNew:
		return runtimeops.AdamWConfigNew(m.Session, call, args, flows)
	case catalog.SGDStateNew:
		return runtimeops.SGDStateNew(m.Session, call, args, flows)
	case catalog.AdamWStateNew:
		return runtimeops.AdamWStateNew(m.Session, call, args, flows)
	case catalog.SGDStep:
		return runtimeops.SGDStep(m.Session, call, args, flows)
	case catalog.AdamWStep:
		return runtimeops.AdamWStep(m.Session, call, args, flows)
	case catalog.AutodiffGrad:
		return m.gradient(call, args)
	case catalog.AutodiffStop:
		return runtimeops.AutodiffStop(m.Session, call, args, flows)
	case catalog.ParametersNew:
		return runtimeops.ParametersNew(m.Session, call, args, flows)
	case catalog.ParametersValues:
		return runtimeops.ParametersValues(m.Session, call, args, flows)
	case catalog.GradientsNew:
		return runtimeops.GradientsNew(m.Session, call, args, flows)
	case catalog.GradientsValues:
		return runtimeops.GradientsValues(m.Session, call, args, flows)
	case catalog.GradientsZero:
		return runtimeops.GradientsZero(m.Session, call, args, flows)
	case catalog.GradientsAdd:
		return runtimeops.GradientsAdd(m.Session, call, args, flows)
	case catalog.GradientsScale:
		return runtimeops.GradientsScale(m.Session, call, args, flows)
	case catalog.DeviceNew:
		return runtimeops.DeviceNew(m.Session, call, args, flows)
	case catalog.TensorNew:
		return runtimeops.TensorNew(m.Session, call, args, flows)
	case catalog.TensorShape:
		return runtimeops.TensorShape(m.Session, call, args, flows)
	case catalog.TensorRank:
		return runtimeops.TensorRank(m.Session, call, args, flows)
	case catalog.TensorDtype:
		return runtimeops.TensorDtype(m.Session, call, args, flows)
	case catalog.TensorDevice:
		return runtimeops.TensorDevice(m.Session, call, args, flows)
	case catalog.TensorToFloat:
		return runtimeops.TensorToFloat(m.Session, call, args, flows)
	case catalog.TensorValues:
		return runtimeops.TensorValues(m.Session, call, args, flows)
	case catalog.TensorAdd:
		return runtimeops.TensorAdd(m.Session, call, args, flows)
	case catalog.TensorSub:
		return runtimeops.TensorSub(m.Session, call, args, flows)
	case catalog.TensorMul:
		return runtimeops.TensorMul(m.Session, call, args, flows)
	case catalog.TensorDiv:
		return runtimeops.TensorDiv(m.Session, call, args, flows)
	case catalog.TensorSqrt:
		return runtimeops.TensorSqrt(m.Session, call, args, flows)
	case catalog.TensorRelu:
		return runtimeops.TensorRelu(m.Session, call, args, flows)
	case catalog.TensorMatmul:
		return runtimeops.TensorMatmul(m.Session, call, args, flows)
	case catalog.TensorBroadcast:
		return runtimeops.TensorBroadcast(m.Session, call, args, flows)
	case catalog.TensorReshape:
		return runtimeops.TensorReshape(m.Session, call, args, flows)
	case catalog.TensorTranspose:
		return runtimeops.TensorTranspose(m.Session, call, args, flows)
	case catalog.TensorSum:
		return runtimeops.TensorSum(m.Session, call, args, flows)
	case catalog.TensorMean:
		return runtimeops.TensorMean(m.Session, call, args, flows)
	case catalog.IntNew:
		return runtimeops.IntNew(m.Session, call, args, flows)
	case catalog.StringNew:
		return runtimeops.StringNew(m.Session, call, args, flows)
	case catalog.BoolNew:
		return runtimeops.BoolNew(m.Session, call, args, flows)
	case catalog.FloatNew:
		return runtimeops.FloatNew(m.Session, call, args, flows)
	case catalog.FloatAdd:
		return runtimeops.FloatAdd(m.Session, call, args, flows)
	case catalog.FloatSub:
		return runtimeops.FloatSub(m.Session, call, args, flows)
	case catalog.FloatMul:
		return runtimeops.FloatMul(m.Session, call, args, flows)
	case catalog.FloatDiv:
		return runtimeops.FloatDiv(m.Session, call, args, flows)
	case catalog.FloatSqrt:
		return runtimeops.FloatSqrt(m.Session, call, args, flows)
	case catalog.FloatEqual:
		return runtimeops.FloatEqual(m.Session, call, args, flows)
	case catalog.FloatLT:
		return runtimeops.FloatLT(m.Session, call, args, flows)
	case catalog.FloatLTE:
		return runtimeops.FloatLTE(m.Session, call, args, flows)
	case catalog.FloatGT:
		return runtimeops.FloatGT(m.Session, call, args, flows)
	case catalog.FloatGTE:
		return runtimeops.FloatGTE(m.Session, call, args, flows)
	case catalog.IntToFloat:
		return runtimeops.IntToFloat(m.Session, call, args, flows)
	case catalog.FloatToInt:
		return runtimeops.FloatToInt(m.Session, call, args, flows)
	case catalog.FloatToString:
		return runtimeops.FloatToString(m.Session, call, args, flows)
	case catalog.ScalarNew:
		return runtimeops.ScalarNew(m.Session, call, args, flows)
	case catalog.PathNew:
		return runtimeops.PathNew(m.Session, call, args, flows)
	case catalog.BytesNew:
		return runtimeops.BytesNew(m.Session, call, args, flows)
	case catalog.ListNew:
		return runtimeops.ListNew(m.Session, call, args, flows)
	case catalog.RecordNew:
		return runtimeops.RecordNew(m.Session, call, args, flows)
	case catalog.MapNew:
		return runtimeops.MapNew(m.Session, call, args, flows)
	case catalog.RangeNew:
		return runtimeops.RangeNew(m.Session, call, args, flows)
	case catalog.Concat:
		return runtimeops.Concat(m.Session, call, args, flows)
	case catalog.Upper:
		return runtimeops.Upper(m.Session, call, args, flows)
	case catalog.Lower:
		return runtimeops.Lower(m.Session, call, args, flows)
	case catalog.StringLength:
		return runtimeops.StringLength(m.Session, call, args, flows)
	case catalog.StringEqual:
		return runtimeops.StringEqual(m.Session, call, args, flows)
	case catalog.ScalarEqual:
		return runtimeops.ScalarEqual(m.Session, call, args, flows)
	case catalog.IntToString:
		return runtimeops.IntToString(m.Session, call, args, flows)
	case catalog.FromScalarsNew:
		return runtimeops.FromScalarsNew(m.Session, call, args, flows)
	case catalog.MathAdd:
		return runtimeops.MathAdd(m.Session, call, args, flows)
	case catalog.MathSub:
		return runtimeops.MathSub(m.Session, call, args, flows)
	case catalog.MathMul:
		return runtimeops.MathMul(m.Session, call, args, flows)
	case catalog.MathDiv:
		return runtimeops.MathDiv(m.Session, call, args, flows)
	case catalog.MathMod:
		return runtimeops.MathMod(m.Session, call, args, flows)
	case catalog.MathLT:
		return runtimeops.MathLT(m.Session, call, args, flows)
	case catalog.MathLTE:
		return runtimeops.MathLTE(m.Session, call, args, flows)
	case catalog.MathGT:
		return runtimeops.MathGT(m.Session, call, args, flows)
	case catalog.MathGTE:
		return runtimeops.MathGTE(m.Session, call, args, flows)
	case catalog.MathEqual:
		return runtimeops.MathEqual(m.Session, call, args, flows)
	case catalog.MathSum:
		return runtimeops.MathSum(m.Session, call, args, flows)
	case catalog.MathProduct:
		return runtimeops.MathProduct(m.Session, call, args, flows)
	case catalog.MathMin:
		return runtimeops.MathMin(m.Session, call, args, flows)
	case catalog.MathMax:
		return runtimeops.MathMax(m.Session, call, args, flows)
	case catalog.Length:
		return runtimeops.Length(m.Session, call, args, flows)
	case catalog.Contains:
		return runtimeops.Contains(m.Session, call, args, flows)
	case catalog.IsEmpty:
		return runtimeops.IsEmpty(m.Session, call, args, flows)
	case catalog.Merge:
		return runtimeops.Merge(m.Session, call, args, flows)
	case catalog.HasKey:
		return runtimeops.HasKey(m.Session, call, args, flows)
	case catalog.Get:
		return runtimeops.Get(m.Session, call, args, flows)
	case catalog.ErrorNew:
		return runtimeops.ErrorNew(m.Session, call, args, flows)
	case catalog.UTF8New:
		return runtimeops.UTF8New(m.Session, call, args, flows)
	case catalog.UTF8BytesNew:
		return runtimeops.UTF8BytesNew(m.Session, call, args, flows)
	case catalog.FileOpenPaths:
		return runtimeops.FileOpenPaths(m.Session, call, args, flows)
	case catalog.FilePathEqual:
		return runtimeops.PathEqual(m.Session, call, args, flows)
	case catalog.FileList:
		return runtimeops.FileList(m.Session, call, args, flows)
	case catalog.FileDirectory, catalog.FileScan:
		return runtimeops.FileObservation(m.Session, call, args, flows)
	case catalog.FileRemoveChecked:
		return runtimeops.FileRemoveChecked(m.Session, call, args, flows)
	case catalog.HostInvocationCurrent:
		return runtimeops.HostInvocationCurrent(m.Session, call, args, flows)
	case catalog.FileRemove:
		return runtimeops.FileRemove(m.Session, call, args, flows)
	case catalog.FileRead:
		return runtimeops.FileRead(m.Session, call, args, flows)
	case catalog.FileAppend:
		return runtimeops.FileAppend(m.Session, call, args, flows)
	case catalog.FileWrite:
		return runtimeops.FileWrite(m.Session, call, args, flows)
	case catalog.ServerNew:
		return runtimeops.ServerNew(m.Session, call, args, flows)
	case catalog.HeadersNew:
		return runtimeops.HeadersNew(m.Session, call, args, flows)
	case catalog.ProcessExecutableNew:
		return runtimeops.ProcessExecutableNew(m.Session, call, args, flows)
	case catalog.ProcessContextCurrent, catalog.ProcessContextNew, catalog.ProcessContextOverride, catalog.ProcessContextWithout, catalog.ProcessContextDirectory, catalog.ProcessContextEnvironmentValue, catalog.ProcessExecutableResolve:
		return runtimeops.ProcessContextOperation(m.Session, call, args, flows)
	case catalog.ProcessCommandNew:
		return runtimeops.ProcessCommandNew(m.Session, call, args, flows)
	case catalog.ProcessTerminationExitCode:
		return runtimeops.ProcessTerminationExitCode(m.Session, call, args, flows)
	case catalog.ProcessTerminationSignal:
		return runtimeops.ProcessTerminationSignal(m.Session, call, args, flows)
	case catalog.URLNew:
		return runtimeops.URLNew(m.Session, call, args, flows)
	case catalog.MethodNew:
		return runtimeops.MethodNew(m.Session, call, args, flows)
	case catalog.ClientRequestNew:
		return runtimeops.ClientRequestNew(m.Session, call, args, flows)
	case catalog.HeaderValues:
		return runtimeops.HeaderValues(m.Session, call, args, flows)
	case catalog.ResponseNew:
		return runtimeops.ResponseNew(m.Session, call, args, flows)
	case catalog.ResponseTextNew:
		return runtimeops.ResponseTextNew(m.Session, call, args, flows)
	case catalog.ResponseBytesNew:
		return runtimeops.ResponseBytesNew(m.Session, call, args, flows)
	case catalog.ListMap:
		return m.listProcessor(call, args, flows[len(args)-1], f)
	case catalog.ListFilter:
		return m.listProcessor(call, args, flows[len(args)-1], f)
	case catalog.ListFold, catalog.FlowFold:
		return m.listProcessor(call, args, flows[len(args)-1], f)
	case catalog.ListFlatMap:
		return m.listProcessor(call, args, flows[len(args)-1], f)
	case catalog.ListUnfold:
		return m.listProcessor(call, args, flows[len(args)-1], f)
	default:
		return nil, fmt.Errorf("missing checked operation %s", call.Operation().Name())
	}
}
