package integration_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

// metadataLocations projects the current wire tables for read-only assertions.
func metadataLocations(t *testing.T, data []byte) []nativeplan.Location {
	t.Helper()
	var document struct {
		ABI   string
		Files []string
		Spans []struct {
			FileIndex  uint64 `json:"file_index"`
			Start, End source.Position
		}
		Locations []struct {
			Span          uint64
			Action, Owner string
			Type, Flow    uint64
			Payload       json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.ABI != "lo-native-2" {
		t.Fatalf("unexpected metadata ABI %q", document.ABI)
	}
	result := make([]nativeplan.Location, len(document.Locations))
	for index, row := range document.Locations {
		if row.Span >= uint64(len(document.Spans)) {
			t.Fatal("span index out of range")
		}
		span := document.Spans[row.Span]
		if span.FileIndex >= uint64(len(document.Files)) {
			t.Fatal("file index out of range")
		}
		location := &result[index]
		if err := json.Unmarshal(row.Payload, location); err != nil {
			t.Fatal(err)
		}
		location.ID = uint64(index)
		location.Action = row.Action
		location.Owner = row.Owner
		location.Type = row.Type
		location.Flow = row.Flow
		location.File = document.Files[span.FileIndex]
		location.Start = span.Start
		location.End = span.End
	}
	return result
}

func decodeLLVMBytes(encoded []byte) ([]byte, error) {
	data := make([]byte, 0, len(encoded))
	for offset := 0; offset < len(encoded); offset++ {
		if encoded[offset] != '\\' {
			data = append(data, encoded[offset])
			continue
		}
		if offset+2 >= len(encoded) {
			return nil, fmt.Errorf("incomplete LLVM byte escape")
		}
		decoded, err := hex.DecodeString(string(encoded[offset+1 : offset+3]))
		if err != nil {
			return nil, err
		}
		data = append(data, decoded[0])
		offset += 2
	}
	return data, nil
}
