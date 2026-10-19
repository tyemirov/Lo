package adapter

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"golang.org/x/sys/unix"
)

const defaultProcessRegistryMaxBytes int64 = 64 << 20

// ProcessExecutableGrant binds one clean absolute path to reviewed bytes.
type ProcessExecutableGrant interface {
	Path() string
	Digest() [32]byte
	processExecutableGrant()
}
type processExecutableGrant struct {
	path   string
	digest [32]byte
}

func (processExecutableGrant) processExecutableGrant() {}
func (g processExecutableGrant) Path() string          { return g.path }
func (g processExecutableGrant) Digest() [32]byte      { return g.digest }

// NewProcessExecutableGrant validates a description without filesystem access.
func NewProcessExecutableGrant(path string, digest [32]byte) (ProcessExecutableGrant, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("Process executable grant requires a clean absolute NUL-free path")
	}
	return processExecutableGrant{path, digest}, nil
}

// ProcessRegistryLimits bounds each registration and admission hash.
type ProcessRegistryLimits interface {
	MaxBytes() int64
	processRegistryLimits()
}
type processRegistryLimits struct{ maximum int64 }

func (processRegistryLimits) processRegistryLimits() {}
func (l processRegistryLimits) MaxBytes() int64      { return l.maximum }

// NewProcessRegistryLimits selects a positive host hashing bound.
func NewProcessRegistryLimits(maxBytes int64) (ProcessRegistryLimits, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("Process registry byte bound must be positive")
	}
	return processRegistryLimits{maxBytes}, nil
}

// DefaultProcessRegistryLimits permits 64 MiB per registered executable.
func DefaultProcessRegistryLimits() ProcessRegistryLimits {
	return processRegistryLimits{defaultProcessRegistryMaxBytes}
}

// ProcessRegistry coordinates trusted drivers and protected File adapters.
// It does not qualify a native driver or enable the standard Process host.
type ProcessRegistry interface {
	OpenFiles(FileRoots, FileIO) (Files, error)
	Driver(ProcessDriver) (ProcessDriver, error)
	Close() error
	processRegistry()
}

type processNamespaceObservation struct {
	path     string
	identity value.FileIdentity
}
type processRegistration struct {
	grant     ProcessExecutableGrant
	file      *os.File
	identity  value.FileIdentity
	ancestors []processNamespaceObservation
}
type processNamespaceEdge struct {
	parent value.FileIdentity
	name   string
}
type processRegistry struct {
	registrations                           map[string]processRegistration
	identities                              map[value.FileIdentity]struct{}
	namespace                               map[processNamespaceEdge]struct{}
	directories                             map[value.FileIdentity]*os.Root
	files                                   []*os.File
	roots                                   []*os.Root
	maximum                                 int64
	mu                                      sync.Mutex
	active                                  int
	closing, closed, releasing, quarantined bool
}

func (*processRegistry) processRegistry() {}
func registryHost(subject string, cause error) error {
	return execution.HostFailure("Process registry", subject, cause)
}

// OpenProcessRegistry freezes reviewed descriptors and protected namespace edges.
// Namespace protection covers participating File mutators; external writers are
// excluded. Future rename, arbitrary link, symlink, or directory removal APIs must
// revisit that scope.
func OpenProcessRegistry(ctx context.Context, grants []ProcessExecutableGrant, limits ProcessRegistryLimits) (ProcessRegistry, error) {
	if limits == nil {
		return nil, registryHost("registration", errors.New("missing registry limits"))
	}
	r := &processRegistry{registrations: map[string]processRegistration{}, identities: map[value.FileIdentity]struct{}{}, namespace: map[processNamespaceEdge]struct{}{}, directories: map[value.FileIdentity]*os.Root{}, maximum: limits.MaxBytes()}
	fail := func(err error) (ProcessRegistry, error) { return nil, errors.Join(err, r.Close()) }
	for _, grant := range grants {
		if err := ctx.Err(); err != nil {
			return fail(registryHost("registration", err))
		}
		if grant == nil {
			return fail(registryHost("registration", errors.New("missing executable grant")))
		}
		if _, exists := r.registrations[grant.Path()]; exists {
			return fail(registryHost("registration", errors.New("duplicate executable grant")))
		}
		file, err := os.OpenFile(grant.Path(), os.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			return fail(registryHost("open executable", err))
		}
		r.files = append(r.files, file)
		info, err := file.Stat()
		if err != nil {
			return fail(registryHost("observe executable", err))
		}
		if !info.Mode().IsRegular() {
			return fail(registryHost("registration", errors.New("executable must be a regular file")))
		}
		identity, _, err := nativeFileIdentity(info)
		if err != nil {
			return fail(registryHost("observe executable", err))
		}
		digest, err := hashProcessFile(ctx, file, r.maximum)
		if err != nil {
			return fail(registryHost("hash executable", err))
		}
		if digest != grant.Digest() {
			return fail(registryHost("registration", errors.New("executable digest differs")))
		}
		r.identities[identity] = struct{}{}
		ancestors, err := r.protectNamespace(ctx, grant.Path())
		if err != nil {
			return fail(err)
		}
		r.registrations[grant.Path()] = processRegistration{grant, file, identity, ancestors}
	}
	if err := ctx.Err(); err != nil {
		return fail(registryHost("registration", err))
	}
	return r, nil
}

func hashProcessFile(ctx context.Context, file *os.File, maximum int64) ([32]byte, error) {
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		size := len(buffer)
		if remaining := maximum - offset; remaining < int64(size) {
			size = int(remaining) + 1
		}
		count, err := file.ReadAt(buffer[:size], offset)
		if int64(count) > maximum-offset {
			return [32]byte{}, errors.New("executable exceeds registry byte bound")
		}
		offset += int64(count)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		if errors.Is(err, io.EOF) {
			var result [32]byte
			copy(result[:], hash.Sum(nil))
			return result, nil
		}
		if err != nil {
			return [32]byte{}, err
		}
		if count == 0 {
			return [32]byte{}, io.ErrNoProgress
		}
	}
}

func (r *processRegistry) protectNamespace(ctx context.Context, path string) ([]processNamespaceObservation, error) {
	var observations []processNamespaceObservation
	for {
		if err := ctx.Err(); err != nil {
			return nil, registryHost("observe namespace", err)
		}
		parent := filepath.Dir(path)
		root, err := os.OpenRoot(parent)
		if err != nil {
			return nil, registryHost("retain namespace", err)
		}
		info, err := root.Stat(".")
		if err != nil {
			return nil, registryHost("observe namespace", errors.Join(err, root.Close()))
		}
		identity, _, err := nativeFileIdentity(info)
		if err != nil {
			return nil, registryHost("observe namespace", errors.Join(err, root.Close()))
		}
		if _, exists := r.directories[identity]; exists {
			if err := root.Close(); err != nil {
				return nil, registryHost("close duplicate namespace handle", err)
			}
		} else {
			r.directories[identity] = root
			r.roots = append(r.roots, root)
		}
		r.identities[identity] = struct{}{}
		r.namespace[processNamespaceEdge{identity, filepath.Base(path)}] = struct{}{}
		observations = append(observations, processNamespaceObservation{parent, identity})
		if parent == path {
			return observations, nil
		}
		path = parent
	}
}

type registryAdmission uint8

const (
	registryFileAdmission registryAdmission = iota
	registryProcessAdmission
)

type processRegistryLease struct {
	registry *processRegistry
	once     sync.Once
}

func (l *processRegistryLease) end() {
	l.once.Do(func() { l.registry.mu.Lock(); l.registry.active--; l.registry.mu.Unlock() })
}
func (r *processRegistry) admit(kind registryAdmission) (*processRegistryLease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing || r.closed {
		return nil, registryHost("admission", errors.New("registry is closing or closed"))
	}
	if kind == registryProcessAdmission && r.quarantined {
		return nil, registryHost("admission", errors.New("registry is quarantined"))
	}
	r.active++
	return &processRegistryLease{registry: r}, nil
}
func (r *processRegistry) cleanupLease() *processRegistryLease {
	r.mu.Lock()
	r.active++
	r.mu.Unlock()
	return &processRegistryLease{registry: r}
}
func (r *processRegistry) quarantine() { r.mu.Lock(); r.quarantined = true; r.mu.Unlock() }

// Close permanently stops admission. Active or unresolved operations retain all
// registration descriptors; callers can retry after owned cleanup completes.
func (r *processRegistry) Close() error {
	r.mu.Lock()
	r.closing = true
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	if r.active != 0 || r.releasing {
		r.mu.Unlock()
		return registryHost("close", errors.New("active or unresolved registry ownership"))
	}
	r.releasing = true
	r.mu.Unlock()
	var failures []error
	for _, file := range r.files {
		failures = append(failures, file.Close())
	}
	for _, root := range r.roots {
		failures = append(failures, root.Close())
	}
	r.mu.Lock()
	r.closed = true
	r.releasing = false
	r.mu.Unlock()
	return errors.Join(failures...)
}

// Driver wraps every transferred child and validates registration before Start.
func (r *processRegistry) Driver(driver ProcessDriver) (ProcessDriver, error) {
	lease, err := r.admit(registryProcessAdmission)
	if err != nil {
		return nil, err
	}
	defer lease.end()
	if driver == nil {
		return nil, registryHost("driver", errors.New("missing driver"))
	}
	return &processRegistryDriver{registry: r, driver: driver}, nil
}

type processRegistryDriver struct {
	registry *processRegistry
	driver   ProcessDriver
}

func (d *processRegistryDriver) Start(ctx context.Context, invocation ProcessInvocation) (ProcessChild, error) {
	lease, err := d.registry.admit(registryProcessAdmission)
	if err != nil {
		return nil, err
	}
	if err := d.registry.validate(ctx, invocation); err != nil {
		lease.end()
		return nil, err
	}
	child, startErr := d.driver.Start(ctx, invocation)
	if child == nil {
		if startErr != nil {
			var unresolved *processUnresolvedStartError
			if errors.As(startErr, &unresolved) {
				d.registry.quarantine()
				return nil, startErr
			}
			lease.end()
			return nil, startErr
		}
		d.registry.quarantine()
		return nil, newProcessUnresolvedStartError()
	}
	return &processRegistryChild{ProcessChild: child, lease: lease}, startErr
}
func (r *processRegistry) validate(ctx context.Context, invocation ProcessInvocation) error {
	if err := ctx.Err(); err != nil {
		return registryHost("admission", err)
	}
	if invocation == nil {
		return registryHost("admission", errors.New("missing invocation"))
	}
	registration, ok := r.registrations[invocation.Executable()]
	if !ok {
		return registryHost("admission", errors.New("unregistered executable"))
	}
	for _, ancestor := range registration.ancestors {
		if err := ctx.Err(); err != nil {
			return registryHost("admission", err)
		}
		info, err := os.Stat(ancestor.path)
		if err != nil {
			return registryHost("admission", err)
		}
		identity, _, err := nativeFileIdentity(info)
		if err != nil {
			return registryHost("admission", err)
		}
		if identity != ancestor.identity {
			return registryHost("admission", errors.New("registered namespace identity differs"))
		}
	}
	current, err := os.OpenFile(registration.grant.Path(), os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return registryHost("admission", err)
	}
	info, statErr := current.Stat()
	closeErr := current.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return registryHost("admission", err)
	}
	identity, _, err := nativeFileIdentity(info)
	if err != nil {
		return registryHost("admission", err)
	}
	if identity != registration.identity {
		return registryHost("admission", errors.New("executable identity differs"))
	}
	retained, err := registration.file.Stat()
	if err != nil {
		return registryHost("admission", err)
	}
	retainedIdentity, _, err := nativeFileIdentity(retained)
	if err != nil {
		return registryHost("admission", err)
	}
	if retainedIdentity != registration.identity {
		return registryHost("admission", errors.New("retained executable identity differs"))
	}
	digest, err := hashProcessFile(ctx, registration.file, r.maximum)
	if err != nil {
		return registryHost("admission", err)
	}
	if digest != registration.grant.Digest() {
		return registryHost("admission", errors.New("executable digest differs"))
	}
	return nil
}

type processRegistryChild struct {
	ProcessChild
	lease               *processRegistryLease
	mu                  sync.Mutex
	releasing, released bool
}

func (c *processRegistryChild) Release(ctx context.Context) error {
	c.mu.Lock()
	if c.released {
		c.mu.Unlock()
		return nil
	}
	if c.releasing {
		c.mu.Unlock()
		c.lease.registry.quarantine()
		return registryHost("Release", errors.New("child release is already in progress"))
	}
	c.releasing = true
	c.mu.Unlock()
	err := c.ProcessChild.Release(ctx)
	if err != nil {
		c.lease.registry.quarantine()
	}
	c.mu.Lock()
	c.releasing = false
	if err == nil {
		c.released = true
	}
	c.mu.Unlock()
	if err == nil {
		c.lease.end()
	}
	return err
}
