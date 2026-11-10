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

func (c *checker) filePredicate(operation catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	if problem := positional(args); problem != nil {
		return expression{}, problem
	}
	inputs := operation.Parameters()
	if len(args) < len(inputs)+1 {
		return expression{}, problem(diagnostic.ArgumentCount, operation.Name()+" requires its observation inputs and a final predicate", span)
	}
	callback, failure := c.flowArgument(args[len(args)-1])
	if failure != nil {
		return expression{}, failure
	}
	parameters := callback.signature.Parameters()
	elements := catalog.FilePredicateElements(operation.Name())
	contexts := len(args) - len(inputs) - 1
	if len(parameters) != contexts+len(elements) {
		return expression{}, problem(diagnostic.ArgumentCount, operation.Name()+" predicate requires contexts followed by the observed values", args[len(args)-1].Span)
	}
	parameterTypes := make([]types.Type, len(parameters))
	for index, parameter := range parameters {
		parameterTypes[index] = parameter.Type
	}
	copy(parameterTypes[contexts:], elements)
	if failure := c.callback(callback, parameterTypes, types.Bool(), args[len(args)-1].Span); failure != nil {
		return expression{}, failure
	}
	nodes := make([]ir.Argument, len(args))
	values := make([]expression, len(args)-1)
	for index := range values {
		var expectedType types.Type
		if index < len(inputs) {
			expectedType = inputs[index].Type
		} else {
			expectedType = parameters[index-len(inputs)].Type
		}
		value, failure := c.expression(args[index].Value, expectedType)
		if failure != nil {
			return expression{}, failure
		}
		if index < len(inputs) {
			value, failure = c.unwrap(value)
			if failure != nil {
				return expression{}, failure
			}
		}
		values[index] = value
		nodes[index] = must(ir.ValueArg(index, value.node))
	}
	nodes[len(args)-1] = must(ir.FlowArg(len(args)-1, identity.FlowID(callback.entry.ID()), args[len(args)-1].Span))
	return builtinResult(operation, operation.Output(), nodes, nil, span, values...), nil
}
