package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

func addProcessContexts(m map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	context := types.Parameter{Name: "context", Type: types.ProcessContext()}
	environment := types.Parameter{Name: "environment", Type: must(types.NewMap(types.String(), types.String()))}
	directory := types.Parameter{Name: "directory", Type: types.Path()}
	for _, entry := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		boundary   Boundary
		errors     []string
	}{
		{ProcessContextCurrent, nil, types.ProcessContext(), Read, []string{ProcessContextUnavailable}},
		{ProcessContextNew, []types.Parameter{environment, directory}, types.ProcessContext(), Pure, []string{ProcessInvalidContext}},
		{ProcessContextOverride, []types.Parameter{context, {Name: "updates", Type: environment.Type}}, types.ProcessContext(), Pure, []string{ProcessInvalidContext}},
		{ProcessContextDirectory, []types.Parameter{context, directory}, types.ProcessContext(), Pure, []string{ProcessInvalidContext}},
		{ProcessContextEnvironmentValue, []types.Parameter{context, {Name: "name", Type: types.String()}}, types.String(), Pure, []string{ProcessEnvironmentAbsent}},
		{ProcessContextWithout, []types.Parameter{context, {Name: "names", Type: must(types.NewList(types.String()))}}, types.ProcessContext(), Pure, []string{ProcessInvalidContext}},
		{ProcessExecutableResolve, []types.Parameter{context, {Name: "name", Type: types.String()}}, types.ProcessExecutable(), Read, []string{ProcessExecutableAbsent, ProcessDiscoveryFailed}},
	} {
		fixed(entry.name, entry.parameters, must(types.NewFallible(entry.output)))
		op := m[entry.name].(operation)
		op.boundary, op.errors = entry.boundary, entry.errors
		m[entry.name] = op
	}
}
