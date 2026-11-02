package runtimeops

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func ProgramPath(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	entry := execution.InitialProgramEntry(session.Context())
	if entry == nil || !entry.Info().Mode().IsRegular() {
		return OperationFailure(call, nil, catalog.ProgramEntryUnavailable, "program entry identity is unavailable"), nil
	}
	return Success(call.Type(), entry.Path()), nil
}
func LockFileNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	return value.NewLockFile(args[0].(value.Text))
}
