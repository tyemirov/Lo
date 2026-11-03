package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	StringStartsWith            = "String.starts_with"
	StringEndsWith              = "String.ends_with"
	StringSlice                 = "String.slice"
	StringCompare               = "String.compare"
	StringSplit                 = "String.split"
	StringSplitFirst            = "String.split_first"
	StringReplaceAll            = "String.replace_all"
	StringTrim                  = "String.trim"
	StringIsWhitespace          = "String.is_whitespace"
	StringSplitWhitespace       = "String.split_whitespace"
	StringLines                 = "String.lines"
	BytesLength                 = "Bytes.length"
	BytesStartsWith             = "Bytes.starts_with"
	BytesSlice                  = "Bytes.slice"
	BytesSplit                  = "Bytes.split"
	BytesTrimStart              = "Bytes.trim_start"
	ListAt                      = "List.at"
	ListSlice                   = "List.slice"
	ListSort                    = "List.sort"
	FilePathToString            = "File.Path.to_string"
	FilePathJoin                = "File.Path.join"
	FilePathParent              = "File.Path.parent"
	FilePathName                = "File.Path.name"
	GlobNew                     = "Glob.new"
	GlobMatches                 = "Glob.matches"
	ProcessArgumentsFormatShell = "Process.Arguments.format_shell"
	StringInvalidBounds         = "String.InvalidBounds"
	StringInvalidSeparator      = "String.InvalidSeparator"
	StringSeparatorAbsent       = "String.SeparatorAbsent"
	BytesInvalidBounds          = "Bytes.InvalidBounds"
	BytesInvalidSeparator       = "Bytes.InvalidSeparator"
	ListInvalidIndex            = "List.InvalidIndex"
	ListInvalidBounds           = "List.InvalidBounds"
	FileInvalidPath             = "File.InvalidPath"
	GlobInvalidPattern          = "Glob.InvalidPattern"
	ProcessInvalidArguments     = "Process.InvalidArguments"
)

func addProcessors(operations map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	parameter := func(name string, typ types.Type) types.Parameter { return types.Parameter{Name: name, Type: typ} }
	text, data, integer := types.String(), types.Bytes(), types.Int()
	for _, spec := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		errors     []string
	}{
		{StringStartsWith, []types.Parameter{parameter("text", text), parameter("prefix", text)}, types.Bool(), nil},
		{StringEndsWith, []types.Parameter{parameter("text", text), parameter("suffix", text)}, types.Bool(), nil},
		{StringSlice, []types.Parameter{parameter("text", text), parameter("start", integer), parameter("end", integer)}, text, []string{StringInvalidBounds}},
		{StringCompare, []types.Parameter{parameter("left", text), parameter("right", text)}, integer, nil},
		{StringSplit, []types.Parameter{parameter("text", text), parameter("separator", text)}, must(types.NewList(text)), []string{StringInvalidSeparator}},
		{StringSplitFirst, []types.Parameter{parameter("text", text), parameter("separator", text)}, must(types.NewRecord([]types.Field{{Name: "before", Type: text}, {Name: "after", Type: text}})), []string{StringInvalidSeparator, StringSeparatorAbsent}},
		{StringReplaceAll, []types.Parameter{parameter("text", text), parameter("old", text), parameter("replacement", text)}, text, []string{StringInvalidSeparator}},
		{StringTrim, []types.Parameter{parameter("text", text)}, text, nil},
		{StringIsWhitespace, []types.Parameter{parameter("text", text)}, types.Bool(), nil},
		{StringSplitWhitespace, []types.Parameter{parameter("text", text)}, must(types.NewList(text)), nil},
		{StringLines, []types.Parameter{parameter("text", text)}, must(types.NewList(text)), nil},
		{BytesLength, []types.Parameter{parameter("bytes", data)}, integer, nil},
		{BytesStartsWith, []types.Parameter{parameter("bytes", data), parameter("prefix", data)}, types.Bool(), nil},
		{BytesSlice, []types.Parameter{parameter("bytes", data), parameter("start", integer), parameter("end", integer)}, data, []string{BytesInvalidBounds}},
		{BytesSplit, []types.Parameter{parameter("bytes", data), parameter("separator", data)}, must(types.NewList(data)), []string{BytesInvalidSeparator}},
		{BytesTrimStart, []types.Parameter{parameter("bytes", data), parameter("prefix", data)}, data, []string{BytesInvalidSeparator}},
		{FilePathToString, []types.Parameter{parameter("path", types.Path())}, text, nil},
		{FilePathJoin, []types.Parameter{parameter("path", types.Path()), parameter("relative", text)}, types.Path(), []string{FileInvalidPath}},
		{FilePathParent, []types.Parameter{parameter("path", types.Path())}, types.Path(), nil},
		{FilePathName, []types.Parameter{parameter("path", types.Path())}, text, nil},
		{GlobNew, []types.Parameter{parameter("pattern", text)}, types.Glob(), []string{GlobInvalidPattern}},
		{GlobMatches, []types.Parameter{parameter("pattern", types.Glob()), parameter("text", text)}, types.Bool(), nil},
		{ProcessArgumentsFormatShell, []types.Parameter{parameter("arguments", must(types.NewList(text)))}, text, []string{ProcessInvalidArguments}},
	} {
		output := spec.output
		if len(spec.errors) > 0 {
			output = must(types.NewFallible(output))
		}
		fixed(spec.name, spec.parameters, output)
		operation := operations[spec.name].(operation)
		operation.errors = spec.errors
		operations[spec.name] = operation
	}
	operations[ListAt] = operation{name: ListAt, rule: ListAccess, parameters: []types.Parameter{{Name: "values"}, {Name: "index", Type: integer}}, errors: []string{ListInvalidIndex}}
	operations[ListSlice] = operation{name: ListSlice, rule: ListAccess, parameters: []types.Parameter{{Name: "values"}, {Name: "start", Type: integer}, {Name: "end", Type: integer}}, errors: []string{ListInvalidBounds}}
	operations[ListSort] = operation{name: ListSort, rule: ListSortProcessor}
}
