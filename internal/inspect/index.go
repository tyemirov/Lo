package inspect

import (
	"fmt"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensorfacts"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type graphIndex struct {
	metadata   map[ir.Expression]*tensorfacts.Metadata
	report     *reportData
	nodes      map[ir.Expression]nodeID
	nodeData   map[nodeID]nodeData
	parameters map[ir.BindingID]nodeID
	statements map[int]nodeID
	flows      map[ir.FlowID]ir.Flow
	order      []ir.Flow
}

func index(program ir.Program, data *reportData, metadata map[ir.Expression]*tensorfacts.Metadata) *graphIndex {
	g := &graphIndex{report: data, nodes: map[ir.Expression]nodeID{}, nodeData: map[nodeID]nodeData{}, parameters: map[ir.BindingID]nodeID{}, statements: map[int]nodeID{}, flows: map[ir.FlowID]ir.Flow{}}
	g.order = program.Flows()
	g.metadata = metadata
	slices.SortFunc(g.order, func(a, b ir.Flow) int { return a.Span().Start().Offset - b.Span().Start().Offset })
	for _, f := range g.order {
		g.flows[f.ID()] = f
		parameters := []parameterData{}
		for _, p := range f.Parameters() {
			id := g.add(nodeData{Kind: "parameter", Type: p.Type.String(), Span: p.Span, Region: string(f.ID()), Binding: p.ID, Inputs: []nodeID{}})
			g.parameters[p.ID] = id
			parameters = append(parameters, parameterData{p.ID, p.Name, p.Type.String()})
		}
		data.Flows = append(data.Flows, flowData{ID: f.ID(), Name: f.Name(), Span: f.Span(), Parameters: parameters, Output: f.Signature().Output().String(), Handler: f.Handler(), Effects: effectsData{f.Effects().Reads, f.Effects().Commits}, Uses: []string{}})
		g.body(f.Body(), string(f.ID()))
	}
	g.body(program.Body(), programEntry)
	return g
}
func (g *graphIndex) add(n nodeData) nodeID {
	if n.Tensor == nil && (n.Type == types.Tensor().String() || n.Type == types.Tensor().String()+"!") {
		n.Tensor = tensorfacts.Unknown()
	}
	n.ID = nodeID(fmt.Sprintf("n%d", len(g.report.Nodes)+1))
	g.report.Nodes = append(g.report.Nodes, n)
	g.nodeData[n.ID] = n
	return n.ID
}
func (g *graphIndex) body(statements []ir.Statement, region string) {
	for _, statement := range statements {
		switch s := statement.(type) {
		case ir.Binding:
			id := g.expression(s.Value(), region)
			g.report.Bindings = append(g.report.Bindings, bindingData{s.ID(), s.Name(), id, region, s.Span()})
		case ir.Commit:
			input := g.expression(s.Target(), region)
			id := g.add(nodeData{Kind: "commit", Type: s.Target().Type().String(), Span: s.Span(), Region: region, Inputs: []nodeID{input}})
			g.statements[s.Span().Start().Offset] = id
		case ir.Return:
			input := g.expression(s.Value(), region)
			id := g.add(nodeData{Kind: "return", Type: s.Value().Type().String(), Span: s.Span(), Region: region, Inputs: []nodeID{input}})
			g.statements[s.Span().Start().Offset] = id
		default:
			panic("unknown checked statement")
		}
	}
}
func (g *graphIndex) expression(expr ir.Expression, region string) nodeID {
	if id, exists := g.nodes[expr]; exists {
		return id
	}
	n := nodeData{Type: expr.Type().String(), Span: expr.Span(), Region: region, Inputs: []nodeID{}}
	n.Tensor = g.metadata[expr]
	input := func(value ir.Expression) nodeID {
		id := g.expression(value, region)
		n.Inputs = append(n.Inputs, id)
		return id
	}
	switch e := expr.(type) {
	case ir.Literal:
		n.Kind = "literal"
		v := e.Text()
		n.Value = &v
	case ir.Reference:
		n.Kind = "reference"
		n.Binding = e.Binding()
	case ir.Projection:
		n.Kind = "projection"
		n.Field = e.Field()
		input(e.Input())
	case ir.Conversion:
		n.Kind = conversionName(e.Conversion())
		input(e.Input())
	case ir.FlowCall:
		n.Kind = "flow-call"
		n.Flow = e.Flow()
		for _, arg := range e.Arguments() {
			id := input(arg.Value())
			n.Arguments = append(n.Arguments, argumentData{Position: arg.Position(), Node: id})
		}
	case ir.BuiltinCall:
		n.Kind = "builtin"
		n.Operation = e.Operation().Name()
		for _, arg := range e.Arguments() {
			switch a := arg.(type) {
			case ir.ValueArgument:
				id := input(a.Value())
				n.Arguments = append(n.Arguments, argumentData{Position: a.Position(), Node: id})
			case ir.FlowArgument:
				n.Arguments = append(n.Arguments, argumentData{Position: a.Position(), Flow: a.Flow()})
			default:
				panic("unknown checked argument")
			}
		}
	case ir.Match:
		n.Kind = "match"
		input(e.Selector())
		for i, arm := range e.Cases() {
			armRegion := fmt.Sprintf("%s/match:%d/case:%d", region, e.Span().Start().Offset, i)
			a := caseData{Body: g.expression(arm.Body(), armRegion)}
			switch c := arm.(type) {
			case ir.ExactCase:
				a.Kind = "exact"
				a.Head = input(c.Head())
			case ir.PredicateCase:
				a.Kind = "predicate"
				a.Flow = c.Flow()
			case ir.DefaultCase:
				a.Kind = "default"
			}
			n.Cases = append(n.Cases, a)
			n.Inputs = append(n.Inputs, a.Body)
		}
	default:
		panic("unknown checked expression")
	}
	id := g.add(n)
	g.nodes[expr] = id
	return id
}
func conversionName(kind ir.ConversionKind) string {
	switch kind {
	case ir.Success:
		return "success"
	case ir.Failure:
		return "failure"
	case ir.Propagate:
		return "propagate"
	case ir.RecordView:
		return "record-view"
	case ir.ScalarListView:
		return "scalar-list-view"
	case ir.FallibleRecordView:
		return "fallible-record-view"
	}
	panic("unknown graph conversion")
}
