package execution

import (
	"context"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"slices"
)

type Function func(Session, []value.Value, source.Span) (value.Value, error)
type Handler struct {
	ID         identity.FlowID
	Name       string
	Parameters []types.Parameter
	Span       source.Span
	Call       Function
}

// Program is the sealed adapter contract for either execution backend.
type Program interface {
	Run(context.Context, Boundary, Budget, device.Config) (value.Value, error)
	NewInvocation(context.Context, Boundary, Budget, device.Config) (Invocation, error)
	program()
}
type program struct {
	body     func(Session) (value.Value, error)
	handlers map[identity.FlowID]Handler
}

func (*program) program() {}
func NewProgram(body func(Session) (value.Value, error), handlers []Handler) (Program, error) {
	if body == nil {
		return nil, fmt.Errorf("program requires a top-level entry")
	}
	table := make(map[identity.FlowID]Handler, len(handlers))
	for _, h := range handlers {
		if h.ID == "" || h.Name == "" || h.Span == nil || h.Call == nil {
			return nil, fmt.Errorf("invalid handler metadata")
		}
		if _, ok := table[h.ID]; ok {
			return nil, fmt.Errorf("duplicate handler identity %s", h.ID)
		}
		for _, p := range h.Parameters {
			if p.Name == "" || p.Type == nil {
				return nil, fmt.Errorf("invalid handler parameter")
			}
		}
		h.Parameters = slices.Clone(h.Parameters)
		table[h.ID] = h
	}
	return &program{body, table}, nil
}
func (p *program) Run(ctx context.Context, b Boundary, budget Budget, config device.Config) (output value.Value, resultErr error) {
	s, err := NewSession(ctx, b, budget, config)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := s.Close(); err != nil {
			output = nil
			resultErr = PreserveCleanup(resultErr, err)
		}
		resultErr = s.preserveFailure(resultErr)
	}()
	v, err := p.body(s)
	if err != nil {
		return nil, err
	}
	return Published(v)
}

type Invocation interface {
	Close() error
	Invoke(identity.FlowID, []value.Value) (value.Value, error)
	invocation()
}
type invocation struct {
	session Session
	program *program
}

func (*invocation) invocation() {}
func (p *program) NewInvocation(ctx context.Context, b Boundary, budget Budget, config device.Config) (Invocation, error) {
	s, err := NewSession(ctx, b, budget, config)
	if err != nil {
		return nil, err
	}
	return &invocation{s, p}, nil
}
func (i *invocation) Invoke(id identity.FlowID, args []value.Value) (output value.Value, resultErr error) {
	defer func() { resultErr = i.session.preserveFailure(resultErr) }()
	h, ok := i.program.handlers[id]
	if !ok || len(args) != len(h.Parameters) {
		return nil, fmt.Errorf("invalid adapter flow invocation %q", id)
	}
	for index, p := range h.Parameters {
		if args[index] == nil || !types.Equal(p.Type, args[index].Type()) {
			return nil, fmt.Errorf("invalid adapter argument %s", p.Name)
		}
	}
	v, err := h.Call(i.session, args, h.Span)
	if err != nil {
		return nil, err
	}
	return Published(v)
}

func (i *invocation) Close() error { return i.session.Close() }
