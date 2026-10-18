package execution

import (
	"errors"
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// FileError carries a recoverable File operation failure across the host boundary.
type FileError interface {
	error
	Kind() string
	Entries() value.List
	Progress() value.Record
	Unwrap() error
}

type fileError struct {
	kind     string
	entries  value.List
	progress value.Record
	cause    error
}

func (e fileError) Error() string          { return fmt.Sprintf("%s: %v", e.kind, e.cause) }
func (e fileError) Kind() string           { return e.kind }
func (e fileError) Entries() value.List    { return e.entries }
func (e fileError) Progress() value.Record { return e.progress }
func (e fileError) Unwrap() error          { return e.cause }

// NewFileError preserves the selected domain kind and the ordered removal receipt.
func NewFileError(kind string, entries value.List, progress value.Record, cause error) FileError {
	return fileError{kind: kind, entries: entries, progress: progress, cause: cause}
}

type removalError struct {
	error
	entries  value.List
	progress value.Record
}

func (e removalError) Unwrap() error          { return e.error }
func (e removalError) Entries() value.List    { return e.entries }
func (e removalError) Progress() value.Record { return e.progress }

// WithRemovalProgress preserves progress without changing the original error class.
func WithRemovalProgress(err error, entries value.List, progress value.Record) error {
	if err == nil {
		return nil
	}
	return removalError{error: err, entries: entries, progress: progress}
}

// RemovalDetails obtains the first complete receipt in an error chain.
func RemovalDetails(err error) (value.List, value.Record, bool) {
	var receipt interface {
		Entries() value.List
		Progress() value.Record
	}
	if errors.As(err, &receipt) && receipt.Entries() != nil && receipt.Progress() != nil {
		return receipt.Entries(), receipt.Progress(), true
	}
	return nil, nil, false
}

// PreserveCleanup reports cleanup failures while keeping the original failure first.
func PreserveCleanup(cause, cleanup error) error {
	if cleanup == nil {
		return cause
	}
	closed := HostFailure("close runtime resources", "program", cleanup)
	if cause == nil {
		return closed
	}
	return errors.Join(cause, closed)
}
