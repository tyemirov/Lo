package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) optimizer(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	bound, d := bindArguments(op.Name(), op.Parameters(), args, span)
	if d != nil {
		return expression{}, d
	}
	v, d := c.processorArgument(bound.byPosition[0], nil)
	if d != nil {
		return expression{}, d
	}
	v, d = c.unwrap(v)
	if d != nil {
		return expression{}, d
	}
	kind := types.SGDStateKind
	config := types.SGDConfig()
	if op.Name() == catalog.AdamWStateNew || op.Name() == catalog.AdamWStep {
		kind = types.AdamWStateKind
		config = types.AdamWConfig()
	}
	var out types.OptimizerState
	var second types.Type
	if op.Name() == catalog.SGDStateNew || op.Name() == catalog.AdamWStateNew {
		p, ok := v.Type().(types.TensorBundle)
		if !ok || p.Kind() != types.ParametersKind {
			return expression{}, diagnostic.Mismatch("Parameters<R>", v.Type().String(), v.Span())
		}
		out = must(types.NewOptimizerState(kind, p.Schema()))
		second = config
	} else {
		s, ok := v.Type().(types.OptimizerState)
		if !ok || s.Kind() != kind {
			return expression{}, diagnostic.Mismatch(string(kind)+"<R>", v.Type().String(), v.Span())
		}
		out = s
		second = must(types.NewTensorBundle(types.GradientsKind, s.Schema()))
	}
	gradient, d := c.processorArgument(bound.byPosition[1], second)
	if d != nil {
		return expression{}, d
	}
	return boundBuiltin(op, fallibleType(out), bound, []expression{v, gradient}, span), nil
}
