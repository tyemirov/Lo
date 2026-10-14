package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) match(e syntax.Match, expected types.Type) (expression, diagnostic.Diagnostic) {
	cases := e.Cases()
	defaults := 0
	for i, arm := range cases {
		if _, ok := arm.(syntax.DefaultCase); ok {
			defaults++
			if i != len(cases)-1 {
				return expression{}, problem(diagnostic.MatchDefault, "match default must be the final case", arm.Span())
			}
		}
	}
	if defaults != 1 {
		return expression{}, problem(diagnostic.MatchDefault, "match requires exactly one default", e.Span())
	}
	selector, d := c.expression(e.Selector(), nil)
	if d != nil {
		return expression{}, d
	}
	selector, d = c.unwrap(selector)
	if d != nil {
		return expression{}, d
	}
	if selector.effects.Commits {
		return expression{}, problem(diagnostic.EffectfulMatch, "match selector must not activate a commit", selector.Span())
	}
	result := expression{}.with(selector)
	branches := make([]expression, len(cases))
	heads := make([]expression, len(cases))
	predicates := make([]identity.FlowID, len(cases))
	var output types.Type
	for i, arm := range cases {
		switch a := arm.(type) {
		case syntax.ExactCase:
			headChecker := *c
			headChecker.failureCode = diagnostic.FallibleCase
			head, d := headChecker.expression(a.Head(), nil)
			if d != nil {
				return expression{}, d
			}
			if head.Type().Kind() == types.FallibleKind || head.propagates {
				return expression{}, problem(diagnostic.FallibleCase, "match case heads must be total", a.Head().Span())
			}
			if head.effects.Commits {
				return expression{}, problem(diagnostic.EffectfulMatch, "match case heads must not activate a commit", a.Head().Span())
			}
			if r, ok := head.Type().(types.Range); ok {
				if !types.Equal(r.Element(), selector.Type()) {
					return expression{}, diagnostic.Mismatch(selector.Type().String(), r.Element().String(), head.Span())
				}
			} else {
				if !types.Comparable(head.Type()) {
					return expression{}, problem(diagnostic.TypeConstraint, "exact case head must be equality-comparable", head.Span())
				}
				if !types.Equal(head.Type(), selector.Type()) {
					return expression{}, diagnostic.Mismatch(selector.Type().String(), head.Type().String(), head.Span())
				}
			}
			heads[i] = head
			result = result.with(head)
		case syntax.PredicateCase:
			f, d := c.namedFlow(a.Flow(), a.Span())
			if d != nil {
				return expression{}, d
			}
			if d := c.callback(f, []types.Type{selector.Type()}, types.Bool(), a.Span()); d != nil {
				return expression{}, d
			}
			predicates[i] = identity.FlowID(f.entry.ID())
			result.effects = result.effects.With(f.graph.Effects())
		case syntax.DefaultCase:
		default:
			panic("unknown match case")
		}
		branch, d := c.expression(arm.Body(), expected)
		if d != nil {
			return expression{}, d
		}
		if branch.effects.Commits {
			return expression{}, problem(diagnostic.EffectfulMatch, "match branches must not activate a commit", branch.Span())
		}
		branches[i] = branch
		result = result.with(branch)
		if output == nil {
			output = branch.Type()
		} else if !types.Equal(output, branch.Type()) {
			if !types.Equal(baseType(output), baseType(branch.Type())) {
				return expression{}, diagnostic.Mismatch(output.String(), branch.Type().String(), branch.Span())
			}
			output = must(types.NewFallible(baseType(output)))
		}
	}
	graphCases := make([]ir.Case, len(cases))
	for i, arm := range cases {
		branch, d := c.coerce(branches[i], output)
		if d != nil {
			return expression{}, d
		}
		switch arm.(type) {
		case syntax.ExactCase:
			graphCases[i] = must(ir.NewExactCase(heads[i].node, branch.node, arm.Span()))
		case syntax.PredicateCase:
			graphCases[i] = must(ir.NewPredicateCase(predicates[i], branch.node, arm.Span()))
		case syntax.DefaultCase:
			graphCases[i] = must(ir.NewDefaultCase(branch.node, arm.Span()))
		}
	}
	result.node = must(ir.NewMatch(output, selector.node, graphCases, e.Span()))
	return result, nil
}
