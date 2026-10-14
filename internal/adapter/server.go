package adapter

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	LoopbackHost    = "127.0.0.1"
	MaxServers      = 64
	MaxRequestBytes = 1 << 20
	MaxHeaderBytes  = 64 << 10
	IOTimeout       = 5 * time.Second
	IdleTimeout     = 30 * time.Second
	ShutdownTimeout = 5 * time.Second
)

type ListenPolicy interface {
	Allows(uint16) bool
	listenPolicy()
}
type listenPolicy struct{ ports map[uint16]bool }

func (listenPolicy) listenPolicy()             {}
func (p listenPolicy) Allows(port uint16) bool { return p.ports[port] }
func NewListenPolicy(ports []uint16) (ListenPolicy, error) {
	p := listenPolicy{ports: map[uint16]bool{}}
	for _, port := range ports {
		if p.ports[port] {
			return nil, fmt.Errorf("duplicate listen port %d", port)
		}
		p.ports[port] = true
	}
	return p, nil
}

type ServerInfo struct {
	Address string `json:"address"`
}
type Servers interface {
	execution.Boundary
	Start(context.Context) (value.Value, error)
	Snapshot() []ServerInfo
	Failures() <-chan error
	Shutdown(context.Context) error
	servers()
}
type activeServer struct {
	info ServerInfo
	http *http.Server
}
type servers struct {
	program        execution.Program
	files          Files
	policy         ListenPolicy
	budget         execution.Budget
	mu             sync.Mutex
	active         []activeServer
	stopping       bool
	started        bool
	fatal          chan error
	failOnce       sync.Once
	shutdownOnce   sync.Once
	shutdownErr    error
	workers        sync.WaitGroup
	requests       sync.WaitGroup
	requestContext context.Context
	cancelRequests context.CancelFunc
	pending        map[net.Conn]struct{}
}

func (*servers) servers() {}
func NewServers(p execution.Program, files Files, policy ListenPolicy, budget execution.Budget) (Servers, error) {
	if p == nil || files == nil || policy == nil || budget == nil {
		return nil, fmt.Errorf("servers require a checked program, files, policy, and budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &servers{program: p, files: files, policy: policy, budget: budget, fatal: make(chan error, 1), requestContext: ctx, cancelRequests: cancel, pending: map[net.Conn]struct{}{}}, nil
}
func (s *servers) Start(ctx context.Context) (value.Value, error) {
	s.mu.Lock()
	if s.started || s.stopping {
		s.mu.Unlock()
		return nil, execution.HostFailure("start HTTP service", "program", errors.New("service already started or stopped"))
	}
	s.started = true
	s.mu.Unlock()
	output, err := s.program.Run(ctx, s, s.budget)
	if err == nil && len(s.Snapshot()) == 0 {
		err = execution.NoServer()
	}
	if err == nil {
		return output, nil
	}
	shutdown, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if closeErr := s.Shutdown(shutdown); closeErr != nil {
		return nil, execution.HostFailure("close failed startup", "program", errors.Join(err, closeErr))
	}
	return nil, err
}
func (s *servers) Read(ctx context.Context, path value.Text) (value.Bytes, error) {
	return s.files.Read(ctx, path)
}
func (s *servers) Commit(ctx context.Context, target value.Target) error {
	t, ok := target.(value.Server)
	if !ok {
		return s.files.Commit(ctx, target)
	}
	n := t.Port().Big()
	if !n.IsUint64() || n.Uint64() > 65535 {
		return fmt.Errorf("%s: invalid port %s", catalog.HTTPListen, n)
	}
	port := uint16(n.Uint64())
	address := net.JoinHostPort(LoopbackHost, strconv.Itoa(int(port)))
	if !s.policy.Allows(port) {
		return execution.Denied(catalog.HTTPListen, address)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return context.Canceled
	}
	if len(s.active) == MaxServers {
		return execution.Limit("active HTTP servers")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("%s %s: %w", catalog.HTTPListen, address, err)
	}
	server := &http.Server{
		ReadHeaderTimeout: IOTimeout, ReadTimeout: IOTimeout, WriteTimeout: IOTimeout, IdleTimeout: IdleTimeout, MaxHeaderBytes: MaxHeaderBytes,
		BaseContext: func(net.Listener) context.Context { return s.requestContext },
		ConnState:   s.connectionState,
		Handler:     http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.request(t, w, r) }),
	}
	s.active = append(s.active, activeServer{ServerInfo{listener.Addr().String()}, server})
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.fail(execution.HostFailure(catalog.HTTPListen, address, err))
		}
	}()
	return nil
}
func (s *servers) Snapshot() []ServerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := make([]ServerInfo, len(s.active))
	for i, a := range s.active {
		infos[i] = a.info
	}
	return infos
}
func (s *servers) Failures() <-chan error { return s.fatal }
func (s *servers) fail(err error)         { s.failOnce.Do(func() { s.fatal <- err }) }
func (s *servers) connectionState(conn net.Conn, state http.ConnState) {
	s.mu.Lock()
	closeNew := state == http.StateNew && s.stopping
	if state == http.StateNew && !s.stopping {
		s.pending[conn] = struct{}{}
	} else {
		delete(s.pending, conn)
	}
	s.mu.Unlock()
	if closeNew {
		if err := closePendingConnection(conn); err != nil {
			s.fail(err)
		}
	}
}
func closePendingConnection(conn net.Conn) error {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return execution.HostFailure("close pending HTTP connection", conn.RemoteAddr().String(), err)
	}
	return nil
}
func (s *servers) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		s.stopping = true
		active := slices.Clone(s.active)
		pending := make([]net.Conn, 0, len(s.pending))
		for conn := range s.pending {
			pending = append(pending, conn)
		}
		s.mu.Unlock()
		for _, conn := range pending {
			s.shutdownErr = errors.Join(s.shutdownErr, closePendingConnection(conn))
		}
		results := make(chan error, len(active))
		for _, a := range active {
			go func(a activeServer) {
				err := a.http.Shutdown(ctx)
				if err != nil {
					s.cancelRequests()
					err = errors.Join(err, a.http.Close())
				}
				if err != nil {
					err = fmt.Errorf("shutdown HTTP server %s: %w", a.info.Address, err)
				}
				results <- err
			}(a)
		}
		for range active {
			s.shutdownErr = errors.Join(s.shutdownErr, <-results)
		}
		s.workers.Wait()
		s.cancelRequests()
		s.requests.Wait()
	})
	return s.shutdownErr
}
