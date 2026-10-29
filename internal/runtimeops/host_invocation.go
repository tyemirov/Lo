package runtimeops

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// HostInvocationCurrent returns captured immutable invocation inputs.
func HostInvocationCurrent(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	initial, err := execution.InitialHostInvocation(session.Context())
	if err != nil {
		return OperationFailure(call, nil, catalog.HostInvocationFailed, err.Error()), nil
	}
	return Success(call.Type(), initial), nil
}
