package inspect

import (
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (a *analyzer) loopMatcherState(c *context, node nodeID, seed abstract) abstract {
	state := a.loopState(c, node, seed.typ)
	if !containsMatcher(seed.typ) {
		return state
	}
	state.matcherRules = seed.matcherRules
	state.matcherKnown = seed.matcherKnown
	shape := seed.typ
	if fallible, ok := shape.(types.Fallible); ok {
		shape = fallible.Base()
	}
	switch collection := shape.(type) {
	case types.Record:
		for _, field := range collection.Fields() {
			if prior, ok := seed.fields[field.Name]; ok {
				state.fields[field.Name] = a.loopMatcherState(c, node, prior)
			}
		}
	case types.List:
		if seed.items != nil {
			state.items = []abstract{}
			if len(seed.items) > 0 {
				item := a.join(seed.items, node, collection.Element())
				state.items = append(state.items, a.loopMatcherState(c, node, item))
			}
		}
	case types.Map:
		if seed.items != nil {
			state.items = []abstract{}
			if len(seed.items) > 0 {
				entryType, _ := types.NewRecord([]types.Field{{Name: "key", Type: collection.Key()}, {Name: "value", Type: collection.Value()}})
				item := a.join(seed.items, node, entryType)
				state.items = append(state.items, a.loopMatcherState(c, node, item))
			}
		}
	}
	return state
}

func containsMatcher(typ types.Type) bool {
	switch shape := typ.(type) {
	case types.Matcher:
		return true
	case types.Fallible:
		return containsMatcher(shape.Base())
	case types.Record:
		for _, field := range shape.Fields() {
			if containsMatcher(field.Type) {
				return true
			}
		}
	case types.List:
		return containsMatcher(shape.Element())
	case types.Map:
		return containsMatcher(shape.Value())
	}
	return false
}

func (a *analyzer) refineMatcherState(current *abstract, node nodeID, next []abstract) bool {
	if len(next) == 0 {
		return false
	}
	possible := append([]abstract{*current}, next...)
	state := a.join(possible, node, current.typ)
	if matcherStateSignature(state) == matcherStateSignature(*current) {
		return false
	}
	*current = state
	return true
}

// Callback identities form a finite state set. Invocation conditions describe
// their use sites and do not create a new possible matcher on each iteration.
func matcherStateSignature(value abstract) string {
	parts := map[string]bool{}
	var collect func(abstract, string)
	collect = func(value abstract, path string) {
		if value.typ == nil {
			return
		}
		shape := value.typ
		if fallible, ok := shape.(types.Fallible); ok {
			shape = fallible.Base()
		}
		if _, ok := shape.(types.Matcher); ok {
			key := path + ":" + shape.String()
			if !value.matcherKnown {
				parts[key+":unknown"] = true
			}
			for _, rule := range value.matcherRules {
				parts[key+":"+string(rule.predicate)+":"+string(rule.handler)] = true
			}
		}
		for name, field := range value.fields {
			collect(field, path+"."+name)
		}
		for _, item := range value.items {
			collect(item, path+"[]")
		}
	}
	collect(value, "state")
	keys := make([]string, 0, len(parts))
	for key := range parts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, "\n")
}

type matcherRule struct {
	predicate  identity.FlowID
	handler    identity.FlowID
	conditions []condition
}

func appendMatcherRules(rules []matcherRule, added ...matcherRule) []matcherRule {
	for _, rule := range added {
		found := false
		for _, prior := range rules {
			if prior.predicate == rule.predicate && prior.handler == rule.handler && conditionsText(prior.conditions) == conditionsText(rule.conditions) {
				found = true
				break
			}
		}
		if !found {
			rules = append(rules, rule)
		}
	}
	return rules
}

type matcherSignature struct {
	input, output string
	reads         bool
}

func matcherKey(typ types.Matcher) matcherSignature {
	return matcherSignature{typ.Input().String(), typ.Output().String(), typ.Reads()}
}

func (g *graphIndex) registerMatcher(call ir.BuiltinCall) {
	name := call.Operation().Name()
	if name != catalog.MatchCaseNew && name != catalog.MatchNew {
		return
	}
	flows := map[int]identity.FlowID{}
	for _, argument := range call.Arguments() {
		if flow, ok := argument.(ir.FlowArgument); ok {
			flows[flow.Position()] = flow.Flow()
		}
	}
	rule := matcherRule{handler: flows[1]}
	if name == catalog.MatchCaseNew {
		rule.predicate = flows[0]
	}
	if g.matcherRules == nil {
		g.matcherRules = map[matcherSignature][]matcherRule{}
	}
	key := matcherKey(call.Type().(types.Matcher))
	g.matcherRules[key] = appendMatcherRules(g.matcherRules[key], rule)
}

func (a *analyzer) constructMatcher(c *context, call ir.BuiltinCall, values []abstract, flows map[int]identity.FlowID) abstract {
	node := a.graph.nodes[call]
	out := a.unknown(node, call.Type())
	out.matcherKnown = true
	for _, value := range values {
		out.dependencies = append(out.dependencies, value.dependencies...)
	}
	if call.Operation().Name() == catalog.MatchCaseNew {
		out.matcherRules = []matcherRule{{predicate: flows[0], handler: flows[1], conditions: mergeConditions(nil, c.conditions)}}
		return out
	}
	for _, item := range values[0].items {
		out.matcherRules = appendMatcherRules(out.matcherRules, item.matcherRules...)
		out.matcherKnown = out.matcherKnown && item.matcherKnown
	}
	if values[0].items == nil {
		out.matcherKnown = false
	}
	out.matcherRules = appendMatcherRules(out.matcherRules, matcherRule{handler: flows[1], conditions: mergeConditions(nil, c.conditions)})
	return out
}

func (a *analyzer) applyMatcher(c *context, call ir.BuiltinCall, request, matcher abstract) abstract {
	node := a.graph.nodes[call]
	rules := appendMatcherRules(nil, matcher.matcherRules...)
	if !matcher.matcherKnown {
		key := matcherKey(matcher.typ.(types.Matcher))
		rules = appendMatcherRules(rules, a.graph.matcherRules[key]...)
		if key.reads {
			key.reads = false
			rules = appendMatcherRules(rules, a.graph.matcherRules[key]...)
		}
	}
	results := make([]abstract, 0, len(rules))
	for index, rule := range rules {
		conditions := mergeConditions(c.conditions, rule.conditions)
		if rule.predicate != "" {
			test := branchContext(c, condition{Kind: testCondition, Node: node, Case: &index, Context: c.id})
			test.conditions = mergeConditions(test.conditions, conditions)
			a.invokeMatcher(test, rule.predicate, "matcher-predicate", node, request)
		}
		branch := branchContext(c, condition{Kind: matchCondition, Node: node, Case: &index, Context: c.id})
		branch.conditions = mergeConditions(branch.conditions, conditions)
		result := a.invokeMatcher(branch, rule.handler, "matcher-handler", node, request)
		results = append(results, withConditions(result, branch.conditions))
	}
	out := a.join(results, node, call.Type())
	out.dependencies = append(out.dependencies, request.dependencies...)
	out.dependencies = append(out.dependencies, matcher.dependencies...)
	return out
}

func (a *analyzer) invokeMatcher(c *context, flow identity.FlowID, kind string, node nodeID, request abstract) abstract {
	if a.graph.flows[flow].Effects().Devices {
		for owner := c; owner != nil; owner = owner.parent {
			for index := range a.data.Flows {
				if a.data.Flows[index].ID == owner.flow {
					a.data.Flows[index].Effects.Devices = true
				}
			}
		}
	}
	if a.matcherActive == nil {
		a.matcherActive = map[identity.FlowID]bool{}
	}
	if a.matcherActive[flow] {
		a.err = diagnostic.New(diagnostic.InspectionLimit, diagnostic.Inspection, "inspection cannot resolve recursive matcher application", a.graph.nodeData[node].Span)
		return a.unknown(node, a.graph.flows[flow].Signature().Output())
	}
	a.matcherActive[flow] = true
	defer delete(a.matcherActive, flow)
	return a.invoke(c, flow, kind, c.entry, node, []abstract{request}, c.conditions)
}
