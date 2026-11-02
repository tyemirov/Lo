package execution

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

// LockError contains only a closed recoverable lock failure kind.
type LockError interface {
	error
	Kind() string
	lockError()
}
type lockError struct{ kind string }

func (lockError) lockError()     {}
func (e lockError) Kind() string { return e.kind }
func (e lockError) Error() string {
	if e.kind == catalog.LockBusy {
		return "file lock is busy"
	}
	return "file lock acquisition failed"
}
func NewLockError(kind string) (LockError, error) {
	if kind != catalog.LockBusy && kind != catalog.LockFailed {
		return nil, fmt.Errorf("invalid lock failure kind")
	}
	return lockError{kind}, nil
}
