package inspect

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type derivativeEdgeData struct {
	Operand int `json:"operand"`
	Node    int `json:"node"`
}
type derivativeTargetData struct {
	Key  string `json:"key"`
	Node int    `json:"node"`
}
type derivativeNodeData struct {
	ID          int                  `json:"id"`
	Binding     ir.BindingID         `json:"binding"`
	Operation   string               `json:"operation,omitempty"`
	Span        source.Span          `json:"span"`
	Invocations []int                `json:"invocations"`
	Operands    []ir.BindingID       `json:"operands"`
	Edges       []derivativeEdgeData `json:"edges"`
	Reverse     bool                 `json:"reverse"`
	Retained    bool                 `json:"retained"`
}
type derivativePlanData struct {
	Result  int                    `json:"result"`
	Targets []derivativeTargetData `json:"targets"`
	Nodes   []derivativeNodeData   `json:"nodes"`
}

func derivativePlan(call autodiff.Call) *derivativePlanData {
	d := &derivativePlanData{Result: call.ResultNode(), Targets: []derivativeTargetData{}, Nodes: []derivativeNodeData{}}
	for _, s := range call.Selections() {
		d.Targets = append(d.Targets, derivativeTargetData{s.Key, s.Node})
	}
	for _, n := range call.Nodes() {
		node := derivativeNodeData{ID: n.ID(), Binding: n.Binding(), Operation: n.Operation(), Span: n.Expression().Span(), Invocations: []int{}, Operands: n.Operands(), Edges: []derivativeEdgeData{}, Reverse: n.Reverse(), Retained: n.Retained()}
		for _, f := range n.Frames() {
			node.Invocations = append(node.Invocations, f.Invocation)
		}
		for _, e := range n.Edges() {
			node.Edges = append(node.Edges, derivativeEdgeData{e.Operand, e.Node})
		}
		d.Nodes = append(d.Nodes, node)
	}
	return d
}
