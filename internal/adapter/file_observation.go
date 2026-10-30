package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func (f *files) observeDirectory(session execution.Session, root *os.Root, directory value.FileDirectory, lease value.FileLease, priorWeight uint64) (value.FileDirectoryObservation, error) {
	if err := session.ReserveFile(1, priorWeight); err != nil {
		return nil, err
	}
	info, err := f.io.Lstat(root, ".")
	if err != nil {
		return nil, err
	}
	identity, _, err := nativeFileIdentity(info)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || identity != directory.Identity {
		return nil, execution.Denied(catalog.FileDirectory, directory.Path)
	}
	metadata, err := nativeFileMetadata(info)
	if err != nil {
		return nil, err
	}
	path, err := value.NewText(types.Path(), directory.Path)
	if err != nil {
		return nil, err
	}
	owner, accessed, modified, changed := value.Int64(int64(metadata.owner)), value.Int64(metadata.accessed), value.Int64(metadata.modified), value.Int64(metadata.changed)
	weight := uint64(16+16+128+len(directory.Path)+len("path")+len("owner_uid")+len("accessed_unix_ns")+len("modified_unix_ns")+len("changed_unix_ns")) + path.Weight() + owner.Weight() + accessed.Weight() + modified.Weight() + changed.Weight()
	if err := session.ReserveFile(0, priorWeight+weight); err != nil {
		return nil, err
	}
	observation := value.FileObservation{Session: session.FileSession(), Lease: lease, Directory: directory, Identity: directory.Identity}
	return value.NewFileDirectoryObservation(observation, path, owner, accessed, modified, changed)
}

// Directory observes one exactly granted directory without enumerating children.
func (f *files) Directory(ctx context.Context, path value.Text) (result value.Value, resultErr error) {
	session, ok := execution.SessionFromContext(ctx)
	if !ok {
		return nil, execution.HostFailure(catalog.FileDirectory, path.Text(), errors.New("missing execution session"))
	}
	if err := ctx.Err(); err != nil {
		return nil, execution.BoundaryHostError(err)
	}
	selected, directory, err := f.exactGrant(f.lists, catalog.FileDirectory, path.Text())
	if err != nil {
		return nil, err
	}
	root, lease, fresh, err := f.directory(session, selected, directory)
	if err != nil {
		return nil, observationFailure(catalog.FileDirectory, catalog.DirectoryFailed, path.Text(), err)
	}
	defer func() {
		if resultErr != nil && fresh {
			resultErr = execution.PreserveCleanup(resultErr, f.releaseDirectory(session.FileSession(), directory, lease))
		}
	}()
	observation, err := f.observeDirectory(session, root, directory, lease, 16)
	if err != nil {
		return nil, observationFailure(catalog.FileDirectory, catalog.DirectoryFailed, path.Text(), err)
	}
	if _, current, err := f.exactGrant(f.lists, catalog.FileDirectory, path.Text()); err != nil {
		return nil, err
	} else if current != directory {
		return nil, execution.Denied(catalog.FileDirectory, path.Text())
	}
	return observation, nil
}

func observationFailure(operation, kind, path string, err error) error {
	if execution.BoundaryHostError(err) != nil {
		return err
	}
	return execution.NewFileError(kind, nil, nil, fmt.Errorf("%s %q: %w", operation, path, err))
}

// Scan publishes surviving observations and explicitly observed vanished names.
func (f *files) Scan(ctx context.Context, path value.Text) (result value.Value, resultErr error) {
	session, ok := execution.SessionFromContext(ctx)
	if !ok {
		return nil, execution.HostFailure(catalog.FileScan, path.Text(), errors.New("missing execution session"))
	}
	if err := ctx.Err(); err != nil {
		return nil, execution.BoundaryHostError(err)
	}
	selected, directory, err := f.exactGrant(f.lists, catalog.FileScan, path.Text())
	if err != nil {
		return nil, err
	}
	root, lease, fresh, err := f.directory(session, selected, directory)
	if err != nil {
		return nil, observationFailure(catalog.FileScan, catalog.ListFailed, path.Text(), err)
	}
	defer func() {
		if resultErr != nil && fresh {
			resultErr = execution.PreserveCleanup(resultErr, f.releaseDirectory(session.FileSession(), directory, lease))
		}
	}()
	directoryValue, err := f.observeDirectory(session, root, directory, lease, 128)
	if err != nil {
		return nil, observationFailure(catalog.FileScan, catalog.ListFailed, path.Text(), err)
	}
	stream, err := root.Open(".")
	if err != nil {
		return nil, observationFailure(catalog.FileScan, catalog.ListFailed, path.Text(), err)
	}
	closeStream := cancelFile(ctx, stream)
	defer func() {
		if err := closeStream(); err != nil {
			result = nil
			resultErr = execution.PreserveCleanup(resultErr, err)
		}
	}()
	entries := []value.Value{}
	vanished := []value.Value{}
	names := map[string]struct{}{}
	weight := uint64(96+len("directory")+len("entries")+len("vanished_names")) + directoryValue.Weight()
	for {
		if err := ctx.Err(); err != nil {
			return nil, execution.BoundaryHostError(err)
		}
		children, readErr := f.io.ReadDir(stream, 128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, observationFailure(catalog.FileScan, catalog.ListFailed, path.Text(), readErr)
		}
		for _, child := range children {
			if limit := session.Limits().FileEntries; limit != 0 && uint64(len(names)) >= limit {
				return nil, execution.Limit("directory entries")
			}
			if err := session.ReserveFile(1, weight); err != nil {
				return nil, err
			}
			name := child.Name()
			if !utf8.ValidString(name) {
				return nil, observationFailure(catalog.FileScan, catalog.InvalidName, directory.Path, errors.New("invalid UTF-8 filename"))
			}
			if _, duplicate := names[name]; duplicate {
				return nil, observationFailure(catalog.FileScan, catalog.ListFailed, directory.Path, errors.New("duplicate directory entry"))
			}
			names[name] = struct{}{}
			info, err := f.io.Lstat(root, name)
			if errors.Is(err, os.ErrNotExist) {
				if err := session.ReserveFile(0, weight+16+uint64(len(name))); err != nil {
					return nil, err
				}
				text, err := value.NewText(types.String(), name)
				if err != nil {
					return nil, err
				}
				weight += text.Weight()
				vanished = append(vanished, text)
				continue
			}
			if err != nil {
				return nil, observationFailure(catalog.FileScan, catalog.ListFailed, filepath.Join(directory.Path, name), err)
			}
			entry, err := observeEntry(session, directory, lease, name, info, weight)
			if err != nil {
				return nil, observationFailure(catalog.FileScan, catalog.ListFailed, filepath.Join(directory.Path, name), err)
			}
			weight += entry.Weight()
			entries = append(entries, entry)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	count := uint64(len(names))
	sortSteps := uint64(0)
	if count > 1 {
		sortSteps = count * uint64(bits.Len64(count-1))
	}
	if err := session.ReserveFile(sortSteps, weight); err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b value.Value) int {
		return strings.Compare(a.(value.FileEntry).FieldsRecord().Field("name").(value.Text).Text(), b.(value.FileEntry).FieldsRecord().Field("name").(value.Text).Text())
	})
	slices.SortFunc(vanished, func(a, b value.Value) int { return strings.Compare(a.(value.Text).Text(), b.(value.Text).Text()) })
	if _, current, err := f.exactGrant(f.lists, catalog.FileScan, path.Text()); err != nil {
		return nil, err
	} else if current != directory {
		return nil, execution.Denied(catalog.FileScan, path.Text())
	}
	entryValues, err := value.NewList(must(types.NewList(types.FileEntry())), entries)
	if err != nil {
		return nil, err
	}
	vanishedValues, err := value.NewList(must(types.NewList(types.String())), vanished)
	if err != nil {
		return nil, err
	}
	return value.NewFileScan(directoryValue, entryValues, vanishedValues)
}
