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
	Executable() ProcessExecutable
	Arguments() List
	Environment() Map
	Directory() Text
	Stdin() Bytes
	processCommand()
}
type processCommand struct {
	base
	executable  ProcessExecutable
	arguments   List
	environment Map
	directory   Text
	stdin       Bytes
}

func (processCommand) target()                         {}
func (processCommand) processCommand()                 {}
func (v processCommand) Executable() ProcessExecutable { return v.executable }
func (v processCommand) Arguments() List               { return v.arguments }
func (v processCommand) Environment() Map              { return v.environment }
func (v processCommand) Directory() Text               { return v.directory }
func (v processCommand) Stdin() Bytes                  { return v.stdin }
func (v processCommand) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Executable  ProcessExecutable `json:"executable"`
		Arguments   List              `json:"arguments"`
		Environment Map               `json:"environment"`
		Directory   Text              `json:"directory"`
		Stdin       Bytes             `json:"stdin"`
	}{v.executable, v.arguments, v.environment, v.directory, v.stdin})
}

// NewProcessCommand validates declared inputs without access or launch.
func NewProcessCommand(executable ProcessExecutable, arguments List, environment Map, directory Text, stdin Bytes) (ProcessCommand, error) {
	if executable == nil || arguments == nil || environment == nil || directory == nil || stdin == nil || !types.Equal(executable.Type(), types.ProcessExecutable()) || !types.Equal(arguments.Type(), must(types.NewList(types.String()))) || !types.Equal(environment.Type(), must(types.NewMap(types.String(), types.String()))) || !types.Equal(directory.Type(), types.Path()) || !types.Equal(stdin.Type(), types.Bytes()) {
		return nil, fmt.Errorf("Process command requires typed declared inputs")
	}
	if !filepath.IsAbs(directory.Text()) || strings.ContainsRune(directory.Text(), 0) {
		return nil, fmt.Errorf("Process directory must be absolute and contain no NUL")
	}
	for index, argument := range arguments.Items() {
		if strings.ContainsRune(argument.(Text).Text(), 0) {
			return nil, fmt.Errorf("Process argument %d contains NUL", index)
		}
	}
	for index, entry := range environment.Entries() {
		name := entry.Key.(Text).Text()
		if name == "" || strings.ContainsAny(name, "\x00=") {
			return nil, fmt.Errorf("Process environment entry %d has an invalid name", index)
		}
		if strings.ContainsRune(entry.Value.(Text).Text(), 0) {
			return nil, fmt.Errorf("Process environment entry %d value contains NUL", index)
		}
	}
	return newProcessCommand(executable, arguments, environment, directory, stdin), nil
}
func newProcessCommand(executable ProcessExecutable, arguments List, environment Map, directory Text, stdin Bytes) ProcessCommand {
	weight := addWeight(executable.Weight(), arguments.Weight())
	weight = addWeight(weight, environment.Weight())
	weight = addWeight(weight, directory.Weight())
	weight = addWeight(weight, stdin.Weight())
	return processCommand{newBase(types.ProcessCommand(), weight), executable, arguments, environment, directory, stdin}
}
func redactedProcessCommand(command ProcessCommand) ProcessCommand {
	arguments := must(NewList(must(types.NewList(types.String())), nil))
	environment := must(NewMap(must(types.NewMap(types.String(), types.String())), nil))
	return newProcessCommand(command.Executable(), arguments, environment, command.Directory(), NewBytes(nil))
}
