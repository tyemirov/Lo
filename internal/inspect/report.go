package inspect

import (
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensorfacts"
)

type nodeID string
type contextID string
type targetID string

type Report interface {
	json.Marshaler
	Text() string
	report()
}
type report struct{ data reportData }

func (*report) report()                        {}
func (r *report) MarshalJSON() ([]byte, error) { return json.Marshal(r.data) }

type reportData struct {
	DeviceResources []deviceResourceData `json:"device_resources"`
	Transfers       []deviceResourceData `json:"transfers"`
	File            string               `json:"file"`
	ResultType      string               `json:"result_type,omitempty"`
	Nodes           []nodeData           `json:"nodes"`
	Bindings        []bindingData        `json:"bindings"`
	Flows           []flowData           `json:"flows"`
	Contexts        []contextData        `json:"contexts"`
	EntryPoints     []entryData          `json:"entry_points"`
	Reads           []resourceData       `json:"reads"`
	Targets         []resourceData       `json:"targets"`
	Activations     []activationData     `json:"activations"`
	ErrorSources    []errorData          `json:"error_sources"`
	Capabilities    []resourceData       `json:"capabilities"`
	UnusedFlows     []identity.FlowID    `json:"unused_flows"`
}
type nodeData struct {
	Derivative *derivativePlanData   `json:"derivative,omitempty"`
	Tensor     *tensorfacts.Metadata `json:"tensor,omitempty"`
	ID         nodeID                `json:"id"`
	Kind       string                `json:"kind"`
	Type       string                `json:"type,omitempty"`
	Span       source.Span           `json:"span"`
	Region     string                `json:"region"`
	Inputs     []nodeID              `json:"inputs"`
	Operation  string                `json:"operation,omitempty"`
	Flow       identity.FlowID       `json:"flow,omitempty"`
	Binding    ir.BindingID          `json:"binding,omitempty"`
	Value      *string               `json:"value,omitempty"`
	Field      string                `json:"field,omitempty"`
	Arguments  []argumentData        `json:"arguments,omitempty"`
	Cases      []caseData            `json:"cases,omitempty"`
}
type argumentData struct {
	Position int             `json:"position"`
	Node     nodeID          `json:"node,omitempty"`
	Flow     identity.FlowID `json:"flow,omitempty"`
}
type caseData struct {
	Kind string          `json:"kind"`
	Head nodeID          `json:"head,omitempty"`
	Flow identity.FlowID `json:"flow,omitempty"`
	Body nodeID          `json:"body"`
}
type bindingData struct {
	ID     ir.BindingID `json:"id"`
	Name   string       `json:"name"`
	Node   nodeID       `json:"node"`
	Region string       `json:"region"`
	Span   source.Span  `json:"span"`
}
type parameterData struct {
	ID   ir.BindingID `json:"id"`
	Name string       `json:"name"`
	Type string       `json:"type"`
}
type effectsData struct {
	Devices bool `json:"devices"`
	Reads   bool `json:"reads"`
	Commits bool `json:"commits"`
}
type flowData struct {
	ID         identity.FlowID `json:"id"`
	Name       string          `json:"name"`
	Span       source.Span     `json:"span"`
	Parameters []parameterData `json:"parameters"`
	Output     string          `json:"output"`
	Handler    identity.FlowID `json:"handler,omitempty"`
	Effects    effectsData     `json:"effects"`
	Uses       []string        `json:"uses"`
}
type contextData struct {
	ID     contextID       `json:"id"`
	Entry  string          `json:"entry,omitempty"`
	Parent contextID       `json:"parent,omitempty"`
	Flow   identity.FlowID `json:"flow,omitempty"`
	Node   nodeID          `json:"node,omitempty"`
	Kind   string          `json:"kind"`
}
type entryData struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Context    contextID       `json:"context"`
	Flow       identity.FlowID `json:"flow,omitempty"`
	Activation string          `json:"activation,omitempty"`
}
type condition struct {
	Context contextID `json:"context"`
	Kind    string    `json:"kind"`
	Node    nodeID    `json:"node"`
	Case    *int      `json:"case,omitempty"`
}
type description struct {
	Kind         string      `json:"kind"`
	Type         string      `json:"type"`
	Node         nodeID      `json:"node"`
	Span         source.Span `json:"span"`
	Dependencies []nodeID    `json:"dependencies"`
	Value        *string     `json:"value,omitempty"`
	Expression   string      `json:"expression,omitempty"`
}
type resourceData struct {
	ID         string          `json:"id"`
	Origin     string          `json:"origin,omitempty"`
	Operation  string          `json:"operation"`
	Context    contextID       `json:"context"`
	Entry      string          `json:"entry,omitempty"`
	Node       nodeID          `json:"node"`
	Target     description     `json:"target"`
	Conditions []condition     `json:"conditions"`
	Handler    identity.FlowID `json:"handler,omitempty"`
	OnError    identity.FlowID `json:"on_error,omitempty"`
}
type targetReference struct {
	ID         targetID    `json:"id"`
	Conditions []condition `json:"conditions"`
}
type activationData struct {
	ID         string            `json:"id"`
	Context    contextID         `json:"context"`
	Entry      string            `json:"entry,omitempty"`
	Node       nodeID            `json:"node"`
	Conditions []condition       `json:"conditions"`
	Targets    []targetReference `json:"targets"`
}
type errorData struct {
	ID         string          `json:"id"`
	Category   string          `json:"category"`
	Operation  string          `json:"operation"`
	Context    contextID       `json:"context"`
	Entry      string          `json:"entry,omitempty"`
	Node       nodeID          `json:"node"`
	Kinds      []string        `json:"kinds"`
	Dynamic    *description    `json:"dynamic,omitempty"`
	Handler    identity.FlowID `json:"handler,omitempty"`
	Conditions []condition     `json:"conditions"`
}

func newReport(file string) reportData {
	return reportData{DeviceResources: []deviceResourceData{}, Transfers: []deviceResourceData{}, File: file, Nodes: []nodeData{}, Bindings: []bindingData{}, Flows: []flowData{}, Contexts: []contextData{}, EntryPoints: []entryData{}, Reads: []resourceData{}, Targets: []resourceData{}, Activations: []activationData{}, ErrorSources: []errorData{}, Capabilities: []resourceData{}, UnusedFlows: []identity.FlowID{}}
}
func (r *report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "file: %s\n", r.data.File)
	if r.data.ResultType != "" {
		fmt.Fprintf(&b, "result: %s\n", r.data.ResultType)
	}
	b.WriteString("entry points:\n")
	for _, e := range r.data.EntryPoints {
		fmt.Fprintf(&b, "  %s %s context=%s flow=%s activation=%s\n", e.ID, e.Kind, e.Context, e.Flow, e.Activation)
	}
	b.WriteString("flows:\n")
	for _, f := range r.data.Flows {
		parameters := make([]string, len(f.Parameters))
		for i, p := range f.Parameters {
			parameters[i] = p.Name + ": " + p.Type
		}
		fmt.Fprintf(&b, "  %s %s(%s) -> %s uses=%s reads=%t commits=%t devices=%t handler=%s at %s\n", f.ID, f.Name, strings.Join(parameters, ", "), f.Output, strings.Join(f.Uses, ","), f.Effects.Reads, f.Effects.Commits, f.Effects.Devices, f.Handler, location(f.Span))
	}
	b.WriteString("contexts:\n")
	for _, c := range r.data.Contexts {
		fmt.Fprintf(&b, "  %s %s entry=%s parent=%s flow=%s node=%s\n", c.ID, c.Kind, c.Entry, c.Parent, c.Flow, c.Node)
	}
	resources := func(name string, rs []resourceData) {
		fmt.Fprintf(&b, "%s:\n", name)
		for _, r := range rs {
			target := fmt.Sprintf("%q", r.Target.Expression)
			if r.Target.Value != nil {
				target = fmt.Sprintf("%q", *r.Target.Value)
			}
			fmt.Fprintf(&b, "  %s %s %s %s context=%s node=%s when=%s", r.ID, r.Operation, r.Target.Kind, target, r.Context, r.Node, conditionsText(r.Conditions))
			if r.Origin != "" {
				fmt.Fprintf(&b, " origin=%s", r.Origin)
			}
			if r.Handler != "" {
				fmt.Fprintf(&b, " handler=%s onError=%s", r.Handler, r.OnError)
			}
			fmt.Fprintf(&b, " at %s\n", location(r.Target.Span))
		}
	}
	for _, site := range r.data.DeviceResources {
		resources("device resource", []resourceData{site.resourceData})
		source, size := "unknown", "unknown"
		if site.SourceDevice != nil {
			source = *site.SourceDevice
		}
		if site.StorageBytes != nil {
			size = fmt.Sprint(*site.StorageBytes)
		}
		fmt.Fprintf(&b, "    source_device=%s storage_bytes=%s\n", source, size)
	}
	resources("reads", r.data.Reads)
	resources("targets", r.data.Targets)
	b.WriteString("activations:\n")
	for _, a := range r.data.Activations {
		fmt.Fprintf(&b, "  %s context=%s node=%s when=%s\n", a.ID, a.Context, a.Node, conditionsText(a.Conditions))
		for _, target := range a.Targets {
			fmt.Fprintf(&b, "    target=%s when=%s\n", target.ID, conditionsText(target.Conditions))
		}
	}
	b.WriteString("errors:\n")
	for _, e := range r.data.ErrorSources {
		kinds := strings.Join(e.Kinds, ",")
		if e.Dynamic != nil {
			kinds = fmt.Sprintf("dynamic %q", e.Dynamic.Expression)
		}
		fmt.Fprintf(&b, "  %s %s %s kinds=%s context=%s node=%s handler=%s when=%s\n", e.ID, e.Category, e.Operation, kinds, e.Context, e.Node, e.Handler, conditionsText(e.Conditions))
	}
	resources("capabilities", r.data.Capabilities)
	b.WriteString("bindings:\n")
	for _, v := range r.data.Bindings {
		fmt.Fprintf(&b, "  %s %s node=%s region=%s at %s\n", v.ID, v.Name, v.Node, v.Region, location(v.Span))
	}
	b.WriteString("nodes:\n")
	for _, n := range r.data.Nodes {
		fmt.Fprintf(&b, "  %s %s %s operation=%s flow=%s binding=%s inputs=%v region=%s at %s\n", n.ID, n.Kind, n.Type, n.Operation, n.Flow, n.Binding, n.Inputs, n.Region, location(n.Span))
		if n.Tensor != nil {
			shape, size := "unknown", "unknown"
			if n.Tensor.Shape != nil {
				shape = "[" + strings.Join(n.Tensor.Shape, ",") + "]"
			}
			if n.Tensor.StorageBytes != nil {
				size = fmt.Sprint(*n.Tensor.StorageBytes)
			}
			fmt.Fprintf(&b, "    tensor dtype=%s profile=%s device=%s layout=%s shape=%s storage_bytes=%s\n", n.Tensor.Dtype, n.Tensor.Profile, n.Tensor.Device, n.Tensor.Layout, shape, size)
		}
		if n.Derivative != nil {
			fmt.Fprintf(&b, "    derivative result=%d targets=%v\n", n.Derivative.Result, n.Derivative.Targets)
			for _, d := range n.Derivative.Nodes {
				fmt.Fprintf(&b, "      %d %s invocations=%v reverse=%t retained=%t edges=%v at %s\n", d.ID, d.Operation, d.Invocations, d.Reverse, d.Retained, d.Edges, location(d.Span))
			}
		}
	}
	fmt.Fprintf(&b, "unused flows: %v\n", r.data.UnusedFlows)
	return b.String()
}
func location(span source.Span) string {
	return fmt.Sprintf("%s:%d:%d", span.File(), span.Start().Line, span.Start().Column)
}
func conditionsText(conditions []condition) string {
	if len(conditions) == 0 {
		return "direct"
	}
	parts := make([]string, len(conditions))
	for i, c := range conditions {
		parts[i] = c.Kind + "(" + string(c.Context) + ":" + string(c.Node)
		if c.Case != nil {
			parts[i] += fmt.Sprintf(",%d", *c.Case)
		}
		parts[i] += ")"
	}
	return strings.Join(parts, " & ")
}

type deviceResourceData struct {
	resourceData
	SourceDevice *string `json:"source_device"`
	StorageBytes *uint64 `json:"storage_bytes"`
}
