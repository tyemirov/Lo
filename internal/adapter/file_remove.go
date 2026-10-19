package adapter

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	removalPreflight = "preflight"
	removalStage     = "remove"
	removalFinalize  = "finalize"
)

type removalReceipt struct {
	entries          value.List
	total, completed int
	bytes            big.Int
}

func (r *removalReceipt) progress(index int, stage string) value.Record {
	stageValue := text(stage)
	bytes := must(value.NewInt(&r.bytes))
	progress := must(value.NewRecord([]value.Field{
		{Name: "total_count", Value: value.Int64(int64(r.total))},
		{Name: "completed_count", Value: value.Int64(int64(r.completed))},
		{Name: "completed_bytes", Value: bytes},
		{Name: "failed_index", Value: value.Int64(int64(index))},
		{Name: "stage", Value: stageValue},
	}))
	return progress
}

func (r *removalReceipt) failure(kind string, index int, stage string, cause error) error {
	progress := r.progress(index, stage)
	if execution.BoundaryHostError(cause) != nil {
		return execution.WithRemovalProgress(cause, r.entries, progress)
	}
	return execution.NewFileError(kind, r.entries, progress, cause)
}

func (f *files) remove(ctx context.Context, target value.RemovalEffect) (value.Value, error) {
	session, ok := execution.SessionFromContext(ctx)
	if !ok {
		return nil, execution.HostFailure(catalog.FileRemoveCapability, "batch", errors.New("missing execution session"))
	}
	entries := target.Entries()
	receipt := &removalReceipt{entries: entries, total: entries.Len()}
	if target.FileSession() != session.FileSession() {
		return nil, receipt.failure(catalog.InvalidSelection, -1, removalPreflight, errors.New("removal effect belongs to another execution session"))
	}
	if entries.Len() == 0 {
		return receipt.success()
	}
	items := entries.Items()
	first := items[0].(value.FileEntry)
	directory := first.Observation().Directory
	_, granted, err := f.exactGrant(f.removes, catalog.FileRemoveCapability, directory.Path)
	if err != nil {
		return nil, receipt.failure(catalog.RemoveFailed, -1, removalPreflight, err)
	}
	if granted != directory {
		return nil, receipt.failure(catalog.EntryChanged, -1, removalPreflight, errors.New("observed directory identity changed"))
	}
	f.mu.Lock()
	registry := f.sessions[session.FileSession()]
	var root *os.Root
	if registry != nil {
		root = registry.directories[directory]
	}
	f.mu.Unlock()
	if root == nil {
		return nil, receipt.failure(catalog.InvalidSelection, -1, removalPreflight, errors.New("observed directory is not retained by this session"))
	}
	// Reserve the ordered public snapshot and all fixed diagnostic fields before mutation.
	diagnosticWeight := entries.Weight() + 2048
	if err := session.ReserveFile(uint64(3*len(items)), diagnosticWeight); err != nil {
		return nil, receipt.failure(catalog.RemoveFailed, -1, removalPreflight, err)
	}
	session.AddWeight(diagnosticWeight)
	receipt.entries = value.Snapshot(entries).(value.List)
	f.recordRemoval(session, receipt)
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, receipt.failure(catalog.RemoveFailed, index, removalPreflight, err)
		}
		if err := f.revalidate(root, item.(value.FileEntry)); err != nil {
			return nil, receipt.failure(revalidationKind(err), index, removalPreflight, err)
		}
	}
	for index, item := range items {
		entry := item.(value.FileEntry)
		if err := ctx.Err(); err != nil {
			return nil, receipt.failure(catalog.RemoveFailed, index, removalStage, err)
		}
		_, current, err := f.exactGrant(f.removes, catalog.FileRemoveCapability, directory.Path)
		if err != nil {
			return nil, receipt.failure(catalog.RemoveFailed, index, removalStage, err)
		}
		if current != directory {
			return nil, receipt.failure(catalog.EntryChanged, index, removalStage, errors.New("observed directory identity changed"))
		}
		if err := f.revalidate(root, entry); err != nil {
			return nil, receipt.failure(revalidationKind(err), index, removalStage, err)
		}
		name := entry.FieldsRecord().Field("name").(value.Text).Text()
		if f.protection != nil {
			if err := f.protection.protectRemoval(root, name); err != nil {
				return nil, receipt.failure(catalog.RemoveFailed, index, removalStage, err)
			}
		}
		if err := f.io.Remove(root, name); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, receipt.failure(catalog.EntryChanged, index, removalStage, fmt.Errorf("remove %q: %w", name, err))
			}
			return nil, receipt.failure(catalog.RemoveFailed, index, removalStage, fmt.Errorf("remove %q: %w", name, err))
		}
		receipt.completed++
		receipt.bytes.Add(&receipt.bytes, entry.FieldsRecord().Field("size_bytes").(value.Integer).Big())
		f.recordRemoval(session, receipt)
		if err := ctx.Err(); err != nil {
			return nil, receipt.failure(catalog.RemoveFailed, -1, removalStage, err)
		}
	}
	return receipt.success()
}

func (f *files) recordRemoval(session execution.Session, receipt *removalReceipt) {
	progress := receipt.progress(-1, removalFinalize)
	session.RecordRemoval(receipt.entries, progress)
	f.mu.Lock()
	f.lastEntries, f.lastProgress = receipt.entries, progress
	f.mu.Unlock()
}

func (f *files) revalidate(root *os.Root, entry value.FileEntry) error {
	fields := entry.FieldsRecord()
	name := fields.Field("name").(value.Text).Text()
	info, err := f.io.Lstat(root, name)
	if err != nil {
		kind := catalog.RemoveFailed
		if errors.Is(err, os.ErrNotExist) {
			kind = catalog.EntryChanged
		}
		return execution.NewFileError(kind, nil, nil, fmt.Errorf("revalidate %q: %w", name, err))
	}
	identity, _, err := nativeFileIdentity(info)
	if err != nil {
		return execution.NewFileError(catalog.RemoveFailed, nil, nil, err)
	}
	observation := entry.Observation()
	modified := info.ModTime().UnixNano()
	if info.Mode().Type() != 0 || identity != observation.Identity || !time.Unix(0, modified).Equal(info.ModTime()) || fields.Field("size_bytes").(value.Integer).Big().Cmp(big.NewInt(info.Size())) != 0 || fields.Field("modified_unix_ns").(value.Integer).Big().Cmp(big.NewInt(modified)) != 0 {
		return fmt.Errorf("selected file %q changed since discovery", name)
	}
	return nil
}

func revalidationKind(err error) string {
	var failure execution.FileError
	if errors.As(err, &failure) {
		return failure.Kind()
	}
	return catalog.EntryChanged
}

func (r *removalReceipt) success() (value.Value, error) {
	fields := must(value.NewRecord([]value.Field{{Name: "total_count", Value: value.Int64(int64(r.total))}, {Name: "completed_count", Value: value.Int64(int64(r.completed))}, {Name: "observed_bytes", Value: must(value.NewInt(&r.bytes))}}))
	return value.NewReceipt(types.FileRemoveReceipt(), fields)
}
