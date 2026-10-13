package inspect

import (
	"fmt"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const programEntry = "program"
const MaxContexts = 4096
const maxStaticText = 64 * 1024
const (
	matchCondition        = "match-case"
	testCondition         = "match-test"
	iterationCondition    = "list-iteration"
	successCondition      = "success"
	commitCondition       = "commit-success"
	handlerCondition      = "handler-error"
	requestCondition      = "adapter-request"
	adapterErrorCondition = "adapter-error"
)

type abstract struct {
	typ          types.Type
	node         nodeID
	known        *string
	dependencies []nodeID
	fields       map[string]abstract
	items        []abstract
	targets      []targetReference
	carried      []targetReference
}
type context struct {
	id         contextID
	entry      string
	handler    ir.FlowID
	conditions []condition
	values     map[ir.BindingID]abstract
}
type analyzer struct {
	program ir.Program
	data    reportData
	graph   *graphIndex
	targets map[targetID]resourceData
	uses    map[ir.FlowID]map[string]bool
	err     diagnostic.Diagnostic
}

func Program(program ir.Program) (Report, diagnostic.Diagnostic) {
	a := &analyzer{program: program, data: newReport(program.Source().Path()), targets: map[targetID]resourceData{}, uses: map[ir.FlowID]map[string]bool{}}
	if t := program.ResultType(); t != nil {
		a.data.ResultType = t.String()
	}
	a.graph = index(program, &a.data)
	root := a.newContext(nil, "program", programEntry, "", "", nil)
	a.data.EntryPoints = append(a.data.EntryPoints, entryData{ID: programEntry, Kind: "program", Context: root.id})
	a.body(root, program.Body())
	for _, f := range a.graph.order {
		if len(a.uses[f.ID()]) == 0 {
			values := make([]abstract, len(f.Parameters()))
			for i, p := range f.Parameters() {
				values[i] = a.unknown(a.graph.parameters[p.ID], p.Type)
			}
			a.invoke(nil, f.ID(), "unused", "", "", values, nil)
		}
	}
	if a.err != nil {
		return nil, a.err
	}
	for i, f := range a.data.Flows {
		uses := a.uses[f.ID]
		for _, kind := range []string{"program", "adapter", "unused"} {
			if uses[kind] {
				a.data.Flows[i].Uses = append(a.data.Flows[i].Uses, kind)
			}
		}
		if !uses["program"] && !uses["adapter"] {
			a.data.UnusedFlows = append(a.data.UnusedFlows, f.ID)
		}
	}
	return &report{a.data}, nil
}
func (a *analyzer) newContext(parent *context, kind, entry string, flow ir.FlowID, node nodeID, conditions []condition) *context {
	if a.err != nil {
		return nil
	}
	if len(a.data.Contexts) >= MaxContexts {
		var span source.Span
		if node != "" {
			span = a.graph.nodeData[node].Span
		} else if flow != "" {
			span = a.graph.flows[flow].Span()
		} else {
			span = a.program.Source().Span(0, 0)
		}
		a.err = diagnostic.New(diagnostic.InspectionLimit, diagnostic.Inspection, "inspection exceeds 4096 invocation contexts", span)
		return nil
	}
	c := &context{id: contextID(fmt.Sprintf("c%d", len(a.data.Contexts)+1)), entry: entry, conditions: mergeConditions(nil, conditions), values: map[ir.BindingID]abstract{}}
	var parentID contextID
	if parent != nil {
		parentID = parent.id
		c.handler = parent.handler
	}
	a.data.Contexts = append(a.data.Contexts, contextData{c.id, entry, parentID, flow, node, kind})
	if flow != "" {
		if a.uses[flow] == nil {
			a.uses[flow] = map[string]bool{}
		}
		use := "adapter"
		if entry == programEntry {
			use = "program"
		} else if entry == "" {
			use = "unused"
		}
		a.uses[flow][use] = true
	}
	return c
}
func branchContext(c *context, guard condition) *context {
	copy := *c
	copy.conditions = mergeConditions(c.conditions, []condition{guard})
	return &copy
}
func (a *analyzer) invoke(parent *context, id ir.FlowID, kind, entry string, node nodeID, values []abstract, conditions []condition) abstract {
	f := a.graph.flows[id]
	if node == "" {
		body := f.Body()
		node = a.graph.statements[body[len(body)-1].Span().Start().Offset]
	}
	if a.err != nil {
		return abstract{typ: f.Signature().Output()}
	}
	c := a.newContext(parent, kind, entry, id, node, conditions)
	if c == nil {
		return abstract{typ: f.Signature().Output()}
	}
	for i, p := range f.Parameters() {
		c.values[p.ID] = values[i]
	}
	if f.Handler() != "" {
		c.handler = f.Handler()
	}
	out := a.body(c, f.Body())
	if f.Handler() != "" && a.err == nil {
		handler := a.graph.flows[f.Handler()]
		errorNode := a.graph.parameters[handler.Parameters()[0].ID]
		input := a.unknown(errorNode, types.Error())
		// A handler starts at the flow boundary, before success guards from the body.
		handlerConditions := mergeConditions(conditions, []condition{{Kind: handlerCondition, Node: node, Context: c.id}})
		owner := *c
		if parent != nil {
			owner.handler = parent.handler
		} else {
			owner.handler = ""
		}
		recovered := a.invoke(&owner, f.Handler(), "self-handler", entry, node, []abstract{input}, handlerConditions)
		out = a.join([]abstract{out, recovered}, node, f.Signature().Output())
	}
	return out
}
func (a *analyzer) body(c *context, body []ir.Statement) abstract {
	var out abstract
	for _, statement := range body {
		if a.err != nil {
			return out
		}
		switch s := statement.(type) {
		case ir.Binding:
			c.values[s.ID()] = a.expression(c, s.Value())
		case ir.Return:
			out = withConditions(a.expression(c, s.Value()), c.conditions)
		case ir.Commit:
			value := a.expression(c, s.Target())
			node := a.graph.statements[s.Span().Start().Offset]
			a.activate(c, node, value)
			c.conditions = mergeConditions(c.conditions, []condition{{Kind: commitCondition, Node: node, Context: c.id}})
		}
	}
	return out
}
func (a *analyzer) unknown(node nodeID, t types.Type) abstract {
	deps := []nodeID{}
	if node != "" {
		deps = append(deps, node)
	}
	return abstract{typ: t, node: node, dependencies: deps, targets: []targetReference{}}
}
func (a *analyzer) describe(value abstract) description {
	n := a.graph.nodeData[value.node]
	d := description{Kind: "dynamic", Type: value.typ.String(), Node: value.node, Span: n.Span, Dependencies: uniqueNodes(value.dependencies)}
	if value.known != nil {
		v := *value.known
		d.Kind = "known"
		d.Value = &v
	} else {
		d.Expression = a.program.Source().Text()[n.Span.Start().Offset:n.Span.End().Offset]
	}
	return d
}
func (a *analyzer) join(values []abstract, node nodeID, t types.Type) abstract {
	result := a.unknown(node, t)
	for _, value := range values {
		result.dependencies = append(result.dependencies, value.dependencies...)
		result.targets = appendTargets(result.targets, value.targets...)
		result.carried = appendTargets(result.carried, value.carried...)
	}
	result.dependencies = uniqueNodes(result.dependencies)
	if len(values) == 0 {
		return result
	}
	same := values[0].known != nil
	for _, value := range values[1:] {
		same = same && value.known != nil && *values[0].known == *value.known
	}
	if same {
		v := *values[0].known
		result.known = &v
	}
	shape := t
	if fallible, ok := shape.(types.Fallible); ok {
		shape = fallible.Base()
	}
	if record, ok := shape.(types.Record); ok {
		result.fields = map[string]abstract{}
		for _, field := range record.Fields() {
			children := make([]abstract, len(values))
			for i, value := range values {
				if child, ok := value.fields[field.Name]; ok {
					child.carried = appendTargets(child.carried, value.carried...)
					child.targets = appendTargets(child.targets, value.carried...)
					children[i] = child
				} else {
					children[i] = a.unknown(value.node, field.Type)
					children[i].targets = value.targets
					children[i].carried = value.carried
				}
			}
			result.fields[field.Name] = a.join(children, node, field.Type)
		}
	}
	return result
}
func withConditions(value abstract, conditions []condition) abstract {
	value.carried = slices.Clone(value.carried)
	for i, target := range value.carried {
		value.carried[i].Conditions = mergeConditions(target.Conditions, conditions)
	}
	value.targets = slices.Clone(value.targets)
	for i, target := range value.targets {
		value.targets[i].Conditions = mergeConditions(target.Conditions, conditions)
	}
	if value.fields != nil {
		fields := make(map[string]abstract, len(value.fields))
		for name, field := range value.fields {
			fields[name] = withConditions(field, conditions)
		}
		value.fields = fields
	}
	if value.items != nil {
		value.items = slices.Clone(value.items)
		for i, item := range value.items {
			value.items[i] = withConditions(item, conditions)
		}
	}
	return value
}
func uniqueNodes(nodes []nodeID) []nodeID {
	out := []nodeID{}
	seen := map[nodeID]bool{}
	for _, node := range nodes {
		if node != "" && !seen[node] {
			seen[node] = true
			out = append(out, node)
		}
	}
	return out
}
func mergeConditions(left, right []condition) []condition {
	out := []condition{}
	for _, c := range append(slices.Clone(left), right...) {
		found := false
		for _, prior := range out {
			sameCase := prior.Case == nil && c.Case == nil || prior.Case != nil && c.Case != nil && *prior.Case == *c.Case
			if prior.Kind == c.Kind && prior.Node == c.Node && prior.Context == c.Context && sameCase {
				found = true
				break
			}
		}
		if !found {
			out = append(out, c)
		}
	}
	return out
}
func appendTargets(targets []targetReference, added ...targetReference) []targetReference {
	out := slices.Clone(targets)
	for _, target := range added {
		found := false
		for _, prior := range out {
			if target.ID == prior.ID && conditionsText(target.Conditions) == conditionsText(prior.Conditions) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, target)
		}
	}
	return out
}
func (a *analyzer) addError(c *context, category, operation string, node nodeID, kinds []string, dynamic *description) {
	if a.err != nil {
		return
	}
	a.data.ErrorSources = append(a.data.ErrorSources, errorData{ID: fmt.Sprintf("e%d", len(a.data.ErrorSources)+1), Category: category, Operation: operation, Context: c.id, Entry: c.entry, Node: node, Kinds: append([]string{}, kinds...), Dynamic: dynamic, Handler: c.handler, Conditions: mergeConditions(nil, c.conditions)})
}
func (a *analyzer) resource(c *context, operation string, node nodeID, target abstract) resourceData {
	return resourceData{Operation: operation, Context: c.id, Entry: c.entry, Node: node, Target: a.describe(target), Conditions: mergeConditions(nil, c.conditions)}
}
func (a *analyzer) capability(r resourceData, operation string) {
	if r.Entry == "" {
		return
	}
	r.ID = fmt.Sprintf("p%d", len(a.data.Capabilities)+1)
	r.Operation = operation
	a.data.Capabilities = append(a.data.Capabilities, r)
}
func (a *analyzer) activate(c *context, node nodeID, value abstract) {
	if a.err != nil {
		return
	}
	targets := withConditions(value, c.conditions).targets
	if len(targets) == 0 {
		operations := []string{catalog.FileAppend, catalog.FileWrite}
		if value.typ.Kind() == types.ServerKind {
			operations = []string{catalog.ServerNew}
		}
		for _, name := range operations {
			op, _ := catalog.Lookup(name)
			targetType := types.Path()
			if op.Boundary() == catalog.Server {
				targetType = types.Int()
			}
			unknown := a.unknown(value.node, targetType)
			unknown.dependencies = value.dependencies
			r := a.resource(c, name, value.node, unknown)
			r.Origin = "dynamic-target"
			r.ID = fmt.Sprintf("t%d", len(a.data.Targets)+1)
			a.data.Targets = append(a.data.Targets, r)
			a.targets[targetID(r.ID)] = r
			targets = append(targets, targetReference{targetID(r.ID), mergeConditions(nil, c.conditions)})
		}
	}
	activation := activationData{ID: fmt.Sprintf("a%d", len(a.data.Activations)+1), Context: c.id, Entry: c.entry, Node: node, Conditions: mergeConditions(nil, c.conditions), Targets: targets}
	a.data.Activations = append(a.data.Activations, activation)
	for _, ref := range targets {
		r := a.targets[ref.ID]
		op, _ := catalog.Lookup(r.Operation)
		r.Context = c.id
		r.Entry = c.entry
		r.Node = node
		r.Conditions = mergeConditions(c.conditions, ref.Conditions)
		a.capability(r, op.Capability())
		arm := *c
		arm.conditions = r.Conditions
		a.addError(&arm, "activation", op.Name(), node, op.ActivationKinds(), nil)
		if op.Boundary() == catalog.Server && r.Handler != "" {
			a.adapter(&arm, r.Handler, r.OnError, activation.ID, node, requestCondition)
			a.adapter(&arm, r.OnError, "", activation.ID, node, adapterErrorCondition)
		}
	}
}
func (a *analyzer) adapter(parent *context, flow, handler ir.FlowID, activation string, node nodeID, conditionKind string) {
	if a.err != nil {
		return
	}
	f := a.graph.flows[flow]
	entry := ""
	kind := "http-handler"
	if conditionKind == adapterErrorCondition {
		kind = "http-error"
	}
	if parent.entry != "" {
		entry = fmt.Sprintf("http%d", len(a.data.EntryPoints))
	}
	conditions := mergeConditions(parent.conditions, []condition{{Kind: commitCondition, Node: node, Context: parent.id}, {Kind: conditionKind, Node: node, Context: parent.id}})
	owner := *parent
	owner.handler = handler
	inputs := make([]abstract, len(f.Parameters()))
	for i, p := range f.Parameters() {
		inputs[i] = a.unknown(a.graph.parameters[p.ID], p.Type)
	}
	contextIndex := len(a.data.Contexts)
	entryIndex := len(a.data.EntryPoints)
	if entry != "" {
		a.data.EntryPoints = append(a.data.EntryPoints, entryData{ID: entry, Kind: kind, Flow: flow, Activation: activation})
	}
	a.invoke(&owner, flow, kind, entry, node, inputs, conditions)
	if entry != "" && a.err == nil {
		a.data.EntryPoints[entryIndex].Context = a.data.Contexts[contextIndex].ID
	}
	if conditionKind == requestCondition && a.err == nil {
		boundary := &context{id: a.data.Contexts[contextIndex].ID, entry: entry, handler: handler, conditions: conditions}
		op, _ := catalog.Lookup(catalog.ServerNew)
		for _, failure := range op.BoundaryErrors() {
			a.addError(boundary, "boundary", failure.Operation, node, []string{failure.Kind}, nil)
		}
	}
}
