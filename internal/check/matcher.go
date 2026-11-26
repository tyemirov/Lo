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

func (c *checker) matcher(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	bound, failure := bindArguments(op.Name(), op.Parameters(), args, span)
	if failure != nil {
		return expression{}, failure
	}
	values := make([]expression, len(args))
	references := make([]*flowWork, len(args))
	var output types.Type

	// The supplied handler defines the input and result types for either constructor.
	switch op.Name() {
	case catalog.MatchCaseNew, catalog.MatchNew:
		handlerArg := bound.byPosition[1]
		handler, failure := c.flowArgument(handlerArg)
		if failure != nil {
			return expression{}, failure
		}
		parameters := handler.signature.Parameters()
		if len(parameters) != 1 {
			return expression{}, problem(diagnostic.ArgumentCount, "matcher handler requires one parameter", handlerArg.Span)
		}
		input, result := parameters[0].Type, handler.signature.Output()
		handlerEffects := handler.graph.Effects()
		if handlerEffects.Commits {
			return expression{}, problem(diagnostic.EffectfulCallback, "matcher handler "+handler.declaration.Name()+" can activate a commit", handlerArg.Span)
		}
		references[1] = handler
		caseType := must(types.NewMatcher(types.MatchCaseKind, input, result, handlerEffects.Reads))
		first := bound.byPosition[0]
		if op.Name() == catalog.MatchCaseNew {
			predicate, failure := c.flowArgument(first)
			if failure != nil {
				return expression{}, failure
			}
			if failure := c.callback(predicate, []types.Type{input}, types.Bool(), first.Span); failure != nil {
				return expression{}, failure
			}
			references[0] = predicate
			output = caseType
		} else {
			var expectedCases types.Type
			if unresolvedListLiteral(first.Value) {
				expectedCases = must(types.NewList(caseType))
			}
			cases, failure := c.expression(first.Value, expectedCases)
			if failure != nil {
				return expression{}, failure
			}
			cases, failure = c.unwrap(cases)
			if failure != nil {
				return expression{}, failure
			}
			list, ok := cases.Type().(types.List)
			if !ok {
				return expression{}, diagnostic.Mismatch("List<Match.Case>", cases.Type().String(), cases.Span())
			}
			caseSignature, ok := list.Element().(types.Matcher)
			if !ok || caseSignature.Kind() != types.MatchCaseKind || !types.Equal(caseSignature.Input(), input) || !types.Equal(caseSignature.Output(), result) {
				return expression{}, diagnostic.Mismatch(must(types.NewList(caseType)).String(), list.String(), cases.Span())
			}
			values[0] = cases
			output = must(types.NewMatcher(types.MatchKind, input, result, caseSignature.Reads() || handlerEffects.Reads))
		}
	case catalog.MatchApply:
		matcher, failure := c.expression(bound.byPosition[1].Value, nil)
		if failure != nil {
			return expression{}, failure
		}
		matcher, failure = c.unwrap(matcher)
		if failure != nil {
			return expression{}, failure
		}
		signature, ok := matcher.Type().(types.Matcher)
		if !ok || signature.Kind() != types.MatchKind {
			return expression{}, diagnostic.Mismatch("Match", matcher.Type().String(), matcher.Span())
		}
		request, failure := c.expression(bound.byPosition[0].Value, signature.Input())
		if failure != nil {
			return expression{}, failure
		}
		values[0], values[1] = request, matcher
		output = signature.Output()
	default:
		return expression{}, problem(diagnostic.TypeConstraint, "unknown matcher operation", span)
	}

	nodes := make([]ir.Argument, len(args))
	for index, position := range bound.positions {
		if reference := references[position]; reference != nil {
			nodes[index] = must(ir.FlowArg(position, identity.FlowID(reference.entry.ID()), bound.source[index].Span))
		} else {
			nodes[index] = must(ir.ValueArg(position, values[position].node))
		}
	}
	value := builtinResult(op, output, nodes, nil, span, values...)
	if op.Name() == catalog.MatchApply {
		value.effects.Reads = value.effects.Reads || values[1].Type().(types.Matcher).Reads()
	}
	return value, nil
}
