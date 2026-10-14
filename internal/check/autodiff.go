package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) gradient(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	if d := positional(args); d != nil {
		return expression{}, d
	}
	if len(args) < 2 {
		return expression{}, problem(diagnostic.ArgumentCount, "Autodiff.grad requires targets, explicit context, and a named loss flow", span)
	}
	f, d := c.flowArgument(args[len(args)-1])
	if d != nil {
		return expression{}, d
	}
	parameters := f.signature.Parameters()
	if len(parameters) != len(args)-1 {
		return expression{}, problem(diagnostic.ArgumentCount, "loss flow must accept context followed by targets", span)
	}
	r, ok := parameters[len(parameters)-1].Type.(types.Record)
	if !ok || len(r.Fields()) == 0 {
		return expression{}, problem(diagnostic.TypeConstraint, "autodiff targets require a nonempty exact tensor record", args[0].Span)
	}
	for _, field := range r.Fields() {
		if !types.Equal(field.Type, types.Tensor()) {
			return expression{}, problem(diagnostic.TypeConstraint, "autodiff target fields require Tensor<FP32>", args[0].Span)
		}
	}
	if !types.Equal(baseType(f.signature.Output()), types.Tensor()) {
		return expression{}, problem(diagnostic.AutodiffLoss, "loss flow must return Tensor<FP32> or Tensor<FP32>!", args[len(args)-1].Span)
	}
	values := make([]expression, len(args)-1)
	nodes := make([]ir.Argument, len(args))
	for i, arg := range args[:len(args)-1] {
		t := types.Type(r)
		if i > 0 {
			t = parameters[i-1].Type
		}
		v, d := c.processorArgument(arg, t)
		if d != nil {
			return expression{}, d
		}
		values[i] = v
		nodes[i] = must(ir.ValueArg(i, v.node))
	}
	nodes[len(args)-1] = must(ir.FlowArg(len(args)-1, identity.FlowID(f.entry.ID()), args[len(args)-1].Span))
	out := must(types.NewRecord([]types.Field{{Name: catalog.ParamValue, Type: types.Tensor()}, {Name: catalog.FieldGradients, Type: r}}))
	result := builtinResult(op, fallibleType(out), nodes, nil, span, values...)
	flows := map[identity.FlowID]ir.Flow{}
	for _, flow := range c.module.order {
		if flow.graph != nil {
			flows[flow.graph.ID()] = flow.graph
		}
	}
	call, d := autodiff.Compile(result.node.(ir.BuiltinCall), f.graph, flows)
	if d != nil {
		return expression{}, d
	}
	result.node = call
	return result, nil
}
