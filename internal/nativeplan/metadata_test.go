package nativeplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestMetadataStreamExactByteBoundary(t *testing.T) {
	for _, text := range []string{strings.Repeat("a", 12000), strings.Repeat("<&\u2028\n\"\\🙂", 1800)} {
		for _, place := range []string{"source", "type", "flow", "derivative", "file", "owner", "literal"} {
			t.Run(place, func(t *testing.T) {
				document := metadataDocument{ABI: ABIIdentity, Types: []typeMetadata{{}}, Files: []string{"file.lo"}, Spans: []metadataSpan{{}}, Locations: []metadataRow{{Action: "literal"}}, Flows: []flowMetadata{{}}, Derivatives: []derivativeMetadata{{Nodes: []derivativeNode{{}}}}}
				switch place {
				case "source":
					document.Source.Path = text
				case "type":
					document.Types[0].Name = text
				case "flow":
					document.Flows[0].Name = text
				case "derivative":
					document.Derivatives[0].Nodes[0].Operation = text
				case "file":
					document.Files[0] = text
				case "owner":
					document.Locations[0].Owner = text
				case "literal":
					document.Locations[0].Payload.Text = &text
				}
				expected, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				for _, limit := range []int{len(expected) - 1, len(expected), len(expected) + 1} {
					out := metadataStream{ctx: context.Background(), limit: limit}
					err := out.value(reflect.ValueOf(document))
					if limit < len(expected) {
						if !errors.Is(err, ErrLimit) {
							t.Fatalf("byte boundary accepted: %v", err)
						}
					} else if err != nil || !bytes.Equal(expected, out.bytes) {
						t.Fatalf("canonical JSON differs at exact boundary: %v", err)
					}
					if len(out.bytes) > limit || cap(out.bytes) > limit {
						t.Fatalf("buffer exceeded byte cap: len=%d cap=%d limit=%d", len(out.bytes), cap(out.bytes), limit)
					}
				}
			})
		}
	}
}
func TestMetadataStreamManyRowsAndCancellation(t *testing.T) {
	document := metadataDocument{ABI: ABIIdentity, Locations: make([]metadataRow, 4096)}
	for index := range document.Locations {
		document.Locations[index] = metadataRow{Span: 0, Action: "reference", Owner: "same"}
	}
	expected, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	out := metadataStream{ctx: context.Background(), limit: len(expected)}
	if err := out.value(reflect.ValueOf(document)); err != nil || !bytes.Equal(out.bytes, expected) {
		t.Fatalf("many row exact boundary: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out = metadataStream{ctx: ctx, limit: len(expected)}
	if err := out.value(reflect.ValueOf(document)); !errors.Is(err, context.Canceled) || len(out.bytes) != 0 {
		t.Fatalf("canceled serialization: %v bytes=%d", err, len(out.bytes))
	}
}
func TestMetadataPublicLowerInternsLongSourceSpans(t *testing.T) {
	for _, path := range []string{strings.Repeat("a", 1<<20), strings.Repeat("<&", 1<<18)} {
		file, err := source.New(path, `return List.new(1,2,3)`)
		if err != nil {
			t.Fatal(err)
		}
		program, diagnostic := compiler.Check(file)
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		plan, err := Lower(context.Background(), program)
		if err != nil {
			t.Fatalf("valid long source rejected: %v", err)
		}
		snapshot := plan.Snapshot()
		var document metadataDocument
		if err := json.Unmarshal(snapshot.Metadata, &document); err != nil {
			t.Fatal(err)
		}
		if len(document.Files) != 1 || document.Files[0] != path || document.Source.Path != path || len(document.Spans) >= len(snapshot.Locations) || len(document.Locations) != len(snapshot.Locations) {
			t.Fatal("source/span interning lost provenance")
		}
		for index, location := range snapshot.Locations {
			row := document.Locations[index]
			span := document.Spans[row.Span]
			if location.ID != uint64(index) || span.Start != location.Start || span.End != location.End {
				t.Fatal("span interning changed original rows")
			}
		}
	}
}
func TestMetadataArtifactValueAndWhitespace(t *testing.T) {
	base := []byte(`{"abi":"lo-native-2"}`)
	artifact := json.RawMessage(strings.Repeat(" ", 1<<20) + `{"extra":[1e2,null,"a b\\\"c"],"extra":[18446744073709551615]}`)
	encoded, err := WithArtifactMetadata(context.Background(), base, artifact)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, artifact); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got["artifact"], compact.Bytes()) {
		t.Fatal("artifact value forms or duplicate fields changed")
	}
	if _, err := WithArtifactMetadata(context.Background(), base, json.RawMessage(`{"bad":`)); err == nil {
		t.Fatal("invalid artifact accepted")
	}
}

func TestMetadataArtifactCanceledWhitespace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	artifact := json.RawMessage(strings.Repeat(" ", 1<<20) + `{"value":1}`)
	if got, err := WithArtifactMetadata(ctx, []byte(`{"abi":"lo-native-2"}`), artifact); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("canceled artifact: bytes=%d error=%v", len(got), err)
	}
}

func TestMetadataArtifactExactByteCap(t *testing.T) {
	base := []byte(`{"abi":"lo-native-2"}`)
	overhead := len(base) + len(`,"artifact":`) + 2
	artifact := make([]byte, MaxModuleBytes-overhead+2)
	artifact[0] = '"'
	for index := 1; index < len(artifact)-1; index++ {
		artifact[index] = 'a'
	}
	artifact[len(artifact)-1] = '"'
	encoded, err := WithArtifactMetadata(context.Background(), base, artifact)
	if err != nil || len(encoded) != MaxModuleBytes || cap(encoded) > MaxModuleBytes {
		t.Fatalf("exact artifact byte cap: len=%d cap=%d err=%v", len(encoded), cap(encoded), err)
	}
	encoded = nil
	artifact = append(artifact[:len(artifact)-1], 'a', '"')
	if _, err := WithArtifactMetadata(context.Background(), base, artifact); !errors.Is(err, ErrLimit) {
		t.Fatalf("artifact cap+1 accepted: %v", err)
	}
}
