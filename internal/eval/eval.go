package eval

import (
	"context"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"math/big"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type machine struct {
	execution.Session
	flows  map[identity.FlowID]*compiledFlow
	frames map[identity.FlowID]*flowFrame
}
type compiledFlow struct {
	name       string
	parameters []ir.Parameter
	body       []ir.Statement
	handler    identity.FlowID
	output     types.Type
	bindings   int
}
type flowFrame struct {
	bindings map[ir.BindingID]value.Value
	inUse    bool
}
type frame struct {
	name     string
	bindings map[ir.BindingID]value.Value
}

func Program(p ir.Program) (execution.Program, error) {
	if p == nil {
		return nil, fmt.Errorf("execution requires a checked program")
	}
	checkedFlows := p.Flows()
	flows := make(map[identity.FlowID]*compiledFlow, len(checkedFlows))
	for _, f := range checkedFlows {
		parameters, body := f.Parameters(), f.Body()
		bindings := len(parameters)
		for _, statement := range body {
			switch statement := statement.(type) {
			case ir.Binding:
				bindings++
			case ir.Commit:
				if statement.ID() != "" {
					bindings++
				}
			}
		}
		flows[f.ID()] = &compiledFlow{name: f.Name(), parameters: parameters, body: body, handler: f.Handler(), output: f.Signature().Output(), bindings: bindings}
	}
	handlers := make([]execution.Handler, 0, len(flows))
	for _, f := range checkedFlows {
		id := f.ID()
		handlers = append(handlers, execution.Handler{ID: id, Name: f.Name(), Parameters: f.Signature().Parameters(), Span: f.Span(), Call: func(s execution.Session, args []value.Value, span source.Span) (value.Value, error) {
			m := &machine{Session: s, flows: flows}
			return m.invoke(id, args, span)
		}})
	}
	body := p.Body()
	return execution.NewProgram(func(s execution.Session) (value.Value, error) {
		m := &machine{Session: s, flows: flows}
		return m.body(body, frame{bindings: map[ir.BindingID]value.Value{}})
	}, handlers)
}
func Run(ctx context.Context, p ir.Program, boundary execution.Boundary, budget execution.Budget, config device.Config) (value.Value, error) {
	program, err := Program(p)
	if err != nil {
		return nil, err
	}
	return program.Run(ctx, boundary, budget, config)
}
func (m *machine) body(body []ir.Statement, f frame) (output value.Value, resultErr error) {
	scope := m.Scope()
	defer func() { scope.Close(output) }()
	for _, s := range body {
		if err := m.Step(); err != nil {
			return nil, err
		}
		switch x := s.(type) {
		case ir.Binding:
			v, err := m.expression(x.Value(), f)
			if err != nil {
				return nil, err
			}
			f.bindings[x.ID()] = v
		case ir.Return:
			return m.expression(x.Value(), f)
		case ir.Commit:
			v, err := m.expression(x.Target(), f)
			if err != nil {
				return nil, err
			}
			activation, err := execution.WithRemovalGuardEvaluator(m.Context(), func(guard value.RemovalGuard, original, current value.FileEntry) (bool, error) {
				arguments := append(guard.Context(), original, current)
				result, err := m.invoke(guard.Flow(), arguments, x.Span())
				if err != nil {
					return false, err
				}
				return result.(value.Boolean).Bool(), nil
			})
			if err != nil {
				return nil, err
			}
			receipt, err := runtimeops.Commit(activationSession{Session: m.Session, ctx: activation}, v.(value.Target), x.Span())
			if err != nil {
				return nil, err
			}
			if x.ID() != "" {
				f.bindings[x.ID()] = receipt
			}

		default:
			panic("unknown checked statement")
		}
	}
	return nil, nil
}

type activationSession struct {
	execution.Session
	ctx context.Context
}

func (session activationSession) Context() context.Context { return session.ctx }
func (m *machine) expression(expr ir.Expression, f frame) (value.Value, error) {
	if err := m.Step(); err != nil {
		return nil, err
	}
	v, err := m.evaluate(expr, f)
	if err != nil {
		return nil, err
	}
	if err := m.ChargeValue(v); err != nil {
		return nil, err
	}
	return v, nil
}
func (m *machine) evaluate(expr ir.Expression, f frame) (value.Value, error) {
	switch x := expr.(type) {
	case ir.Literal:
		switch x.Type().Kind() {
		case types.IntKind:
			n, ok := new(big.Int).SetString(x.Text(), 10)
			if !ok {
				panic("invalid checked integer")
			}
			return runtimeops.Integer(n)
		case types.StringKind:
			return must(value.NewText(types.String(), x.Text())), nil
		case types.BoolKind:
			return value.Bool(x.Text() == "true"), nil
		case types.NullKind:
			return value.Null(), nil
		case types.FloatKind:
			n, err := strconv.ParseFloat(x.Text(), 64)
			if err != nil {
				panic(err)
			}
			return must(value.NewFloat(n)), nil
		default:
			panic("unknown checked literal")
		}
	case ir.Reference:
		v, ok := f.bindings[x.Binding()]
		if !ok {
			panic("absent checked binding")
		}
		return v, nil
	case ir.Projection:
		v, err := m.expression(x.Input(), f)
		if err != nil {
			return nil, err
		}
		return value.Project(v, x.Field()), nil
	case ir.Conversion:
		v, err := m.expression(x.Input(), f)
		if err != nil {
			return nil, err
		}
		switch x.Conversion() {
		case ir.Success:
			return must(value.Success(x.Type().(types.Fallible), v)), nil
		case ir.Failure:
			return must(value.Failure(x.Type().(types.Fallible), v.(value.Error))), nil
		case ir.Propagate:
			result, cause := v.(value.Fallible).Outcome()
			if cause != nil {
				return nil, execution.Failed(cause)
			}
			return result, nil
		case ir.RecordView:
			return value.RecordView(v.(value.Record)), nil
		case ir.FallibleRecordView:
			result, cause := v.(value.Fallible).Outcome()
			if cause != nil {
				return must(value.Failure(x.Type().(types.Fallible), cause)), nil
			}
			return must(value.Success(x.Type().(types.Fallible), value.RecordView(result.(value.Record)))), nil
		case ir.ScalarListView:
			items := []value.Value{}
			limit := m.Limits().ListItems
			for _, r := range v.(value.Text).Text() {
				if limit != 0 && uint64(len(items)) >= limit {
					return nil, execution.Limit("list elements")
				}
				items = append(items, must(value.NewText(types.Scalar(), string(r))))
			}
			return must(value.NewList(x.Type().(types.List), items)), nil
		default:
			panic("unknown checked conversion")
		}
	case ir.FlowCall:
		arguments := x.Arguments()
		args := make([]value.Value, len(arguments))
		for _, a := range arguments {
			v, err := m.expression(a.Value(), f)
			if err != nil {
				return nil, err
			}
			args[a.Position()] = v
		}
		return m.invoke(x.Flow(), args, x.Span())
	case ir.BuiltinCall:
		arguments := x.Arguments()
		args := make([]value.Value, len(arguments))
		var flows map[int]identity.FlowID
		for _, a := range arguments {
			switch a := a.(type) {
			case ir.ValueArgument:
				v, err := m.expression(a.Value(), f)
				if err != nil {
					return nil, err
				}
				args[a.Position()] = v
			case ir.FlowArgument:
				if flows == nil {
					flows = map[int]identity.FlowID{}
				}
				flows[a.Position()] = a.Flow()
			default:
				panic("unknown checked argument")
			}
		}
		return m.builtin(x, args, flows, f)
	case ir.Match:
		selector, err := m.expression(x.Selector(), f)
		if err != nil {
			return nil, err
		}
		for _, arm := range x.Cases() {
			selected := false
			switch arm := arm.(type) {
			case ir.DefaultCase:
				selected = true
			case ir.ExactCase:
				head, err := m.expression(arm.Head(), f)
				if err != nil {
					return nil, err
				}
				if r, ok := head.(value.Range); ok {
					selected = r.Contains(selector)
				} else {
					selected = value.Equal(selector, head)
				}
			case ir.PredicateCase:
				v, err := m.invoke(arm.Flow(), []value.Value{selector}, arm.Span())
				if err != nil {
					return nil, err
				}
				selected = v.(value.Boolean).Bool()
			default:
				panic("unknown checked match case")
			}
			if selected {
				return m.expression(arm.Body(), f)
			}
		}
		panic("checked match has no default")
	default:
		panic("unknown checked expression")
	}
}
func (m *machine) invoke(id identity.FlowID, args []value.Value, span source.Span) (value.Value, error) {
	if err := m.Enter(); err != nil {
		return nil, err
	}
	defer m.Leave()
	f := m.flows[id]
	if f == nil {
		panic("absent checked flow")
	}
	result, err := m.flowBody(id, f, args)
	var cause value.Error
	if err != nil {
		if e, ok := err.(execution.Failure); ok {
			cause = e.Value()
		} else {
			return nil, err
		}
	}
	if v, ok := result.(value.Fallible); ok {
		_, cause = v.Outcome()
	}
	if cause != nil {
		fields := make([]value.Field, len(args))
		for i, parameter := range f.parameters {
			fields[i] = value.Field{Name: parameter.Name, Value: args[i]}
		}
		cause = value.WithFrame(cause, f.name, cause.Operation(), must(value.NewRecord(fields)))
	}
	if err := m.CheckCleanup(cause); err != nil {
		return nil, err
	}
	if cause == nil {
		return result, nil
	}
	if f.handler != "" {
		return m.invoke(f.handler, []value.Value{cause}, span)
	}
	out, ok := f.output.(types.Fallible)
	if !ok {
		panic("unhandled failure in checked total flow")
	}
	cause = value.At(cause, f.name, span)
	return must(value.Failure(out, cause)), nil
}
func (m *machine) flowBody(id identity.FlowID, flow *compiledFlow, args []value.Value) (value.Value, error) {
	cached := m.frames[id]
	if cached == nil {
		cached = &flowFrame{bindings: make(map[ir.BindingID]value.Value, flow.bindings)}
		if m.frames == nil {
			m.frames = map[identity.FlowID]*flowFrame{}
		}
		m.frames[id] = cached
	} else if cached.inUse {
		cached = &flowFrame{bindings: make(map[ir.BindingID]value.Value, flow.bindings)}
	}
	cached.inUse = true
	defer func() {
		clear(cached.bindings)
		cached.inUse = false
	}()
	for index, parameter := range flow.parameters {
		cached.bindings[parameter.ID] = args[index]
	}
	return m.body(flow.body, frame{name: flow.name, bindings: cached.bindings})
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
