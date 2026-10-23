package nativeplan

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type sourceMetadata struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type metadataSpan struct {
	FileIndex uint64          `json:"file_index"`
	Start     source.Position `json:"start"`
	End       source.Position `json:"end"`
}
type metadataSpanKey struct {
	File       string
	Start, End source.Position
}
type metadataPayload struct {
	Text            *string             `json:"text,omitempty"`
	FloatBits       *string             `json:"float_bits,omitempty"`
	Field           *string             `json:"field,omitempty"`
	Operation       *string             `json:"operation,omitempty"`
	Labels          *[]string           `json:"labels,omitempty"`
	Arguments       *[]ArgumentMetadata `json:"arguments,omitempty"`
	DerivativePlan  *uint64             `json:"derivative_plan,omitempty"`
	DerivativeNode  *uint64             `json:"derivative_node,omitempty"`
	DerivativeFrame *uint64             `json:"derivative_frame,omitempty"`
}
type metadataRow struct {
	Span    uint64          `json:"span"`
	Action  string          `json:"action"`
	Type    uint64          `json:"type"`
	Owner   string          `json:"owner"`
	Flow    uint64          `json:"flow"`
	Payload metadataPayload `json:"payload"`
}
type metadataDocument struct {
	ABI         string               `json:"abi"`
	HasResult   bool                 `json:"has_result"`
	Source      sourceMetadata       `json:"source"`
	Types       []typeMetadata       `json:"types"`
	Files       []string             `json:"files"`
	Spans       []metadataSpan       `json:"spans"`
	Locations   []metadataRow        `json:"locations"`
	Flows       []flowMetadata       `json:"flows"`
	Derivatives []derivativeMetadata `json:"derivatives"`
}

// metadataStream encodes only the closed metadata DTO types. Each write observes
// the byte cap before allocation; string escaping uses at most one small chunk.
type metadataStream struct {
	ctx   context.Context
	bytes []byte
	limit int
}

func (out *metadataStream) text(text string) error {
	if err := out.ctx.Err(); err != nil {
		return err
	}
	if len(text) > out.limit-len(out.bytes) {
		return fmt.Errorf("%w: metadata exceeds %d bytes", ErrLimit, out.limit)
	}
	out.grow(len(text))
	out.bytes = append(out.bytes, text...)
	return nil
}
func (out *metadataStream) quoted(text string) error {
	if err := out.text(`"`); err != nil {
		return err
	}
	for start := 0; start < len(text); {
		end := start
		for end < len(text) && end-start < 4096 {
			_, size := utf8.DecodeRuneInString(text[end:])
			end += size
		}
		encoded, err := json.Marshal(text[start:end])
		if err != nil {
			return err
		}
		if err := out.text(string(encoded[1 : len(encoded)-1])); err != nil {
			return err
		}
		start = end
	}
	return out.text(`"`)
}
func (out *metadataStream) value(value reflect.Value) error {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return out.text("null")
		}
		return out.value(value.Elem())
	case reflect.String:
		return out.quoted(value.String())
	case reflect.Bool:
		return out.text(strconv.FormatBool(value.Bool()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return out.text(strconv.FormatUint(value.Uint(), 10))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return out.text(strconv.FormatInt(value.Int(), 10))
	case reflect.Slice:
		if value.IsNil() {
			return out.text("null")
		}
		if err := out.text("["); err != nil {
			return err
		}
		for index := 0; index < value.Len(); index++ {
			if index > 0 {
				if err := out.text(","); err != nil {
					return err
				}
			}
			if err := out.value(value.Index(index)); err != nil {
				return err
			}
		}
		return out.text("]")
	case reflect.Struct:
		if err := out.text("{"); err != nil {
			return err
		}
		written := false
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				return fmt.Errorf("metadata field %s has no canonical JSON key", field.Name)
			}
			if len(tag) > 1 && tag[1] == "omitempty" && value.Field(index).IsZero() {
				continue
			}
			if written {
				if err := out.text(","); err != nil {
					return err
				}
			}
			written = true
			if err := out.quoted(name); err != nil {
				return err
			}
			if err := out.text(":"); err != nil {
				return err
			}
			if err := out.value(value.Field(index)); err != nil {
				return err
			}
		}
		return out.text("}")
	default:
		return fmt.Errorf("unsupported canonical metadata value %s", value.Kind())
	}
}
func marshalMetadata(ctx context.Context, document metadataDocument) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := metadataStream{ctx: ctx, limit: MaxModuleBytes}
	if err := out.value(reflect.ValueOf(document)); err != nil {
		return nil, fmt.Errorf("native metadata: %w", err)
	}
	return out.bytes, nil
}
func (l *lowering) accountMetadata(size int) {
	if l.err != nil {
		return
	}
	if size > MaxModuleBytes-l.metadataLowerBound {
		l.err = fmt.Errorf("%w: metadata exceeds %d bytes", ErrLimit, MaxModuleBytes)
		return
	}
	l.metadataLowerBound += size
}
func (l *lowering) internSpan(span source.Span) uint64 {
	key := metadataSpanKey{span.File(), span.Start(), span.End()}
	if id, exists := l.spanIDs[key]; exists {
		return id
	}
	file, exists := l.fileIDs[key.File]
	if !exists {
		file = uint64(len(l.files))
		l.fileIDs[key.File] = file
		l.files = append(l.files, key.File)
		l.accountMetadata(len(key.File))
		l.accountMetadata(2)
	}
	id := uint64(len(l.spans))
	l.spanIDs[key] = id
	l.spans = append(l.spans, metadataSpan{file, key.Start, key.End})
	// These literal skeletons are lower bounds: every number has at least one
	// byte and JSON string escaping cannot shorten the original UTF-8 bytes.
	l.accountMetadata(len(`{"file_index":0,"start":{"offset":0,"line":0,"column":0},"end":{"offset":0,"line":0,"column":0}}`))
	return id
}
func (l *lowering) encodeMetadata() ([]byte, error) {
	rows := make([]metadataRow, len(l.metadata.Locations))
	for index := range l.metadata.Locations {
		location := &l.metadata.Locations[index]
		payload := metadataPayload{}
		switch {
		case location.Action == "literal":
			payload.Text = &location.Text
			if l.metadata.Types[location.Type].Kind == "Float" {
				payload.FloatBits = &location.FloatBits
			}
		case location.Action == "project":
			payload.Field = &location.Field
		case location.Action == "operation":
			payload.Operation = &location.Operation
			payload.Labels = &location.Labels
			payload.Arguments = &location.Arguments
		case location.Action == "body" || strings.HasPrefix(location.Action, "derivative_"):
			payload.DerivativePlan = &location.DerivativePlan
			payload.DerivativeNode = &location.DerivativeNode
			payload.DerivativeFrame = &location.DerivativeFrame
			if location.Action != "body" {
				payload.Operation = &location.Operation
				payload.Labels = &location.Labels
				payload.Arguments = &location.Arguments
			}
		}
		rows[index] = metadataRow{l.siteSpans[index], location.Action, location.Type, location.Owner, location.Flow, payload}
	}
	return marshalMetadata(l.ctx, metadataDocument{l.metadata.ABI, l.metadata.HasResult, l.metadata.Source, l.metadata.Types, l.files, l.spans, rows, l.metadata.Flows, l.metadata.Derivatives})
}

// WithArtifactMetadata adds the artifact JSON value without buffering its full
// encoding or decoding the canonical metadata into a second wire shape.
func WithArtifactMetadata(ctx context.Context, metadata []byte, artifact json.RawMessage) ([]byte, error) {
	if ctx == nil || len(metadata) == 0 || metadata[len(metadata)-1] != '}' {
		return nil, fmt.Errorf("artifact metadata requires a context and canonical object")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !json.Valid(artifact) {
		return nil, fmt.Errorf("invalid artifact metadata JSON")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := metadataStream{ctx: ctx, limit: MaxModuleBytes}
	if err := out.raw(metadata[:len(metadata)-1]); err != nil {
		return nil, err
	}
	if err := out.text(`,"artifact":`); err != nil {
		return nil, err
	}
	var chunk [4096]byte
	used := 0
	quoted, escaped := false, false
	for index, character := range artifact {
		if index%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if !quoted && (character == ' ' || character == '\t' || character == '\r' || character == '\n') {
			continue
		}
		chunk[used] = character
		used++
		if quoted {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				quoted = false
			}
		} else if character == '"' {
			quoted = true
		}
		if used == len(chunk) {
			if err := out.raw(chunk[:used]); err != nil {
				return nil, err
			}
			used = 0
		}
	}
	if err := out.raw(chunk[:used]); err != nil {
		return nil, err
	}
	if err := out.text("}"); err != nil {
		return nil, err
	}
	return out.bytes, nil
}
func (out *metadataStream) raw(encoded []byte) error {
	if err := out.ctx.Err(); err != nil {
		return err
	}
	if len(encoded) > out.limit-len(out.bytes) {
		return fmt.Errorf("%w: metadata exceeds %d bytes", ErrLimit, out.limit)
	}
	out.grow(len(encoded))
	out.bytes = append(out.bytes, encoded...)
	return nil
}

func (out *metadataStream) grow(added int) {
	needed := len(out.bytes) + added
	if needed <= cap(out.bytes) {
		return
	}
	capacity := cap(out.bytes) * 2
	if capacity < needed {
		capacity = needed
	}
	if capacity > out.limit {
		capacity = out.limit
	}
	replacement := make([]byte, len(out.bytes), capacity)
	copy(replacement, out.bytes)
	out.bytes = replacement
}
