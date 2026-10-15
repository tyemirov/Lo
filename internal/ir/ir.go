package ir

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type BindingID string
type Effects struct {
	Devices bool
	Reads   bool
	Commits bool
}

func (e Effects) With(other Effects) Effects {
	return Effects{Devices: e.Devices || other.Devices, Reads: e.Reads || other.Reads, Commits: e.Commits || other.Commits}
}

type Expression interface {
	Type() types.Type
	Span() source.Span
	expression()
}
type base struct {
	t    types.Type
	span source.Span
}

func (base) expression()         {}
func (b base) Type() types.Type  { return b.t }
func (b base) Span() source.Span { return b.span }
func newBase(t types.Type, s source.Span) (base, error) {
	if t == nil || s == nil {
		return base{}, fmt.Errorf("graph expression requires type and span")
	}
	return base{t, s}, nil
}

type Literal interface {
	Expression
	Text() string
}
type literal struct {
	base
	text string
}

func (l *literal) Text() string { return l.text }
func NewLiteral(t types.Type, text string, s source.Span) (Literal, error) {
	b, e := newBase(t, s)
	if e != nil {
		return nil, e
	}
	return &literal{b, text}, nil
}

type Reference interface {
	Expression
	Binding() BindingID
}
type reference struct {
	base
	binding BindingID
}

func (r *reference) Binding() BindingID { return r.binding }
func NewReference(t types.Type, id BindingID, s source.Span) (Reference, error) {
	b, e := newBase(t, s)
	if e != nil {
		return nil, e
	}
	if id == "" {
		return nil, fmt.Errorf("empty binding reference")
	}
	return &reference{b, id}, nil
}

type Argument interface {
	Position() int
	Span() source.Span
	argument()
}
type argBase struct {
	position int
	span     source.Span
}

func (argBase) argument()           {}
func (a argBase) Position() int     { return a.position }
func (a argBase) Span() source.Span { return a.span }

type ValueArgument interface {
	Argument
	Value() Expression
}
type valueArgument struct {
	argBase
	value Expression
}

func (a valueArgument) Value() Expression { return a.value }
func ValueArg(position int, value Expression) (ValueArgument, error) {
	if value == nil || position < 0 {
		return nil, fmt.Errorf("invalid checked value argument")
	}
	return valueArgument{argBase{position, value.Span()}, value}, nil
}

type FlowArgument interface {
	Argument
	Flow() identity.FlowID
}
type flowArgument struct {
	argBase
	flow identity.FlowID
}

func (a flowArgument) Flow() identity.FlowID { return a.flow }
func FlowArg(position int, id identity.FlowID, s source.Span) (FlowArgument, error) {
	if id == "" || s == nil || position < 0 {
		return nil, fmt.Errorf("invalid checked flow argument")
	}
	return flowArgument{argBase{position, s}, id}, nil
}

type BuiltinCall interface {
	Expression
	Operation() catalog.Operation
	Arguments() []Argument
	Labels() []string
}
type builtinCall struct {
	base
	op     catalog.Operation
	args   []Argument
	labels []string
}

func (c *builtinCall) Operation() catalog.Operation { return c.op }
func (c *builtinCall) Arguments() []Argument        { return slices.Clone(c.args) }
func (c *builtinCall) Labels() []string             { return slices.Clone(c.labels) }
func NewBuiltin(t types.Type, op catalog.Operation, args []Argument, labels []string, s source.Span) (BuiltinCall, error) {
	b, e := newBase(t, s)
	if e != nil {
		return nil, e
	}
	if op == nil {
		return nil, fmt.Errorf("absent checked operation")
	}
	return &builtinCall{b, op, slices.Clone(args), slices.Clone(labels)}, nil
}

type FlowCall interface {
	Expression
	Flow() identity.FlowID
	Arguments() []ValueArgument
}
type flowCall struct {
	base
	flow identity.FlowID
	args []ValueArgument
}

func (c *flowCall) Flow() identity.FlowID      { return c.flow }
func (c *flowCall) Arguments() []ValueArgument { return slices.Clone(c.args) }
func NewFlowCall(t types.Type, id identity.FlowID, args []ValueArgument, s source.Span) (FlowCall, error) {
	b, e := newBase(t, s)
	if e != nil {
		return nil, e
	}
	if id == "" {
		return nil, fmt.Errorf("empty checked flow reference")
	}
	return &flowCall{b, id, slices.Clone(args)}, nil
}

type Projection interface {
	Expression
	Input() Expression
	Field() string
}
type projection struct {
	base
	input Expression
	field string
}

func (p *projection) Input() Expression { return p.input }
func (p *projection) Field() string     { return p.field }
func NewProjection(t types.Type, input Expression, field string, s source.Span) (Projection, error) {
	b, e := newBase(t, s)
	if e != nil {
		return nil, e
	}
	if input == nil || field == "" {
		return nil, fmt.Errorf("invalid checked projection")
	}
	return &projection{b, input, field}, nil
}

type ConversionKind uint8

const (
	Success ConversionKind = iota + 1
	Failure
	Propagate
	RecordView
	ScalarListView
	FallibleRecordView
)

type Conversion interface {
	Expression
	Input() Expression
	Conversion() ConversionKind
}
type conversion struct {
	base
	input Expression
	kind  ConversionKind
}

func (c *conversion) Input() Expression          { return c.input }
func (c *conversion) Conversion() ConversionKind { return c.kind }
func NewConversion(t types.Type, input Expression, kind ConversionKind) (Conversion, error) {
	if input == nil {
		return nil, fmt.Errorf("absent conversion input")
	}
	b, e := newBase(t, input.Span())
	if e != nil {
		return nil, e
	}
	valid := false
	switch kind {
	case Success:
		if f, ok := t.(types.Fallible); ok {
			valid = types.Equal(f.Base(), input.Type())
		}
	case Failure:
		valid = t.Kind() == types.FallibleKind && input.Type().Kind() == types.ErrorKind
	case Propagate:
		if f, ok := input.Type().(types.Fallible); ok {
			valid = types.Equal(f.Base(), t)
		}
	case RecordView:
		valid = t.Kind() == types.AnyRecordKind && input.Type().Kind() == types.RecordKind
	case ScalarListView:
		if l, ok := t.(types.List); ok {
			valid = l.Element().Kind() == types.ScalarKind && input.Type().Kind() == types.StringKind
		}
	case FallibleRecordView:
		from, fromOK := input.Type().(types.Fallible)
		to, toOK := t.(types.Fallible)
		valid = fromOK && toOK && from.Base().Kind() == types.RecordKind && to.Base().Kind() == types.AnyRecordKind
	}
	if !valid {
		return nil, fmt.Errorf("invalid graph conversion from %s to %s", input.Type(), t)
	}
	return &conversion{b, input, kind}, nil
}

type Case interface {
	Body() Expression
	Span() source.Span
	caseNode()
}
type caseBase struct {
	body Expression
	span source.Span
}

func (caseBase) caseNode()           {}
func (c caseBase) Body() Expression  { return c.body }
func (c caseBase) Span() source.Span { return c.span }

type ExactCase interface {
	Case
	Head() Expression
}
type exactCase struct {
	caseBase
	head Expression
}

func (c exactCase) Head() Expression { return c.head }
func NewExactCase(head, body Expression, s source.Span) (ExactCase, error) {
	if head == nil || body == nil || s == nil {
		return nil, fmt.Errorf("incomplete exact case")
	}
	return exactCase{caseBase{body, s}, head}, nil
}

type PredicateCase interface {
	Case
	Flow() identity.FlowID
}
type predicateCase struct {
	caseBase
	flow identity.FlowID
}

func (c predicateCase) Flow() identity.FlowID { return c.flow }
func NewPredicateCase(id identity.FlowID, body Expression, s source.Span) (PredicateCase, error) {
	if id == "" || body == nil || s == nil {
		return nil, fmt.Errorf("incomplete predicate case")
	}
	return predicateCase{caseBase{body, s}, id}, nil
}

type DefaultCase interface {
	Case
	defaultCase()
}
type defaultCase struct{ caseBase }

func (defaultCase) defaultCase() {}
func NewDefaultCase(body Expression, s source.Span) (DefaultCase, error) {
	if body == nil || s == nil {
		return nil, fmt.Errorf("incomplete default case")
	}
	return defaultCase{caseBase{body, s}}, nil
}

type Match interface {
	Expression
	Selector() Expression
	Cases() []Case
}
type match struct {
	base
	selector Expression
	cases    []Case
}

func (m *match) Selector() Expression { return m.selector }
func (m *match) Cases() []Case        { return slices.Clone(m.cases) }
func NewMatch(t types.Type, selector Expression, cases []Case, s source.Span) (Match, error) {
	b, e := newBase(t, s)
	if e != nil {
		return nil, e
	}
	if selector == nil || len(cases) == 0 {
		return nil, fmt.Errorf("incomplete checked match")
	}
	defaults := 0
	for i, arm := range cases {
		if arm == nil || !types.Equal(arm.Body().Type(), t) {
			return nil, fmt.Errorf("inconsistent checked match branch")
		}
		if _, ok := arm.(DefaultCase); ok {
			defaults++
			if i != len(cases)-1 {
				return nil, fmt.Errorf("nonfinal checked default")
			}
		}
	}
	if defaults != 1 {
		return nil, fmt.Errorf("checked match requires one default")
	}
	return &match{b, selector, slices.Clone(cases)}, nil
}

type Statement interface {
	Span() source.Span
	statement()
}
type statementBase struct{ span source.Span }

func (statementBase) statement()          {}
func (s statementBase) Span() source.Span { return s.span }

type Binding interface {
	Statement
	ID() BindingID
	Name() string
	Value() Expression
}
type binding struct {
	statementBase
	id    BindingID
	name  string
	value Expression
}

func (b binding) ID() BindingID     { return b.id }
func (b binding) Name() string      { return b.name }
func (b binding) Value() Expression { return b.value }
func NewBinding(id BindingID, name string, value Expression, s source.Span) (Binding, error) {
	if id == "" || name == "" || value == nil || s == nil {
		return nil, fmt.Errorf("incomplete checked binding")
	}
	return binding{statementBase{s}, id, name, value}, nil
}

type Commit interface {
	Statement
	Target() Expression
}
type commit struct {
	statementBase
	target Expression
}

func (c commit) Target() Expression { return c.target }
func NewCommit(target Expression, s source.Span) (Commit, error) {
	if target == nil || s == nil {
		return nil, fmt.Errorf("incomplete commit")
	}
	if target.Type().Kind() != types.EffectKind && target.Type().Kind() != types.ServerKind {
		return nil, fmt.Errorf("invalid commit target type %s", target.Type())
	}
	return commit{statementBase{s}, target}, nil
}

type Return interface {
	Statement
	Value() Expression
	returnStatement()
}
type returned struct {
	statementBase
	value Expression
}

func (returned) returnStatement()    {}
func (r returned) Value() Expression { return r.value }
func NewReturn(value Expression, s source.Span) (Return, error) {
	if value == nil || s == nil {
		return nil, fmt.Errorf("incomplete checked return")
	}
	return returned{statementBase{s}, value}, nil
}

type Parameter struct {
	ID   BindingID
	Name string
	Type types.Type
	Span source.Span
}
type Flow interface {
	ID() identity.FlowID
	Name() string
	Signature() types.Signature
	Parameters() []Parameter
	Handler() identity.FlowID
	Body() []Statement
	Effects() Effects
	Span() source.Span
	flow()
}
type flow struct {
	id         identity.FlowID
	name       string
	signature  types.Signature
	parameters []Parameter
	handler    identity.FlowID
	body       []Statement
	effects    Effects
	span       source.Span
}

func (*flow) flow()                        {}
func (f *flow) ID() identity.FlowID        { return f.id }
func (f *flow) Name() string               { return f.name }
func (f *flow) Signature() types.Signature { return f.signature }
func (f *flow) Parameters() []Parameter    { return slices.Clone(f.parameters) }
func (f *flow) Handler() identity.FlowID   { return f.handler }
func (f *flow) Body() []Statement          { return slices.Clone(f.body) }
func (f *flow) Effects() Effects           { return f.effects }
func (f *flow) Span() source.Span          { return f.span }
func NewFlow(id identity.FlowID, name string, sig types.Signature, params []Parameter, handler identity.FlowID, body []Statement, effects Effects, s source.Span) (Flow, error) {
	if id == "" || name == "" || sig == nil || len(body) == 0 || s == nil {
		return nil, fmt.Errorf("incomplete checked flow")
	}
	if len(params) != len(sig.Parameters()) {
		return nil, fmt.Errorf("inconsistent checked parameters")
	}
	seen := map[BindingID]bool{}
	for i, p := range params {
		if p.ID == "" || seen[p.ID] || p.Span == nil || p.Type == nil || p.Name != sig.Parameters()[i].Name || !types.Equal(p.Type, sig.Parameters()[i].Type) {
			return nil, fmt.Errorf("invalid checked parameter %s", p.Name)
		}
		seen[p.ID] = true
	}
	if err := validBody(body); err != nil {
		return nil, err
	}
	output, ok := body[len(body)-1].(Return)
	if !ok || !types.Equal(output.Value().Type(), sig.Output()) {
		return nil, fmt.Errorf("inconsistent checked return")
	}
	if handler != "" && sig.Output().Kind() == types.FallibleKind {
		return nil, fmt.Errorf("fallible flow has self-handler")
	}
	return &flow{id, name, sig, slices.Clone(params), handler, slices.Clone(body), effects, s}, nil
}

type Program interface {
	Source() source.File
	Body() []Statement
	Flows() []Flow
	ResultType() types.Type
	program()
}
type program struct {
	source     source.File
	body       []Statement
	flows      []Flow
	resultType types.Type
}

func (*program) program()                 {}
func (p *program) Source() source.File    { return p.source }
func (p *program) Body() []Statement      { return slices.Clone(p.body) }
func (p *program) Flows() []Flow          { return slices.Clone(p.flows) }
func (p *program) ResultType() types.Type { return p.resultType }
func NewProgram(src source.File, body []Statement, flows []Flow) (Program, error) {
	if src == nil {
		return nil, fmt.Errorf("checked program requires a source")
	}
	if err := validBody(body); err != nil {
		return nil, err
	}
	seen := map[identity.FlowID]bool{}
	for _, f := range flows {
		if f == nil || seen[f.ID()] {
			return nil, fmt.Errorf("invalid checked flow table")
		}
		seen[f.ID()] = true
	}
	var result types.Type
	if len(body) > 0 {
		if r, ok := body[len(body)-1].(Return); ok {
			result = r.Value().Type()
		}
	}
	return &program{src, slices.Clone(body), slices.Clone(flows), result}, nil
}

func validBody(body []Statement) error {
	seen := map[BindingID]bool{}
	for i, statement := range body {
		if statement == nil {
			return fmt.Errorf("absent checked statement")
		}
		if b, ok := statement.(Binding); ok {
			if seen[b.ID()] {
				return fmt.Errorf("duplicate checked binding")
			}
			seen[b.ID()] = true
		}
		if _, ok := statement.(Return); ok && i != len(body)-1 {
			return fmt.Errorf("nonfinal checked return")
		}
	}
	return nil
}
