//go:build lo_process_qualification

package startup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/experiments/process-startup"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Public candidate surface: NewSupervisor(Creator), implementing ProcessDriver;
// Creator.Create(ctx, invocation) -> (Primary,error). Primary embeds ProcessChild
// and supplies Events, ArmExecution, Permit. Ready acknowledges confinement plus
// validated inputs; Executed is a distinct affirmative witness. Failed contains
// only a startup stage. Existing lifecycle contexts own time/capture limits.
type creatorFunc func(context.Context, adapter.ProcessInvocation) (startup.Primary, error)

func (f creatorFunc) Create(ctx context.Context, invocation adapter.ProcessInvocation) (startup.Primary, error) {
	return f(ctx, invocation)
}

type captureDriver struct{ invocation adapter.ProcessInvocation }

func (d *captureDriver) Start(_ context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
	d.invocation = invocation
	return nil, errors.New("fixture invocation captured")
}

type inertBoundary struct{}

func (inertBoundary) Read(context.Context, value.Text) (value.Bytes, error) {
	return nil, errors.New("unexpected read")
}
func (inertBoundary) List(context.Context, value.Text) (value.List, error) {
	return nil, errors.New("unexpected list")
}
func (inertBoundary) OpenPaths(context.Context, value.Text) (value.List, error) {
	return nil, errors.New("unexpected discovery")
}
func (inertBoundary) Commit(context.Context, value.Target) (value.Value, error) {
	return nil, errors.New("unexpected target")
}

func fixtureProgram(t *testing.T) execution.Program {
	return fixtureProgramWithInput(t, "stdin")
}

func fixtureProgramWithInput(t *testing.T, input string) execution.Program {
	t.Helper()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	code := fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(executable, List.new("argument"), Map.new(Record.new(key: "NAME", value: "explicit")), File.Path.new(%q), UTF8.Bytes.new(%q)); result = commit command; return result }; return Main()`, filepath.Join(root, "inert-executable"), root, input)
	text, err := source.New(filepath.Join(root, "fixture.lo"), code)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(text)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func invocation(t *testing.T) adapter.ProcessInvocation {
	t.Helper()
	driver := &captureDriver{}
	lifecycle, err := adapter.NewProcessLifecycle(driver, adapter.DefaultProcessLimits())
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithProcessLifecycle(inertBoundary{}, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := fixtureProgram(t).Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
	var failure execution.Failure
	if !errors.As(runErr, &failure) || driver.invocation == nil {
		t.Fatal("public lifecycle did not supply an invocation")
	}
	return driver.invocation
}

type discardInput struct{}

func (discardInput) Write(data []byte) (int, error) { return len(data), nil }
func (discardInput) Close() error                   { return nil }

type primary struct {
	events                          chan startup.Event
	permit                          chan struct{}
	exited                          chan struct{}
	stopped, reaped                 chan struct{}
	arm                             func(context.Context) error
	input                           io.WriteCloser
	output                          io.ReadCloser
	errorOutput                     io.ReadCloser
	beforeWaitReturn                func()
	waitFailure, permitFailure      error
	releaseHook                     func()
	mu                              sync.Mutex
	order                           []string
	exitOnce, permitOnce            sync.Once
	stopOnce                        sync.Once
	waits, stops, releases, permits atomic.Int32
	releaseFailure                  atomic.Bool
}

func newPrimary() *primary {
	return &primary{events: make(chan startup.Event, 8), permit: make(chan struct{}), exited: make(chan struct{}), stopped: make(chan struct{}), reaped: make(chan struct{})}
}
func (*primary) StartConfirmed() bool { return true }
func (p *primary) Stdin() io.WriteCloser {
	if p.input != nil {
		return p.input
	}
	return discardInput{}
}
func (p *primary) Stdout() io.ReadCloser {
	if p.output != nil {
		return p.output
	}
	return io.NopCloser(bytes.NewReader(nil))
}
func (p *primary) Stderr() io.ReadCloser {
	if p.errorOutput != nil {
		return p.errorOutput
	}
	return io.NopCloser(bytes.NewReader(nil))
}
func (p *primary) Events() <-chan startup.Event { return p.events }
func (p *primary) ArmExecution(ctx context.Context) error {
	p.mu.Lock()
	p.order = append(p.order, "arm")
	p.mu.Unlock()
	if p.arm != nil {
		return p.arm(ctx)
	}
	return nil
}
func (p *primary) Permit(context.Context) (bool, error) {
	p.permits.Add(1)
	p.mu.Lock()
	p.order = append(p.order, "permit")
	p.mu.Unlock()
	p.permitOnce.Do(func() { close(p.permit) })
	return true, p.permitFailure
}
func (p *primary) Wait(ctx context.Context) (value.ProcessTermination, error) {
	p.waits.Add(1)
	select {
	case <-p.exited:
		if p.beforeWaitReturn != nil {
			p.beforeWaitReturn()
		}
		close(p.reaped)
		termination, err := value.NewProcessExited(value.Int64(0))
		if err != nil {
			return nil, err
		}
		return termination, p.waitFailure
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p *primary) Stop(context.Context) error {
	p.stops.Add(1)
	p.stopOnce.Do(func() { close(p.stopped) })
	p.exitOnce.Do(func() { close(p.exited) })
	return nil
}
func (p *primary) Release(context.Context) error {
	p.releases.Add(1)
	if p.releaseHook != nil {
		p.releaseHook()
	}
	if p.releaseFailure.Load() {
		return errors.New("fixture release unresolved")
	}
	return nil
}
func (p *primary) exit() { p.exitOnce.Do(func() { close(p.exited) }) }

func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}
func waitFor(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-bounded(t).Done():
		t.Fatal("fixture event did not complete")
	}
}
func noPermit(t *testing.T, p *primary) {
	t.Helper()
	select {
	case <-p.permit:
		t.Fatal("unexpected execution permit")
	default:
	}
}
func release(t *testing.T, child adapter.ProcessChild) {
	t.Helper()
	if err := child.Release(bounded(t)); err != nil {
		t.Fatal(err)
	}
}

func TestPreCanceledAdmissionCreatesNothing(t *testing.T) {
	var creates atomic.Int32
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		creates.Add(1)
		return newPrimary(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	child, err := supervisor.Start(ctx, invocation(t))
	if err == nil || child != nil || creates.Load() != 0 {
		t.Fatal("canceled admission obtained creation authority")
	}
}

func TestPendingCreationOwnsSlotAndPromptPipes(t *testing.T) {
	entered, settle := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(settle) }) })
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		close(entered)
		<-settle
		return nil, errors.New("settled without resources")
	}))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil || child == nil || child.StartConfirmed() {
		t.Fatal("pending owner was not transferred")
	}
	waitFor(t, entered)
	second, err := supervisor.Start(context.Background(), invocation(t))
	if err == nil || second != nil {
		t.Fatal("pending creation did not consume the sole slot")
	}
	ports := make(chan struct{})
	go func() {
		if child.Stdin() != nil && child.Stdout() != nil && child.Stderr() != nil {
			close(ports)
		}
	}()
	waitFor(t, ports)
	readDone, stderrDone, writeDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { _, _ = child.Stdout().Read(make([]byte, 1)); close(readDone) }()
	go func() { _, _ = child.Stderr().Read(make([]byte, 1)); close(stderrDone) }()
	go func() { _, _ = child.Stdin().Write(make([]byte, 4096)); close(writeDone) }()
	if err := child.Stdout().Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Stderr().Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, readDone)
	waitFor(t, stderrDone)
	waitFor(t, writeDone)
	once.Do(func() { close(settle) })
	release(t, child)
	if child.StartConfirmed() {
		t.Fatal("settled no-child creation fabricated primary confirmation")
	}
}

func TestReadinessArmsWitnessBeforePermitAndRequiresExecutionConfirmation(t *testing.T) {
	p := newPrimary()
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	waitFor(t, p.permit)
	if !child.StartConfirmed() {
		t.Fatal("primary creation was not confirmed")
	}
	p.mu.Lock()
	order := append([]string(nil), p.order...)
	p.mu.Unlock()
	if fmt.Sprint(order) != "[arm permit]" {
		t.Fatalf("witness order: %v", order)
	}
	p.exit()
	termination, waitErr := child.Wait(bounded(t))
	if termination == nil || waitErr == nil {
		t.Fatal("primary exit without execution witness became success")
	}
	release(t, child)
	if p.waits.Load() != 1 {
		t.Fatal("primary was not reaped exactly once")
	}
}

func TestWitnessFailurePreventsPermit(t *testing.T) {
	p := newPrimary()
	p.arm = func(context.Context) error { return errors.New("witness unavailable") }
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	if _, err := child.Wait(bounded(t)); err == nil {
		t.Fatal("failed witness setup became success")
	}
	noPermit(t, p)
	release(t, child)
}

func TestRegistryRejectionPreventsCreation(t *testing.T) {
	var creates atomic.Int32
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		creates.Add(1)
		return newPrimary(), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.OpenProcessRegistry(context.Background(), nil, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})
	driver, err := registry.Driver(supervisor)
	if err != nil {
		t.Fatal(err)
	}
	child, err := driver.Start(context.Background(), invocation(t))
	if err == nil || child != nil || creates.Load() != 0 {
		t.Fatal("unregistered executable reached creation")
	}
}

func TestCancellationBeforeReadinessPreventsPermit(t *testing.T) {
	p := newPrimary()
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	child, err := supervisor.Start(ctx, invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := child.Stop(bounded(t)); err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	release(t, child)
	noPermit(t, p)
}

func TestCancellationAtFinalWitnessCheckPreventsPermit(t *testing.T) {
	p := newPrimary()
	ctx, cancel := context.WithCancel(context.Background())
	p.arm = func(context.Context) error { cancel(); return nil }
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(ctx, invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	if _, err := child.Wait(bounded(t)); err == nil {
		t.Fatal("canceled startup became success")
	}
	release(t, child)
	noPermit(t, p)
}

func TestCancellationAfterPermitRetainsActivationUncertainty(t *testing.T) {
	p := newPrimary()
	ctx, cancel := context.WithCancel(context.Background())
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(ctx, invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	waitFor(t, p.permit)
	cancel()
	if err := child.Stop(bounded(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Wait(bounded(t)); err == nil {
		t.Fatal("post-permit cancellation became success")
	}
	release(t, child)
	if p.stops.Load() == 0 || !child.StartConfirmed() {
		t.Fatal("post-permit primary ownership was lost")
	}
}

func TestExplicitExecutionWitnessAllowsSharedLifecycleResult(t *testing.T) {
	p := newPrimary()
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := adapter.NewProcessLifecycle(supervisor, adapter.DefaultProcessLimits())
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithProcessLifecycle(inertBoundary{}, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	program, ctx := fixtureProgram(t), bounded(t)
	go func() {
		_, runErr := program.Run(ctx, boundary, execution.DefaultBudget(), device.DefaultConfig())
		completed <- runErr
	}()
	p.events <- startup.Event{Kind: startup.Ready}
	waitFor(t, p.permit)
	p.events <- startup.Event{Kind: startup.Executed}
	p.exit()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-bounded(t).Done():
		t.Fatal("confirmed target did not complete")
	}
}

func TestControlEOFOrFailureNeverAuthorizesTarget(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			p := newPrimary()
			supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
			if err != nil {
				t.Fatal(err)
			}
			child, err := supervisor.Start(context.Background(), invocation(t))
			if err != nil {
				t.Fatal(err)
			}
			if failed {
				p.events <- startup.Event{Kind: startup.Failed, Stage: "confinement"}
			} else {
				close(p.events)
			}
			if _, err := child.Wait(bounded(t)); err == nil {
				t.Fatal("control failure became successful execution")
			}
			release(t, child)
			noPermit(t, p)
		})
	}
}

func TestExecutionEventBeforeReadinessIsNotAuthority(t *testing.T) {
	p := newPrimary()
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Executed}
	if _, err := child.Wait(bounded(t)); err == nil {
		t.Fatal("premature execution event became authority")
	}
	release(t, child)
	noPermit(t, p)
}

func TestRepeatedReadyCannotPublishAnotherPermit(t *testing.T) {
	p := newPrimary()
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	waitFor(t, p.permit)
	p.events <- startup.Event{Kind: startup.Ready}
	p.exit()
	_, _ = child.Wait(bounded(t))
	release(t, child)
	if p.permits.Load() != 1 {
		t.Fatal("readiness replay published another execution permit")
	}
}

func TestUnresolvedCreationQuarantinesEvenAfterLateCleanup(t *testing.T) {
	entered, settle := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(settle) }) })
	p := newPrimary()
	var creates atomic.Int32
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		creates.Add(1)
		close(entered)
		<-settle
		return p, errors.New("late primary with creation failure")
	}))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, entered)
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := child.Release(expired); err == nil {
		t.Fatal("unsettled creation reported successful release")
	}
	if later, err := supervisor.Start(context.Background(), invocation(t)); err == nil || later != nil {
		t.Fatal("unresolved owner did not quarantine admission")
	}
	once.Do(func() { close(settle) })
	waitFor(t, p.stopped)
	waitFor(t, p.reaped)
	release(t, child)
	if !child.StartConfirmed() || p.stops.Load() == 0 || p.releases.Load() == 0 {
		t.Fatal("late primary was not retained through cleanup")
	}
	if later, err := supervisor.Start(context.Background(), invocation(t)); err == nil || later != nil || creates.Load() != 1 {
		t.Fatal("late cleanup erased sticky quarantine")
	}
}

func TestCreatorNilNilRetainsUnresolvedOwnership(t *testing.T) {
	var creates atomic.Int32
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		creates.Add(1)
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil || child == nil {
		t.Fatal("creation owner was not transferred")
	}
	if _, err := child.Wait(bounded(t)); err == nil {
		t.Fatal("nil/nil creator became successful execution")
	}
	if err := child.Release(bounded(t)); err == nil {
		t.Fatal("nil/nil creator proved resource-free release")
	}
	if child.StartConfirmed() {
		t.Fatal("nil/nil creator fabricated confirmed primary")
	}
	if later, err := supervisor.Start(context.Background(), invocation(t)); err == nil || later != nil || creates.Load() != 1 {
		t.Fatal("nil/nil creator failed to retain/quarantine admission")
	}
}

func TestSettledNilErrorReleasesSlotWithoutFabricatedTermination(t *testing.T) {
	var creates atomic.Int32
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		creates.Add(1)
		return nil, errors.New("settled with no resources")
	}))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		child, err := supervisor.Start(context.Background(), invocation(t))
		if err != nil || child == nil {
			t.Fatal("cleanly released slot was unavailable")
		}
		termination, waitErr := child.Wait(bounded(t))
		if termination != nil || waitErr == nil {
			t.Fatal("no-primary creation fabricated termination")
		}
		release(t, child)
		if child.StartConfirmed() {
			t.Fatal("no-primary creation fabricated confirmation")
		}
	}
	if creates.Load() != 2 {
		t.Fatal("slot was not reused after settled release")
	}
}

func TestFailedPrimaryReleaseQuarantinesSlot(t *testing.T) {
	p := newPrimary()
	p.releaseFailure.Store(true)
	var creates atomic.Int32
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		creates.Add(1)
		return p, errors.New("created primary with startup error")
	}))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil || child == nil {
		t.Fatal("partial creation owner was not transferred")
	}
	if err := child.Release(bounded(t)); err == nil {
		t.Fatal("failed primary release became complete")
	}
	if later, err := supervisor.Start(context.Background(), invocation(t)); err == nil || later != nil {
		t.Fatal("failed release did not quarantine admission")
	}
	p.releaseFailure.Store(false)
	release(t, child)
	if !child.StartConfirmed() || p.waits.Load() != 1 || p.stops.Load() == 0 {
		t.Fatal("partial primary lost stop/reap ownership")
	}
	if later, err := supervisor.Start(context.Background(), invocation(t)); err == nil || later != nil || creates.Load() != 1 {
		t.Fatal("later release erased quarantine")
	}
}

func TestFastExitQueuesExecutionWitnessBeforeWaitReturns(t *testing.T) {
	p := newPrimary()
	p.beforeWaitReturn = func() { p.events <- startup.Event{Kind: startup.Executed} }
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	p.events <- startup.Event{Kind: startup.Ready}
	waitFor(t, p.permit)
	p.exit()
	termination, waitErr := child.Wait(bounded(t))
	if termination == nil || waitErr != nil {
		t.Fatalf("queued fast-exit execution evidence was lost: %v", waitErr)
	}
	release(t, child)
}

type waitSignalDriver struct {
	adapter.ProcessDriver
	returned chan struct{}
}
type waitSignalChild struct {
	adapter.ProcessChild
	returned chan struct{}
	once     sync.Once
}

func (d waitSignalDriver) Start(ctx context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
	child, err := d.ProcessDriver.Start(ctx, invocation)
	if child == nil {
		return nil, err
	}
	return &waitSignalChild{ProcessChild: child, returned: d.returned}, err
}
func (c *waitSignalChild) Wait(ctx context.Context) (value.ProcessTermination, error) {
	termination, err := c.ProcessChild.Wait(ctx)
	c.once.Do(func() { close(c.returned) })
	return termination, err
}

type lateReadFailure struct {
	after   <-chan struct{}
	closed  chan struct{}
	once    sync.Once
	payload []byte
}

func (r *lateReadFailure) Read(destination []byte) (int, error) {
	select {
	case <-r.after:
		return copy(destination, r.payload), errors.New("private-output-error")
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
}
func (r *lateReadFailure) Close() error { r.once.Do(func() { close(r.closed) }); return nil }

type earlyWriteFailure struct {
	failed    chan struct{}
	permit    <-chan struct{}
	closed    chan struct{}
	once      sync.Once
	closeOnce sync.Once
}

func (w *earlyWriteFailure) Write([]byte) (int, error) {
	select {
	case <-w.permit:
	case <-w.closed:
		return 0, io.ErrClosedPipe
	}
	w.once.Do(func() { close(w.failed) })
	return 0, errors.New("private-input-error")
}
func (w *earlyWriteFailure) Close() error { w.closeOnce.Do(func() { close(w.closed) }); return nil }

func runWithDriver(t *testing.T, driver adapter.ProcessDriver, input string) <-chan error {
	completed, _ := runAndCaptureFailure(t, driver, input)
	return completed
}

type failureBoundary struct {
	execution.Boundary
	failures chan error
}

func (b failureBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	result, err := b.Boundary.Commit(ctx, target)
	if err != nil {
		b.failures <- err
	}
	return result, err
}

func runAndCaptureFailure(t *testing.T, driver adapter.ProcessDriver, input string) (<-chan error, <-chan error) {
	t.Helper()
	limits, err := adapter.NewProcessLimits(1024, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := adapter.NewProcessLifecycle(driver, limits)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithProcessLifecycle(inertBoundary{}, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	program := fixtureProgramWithInput(t, input)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	t.Cleanup(cancel)
	completed := make(chan error, 1)
	failures := make(chan error, 1)
	go func() {
		_, runErr := program.Run(ctx, failureBoundary{Boundary: boundary, failures: failures}, execution.DefaultBudget(), device.DefaultConfig())
		completed <- runErr
	}()
	return completed, failures
}

func requireActivationFailure(t *testing.T, completed <-chan error) {
	t.Helper()
	select {
	case err := <-completed:
		var failure execution.Failure
		if !errors.As(err, &failure) {
			t.Fatalf("IO failure did not produce ordinary activation failure: %v", err)
		}
		encoded, marshalErr := json.Marshal(failure.Value())
		if marshalErr != nil || !strings.Contains(string(encoded), "Process.ExecutionFailed") {
			t.Fatalf("wrong activation failure: %s %v", encoded, marshalErr)
		}
		if strings.Contains(string(encoded), "private-output-error") || strings.Contains(string(encoded), "private-input-error") {
			t.Fatal("private IO cause escaped into Lo diagnostics")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("IO failure did not return")
	}
}

func TestOutputReadFailureAfterTerminationCannotBecomeCleanEOF(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p := newPrimary()
			waitReturned := make(chan struct{})
			p.output = &lateReadFailure{after: waitReturned, closed: make(chan struct{}), payload: []byte("ok")[:count]}
			supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
			if err != nil {
				t.Fatal(err)
			}
			completed := runWithDriver(t, waitSignalDriver{ProcessDriver: supervisor, returned: waitReturned}, "stdin")
			p.events <- startup.Event{Kind: startup.Ready}
			waitFor(t, p.permit)
			p.events <- startup.Event{Kind: startup.Executed}
			p.exit()
			requireActivationFailure(t, completed)
		})
	}
}

func TestEarlyInputWriteFailureUnblocksUpstreamWithoutExecutionTimeout(t *testing.T) {
	p := newPrimary()
	input := &earlyWriteFailure{failed: make(chan struct{}), permit: p.permit, closed: make(chan struct{})}
	p.input = input
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	completed := runWithDriver(t, supervisor, strings.Repeat("x", 128<<10))
	p.events <- startup.Event{Kind: startup.Ready}
	waitFor(t, p.permit)
	p.events <- startup.Event{Kind: startup.Executed}
	waitFor(t, input.failed)
	p.exit()
	requireActivationFailure(t, completed)
}

type partialInput struct {
	failed chan struct{}
	once   sync.Once
}

func (w *partialInput) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.failed) })
	count := 2
	if len(data) < count {
		count = len(data)
	}
	return count, errors.New("private-input-error")
}
func (*partialInput) Close() error { return nil }

func TestStdinProgressCountsOnlyNativeAcceptedBytes(t *testing.T) {
	p := newPrimary()
	input := &partialInput{failed: make(chan struct{})}
	p.input = input
	p.arm = func(context.Context) error { return errors.New("fixture witness unavailable") }
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	completed, failures := runAndCaptureFailure(t, supervisor, "abcde")
	waitFor(t, input.failed)
	p.events <- startup.Event{Kind: startup.Ready}
	requireActivationFailure(t, completed)
	select {
	case failure := <-failures:
		progress := execution.ProcessProgress(failure)
		if progress == nil || progress.Field("stdin_written").(value.Integer).Big().Int64() != 2 {
			t.Fatal("stdin progress acknowledged bytes before native acceptance")
		}
	case <-bounded(t).Done():
		t.Fatal("lifecycle failure evidence missing")
	}
}

func TestOutputReadPreservesPartialCountAndError(t *testing.T) {
	p := newPrimary()
	ready := make(chan struct{})
	close(ready)
	p.output = &lateReadFailure{after: ready, closed: make(chan struct{}), payload: []byte("ok")}
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release(t, child) })
	data := make([]byte, 4)
	count, readErr := child.Stdout().Read(data)
	if count != 2 || readErr == nil || string(data[:count]) != "ok" {
		t.Fatal("pending output port changed native partial read result")
	}
}

type observedRead struct {
	closed chan struct{}
	once   sync.Once
	closes atomic.Int32
}

func (*observedRead) Read([]byte) (int, error) { return 0, io.EOF }
func (r *observedRead) Close() error {
	r.closes.Add(1)
	r.once.Do(func() { close(r.closed) })
	return nil
}

type observedWrite struct {
	closed chan struct{}
	once   sync.Once
	closes atomic.Int32
}

func (*observedWrite) Write(data []byte) (int, error) { return len(data), nil }
func (w *observedWrite) Close() error {
	w.closes.Add(1)
	w.once.Do(func() { close(w.closed) })
	return nil
}

func TestClosingPendingPortsClosesLateEndpointsWithoutExposure(t *testing.T) {
	settle := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(settle) }) })
	p := newPrimary()
	input := &observedWrite{closed: make(chan struct{})}
	output, diagnostic := &observedRead{closed: make(chan struct{})}, &observedRead{closed: make(chan struct{})}
	p.input, p.output, p.errorOutput = input, output, diagnostic
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { <-settle; return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { once.Do(func() { close(settle) }); release(t, child) })
	if err := child.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Stdout().Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Stderr().Close(); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(settle) })
	waitFor(t, input.closed)
	waitFor(t, output.closed)
	waitFor(t, diagnostic.closed)
	if input.closes.Load() != 1 || output.closes.Load() != 1 || diagnostic.closes.Load() != 1 {
		t.Fatal("late endpoint was closed more than once")
	}
}

func TestCancellationUnblocksPortAwaitingCreation(t *testing.T) {
	settle := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(settle) }) })
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
		<-settle
		return nil, errors.New("no resources")
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	child, err := supervisor.Start(ctx, invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { once.Do(func() { close(settle) }); release(t, child) })
	completed := make(chan error, 1)
	go func() { _, readErr := child.Stdout().Read(make([]byte, 1)); completed <- readErr }()
	cancel()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("canceled pending read became success")
		}
	case <-bounded(t).Done():
		t.Fatal("pending read ignored cancellation")
	}
}

type inflightInput struct {
	entered, closed, finished        chan struct{}
	enterOnce, closeOnce, finishOnce sync.Once
	closes                           atomic.Int32
}

func (w *inflightInput) Write([]byte) (int, error) {
	w.enterOnce.Do(func() { close(w.entered) })
	<-w.closed
	w.finishOnce.Do(func() { close(w.finished) })
	return 0, io.ErrClosedPipe
}
func (w *inflightInput) Close() error {
	w.closes.Add(1)
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

func TestConcurrentCloseAndReleaseJoinInflightNativeWrite(t *testing.T) {
	p := newPrimary()
	input := &inflightInput{entered: make(chan struct{}), closed: make(chan struct{}), finished: make(chan struct{})}
	p.input = input
	var releaseTooEarly atomic.Bool
	p.releaseHook = func() {
		select {
		case <-input.finished:
		default:
			releaseTooEarly.Store(true)
		}
	}
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	written := make(chan struct{})
	var count int
	var writeErr error
	go func() { count, writeErr = child.Stdin().Write([]byte("input")); close(written) }()
	waitFor(t, input.entered)
	released := make(chan error, 1)
	ctx := bounded(t)
	go func() { released <- child.Release(ctx) }()
	if err := child.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, written)
	select {
	case err := <-released:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("release did not join native IO")
	}
	if count != 0 || writeErr == nil || releaseTooEarly.Load() || input.closes.Load() != 1 {
		t.Fatal("pending port lost native count/error or release ownership")
	}
}

func TestCallbackFailureClassificationAndDiagnosticRedaction(t *testing.T) {
	classes := []struct {
		name     string
		cause    error
		expected execution.HostError
	}{
		{"denied", execution.Denied("private-host-operation", "private-host-subject"), execution.Denied("safe", "stage")},
		{"limit", execution.Limit("private-host-limit"), execution.Limit("safe")},
		{"unavailable", execution.ProcessUnavailable(), execution.ProcessUnavailable()},
		{"host", execution.HostFailure("private-host-operation", "private-host-subject", errors.New("private-host-payload")), execution.HostFailure("safe", "stage", nil)},
		{"other-host-code", execution.ServerRequiresServe(), execution.HostFailure("safe", "stage", nil)},
		{"ordinary", errors.New("private-host-payload"), nil},
		{"unrelated-cancel", context.Canceled, execution.HostFailure("safe", "stage", nil)},
		{"unrelated-deadline", context.DeadlineExceeded, execution.HostFailure("safe", "stage", nil)},
	}
	for _, stage := range []string{"creator", "wait", "arm", "permit"} {
		for _, class := range classes {
			t.Run(stage+"/"+class.name, func(t *testing.T) {
				p := newPrimary()
				if stage == "wait" {
					p.waitFailure = class.cause
				}
				if stage == "arm" {
					p.arm = func(context.Context) error { return class.cause }
				}
				if stage == "permit" {
					p.permitFailure = class.cause
				}
				supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) {
					if stage == "creator" {
						return nil, class.cause
					}
					return p, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				completed := runWithDriver(t, supervisor, "stdin")
				if stage != "creator" {
					p.events <- startup.Event{Kind: startup.Ready}
				}
				if stage == "wait" {
					waitFor(t, p.permit)
					p.events <- startup.Event{Kind: startup.Executed}
					p.exit()
				}
				select {
				case runErr := <-completed:
					if runErr == nil {
						t.Fatal("callback failure became successful execution")
					}
					var actual execution.HostError
					if class.expected != nil {
						if !errors.As(runErr, &actual) || actual.Code() != class.expected.Code() {
							t.Errorf("host class was lost: %v", runErr)
						}
					} else {
						var failure execution.Failure
						if !errors.As(runErr, &failure) || failure.Value().Kind() != "Process.ExecutionFailed" {
							t.Errorf("ordinary callback became host failure: %v", runErr)
						}
					}
					var diagnostic bytes.Buffer
					status := runner.ReportRuntime(&diagnostic, runner.JsonFormat, runErr)
					if class.expected != nil && status != runner.ExitHost {
						t.Error("host callback became recoverable")
					}
					if strings.Contains(diagnostic.String(), "private-host-") || runErr != nil && strings.Contains(runErr.Error(), "private-host-") {
						t.Error("private callback payload escaped")
					}
				case <-bounded(t).Done():
					t.Fatal("callback failure did not settle")
				}
			})
		}
	}
}

func TestInternalStopCallbackCancellationRemainsOrdinary(t *testing.T) {
	p := newPrimary()
	p.waitFailure = context.Canceled
	supervisor, err := startup.NewSupervisor(creatorFunc(func(context.Context, adapter.ProcessInvocation) (startup.Primary, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	child, err := supervisor.Start(context.Background(), invocation(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Stop(bounded(t)); err != nil {
		t.Fatal(err)
	}
	_, waitErr := child.Wait(bounded(t))
	if waitErr == nil || errors.Is(waitErr, context.Canceled) || execution.BoundaryHostError(waitErr) != nil {
		t.Fatal("internal stop fabricated external cancellation")
	}
	release(t, child)
}
