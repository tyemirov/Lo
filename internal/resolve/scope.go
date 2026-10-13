package resolve

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type Kind uint8

const (
	Value Kind = iota + 1
	Flow
)

type Entry interface {
	ID() string
	Name() string
	Kind() Kind
	Span() source.Span
	entry()
}
type entry struct {
	id, name string
	kind     Kind
	span     source.Span
}

func (*entry) entry()              {}
func (e *entry) ID() string        { return e.id }
func (e *entry) Name() string      { return e.name }
func (e *entry) Kind() Kind        { return e.kind }
func (e *entry) Span() source.Span { return e.span }

type Scope interface {
	Declare(string, Kind, source.Span) (Entry, diagnostic.Diagnostic)
	Local(string) (Entry, bool)
	Lookup(string) (Entry, bool)
	scope()
}
type scope struct {
	parent  Scope
	entries map[string]Entry
}

func (*scope) scope()        {}
func New(parent Scope) Scope { return &scope{parent, make(map[string]Entry)} }
func (s *scope) Declare(name string, kind Kind, span source.Span) (Entry, diagnostic.Diagnostic) {
	if catalog.Predeclared(name) {
		return nil, diagnostic.New(diagnostic.PredeclaredName, diagnostic.Resolve, fmt.Sprintf("%s is a predeclared name", name), span)
	}
	if prior, ok := s.entries[name]; ok {
		return nil, diagnostic.WithRelated(diagnostic.New(diagnostic.DuplicateName, diagnostic.Resolve, "duplicate declaration "+name, span), prior.Span())
	}
	if name == "" || span == nil || (kind != Value && kind != Flow) {
		panic("invalid source declaration")
	}
	e := &entry{fmt.Sprintf("%d:%d", kind, span.Start().Offset), name, kind, span}
	s.entries[name] = e
	return e, nil
}
func (s *scope) Local(name string) (Entry, bool) { e, ok := s.entries[name]; return e, ok }
func (s *scope) Lookup(name string) (Entry, bool) {
	if e, ok := s.Local(name); ok {
		return e, true
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return nil, false
}
