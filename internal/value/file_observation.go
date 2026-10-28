package value

import (
	"fmt"
	"path/filepath"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type publicFileValue struct {
	base
	fields Record
}

func (v publicFileValue) FieldsRecord() Record { return v.fields }
func (v publicFileValue) MarshalJSON() ([]byte, error) {
	fields := make(map[string]Value, len(v.fields.Fields()))
	for _, field := range v.fields.Fields() {
		fields[field.Name] = field.Value
	}
	return encode(v, fields)
}

// FileDirectoryObservation contains immutable root metadata and private provenance.
type FileDirectoryObservation interface {
	Value
	FieldsRecord() Record
	Observation() FileObservation
	fileDirectoryObservation()
}

type fileDirectoryObservation struct {
	publicFileValue
	observation FileObservation
}

func (fileDirectoryObservation) fileDirectoryObservation()      {}
func (v fileDirectoryObservation) Observation() FileObservation { return v.observation }

// NewFileDirectoryObservation constructs validated metadata for one retained root.
func NewFileDirectoryObservation(observation FileObservation, path Text, ownerUID, accessedUnixNS, modifiedUnixNS, changedUnixNS Integer) (FileDirectoryObservation, error) {
	if observation.Session.token == nil || path == nil || path.Type().Kind() != types.PathKind || !filepath.IsAbs(path.Text()) || path.Text() != observation.Directory.Path || ownerUID == nil || ownerUID.Big().Sign() < 0 || ownerUID.Big().BitLen() > 32 || accessedUnixNS == nil || modifiedUnixNS == nil || changedUnixNS == nil {
		return nil, fmt.Errorf("invalid directory observation")
	}
	fields, err := NewRecord([]Field{{"path", path}, {"owner_uid", ownerUID}, {"accessed_unix_ns", accessedUnixNS}, {"modified_unix_ns", modifiedUnixNS}, {"changed_unix_ns", changedUnixNS}})
	if err != nil {
		return nil, err
	}
	return fileDirectoryObservation{publicFileValue{newBase(types.FileDirectory(), addWeight(fields.Weight(), 128+uint64(len(path.Text())))), fields}, observation}, nil
}

// FileScan contains one directory observation and explicit child discovery results.
type FileScan interface {
	Value
	FieldsRecord() Record
	fileScan()
}

type fileScan struct{ publicFileValue }

func (fileScan) fileScan() {}

// NewFileScan constructs an immutable scan report from validated observations.
func NewFileScan(directory FileDirectoryObservation, entries, vanished List) (FileScan, error) {
	if directory == nil || entries == nil || vanished == nil || !types.Equal(entries.Type(), must(types.NewList(types.FileEntry()))) || !types.Equal(vanished.Type(), must(types.NewList(types.String()))) {
		return nil, fmt.Errorf("scan requires validated directory and child observations")
	}
	fields, err := NewRecord([]Field{{"directory", directory}, {"entries", entries}, {"vanished_names", vanished}})
	if err != nil {
		return nil, err
	}
	return fileScan{publicFileValue{newBase(types.FileScan(), fields.Weight()), fields}}, nil
}
