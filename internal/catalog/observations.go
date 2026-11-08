package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	HostInvocationCurrent = "Host.Invocation.current"
	HostInvocationFailed  = "Host.InvocationFailed"
	ClockNowUnixNS        = "Clock.now_unix_ns"
	ClockFailed           = "Clock.Failed"
	FileSystemUsage       = "File.System.usage"
	FileSystemUsageFailed = "File.System.UsageFailed"
	FileDirectory         = "File.directory"
	FileScan              = "File.scan"
	FileDirectoryScan     = "File.Directory.scan"
	FileDirectoryRefresh  = "File.Directory.refresh"
	FileDirectoryChild    = "File.Directory.child"
	FileDirectoryEntry    = "File.Directory.entry"
	FileEntryObserve      = "File.Entry.observe"
	FileObserveTree       = "File.observe_tree"
	FileRemoveChecked     = "File.Remove.checked"
	DirectoryFailed       = "File.DirectoryFailed"
	DirectoryChanged      = "File.DirectoryChanged"
	EntryAbsent           = "File.EntryAbsent"
)

func addObservations(operations map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	for _, spec := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		errors     []string
	}{
		{HostInvocationCurrent, nil, types.HostInvocation(), []string{HostInvocationFailed}},
		{ClockNowUnixNS, nil, types.Int(), []string{ClockFailed}},
		{FileSystemUsage, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, types.FileSystemUsage(), []string{DirectoryAbsent, FileSystemUsageFailed}},
		{FileDirectory, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, types.FileDirectory(), []string{DirectoryAbsent, DirectoryFailed}},
		{FileScan, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, types.FileScan(), []string{DirectoryAbsent, ListFailed, InvalidName}},
		{FileDirectoryScan, []types.Parameter{{Name: "directory", Type: types.FileDirectory()}}, types.FileScan(), []string{DirectoryChanged, ListFailed, InvalidName}},
		{FileDirectoryRefresh, []types.Parameter{{Name: "directory", Type: types.FileDirectory()}}, types.FileDirectory(), []string{DirectoryChanged, DirectoryFailed}},
		{FileDirectoryChild, []types.Parameter{{Name: "directory", Type: types.FileDirectory()}, {Name: "entry", Type: types.FileEntry()}}, types.FileDirectory(), []string{DirectoryChanged, DirectoryFailed, DirectoryAbsent}},
		{FileDirectoryEntry, []types.Parameter{{Name: "directory", Type: types.FileDirectory()}, {Name: "name", Type: types.String()}}, types.FileEntry(), []string{DirectoryChanged, EntryAbsent, InvalidName, ListFailed}},
		{FileEntryObserve, []types.Parameter{{Name: "entry", Type: types.FileEntry()}}, must(types.NewList(types.FileEntry())), []string{DirectoryChanged, ListFailed}},
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
