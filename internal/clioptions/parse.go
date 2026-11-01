// Package clioptions implements pure declared Boolean option parsing.
package clioptions

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	InvalidDefinition = "CLI.InvalidDefinition"
	InvalidArguments  = "CLI.InvalidArguments"
	helpShort         = "-h"
	helpLong          = "--help"
	helpRow           = "  -h, --help  Show this help message and exit.\n"
)

// Flag declares a canonical Boolean option and its spellings.
type Flag struct {
	Name    string
	Aliases []string
	Group   string
	Help    string
}

// Option is one canonical selection in declaration order.
type Option struct {
	Name     string
	Selected bool
}

// Options contains public selections and declared help without raw arguments.
type Options struct {
	Flags         []Option
	HelpRequested bool
	HelpText      string
}

// Failure reports sanitized declaration or argument failure evidence.
type Failure struct {
	kind       string
	reason     string
	index      int
	candidates []string
}

func (f *Failure) Error() string  { return "declared option parsing failed" }
func (f *Failure) Kind() string   { return f.kind }
func (f *Failure) Reason() string { return f.reason }
func (f *Failure) Index() int     { return f.index }
func (f *Failure) Candidates() []string {
	return append([]string{}, f.candidates...)
}

type spelling struct {
	text  string
	owner int // Zero is the reserved help option; declarations start at one.
}

type definition struct {
	flags         []Flag
	exact         map[string]int
	long          []spelling
	help          string
	abbreviations bool
}

func failure(kind, reason string, index int, candidates ...string) error {
	return &Failure{kind: kind, reason: reason, index: index, candidates: append([]string{}, candidates...)}
}

func validText(text string, step func() error) (bool, error) {
	if !utf8.ValidString(text) {
		return false, nil
	}
	for _, scalar := range text {
		if err := step(); err != nil {
			return false, err
		}
		if unicode.IsControl(scalar) {
			return false, nil
		}
	}
	return true, nil
}

func asciiLetterDigit(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

func validSpelling(text string, step func() error) (bool, error) {
	for index := range len(text) {
		if err := step(); err != nil {
			return false, err
		}
		character := text[index]
		switch {
		case index == 0:
			if character != '-' {
				return false, nil
			}
		case len(text) == 2:
			if !asciiLetterDigit(character) {
				return false, nil
			}
		case index == 1:
			if character != '-' {
				return false, nil
			}
		case index == 2 || index == len(text)-1:
			if !asciiLetterDigit(character) {
				return false, nil
			}
		default:
			if !asciiLetterDigit(character) && character != '-' {
				return false, nil
			}
		}
	}
	return len(text) >= 2 && text != "--", nil
}

func compile(flags []Flag, name, description string, abbreviations bool, step func() error) (definition, error) {
	result := definition{exact: map[string]int{helpShort: 0, helpLong: 0}, long: []spelling{{helpLong, 0}}, abbreviations: abbreviations}
	for _, item := range []struct{ text, reason string }{{name, "invalid-name"}, {description, "invalid-description"}} {
		valid, err := validText(item.text, step)
		if err != nil {
			return definition{}, err
		}
		if !valid || (item.reason == "invalid-name" && item.text == "") {
			return definition{}, failure(InvalidDefinition, item.reason, -1)
		}
	}
	var help strings.Builder
	help.WriteString("Usage: ")
	help.WriteString(name)
	help.WriteString(" [OPTIONS]\n\n")
	if description != "" {
		help.WriteString(description)
		help.WriteString("\n\n")
	}
	help.WriteString("Options:\n")
	help.WriteString(helpRow)
	for index, input := range flags {
		if err := step(); err != nil {
			return definition{}, err
		}
		flag := Flag{Name: input.Name, Aliases: append([]string{}, input.Aliases...), Group: input.Group, Help: input.Help}
		for _, item := range []struct{ text, reason string }{{flag.Group, "invalid-group"}, {flag.Help, "invalid-help"}} {
			valid, err := validText(item.text, step)
			if err != nil {
				return definition{}, err
			}
			if !valid {
				return definition{}, failure(InvalidDefinition, item.reason, index)
			}
		}
		spellings := append([]string{flag.Name}, flag.Aliases...)
		for alias, text := range spellings {
			valid, err := validSpelling(text, step)
			if err != nil {
				return definition{}, err
			}
			if !valid {
				reason := "invalid-flag-name"
				if alias != 0 {
					reason = "invalid-alias"
				}
				return definition{}, failure(InvalidDefinition, reason, index)
			}
			if text == helpShort || text == helpLong {
				return definition{}, failure(InvalidDefinition, "reserved-spelling", index)
			}
			if _, exists := result.exact[text]; exists {
				return definition{}, failure(InvalidDefinition, "duplicate-spelling", index)
			}
			result.exact[text] = index + 1
			if strings.HasPrefix(text, "--") {
				result.long = append(result.long, spelling{text, index + 1})
			}
		}
		result.flags = append(result.flags, flag)
		help.WriteString("  ")
		help.WriteString(strings.Join(spellings, ", "))
		help.WriteString("  ")
		help.WriteString(flag.Help)
		help.WriteByte('\n')
	}
	result.help = help.String()
	return result, nil
}

func (d definition) candidates(owners []int) []string {
	selected := make(map[int]bool, len(owners))
	for _, owner := range owners {
		selected[owner] = true
	}
	result := []string{}
	if selected[0] {
		result = append(result, helpLong)
	}
	for index, flag := range d.flags {
		if selected[index+1] {
			result = append(result, flag.Name)
		}
	}
	return result
}

func (d definition) resolve(argument string, index int, step func() error) (int, error) {
	if owner, exists := d.exact[argument]; exists {
		return owner, nil
	}
	if d.abbreviations && strings.HasPrefix(argument, "--") {
		owners := []int{}
		seen := map[int]bool{}
		for _, candidate := range d.long {
			if err := step(); err != nil {
				return 0, err
			}
			if strings.HasPrefix(candidate.text, argument) && !seen[candidate.owner] {
				seen[candidate.owner] = true
				owners = append(owners, candidate.owner)
			}
		}
		if len(owners) == 1 {
			return owners[0], nil
		}
		if len(owners) > 1 {
			return 0, failure(InvalidArguments, "ambiguous-option", index, d.candidates(owners)...)
		}
	}
	return 0, failure(InvalidArguments, "unknown-option", index)
}

// Parse validates declarations, then parses tokens with caller-owned work accounting.
// The required step callback propagates cancellation and work failures unchanged.
func Parse(arguments []string, flags []Flag, name, description string, abbreviations bool, step func() error) (Options, error) {
	if step == nil {
		return Options{}, errors.New("CLI parser requires work accounting")
	}
	definition, err := compile(flags, name, description, abbreviations, step)
	if err != nil {
		return Options{}, err
	}
	result := Options{Flags: make([]Option, len(definition.flags)), HelpText: definition.help}
	for index, flag := range definition.flags {
		result.Flags[index] = Option{Name: flag.Name}
	}
	groups := map[string]int{}
	for index, argument := range arguments {
		if err := step(); err != nil {
			return Options{}, err
		}
		valid, err := validText(argument, step)
		if err != nil {
			return Options{}, err
		}
		if !valid {
			return Options{}, failure(InvalidArguments, "invalid-argument", index)
		}
		valid, err = validSpelling(argument, step)
		if err != nil {
			return Options{}, err
		}
		if !valid {
			return Options{}, failure(InvalidArguments, "unsupported-token", index)
		}
		owner, err := definition.resolve(argument, index, step)
		if err != nil {
			return Options{}, err
		}
		if owner == 0 {
			result.HelpRequested = true
			return result, nil
		}
		flag := definition.flags[owner-1]
		if flag.Group != "" {
			if previous, exists := groups[flag.Group]; exists && previous != owner {
				return Options{}, failure(InvalidArguments, "exclusive-group", index, definition.candidates([]int{previous, owner})...)
			}
			groups[flag.Group] = owner
		}
		result.Flags[owner-1].Selected = true
	}
	return result, nil
}
