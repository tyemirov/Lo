package eval

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	DefaultSteps        uint64 = 1_000_000
	MaxBytes                   = 16 << 20
	MaxItems                   = 1_000_000
	MaxIntegerBits             = 1_048_576
	MaxDepth                   = 256
	MaxValueWeight      uint64 = 16 << 20
	MaxEvaluationWeight uint64 = 64 << 20
)

type Boundary interface {
	Read(context.Context, value.Text) (value.Bytes, error)
	Commit(context.Context, value.Target) error
}
type Budget interface {
	Steps() uint64
	budget()
}
type budget uint64

func (budget) budget()         {}
func (b budget) Steps() uint64 { return uint64(b) }
func NewBudget(steps uint64) (Budget, error) {
	if steps == 0 {
		return nil, fmt.Errorf("execution steps must be positive")
	}
	return budget(steps), nil
}
func DefaultBudget() Budget { return budget(DefaultSteps) }

type Failure interface {
	error
	Value() value.Error
	failure()
}
type failure struct{ cause value.Error }

func (failure) failure()             {}
func (f failure) Value() value.Error { return f.cause }
func (f failure) Error() string      { return f.cause.Kind() + ": " + f.cause.Message() }

type HostError interface {
	error
	Code() diagnostic.Code
	Unwrap() error
	hostError()
}
type hostError struct {
	code    diagnostic.Code
	message string
	cause   error
}

func (hostError) hostError()              {}
func (e hostError) Code() diagnostic.Code { return e.code }
func (e hostError) Unwrap() error         { return e.cause }
func (e hostError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func Denied(operation, subject string) HostError {
	return hostError{diagnostic.CapabilityDenied, fmt.Sprintf("%s %q has no host capability", operation, subject), nil}
}
func Limit(subject string) HostError {
	return hostError{diagnostic.RuntimeLimit, "execution limit: " + subject, nil}
}
func HostFailure(operation, subject string, cause error) HostError {
	return hostError{diagnostic.RuntimeHost, fmt.Sprintf("%s %q", operation, subject), cause}
}
func ServerRequiresServe() HostError {
	return hostError{diagnostic.ServerRequiresServe, "HTTP server activation requires lo serve", nil}
}
func NoServer() HostError {
	return hostError{diagnostic.NoServer, "serve requires an activated HTTP server", nil}
}
func boundaryHostError(err error) HostError {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return hostError{diagnostic.RuntimeCancelled, "execution cancelled", err}
	}
	var host HostError
	if errors.As(err, &host) {
		return hostError{host.Code(), "boundary operation failed", err}
	}
	return nil
}

type machine struct {
	ctx       context.Context
	boundary  Boundary
	flows     map[ir.FlowID]ir.Flow
	remaining uint64
	depth     int
	weight    uint64
}
type frame struct {
	name     string
	bindings map[ir.BindingID]value.Value
}

func Run(ctx context.Context, p ir.Program, boundary Boundary, limit Budget) (value.Value, error) {
	m, err := newMachine(ctx, p, boundary, limit)
	if err != nil {
		return nil, err
	}
	v, err := m.body(p.Body(), frame{bindings: map[ir.BindingID]value.Value{}})
	if err != nil {
		return nil, err
	}
	return published(v)
}

type Invocation interface {
	Invoke(ir.FlowID, []value.Value) (value.Value, error)
	invocation()
}

func (*machine) invocation() {}
func NewInvocation(ctx context.Context, p ir.Program, boundary Boundary, limit Budget) (Invocation, error) {
	return newMachine(ctx, p, boundary, limit)
}
func newMachine(ctx context.Context, p ir.Program, boundary Boundary, limit Budget) (*machine, error) {
	if ctx == nil || p == nil || boundary == nil || limit == nil {
		return nil, fmt.Errorf("execution requires context, checked program, boundary, and budget")
	}
	m := &machine{ctx: ctx, boundary: boundary, flows: map[ir.FlowID]ir.Flow{}, remaining: limit.Steps()}
	for _, f := range p.Flows() {
		m.flows[f.ID()] = f
	}
	return m, nil
}
func (m *machine) Invoke(id ir.FlowID, args []value.Value) (value.Value, error) {
	f := m.flows[id]
	if f == nil || len(args) != len(f.Parameters()) {
		return nil, fmt.Errorf("invalid adapter flow invocation %q", id)
	}
	for i, p := range f.Parameters() {
		if args[i] == nil || !types.Equal(p.Type, args[i].Type()) {
			return nil, fmt.Errorf("invalid adapter argument %s", p.Name)
		}
	}
	v, err := m.invoke(id, args, f.Span())
	if err != nil {
		return nil, err
	}
	return published(v)
}
func published(v value.Value) (value.Value, error) {
	if f, ok := v.(value.Fallible); ok {
		_, cause := f.Outcome()
		if cause != nil {
			return nil, failure{cause}
		}
	}
	return v, nil
}
func (m *machine) step() error {
	if err := m.ctx.Err(); err != nil {
		return hostError{diagnostic.RuntimeCancelled, "execution cancelled", err}
	}
	if m.remaining == 0 {
		return Limit("evaluation steps")
	}
	m.remaining--
	return nil
}
func (m *machine) body(body []ir.Statement, f frame) (value.Value, error) {
	for _, s := range body {
		if err := m.step(); err != nil {
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
			if err := m.boundary.Commit(m.ctx, v.(value.Target)); err != nil {
				if host := boundaryHostError(err); host != nil {
					return nil, host
				}
				return nil, failure{activationError(v.(value.Target), err, x.Span())}
			}
		default:
			panic("unknown checked statement")
		}
	}
	return nil, nil
}
func (m *machine) expression(expr ir.Expression, f frame) (value.Value, error) {
	if err := m.step(); err != nil {
		return nil, err
	}
	v, err := m.evaluate(expr, f)
	if err != nil {
		return nil, err
	}
	if v.Weight() > MaxValueWeight {
		return nil, Limit("expanded value bytes")
	}
	if v.Weight() > MaxEvaluationWeight-m.weight {
		return nil, Limit("evaluation value bytes")
	}
	m.weight += v.Weight()
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
			return m.integer(n)
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
				return nil, failure{cause}
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
			for _, r := range v.(value.Text).Text() {
				if len(items) == MaxItems {
					return nil, Limit("list elements")
				}
				items = append(items, must(value.NewText(types.Scalar(), string(r))))
			}
			return must(value.NewList(x.Type().(types.List), items)), nil
		default:
			panic("unknown checked conversion")
		}
	case ir.FlowCall:
		args := make([]value.Value, len(x.Arguments()))
		for _, a := range x.Arguments() {
			v, err := m.expression(a.Value(), f)
			if err != nil {
				return nil, err
			}
			args[a.Position()] = v
		}
		return m.invoke(x.Flow(), args, x.Span())
	case ir.BuiltinCall:
		args := make([]value.Value, len(x.Arguments()))
		flows := map[int]ir.FlowID{}
		for _, a := range x.Arguments() {
			switch a := a.(type) {
			case ir.ValueArgument:
				v, err := m.expression(a.Value(), f)
				if err != nil {
					return nil, err
				}
				args[a.Position()] = v
			case ir.FlowArgument:
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
func (m *machine) invoke(id ir.FlowID, args []value.Value, span source.Span) (value.Value, error) {
	if m.depth == MaxDepth {
		return nil, Limit("flow depth")
	}
	m.depth++
	defer func() { m.depth-- }()
	f := m.flows[id]
	if f == nil {
		panic("absent checked flow")
	}
	bindings := map[ir.BindingID]value.Value{}
	fields := make([]value.Field, len(args))
	for i, p := range f.Parameters() {
		bindings[p.ID] = args[i]
		fields[i] = value.Field{Name: p.Name, Value: args[i]}
	}
	result, err := m.body(f.Body(), frame{f.Name(), bindings})
	var cause value.Error
	if err != nil {
		if e, ok := err.(Failure); ok {
			cause = e.Value()
		} else {
			return nil, err
		}
	}
	if v, ok := result.(value.Fallible); ok {
		_, cause = v.Outcome()
	}
	if cause == nil {
		return result, nil
	}
	cause = value.WithFrame(cause, f.Name(), cause.Operation(), must(value.NewRecord(fields)))
	if f.Handler() != "" {
		return m.invoke(f.Handler(), []value.Value{cause}, span)
	}
	out, ok := f.Signature().Output().(types.Fallible)
	if !ok {
		panic("unhandled failure in checked total flow")
	}
	cause = value.At(cause, f.Name(), span)
	return must(value.Failure(out, cause)), nil
}
func (m *machine) integer(n *big.Int) (value.Value, error) {
	if n.BitLen() > MaxIntegerBits {
		return nil, Limit("integer bits")
	}
	return must(value.NewInt(n)), nil
}
func (m *machine) list(t types.List, items []value.Value) (value.Value, error) {
	if len(items) > MaxItems {
		return nil, Limit("list elements")
	}
	return must(value.NewList(t, items)), nil
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
