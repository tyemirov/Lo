package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	HostInvocationCurrent = "Host.Invocation.current"
	HostInvocationFailed  = "Host.InvocationFailed"
	FileDirectory         = "File.directory"
	FileScan              = "File.scan"
	FileRemoveChecked     = "File.Remove.checked"
	DirectoryFailed       = "File.DirectoryFailed"
)

func addObservations(operations map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	for _, spec := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		errors     []string
	}{
		{HostInvocationCurrent, nil, types.HostInvocation(), []string{HostInvocationFailed}},
		{FileDirectory, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, types.FileDirectory(), []string{DirectoryAbsent, DirectoryFailed}},
		{FileScan, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, types.FileScan(), []string{DirectoryAbsent, ListFailed, InvalidName}},
	} {
		fixed(spec.name, spec.parameters, must(types.NewFallible(spec.output)))
		operation := operations[spec.name].(operation)
		operation.boundary, operation.errors = Read, spec.errors
		operations[spec.name] = operation
	}
	operations[FileRemoveChecked] = operation{name: FileRemoveChecked, rule: CheckedRemovalConstructor,
		boundary: Effect, output: must(types.NewFallible(types.FileRemove())),
		errors: []string{InvalidSelection}, activation: []string{EntryChanged, RemoveFailed}}
}
