package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

func addProcessCommands(m map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	command := types.Parameter{Name: "command", Type: types.ProcessCommand()}
	for _, entry := range []struct {
		name      string
		parameter types.Parameter
	}{
		{ProcessCommandWithTimeout, types.Parameter{Name: "timeout_ms", Type: types.Int()}},
		{ProcessCommandWithOutput, types.Parameter{Name: "mode", Type: types.String()}},
	} {
		fixed(entry.name, []types.Parameter{command, entry.parameter}, must(types.NewFallible(types.ProcessCommand())))
		op := m[entry.name].(operation)
		op.errors = []string{ProcessInvalidCommand}
		m[entry.name] = op
	}
}
