package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// FileIO is the file content, metadata, removal, and retained-handle boundary.
type FileIO interface {
	Write(context.Context, *os.File, []byte) (int, error)
	CloseFile(*os.File) error
	ProbeOpenFiles(context.Context, *os.Root, FileProbeExecutable) ([]byte, error)
	ReadDir(*os.File, int) ([]os.DirEntry, error)
	Lstat(*os.Root, string) (os.FileInfo, error)
	Remove(*os.Root, string) error
	CloseDirectory(*os.Root) error
}
type nativeFileIO struct{}

func (nativeFileIO) Write(ctx context.Context, file *os.File, data []byte) (int, error) {
	return writeFile(ctx, file, data)
}
func (nativeFileIO) CloseFile(file *os.File) error { return file.Close() }

func (nativeFileIO) ReadDir(file *os.File, count int) ([]os.DirEntry, error) {
	return file.ReadDir(count)
}

func (nativeFileIO) Lstat(root *os.Root, name string) (os.FileInfo, error) { return root.Lstat(name) }
func (nativeFileIO) Remove(root *os.Root, name string) error               { return root.Remove(name) }
func (nativeFileIO) CloseDirectory(root *os.Root) error                    { return root.Close() }
func NativeFileIO() FileIO                                                 { return nativeFileIO{} }

type observedDirectory struct {
	root  *os.Root
	lease value.FileLease
}

type fileRegistry struct {
	directories map[value.FileDirectory]observedDirectory
}

func exactPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve file directory %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve file directory %q: %w", absolute, err)
	}
	if resolved != absolute {
		return "", fmt.Errorf("file directory %q traverses a symlink", absolute)
	}
	return absolute, nil
}

func openExactRoots(paths []string) ([]grant, error) {
	roots := []grant{}
	seen := map[string]bool{}
	fail := func(err error) ([]grant, error) { return nil, errors.Join(err, closeRoots(roots, (*os.Root).Close)) }
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fail(err)
		}
		if seen[absolute] {
			return fail(fmt.Errorf("duplicate adapter root %q", absolute))
		}
		seen[absolute] = true
		// An absent grant stays inert for this invocation, including if a directory appears later.
		prefix := absolute
		for {
			_, statErr := os.Lstat(prefix)
			if errors.Is(statErr, os.ErrNotExist) {
				prefix = filepath.Dir(prefix)
				continue
			}
			if statErr != nil {
				return fail(fmt.Errorf("observe adapter root %q: %w", absolute, statErr))
			}
			if _, err := exactPath(prefix); err != nil {
				return fail(err)
			}
			break
		}
		if prefix != absolute {
			roots = append(roots, grant{path: absolute})
			continue
		}
		root, err := os.OpenRoot(absolute)
		if err != nil {
			return fail(fmt.Errorf("open adapter root %q: %w", absolute, err))
		}
		roots = append(roots, grant{path: absolute, root: root})
	}
	return roots, nil
}

func (f *files) exactGrant(roots []grant, operation, path string) (grant, value.FileDirectory, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.base, path)
	}
	path = filepath.Clean(path)
	for _, candidate := range roots {
		if candidate.path != path {
			continue
		}
		if candidate.root == nil {
			_, err := os.Lstat(path)
			if (operation == catalog.FileList || operation == catalog.FileDirectory || operation == catalog.FileScan) && errors.Is(err, os.ErrNotExist) {
				return grant{}, value.FileDirectory{}, execution.NewFileError(catalog.DirectoryAbsent, nil, nil, fmt.Errorf("directory %q is absent", path))
			}
			return grant{}, value.FileDirectory{}, execution.Denied(operation, path)
		}
		if _, err := exactPath(path); err != nil {
			if (operation == catalog.FileList || operation == catalog.FileDirectory || operation == catalog.FileScan) && errors.Is(err, os.ErrNotExist) {
				return grant{}, value.FileDirectory{}, execution.NewFileError(catalog.DirectoryAbsent, nil, nil, err)
			}
			return grant{}, value.FileDirectory{}, execution.Denied(operation, path)
		}
		current, err := os.Lstat(path)
		if err != nil {
			return grant{}, value.FileDirectory{}, execution.Denied(operation, path)
		}
		opened, err := candidate.root.Stat(".")
		if err != nil {
			return grant{}, value.FileDirectory{}, execution.HostFailure(operation, path, err)
		}
		if !current.IsDir() || !os.SameFile(current, opened) {
			return grant{}, value.FileDirectory{}, execution.Denied(operation, path)
		}
		identity, _, err := nativeFileIdentity(opened)
		if err != nil {
			return grant{}, value.FileDirectory{}, execution.HostFailure(operation, path, err)
		}
		return candidate, value.FileDirectory{Path: path, Identity: identity}, nil
	}
	return grant{}, value.FileDirectory{}, execution.Denied(operation, path)
}

func (f *files) directory(session execution.Session, source grant, identity value.FileDirectory) (*os.Root, value.FileLease, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := session.FileSession()
	registry := f.sessions[key]
	if registry == nil {
		registry = &fileRegistry{directories: map[value.FileDirectory]observedDirectory{}}
		if err := session.OnClose(func() error { return f.closeSession(key) }); err != nil {
			return nil, value.FileLease{}, false, err
		}
		f.sessions[key] = registry
	}
	if observed, ok := registry.directories[identity]; ok {
		return observed.root, observed.lease, false, nil
	}
	if limit := session.Limits().FileDirectories; limit != 0 && uint64(len(registry.directories)) >= limit {
		return nil, value.FileLease{}, false, execution.Limit("retained file directories")
	}
	root, err := source.root.OpenRoot(".")
	if err != nil {
		return nil, value.FileLease{}, false, err
	}
	lease := value.NewFileLease()
	if err := session.RegisterFileLease(lease, func() error { return f.releaseDirectory(key, identity, lease) }); err != nil {
		return nil, value.FileLease{}, false, execution.PreserveCleanup(err, f.io.CloseDirectory(root))
	}
	registry.directories[identity] = observedDirectory{root, lease}
	return root, lease, true, nil
}

func (f *files) closeSession(key value.FileSession) error {
	f.mu.Lock()
	registry := f.sessions[key]
	delete(f.sessions, key)
	f.mu.Unlock()
	if registry == nil {
		return nil
	}
	var result error
	for directory, observed := range registry.directories {
		if err := f.io.CloseDirectory(observed.root); err != nil {
			result = errors.Join(result, fmt.Errorf("close observed directory %q: %w", directory.Path, err))
		}
	}
	return result
}

func (f *files) releaseDirectory(session value.FileSession, directory value.FileDirectory, lease value.FileLease) error {
	f.mu.Lock()
	registry := f.sessions[session]
	if registry == nil {
		f.mu.Unlock()
		return nil
	}
	observed, ok := registry.directories[directory]
	if !ok || observed.lease != lease {
		f.mu.Unlock()
		return nil
	}
	delete(registry.directories, directory)
	f.mu.Unlock()
	if err := f.io.CloseDirectory(observed.root); err != nil {
		return fmt.Errorf("close observed directory %q: %w", directory.Path, err)
	}
	return nil
}

func listFailure(kind, path string, err error) error {
	if execution.BoundaryHostError(err) != nil {
		return err
	}
	return execution.NewFileError(kind, nil, nil, fmt.Errorf("%s %q: %w", catalog.FileList, path, err))
}

func (f *files) List(ctx context.Context, path value.Text) (result value.List, resultErr error) {
	session, ok := execution.SessionFromContext(ctx)
	if !ok {
		return nil, execution.HostFailure(catalog.FileList, path.Text(), errors.New("missing execution session"))
	}
	if err := ctx.Err(); err != nil {
		return nil, execution.BoundaryHostError(err)
	}
	selected, directory, err := f.exactGrant(f.lists, catalog.FileList, path.Text())
	if err != nil {
		return nil, err
	}
	root, lease, fresh, err := f.directory(session, selected, directory)
	if err != nil {
		return nil, listFailure(catalog.ListFailed, path.Text(), err)
	}
	defer func() {
		if resultErr != nil && fresh {
			resultErr = execution.PreserveCleanup(resultErr, f.releaseDirectory(session.FileSession(), directory, lease))
		}
	}()
	stream, err := root.Open(".")
	if err != nil {
		return nil, listFailure(catalog.ListFailed, path.Text(), err)
	}
	closeStream := cancelFile(ctx, stream)
	defer func() {
		if err := closeStream(); err != nil {
			result = nil
			resultErr = execution.PreserveCleanup(resultErr, err)
		}
	}()
	items := make([]value.Value, 0)
	// The published result includes the list and its fallible success wrapper.
	weight := uint64(32)
	for {
		if err := ctx.Err(); err != nil {
			return nil, execution.BoundaryHostError(err)
		}
		children, readErr := f.io.ReadDir(stream, 128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, listFailure(catalog.ListFailed, directory.Path, readErr)
		}
		for _, child := range children {
			if limit := session.Limits().FileEntries; limit != 0 && uint64(len(items)) >= limit {
				return nil, execution.Limit("directory entries")
			}
			if err := session.ReserveFile(1, weight); err != nil {
				return nil, err
			}
			name := child.Name()
			if !utf8.ValidString(name) {
				return nil, listFailure(catalog.InvalidName, directory.Path, errors.New("invalid UTF-8 filename"))
			}
			info, err := f.io.Lstat(root, name)
			if err != nil {
				return nil, listFailure(catalog.ListFailed, filepath.Join(directory.Path, name), err)
			}
			entry, err := observeEntry(session, directory, lease, name, info, weight)
			if err != nil {
				return nil, listFailure(catalog.ListFailed, filepath.Join(directory.Path, name), err)
			}
			weight += entry.Weight()
			items = append(items, entry)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	count := uint64(len(items))
	var sortSteps uint64
	if count > 1 {
		sortSteps = count * uint64(bits.Len64(count-1))
	}
	if err := session.ReserveFile(sortSteps, weight); err != nil {
		return nil, err
	}
	slices.SortFunc(items, func(a, b value.Value) int {
		return strings.Compare(a.(value.FileEntry).FieldsRecord().Field("name").(value.Text).Text(), b.(value.FileEntry).FieldsRecord().Field("name").(value.Text).Text())
	})
	listType, err := types.NewList(types.FileEntry())
	if err != nil {
		return nil, err
	}
	return value.NewList(listType, items)
}

func observeEntry(session execution.Session, directory value.FileDirectory, lease value.FileLease, name string, info os.FileInfo, priorWeight uint64) (value.FileEntry, error) {
	identity, links, err := nativeFileIdentity(info)
	if err != nil {
		return nil, err
	}
	metadata, err := nativeFileMetadata(info)
	if err != nil {
		return nil, err
	}
	kind := value.OtherFile
	switch info.Mode().Type() {
	case 0:
		kind = value.RegularFile
	case os.ModeDir:
		kind = value.DirectoryFile
	case os.ModeSymlink:
		kind = value.SymlinkFile
	}
	pathValue, err := value.NewText(types.Path(), filepath.Join(directory.Path, name))
	if err != nil {
		return nil, err
	}
	nameValue, err := value.NewText(types.String(), name)
	if err != nil {
		return nil, err
	}
	linkValue, err := value.NewInt(new(big.Int).SetUint64(links))
	if err != nil {
		return nil, err
	}
	sizeValue, modifiedValue := value.Int64(info.Size()), value.Int64(metadata.modified)
	ownerValue, accessedValue, changedValue := value.Int64(int64(metadata.owner)), value.Int64(metadata.accessed), value.Int64(metadata.changed)
	// Reserve the exact expanded entry representation before its record allocation.
	weight := uint64(16 + 128 + len(directory.Path) + 16)
	weight += uint64(len("path") + len("name") + len("kind") + len("size_bytes") + len("modified_unix_ns") + len("link_count") + len("owner_uid") + len("accessed_unix_ns") + len("changed_unix_ns"))
	weight += pathValue.Weight() + nameValue.Weight() + uint64(16+len(kind)) + sizeValue.Weight() + modifiedValue.Weight() + linkValue.Weight() + ownerValue.Weight() + accessedValue.Weight() + changedValue.Weight()
	if err := session.ReserveFile(0, priorWeight+weight); err != nil {
		return nil, err
	}
	observation := value.FileObservation{Session: session.FileSession(), Lease: lease, Directory: directory, Identity: identity}
	return value.NewFileEntry(observation, pathValue, nameValue, kind, sizeValue, modifiedValue, linkValue, ownerValue, accessedValue, changedValue)
}
