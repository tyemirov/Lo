package runtimeops

import (
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// ProcessContextOperation evaluates context operations with immutable invocation inputs.
func ProcessContextOperation(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	var result value.Value
	var err error
	kind := catalog.ProcessInvalidContext
	switch call.Operation().Name() {
	case catalog.ProcessContextCurrent:
		result, err = execution.InitialProcessContext(session.Context())
		kind = catalog.ProcessContextUnavailable
	case catalog.ProcessContextNew:
		result, err = value.NewProcessContext(args[0].(value.Map), args[1].(value.Text))
	case catalog.ProcessContextOverride:
		result, err = args[0].(value.ProcessContext).WithEnvironment(args[1].(value.Map))
	case catalog.ProcessContextWithout:
		result, err = args[0].(value.ProcessContext).WithoutEnvironment(args[1].(value.List))
	case catalog.ProcessContextDirectory:
		result, err = args[0].(value.ProcessContext).WithDirectory(args[1].(value.Text))
	case catalog.ProcessContextEnvironmentValue:
		text, found := args[0].(value.ProcessContext).EnvironmentValue(args[1].(value.Text).Text())
		if !found {
			return OperationFailure(call, nil, catalog.ProcessEnvironmentAbsent, "Process context variable is absent"), nil
		}
		result = text
	case catalog.ProcessExecutableResolve:
		result, err = adapter.ResolveProcessExecutable(session.Context(), args[0].(value.ProcessContext), args[1].(value.Text).Text())
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		kind = catalog.ProcessDiscoveryFailed
		if errors.Is(err, adapter.ErrProcessExecutableAbsent) {
			kind = catalog.ProcessExecutableAbsent
		}
	}
	if err != nil {
		return OperationFailure(call, nil, kind, err.Error()), nil
	}
	return Success(call.Type(), result), nil
}
