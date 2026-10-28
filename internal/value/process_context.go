package value

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// ProcessContext is an immutable environment and directory, without execution authority.
type ProcessContext interface {
	Value
	Environment() Map
	Directory() Text
	Origin() string
	WithEnvironment(Map) (ProcessContext, error)
	WithoutEnvironment(List) (ProcessContext, error)
	WithDirectory(Text) (ProcessContext, error)
	EnvironmentValue(string) (Text, bool)
	processContext()
}

type processContext struct {
	base
	environment Map
	directory   Text
	origin      string
}

func (processContext) processContext()    {}
func (v processContext) Environment() Map { return v.environment }
func (v processContext) Directory() Text  { return v.directory }
func (v processContext) Origin() string   { return v.origin }
func (v processContext) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Origin   string `json:"origin"`
		Redacted bool   `json:"redacted"`
	}{v.origin, true})
}

// NewProcessContext validates explicit context data without host access.
func NewProcessContext(environment Map, directory Text) (ProcessContext, error) {
	return processContextWithOrigin(environment, directory, "explicit")
}

// NewInheritedProcessContext validates a captured invocation input.
func NewInheritedProcessContext(environment Map, directory Text) (ProcessContext, error) {
	return processContextWithOrigin(environment, directory, "inherited")
}

func processContextWithOrigin(environment Map, directory Text, origin string) (ProcessContext, error) {
	if err := validateProcessEnvironment(environment); err != nil {
		return nil, err
	}
	if err := validateProcessDirectory(directory); err != nil {
		return nil, err
	}
	return newProcessContext(environment, directory, origin), nil
}

func validateProcessDirectory(directory Text) error {
	if directory == nil || !types.Equal(directory.Type(), types.Path()) || !filepath.IsAbs(directory.Text()) || strings.ContainsRune(directory.Text(), 0) {
		return fmt.Errorf("Process context directory must be absolute and contain no NUL")
	}
	return nil
}

func validProcessEnvironmentName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\x00=")
}

func validateProcessEnvironment(environment Map) error {
	if environment == nil || !types.Equal(environment.Type(), must(types.NewMap(types.String(), types.String()))) {
		return fmt.Errorf("Process context requires Map<String, String>")
	}
	for index, entry := range environment.Entries() {
		name := entry.Key.(Text).Text()
		if !validProcessEnvironmentName(name) {
			return fmt.Errorf("Process context environment entry %d has an invalid name", index)
		}
		if strings.ContainsRune(entry.Value.(Text).Text(), 0) {
			return fmt.Errorf("Process context environment entry %d value contains NUL", index)
		}
	}
	return nil
}

func newProcessContext(environment Map, directory Text, origin string) ProcessContext {
	return processContext{newBase(types.ProcessContext(), addWeight(environment.Weight(), directory.Weight())), environment, directory, origin}
}

func (v processContext) EnvironmentValue(name string) (Text, bool) {
	for _, entry := range v.environment.Entries() {
		if entry.Key.(Text).Text() == name {
			return entry.Value.(Text), true
		}
	}
	return nil, false
}

func (v processContext) WithEnvironment(updates Map) (ProcessContext, error) {
	if err := validateProcessEnvironment(updates); err != nil {
		return nil, err
	}
	entries := v.environment.Entries()
	for _, update := range updates.Entries() {
		found := false
		for index, entry := range entries {
			if entry.Key.(Text).Text() == update.Key.(Text).Text() {
				entries[index] = update
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, update)
		}
	}
	environment, err := NewMap(v.environment.Type().(types.Map), entries)
	if err != nil {
		return nil, err
	}
	return newProcessContext(environment, v.directory, "derived"), nil
}

func (v processContext) WithoutEnvironment(names List) (ProcessContext, error) {
	if names == nil || !types.Equal(names.Type(), must(types.NewList(types.String()))) {
		return nil, fmt.Errorf("Process context removals require List<String>")
	}
	removed := map[string]bool{}
	for _, name := range names.Items() {
		if !validProcessEnvironmentName(name.(Text).Text()) {
			return nil, fmt.Errorf("Process context removal has an invalid name")
		}
		removed[name.(Text).Text()] = true
	}
	entries := make([]Entry, 0, len(v.environment.Entries()))
	for _, entry := range v.environment.Entries() {
		if !removed[entry.Key.(Text).Text()] {
			entries = append(entries, entry)
		}
	}
	environment, err := NewMap(v.environment.Type().(types.Map), entries)
	if err != nil {
		return nil, err
	}
	return newProcessContext(environment, v.directory, "derived"), nil
}

func (v processContext) WithDirectory(directory Text) (ProcessContext, error) {
	if err := validateProcessDirectory(directory); err != nil {
		return nil, err
	}
	return newProcessContext(v.environment, directory, "derived"), nil
}

func redactedProcessContext(v ProcessContext) ProcessContext {
	environment := must(NewMap(must(types.NewMap(types.String(), types.String())), nil))
	directory := must(NewText(types.Path(), "/"))
	return newProcessContext(environment, directory, v.Origin())
}
