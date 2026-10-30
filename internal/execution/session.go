package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Session owns the logical resources of one top-level run or HTTP request.
type Session interface {
	FileSession() value.FileSession
	Limits() Limits
	RegisterFileLease(value.FileLease, func() error) error
	ReserveWorklist(int) error
	ReleaseWorklist(int)
	ReserveFile(uint64, uint64) error
	RecordRemoval(value.List, value.Record)
	RecordActivation(string, source.Span, value.Value) error
	RecordFlowProgress(int, int, value.Value) (FlowProgress, error)
	RecordWorklistProgress(int, int, value.Value) (FlowProgress, error)
	OnClose(func() error) error
	Close() error
	Resources() device.Session
	Scope() Scope
	Context() context.Context
	Boundary() Boundary
	Step() error
	ChargeValue(value.Value) error
	CheckCleanup(value.Error) error
	TensorBudget(uint64, uint64) error
	Enter() error
	Leave()
	session()
	preserveFailure(error) error
}
type session struct {
	resources             device.Session
	ctx                   context.Context
	done                  <-chan struct{}
	boundary              Boundary
	remaining, live, data uint64
	limits                Limits
	values                *ownership[uint64]
	graphs                valueGraphs
	leases                *ownership[value.FileLease]
	leaseClosers          map[value.FileLease]func() error
	pendingCleanup        error
	cleanup               error
	progressWeight        uint64
	depth                 int
	fileSession           value.FileSession
	closeHooks            []func() error
	closed                bool
	removalEntries        value.List
	removalProgress       value.Record
	flowProgress          FlowProgress
	lastActivation        ActivationProgress
}

func (*session) session() {}
func NewSession(ctx context.Context, boundary Boundary, budget Budget, config device.Config) (Session, error) {
	if ctx == nil || boundary == nil || budget == nil {
		return nil, fmt.Errorf("execution requires context, boundary, and budget")
	}
	ctx = CaptureProcessContext(ctx)
	ctx = CaptureHostInvocation(ctx, nil)
	resources, err := device.NewSession(config)
	if err != nil {
		return nil, err
	}
	s := &session{done: ctx.Done(), boundary: boundary, remaining: budget.Steps(), limits: budget.Limits(), resources: resources, fileSession: value.NewFileSession(), leaseClosers: map[value.FileLease]func() error{}}
	s.values = newOwnership(func(identity uint64, weight uint64) {
		s.graphs.remove(identity)
		s.live -= weight
	})
	s.leases = newOwnership(func(lease value.FileLease, _ uint64) {
		close := s.leaseClosers[lease]
		delete(s.leaseClosers, lease)
		if close != nil {
			if err := close(); err != nil {
				fault := HostFailure("close", "file observation", err)
				s.cleanup = errors.Join(s.cleanup, fault)
				s.pendingCleanup = errors.Join(s.pendingCleanup, fault)
			}
		}
	})
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
func (s *session) Limits() Limits           { return s.limits }

// contextError avoids walking immutable context wrappers for each value.
func (s *session) contextError() error {
	select {
	case <-s.done:
		return s.ctx.Err()
	default:
		return nil
	}
}
func (s *session) Step() error {
	if s.cleanup != nil {
		return s.cleanupFailure(nil)
	}
	if err := s.contextError(); err != nil {
		return hostError{diagnostic.RuntimeCancelled, "execution cancelled", err}
	}
	if s.remaining == 0 {
		return Limit("evaluation steps")
	}
	s.remaining--
	return nil
}

// CheckCleanup stops callback handling after a terminal resource-release failure.
func (s *session) CheckCleanup(cause value.Error) error {
	if s.cleanup == nil {
		return nil
	}
	return s.cleanupFailure(cause)
}
func (s *session) cleanupFailure(v value.Value) error {
	cleanup := s.pendingCleanup
	s.pendingCleanup = nil
	if cleanup == nil {
		cleanup = s.cleanup
	}
	var cause value.Error
	if failure, ok := v.(value.Fallible); ok {
		_, cause = failure.Outcome()
	} else {
		cause, _ = v.(value.Error)
	}
	if cause != nil {
		return PreserveCleanup(Failed(cause), cleanup)
	}
	return cleanup
}
func exceeds(limit, current, added uint64) bool {
	return added > ^uint64(0)-current || (limit != 0 && (current > limit || added > limit-current))
}
func (s *session) preflight(weight uint64) error {
	if s.cleanup != nil {
		return s.cleanupFailure(nil)
	}
	if err := s.contextError(); err != nil {
		return BoundaryHostError(err)
	}
	if exceeds(s.limits.ValueBytes, 0, weight) {
		return Limit("expanded value bytes")
	}
	if exceeds(s.limits.LiveBytes, s.live, weight) {
		return Limit("live value bytes")
	}
	if exceeds(s.limits.DataBytes, s.data, weight) {
		return Limit("cumulative data bytes")
	}
	return nil
}
func (s *session) ChargeValue(v value.Value) error {
	if v == nil {
		return nil
	}
	if s.cleanup != nil {
		return s.cleanupFailure(v)
	}
	if err := s.contextError(); err != nil {
		return BoundaryHostError(err)
	}
	weight := v.Weight()
	id := value.Identity(v)
	if s.values.lookup(id) != nil {
		return nil
	}
	if limit := s.limits.ListItems; limit != 0 {
		if s.maximumListWidth(v) > limit {
			return preserveValueFlowProgress(Limit("list elements"), v)
		}
	}
	added := weight
	if exceeds(s.limits.ValueBytes, 0, weight) {
		return preserveValueFlowProgress(Limit("expanded value bytes"), v)
	}
	if exceeds(s.limits.LiveBytes, s.live, added) {
		return preserveValueFlowProgress(Limit("live value bytes"), v)
	}
	if exceeds(s.limits.DataBytes, s.data, weight) {
		return preserveValueFlowProgress(Limit("cumulative data bytes"), v)
	}
	s.data += weight
	if added != 0 {
		s.live += weight
		s.values.add(id, weight)
	}
	return nil
}
func (s *session) TensorBudget(steps, weight uint64) error {
	if err := s.preflight(weight); err != nil {
		return err
	}
	if steps > s.remaining {
		return Limit("tensor work steps")
	}
	s.remaining -= steps
	return nil
}

// RegisterFileLease attaches an opened observation to the current scope.
func (s *session) RegisterFileLease(lease value.FileLease, close func() error) error {
	if s.closed || close == nil {
		return fmt.Errorf("closed execution or absent observation cleanup")
	}
	s.leaseClosers[lease] = close
	s.leases.add(lease, 0)
	return nil
}

// ReserveWorklist accounts for pending item slots and pin receipts.
func (s *session) ReserveWorklist(count int) error {
	weight := uint64(count) * 16
	if s.cleanup != nil {
		return s.cleanupFailure(nil)
	}
	if err := s.contextError(); err != nil {
		return BoundaryHostError(err)
	}
	if exceeds(s.limits.LiveBytes, s.live, weight) {
		return Limit("live value bytes")
	}
	s.live += weight
	return nil
}

// ReleaseWorklist releases pending slots after consumption or discard.
func (s *session) ReleaseWorklist(count int) { s.live -= uint64(count) * 16 }
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
	Pin(value.Value) (func(), error)
	scope()
}
type resourceScope struct {
	session *session
	owner   device.Scope
	values  ownershipScope[uint64]
	leases  ownershipScope[value.FileLease]
	closed  bool
}

func (*resourceScope) scope() {}
func (s *session) Scope() Scope {
	scope := &resourceScope{session: s, owner: s.resources.Scope()}
	s.values.enter(&scope.values)
	s.leases.enter(&scope.leases)
	return scope
}
func valueRoots(values ...value.Value) []uint64 {
	var roots []uint64
	value.Visit(values, func(v value.Value) { roots = append(roots, value.Identity(v)) })
	return roots
}
func (scope *resourceScope) Close(v value.Value) {
	if scope.closed {
		return
	}
	scope.closed = true
	var allocations []device.Allocation
	if !scope.owner.Empty() {
		allocations = value.Allocations(v)
	}
	scope.owner.Close(allocations)
	scope.values.releasePins()
	if len(scope.values.candidates) != 0 {
		scope.visitValues([]value.Value{v}, true)
	}
	scope.values.close()
	scope.leases.releasePins()
	if len(scope.leases.candidates) != 0 {
		for _, lease := range value.FileLeases(v) {
			scope.leases.retainKey(lease)
		}
	}
	scope.leases.close()
}
func (scope *resourceScope) Keep(values ...value.Value) {
	if !scope.owner.Empty() {
		scope.owner.Keep(value.Allocations(values...))
	}
	if len(scope.values.candidates) != 0 {
		scope.visitValues(values, false)
		scope.values.keep()
	}
	if len(scope.leases.candidates) != 0 {
		for _, lease := range value.FileLeases(values...) {
			scope.leases.keepKey(lease)
		}
		scope.leases.keep()
	}
}
func (scope *resourceScope) Retain(v value.Value) {
	if !scope.owner.Empty() {
		scope.owner.Retain(value.Allocations(v))
	}
	if len(scope.values.candidates) != 0 || len(scope.values.pins) != 0 {
		scope.visitValues([]value.Value{v}, true)
	}
	if len(scope.leases.candidates) != 0 || len(scope.leases.pins) != 0 {
		for _, lease := range value.FileLeases(v) {
			scope.leases.retainKey(lease)
		}
	}
}
func (scope *resourceScope) Pin(v value.Value) (func(), error) {
	if scope.session.cleanup != nil {
		return nil, scope.session.cleanup
	}
	if err := scope.session.contextError(); err != nil {
		return nil, BoundaryHostError(err)
	}
	if exceeds(scope.session.limits.ValueBytes, 0, v.Weight()) {
		return nil, Limit("expanded value bytes")
	}
	idValue := value.Identity(v)
	if scope.session.values.lookup(idValue) == nil {
		weight := v.Weight()
		if exceeds(scope.session.limits.LiveBytes, scope.session.live, weight) {
			return nil, Limit("live value bytes")
		}
		scope.session.live += weight
		scope.session.values.add(idValue, weight)
	}
	releaseDevice := scope.owner.Pin(value.Allocations(v))
	releaseValues := scope.values.pin(valueRoots(v))
	releaseLeases := releaseEmptyPin
	if scope.session.leases.count != 0 {
		releaseLeases = scope.leases.pin(value.FileLeases(v))
	}
	return func() {
		releaseDevice()
		releaseValues()
		releaseLeases()
	}, nil
}

type sessionContextKey struct{}

// SessionFromContext returns the execution that owns boundary resources.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionContextKey{}).(Session)
	return s, ok
}
func (s *session) FileSession() value.FileSession { return s.fileSession }
func (s *session) ReserveFile(steps, weight uint64) error {
	if err := s.preflight(weight); err != nil {
		return err
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
	s.graphs = valueGraphs{}
	var failures []error
	for lease := range s.leaseClosers {
		s.leases.drop(lease)
	}
	if s.pendingCleanup != nil {
		failures = append(failures, s.pendingCleanup)
		s.pendingCleanup = nil
	}
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
		return WithActivationProgress(WithFlowProgress(err, s.flowProgress), s.lastActivation)
	}
	return err
}

func (s *session) RecordRemoval(entries value.List, progress value.Record) {
	s.removalEntries = entries
	s.removalProgress = progress
}

func (s *session) preserveFailure(err error) error {
	if err == nil {
		return err
	}
	if ActivationDetails(err) == nil {
		err = WithActivationProgress(err, s.lastActivation)
	}
	if BoundaryHostError(err) == nil {
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
