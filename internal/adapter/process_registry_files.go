package adapter

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"golang.org/x/sys/unix"
)

// OpenFiles creates an adapter whose authority is coordinated with this registry.
func (r *processRegistry) OpenFiles(roots FileRoots, filesystem FileIO) (Files, error) {
	lease, err := r.admit(registryFileAdmission)
	if err != nil {
		return nil, err
	}
	defer lease.end()
	if filesystem == nil {
		return nil, registryHost("File factory", errors.New("missing filesystem operations"))
	}
	base, err := OpenFiles(roots, registryFileIO{FileIO: filesystem, registry: r})
	if err != nil {
		return nil, err
	}
	base.(*files).protections = append(base.(*files).protections, r)
	return &processRegistryFiles{Files: base, registry: r}, nil
}

// Cleanup callbacks remain counted without reopening authority after Close.
type registryFileIO struct {
	FileIO
	registry *processRegistry
}

func (f registryFileIO) CloseDirectory(root *os.Root) error {
	lease := f.registry.cleanupLease()
	defer lease.end()
	return f.FileIO.CloseDirectory(root)
}

type processRegistryFiles struct {
	Files
	registry                   *processRegistry
	mu                         sync.Mutex
	active                     int
	closing, releasing, closed bool
}

func (f *processRegistryFiles) begin() (func(), error) {
	f.mu.Lock()
	if f.closing || f.closed {
		f.mu.Unlock()
		return nil, registryHost("File admission", errors.New("File adapter is closing or closed"))
	}
	lease, err := f.registry.admit(registryFileAdmission)
	if err != nil {
		f.mu.Unlock()
		return nil, err
	}
	f.active++
	f.mu.Unlock()
	return func() { f.mu.Lock(); f.active--; f.mu.Unlock(); lease.end() }, nil
}
func (f *processRegistryFiles) Read(ctx context.Context, path value.Text) (value.Bytes, error) {
	end, err := f.begin()
	if err != nil {
		return nil, err
	}
	defer end()
	return f.Files.Read(ctx, path)
}
func (f *processRegistryFiles) List(ctx context.Context, path value.Text) (value.List, error) {
	end, err := f.begin()
	if err != nil {
		return nil, err
	}
	defer end()
	return f.Files.List(ctx, path)
}
func (f *processRegistryFiles) OpenPaths(ctx context.Context, path value.Text) (value.List, error) {
	end, err := f.begin()
	if err != nil {
		return nil, err
	}
	defer end()
	return f.Files.OpenPaths(ctx, path)
}
func (f *processRegistryFiles) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	end, err := f.begin()
	if err != nil {
		return nil, err
	}
	defer end()
	return f.Files.Commit(ctx, target)
}
func (f *processRegistryFiles) Close() error {
	f.mu.Lock()
	f.closing = true
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	if f.active != 0 || f.releasing {
		f.mu.Unlock()
		return registryHost("File close", errors.New("File adapter has active operations"))
	}
	f.releasing = true
	f.mu.Unlock()
	lease := f.registry.cleanupLease()
	err := f.Files.Close()
	lease.end()
	f.mu.Lock()
	f.releasing = false
	f.closed = true
	f.mu.Unlock()
	return err
}

func (r *processRegistry) protectIdentity(info os.FileInfo) error {
	identity, _, err := nativeFileIdentity(info)
	if err != nil {
		return registryHost("File mutation identity", err)
	}
	if _, protected := r.identities[identity]; protected {
		return registryHost("File mutation", errors.New("registered executable or namespace is protected"))
	}
	return nil
}
func (r *processRegistry) protectFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return registryHost("File mutation identity", err)
	}
	return r.protectIdentity(info)
}
func (r *processRegistry) protectName(root *os.Root, path string) error {
	parent, err := root.OpenRoot(filepath.Dir(path))
	if err != nil {
		return registryHost("File mutation namespace", err)
	}
	info, statErr := parent.Stat(".")
	closeErr := parent.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return registryHost("File mutation namespace", err)
	}
	identity, _, err := nativeFileIdentity(info)
	if err != nil {
		return registryHost("File mutation namespace", err)
	}
	if _, protected := r.namespace[processNamespaceEdge{identity, filepath.Base(path)}]; protected {
		return registryHost("File mutation", errors.New("registered namespace entry is protected"))
	}
	return nil
}
func (r *processRegistry) protectRemoval(root *os.Root, path string) error {
	if err := r.protectName(root, path); err != nil {
		return err
	}
	info, err := root.Lstat(path)
	if err != nil {
		return err
	}
	return r.protectIdentity(info)
}

func (f *files) protectName(root *os.Root, path string) error {
	for _, protection := range f.protections {
		if err := protection.protectName(root, path); err != nil {
			return err
		}
	}
	return nil
}

func (f *files) protectFile(file *os.File) error {
	for _, protection := range f.protections {
		if err := protection.protectFile(file); err != nil {
			return err
		}
	}
	return nil
}

func (f *files) protectRemoval(root *os.Root, path string) error {
	for _, protection := range f.protections {
		if err := protection.protectRemoval(root, path); err != nil {
			return err
		}
	}
	return nil
}

func (f *files) protectedContent(root *os.Root, path, absolute string, appendContent bool) (*os.File, error) {
	if err := f.protectName(root, path); err != nil {
		return nil, err
	}
	flags := os.O_WRONLY | unix.O_NONBLOCK
	if appendContent {
		flags |= os.O_APPEND
	}
	file, err := root.OpenFile(path, flags, 0)
	if errors.Is(err, os.ErrNotExist) {
		created, collision, createErr := f.protectedCreate(root, path, absolute)
		if createErr != nil {
			return nil, createErr
		}
		if collision {
			if !appendContent {
				return nil, os.ErrExist
			}
		} else if !appendContent {
			return created, nil
		} else if err := created.Close(); err != nil {
			return nil, err
		}
		file, err = root.OpenFile(path, flags, 0)
	}
	if err != nil {
		return nil, err
	}
	if err := f.protectFile(file); err != nil {
		return nil, execution.WithOperationCleanup(err, f.io.CloseFile(file))
	}
	if !appendContent {
		if err := file.Truncate(0); err != nil {
			return nil, execution.WithOperationCleanup(err, f.io.CloseFile(file))
		}
	}
	return file, nil
}

func (f *files) protectedCreate(root *os.Root, path, absolute string) (*os.File, bool, error) {
	parentPath := filepath.Dir(absolute)
	parentIdentity, err := verifiedProcessParent(root, filepath.Dir(path), parentPath)
	if err != nil {
		return nil, false, err
	}
	prefix := ".lo-create-" + rand.Text() + "-"
	// Every name CreateTemp can select must avoid the frozen protected edges.
	for _, protection := range f.protections {
		for edge := range protection.namespace {
			if edge.parent == parentIdentity && strings.HasPrefix(edge.name, prefix) {
				return nil, false, registryHost("File creation", errors.New("temporary namespace prefix is protected"))
			}
		}
	}
	// CreateTemp supplies library-default exclusive creation. The verified parent
	// pathname remains stable under this registry's trusted-host model: external
	// namespace writers and participating rename/symlink/directory removal are
	// excluded. Publication and cleanup still use the retained granted root.
	created, err := os.CreateTemp(parentPath, prefix)
	if err != nil {
		return nil, false, err
	}
	temporary := filepath.Join(filepath.Dir(path), filepath.Base(created.Name()))
	cleanup := func() error {
		if err := f.protectRemoval(root, temporary); err != nil {
			return err
		}
		return root.Remove(temporary)
	}
	fail := func(primary error) (*os.File, bool, error) {
		closeErr := created.Close()
		return nil, false, execution.WithOperationCleanup(primary, execution.WithOperationCleanup(closeErr, cleanup()))
	}
	if err := f.protectName(root, temporary); err != nil {
		return fail(err)
	}
	if err := f.protectFile(created); err != nil {
		return fail(err)
	}
	if err := f.protectName(root, path); err != nil {
		return fail(err)
	}
	// Neither participating File operations nor this link can replace an entry.
	linkErr := root.Link(temporary, path)
	if errors.Is(linkErr, os.ErrExist) {
		_, _, cleanupErr := fail(nil)
		return nil, true, cleanupErr
	}
	if linkErr != nil {
		return fail(linkErr)
	}
	if err := cleanup(); err != nil {
		return nil, false, execution.WithOperationCleanup(err, created.Close())
	}
	return created, false, nil
}

func verifiedProcessParent(root *os.Root, relative, absolute string) (value.FileIdentity, error) {
	parent, err := root.OpenRoot(relative)
	if err != nil {
		return value.FileIdentity{}, registryHost("File creation parent", err)
	}
	retained, statErr := parent.Stat(".")
	closeErr := parent.Close()
	if err := execution.WithOperationCleanup(statErr, closeErr); err != nil {
		return value.FileIdentity{}, registryHost("File creation parent", err)
	}
	retainedIdentity, _, err := nativeFileIdentity(retained)
	if err != nil {
		return value.FileIdentity{}, registryHost("File creation parent", err)
	}
	current, err := os.Stat(absolute)
	if err != nil {
		return value.FileIdentity{}, registryHost("File creation parent", err)
	}
	currentIdentity, _, err := nativeFileIdentity(current)
	if err != nil {
		return value.FileIdentity{}, registryHost("File creation parent", err)
	}
	if currentIdentity != retainedIdentity {
		return value.FileIdentity{}, registryHost("File creation parent", errors.New("parent pathname differs from retained granted parent"))
	}
	return retainedIdentity, nil
}
