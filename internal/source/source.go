package source

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"
)

const MaxBytes = 16 * 1024 * 1024

type Position struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Span interface {
	json.Marshaler
	File() string
	Start() Position
	End() Position
	span()
}

type File interface {
	Path() string
	Text() string
	Span(start, end int) Span
	file()
}

type file struct {
	path  string
	text  string
	lines []int
}

func New(path, text string) (File, error) {
	if path == "" {
		return nil, fmt.Errorf("source path is empty")
	}
	if len(text) > MaxBytes {
		return nil, fmt.Errorf("source %s exceeds %d bytes", path, MaxBytes)
	}
	lines := []int{0}
	for i := range len(text) {
		if text[i] == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &file{path, text, lines}, nil
}

func (*file) file()          {}
func (f *file) Path() string { return f.path }
func (f *file) Text() string { return f.text }
func (f *file) position(offset int) Position {
	i := sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > offset }) - 1
	return Position{offset, i + 1, utf8.RuneCountInString(f.text[f.lines[i]:offset]) + 1}
}
func (f *file) Span(start, end int) Span {
	return location{f.path, f.position(start), f.position(end)}
}

type location struct {
	path       string
	start, end Position
}

func (location) span()             {}
func (s location) File() string    { return s.path }
func (s location) Start() Position { return s.start }
func (s location) End() Position   { return s.end }
func (s location) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		File  string   `json:"file"`
		Start Position `json:"start"`
		End   Position `json:"end"`
	}{s.path, s.start, s.end})
}
