package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) checkedRemoval(operation catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	if problem := positional(args); problem != nil {
		return expression{}, problem
	}
	if len(args) < 2 {
		return expression{}, problem(diagnostic.ArgumentCount, operation.Name()+" requires entries and a final predicate", span)
	}
	callback, failure := c.flowArgument(args[len(args)-1])
	if failure != nil {
		return expression{}, failure
	}
	parameters := callback.signature.Parameters()
	if len(parameters) != len(args) {
		return expression{}, problem(diagnostic.ArgumentCount, "removal predicate requires contexts, original entry, and current entry", args[len(args)-1].Span)
	}
	parameterTypes := make([]types.Type, len(parameters))
	for index, parameter := range parameters {
		parameterTypes[index] = parameter.Type
	}
	parameterTypes[len(parameters)-2], parameterTypes[len(parameters)-1] = types.FileEntry(), types.FileEntry()
	if failure := c.callback(callback, parameterTypes, types.Bool(), args[len(args)-1].Span); failure != nil {
		return expression{}, failure
	}
	nodes := make([]ir.Argument, len(args))
	values := make([]expression, len(args)-1)
	for index := range values {
		expected := must(types.NewList(types.FileEntry()))
		var expectedType types.Type = expected
		if index > 0 {
			expectedType = parameters[index-1].Type
		}
		value, failure := c.expression(args[index].Value, expectedType)
		if failure != nil {
			return expression{}, failure
		}
		if index == 0 {
			value, failure = c.unwrap(value)
			if failure != nil {
				return expression{}, failure
			}
		}
		values[index] = value
		nodes[index] = must(ir.ValueArg(index, value.node))
	}
	nodes[len(args)-1] = must(ir.FlowArg(len(args)-1, identity.FlowID(callback.entry.ID()), args[len(args)-1].Span))
	return builtinResult(operation, must(types.NewFallible(types.FileRemove())), nodes, nil, span, values...), nil
}
