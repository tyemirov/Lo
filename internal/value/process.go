package value

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// ProcessExecutable is a validated executable description without filesystem access.
type ProcessExecutable interface {
	Value
	Path() Text
	processExecutable()
}
type processExecutable struct {
	base
	path Text
}

func (processExecutable) processExecutable() {}
func (v processExecutable) Path() Text       { return v.path }
func (v processExecutable) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Path Text `json:"path"`
	}{v.path})
}

// NewProcessExecutable validates an absolute path without normalization or access.
func NewProcessExecutable(path Text) (ProcessExecutable, error) {
	if path == nil || !types.Equal(path.Type(), types.Path()) || !filepath.IsAbs(path.Text()) || strings.ContainsRune(path.Text(), 0) {
		return nil, fmt.Errorf("Process executable path must be absolute and contain no NUL")
	}
	return processExecutable{newBase(types.ProcessExecutable(), path.Weight()), path}, nil
}

// ProcessCommand keeps every declared input of an inert command.
type ProcessCommand interface {
	Target
	Context() ProcessContext
	Executable() ProcessExecutable
	Arguments() List
	Environment() Map
	Directory() Text
	Stdin() Bytes
	processCommand()
}
type processCommand struct {
	base
	context    ProcessContext
	executable ProcessExecutable
	arguments  List
	stdin      Bytes
}

func (processCommand) target()                         {}
func (processCommand) processCommand()                 {}
func (v processCommand) Executable() ProcessExecutable { return v.executable }
func (v processCommand) Context() ProcessContext       { return v.context }
func (v processCommand) Arguments() List               { return v.arguments }
func (v processCommand) Environment() Map              { return v.context.Environment() }
func (v processCommand) Directory() Text               { return v.context.Directory() }
func (v processCommand) Stdin() Bytes                  { return v.stdin }
func (v processCommand) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Context    ProcessContext    `json:"context"`
		Executable ProcessExecutable `json:"executable"`
		Arguments  List              `json:"arguments"`
		Stdin      Bytes             `json:"stdin"`
	}{v.context, v.executable, v.arguments, v.stdin})
}

// NewProcessCommand validates declared inputs without access or launch.
func NewProcessCommand(context ProcessContext, executable ProcessExecutable, arguments List, stdin Bytes) (ProcessCommand, error) {
	if context == nil || executable == nil || arguments == nil || stdin == nil || !types.Equal(executable.Type(), types.ProcessExecutable()) || !types.Equal(arguments.Type(), must(types.NewList(types.String()))) || !types.Equal(stdin.Type(), types.Bytes()) {
		return nil, fmt.Errorf("Process command requires typed declared inputs")
	}
	for index, argument := range arguments.Items() {
		if strings.ContainsRune(argument.(Text).Text(), 0) {
			return nil, fmt.Errorf("Process argument %d contains NUL", index)
		}
	}
	return newProcessCommand(context, executable, arguments, stdin), nil
}
func newProcessCommand(context ProcessContext, executable ProcessExecutable, arguments List, stdin Bytes) ProcessCommand {
	weight := addWeight(executable.Weight(), arguments.Weight())
	weight = addWeight(weight, context.Weight())
	weight = addWeight(weight, stdin.Weight())
	return processCommand{newBase(types.ProcessCommand(), weight), context, executable, arguments, stdin}
}
func redactedProcessCommand(command ProcessCommand) ProcessCommand {
	arguments := must(NewList(must(types.NewList(types.String())), nil))
	return newProcessCommand(redactedProcessContext(command.Context()), command.Executable(), arguments, NewBytes(nil))
}
