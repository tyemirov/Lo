package runtimeops

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func URLNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	result, err := value.NewURL(args[0].(value.Text).Text())
	if err != nil {
		return httpConstructorFailure(call, catalog.InvalidURL, err.Error()), nil
	}
	return Success(call.Type(), result), nil
}
func MethodNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	result, err := value.NewMethod(args[0].(value.Text).Text())
	if err != nil {
		return httpConstructorFailure(call, catalog.InvalidMethod, err.Error()), nil
	}
	return Success(call.Type(), result), nil
}
func ClientRequestNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	result, err := value.NewClientRequest(args[0].(value.Text), args[1].(value.URL), args[2].(value.Headers), args[3].(value.Bytes))
	if err != nil {
		return httpConstructorFailure(call, catalog.InvalidRequest, err.Error()), nil
	}
	return Success(call.Type(), result), nil
}
func httpConstructorFailure(call Call, kind, message string) value.Value {
	input := must(value.NewRecord(nil))
	cause := value.At(must(value.NewError(kind, message, input)), call.Operation().Name(), call.Span())
	return must(value.Failure(call.Type().(types.Fallible), cause))
}
