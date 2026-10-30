package value

import (
	"fmt"
	"path/filepath"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// FileSession identifies one execution's observations.
type FileSession struct{ token *fileSessionToken }
type fileSessionToken struct{ marker byte }

func NewFileSession() FileSession { return FileSession{&fileSessionToken{}} }

// FileLease identifies one retained observation handle.
type FileLease struct{ token *fileLeaseToken }
type fileLeaseToken struct{ marker byte }

// NewFileLease creates a distinct observation lease.
func NewFileLease() FileLease { return FileLease{&fileLeaseToken{}} }

// FileIdentity contains the filesystem identity of an observed object.
type FileIdentity struct{ Device, Inode uint64 }

// FileDirectory identifies the retained parent directory.
type FileDirectory struct {
	Path     string
	Identity FileIdentity
}

// FileObservation carries authority-free provenance from a filesystem observation.
type FileObservation struct {
	Session   FileSession
	Lease     FileLease
	Directory FileDirectory
	Identity  FileIdentity
}

// FileKind is the closed classification of a directory child.
type FileKind string

const (
	RegularFile   FileKind = "regular"
	DirectoryFile FileKind = "directory"
	SymlinkFile   FileKind = "symlink"
	OtherFile     FileKind = "other"
)

// FileEntry is an immutable observation with opaque execution provenance.
type FileEntry interface {
	Value
	FieldsRecord() Record
	Observation() FileObservation
	Kind() FileKind
	fileEntry()
}
type fileEntry struct {
	publicFileEntry
	observation FileObservation
	kind        FileKind
}
type publicFileEntry struct {
	base
	fields Record
}

func (v publicFileEntry) MarshalJSON() ([]byte, error) {
	fields := map[string]Value{}
	for _, f := range v.fields.Fields() {
		fields[f.Name] = f.Value
	}
	return encode(v, fields)
}
func (fileEntry) fileEntry()                     {}
func (v fileEntry) FieldsRecord() Record         { return v.fields }
func (v fileEntry) Observation() FileObservation { return v.observation }
func (v fileEntry) Kind() FileKind               { return v.kind }
func NewFileEntry(observation FileObservation, path Text, name Text, kind FileKind, sizeBytes, modifiedUnixNS, linkCount, ownerUID, accessedUnixNS, changedUnixNS Integer) (FileEntry, error) {
	if observation.Session.token == nil || observation.Lease.token == nil || !filepath.IsAbs(observation.Directory.Path) || path == nil || path.Type().Kind() != types.PathKind || name == nil || name.Type().Kind() != types.StringKind || name.Text() == "" || filepath.Base(name.Text()) != name.Text() || name.Text() == "." || name.Text() == ".." || path.Text() != filepath.Join(observation.Directory.Path, name.Text()) || sizeBytes == nil || sizeBytes.Big().Sign() < 0 || modifiedUnixNS == nil || linkCount == nil || linkCount.Big().Sign() < 0 || ownerUID == nil || ownerUID.Big().Sign() < 0 || ownerUID.Big().BitLen() > 32 || accessedUnixNS == nil || changedUnixNS == nil {
		return nil, fmt.Errorf("invalid file entry observation")
	}
	switch kind {
	case RegularFile, DirectoryFile, SymlinkFile, OtherFile:
	default:
		return nil, fmt.Errorf("invalid file entry kind")
	}
	fields, err := NewRecord([]Field{{"path", path}, {"name", name}, {"kind", must(NewText(types.String(), string(kind)))}, {"size_bytes", sizeBytes}, {"modified_unix_ns", modifiedUnixNS}, {"link_count", linkCount}, {"owner_uid", ownerUID}, {"accessed_unix_ns", accessedUnixNS}, {"changed_unix_ns", changedUnixNS}})
	if err != nil {
		return nil, err
	}
	return fileEntry{publicFileEntry{newBase(types.FileEntry(), addWeight(fields.Weight(), 128+uint64(len(observation.Directory.Path)))), fields}, observation, kind}, nil
}

// RemovalEffect retains an ordered immutable selection.
type RemovalEffect interface {
	Effect
	Entries() List
	FileSession() FileSession
	Guard() (RemovalGuard, bool)
	removalEffect()
}
type removalEffect struct {
	base
	entries List
	session FileSession
	guard   RemovalGuard
}

func (removalEffect) target()                       {}
func (removalEffect) effect()                       {}
func (removalEffect) removalEffect()                {}
func (v removalEffect) Entries() List               { return v.entries }
func (v removalEffect) FileSession() FileSession    { return v.session }
func (v removalEffect) Guard() (RemovalGuard, bool) { return v.guard, v.guard != nil }
func (removalEffect) Operation() catalog.Operation {
	op, _ := catalog.Lookup(catalog.FileRemove)
	return op
}
func (v removalEffect) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Operation string `json:"operation"`
		Entries   List   `json:"entries"`
		Guard     any    `json:"guard,omitempty"`
	}{catalog.FileRemove, v.entries, removalGuardJSON(v.guard)})
}
func NewRemovalEffect(session FileSession, entries List) (RemovalEffect, error) {
	if session.token == nil || entries == nil || !types.Equal(entries.Type(), must(types.NewList(types.FileEntry()))) {
		return nil, fmt.Errorf("removal requires current file entries")
	}
	names := map[string]bool{}
	var directory FileDirectory
	var lease FileLease
	for index, item := range entries.Items() {
		entry, ok := item.(FileEntry)
		if !ok {
			return nil, fmt.Errorf("removal requires observed file entries")
		}
		observation := entry.Observation()
		name := entry.FieldsRecord().Field("name").(Text).Text()
		if observation.Session != session || entry.Kind() != RegularFile || names[name] || index > 0 && (observation.Directory != directory || observation.Lease != lease) {
			return nil, fmt.Errorf("invalid removal selection")
		}
		directory = observation.Directory
		lease = observation.Lease
		names[name] = true
	}
	return removalEffect{newBase(types.FileRemove(), addWeight(uint64(len(catalog.FileRemove)), entries.Weight())), entries, session, nil}, nil
}

// RemovalGuard is a sealed immutable predicate description.
type RemovalGuard interface {
	Flow() identity.FlowID
	Context() []Value
	removalGuard()
}
type removalGuard struct {
	flow    identity.FlowID
	context []Value
}

func (removalGuard) removalGuard()           {}
func (g removalGuard) Flow() identity.FlowID { return g.flow }
func (g removalGuard) Context() []Value      { return append([]Value(nil), g.context...) }
func removalGuardJSON(guard RemovalGuard) any {
	if guard == nil {
		return nil
	}
	contexts := guard.Context()
	for index, item := range contexts {
		contexts[index] = Snapshot(item)
	}
	return struct {
		Predicate string  `json:"predicate"`
		Context   []Value `json:"context"`
	}{string(guard.Flow()), contexts}
}

// NewCheckedRemovalEffect captures a resolved pure predicate and immutable contexts.
func NewCheckedRemovalEffect(session FileSession, entries List, flow identity.FlowID, contexts []Value) (RemovalEffect, error) {
	if flow == "" {
		return nil, fmt.Errorf("checked removal requires a predicate")
	}
	effect, err := NewRemovalEffect(session, entries)
	if err != nil {
		return nil, err
	}
	guarded := effect.(removalEffect)
	captured := append([]Value(nil), contexts...)
	payload := guarded.Weight() - 16 + uint64(len(flow)) + 32
	for _, item := range captured {
		if item == nil {
			return nil, fmt.Errorf("checked removal requires validated contexts")
		}
		payload = addWeight(payload, item.Weight())
	}
	guarded.base = newBase(types.FileRemove(), payload)
	guarded.guard = removalGuard{flow, captured}
	return guarded, nil
}
