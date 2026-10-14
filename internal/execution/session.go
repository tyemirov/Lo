package execution

import (
	"context"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Session owns the logical resources of one top-level run or HTTP request.
type Session interface {
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
	ctx               context.Context
	boundary          Boundary
	remaining, weight uint64
	depth             int
}

func (*session) session() {}
func NewSession(ctx context.Context, boundary Boundary, budget Budget) (Session, error) {
	if ctx == nil || boundary == nil || budget == nil {
		return nil, fmt.Errorf("execution requires context, boundary, and budget")
	}
	return &session{ctx: ctx, boundary: boundary, remaining: budget.Steps()}, nil
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
