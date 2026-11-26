// Package testsession owns immutable compiler fixtures for one isolated test run.
package testsession

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	maxRequestBytes     = 16 << 20
	defaultStorageBytes = 8 << 30
	compileOperation    = "compile"
	retainedOperation   = "compile-retained"
	sourceOperation     = "source"
	inputsOperation     = "inputs"
	leaseOperation      = "lease"
)

// Config declares session-owned fixture inputs and the global backend limit.
type Config struct {
	Directory, Toolchain, Compiler string
	BuildSlots                     int
	MaxBytes                       int64
}

// CompilePurpose selects the source contract for a native fixture.
type CompilePurpose string

const (
	SourceIdentity CompilePurpose = ""
	RuntimeValues  CompilePurpose = "runtime-values"
)

// CompileRequest identifies one native runtime fixture by its original source.
type CompileRequest struct {
	Purpose CompilePurpose `json:"purpose,omitempty"`
	Source  string         `json:"source"`
	Target  string         `json:"target,omitempty"`
	Backend string         `json:"backend,omitempty"`
}

// CompileResult identifies the immutable executable and its selected runtime.
type CompileResult struct {
	Artifact string `json:"artifact"`
	Runtime  string `json:"runtime"`
}

// InputsResult names the session's verified, private tool distribution.
type InputsResult struct {
	Manifest string `json:"manifest"`
	Compiler string `json:"compiler"`
}

// Statistics describes completed fixture preparation and cache reuse.
type Statistics struct {
	CodeBuilds uint64
	CodeReused uint64
	Builds     uint64
	Reused     uint64
	Bytes      int64
	Evicted    uint64
	Retained   uint64
}

type fileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type targetInputs struct {
	Triple   string                  `json:"triple"`
	Linker   fileIdentity            `json:"linker"`
	Runtime  fileIdentity            `json:"runtime"`
	Inputs   map[string]fileIdentity `json:"inputs"`
	LinkArgs []string                `json:"link_args"`
}

type metalInputs struct {
	targetInputs
	Target   string          `json:"target"`
	Identity json.RawMessage `json:"identity"`
	Kernel   fileIdentity    `json:"kernel"`
}

type distribution struct {
	ABI           string                  `json:"abi"`
	LLVM          string                  `json:"llvm"`
	Host          string                  `json:"host"`
	Compiler      fileIdentity            `json:"compiler"`
	Targets       map[string]targetInputs `json:"targets"`
	HostLibraries map[string]fileIdentity `json:"host_libraries,omitempty"`
	Metal         *metalInputs            `json:"metal,omitempty"`
}

type request struct {
	Operation string         `json:"operation"`
	Compile   CompileRequest `json:"compile,omitempty"`
	Source    string         `json:"source,omitempty"`
}

type response struct {
	Compile CompileResult `json:"compile,omitempty"`
	Inputs  InputsResult  `json:"inputs,omitempty"`
	Source  string        `json:"source,omitempty"`
	Error   string        `json:"error,omitempty"`
	Failure *CommandError `json:"failure,omitempty"`
}

type buildRecord struct {
	done         chan struct{}
	cancel       context.CancelFunc
	waiters      int
	result       CompileResult
	err          error
	bytes        int64
	lastUsed     uint64
	sourceDigest string
}

// Server coordinates compiler preparation and backend leases across workers.
type Server struct {
	root, directory, address string
	inputs                   InputsResult
	distribution             distribution
	identity                 string
	ctx                      context.Context
	cancel                   context.CancelFunc
	listener                 net.Listener
	slots                    chan struct{}
	mu                       sync.Mutex
	builds                   map[string]*buildRecord
	valueCodes               map[string]*buildRecord
	connections              map[net.Conn]struct{}
	stats                    Statistics
	maxBytes                 int64
	workers                  sync.WaitGroup
	closeOnce                sync.Once
	closeErr                 error
	entryMu                  sync.Mutex
	entries                  map[string]string
	clock                    uint64
}

func localPath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %q", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("path leaves Lo: %q", path)
	}
	return resolved, nil
}

func repositoryRoot() (string, error) {
	path, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(path, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/MarcoPoloResearchLab/Lo\n") {
			return filepath.EvalSymlinks(path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("working directory must stay inside Lo")
		}
		path = parent
	}
}

// Start verifies and snapshots the selected tools before accepting worker work.
func Start(ctx context.Context, config Config) (_ *Server, failure error) {
	if ctx == nil || config.BuildSlots < 1 || config.BuildSlots > 1024 {
		return nil, fmt.Errorf("session requires a context and 1..1024 backend slots")
	}
	root, err := repositoryRoot()
	if err != nil {
		return nil, err
	}
	compiler, err := localPath(root, config.Compiler)
	if err != nil {
		return nil, fmt.Errorf("session compiler: %w", err)
	}
	manifest, err := localPath(root, config.Toolchain)
	if err != nil {
		return nil, fmt.Errorf("session toolchain: %w", err)
	}
	if !filepath.IsAbs(config.Directory) {
		return nil, fmt.Errorf("session directory must be absolute")
	}
	parent, err := localPath(root, filepath.Dir(config.Directory))
	if err != nil {
		return nil, fmt.Errorf("session directory parent: %w", err)
	}
	directory := filepath.Join(parent, filepath.Base(config.Directory))
	relative, err := filepath.Rel(filepath.Join(root, "_scratch"), directory)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("session directory must stay inside Lo/_scratch")
	}
	if err := os.Mkdir(directory, os.ModePerm); err != nil {
		return nil, fmt.Errorf("create fresh session directory: %w", err)
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, os.RemoveAll(directory))
		}
	}()
	serverCtx, cancel := context.WithCancel(ctx)
	server := &Server{root: root, directory: directory, ctx: serverCtx, cancel: cancel,
		slots: make(chan struct{}, config.BuildSlots), builds: make(map[string]*buildRecord), valueCodes: make(map[string]*buildRecord), connections: make(map[net.Conn]struct{}), entries: make(map[string]string), maxBytes: config.MaxBytes}
	if server.maxBytes == 0 {
		server.maxBytes = defaultStorageBytes
	}
	if server.maxBytes < 1 {
		cancel()
		return nil, fmt.Errorf("session storage limit must be positive")
	}
	if err := server.snapshot(manifest, compiler); err != nil {
		cancel()
		return nil, err
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		cancel()
		return nil, fmt.Errorf("session socket identity: %w", err)
	}
	server.address = filepath.Join(root, "_scratch", "ts-"+hex.EncodeToString(nonce[:]))
	server.listener, err = net.Listen("unix", server.address)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen for session workers: %w", err)
	}
	context.AfterFunc(server.ctx, func() { _ = server.listener.Close() })
	server.workers.Go(server.accept)
	return server, nil
}

// Address returns the repository-local Unix socket for session clients.
func (server *Server) Address() string { return server.address }

// Stats returns a synchronized snapshot of fixture preparation counts.
func (server *Server) Stats() Statistics {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.stats
}

// Close joins owned backend work, closes worker connections, and removes state.
func (server *Server) Close() error {
	server.closeOnce.Do(func() {
		server.cancel()
		server.closeErr = server.listener.Close()
		if errors.Is(server.closeErr, net.ErrClosed) {
			server.closeErr = nil
		}
		server.mu.Lock()
		for connection := range server.connections {
			_ = connection.Close()
		}
		server.mu.Unlock()
		server.workers.Wait()
		server.closeErr = errors.Join(server.closeErr, os.RemoveAll(server.directory))
	})
	return server.closeErr
}

func (server *Server) accept() {
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.mu.Lock()
		if server.ctx.Err() != nil {
			server.mu.Unlock()
			_ = connection.Close()
			return
		}
		server.connections[connection] = struct{}{}
		server.workers.Go(func() { server.serve(connection) })
		server.mu.Unlock()
	}
}

func (server *Server) serve(connection net.Conn) {
	stopCancellation := context.AfterFunc(server.ctx, func() { _ = connection.Close() })
	defer stopCancellation()
	defer func() {
		_ = connection.Close()
		server.mu.Lock()
		delete(server.connections, connection)
		server.mu.Unlock()
	}()
	var selected request
	decoder := json.NewDecoder(io.LimitReader(connection, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selected); err != nil {
		_ = json.NewEncoder(connection).Encode(response{Error: fmt.Sprintf("decode session request: %v", err)})
		return
	}
	ctx, cancel := context.WithCancel(server.ctx)
	defer cancel()
	disconnected := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, connection)
		cancel()
		close(disconnected)
	}()
	var result response
	var err error
	switch selected.Operation {
	case leaseOperation:
		select {
		case server.slots <- struct{}{}:
			defer func() { <-server.slots }()
			if err := json.NewEncoder(connection).Encode(response{}); err != nil {
				return
			}
			select {
			case <-ctx.Done():
			case <-disconnected:
			}
			return
		case <-ctx.Done():
			return
		}
	case inputsOperation:
		result.Inputs = server.inputs
	case sourceOperation:
		result.Source, err = server.source(selected.Source)
	case compileOperation, retainedOperation:
		var release func()
		result.Compile, release, err = server.compile(ctx, selected.Compile)
		if release != nil {
			if selected.Operation == retainedOperation {
				server.mu.Lock()
				server.stats.Retained++
				server.mu.Unlock()
				defer func() {
					release()
					server.mu.Lock()
					server.stats.Retained--
					server.mu.Unlock()
				}()
				if err := json.NewEncoder(connection).Encode(result); err != nil {
					return
				}
				<-ctx.Done()
				return
			}
			defer release()
		}
	default:
		err = fmt.Errorf("unknown session operation %q", selected.Operation)
	}
	if err != nil {
		result.Error = err.Error()
		errors.As(err, &result.Failure)
	}
	_ = json.NewEncoder(connection).Encode(result)
}

func (server *Server) source(text string) (string, error) {
	sum := sha256.Sum256([]byte(text))
	path := filepath.Join(server.directory, "sources", hex.EncodeToString(sum[:])+".lo")
	server.mu.Lock()
	defer server.mu.Unlock()
	if current, err := os.ReadFile(path); err == nil {
		if string(current) != text {
			return "", fmt.Errorf("immutable fixture source changed: %q", path)
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read immutable fixture source: %w", err)
	}
	if err := server.makeSpaceLocked(int64(len(text))); err != nil {
		return "", fmt.Errorf("reserve immutable fixture source: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return "", fmt.Errorf("create immutable source directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".source-")
	if err != nil {
		return "", fmt.Errorf("create immutable source: %w", err)
	}
	staged := file.Name()
	_, writeErr := file.WriteString(text)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return "", fmt.Errorf("write immutable source: %w", errors.Join(err, os.Remove(staged)))
	}
	if err := os.Rename(staged, path); err != nil {
		return "", fmt.Errorf("publish immutable source: %w", errors.Join(err, os.Remove(staged)))
	}
	server.stats.Bytes += int64(len(text))
	return path, nil
}

func (server *Server) compile(ctx context.Context, selected CompileRequest) (CompileResult, func(), error) {
	if selected.Purpose != SourceIdentity && selected.Purpose != RuntimeValues {
		return CompileResult{}, nil, fmt.Errorf("unknown fixture purpose %q", selected.Purpose)
	}
	path, err := localPath(server.root, selected.Source)
	if err != nil {
		return CompileResult{}, nil, fmt.Errorf("fixture source: %w", err)
	}
	if selected.Target == "" {
		selected.Target = runtime.GOOS + "/" + runtime.GOARCH
	}
	if selected.Backend == "" {
		selected.Backend = "software"
	}
	if selected.Backend != "software" && selected.Backend != "metal" {
		return CompileResult{}, nil, fmt.Errorf("unknown fixture backend %q", selected.Backend)
	}
	if _, ok := server.distribution.Targets[selected.Target]; !ok {
		return CompileResult{}, nil, fmt.Errorf("unknown fixture target %q", selected.Target)
	}
	if selected.Backend == "metal" && (server.distribution.Metal == nil || server.distribution.Metal.Target != selected.Target) {
		return CompileResult{}, nil, fmt.Errorf("fixture toolchain lacks selected Metal target")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CompileResult{}, nil, fmt.Errorf("read fixture source: %w", err)
	}
	selected.Source = filepath.Clean(selected.Source)
	sourceDigest := fmt.Sprintf("%x", sha256.Sum256(data))
	keyData, _ := json.Marshal(struct {
		Request        CompileRequest
		Source, Inputs string
	}{selected, sourceDigest, server.identity})
	key := fmt.Sprintf("%x", sha256.Sum256(keyData))
	server.mu.Lock()
	record := server.builds[key]
	if record == nil {
		buildCtx, cancel := context.WithCancel(server.ctx)
		record = &buildRecord{done: make(chan struct{}), cancel: cancel, sourceDigest: sourceDigest}
		server.builds[key] = record
		server.stats.Builds++
		server.workers.Go(func() { server.build(buildCtx, key, selected, record) })
	} else {
		server.stats.Reused++
	}
	record.waiters++
	server.clock++
	record.lastUsed = server.clock
	server.mu.Unlock()
	release := func() { server.releaseBuild(key, record) }
	select {
	case <-record.done:
		if record.err != nil {
			release()
			return CompileResult{}, nil, record.err
		}
		return record.result, release, nil
	case <-ctx.Done():
		release()
		return CompileResult{}, nil, fmt.Errorf("fixture requester disconnected or canceled: %w", ctx.Err())
	}
}

func (server *Server) build(ctx context.Context, key string, selected CompileRequest, record *buildRecord) {
	defer record.cancel()
	var result CompileResult
	var err error
	select {
	case server.slots <- struct{}{}:
		result, err = server.buildFixture(ctx, key, selected, record)
		<-server.slots
	case <-ctx.Done():
		err = ctx.Err()
	}
	server.mu.Lock()
	select {
	case <-record.done:
	default:
		record.result, record.err = result, err
		if err != nil && server.builds[key] == record {
			delete(server.builds, key)
		}
		close(record.done)
	}
	server.mu.Unlock()
}

func (server *Server) releaseBuild(key string, record *buildRecord) {
	server.mu.Lock()
	defer server.mu.Unlock()
	record.waiters--
	if record.waiters == 0 {
		select {
		case <-record.done:
		default:
			record.cancel()
			if server.builds[key] == record {
				delete(server.builds, key)
			}
		}
	}
}

func (server *Server) makeSpaceLocked(required int64) error {
	for server.stats.Bytes+required > server.maxBytes {
		var oldest *buildRecord
		var oldestKey string
		var oldestCache map[string]*buildRecord
		for _, cache := range []map[string]*buildRecord{server.builds, server.valueCodes} {
			for key, record := range cache {
				if record.waiters != 0 || record.err != nil || record.bytes == 0 {
					continue
				}
				select {
				case <-record.done:
				default:
					continue
				}
				if oldest == nil || record.lastUsed < oldest.lastUsed {
					oldest, oldestKey, oldestCache = record, key, cache
				}
			}
		}
		if oldest == nil {
			return fmt.Errorf("session storage budget exhausted by immutable inputs or active artifacts: used=%d requested=%d limit=%d", server.stats.Bytes, required, server.maxBytes)
		}
		if err := os.Remove(oldest.result.Artifact); err != nil {
			return fmt.Errorf("evict inactive fixture %q: %w", oldest.result.Artifact, err)
		}
		delete(oldestCache, oldestKey)
		server.stats.Bytes -= oldest.bytes
		server.stats.Evicted++
	}
	return nil
}
