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

func (c *checker) listAccess(op catalog.Operation, args []syntax.Argument, expected types.Type, span source.Span) (expression, diagnostic.Diagnostic) {
	bound, failure := bindArguments(op.Name(), op.Parameters(), args, span)
	if failure != nil {
		return expression{}, failure
	}
	var inputType types.Type
	if expected != nil && unresolvedListLiteral(bound.byPosition[0].Value) {
		if op.Name() == catalog.ListAt {
			inputType = must(types.NewList(expected))
		} else if expected.Kind() == types.ListKind {
			inputType = expected
		}
	}
	values := make([]expression, len(args))
	input, failure := c.processorArgument(bound.byPosition[0], inputType)
	if failure != nil {
		return expression{}, failure
	}
	input, failure = c.asList(input, nil)
	if failure != nil {
		return expression{}, failure
	}
	values[0] = input
	for _, pos := range bound.positions {
		if pos == 0 {
			continue
		}
		value, failure := c.expression(bound.byPosition[pos].Value, types.Int())
		if failure != nil {
			return expression{}, failure
		}
		values[pos] = value
	}
	output := input.Type()
	if op.Name() == catalog.ListAt {
		output = input.Type().(types.List).Element()
	}
	return boundBuiltin(op, fallibleType(output), bound, values, span), nil
}

func unresolvedListLiteral(value syntax.Expression) bool {
	call, ok := value.(syntax.Call)
	if !ok {
		return false
	}
	arguments := call.Arguments()
	switch call.Name() {
	case catalog.ListNew:
		return len(arguments) == 0 || unresolvedListLiteral(arguments[0].Value)
	case catalog.ListAt, catalog.ListSlice:
		return len(arguments) > 0 && unresolvedListLiteral(arguments[0].Value)
	}
	return false
}

func (c *checker) listSort(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	if failure := positional(args); failure != nil {
		return expression{}, failure
	}
	if len(args) < 2 {
		return expression{}, problem(diagnostic.ArgumentCount, "List.sort requires values and a final comparison flow", span)
	}
	callback, failure := c.flowArgument(args[len(args)-1])
	if failure != nil {
		return expression{}, failure
	}
	parameters := callback.signature.Parameters()
	if len(parameters) != len(args) {
		return expression{}, problem(diagnostic.ArgumentCount, "sort comparison requires contexts and two elements", args[len(args)-1].Span)
	}
	parameterTypes := make([]types.Type, len(parameters))
	for index, parameter := range parameters {
		parameterTypes[index] = parameter.Type
	}
	parameterTypes[len(parameters)-1] = parameterTypes[len(parameters)-2]
	if failure := c.callback(callback, parameterTypes, types.Int(), args[len(args)-1].Span); failure != nil {
		return expression{}, failure
	}
	values := make([]expression, len(args)-1)
	nodes := make([]ir.Argument, len(args))
	for index := range values {
		expected := parameterTypes[len(parameterTypes)-1]
		if index == 0 {
			expected = must(types.NewList(expected))
		} else {
			expected = parameterTypes[index-1]
		}
		value, failure := c.processorArgument(args[index], expected)
		if failure != nil {
			return expression{}, failure
		}
		if index == 0 {
			value, failure = c.asList(value, parameterTypes[len(parameterTypes)-1])
			if failure != nil {
				return expression{}, failure
			}
		}
		values[index] = value
		nodes[index] = must(ir.ValueArg(index, value.node))
	}
	nodes[len(args)-1] = must(ir.FlowArg(len(args)-1, identity.FlowID(callback.entry.ID()), args[len(args)-1].Span))
	return builtinResult(op, values[0].Type(), nodes, nil, span, values...), nil
}
