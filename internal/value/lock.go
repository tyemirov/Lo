package value

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// LockFile is an inert exclusive nonblocking lock description.
type LockFile interface {
	Target
	Path() Text
	lockFile()
}
type lockFile struct {
	base
	path Text
}

func (lockFile) target()      {}
func (lockFile) lockFile()    {}
func (v lockFile) Path() Text { return v.path }
func (v lockFile) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Redacted bool `json:"redacted"`
	}{true})
}

// NewLockFile validates an inert target without filesystem access.
func NewLockFile(path Text) (LockFile, error) {
	if path == nil || !types.Equal(path.Type(), types.Path()) {
		return nil, fmt.Errorf("lock target requires File.Path")
	}
	return lockFile{newBase(types.LockFile(), path.Weight()), path}, nil
}

// LockLease is completion metadata without a resource handle or release authority.
type LockLease interface {
	Value
	lockLease()
}
type lockLease struct{ base }

func (lockLease) lockLease() {}
func (v lockLease) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Redacted bool `json:"redacted"`
	}{true})
}
func NewLockLease() LockLease    { return lockLease{newBase(types.LockLease(), 0)} }
func snapshotLockFile() LockFile { return lockFile{base: newBase(types.LockFile(), 0)} }
