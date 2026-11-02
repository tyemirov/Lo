package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// ProgramEntry binds an explicit host entry to its retained source object.
type ProgramEntry interface {
	Path() value.Text
	Info() os.FileInfo
	ReadSource() (string, error)
	Close() error
	programEntry()
}
type programEntry struct {
	path     value.Text
	file     *os.File
	info     os.FileInfo
	once     sync.Once
	mu       sync.Mutex
	claimed  bool
	closed   bool
	closeErr error
	readOnce sync.Once
	source   string
	readErr  error
}

func (*programEntry) programEntry()       {}
func (e *programEntry) Path() value.Text  { return e.path }
func (e *programEntry) Info() os.FileInfo { return e.info }
func (e *programEntry) ReadSource() (string, error) {
	e.readOnce.Do(func() {
		data, err := io.ReadAll(io.LimitReader(e.file, source.MaxBytes+1))
		if err != nil {
			e.readErr = fmt.Errorf("read program entry: %w", err)
			return
		}
		e.source = string(data)
	})
	return e.source, e.readErr
}
func (e *programEntry) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	e.once.Do(func() { e.closeErr = e.file.Close() })
	return e.closeErr
}

// OpenProgramEntry opens an existing entry independently of argv or source metadata.
// The caller transfers its lifetime to the host session before evaluation.
func OpenProgramEntry(path string) (ProgramEntry, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (ProgramEntry, error) { return nil, errors.Join(err, file.Close()) }
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return fail(err)
	}
	current, err := os.Stat(physical)
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(info, current) {
		return fail(fmt.Errorf("program entry identity changed"))
	}
	text, err := value.NewText(types.Path(), physical)
	if err != nil {
		return fail(err)
	}
	return &programEntry{path: text, file: file, info: info}, nil
}

type programEntryKey struct{}

// WithProgramEntry injects an explicitly bound entry before an embedded run.
// A processhost session adopts it once. Direct Program.Run callers own its close.
func WithProgramEntry(ctx context.Context, entry ProgramEntry) (context.Context, error) {
	if ctx == nil || entry == nil {
		return nil, fmt.Errorf("program entry injection requires context and entry")
	}
	bound := entry.(*programEntry)
	bound.mu.Lock()
	closed := bound.closed
	bound.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("program entry is closed")
	}
	return context.WithValue(ctx, programEntryKey{}, entry), nil
}

// ClaimProgramEntry transfers one captured source to exactly one host session.
// Request execution inherits this observation without another adoption.
func ClaimProgramEntry(entry ProgramEntry) error {
	bound := entry.(*programEntry)
	bound.mu.Lock()
	defer bound.mu.Unlock()
	if bound.closed || bound.claimed {
		return fmt.Errorf("program entry already belongs to a host lifetime")
	}
	bound.claimed = true
	return nil
}

// CloseUnclaimedProgramEntry releases a capture when host admission did not take
// ownership. It never closes a source owned by another invocation.
func CloseUnclaimedProgramEntry(entry ProgramEntry) error {
	if entry == nil {
		return nil
	}
	bound := entry.(*programEntry)
	bound.mu.Lock()
	defer bound.mu.Unlock()
	if bound.claimed {
		return nil
	}
	bound.closed = true
	bound.once.Do(func() { bound.closeErr = bound.file.Close() })
	return bound.closeErr
}

// InitialProgramEntry returns the entry captured by the host, or explicit absence.
func InitialProgramEntry(ctx context.Context) ProgramEntry {
	entry, _ := ctx.Value(programEntryKey{}).(ProgramEntry)
	return entry
}

// InheritProgramEntry preserves the parent entry through server callbacks.
func InheritProgramEntry(ctx, parent context.Context) context.Context {
	if entry := InitialProgramEntry(parent); entry != nil {
		return context.WithValue(ctx, programEntryKey{}, entry)
	}
	return ctx
}
