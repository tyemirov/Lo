package inspect

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (a *analyzer) expression(c *context, expr ir.Expression) abstract {
	node := a.graph.nodes[expr]
	if a.err != nil {
		return a.unknown(node, expr.Type())
	}
	var out abstract
	switch e := expr.(type) {
	case ir.Literal:
		out = a.unknown(node, e.Type())
		value := e.Text()
		if e.Type().Kind() == types.IntKind {
			n, ok := new(big.Int).SetString(value, 10)
			if !ok {
				panic("invalid checked integer")
			}
			value = n.String()
		}
		out.known = &value
	case ir.Reference:
		var ok bool
		out, ok = c.values[e.Binding()]
		if !ok {
			panic("checked binding is absent from inspection context")
		}
		out.dependencies = uniqueNodes(append(append([]nodeID{}, out.dependencies...), node))
	case ir.Projection:
		value := a.expression(c, e.Input())
		if field, ok := value.fields[e.Field()]; ok {
			out = field
		} else {
			out = a.unknown(node, e.Type())
			out.targets = appendTargets(nil, value.targets...)
		}
		out.dependencies = uniqueNodes(append(out.dependencies, value.dependencies...))
		out.targets = appendTargets(out.targets, value.carried...)
		out.carried = appendTargets(out.carried, value.carried...)
	case ir.Conversion:
		out = a.expression(c, e.Input())
		if e.Conversion() == ir.Propagate {
			c.conditions = mergeConditions(c.conditions, []condition{{Kind: successCondition, Node: node, Context: c.id}})
		}
		if e.Conversion() == ir.ScalarListView && out.known != nil {
			out.items = []abstract{}
			for _, r := range *out.known {
				v := a.unknown(node, types.Scalar())
				text := string(r)
				v.known = &text
				out.items = append(out.items, v)
			}
			out.known = nil
		}
	case ir.FlowCall:
		f := a.graph.flows[e.Flow()]
		values := make([]abstract, len(f.Parameters()))
		for _, arg := range e.Arguments() {
			values[arg.Position()] = a.expression(c, arg.Value())
		}
		out = a.invoke(c, e.Flow(), "call", c.entry, node, values, c.conditions)
	case ir.BuiltinCall:
		out = a.builtin(c, e)
	case ir.Match:
		selector := a.expression(c, e.Selector())
		values := []abstract{}
		for i, arm := range e.Cases() {
			test := branchContext(c, condition{Kind: testCondition, Node: node, Case: &i, Context: c.id})
			branch := branchContext(c, condition{Kind: matchCondition, Node: node, Case: &i, Context: c.id})
			switch head := arm.(type) {
			case ir.ExactCase:
				a.expression(test, head.Head())
			case ir.PredicateCase:
				a.invoke(test, head.Flow(), "predicate", c.entry, node, []abstract{selector}, test.conditions)
			}
			values = append(values, withConditions(a.expression(branch, arm.Body()), branch.conditions))
		}
		out = a.join(values, node, e.Type())
		if mayPropagate(e) {
			c.conditions = mergeConditions(c.conditions, []condition{{Kind: successCondition, Node: node, Context: c.id}})
		}
	default:
		panic("unknown checked expression")
	}
	out.typ = expr.Type()
	out.node = node
	out.dependencies = uniqueNodes(append(out.dependencies, node))
	return withConditions(out, c.conditions)
}
func (a *analyzer) builtin(c *context, e ir.BuiltinCall) abstract {
	node := a.graph.nodes[e]
	op := e.Operation()
	values := make([]abstract, len(e.Arguments()))
	flows := map[int]ir.FlowID{}
	for _, arg := range e.Arguments() {
		switch value := arg.(type) {
		case ir.ValueArgument:
			values[value.Position()] = a.expression(c, value.Value())
		case ir.FlowArgument:
			flows[value.Position()] = value.Flow()
		}
	}
	out := a.unknown(node, e.Type())
	for _, value := range values {
		out.dependencies = append(out.dependencies, value.dependencies...)
		out.targets = appendTargets(out.targets, value.targets...)
	}
	if len(op.ErrorKinds()) > 0 {
		a.addError(c, "fallible", op.Name(), node, op.ErrorKinds(), nil)
	}
	switch op.Boundary() {
	case catalog.Read:
		r := a.resource(c, op.Name(), node, values[0])
		r.ID = nextID("r", len(a.data.Reads))
		a.data.Reads = append(a.data.Reads, r)
		a.capability(r, op.Capability())
	case catalog.Effect, catalog.Server:
		r := a.resource(c, op.Name(), node, values[0])
		r.Origin = "construction"
		r.ID = nextID("t", len(a.data.Targets))
		if op.Boundary() == catalog.Server {
			r.Handler = flows[1]
			r.OnError = flows[2]
		}
		a.data.Targets = append(a.data.Targets, r)
		a.targets[targetID(r.ID)] = r
		out.targets = []targetReference{{ID: targetID(r.ID), Conditions: mergeConditions(nil, c.conditions)}}
	}
	switch op.Rule() {
	case catalog.RecordConstructor:
		out.fields = map[string]abstract{}
		for i, label := range e.Labels() {
			out.fields[label] = values[i]
		}
	case catalog.ListConstructor:
		out.items = values
	case catalog.MapConstructor:
		out.items = values
	case catalog.RecordMerge:
		out.fields = map[string]abstract{}
		for _, value := range values {
			for name, field := range value.fields {
				out.fields[name] = field
			}
		}
	case catalog.MapLookup:
		if op.Name() == catalog.Get {
			candidates := []abstract{}
			for _, entry := range values[0].items {
				if value, ok := entry.fields["value"]; ok {
					candidates = append(candidates, value)
				}
			}
			if len(candidates) > 0 {
				out = a.join(candidates, node, e.Type())
			}
		}
	case catalog.ListProcessor:
		return a.list(c, e, values, flows[len(e.Arguments())-1])
	}
	switch op.Name() {
	case catalog.IntNew, catalog.StringNew, catalog.BoolNew, catalog.FloatNew, catalog.ScalarNew, catalog.PathNew:
		out = values[0]
	case catalog.Concat:
		var b strings.Builder
		known := true
		for _, value := range values {
			if value.known == nil || b.Len()+len(*value.known) > maxStaticText {
				known = false
				break
			}
			b.WriteString(*value.known)
		}
		if known {
			s := b.String()
			out.known = &s
		}
	case catalog.Upper, catalog.Lower:
		if values[0].known != nil && len(*values[0].known) <= maxStaticText {
			s := *values[0].known
			if op.Name() == catalog.Upper {
				s = strings.ToUpper(s)
			} else {
				s = strings.ToLower(s)
			}
			if len(s) <= maxStaticText {
				out.known = &s
			}
		}
	case catalog.IntToString:
		out.known = values[0].known
	case catalog.ErrorNew:
		out.fields = map[string]abstract{"kind": values[0], "message": values[1], "input": values[2]}
		if values[0].known != nil {
			a.addError(c, "value", op.Name(), node, []string{*values[0].known}, nil)
		} else {
			d := a.describe(values[0])
			a.addError(c, "value", op.Name(), node, nil, &d)
		}
	}
	return out
}
func (a *analyzer) list(c *context, e ir.BuiltinCall, values []abstract, callback ir.FlowID) abstract {
	node := a.graph.nodes[e]
	name := e.Operation().Name()
	flow := a.graph.flows[callback]
	args := make([]abstract, len(flow.Parameters()))
	contextStart := 1
	if name == catalog.ListFold || name == catalog.ListUnfold {
		contextStart = 2
	}
	for i := contextStart; i < len(values)-1; i++ {
		args[i-contextStart] = values[i]
	}
	if name == catalog.ListUnfold {
		// Later iterations can change the state, so the symbolic state is dynamic.
		args[len(args)-1] = a.unknown(values[0].node, flow.Parameters()[len(args)-1].Type)
		args[len(args)-1].targets = values[0].targets
	} else {
		elementType := flow.Parameters()[len(args)-1].Type
		if len(values[0].items) > 0 {
			args[len(args)-1] = a.join(values[0].items, values[0].node, elementType)
		} else {
			args[len(args)-1] = a.unknown(values[0].node, elementType)
			args[len(args)-1].targets = values[0].targets
		}
		if name == catalog.ListFold {
			state := a.unknown(values[1].node, flow.Parameters()[len(args)-2].Type)
			state.targets = values[1].targets
			args[len(args)-2] = state
		}
	}
	child := branchContext(c, condition{Kind: iterationCondition, Node: node, Context: c.id})
	result := a.invoke(child, callback, "list-callback", c.entry, node, args, child.conditions)
	out := a.unknown(node, e.Type())
	out.dependencies = append(out.dependencies, result.dependencies...)
	for _, value := range values {
		out.dependencies = append(out.dependencies, value.dependencies...)
	}
	switch name {
	case catalog.ListMap:
		out.items = []abstract{result}
		out.targets = result.targets
	case catalog.ListFilter:
		out.items = values[0].items
		out.targets = values[0].targets
	case catalog.ListFold:
		out = a.join([]abstract{values[1], result}, node, e.Type())
	case catalog.ListFlatMap:
		out.items = result.items
		out.targets = result.targets
	case catalog.ListUnfold:
		if value, ok := result.fields["value"]; ok {
			value.targets = appendTargets(value.targets, result.targets...)
			value.carried = appendTargets(value.carried, result.targets...)
			out.items = []abstract{value}
			out.targets = value.targets
		} else {
			out.targets = result.targets
		}
	}
	return out
}
func mayPropagate(e ir.Expression) bool {
	switch n := e.(type) {
	case ir.Conversion:
		return n.Conversion() == ir.Propagate || mayPropagate(n.Input())
	case ir.Projection:
		return mayPropagate(n.Input())
	case ir.BuiltinCall:
		for _, a := range n.Arguments() {
			if v, ok := a.(ir.ValueArgument); ok && mayPropagate(v.Value()) {
				return true
			}
		}
	case ir.FlowCall:
		for _, a := range n.Arguments() {
			if mayPropagate(a.Value()) {
				return true
			}
		}
	case ir.Match:
		if mayPropagate(n.Selector()) {
			return true
		}
		for _, c := range n.Cases() {
			if mayPropagate(c.Body()) {
				return true
			}
			if x, ok := c.(ir.ExactCase); ok && mayPropagate(x.Head()) {
				return true
			}
		}
	}
	return false
}

func nextID(prefix string, prior int) string { return fmt.Sprintf("%s%d", prefix, prior+1) }
