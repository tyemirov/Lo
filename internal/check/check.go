package check

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/resolve"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensorfacts"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type flowState uint8

const (
	unvisited flowState = iota
	visiting
	complete
)

type flowWork struct {
	declaration syntax.Flow
	entry       resolve.Entry
	scope       resolve.Scope
	signature   types.Signature
	parameters  []ir.Parameter
	state       flowState
	graph       ir.Flow
}
type module struct {
	flows       map[string]*flowWork
	order       []*flowWork
	bindings    map[ir.BindingID]types.Type
	definitions map[ir.BindingID]ir.Expression
	stack       []*flowWork
}
type checker struct {
	module      *module
	scope       resolve.Scope
	flow        *flowWork
	failureCode diagnostic.Code
}
type expression struct {
	node       ir.Expression
	effects    ir.Effects
	propagates bool
}

func (e expression) Type() types.Type  { return e.node.Type() }
func (e expression) Span() source.Span { return e.node.Span() }
func (e expression) with(other expression) expression {
	e.effects = e.effects.With(other.effects)
	e.propagates = e.propagates || other.propagates
	return e
}

func Program(program syntax.Program) (ir.Program, diagnostic.Diagnostic) {
	m := &module{flows: make(map[string]*flowWork), bindings: make(map[ir.BindingID]types.Type), definitions: make(map[ir.BindingID]ir.Expression)}
	scope := resolve.New(nil)
	if d := m.declare(scope, program.Statements()); d != nil {
		return nil, d
	}
	c := checker{module: m, scope: scope}
	body, _, d := c.body(program.Statements())
	if d != nil {
		return nil, d
	}
	flows := make([]ir.Flow, len(m.order))
	for i, f := range m.order {
		if d := m.compileFlow(f, f.declaration.Span()); d != nil {
			return nil, d
		}
		flows[i] = f.graph
	}
	p := must(ir.NewProgram(program.Source(), body, flows))
	if _, d := tensorfacts.Analyze(p); d != nil {
		return nil, d
	}
	return p, nil
}
func (m *module) declare(scope resolve.Scope, body []syntax.Statement) diagnostic.Diagnostic {
	for _, statement := range body {
		switch s := statement.(type) {
		case syntax.Binding:
			if _, d := scope.Declare(s.Name(), resolve.Value, s.Span()); d != nil {
				return d
			}
		case syntax.Flow:
			entry, d := scope.Declare(s.Name(), resolve.Flow, s.Span())
			if d != nil {
				return d
			}
			f := &flowWork{declaration: s, entry: entry, scope: resolve.New(scope)}
			m.flows[entry.ID()] = f
			m.order = append(m.order, f)
		}
	}
	for _, statement := range body {
		s, ok := statement.(syntax.Flow)
		if !ok {
			continue
		}
		entry, _ := scope.Local(s.Name())
		f := m.flows[entry.ID()]
		parameters := make([]types.Parameter, len(s.Parameters()))
		for i, p := range s.Parameters() {
			entry, d := f.scope.Declare(p.Name, resolve.Value, p.Span)
			if d != nil {
				return d
			}
			t, d := typeExpression(p.Type)
			if d != nil {
				return d
			}
			id := ir.BindingID(entry.ID())
			m.bindings[id] = t
			parameters[i] = types.Parameter{Name: p.Name, Type: t}
			f.parameters = append(f.parameters, ir.Parameter{ID: id, Name: p.Name, Type: t, Span: p.Span})
		}
		output, d := typeExpression(s.Output())
		if d != nil {
			return d
		}
		f.signature = must(types.NewSignature(parameters, output))
		if d := m.declare(f.scope, s.Body()); d != nil {
			return d
		}
	}
	return nil
}
func (m *module) compileFlow(f *flowWork, span source.Span) diagnostic.Diagnostic {
	switch f.state {
	case complete:
		return nil
	case visiting:
		d := problem(diagnostic.Cycle, "flow reference creates a cycle through "+f.declaration.Name(), span)
		for _, active := range m.stack {
			d = diagnostic.WithRelated(d, active.declaration.Span())
		}
		return d
	}
	f.state = visiting
	m.stack = append(m.stack, f)
	defer func() { m.stack = m.stack[:len(m.stack)-1] }()
	c := checker{module: m, scope: f.scope, flow: f}
	var handler identity.FlowID
	var handlerEffects ir.Effects
	if name := f.declaration.Handler(); name != "" {
		h, d := c.namedFlow(name, f.declaration.Span())
		if d != nil {
			return d
		}
		if d := c.callback(h, []types.Type{types.Error()}, f.signature.Output(), f.declaration.Span()); d != nil {
			return d
		}
		handler = identity.FlowID(h.entry.ID())
		handlerEffects = h.graph.Effects()
	}
	body, effects, d := c.body(f.declaration.Body())
	if d != nil {
		return d
	}
	effects = effects.With(handlerEffects)
	if effects.Commits && !c.canPropagate() {
		return problem(diagnostic.EffectfulTotal, "an effectful flow needs a fallible output or a self-handler", f.declaration.Span())
	}
	f.graph = must(ir.NewFlow(identity.FlowID(f.entry.ID()), f.declaration.Name(), f.signature, f.parameters, handler, body, effects, f.declaration.Span()))
	f.state = complete
	return nil
}
func (c *checker) body(statements []syntax.Statement) ([]ir.Statement, ir.Effects, diagnostic.Diagnostic) {
	var body []ir.Statement
	var effects ir.Effects
	for _, statement := range statements {
		switch s := statement.(type) {
		case syntax.Flow:
			// Nested declarations have independent invocation regions.
			if c.flow == nil {
				e, _ := c.scope.Local(s.Name())
				if d := c.module.compileFlow(c.module.flows[e.ID()], s.Span()); d != nil {
					return nil, effects, d
				}
			}
		case syntax.Binding:
			value, d := c.expression(s.Value(), nil)
			if d != nil {
				return nil, effects, d
			}
			if c.flow != nil {
				value, d = c.unwrap(value)
				if d != nil {
					return nil, effects, d
				}
			}
			entry, _ := c.scope.Local(s.Name())
			id := ir.BindingID(entry.ID())
			c.module.bindings[id] = value.Type()
			c.module.definitions[id] = value.node
			body = append(body, must(ir.NewBinding(id, s.Name(), value.node, s.Span())))
			effects = effects.With(value.effects)
		case syntax.Return:
			var expected types.Type
			if c.flow != nil {
				expected = c.flow.signature.Output()
			}
			value, d := c.expression(s.Value(), expected)
			if d != nil {
				return nil, effects, d
			}
			body = append(body, must(ir.NewReturn(value.node, s.Span())))
			effects = effects.With(value.effects)
		case syntax.Commit:
			value, d := c.reference(s.Target(), s.Span())
			if d != nil {
				return nil, effects, d
			}
			value, d = c.unwrap(value)
			if d != nil {
				return nil, effects, d
			}
			if value.Type().Kind() != types.EffectKind && value.Type().Kind() != types.ServerKind {
				return nil, effects, problem(diagnostic.NotCommittable, "commit requires Effect or HTTP.Server, got "+value.Type().String(), s.Span())
			}
			if c.flow != nil && !c.canPropagate() {
				return nil, effects, problem(diagnostic.Unhandled, "commit failure needs a fallible flow or a self-handler", s.Span())
			}
			body = append(body, must(ir.NewCommit(value.node, s.Span())))
			effects = effects.With(value.effects)
			effects.Commits = true
		default:
			panic("unknown syntax statement")
		}
	}
	return body, effects, nil
}
func (c *checker) reference(name string, span source.Span) (expression, diagnostic.Diagnostic) {
	e, local := c.scope.Local(name)
	if !local {
		var found bool
		e, found = c.scope.Lookup(name)
		if !found {
			return expression{}, unknown("value", name, span)
		}
		if e.Kind() == resolve.Value {
			return expression{}, diagnostic.WithRelated(diagnostic.New(diagnostic.Capture, diagnostic.Resolve, "flow cannot capture outer value "+name, span), e.Span())
		}
	}
	if e.Kind() == resolve.Flow {
		return expression{}, problem(diagnostic.FlowReference, "flow references are valid only as callbacks or handlers", span)
	}
	id := ir.BindingID(e.ID())
	t, ready := c.module.bindings[id]
	if !ready {
		return expression{}, unknown("earlier value", name, span)
	}
	return expression{node: must(ir.NewReference(t, id, span))}, nil
}
func (c *checker) namedFlow(name string, span source.Span) (*flowWork, diagnostic.Diagnostic) {
	e, ok := c.scope.Lookup(name)
	if !ok {
		return nil, unknown("flow", name, span)
	}
	if e.Kind() != resolve.Flow {
		return nil, problem(diagnostic.FlowReference, name+" is not a named flow", span)
	}
	f := c.module.flows[e.ID()]
	if d := c.module.compileFlow(f, span); d != nil {
		return nil, d
	}
	return f, nil
}
func (c *checker) flowArgument(arg syntax.Argument) (*flowWork, diagnostic.Diagnostic) {
	r, ok := arg.Value.(syntax.Reference)
	if !ok {
		return nil, problem(diagnostic.FlowReference, "callback requires a named flow reference", arg.Span)
	}
	return c.namedFlow(r.Name(), arg.Span)
}
func (c *checker) callback(f *flowWork, parameters []types.Type, output types.Type, span source.Span) diagnostic.Diagnostic {
	if f.graph.Effects().Commits {
		return problem(diagnostic.EffectfulCallback, "callback "+f.declaration.Name()+" can activate a commit", span)
	}
	actual := f.signature.Parameters()
	if len(actual) != len(parameters) {
		return problem(diagnostic.ArgumentCount, fmt.Sprintf("callback %s expects %d parameters, got %d", f.declaration.Name(), len(parameters), len(actual)), span)
	}
	for i, p := range actual {
		if !types.Equal(p.Type, parameters[i]) {
			return diagnostic.Mismatch(parameters[i].String(), p.Type.String(), span)
		}
	}
	if output != nil && !types.Equal(f.signature.Output(), output) {
		return diagnostic.Mismatch(output.String(), f.signature.Output().String(), span)
	}
	return nil
}
func (c *checker) canPropagate() bool {
	return c.flow != nil && (c.flow.signature.Output().Kind() == types.FallibleKind || c.flow.declaration.Handler() != "")
}
func (c *checker) unwrap(e expression) (expression, diagnostic.Diagnostic) {
	f, ok := e.Type().(types.Fallible)
	if !ok {
		return e, nil
	}
	if c.failureCode != "" {
		return expression{}, problem(c.failureCode, "case heads must be total", e.Span())
	}
	if !c.canPropagate() {
		return expression{}, problem(diagnostic.Unhandled, "fallible value needs a fallible flow or a self-handler", e.Span())
	}
	e.node = must(ir.NewConversion(f.Base(), e.node, ir.Propagate))
	e.propagates = true
	return e, nil
}
func (c *checker) coerce(e expression, expected types.Type) (expression, diagnostic.Diagnostic) {
	if expected == nil || types.Equal(e.Type(), expected) {
		return e, nil
	}
	if f, ok := expected.(types.Fallible); ok {
		if e.Type().Kind() == types.ErrorKind && f.Base().Kind() != types.ErrorKind {
			e.node = must(ir.NewConversion(expected, e.node, ir.Failure))
			return e, nil
		}
		if actual, ok := e.Type().(types.Fallible); ok {
			if actual.Base().Kind() == types.RecordKind && f.Base().Kind() == types.AnyRecordKind {
				e.node = must(ir.NewConversion(expected, e.node, ir.FallibleRecordView))
				return e, nil
			}
			return expression{}, diagnostic.Mismatch(expected.String(), e.Type().String(), e.Span())
		}
		value, d := c.coerce(e, f.Base())
		if d != nil {
			return expression{}, d
		}
		value.node = must(ir.NewConversion(expected, value.node, ir.Success))
		return value, nil
	}
	if e.Type().Kind() == types.FallibleKind {
		value, d := c.unwrap(e)
		if d != nil {
			return expression{}, d
		}
		return c.coerce(value, expected)
	}
	if expected.Kind() == types.AnyRecordKind && e.Type().Kind() == types.RecordKind {
		e.node = must(ir.NewConversion(expected, e.node, ir.RecordView))
		return e, nil
	}
	return expression{}, diagnostic.Mismatch(expected.String(), e.Type().String(), e.Span())
}
func problem(code diagnostic.Code, message string, span source.Span) diagnostic.Diagnostic {
	return diagnostic.New(code, diagnostic.Check, message, span)
}
func unknown(subject, name string, span source.Span) diagnostic.Diagnostic {
	return diagnostic.New(diagnostic.UnknownName, diagnostic.Resolve, "unknown "+subject+" "+name, span)
}
func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
func baseType(t types.Type) types.Type {
	if f, ok := t.(types.Fallible); ok {
		return f.Base()
	}
	return t
}
func fallibleType(t types.Type) types.Type {
	if t.Kind() == types.FallibleKind {
		return t
	}
	return must(types.NewFallible(t))
}
