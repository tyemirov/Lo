package runtimeops

import (
	"errors"
	"math/big"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/clioptions"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// ConsoleLineNew constructs an inert target without retaining a host sink.
func ConsoleLineNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	line, err := value.NewConsoleLine(args[0].(value.Text).Text(), args[1].(value.Text).Text(), args[2].(value.Boolean).Bool())
	if err != nil {
		return OperationFailure(call, nil, catalog.ConsoleInvalidLine, err.Error()), nil
	}
	return Success(call.Type(), line), nil
}

// CLIParse publishes declared selections without retaining the argument vector.
func CLIParse(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	arguments := make([]string, 0, args[0].(value.List).Len())
	for _, item := range args[0].(value.List).Items() {
		if err := session.Step(); err != nil {
			return nil, err
		}
		arguments = append(arguments, item.(value.Text).Text())
	}
	flags := make([]clioptions.Flag, 0, args[1].(value.List).Len())
	for _, item := range args[1].(value.List).Items() {
		if err := session.Step(); err != nil {
			return nil, err
		}
		fields := item.(value.Record)
		name := fields.Field("name")
		aliases := fields.Field("aliases")
		group := fields.Field("group")
		help := fields.Field("help")
		flag := clioptions.Flag{Name: name.(value.Text).Text(), Group: group.(value.Text).Text(), Help: help.(value.Text).Text()}
		for _, alias := range aliases.(value.List).Items() {
			if err := session.Step(); err != nil {
				return nil, err
			}
			flag.Aliases = append(flag.Aliases, alias.(value.Text).Text())
		}
		flags = append(flags, flag)
	}
	options, err := clioptions.Parse(arguments, flags, args[2].(value.Text).Text(), args[3].(value.Text).Text(), args[4].(value.Boolean).Bool(), session.Step)
	if err != nil {
		var failure *clioptions.Failure
		if !errors.As(err, &failure) {
			return nil, err
		}
		candidates := make([]value.Value, 0, len(failure.Candidates()))
		for _, name := range failure.Candidates() {
			candidates = append(candidates, must(value.NewText(types.String(), name)))
		}
		input := must(value.NewRecord([]value.Field{
			{Name: "reason", Value: must(value.NewText(types.String(), failure.Reason()))},
			{Name: "index", Value: must(value.NewInt(big.NewInt(int64(failure.Index()))))},
			{Name: "candidates", Value: must(value.NewList(must(types.NewList(types.String())), candidates))},
		}))
		problem := value.At(must(value.NewError(failure.Kind(), "declared option parsing failed", input)), catalog.CLIParse, call.Span())
		return must(value.Failure(call.Type().(types.Fallible), problem)), nil
	}
	entries := make([]value.Entry, 0, len(options.Flags))
	for _, flag := range options.Flags {
		if err := session.Step(); err != nil {
			return nil, err
		}
		entries = append(entries, value.Entry{Key: must(value.NewText(types.String(), flag.Name)), Value: value.Bool(flag.Selected)})
	}
	fields := must(value.NewRecord([]value.Field{
		{Name: "flags", Value: must(value.NewMap(must(types.NewMap(types.String(), types.Bool())), entries))},
		{Name: "help_requested", Value: value.Bool(options.HelpRequested)},
		{Name: "help_text", Value: must(value.NewText(types.String(), options.HelpText))},
	}))
	return Success(call.Type(), must(value.NewCLIOptions(fields))), nil
}

// ProgramExitNew constructs terminal data without selecting a process status.
func ProgramExitNew(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	terminal, err := value.NewProgramExit(args[0].(value.Integer), args[1].(value.Bytes), args[2].(value.Bytes))
	if err != nil {
		return OperationFailure(call, nil, catalog.ProgramInvalidExit, err.Error()), nil
	}
	return Success(call.Type(), terminal), nil
}
