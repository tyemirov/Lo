package processhost

import (
	"context"
	"errors"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Config contains pure authority descriptions and shared lifecycle limits. It
// does not establish backend support or qualify a helper image.
type Config interface {
	Policy() adapter.ProcessPolicy
	Limits() adapter.ProcessLimits
	config()
}

type config struct {
	policy adapter.ProcessPolicy
	limits adapter.ProcessLimits
}

func (config) config()                         {}
func (c config) Policy() adapter.ProcessPolicy { return c.policy }
func (c config) Limits() adapter.ProcessLimits { return c.limits }

// NewConfig constructs a description without filesystem access or activation.
func NewConfig(policy adapter.ProcessPolicy, limits adapter.ProcessLimits) (Config, error) {
	if policy == nil || limits == nil {
		return nil, host("missing session configuration")
	}
	return config{policy: policy, limits: limits}, nil
}

// Session owns the host boundary and a single retained shutdown owner. Close's
// context bounds only its caller's wait. WaitRetained joins the same owner and
// never retries cleanup. A historical error does not itself mean live ownership.
type Session interface {
	Boundary() execution.Boundary
	Close(context.Context) error
	WaitRetained() error
	session()
}

type session struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	boundary execution.Boundary
	files    adapter.Files
	registry adapter.ProcessRegistry
	driver   *sessionDriver
	active   int
	closing  bool
	changed  chan struct{}
	done     chan struct{}
	history  error
	cleanup  error
}

func (*session) session()                       {}
func (s *session) Boundary() execution.Boundary { return s }

// Open selects support before looking up any helper or registered executable.
// Configured grants select the qualified native profile. Without grants, the
// session retains File support and rejects Process activation.
func Open(ctx context.Context, configuration Config, roots adapter.FileRoots) (Session, error) {
	if configuration == nil {
		return nil, host("missing session configuration")
	}
	if len(configuration.Policy().Grants()) != 0 {
		creator, err := NewDarwinCreator(ctx, configuration)
		if err != nil {
			return nil, err
		}
		return openSession(ctx, configuration, roots, creator)
	}
	return openSession(ctx, configuration, roots, nil)
}

// OpenTrusted is a trusted Go-embedding boundary, not native qualification. The
// Creator must honor Primary ownership, confinement, execution-witness, and
// resource contracts. Only configured command grants are protected here; a
// production native assembly must additionally protect its reviewed helper.
func OpenTrusted(ctx context.Context, configuration Config, roots adapter.FileRoots, creator Creator) (Session, error) {
	if configuration == nil || creator == nil {
		return nil, host("missing trusted session dependencies")
	}
	return openSession(ctx, configuration, roots, creator)
}

func openSession(ctx context.Context, configuration Config, roots adapter.FileRoots, creator Creator) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	grants := configuration.Policy().Grants()
	if protected, ok := creator.(interface {
		protectedImages() []adapter.ProcessExecutableGrant
	}); ok {
		grants = append(grants, protected.protectedImages()...)
	}
	registry, err := adapter.OpenProcessRegistry(ctx, grants, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		return nil, err
	}
	files, err := registry.OpenFiles(roots, adapter.NativeFileIO())
	if err != nil {
		return nil, execution.PreserveCleanup(err, registry.Close())
	}
	childContext, cancel := context.WithCancel(ctx)
	s := &session{ctx: childContext, cancel: cancel, files: files, registry: registry, changed: make(chan struct{}), done: make(chan struct{})}
	var boundary execution.Boundary
	if creator == nil {
		boundary, err = adapter.WithProcess(files)
	} else {
		var supervisor *Supervisor
		supervisor, err = NewSupervisor(creator)
		if err == nil {
			var registered adapter.ProcessDriver
			registered, err = registry.Driver(supervisor)
			if err == nil {
				s.driver = &sessionDriver{driver: registered, children: make(map[*sessionChild]struct{})}
				var lifecycle adapter.ProcessLifecycle
				lifecycle, err = adapter.NewProcessLifecycle(s.driver, configuration.Limits())
				if err == nil {
					boundary, err = adapter.WithProcessLifecycle(files, lifecycle)
				}
			}
		}
	}
	if err != nil {
		cancel()
		return nil, execution.PreserveCleanup(err, errors.Join(files.Close(), registry.Close()))
	}
	s.boundary = boundary
	// Parent cancellation uses the same terminal transition and cleanup owner.
	go func() {
		select {
		case <-childContext.Done():
			s.beginClose()
		case <-s.done:
		}
	}()
	return s, nil
}

func (s *session) notify() { close(s.changed); s.changed = make(chan struct{}) }

func (s *session) admit(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	if s.closing || s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, nil, host("session is closed")
	}
	s.active++
	s.mu.Unlock()
	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return operation, func() {
		stop()
		cancel()
		s.mu.Lock()
		s.active--
		s.notify()
		s.mu.Unlock()
	}, nil
}

func (s *session) Read(ctx context.Context, path value.Text) (value.Bytes, error) {
	operation, end, err := s.admit(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	return s.boundary.Read(operation, path)
}
func (s *session) List(ctx context.Context, path value.Text) (value.List, error) {
	operation, end, err := s.admit(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	return s.boundary.List(operation, path)
}
func (s *session) OpenPaths(ctx context.Context, path value.Text) (value.List, error) {
	operation, end, err := s.admit(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	return s.boundary.OpenPaths(operation, path)
}
func (s *session) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	operation, end, err := s.admit(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	return s.boundary.Commit(operation, target)
}

func (s *session) beginClose() {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.cancel()
	s.notify()
	s.mu.Unlock()
	go s.closeOwned()
}

func (s *session) closeOwned() {
	for {
		s.mu.Lock()
		active, changed := s.active, s.changed
		s.mu.Unlock()
		if active == 0 {
			break
		}
		<-changed
	}
	var childErr error
	if s.driver != nil {
		childErr = s.driver.releaseOwned()
	}
	cleanup := errors.Join(childErr, s.files.Close(), s.registry.Close())
	s.mu.Lock()
	s.cleanup = cleanup
	s.mu.Unlock()
	close(s.done)
}

func (s *session) result() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.history, s.cleanup)
}

func (s *session) Close(ctx context.Context) error {
	s.beginClose()
	select {
	case <-s.done:
		return s.result()
	case <-ctx.Done():
		s.mu.Lock()
		s.history = errors.Join(s.history, ctx.Err())
		s.mu.Unlock()
		return errors.Join(s.result(), ctx.Err())
	}
}

func (s *session) WaitRetained() error {
	<-s.done
	return s.result()
}

// The registry sees one resource-release operation which outlives lifecycle
// caller deadlines. A timed-out waiter cannot prematurely discharge its lease.
type sessionDriver struct {
	driver   adapter.ProcessDriver
	mu       sync.Mutex
	children map[*sessionChild]struct{}
}

func (d *sessionDriver) Start(ctx context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
	child, err := d.driver.Start(ctx, invocation)
	if child == nil {
		return nil, err
	}
	tracked := &sessionChild{ProcessChild: child, driver: d, settled: make(chan struct{})}
	d.mu.Lock()
	d.children[tracked] = struct{}{}
	d.mu.Unlock()
	return tracked, err
}
func (d *sessionDriver) releaseOwned() error {
	d.mu.Lock()
	children := make([]*sessionChild, 0, len(d.children))
	for child := range d.children {
		children = append(children, child)
	}
	d.mu.Unlock()
	var result error
	for _, child := range children {
		result = errors.Join(result, child.Release(context.Background()))
		// A completed failed Release does not prove that resources are absent.
		// There is no retry or polling owner. Such unresolved ownership remains
		// retained, with Close caller deadlines still bounding their own waits.
		<-child.settled
	}
	return result
}

type sessionChild struct {
	adapter.ProcessChild
	driver  *sessionDriver
	once    sync.Once
	done    chan struct{}
	settled chan struct{}
	err     error
}

func (child *sessionChild) Release(ctx context.Context) error {
	child.once.Do(func() {
		child.done = make(chan struct{})
		go func() {
			child.err = child.ProcessChild.Release(context.WithoutCancel(ctx))
			if child.err == nil {
				child.driver.mu.Lock()
				delete(child.driver.children, child)
				child.driver.mu.Unlock()
				close(child.settled)
			}
			close(child.done)
		}()
	})
	select {
	case <-child.done:
		return child.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
