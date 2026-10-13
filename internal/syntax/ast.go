package syntax

import (
	"encoding/json"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type Node interface {
	json.Marshaler
	Span() source.Span
	node()
}
type Statement interface {
	Node
	statement()
}
type Expression interface {
	Node
	expression()
}
type TypeExpression interface {
	Node
	typeExpression()
}
type Program interface {
	Node
	Statements() []Statement
	Source() source.File
}
type Binding interface {
	Statement
	Name() string
	Value() Expression
}
type Flow interface {
	Statement
	Name() string
	Parameters() []Parameter
	Output() TypeExpression
	Handler() string
	Body() []Statement
}
type Commit interface {
	Statement
	Target() string
}
type Return interface {
	Statement
	Value() Expression
	returnStatement()
}
type Literal interface {
	Expression
	LiteralKind() LiteralKind
	Text() string
}
type Reference interface {
	Expression
	Name() string
	reference()
}
type Projection interface {
	Expression
	Parts() []string
}
type Call interface {
	Expression
	Name() string
	Arguments() []Argument
	call()
}
type Pipe interface {
	Expression
	Left() Expression
	Name() string
	Arguments() []Argument
}
type Match interface {
	Expression
	Selector() Expression
	Cases() []Case
}
type Case interface {
	Node
	Body() Expression
	caseNode()
}
type ExactCase interface {
	Case
	Head() Expression
}
type PredicateCase interface {
	Case
	Flow() string
}
type DefaultCase interface {
	Case
	defaultCase()
}
type NamedType interface {
	TypeExpression
	Name() string
	Arguments() []TypeExpression
}
type RecordType interface {
	TypeExpression
	Fields() []Field
}
type FallibleType interface {
	TypeExpression
	Base() TypeExpression
}

type Parameter struct {
	Name string         `json:"name"`
	Type TypeExpression `json:"type"`
	Span source.Span    `json:"span"`
}
type Argument struct {
	Name  string      `json:"name,omitempty"`
	Value Expression  `json:"value"`
	Span  source.Span `json:"span"`
}
type Field struct {
	Name string         `json:"name"`
	Type TypeExpression `json:"type"`
	Span source.Span    `json:"span"`
}

type base struct{ span source.Span }

func (base) node()               {}
func (b base) Span() source.Span { return b.span }

type statementBase struct{ base }

func (statementBase) statement() {}

type expressionBase struct{ base }

func (expressionBase) expression() {}

type typeBase struct{ base }

func (typeBase) typeExpression() {}

type caseBase struct {
	base
	body Expression
}

func (caseBase) caseNode()          {}
func (c caseBase) Body() Expression { return c.body }

type header struct {
	Kind string      `json:"kind"`
	Span source.Span `json:"span"`
}

type program struct {
	base
	source     source.File
	statements []Statement
}

func (p program) Source() source.File     { return p.source }
func (p program) Statements() []Statement { return slices.Clone(p.statements) }
func (p program) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Statements []Statement `json:"statements"`
	}{header{"program", p.span}, p.statements})
}

type binding struct {
	statementBase
	name  string
	value Expression
}

func (b binding) Name() string      { return b.name }
func (b binding) Value() Expression { return b.value }
func (b binding) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Name  string     `json:"name"`
		Value Expression `json:"value"`
	}{header{"binding", b.span}, b.name, b.value})
}

type flow struct {
	statementBase
	name       string
	parameters []Parameter
	output     TypeExpression
	handler    string
	body       []Statement
}

func (f flow) Name() string            { return f.name }
func (f flow) Parameters() []Parameter { return slices.Clone(f.parameters) }
func (f flow) Output() TypeExpression  { return f.output }
func (f flow) Handler() string         { return f.handler }
func (f flow) Body() []Statement       { return slices.Clone(f.body) }
func (f flow) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Name       string         `json:"name"`
		Parameters []Parameter    `json:"parameters"`
		Output     TypeExpression `json:"output"`
		Handler    string         `json:"handler,omitempty"`
		Body       []Statement    `json:"body"`
	}{header{"flow", f.span}, f.name, f.parameters, f.output, f.handler, f.body})
}

type commit struct {
	statementBase
	target string
}

func (c commit) Target() string { return c.target }
func (c commit) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Target string `json:"target"`
	}{header{"commit", c.span}, c.target})
}

type returnNode struct {
	statementBase
	value Expression
}

func (r returnNode) returnStatement()  {}
func (r returnNode) Value() Expression { return r.value }
func (r returnNode) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Value Expression `json:"value"`
	}{header{"return", r.span}, r.value})
}

type LiteralKind string

const (
	StringLiteral LiteralKind = "String"
	IntLiteral    LiteralKind = "Int"
	FloatLiteral  LiteralKind = "Float"
	BoolLiteral   LiteralKind = "Bool"
	NullLiteral   LiteralKind = "Null"
)

type literal struct {
	expressionBase
	kind LiteralKind
	text string
}

func (l literal) LiteralKind() LiteralKind { return l.kind }
func (l literal) Text() string             { return l.text }
func (l literal) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		LiteralKind LiteralKind `json:"literal_kind"`
		Text        string      `json:"text"`
	}{header{"literal", l.span}, l.kind, l.text})
}

type reference struct {
	expressionBase
	name string
}

func (r reference) reference()   {}
func (r reference) Name() string { return r.name }
func (r reference) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Name string `json:"name"`
	}{header{"reference", r.span}, r.name})
}

type projection struct {
	expressionBase
	parts []string
}

func (p projection) Parts() []string { return slices.Clone(p.parts) }
func (p projection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Parts []string `json:"parts"`
	}{header{"projection", p.span}, p.parts})
}

type call struct {
	expressionBase
	name      string
	arguments []Argument
}

func (c call) call()                 {}
func (c call) Name() string          { return c.name }
func (c call) Arguments() []Argument { return slices.Clone(c.arguments) }
func (c call) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Name      string     `json:"name"`
		Arguments []Argument `json:"arguments"`
	}{header{"call", c.span}, c.name, c.arguments})
}

type pipe struct {
	expressionBase
	left      Expression
	name      string
	arguments []Argument
}

func (p pipe) Left() Expression      { return p.left }
func (p pipe) Name() string          { return p.name }
func (p pipe) Arguments() []Argument { return slices.Clone(p.arguments) }
func (p pipe) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Left      Expression `json:"left"`
		Name      string     `json:"name"`
		Arguments []Argument `json:"arguments"`
	}{header{"pipe", p.span}, p.left, p.name, p.arguments})
}

type match struct {
	expressionBase
	selector Expression
	cases    []Case
}

func (m match) Selector() Expression { return m.selector }
func (m match) Cases() []Case        { return slices.Clone(m.cases) }
func (m match) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Selector Expression `json:"selector"`
		Cases    []Case     `json:"cases"`
	}{header{"match", m.span}, m.selector, m.cases})
}

type exactCase struct {
	caseBase
	head Expression
}

func (c exactCase) Head() Expression { return c.head }
func (c exactCase) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Head Expression `json:"head"`
		Body Expression `json:"body"`
	}{header{"exact", c.span}, c.head, c.body})
}

type predicateCase struct {
	caseBase
	flow string
}

func (c predicateCase) Flow() string { return c.flow }
func (c predicateCase) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Flow string     `json:"flow"`
		Body Expression `json:"body"`
	}{header{"predicate", c.span}, c.flow, c.body})
}

type defaultCase struct{ caseBase }

func (defaultCase) defaultCase() {}
func (c defaultCase) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Body Expression `json:"body"`
	}{header{"default", c.span}, c.body})
}

type namedType struct {
	typeBase
	name      string
	arguments []TypeExpression
}

func (t namedType) Name() string                { return t.name }
func (t namedType) Arguments() []TypeExpression { return slices.Clone(t.arguments) }
func (t namedType) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Name      string           `json:"name"`
		Arguments []TypeExpression `json:"arguments"`
	}{header{"named_type", t.span}, t.name, t.arguments})
}

type recordType struct {
	typeBase
	fields []Field
}

func (t recordType) Fields() []Field { return slices.Clone(t.fields) }
func (t recordType) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Fields []Field `json:"fields"`
	}{header{"record_type", t.span}, t.fields})
}

type fallibleType struct {
	typeBase
	inner TypeExpression
}

func (t fallibleType) Base() TypeExpression { return t.inner }
func (t fallibleType) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		header
		Base TypeExpression `json:"base"`
	}{header{"fallible_type", t.span}, t.inner})
}
