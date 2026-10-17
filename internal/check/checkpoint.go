package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) checkpoint(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	bound, d := bindArguments(op.Name(), op.Parameters(), args, span)
	if d != nil {
		return expression{}, d
	}
	values := make([]expression, len(args))
	for position, parameter := range op.Parameters() {
		v, d := c.processorArgument(bound.byPosition[position], parameter.Type)
		if d != nil {
			return expression{}, d
		}
		v, d = c.unwrap(v)
		if d != nil {
			return expression{}, d
		}
		values[position] = v
	}
	position := 0
	if op.Name() == catalog.CheckpointDecode {
		position = 1
	}
	t := values[position].Type()
	valid := t.Kind() == types.ParametersKind || t.Kind() == types.SGDStateKind || t.Kind() == types.AdamWStateKind
	for name, kind := range map[string]types.Kind{catalog.ParametersTo: types.ParametersKind, catalog.SGDStateTo: types.SGDStateKind, catalog.AdamWStateTo: types.AdamWStateKind} {
		if op.Name() == name {
			valid = t.Kind() == kind
		}
	}
	if !valid {
		return expression{}, problem(diagnostic.TypeConstraint, op.Name()+" requires supported Parameters or optimizer state", values[position].Span())
	}
	if op.Name() == catalog.CheckpointEncode {
		t = types.Bytes()
	}
	return boundBuiltin(op, fallibleType(t), bound, values, span), nil
}
