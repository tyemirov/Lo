package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) tensorBundle(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	bound, d := bindArguments(op.Name(), op.Parameters(), args, span)
	if d != nil {
		return expression{}, d
	}
	values := make([]expression, len(args))
	v, d := c.processorArgument(bound.byPosition[0], nil)
	if d != nil {
		return expression{}, d
	}
	v, d = c.unwrap(v)
	if d != nil {
		return expression{}, d
	}
	values[0] = v
	var out types.Type
	name := op.Name()
	if name == catalog.ParametersNew {
		t, err := types.NewTensorBundle(types.ParametersKind, v.Type())
		if err != nil {
			return expression{}, problem(diagnostic.TypeConstraint, err.Error(), v.Span())
		}
		out = fallibleType(t)
	} else {
		kind := types.GradientsKind
		if name == catalog.ParametersValues || name == catalog.GradientsNew || name == catalog.GradientsZero {
			kind = types.ParametersKind
		}
		t, ok := v.Type().(types.TensorBundle)
		if !ok || t.Kind() != kind {
			return expression{}, diagnostic.Mismatch(string(kind)+"<R>", v.Type().String(), v.Span())
		}
		out = fallibleType(must(types.NewTensorBundle(types.GradientsKind, t.Schema())))
		switch name {
		case catalog.ParametersValues, catalog.GradientsValues:
			out = t.Schema()
		case catalog.GradientsNew, catalog.GradientsAdd, catalog.GradientsScale:
			expected := types.Type(t.Schema())
			if name == catalog.GradientsAdd {
				expected = t
			}
			if name == catalog.GradientsScale {
				expected = types.Float()
			}
			values[1], d = c.processorArgument(bound.byPosition[1], expected)
			if d != nil {
				return expression{}, d
			}
		}
	}
	return boundBuiltin(op, out, bound, values, span), nil
}
