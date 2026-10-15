// Package device owns authorized device sessions and invocation storage.
package device

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

const (
	CPU                         = "cpu"
	Capability                  = "Device.use"
	Mismatch                    = "Tensor.DeviceMismatch"
	SimulationProfile           = "cuda-sim-fp32"
	CUDAProfile                 = "cuda-fp32-pedantic"
	MetalTransferProfile        = "metal-transfer-fp32"
	EventOpen                   = "open"
	EventAllocate               = "allocate"
	EventTransfer               = "transfer"
	EventRelease                = "release"
	EventClose                  = "close"
	CUDA                        = "cuda:0"
	Simulation                  = "cuda-sim:0"
	Metal                       = "metal:0"
	Unavailable                 = "Device.Unavailable"
	AllocationFailed            = "Device.AllocationFailed"
	TransferFailed              = "Device.TransferFailed"
	UnsupportedOperation        = "Device.UnsupportedOperation"
	UnsupportedProfile          = "Device.UnsupportedProfile"
	LibraryFailed               = "Device.LibraryFailed"
	Lost                        = "Device.Lost"
	MaxStorage           uint64 = 64 << 20
	MaxTape              uint64 = 32 << 20
	MaxOptimizer         uint64 = 32 << 20
	MaxWorkspace         uint64 = 16 << 20
)

// Fault is a recoverable failure at a backend boundary.
type Fault interface {
	error
	Kind() string
	fault()
}
type fault struct {
	kind  string
	cause error
}

func (*fault) fault()          {}
func (f *fault) Kind() string  { return f.kind }
func (f *fault) Error() string { return f.kind + ": " + f.cause.Error() }
func (f *fault) Unwrap() error { return f.cause }

// NewFault validates a recoverable backend failure at the adapter boundary.
func NewFault(kind string, cause error) (Fault, error) {
	if cause == nil {
		return nil, fmt.Errorf("device failure requires a cause")
	}
	switch kind {
	case Unavailable, AllocationFailed, TransferFailed, UnsupportedOperation, UnsupportedProfile, LibraryFailed, Lost, Mismatch:
		return &fault{kind, cause}, nil
	default:
		return nil, fmt.Errorf("invalid device failure kind %q", kind)
	}
}

// BudgetError reports host resource exhaustion.
type BudgetError interface {
	error
	budgetError()
}
type budgetError struct{ subject string }

func (*budgetError) budgetError()    {}
func (e *budgetError) Error() string { return "device resource limit: " + e.subject }

// Buffer is an opaque allocation; transfers preserve FP32 bits.
type Buffer interface {
	Bytes() uint64
	Upload(context.Context, []float32) error
	Download(context.Context, []float32) error
	Close() error
}

// Backend owns one invocation's device context.
type Backend interface {
	Allocate(context.Context, uint64) (Buffer, error)
	Identity() Identity
	Close() error
}

// Identity describes the opened backend without tensor data.
type Identity struct {
	Name         string `json:"name"`
	Profile      string `json:"profile"`
	RegistryID   string `json:"registry_id,omitempty"`
	KernelSHA256 string `json:"kernel_sha256,omitempty"`
}

// Factory opens only the explicitly selected backend after authorization.
type Factory interface {
	Open(context.Context, string) (Backend, error)
}

// Receipt records resource lifetime without tensor contents.
type Receipt struct {
	Event   string    `json:"event"`
	ID      uint64    `json:"allocation"`
	Device  string    `json:"device"`
	Bytes   uint64    `json:"bytes"`
	Live    uint64    `json:"live_bytes"`
	Backend *Identity `json:"backend,omitempty"`
}
type Observer func(Receipt)

// Config is the validated grant and backend dependency for one invocation.
type Config interface {
	Grant() string
	config()
	settings() configuration
}
type configuration struct {
	grant    string
	factory  Factory
	observer Observer
}

func (configuration) config()                   {}
func (c configuration) settings() configuration { return c }
func NewConfig(grant string, factory Factory, observer Observer) (Config, error) {
	if grant != "" && grant != CUDA && grant != Simulation && grant != Metal {
		return nil, fmt.Errorf("invalid device grant %q", grant)
	}
	if factory == nil {
		return nil, fmt.Errorf("device configuration requires a backend factory")
	}
	return configuration{grant, factory, observer}, nil
}
func DefaultConfig() Config { return configuration{factory: DefaultFactory()} }

// DefaultFactory dispatches only to the explicitly selected backend.
func DefaultFactory() Factory { return buildFactory{} }

type buildFactory struct{}

func (buildFactory) Open(ctx context.Context, selector string) (Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selector == Metal {
		return openMetal(ctx)
	}
	return (SoftwareFactory{}).Open(ctx, selector)
}

// SoftwareFactory implements explicit simulation and rejects other selectors.
type SoftwareFactory struct{}

func (SoftwareFactory) Open(ctx context.Context, selector string) (Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selector != Simulation {
		return nil, &fault{Unavailable, fmt.Errorf("backend %s is not present in this build", selector)}
	}
	return &softwareBackend{}, nil
}

type softwareBackend struct{}

func (*softwareBackend) Identity() Identity {
	return Identity{Name: "software simulation", Profile: SimulationProfile}
}
func (*softwareBackend) Close() error { return nil }
func (*softwareBackend) Allocate(ctx context.Context, bytes uint64) (Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &softwareBuffer{values: make([]float32, bytes/4)}, nil
}

type softwareBuffer struct{ values []float32 }

func (b *softwareBuffer) Bytes() uint64 { return uint64(len(b.values)) * 4 }
func (b *softwareBuffer) Upload(ctx context.Context, values []float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copy(b.values, values)
	return ctx.Err()
}
func (b *softwareBuffer) Download(ctx context.Context, values []float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copy(values, b.values)
	return ctx.Err()
}
func (b *softwareBuffer) Close() error { b.values = nil; return nil }

// Class selects an additional storage budget. All classes count in MaxStorage.
type Class uint8

const (
	TensorStorage Class = iota
	TapeStorage
	OptimizerStorage
	WorkspaceStorage
)

// allocation is a session-owned storage receipt. Host elements stay valid after
// publication transfers ownership to the caller.
type allocation struct {
	owner    *session
	id       uint64
	location string
	bytes    uint64
	host     []float32
	buffer   Buffer
	scope    *scope
	classes  [4]bool
}

func (a *allocation) Bytes() uint64    { return a.bytes }
func (a *allocation) Location() string { return a.location }
func (a *allocation) Host() []float32  { return a.host }

// session owns the resource ledger and lazy backend for one run or request.
type session struct {
	config      configuration
	backend     Backend
	next, live  uint64
	classBytes  [4]uint64
	current     *scope
	allocations map[uint64]*allocation
	cleanup     error
	lost        error
	closed      bool
}

func NewSession(config Config) (Session, error) {
	if config == nil {
		return nil, fmt.Errorf("device session requires configuration")
	}
	s := &session{config: config.settings()}
	s.current = &scope{session: s}
	return s, nil
}
func (s *session) Granted(selector string) bool { return selector == CPU || selector == s.config.grant }
func (s *session) emit(event string, allocation *allocation) {
	if s.config.observer == nil {
		return
	}
	r := Receipt{Event: event, Live: s.live}
	if allocation != nil {
		r.ID = allocation.id
		r.Device = allocation.location
		r.Bytes = allocation.bytes
	}
	s.config.observer(r)
}
func (s *session) Open(ctx context.Context, selector string) error {
	if !s.Granted(selector) {
		return fmt.Errorf("unauthorized internal device request %s", selector)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.lost != nil {
		return s.lost
	}
	if s.backend != nil {
		return nil
	}
	backend, err := s.config.factory.Open(ctx, selector)
	if err != nil {
		return s.backendFault(ctx, Unavailable, err)
	}
	if backend == nil {
		return &fault{Unavailable, fmt.Errorf("backend factory returned no device")}
	}
	s.backend = backend
	if s.config.observer != nil {
		identity := backend.Identity()
		s.config.observer(Receipt{Event: EventOpen, Device: selector, Live: s.live, Backend: &identity})
	}
	return nil
}
func (s *session) Allocate(ctx context.Context, location string, bytes uint64) (Allocation, error) {
	if s.closed {
		return nil, fmt.Errorf("allocate after device session close")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bytes%4 != 0 {
		return nil, fmt.Errorf("FP32 allocation size is not divisible by four")
	}
	if bytes > MaxStorage-s.live {
		return nil, &budgetError{"live tensor storage"}
	}
	a := &allocation{owner: s, location: location, bytes: bytes, scope: s.current}
	if location == CPU {
		a.host = make([]float32, bytes/4)
	} else {
		if err := s.Open(ctx, location); err != nil {
			return nil, err
		}
		buffer, err := s.backend.Allocate(ctx, bytes)
		if err != nil {
			return nil, s.backendFault(ctx, AllocationFailed, err)
		}
		if buffer == nil {
			return nil, &fault{AllocationFailed, fmt.Errorf("device allocation returned no buffer")}
		}
		if buffer.Bytes() < bytes {
			s.cleanup = errors.Join(s.cleanup, buffer.Close())
			return nil, &fault{AllocationFailed, fmt.Errorf("backend reserved %d bytes for %d requested bytes", buffer.Bytes(), bytes)}
		}
		if buffer.Bytes() > MaxStorage-s.live {
			closeErr := buffer.Close()
			if closeErr != nil {
				s.cleanup = errors.Join(s.cleanup, closeErr)
			}
			return nil, &budgetError{"reserved device storage"}
		}
		a.bytes, a.buffer = buffer.Bytes(), buffer
	}
	s.next++
	a.id = s.next
	s.live += a.bytes
	if s.allocations == nil {
		s.allocations = map[uint64]*allocation{}
	}
	s.allocations[a.id] = a
	s.current.own(a)
	s.emit(EventAllocate, a)
	return a, nil
}
func (s *session) backendFault(ctx context.Context, kind string, err error) error {
	var failure Fault
	isFault := errors.As(err, &failure)
	if isFault && failure.Kind() == Lost {
		s.lost = err
	}
	// A boundary can fail while cancellation is pending. Cancellation remains
	// a host diagnostic and cannot enter a Lo error handler.
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	if isFault {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &fault{kind, err}
}
func (s *session) Transfer(ctx context.Context, from, to Allocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.lost != nil {
		return s.lost
	}
	source, target := from.receipt(), to.receipt()
	if source.owner != s || target.owner != s {
		return fmt.Errorf("transfer allocation belongs to another invocation")
	}
	var err error
	if source.location == CPU && target.buffer != nil {
		err = target.buffer.Upload(ctx, source.host)
	} else if target.location == CPU && source.buffer != nil {
		err = source.buffer.Download(ctx, target.host)
	} else {
		return fmt.Errorf("invalid transfer allocation pair")
	}
	if err != nil {
		return s.backendFault(ctx, TransferFailed, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	s.emit(EventTransfer, target)
	return nil
}

// CheckBudget checks additional storage before an operation allocates it.
func (s *session) CheckBudget(class Class, additional uint64) error {
	limits := [4]uint64{MaxStorage, MaxTape, MaxOptimizer, MaxWorkspace}
	names := [4]string{"tensor", "tape", "optimizer", "workspace"}
	if class > WorkspaceStorage {
		return fmt.Errorf("invalid storage class")
	}
	used := s.classBytes[class]
	if class == TensorStorage {
		used = s.live
	}
	if additional > limits[class]-used {
		return &budgetError{names[class] + " storage"}
	}
	return nil
}
func (s *session) Classify(allocations []Allocation, class Class) error {
	if err := s.CheckBudget(class, 0); err != nil {
		return err
	}
	unique := map[*allocation]bool{}
	var extra uint64
	for _, receipt := range allocations {
		a := receipt.receipt()
		if !a.classes[class] && !unique[a] {
			unique[a] = true
			extra += a.bytes
		}
	}
	if err := s.CheckBudget(class, extra); err != nil {
		return err
	}
	for a := range unique {
		a.classes[class] = true
	}
	s.classBytes[class] += extra
	return nil
}
func (s *session) Unclassify(allocations []Allocation, class Class) {
	for _, receipt := range allocations {
		a := receipt.receipt()
		if a.classes[class] {
			a.classes[class] = false
			s.classBytes[class] -= a.bytes
		}
	}
}
func (s *session) Release(receipt Allocation) {
	a := receipt.receipt()
	if _, ok := s.allocations[a.id]; !ok {
		return
	}
	if a.buffer != nil {
		s.cleanup = errors.Join(s.cleanup, a.buffer.Close())
	}
	delete(s.allocations, a.id)
	delete(a.scope.allocations, a.id)
	s.live -= a.bytes
	for class, active := range a.classes {
		if active {
			s.classBytes[class] -= a.bytes
			a.classes[class] = false
		}
	}
	s.emit(EventRelease, a)
}
func (s *session) Close() error {
	if s.closed {
		return s.cleanup
	}
	ids := make([]uint64, 0, len(s.allocations))
	for id := range s.allocations {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		s.Release(s.allocations[id])
	}
	if s.backend != nil {
		s.cleanup = errors.Join(s.cleanup, s.backend.Close())
		s.backend = nil
	}
	s.closed = true
	s.emit(EventClose, nil)
	return s.cleanup
}

// scope releases allocations that cannot escape a flow or callback loop.
type scope struct {
	session     *session
	parent      *scope
	allocations map[uint64]*allocation
}

func (s *session) Scope() Scope {
	scope := &scope{session: s, parent: s.current}
	s.current = scope
	return scope
}
func (scope *scope) own(a *allocation) {
	if scope.allocations == nil {
		scope.allocations = map[uint64]*allocation{}
	}
	scope.allocations[a.id] = a
	a.scope = scope
}
func (scope *scope) Empty() bool { return len(scope.allocations) == 0 }
func (scope *scope) Keep(roots []Allocation) {
	keep := map[*allocation]bool{}
	for _, receipt := range roots {
		keep[receipt.receipt()] = true
	}
	ids := make([]uint64, 0, len(scope.allocations))
	for id := range scope.allocations {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		a := scope.allocations[id]
		if !keep[a] {
			scope.session.Release(a)
		}
	}
}

// Retain moves an accumulated callback result into its enclosing scope.
func (scope *scope) Retain(roots []Allocation) {
	for _, receipt := range roots {
		a := receipt.receipt()
		if a.scope == scope {
			delete(scope.allocations, a.id)
			scope.parent.own(a)
		}
	}
}
func (scope *scope) Close(roots []Allocation) {
	scope.Keep(roots)
	scope.Retain(roots)
	scope.session.current = scope.parent
}

func (c configuration) Grant() string { return c.grant }

// Allocation identifies validated, session-owned storage.
type Allocation interface {
	Bytes() uint64
	Location() string
	Host() []float32
	receipt() *allocation
}

func (a *allocation) receipt() *allocation { return a }

// Session owns an invocation's lazy backend and resource ledger.
type Session interface {
	Granted(string) bool
	Allocate(context.Context, string, uint64) (Allocation, error)
	Transfer(context.Context, Allocation, Allocation) error
	CheckBudget(Class, uint64) error
	Classify([]Allocation, Class) error
	Unclassify([]Allocation, Class)
	Release(Allocation)
	Scope() Scope
	Close() error
	session()
}

func (*session) session() {}

// Scope releases allocations that do not escape the current execution region.
type Scope interface {
	Empty() bool
	Retain([]Allocation)
	Keep([]Allocation)
	Close([]Allocation)
	scope()
}

func (*scope) scope() {}
