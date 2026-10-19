package runtimeops

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// ProcessExecutableNew constructs a pure executable description.
func ProcessExecutableNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	executable, err := value.NewProcessExecutable(args[0].(value.Text))
	if err != nil {
		return OperationFailure(call, nil, catalog.ProcessInvalidExecutable, err.Error()), nil
	}
	return Success(call.Type(), executable), nil
}

// ProcessCommandNew constructs a pure command description.
func ProcessCommandNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	command, err := value.NewProcessCommand(args[0].(value.ProcessExecutable), args[1].(value.List), args[2].(value.Map), args[3].(value.Text), args[4].(value.Bytes))
	if err != nil {
		return OperationFailure(call, nil, catalog.ProcessInvalidCommand, err.Error()), nil
	}
	return Success(call.Type(), command), nil
}

// ProcessTerminationExitCode selects the exit-code variant of a termination.
func ProcessTerminationExitCode(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	code, err := args[0].(value.ProcessTermination).ExitCode()
	if err != nil {
		return OperationFailure(call, nil, catalog.ProcessTerminationKind, err.Error()), nil
	}
	return Success(call.Type(), code), nil
}

// ProcessTerminationSignal selects the signal variant of a termination.
func ProcessTerminationSignal(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	signal, err := args[0].(value.ProcessTermination).Signal()
	if err != nil {
		return OperationFailure(call, nil, catalog.ProcessTerminationKind, err.Error()), nil
	}
	return Success(call.Type(), signal), nil
}
