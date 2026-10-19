package adapter

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type Files interface {
	execution.Boundary
	Close() error
	files()
}
type grant struct {
	path string
	root *os.Root
}
type files struct {
	base         string
	reads        []grant
	writes       []grant
	lists        []grant
	removes      []grant
	openFiles    []grant
	io           FileIO
	mu           sync.Mutex
	sessions     map[value.FileSession]*fileRegistry
	lastEntries  value.List
	lastProgress value.Record
}

func (*files) files() {}

// FileRoots separates content access from exact-directory discovery and removal.
type FileRoots struct{ Read, Write, List, Remove, OpenFiles []string }

func OpenFiles(roots FileRoots, filesystem FileIO) (Files, error) {
	if filesystem == nil {
		return nil, fmt.Errorf("file adapter requires filesystem operations")
	}
	base, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve execution directory: %w", err)
	}
	f := &files{base: base, io: filesystem, sessions: map[value.FileSession]*fileRegistry{}}
	f.reads, err = openRoots(roots.Read)
	if err != nil {
		return nil, err
	}
	f.writes, err = openRoots(roots.Write)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	f.lists, err = openExactRoots(roots.List)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	f.removes, err = openExactRoots(roots.Remove)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	f.openFiles, err = openExactRoots(roots.OpenFiles)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}
func openRoots(paths []string) ([]grant, error) {
	roots := []grant{}
	fail := func(err error) ([]grant, error) { return nil, errors.Join(err, closeRoots(roots, (*os.Root).Close)) }
	seen := map[string]bool{}
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fail(fmt.Errorf("resolve adapter root %q: %w", path, err))
		}
		if seen[absolute] {
			return fail(fmt.Errorf("duplicate adapter root %q", absolute))
		}
		seen[absolute] = true
		root, err := os.OpenRoot(absolute)
		if err != nil {
			return fail(fmt.Errorf("open adapter root %q: %w", absolute, err))
		}
		roots = append(roots, grant{absolute, root})
	}
	slices.SortFunc(roots, func(a, b grant) int { return len(b.path) - len(a.path) })
	return roots, nil
}
func (f *files) Close() error {
	var failures []error
	for _, roots := range [][]grant{f.reads, f.writes, f.lists, f.removes, f.openFiles} {
		failures = append(failures, closeRoots(roots, f.io.CloseDirectory))
	}
	err := errors.Join(failures...)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil && f.lastProgress != nil {
		return execution.WithRemovalProgress(err, f.lastEntries, f.lastProgress)
	}
	return err
}
func closeRoots(roots []grant, close func(*os.Root) error) error {
	var result error
	for _, g := range roots {
		if g.root == nil {
			continue
		}
		if err := close(g.root); err != nil {
			result = errors.Join(result, fmt.Errorf("close adapter root %q: %w", g.path, err))
		}
	}
	return result
}
func (f *files) Read(ctx context.Context, path value.Text) (value.Bytes, error) {
	if err := ctx.Err(); err != nil {
		return nil, execution.HostFailure(catalog.FileRead, path.Text(), err)
	}
	selected, relative, absolute, err := f.resolve(f.reads, catalog.FileRead, path.Text())
	if err != nil {
		return nil, err
	}
	file, err := selected.OpenFile(relative, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", catalog.FileRead, absolute, err)
	}
	closeFile := cancelFile(ctx, file)
	data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx, file}, execution.MaxBytes+1))
	closeErr := closeFile()
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("%s %q: %w", catalog.FileRead, absolute, errors.Join(readErr, closeErr))
	}
	if len(data) > execution.MaxBytes {
		return nil, execution.Limit("file read bytes")
	}
	if err := ctx.Err(); err != nil {
		return nil, execution.HostFailure(catalog.FileRead, absolute, err)
	}
	return value.NewBytes(data), nil
}
func (f *files) resolve(roots []grant, operation, path string) (*os.Root, string, string, error) {
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(f.base, absolute)
	}
	absolute = filepath.Clean(absolute)
	for _, g := range roots {
		rel, err := filepath.Rel(g.path, absolute)
		if err == nil && filepath.IsLocal(rel) {
			return g.root, rel, absolute, nil
		}
	}
	return nil, "", absolute, execution.Denied(operation, absolute)
}
func (f *files) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	switch target := target.(type) {
	case value.Server:
		return nil, execution.ServerRequiresServe()
	case value.RemovalEffect:
		return f.remove(ctx, target)
	case value.ContentEffect:
		operation := target.Operation().Capability()
		root, relative, absolute, err := f.resolve(f.writes, operation, target.Path().Text())
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, execution.WithWriteProgress(execution.HostFailure(operation, absolute, err), 0, "open")
		}
		var file *os.File
		var data []byte
		if target.Type().Kind() == types.FileWriteKind {
			file, err = root.Create(relative)
			data = target.Content().(value.Bytes).Bytes()
		} else {
			file, err = openAppend(root, relative)
			data = target.Content().(value.Bytes).Bytes()
		}
		if err != nil {
			return nil, execution.WithWriteProgress(fmt.Errorf("%s %q: %w", operation, absolute, err), 0, "open")
		}
		closeFile := cancelFileWithClose(ctx, file, f.io.CloseFile)
		confirmed, writeErr := f.io.Write(ctx, file, data)
		closeErr := closeFile()
		if writeErr != nil || closeErr != nil {
			stage := "finalize"
			if writeErr != nil {
				stage = "write"
			}
			return nil, execution.WithWriteProgress(fmt.Errorf("%s %q: %w", operation, absolute, execution.WithOperationCleanup(writeErr, closeErr)), confirmed, stage)
		}
		if err := ctx.Err(); err != nil {
			return nil, execution.WithWriteProgress(execution.HostFailure(operation, absolute, err), confirmed, "finalize")
		}
		resultType, _ := catalog.ActivationResult(target.Type())
		fields := must(value.NewRecord([]value.Field{{Name: "path", Value: target.Path()}, {Name: "bytes_written", Value: value.Int64(int64(confirmed))}}))
		return value.NewReceipt(resultType, fields)
	default:
		panic("unknown checked target")
	}
}

func cancelFile(ctx context.Context, file *os.File) func() error {
	return cancelFileWithClose(ctx, file, (*os.File).Close)
}
func cancelFileWithClose(ctx context.Context, file *os.File, close func(*os.File) error) func() error {
	closed := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() { closed <- close(file) })
	return func() error {
		if stop() {
			return close(file)
		}
		return errors.Join(ctx.Err(), <-closed)
	}
}

func openAppend(root *os.Root, path string) (*os.File, error) {
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_APPEND|unix.O_NONBLOCK, 0)
	if !errors.Is(err, os.ErrNotExist) {
		return file, err
	}
	// Link a new empty file without replacing a concurrently created target.
	temporary := filepath.Join(filepath.Dir(path), ".lo-create-"+rand.Text())
	created, createErr := root.Create(temporary)
	if createErr != nil {
		return nil, createErr
	}
	closeErr := created.Close()
	if closeErr != nil {
		return nil, errors.Join(closeErr, root.Remove(temporary))
	}
	linkErr := root.Link(temporary, path)
	removeErr := root.Remove(temporary)
	if errors.Is(linkErr, os.ErrExist) {
		linkErr = nil
	}
	if err := errors.Join(linkErr, removeErr); err != nil {
		return nil, err
	}
	return root.OpenFile(path, os.O_WRONLY|os.O_APPEND|unix.O_NONBLOCK, 0)
}
