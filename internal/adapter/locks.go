package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"golang.org/x/sys/unix"
)

const (
	MaxFileLocks     = 128
	MaxLockPathBytes = 4096
	MaxLockPathDepth = 256
)

// LockPolicy contains exact file authority, separate from File content grants.
type LockPolicy interface {
	Paths() []string
	Entry() bool
	lockPolicy()
}
type lockPolicy struct {
	paths []string
	entry bool
}

func (lockPolicy) lockPolicy()       {}
func (p lockPolicy) Paths() []string { return slices.Clone(p.paths) }
func (p lockPolicy) Entry() bool     { return p.entry }
func NewLockPolicy(paths []string, entry bool) (LockPolicy, error) {
	if len(paths) > MaxFileLocks {
		return nil, execution.Limit("lock grants")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) || !utf8.ValidString(path) || len(path) > MaxLockPathBytes || len(strings.FieldsFunc(path, func(r rune) bool { return r == '/' })) > MaxLockPathDepth || seen[path] {
			return nil, fmt.Errorf("lock grants require distinct clean absolute paths")
		}
		seen[path] = true
	}
	return lockPolicy{slices.Clone(paths), entry}, nil
}

// LockIO injects only lock descriptor operations. Open must create an independent
// close-on-exec description without creating or truncating the selected file.
type LockIO interface {
	Open(string) (*os.File, error)
	TryExclusive(*os.File) error
	Close(*os.File) error
}
type nativeLockIO struct{}

func NativeLockIO() LockIO { return nativeLockIO{} }
func (nativeLockIO) Open(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}
func (nativeLockIO) TryExclusive(file *os.File) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var result error
	err = raw.Control(func(fd uintptr) { result = unix.Flock(int(fd), unix.LOCK_EX|unix.LOCK_NB) })
	return errors.Join(err, result)
}
func (nativeLockIO) Close(file *os.File) error { return file.Close() }

type lockGrant struct {
	path      string
	identity  value.FileIdentity
	ancestors []processNamespaceObservation
}

// Locks owns grant identities and acquired leases for a complete host lifetime.
type Locks interface {
	Wrap(execution.Boundary) execution.Boundary
	Close() error
	locks()
}

type lockOwner struct {
	mu         sync.Mutex
	grants     map[string]lockGrant
	protection *processRegistry
	io         LockIO
	leases     []*os.File
	pins       []*os.File
	closed     bool
	cleanup    error
}

func (*lockOwner) locks() {}

func lockHost(subject string) error {
	return execution.HostFailure(catalog.LockAcquire, subject, errors.New("lock identity or ownership admission failed"))
}

type lockCloseFailure struct{ cause error }

func (lockCloseFailure) Error() string   { return "lock descriptor close failed" }
func (e lockCloseFailure) Unwrap() error { return e.cause }

// OpenLocks pins authority before evaluation without acquiring any lock.
func OpenLocks(ctx context.Context, policy LockPolicy, entry execution.ProgramEntry, filesystem LockIO) (Locks, error) {
	if policy == nil || filesystem == nil {
		return nil, lockHost("configuration")
	}
	paths := policy.Paths()
	if policy.Entry() {
		if entry == nil {
			return nil, lockHost("entry unavailable")
		}
		if !slices.Contains(paths, entry.Path().Text()) {
			paths = append(paths, entry.Path().Text())
		}
	}
	if len(paths) > MaxFileLocks {
		return nil, execution.Limit("lock grants")
	}
	if _, err := NewLockPolicy(paths, false); err != nil {
		return nil, err
	}
	base, err := OpenProtectedProcessRegistry(ctx, nil, nil, DefaultProcessRegistryLimits())
	if err != nil {
		return nil, err
	}
	owner := &lockOwner{grants: map[string]lockGrant{}, protection: base.(*processRegistry), io: filesystem}
	fail := func(cause error) (Locks, error) { return nil, execution.PreserveCleanup(cause, owner.Close()) }
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return fail(lockHost("grant path"))
		}
		file, err := filesystem.Open(path)
		if err != nil {
			if file != nil {
				if closeErr := filesystem.Close(file); closeErr != nil {
					return fail(execution.HostFailure(catalog.LockAcquire, "partial grant cleanup", lockCloseFailure{closeErr}))
				}
			}
			return fail(lockHost("grant open"))
		}
		if file == nil {
			return fail(lockHost("malformed grant open result"))
		}
		owner.pins = append(owner.pins, file)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fail(lockHost("regular file"))
		}
		identity, _, err := nativeFileIdentity(info)
		if err != nil {
			return fail(lockHost("grant identity"))
		}
		if entry != nil && path == entry.Path().Text() && !os.SameFile(entry.Info(), info) {
			return fail(lockHost("entry replacement"))
		}
		ancestors, err := owner.protection.protectNamespace(ctx, path)
		if err != nil {
			return fail(err)
		}
		owner.protection.identities[identity] = struct{}{}
		grant := lockGrant{path, identity, ancestors}
		if err := grant.validate(ctx); err != nil {
			return fail(err)
		}
		owner.grants[path] = grant
	}
	return owner, nil
}

func (g lockGrant) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(g.path)
	if err != nil || resolved != g.path {
		return lockHost("pathname")
	}
	for _, ancestor := range g.ancestors {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(ancestor.path)
		if err != nil {
			return lockHost("parent pathname")
		}
		identity, _, err := nativeFileIdentity(info)
		if err != nil || identity != ancestor.identity {
			return lockHost("parent identity")
		}
	}
	info, err := os.Lstat(g.path)
	if err != nil || !info.Mode().IsRegular() {
		return lockHost("pathname identity")
	}
	identity, _, err := nativeFileIdentity(info)
	if err != nil || identity != g.identity {
		return lockHost("replacement")
	}
	return nil
}

func (l *lockOwner) acquire(ctx context.Context, target value.LockFile) (value.Value, error) {
	if l == nil || l.protection == nil {
		return nil, lockHost("configuration")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, lockHost("closed host")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if target.Path() == nil {
		return nil, execution.Denied(catalog.LockAcquire, "snapshot")
	}
	grant, ok := l.grants[target.Path().Text()]
	if !ok {
		return nil, execution.Denied(catalog.LockAcquire, "file")
	}
	if len(l.leases) >= MaxFileLocks {
		return nil, execution.Limit("lock leases")
	}
	if err := grant.validate(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		failure, _ := execution.NewLockError(catalog.LockFailed)
		return nil, failure
	}
	file, err := l.io.Open(grant.path)
	if file == nil {
		if err == nil {
			return nil, lockHost("malformed acquisition open result")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		failure, _ := execution.NewLockError(catalog.LockFailed)
		return nil, failure
	}
	fail := func(cause error) (value.Value, error) {
		if err := l.io.Close(file); err != nil {
			return nil, execution.HostFailure(catalog.LockAcquire, "descriptor cleanup", lockCloseFailure{err})
		}
		return nil, cause
	}
	failed, _ := execution.NewLockError(catalog.LockFailed)
	if err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(failed)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fail(failed)
	}
	identity, _, err := nativeFileIdentity(info)
	if err != nil || identity != grant.identity {
		return fail(failed)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := l.io.TryExclusive(file); err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		kind := catalog.LockFailed
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			kind = catalog.LockBusy
		}
		failure, _ := execution.NewLockError(kind)
		return fail(failure)
	}
	if err := grant.validate(ctx); err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(failed)
	}
	l.leases = append(l.leases, file)
	return value.NewLockLease(), nil
}

// Close releases leases once, after the host joins all participating children.
func (l *lockOwner) Close() error {
	if l == nil {
		return nil
	}
	if l.protection == nil {
		return lockHost("configuration")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.cleanup
	}
	l.closed = true
	var failures []error
	for _, file := range l.leases {
		if err := l.io.Close(file); err != nil {
			failures = append(failures, execution.HostFailure(catalog.LockAcquire, "lease close", lockCloseFailure{err}))
		}
	}
	for _, file := range l.pins {
		if err := l.io.Close(file); err != nil {
			failures = append(failures, execution.HostFailure(catalog.LockAcquire, "grant close", lockCloseFailure{err}))
		}
	}
	failures = append(failures, l.protection.Close())
	l.cleanup = errors.Join(failures...)
	return l.cleanup
}

type lockBoundary struct {
	execution.Boundary
	owner *lockOwner
}

func (l *lockOwner) Wrap(boundary execution.Boundary) execution.Boundary {
	return &lockBoundary{boundary, l}
}
func (b *lockBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	if lock, ok := target.(value.LockFile); ok {
		return b.owner.acquire(ctx, lock)
	}
	return b.Boundary.Commit(ctx, target)
}
func (b *lockBoundary) Directory(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileDirectory, path)
	if err != nil {
		return nil, err
	}
	return observer.Directory(ctx, path)
}
func (b *lockBoundary) Scan(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileScan, path)
	if err != nil {
		return nil, err
	}
	return observer.Scan(ctx, path)
}
