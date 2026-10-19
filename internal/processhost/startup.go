// Package processhost owns Process sessions and retained startup resources.
package processhost

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type EventKind uint8

const (
	Ready EventKind = iota + 1
	Executed
	Failed
)

type Stage string

const (
	Creation    Stage = "creation"
	Confinement Stage = "confinement"
	Input       Stage = "input"
	Readiness   Stage = "readiness"
	Witness     Stage = "witness"
	Permission  Stage = "permit"
	Execution   Stage = "execution"
	Protocol    Stage = "protocol"
)

type Event struct {
	Kind  EventKind
	Stage Stage
}

// Primary owns startup resources, which can exist before OS creation. Its
// StartConfirmed reports actual primary creation, not merely cleanup ownership.
// Ready acknowledges installed
// confinement and validated command inputs. ArmExecution must install the witness
// before Permit. Permit is a prompt, nonblocking publication boundary: true means
// the affirmative permit was published, including when accompanied by an error.
// Executed is independent confirmation of target exec, never control-channel EOF.
// Before Wait returns, all related execution/failure events must already be queued
// on Events. The supervisor drains that finite queue, without waiting for closure.
// There are at most sixteen startup events. All inherited ProcessChild resource
// and context contracts apply; getters return stable owned channels.
type Primary interface {
	adapter.ProcessChild
	Events() <-chan Event
	ArmExecution(context.Context) error
	Permit(context.Context) (bool, error)
}

// Creator may settle after cancellation. Every nonnil Primary transfers ownership
// even with an error. Nil plus an error promises settled resource-free failure;
// nil/nil cannot establish absence and permanently retains unresolved ownership.
type Creator interface {
	Create(context.Context, adapter.ProcessInvocation) (Primary, error)
}

type Supervisor struct {
	mu          sync.Mutex
	creator     Creator
	active      *owner
	quarantined bool
}

func NewSupervisor(creator Creator) (*Supervisor, error) {
	if creator == nil {
		return nil, host("missing creator")
	}
	return &Supervisor{creator: creator}, nil
}

func host(message string) error {
	return execution.HostFailure("Process startup", "ownership", errors.New(message))
}

func failure(stage Stage) error { return errors.New("Process startup failed during " + string(stage)) }
func validStage(stage Stage) bool {
	switch stage {
	case Creation, Confinement, Input, Readiness, Witness, Permission, Execution, Protocol:
		return true
	}
	return false
}

// Start admits one retained owner before launching asynchronous OS creation.
// Busy admission rejects immediately; it neither queues nor creates another child.
func (s *Supervisor) Start(ctx context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if invocation == nil {
		return nil, host("missing invocation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quarantined || s.active != nil {
		return nil, host("executor unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	creationContext, cancel := context.WithCancel(ctx)
	o := &owner{supervisor: s, ctx: creationContext, executionContext: ctx, cancelContext: cancel, creationSettled: make(chan struct{}), created: make(chan struct{}), waited: make(chan struct{}), canceled: make(chan struct{})}
	o.input = &pendingWrite{newPendingPort(ctx, creationContext.Done())}
	o.output = &pendingRead{newPendingPort(ctx, creationContext.Done())}
	o.diagnostic = &pendingRead{newPendingPort(ctx, creationContext.Done())}
	s.active = o
	go o.create(invocation)
	return o, nil
}

func (s *Supervisor) quarantine() { s.mu.Lock(); s.quarantined = true; s.mu.Unlock() }
func (s *Supervisor) discharge(o *owner) {
	s.mu.Lock()
	if s.active == o {
		s.active = nil
	}
	s.mu.Unlock()
}

type releaseAttempt struct {
	done chan struct{}
	err  error
}
type stopAttempt struct {
	done chan struct{}
	err  error
}
type termination struct {
	value value.ProcessTermination
	err   error
}
type owner struct {
	supervisor                             *Supervisor
	ctx                                    context.Context
	executionContext                       context.Context
	cancelContext                          context.CancelFunc
	mu                                     sync.Mutex
	primary                                Primary
	created, waited, canceled              chan struct{}
	creationSettled                        chan struct{}
	cancelOnce                             sync.Once
	confirmed                              atomic.Bool
	unresolved, released, permit, executed bool
	finalizing                             bool
	startupErr, waitErr                    error
	termination                            value.ProcessTermination
	input                                  *pendingWrite
	output, diagnostic                     *pendingRead
	stopMu                                 sync.Mutex
	stop                                   *stopAttempt
	releaseMu                              sync.Mutex
	release                                *releaseAttempt
}

func (o *owner) stopPrimary(_ Primary) { _ = o.ensureStop(false) }

// One retained stop job waits for creation, including beyond caller deadlines.
// Its callback cannot overlap another stop job. An unresponsive OS operation
// leaves ownership/quarantine intact; this does not establish a lifetime bound.
func (o *owner) ensureStop(retry bool) *stopAttempt {
	o.stopMu.Lock()
	defer o.stopMu.Unlock()
	o.mu.Lock()
	released, finalizing := o.released, o.finalizing
	o.mu.Unlock()
	if released {
		result := &stopAttempt{done: make(chan struct{})}
		close(result.done)
		return result
	}
	job := o.stop
	if job != nil && retry && !finalizing {
		select {
		case <-job.done:
			if job.err != nil {
				job = nil
			}
		default:
		}
	}
	if job == nil {
		job = &stopAttempt{done: make(chan struct{})}
		o.stop = job
		go func() {
			<-o.creationSettled
			o.mu.Lock()
			primary, unresolved := o.primary, o.unresolved
			o.mu.Unlock()
			if unresolved {
				job.err = host("unresolved creation")
			} else if primary != nil {
				if err := primary.Stop(context.WithoutCancel(o.ctx)); err != nil {
					job.err = host("primary stop failed")
				}
			}
			if job.err != nil {
				o.supervisor.quarantine()
			}
			close(job.done)
		}()
	}
	return job
}

func (o *owner) StartConfirmed() bool {
	if o.confirmed.Load() {
		return true
	}
	o.mu.Lock()
	primary := o.primary
	o.mu.Unlock()
	if primary != nil && primary.StartConfirmed() {
		o.confirmed.Store(true)
	}
	return o.confirmed.Load()
}
func (o *owner) Stdin() io.WriteCloser { return o.input }
func (o *owner) Stdout() io.ReadCloser { return o.output }
func (o *owner) Stderr() io.ReadCloser { return o.diagnostic }

func (o *owner) markCanceled() {
	o.mu.Lock()
	o.cancelOnce.Do(func() { close(o.canceled) })
	o.cancelContext()
	o.mu.Unlock()
	o.ensureStop(false)
}

func (o *owner) failed(err error) {
	o.mu.Lock()
	if o.startupErr == nil {
		o.startupErr = err
	}
	o.mu.Unlock()
}

func (o *owner) closePorts() error {
	if errors.Join(o.input.Close(), o.output.Close(), o.diagnostic.Close()) != nil {
		return host("channel release failed")
	}
	return nil
}

func (o *owner) cancellationFailure() error {
	if err := o.executionContext.Err(); err != nil {
		return err
	}
	return failure(Execution)
}

// callbackFailure reconstructs only the closed public class and startup stage.
// It never retains a callback message, subject, or raw error chain.
func (o *owner) callbackFailure(err error, stage Stage) error {
	if err == nil {
		return nil
	}
	if original := o.executionContext.Err(); original != nil {
		return original
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if o.ctx.Err() != nil {
			return failure(stage)
		}
		return host("unexpected callback cancellation during " + string(stage))
	}
	var boundary execution.HostError
	if errors.As(err, &boundary) {
		switch boundary.Code() {
		case diagnostic.CapabilityDenied:
			return execution.Denied("Process startup", string(stage))
		case diagnostic.RuntimeLimit:
			return execution.Limit("Process startup " + string(stage))
		case diagnostic.ProcessUnavailable:
			return execution.ProcessUnavailable()
		default:
			return host("boundary failed during " + string(stage))
		}
	}
	return failure(stage)
}

func (o *owner) create(invocation adapter.ProcessInvocation) {
	p, err := o.supervisor.creator.Create(o.ctx, invocation)
	confirmed := p != nil && p.StartConfirmed()
	o.mu.Lock()
	o.primary = p
	if p != nil {
		o.confirmed.Store(confirmed)
	}
	if p == nil && err == nil {
		o.unresolved = true
		o.startupErr = host("unresolved creation")
	}
	if err != nil {
		o.startupErr = o.callbackFailure(err, Creation)
	}
	if executionErr := o.executionContext.Err(); executionErr != nil {
		o.waitErr = executionErr
	}
	o.mu.Unlock()
	close(o.creationSettled)
	if p == nil {
		if err == nil {
			o.supervisor.quarantine()
		}
		o.input.fail(failure(Creation))
		o.output.fail(failure(Creation))
		o.diagnostic.fail(failure(Creation))
		close(o.created)
		_ = o.closePorts()
		close(o.waited)
		return
	}
	terminal := make(chan termination, 1)
	// Reaping and stopping own the primary before channel binding/closure can
	// block. Such unresolved callbacks retain ownership and quarantine admission.
	go func() {
		result, waitErr := p.Wait(context.WithoutCancel(o.ctx))
		terminal <- termination{result, waitErr}
	}()
	input, output, diagnostic := p.Stdin(), p.Stdout(), p.Stderr()
	if input == nil || output == nil || diagnostic == nil {
		o.failed(host("missing primary channel"))
	}
	o.input.bind(input)
	o.output.bind(output)
	o.diagnostic.bind(diagnostic)
	close(o.created)
	if err != nil || input == nil || output == nil || diagnostic == nil || o.ctx.Err() != nil {
		o.stopPrimary(p)
	}
	o.protocol(p, terminal)
}

func (o *owner) event(p Primary, event Event, terminal bool) {
	if event.Kind != Failed && event.Stage != "" {
		o.failed(failure(Protocol))
		if !terminal {
			o.stopPrimary(p)
		}
		return
	}
	switch event.Kind {
	case Ready:
		o.mu.Lock()
		denied := o.permit || o.startupErr != nil || o.ctx.Err() != nil
		o.mu.Unlock()
		if terminal || denied {
			o.failed(failure(Protocol))
			if !terminal {
				o.stopPrimary(p)
			}
			return
		}
		if err := p.ArmExecution(o.ctx); err != nil {
			o.failed(o.callbackFailure(err, Witness))
			o.stopPrimary(p)
			return
		}
		// Permit is required to be prompt/nonblocking. This narrow publication
		// boundary shares the lock with cancellation; no creation/cleanup/IO
		// callback executes under it. Overlapping cancellation may lose this order.
		o.mu.Lock()
		if o.ctx.Err() != nil || o.startupErr != nil {
			if o.startupErr == nil {
				o.startupErr = o.cancellationFailure()
			}
			o.mu.Unlock()
			o.stopPrimary(p)
			return
		}
		published, err := p.Permit(o.ctx)
		o.permit = published
		if err != nil {
			o.startupErr = o.callbackFailure(err, Permission)
		} else if !published {
			o.startupErr = failure(Permission)
		}
		o.mu.Unlock()
		if err != nil || !published {
			o.stopPrimary(p)
		}
	case Executed:
		o.mu.Lock()
		invalid := !o.permit || o.executed
		if invalid {
			if o.startupErr == nil {
				o.startupErr = failure(Protocol)
			}
		} else {
			o.executed = true
		}
		o.mu.Unlock()
		if invalid && !terminal {
			o.stopPrimary(p)
		}
	case Failed:
		stage := event.Stage
		if !validStage(stage) {
			stage = Protocol
		}
		o.failed(failure(stage))
		if !terminal {
			o.stopPrimary(p)
		}
	default:
		o.failed(failure(Protocol))
		if !terminal {
			o.stopPrimary(p)
		}
	}
}

func (o *owner) protocol(p Primary, terminal <-chan termination) {
	events := p.Events()
	if events == nil {
		o.failed(failure(Protocol))
		o.stopPrimary(p)
	}
	canceled := o.ctx.Done()
	count := 0
	for {
		select {
		case <-canceled:
			o.markCanceled()
			o.failed(o.cancellationFailure())
			canceled = nil
			o.stopPrimary(p)
		case event, ok := <-events:
			if !ok {
				o.mu.Lock()
				executed := o.executed
				o.mu.Unlock()
				if !executed {
					o.failed(failure(Protocol))
					o.stopPrimary(p)
				}
				events = nil
				continue
			}
			count++
			if count > 16 {
				o.failed(failure(Protocol))
				events = nil
				o.stopPrimary(p)
				continue
			}
			o.event(p, event, false)
		case result := <-terminal:
			// Wait's contract puts terminal-related events in the queue first.
			// Drain only already-published evidence, never wait for channel close.
			draining := true
			for draining && count <= 16 {
				select {
				case event, ok := <-events:
					if !ok {
						draining = false
						break
					}
					count++
					o.event(p, event, true)
				default:
					draining = false
				}
			}
			o.mu.Lock()
			o.termination = result.value
			if result.value != nil {
				o.confirmed.Store(true)
			}
			if result.err != nil {
				o.waitErr = o.callbackFailure(result.err, Execution)
			}
			if result.value == nil && result.err == nil {
				o.waitErr = host("missing primary termination")
			}
			if count > 16 || !o.executed {
				if o.startupErr == nil {
					o.startupErr = failure(Execution)
				}
			}
			if o.ctx.Err() != nil && o.startupErr == nil {
				o.startupErr = o.cancellationFailure()
			}
			if executionErr := o.executionContext.Err(); executionErr != nil {
				o.waitErr = errors.Join(o.waitErr, executionErr)
			}
			o.mu.Unlock()
			close(o.waited)
			return
		}
	}
}

func (o *owner) Wait(ctx context.Context) (value.ProcessTermination, error) {
	select {
	case <-o.waited:
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.termination, errors.Join(o.startupErr, o.waitErr)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (o *owner) Stop(ctx context.Context) error {
	o.markCanceled()
	job := o.ensureStop(true)
	select {
	case <-job.done:
		return job.err
	case <-ctx.Done():
		o.supervisor.quarantine()
		return ctx.Err()
	}
}

func (o *owner) Release(ctx context.Context) error {
	o.releaseMu.Lock()
	o.mu.Lock()
	released := o.released
	o.mu.Unlock()
	if released {
		o.releaseMu.Unlock()
		return nil
	}
	attempt := o.release
	if attempt != nil {
		select {
		case <-attempt.done:
			if attempt.err != nil {
				attempt = nil
			}
		default:
		}
	}
	if attempt == nil {
		attempt = &releaseAttempt{done: make(chan struct{})}
		o.release = attempt
		// A caller deadline reports failure/quarantine but does not abandon this
		// one retained release job. It must clean up a primary arriving later.
		// This can outlive the caller and does not prove bounded OS lifetime.
		go func() { attempt.err = o.releaseOwned(context.WithoutCancel(ctx)); close(attempt.done) }()
	}
	o.releaseMu.Unlock()
	select {
	case <-attempt.done:
		if attempt.err != nil {
			o.supervisor.quarantine()
		}
		return attempt.err
	case <-ctx.Done():
		o.supervisor.quarantine()
		return ctx.Err()
	}
}

func (o *owner) releaseOwned(ctx context.Context) error {
	o.markCanceled()
	preCloseErr := o.closePorts()
	select {
	case <-o.created:
	case <-ctx.Done():
		return ctx.Err()
	}
	job := o.ensureStop(true)
	o.mu.Lock()
	p, unresolved := o.primary, o.unresolved
	o.finalizing = true
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		if !o.released {
			o.finalizing = false
		}
		o.mu.Unlock()
	}()
	if unresolved {
		return host("unresolved creation")
	}
	select {
	case <-job.done:
		if job.err != nil {
			return job.err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-o.waited:
	case <-ctx.Done():
		return ctx.Err()
	}
	closeErr := errors.Join(preCloseErr, o.closePorts())
	var joinErr error
	for _, port := range []*pendingPort{o.input.pendingPort, o.output.pendingPort, o.diagnostic.pendingPort} {
		if err := port.join(ctx); err != nil {
			joinErr = host("channel release failed")
		}
	}
	if p != nil {
		if err := p.Release(ctx); err != nil {
			return host("primary release failed")
		}
	}
	if closeErr != nil {
		return closeErr
	}
	if joinErr != nil {
		return joinErr
	}
	o.mu.Lock()
	o.released = true
	o.mu.Unlock()
	o.supervisor.discharge(o)
	return nil
}
