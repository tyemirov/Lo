package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	JSONDecode        = "JSON.decode"
	JSONGet           = "JSON.get"
	JSONAt            = "JSON.at"
	JSONString        = "JSON.string"
	JSONBool          = "JSON.bool"
	JSONInt           = "JSON.int"
	JSONFloat         = "JSON.float"
	JSONKind          = "JSON.kind"
	JSONLength        = "JSON.length"
	JSONInvalidData   = "JSON.InvalidData"
	JSONTypeMismatch  = "JSON.TypeMismatch"
	JSONFieldAbsent   = "JSON.FieldAbsent"
	JSONInvalidIndex  = "JSON.InvalidIndex"
	JSONInvalidNumber = "JSON.InvalidNumber"
)

func addJSON(operations map[string]Operation, fixed func(string, []types.Parameter, types.Type)) {
	value := types.Parameter{Name: ParamValue, Type: types.JSONValue()}
	for _, spec := range []struct {
		name       string
		parameters []types.Parameter
		output     types.Type
		errors     []string
	}{
		{JSONDecode, []types.Parameter{{Name: "data", Type: types.Bytes()}}, types.JSONValue(), []string{JSONInvalidData}},
		{JSONGet, []types.Parameter{value, {Name: "key", Type: types.String()}}, types.JSONValue(), []string{JSONTypeMismatch, JSONFieldAbsent}},
		{JSONAt, []types.Parameter{value, {Name: "index", Type: types.Int()}}, types.JSONValue(), []string{JSONTypeMismatch, JSONInvalidIndex}},
		{JSONString, []types.Parameter{value}, types.String(), []string{JSONTypeMismatch}},
		{JSONBool, []types.Parameter{value}, types.Bool(), []string{JSONTypeMismatch}},
		{JSONInt, []types.Parameter{value}, types.Int(), []string{JSONTypeMismatch, JSONInvalidNumber}},
		{JSONFloat, []types.Parameter{value}, types.Float(), []string{JSONTypeMismatch, JSONInvalidNumber}},
		{JSONKind, []types.Parameter{value}, types.String(), nil},
		{JSONLength, []types.Parameter{value}, types.Int(), []string{JSONTypeMismatch}},
	} {
		output := spec.output
		if len(spec.errors) > 0 {
			output = must(types.NewFallible(output))
		}
		fixed(spec.name, spec.parameters, output)
		definition := operations[spec.name].(operation)
		definition.errors = spec.errors
		operations[spec.name] = definition
	}
}
