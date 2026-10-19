package value

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// Headers contains canonical HTTP names and ordered opaque byte values.
type Headers interface {
	Value
	Entries() List
	headers()
}
type headers struct {
	base
	entries List
}

func (headers) headers()        {}
func (v headers) Entries() List { return v.entries }
func (v headers) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Entries List `json:"entries"`
	}{v.entries})
}

// CanonicalHeaderName validates an ASCII HTTP token and lowers its case.
func CanonicalHeaderName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("HTTP header name is empty")
	}
	for _, character := range []byte(name) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return "", fmt.Errorf("HTTP header name is not an ASCII token")
	}
	return strings.ToLower(name), nil
}

// NewHeaders validates and canonicalizes immutable HTTP header entries.
func NewHeaders(entries List) (Headers, error) {
	if entries == nil || !types.Equal(entries.Type(), types.HeaderEntries()) {
		return nil, fmt.Errorf("HTTP headers require byte-valued entries")
	}
	items := make([]Value, 0, entries.Len())
	for index, item := range entries.Items() {
		fields := item.(Record)
		name, err := CanonicalHeaderName(fields.Field("name").(Text).Text())
		if err != nil {
			return nil, fmt.Errorf("HTTP header entry %d name: %w", index, err)
		}
		content := fields.Field("value").(Bytes)
		for _, character := range content.Bytes() {
			if character != 9 && (character < 32 || character == 127) {
				return nil, fmt.Errorf("HTTP header entry %d value contains a prohibited byte", index)
			}
		}
		items = append(items, must(NewRecord([]Field{{Name: "name", Value: must(NewText(types.String(), name))}, {Name: "value", Value: content}})))
	}
	slices.SortStableFunc(items, func(left, right Value) int {
		return strings.Compare(left.(Record).Field("name").(Text).Text(), right.(Record).Field("name").(Text).Text())
	})
	canonical := must(NewList(types.HeaderEntries(), items))
	return headers{newBase(types.Headers(), canonical.Weight()), canonical}, nil
}
func redactedHeaders(header Headers) Headers {
	items := make([]Value, 0, header.Entries().Len())
	for _, item := range header.Entries().Items() {
		fields := item.(Record)
		items = append(items, must(NewRecord([]Field{{Name: "name", Value: fields.Field("name")}, {Name: "value", Value: NewBytes(nil)}})))
	}
	entries := must(NewList(types.HeaderEntries(), items))
	return headers{newBase(types.Headers(), entries.Weight()), entries}
}
