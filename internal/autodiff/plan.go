package autodiff

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const MaxNodes = 100_000

type Edge struct{ Operand, Node int }
type Selection struct {
	Key  string
	Node int
}
type Argument struct {
	Name    string
	Binding ir.BindingID
}
type Frame struct {
	Invocation int
	Name       string
	Span       source.Span
	Arguments  []Argument
}

type Node interface {
	ID() int
	Binding() ir.BindingID
	Expression() ir.Expression
	Operation() string
	Operands() []ir.BindingID
	Edges() []Edge
	Frames() []Frame
	Reverse() bool
	Retained() bool
	Release() []ir.BindingID
	node()
}
type node struct {
	id                int
	expression        ir.Expression
	operation         string
	operands          []ir.BindingID
	edges             []Edge
	frames            []Frame
	reverse, retained bool
	release           []ir.BindingID
}

func (*node) node()                       {}
func (n *node) ID() int                   { return n.id }
func (n *node) Binding() ir.BindingID     { return binding(n.id) }
func (n *node) Expression() ir.Expression { return n.expression }
func (n *node) Operation() string         { return n.operation }
func (n *node) Operands() []ir.BindingID  { return slices.Clone(n.operands) }
func (n *node) Edges() []Edge             { return slices.Clone(n.edges) }
func (n *node) Frames() []Frame {
	out := slices.Clone(n.frames)
	for i := range out {
		out[i].Arguments = slices.Clone(out[i].Arguments)
	}
	return out
}
func (n *node) Reverse() bool           { return n.reverse }
func (n *node) Retained() bool          { return n.retained }
func (n *node) Release() []ir.BindingID { return slices.Clone(n.release) }

// Call is a checked builtin with its immutable, expanded derivative plan.
// Each helper invocation has distinct nodes, even when its inputs are equal.
type Call interface {
	ir.BuiltinCall
	Nodes() []Node
	Parameters() []ir.Parameter
	Selections() []Selection
	Result() ir.BindingID
	ResultNode() int
}
type call struct {
	ir.BuiltinCall
	nodes      []Node
	parameters []ir.Parameter
	selections []Selection
	result     int
}

func (c *call) Nodes() []Node              { return slices.Clone(c.nodes) }
func (c *call) Parameters() []ir.Parameter { return slices.Clone(c.parameters) }
func (c *call) Selections() []Selection    { return slices.Clone(c.selections) }
func (c *call) Result() ir.BindingID       { return binding(c.result) }
func (c *call) ResultNode() int            { return c.result }

type symbol struct {
	id     int
	ref    ir.Expression
	active bool
	fields map[string]symbol
}
type builder struct {
	flows       map[identity.FlowID]ir.Flow
	nodes       []*node
	frames      []Frame
	invocations int
	visits      int
	err         diagnostic.Diagnostic
}

func binding(id int) ir.BindingID { return ir.BindingID(fmt.Sprintf("$derivative:%d", id)) }
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
func base(t types.Type) types.Type {
	if f, ok := t.(types.Fallible); ok {
		return f.Base()
	}
	return t
}

func Compile(original ir.BuiltinCall, loss ir.Flow, flows map[identity.FlowID]ir.Flow) (Call, diagnostic.Diagnostic) {
	b := &builder{flows: flows}
	params := loss.Parameters()
	args := make([]symbol, len(params))
	for i, p := range params {
		args[i] = b.emit(must(ir.NewReference(p.Type, p.ID, p.Span)), nil, nil, false)
	}
	targets := &args[len(args)-1]
	targets.fields = map[string]symbol{}
	selected := []Selection{}
	for _, f := range params[len(params)-1].Type.(types.Record).Fields() {
		s := b.emit(must(ir.NewProjection(f.Type, targets.ref, f.Name, targets.ref.Span())), []symbol{*targets}, nil, true)
		targets.fields[f.Name] = s
		selected = append(selected, Selection{f.Name, s.id})
	}
	result := b.invoke(loss, args, original.Span())
	if ft, ok := loss.Signature().Output().(types.Fallible); ok && b.err == nil {
		result = b.convert(ft.Base(), result, ir.Propagate)
	}
	if b.err != nil {
		return nil, b.err
	}
	// Only nodes on a selected-input-to-loss path enter the reverse graph.
	if result.active {
		b.nodes[result.id-1].reverse = true
	}
	for i := len(b.nodes) - 1; i >= 0; i-- {
		n := b.nodes[i]
		if n.reverse {
			for _, e := range n.edges {
				b.nodes[e.Node-1].reverse = true
			}
		}
	}
	keep := map[ir.BindingID]bool{binding(result.id): true}
	for _, s := range selected {
		keep[binding(s.Node)] = true
	}
	for _, n := range b.nodes {
		if n.reverse && n.operation != "" {
			for _, id := range n.operands {
				keep[id] = true
			}
			if n.operation == catalog.TensorSqrt {
				keep[n.Binding()] = true
			}
		}
	}
	last := map[ir.BindingID]int{}
	seenFrames := map[int]bool{}
	for i, n := range b.nodes {
		last[n.Binding()] = i
		for _, id := range n.operands {
			last[id] = i
		}
		for _, f := range n.frames {
			if !seenFrames[f.Invocation] {
				seenFrames[f.Invocation] = true
				for _, a := range f.Arguments {
					last[a.Binding] = i
				}
			}
		}
		n.retained = keep[n.Binding()]
	}
	for id, i := range last {
		if !keep[id] {
			b.nodes[i].release = append(b.nodes[i].release, id)
		}
	}
	out := &call{BuiltinCall: original, parameters: params, selections: selected, result: result.id}
	for _, n := range b.nodes {
		slices.Sort(n.release)
		out.nodes = append(out.nodes, n)
	}
	return out, nil
}
func (b *builder) fail(message string, s source.Span) {
	if b.err == nil {
		b.err = diagnostic.New(diagnostic.AutodiffUnsupported, diagnostic.Check, message, s)
	}
}
func (b *builder) emit(expr ir.Expression, args []symbol, edges []Edge, active bool) symbol {
	if b.err != nil {
		return symbol{}
	}
	if len(b.nodes) == MaxNodes {
		b.err = diagnostic.New(diagnostic.AutodiffLimit, diagnostic.Check, "derivative plan exceeds 100000 nodes", expr.Span())
		return symbol{}
	}
	n := &node{id: len(b.nodes) + 1, expression: expr, edges: edges, frames: slices.Clone(b.frames)}
	for _, a := range args {
		n.operands = append(n.operands, binding(a.id))
	}
	if c, ok := expr.(ir.BuiltinCall); ok {
		n.operation = c.Operation().Name()
	}
	b.nodes = append(b.nodes, n)
	return symbol{id: n.id, ref: must(ir.NewReference(expr.Type(), n.Binding(), expr.Span())), active: active}
}
func (b *builder) convert(t types.Type, input symbol, kind ir.ConversionKind) symbol {
	edges := []Edge{}
	if input.active {
		edges = append(edges, Edge{0, input.id})
	}
	result := b.emit(must(ir.NewConversion(t, input.ref, kind)), []symbol{input}, edges, input.active)
	result.fields = input.fields
	return result
}
func (b *builder) invoke(f ir.Flow, args []symbol, span source.Span) symbol {
	if b.err != nil {
		return symbol{}
	}
	if f.Handler() != "" {
		b.fail("self-handling flow is not differentiable: "+f.Name(), f.Span())
		return symbol{}
	}
	if len(b.frames) >= 256 {
		b.err = diagnostic.New(diagnostic.AutodiffLimit, diagnostic.Check, "derivative helper depth exceeds 256", span)
		return symbol{}
	}
	b.invocations++
	frame := Frame{Invocation: b.invocations, Name: f.Name(), Span: span}
	env := map[ir.BindingID]symbol{}
	for i, p := range f.Parameters() {
		env[p.ID] = args[i]
		frame.Arguments = append(frame.Arguments, Argument{p.Name, binding(args[i].id)})
	}
	b.frames = append(b.frames, frame)
	defer func() { b.frames = b.frames[:len(b.frames)-1] }()
	for _, statement := range f.Body() {
		if b.err != nil {
			return symbol{}
		}
		switch s := statement.(type) {
		case ir.Binding:
			env[s.ID()] = b.expression(s.Value(), env)
		case ir.Return:
			result := b.expression(s.Value(), env)
			if ft, ok := f.Signature().Output().(types.Fallible); ok && b.err == nil {
				result = b.convert(ft.Base(), result, ir.Propagate)
				if b.err == nil {
					result = b.convert(ft, result, ir.Success)
				}
			}
			return result
		case ir.Commit:
			b.fail("commit activation is not differentiable", s.Span())
			return symbol{}
		}
	}
	return symbol{}
}
func (b *builder) expression(expr ir.Expression, env map[ir.BindingID]symbol) symbol {
	if b.err != nil {
		return symbol{}
	}
	b.visits++
	if b.visits > MaxNodes {
		b.err = diagnostic.New(diagnostic.AutodiffLimit, diagnostic.Check, "derivative expansion exceeds 100000 expression visits", expr.Span())
		return symbol{}
	}
	switch e := expr.(type) {
	case ir.Reference:
		return env[e.Binding()]
	case ir.Literal:
		return b.emit(e, nil, nil, false)
	case ir.Conversion:
		if e.Conversion() == ir.Propagate || e.Conversion() == ir.Success {
			input := b.expression(e.Input(), env)
			if b.err != nil {
				return symbol{}
			}
			return b.convert(e.Type(), input, e.Conversion())
		}
		b.fail("unsupported conversion in differentiated loss", e.Span())
	case ir.Projection:
		input := b.expression(e.Input(), env)
		if b.err != nil {
			return symbol{}
		}
		if field, ok := input.fields[e.Field()]; ok {
			return field
		}
		return b.emit(must(ir.NewProjection(e.Type(), input.ref, e.Field(), e.Span())), []symbol{input}, nil, false)
	case ir.FlowCall:
		args := make([]symbol, len(e.Arguments()))
		for _, a := range e.Arguments() {
			args[a.Position()] = b.expression(a.Value(), env)
		}
		return b.invoke(b.flows[e.Flow()], args, e.Span())
	case ir.Match:
		b.fail("Match.new is not differentiable", e.Span())
	case ir.BuiltinCall:
		op := e.Operation().Name()
		if !allowed(op) {
			b.fail("unsupported operation in differentiated loss: "+op, e.Span())
			return symbol{}
		}
		args := make([]symbol, len(e.Arguments()))
		checked := make([]ir.Argument, len(args))
		for i, a := range e.Arguments() {
			v, ok := a.(ir.ValueArgument)
			if !ok {
				b.fail("callbacks are not differentiable", a.Span())
				return symbol{}
			}
			args[a.Position()] = b.expression(v.Value(), env)
			if b.err != nil {
				return symbol{}
			}
			checked[i] = must(ir.ValueArg(a.Position(), args[a.Position()].ref))
		}
		edges := []Edge{}
		if base(e.Type()).Kind() == types.TensorKind && op != catalog.AutodiffStop {
			for i, a := range args {
				if a.active && a.ref.Type().Kind() == types.TensorKind {
					edges = append(edges, Edge{i, a.id})
				}
			}
		}
		s := b.emit(must(ir.NewBuiltin(e.Type(), e.Operation(), checked, e.Labels(), e.Span())), args, edges, len(edges) > 0)
		if op == catalog.RecordNew {
			s.fields = map[string]symbol{}
			for i, label := range e.Labels() {
				s.fields[label] = args[i]
			}
		}
		return s
	default:
		panic("unknown checked derivative expression")
	}
	return symbol{}
}
func allowed(op string) bool {
	switch op {
	case catalog.TensorAdd, catalog.TensorSub, catalog.TensorMul, catalog.TensorDiv, catalog.TensorSqrt, catalog.TensorRelu,
		catalog.TensorMatmul, catalog.TensorBroadcast, catalog.TensorReshape, catalog.TensorTranspose, catalog.TensorSum, catalog.TensorMean,
		catalog.TensorShape, catalog.TensorRank, catalog.TensorDtype, catalog.TensorDevice, catalog.AutodiffStop,
		catalog.RecordNew, catalog.ListNew, catalog.IntNew, catalog.MathAdd, catalog.MathSub, catalog.MathMul,
		catalog.MathLT, catalog.MathLTE, catalog.MathGT, catalog.MathGTE, catalog.MathEqual, catalog.MathSum, catalog.MathProduct:
		return true
	}
	return false
}
