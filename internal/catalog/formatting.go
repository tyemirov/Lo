package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	IntParse                = "Int.parse"
	FloatFormatFixed        = "Float.format_fixed"
	StringFormat            = "String.format"
	IntInvalidText          = "Int.InvalidText"
	NumericInvalidPrecision = "Numeric.InvalidPrecision"
	StringInvalidFormat     = "String.InvalidFormat"
)

func addFormatting(operations map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	for _, spec := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		errorKind  string
	}{
		{IntParse, []types.Parameter{{Name: "text", Type: types.String()}}, types.Int(), IntInvalidText},
		{FloatFormatFixed, []types.Parameter{{Name: ParamValue, Type: types.Float()}, {Name: "places", Type: types.Int()}}, types.String(), NumericInvalidPrecision},
		{StringFormat, []types.Parameter{{Name: "template", Type: types.String()}, {Name: "fields", Type: types.AnyRecord()}}, types.String(), StringInvalidFormat},
	} {
		fixed(spec.name, spec.parameters, must(types.NewFallible(spec.output)))
		definition := operations[spec.name].(operation)
		definition.errors = []string{spec.errorKind}
		if spec.name == StringFormat {
			definition.rule = StringFormatOperation
		}
		operations[spec.name] = definition
	}
}

// AcceptsFormatFields reports whether a concrete record contains only total formatting scalars.
func AcceptsFormatFields(typ types.Type) bool {
	record, ok := typ.(types.Record)
	if !ok {
		return false
	}
	for _, field := range record.Fields() {
		switch field.Type.Kind() {
		case types.StringKind, types.IntKind, types.FloatKind, types.BoolKind:
		default:
			return false
		}
	}
	return true
}
