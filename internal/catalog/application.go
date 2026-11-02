package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	ProgramPath             = "Program.path"
	ProgramEntryUnavailable = "Program.EntryUnavailable"
	LockFileNew             = "Lock.File.new"
	LockAcquire             = "Lock.acquire"
	LockBusy                = "Lock.Busy"
	LockFailed              = "Lock.Failed"
	ConsoleLineNew          = "Console.Line.new"
	ConsoleWrite            = "Console.write"
	ConsoleInvalidLine      = "Console.InvalidLine"
	ConsoleWriteFailed      = "Console.WriteFailed"
	ProgramExitNew          = "Program.Exit.new"
	ProgramInvalidExit      = "Program.InvalidExit"
	CLIParse                = "CLI.parse"
	CLIInvalidDefinition    = "CLI.InvalidDefinition"
	CLIInvalidArguments     = "CLI.InvalidArguments"
)

func addApplications(operations map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	fixed(ProgramPath, nil, must(types.NewFallible(types.Path())))
	observed := operations[ProgramPath].(operation)
	observed.boundary = Read
	observed.errors = []string{ProgramEntryUnavailable}
	operations[ProgramPath] = observed
	fixed(LockFileNew, []types.Parameter{{Name: "path", Type: types.Path()}}, types.LockFile())
	target := operations[LockFileNew].(operation)
	target.boundary = Effect
	target.targetPosition = 0
	target.activation = []string{LockBusy, LockFailed}
	operations[LockFileNew] = target

	flag := must(types.NewRecord([]types.Field{
		{Name: "name", Type: types.String()},
		{Name: "aliases", Type: must(types.NewList(types.String()))},
		{Name: "group", Type: types.String()},
		{Name: "help", Type: types.String()},
	}))
	for _, spec := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		errors     []string
	}{
		{ConsoleLineNew, []types.Parameter{{Name: "stream", Type: types.String()}, {Name: "text", Type: types.String()}, {Name: "flush", Type: types.Bool()}}, types.ConsoleLine(), []string{ConsoleInvalidLine}},
		{ProgramExitNew, []types.Parameter{{Name: "code", Type: types.Int()}, {Name: "stdout", Type: types.Bytes()}, {Name: "stderr", Type: types.Bytes()}}, types.ProgramExit(), []string{ProgramInvalidExit}},
		{CLIParse, []types.Parameter{{Name: "arguments", Type: must(types.NewList(types.String()))}, {Name: "flags", Type: must(types.NewList(flag))}, {Name: "name", Type: types.String()}, {Name: "description", Type: types.String()}, {Name: "abbreviations", Type: types.Bool()}}, types.CLIOptions(), []string{CLIInvalidDefinition, CLIInvalidArguments}},
	} {
		fixed(spec.name, spec.parameters, must(types.NewFallible(spec.output)))
		operation := operations[spec.name].(operation)
		operation.errors = spec.errors
		if spec.name == ConsoleLineNew {
			operation.boundary = Effect
			operation.targetPosition = 0
			operation.activation = []string{ConsoleWriteFailed}
		}
		operations[spec.name] = operation
	}
}
