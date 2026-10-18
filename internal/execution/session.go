package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Session owns the logical resources of one top-level run or HTTP request.
type Session interface {
	FileSession() value.FileSession
	ReserveFile(uint64, uint64) error
	RecordRemoval(value.List, value.Record)
	RecordFlowProgress(int, int, value.Value) (FlowProgress, error)
	OnClose(func() error) error
	Close() error
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
	preserveFailure(error) error
}
type session struct {
	resources         device.Session
	ctx               context.Context
	boundary          Boundary
	remaining, weight uint64
	depth             int
	fileSession       value.FileSession
	closeHooks        []func() error
	closed            bool
	removalEntries    value.List
	removalProgress   value.Record
	flowProgress      FlowProgress
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
	s := &session{boundary: boundary, remaining: budget.Steps(), resources: resources, fileSession: value.NewFileSession()}
	if capture, ok := ctx.Value(failureCaptureKey{}).(*failureCapture); ok {
		if err := capture.bind(s); err != nil {
			return nil, errors.Join(err, resources.Close())
		}
	}
	s.ctx = context.WithValue(ctx, sessionContextKey{}, Session(s))
	return s, nil
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

type sessionContextKey struct{}

// SessionFromContext returns the execution that owns boundary resources.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionContextKey{}).(Session)
	return s, ok
}
func (s *session) FileSession() value.FileSession { return s.fileSession }
func (s *session) ReserveFile(steps, weight uint64) error {
	if err := s.ctx.Err(); err != nil {
		return BoundaryHostError(err)
	}
	if weight > MaxValueWeight {
		return Limit("expanded file value bytes")
	}
	if weight > MaxEvaluationWeight-s.weight {
		return Limit("evaluation file value bytes")
	}
	if steps > s.remaining {
		return Limit("file work steps")
	}
	s.remaining -= steps
	return nil
}
func (s *session) OnClose(close func() error) error {
	if s.closed || close == nil {
		return fmt.Errorf("closed execution or absent resource cleanup")
	}
	s.closeHooks = append(s.closeHooks, close)
	return nil
}
func (s *session) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var failures []error
	for index := len(s.closeHooks) - 1; index >= 0; index-- {
		if err := s.closeHooks[index](); err != nil {
			failures = append(failures, err)
		}
	}
	s.closeHooks = nil
	if err := s.resources.Close(); err != nil {
		failures = append(failures, err)
	}
	err := errors.Join(failures...)
	if err != nil {
		if s.removalProgress != nil {
			err = WithRemovalProgress(err, s.removalEntries, s.removalProgress)
		}
		return WithFlowProgress(err, s.flowProgress)
	}
	return err
}

func (s *session) RecordRemoval(entries value.List, progress value.Record) {
	s.removalEntries = entries
	s.removalProgress = progress
}

func (s *session) preserveFailure(err error) error {
	if err == nil || BoundaryHostError(err) == nil {
		return err
	}
	if _, _, ok := RemovalDetails(err); !ok && s.removalProgress != nil {
		err = WithRemovalProgress(err, s.removalEntries, s.removalProgress)
	}
	if len(FlowDetails(err)) == 0 {
		err = WithFlowProgress(err, s.flowProgress)
	}
	return err
}
