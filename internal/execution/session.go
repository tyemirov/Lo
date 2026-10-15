package execution

import (
	"context"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Session owns the logical resources of one top-level run or HTTP request.
type Session interface {
	Resources() device.Session
	Scope() Scope
	Context() context.Context
	Boundary() Boundary
	Step() error
	ChargeValue(value.Value) error
	TensorBudget(uint64, uint64) error
	AddWeight(uint64)
	Enter() error
	Leave()
	session()
}
type session struct {
	resources         device.Session
	ctx               context.Context
	boundary          Boundary
	remaining, weight uint64
	depth             int
}

func (*session) session() {}
func NewSession(ctx context.Context, boundary Boundary, budget Budget, config device.Config) (Session, error) {
	if ctx == nil || boundary == nil || budget == nil {
		return nil, fmt.Errorf("execution requires context, boundary, and budget")
	}
	resources, err := device.NewSession(config)
	if err != nil {
		return nil, err
	}
	return &session{ctx: ctx, boundary: boundary, remaining: budget.Steps(), resources: resources}, nil
}
func (s *session) Context() context.Context { return s.ctx }
func (s *session) Boundary() Boundary       { return s.boundary }
func (s *session) Step() error {
	if err := s.ctx.Err(); err != nil {
		return hostError{diagnostic.RuntimeCancelled, "execution cancelled", err}
	}
	if s.remaining == 0 {
		return Limit("evaluation steps")
	}
	s.remaining--
	return nil
}
func (s *session) ChargeValue(v value.Value) error {
	weight := v.Weight()
	if weight > MaxValueWeight {
		return Limit("expanded value bytes")
	}
	if weight > MaxEvaluationWeight-s.weight {
		return Limit("evaluation value bytes")
	}
	s.weight += weight
	return nil
}
func (s *session) TensorBudget(steps, weight uint64) error {
	if err := s.ctx.Err(); err != nil {
		return BoundaryHostError(err)
	}
	if weight > MaxValueWeight {
		return Limit("expanded tensor result bytes")
	}
	if weight > MaxEvaluationWeight-s.weight {
		return Limit("evaluation tensor result bytes")
	}
	if steps > s.remaining {
		return Limit("tensor work steps")
	}
	s.remaining -= steps
	return nil
}
func (s *session) AddWeight(weight uint64) { s.weight += weight }
func (s *session) Enter() error {
	if s.depth == MaxDepth {
		return Limit("flow depth")
	}
	s.depth++
	return nil
}
func (s *session) Leave() { s.depth-- }

func (s *session) Resources() device.Session { return s.resources }

type Scope interface {
	Retain(value.Value)
	Close(value.Value)
	Keep(...value.Value)
	scope()
}
type resourceScope struct{ owner device.Scope }

func (resourceScope) scope()    {}
func (s *session) Scope() Scope { return resourceScope{s.resources.Scope()} }
func (s resourceScope) Close(v value.Value) {
	var roots []device.Allocation
	if !s.owner.Empty() {
		roots = value.Allocations(v)
	}
	s.owner.Close(roots)
}
func (s resourceScope) Keep(values ...value.Value) {
	if !s.owner.Empty() {
		s.owner.Keep(value.Allocations(values...))
	}
}
func (s resourceScope) Retain(v value.Value) {
	if !s.owner.Empty() {
		s.owner.Retain(value.Allocations(v))
	}
}
