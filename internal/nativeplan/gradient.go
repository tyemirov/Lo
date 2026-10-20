package nativeplan

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type derivativeFrame struct {
	Invocation int                  `json:"invocation"`
	Name       string               `json:"name"`
	Arguments  []derivativeArgument `json:"arguments"`
}
type derivativeArgument struct {
	Name string `json:"name"`
	Node int    `json:"node"`
}
type derivativeEdge struct {
	Operand int `json:"operand"`
	Node    int `json:"node"`
}
type derivativeSelection struct {
	Key  string `json:"key"`
	Node int    `json:"node"`
}
type derivativeNode struct {
	ID           int               `json:"id"`
	Location     uint64            `json:"location"`
	Operation    string            `json:"operation"`
	Operands     int               `json:"operands"`
	OperandNodes []int             `json:"operand_nodes"`
	Conversion   bool              `json:"conversion"`
	Reverse      bool              `json:"reverse"`
	Frames       []derivativeFrame `json:"frames"`
	Edges        []derivativeEdge  `json:"edges"`
}
type derivativeMetadata struct {
	ID         uint64                `json:"id"`
	Type       uint64                `json:"type"`
	Location   uint64                `json:"location"`
	ResultNode int                   `json:"result_node"`
	Nodes      []derivativeNode      `json:"nodes"`
	Selections []derivativeSelection `json:"selections"`
	Arguments  []uint64              `json:"arguments"`
}

func (b *functionBuilder) derivativeSite(call autodiff.Call, action string, typ types.Type, plan uint64, node int) uint64 {
	site := b.site(call.Span(), action, typ)
	b.l.metadata.Locations[site].DerivativePlan = plan
	b.l.metadata.Locations[site].DerivativeNode = uint64(node)
	return site
}

func (b *functionBuilder) gradient(call autodiff.Call) ValueID {
	arguments := []ValueID{}
	argumentTypes := []types.Type{}
	for _, argument := range call.Arguments() {
		if value, ok := argument.(ir.ValueArgument); ok {
			arguments = append(arguments, b.expression(value.Value()))
			argumentTypes = append(argumentTypes, value.Value().Type())
		}
	}
	plan := uint64(len(b.l.metadata.Derivatives))
	site := b.derivativeSite(call, "derivative_call", call.Type(), plan, 0)
	b.l.metadata.Locations[site].Operation = call.Operation().Name()
	metadata := derivativeMetadata{ID: plan, Type: b.l.typeID(call.Type()), Location: site, ResultNode: call.ResultNode(), Nodes: []derivativeNode{}, Selections: []derivativeSelection{}, Arguments: []uint64{}}
	for _, typ := range argumentTypes {
		metadata.Arguments = append(metadata.Arguments, b.l.typeID(typ))
	}
	for _, selected := range call.Selections() {
		metadata.Selections = append(metadata.Selections, derivativeSelection{selected.Key, selected.Node})
	}
	b.l.metadata.Derivatives = append(b.l.metadata.Derivatives, metadata)
	function := b.l.gradientFunction(call, plan, argumentTypes)
	b.l.synthetic = append(b.l.synthetic, function)
	return b.call(function.Symbol, arguments, call.Span(), call.Type(), site)
}

func (l *lowering) gradientFunction(call autodiff.Call, plan uint64, argumentTypes []types.Type) Function {
	b := &functionBuilder{l: l, bindings: map[ir.BindingID]ValueID{}, bindingTypes: map[ir.BindingID]types.Type{}, resultValues: map[ResultID]ValueID{}}
	b.f = Function{Symbol: fmt.Sprintf("lo_derivative_%d", plan), Name: "<derivative>", Flow: -2, Arity: len(argumentTypes)}
	b.f.Site = b.derivativeSite(call, "body", types.Null(), plan, 0)
	b.block()
	b.finish = b.block()
	b.bodyCleanup = b.block()
	tapeCleanup := b.block()
	b.errorExit = b.finish
	admit := b.block()
	invalid := b.invalid(b.f.Site, b.finish)
	b.end(Terminator{Kind: Arity, Targets: [4]BlockID{admit, invalid}, Site: b.f.Site})
	b.current = admit
	b.checked(ScopeEnterSymbol, nil, b.site(call.Span(), "scope_enter", nil), false, false)
	b.errorExit = b.bodyCleanup
	b.checked("lo_derivative_enter", nil, b.derivativeSite(call, "derivative_enter", types.Null(), plan, 0), false, false)
	b.errorExit = tapeCleanup
	arguments := []ValueID{}
	for index, typ := range argumentTypes {
		id := b.value(false)
		b.instruction(Instruction{Kind: Parameter, Value: id, Index: uint64(index), Site: b.site(call.Span(), "parameter", typ)})
		arguments = append(arguments, id)
	}
	for index, param := range call.Parameters() {
		argument := index + 1
		if index == len(call.Parameters())-1 {
			argument = 0
		}
		b.bindings[param.ID] = arguments[argument]
		b.bindingTypes[param.ID] = param.Type
	}
	seenFrames := map[int]bool{}
	nodes := call.Nodes()
	bindingsToNodes := map[ir.BindingID]int{}
	for _, node := range nodes {
		bindingsToNodes[node.Binding()] = node.ID()
	}
	for _, node := range nodes {
		metadata := derivativeNode{ID: node.ID(), Location: b.site(node.Expression().Span(), "derivative_node", node.Expression().Type()), Operation: node.Operation(), Operands: len(node.Operands()), OperandNodes: []int{}, Reverse: node.Reverse(), Frames: []derivativeFrame{}, Edges: []derivativeEdge{}}
		for _, operand := range node.Operands() {
			metadata.OperandNodes = append(metadata.OperandNodes, bindingsToNodes[operand])
		}
		l.metadata.Locations[metadata.Location].DerivativePlan = plan
		l.metadata.Locations[metadata.Location].DerivativeNode = uint64(node.ID())
		_, metadata.Conversion = node.Expression().(ir.Conversion)
		for _, frame := range node.Frames() {
			value := derivativeFrame{Invocation: frame.Invocation, Name: frame.Name, Arguments: []derivativeArgument{}}
			for _, argument := range frame.Arguments {
				value.Arguments = append(value.Arguments, derivativeArgument{argument.Name, bindingsToNodes[argument.Binding]})
			}
			metadata.Frames = append(metadata.Frames, value)
		}
		for _, edge := range node.Edges() {
			metadata.Edges = append(metadata.Edges, derivativeEdge{edge.Operand, edge.Node})
		}
		l.metadata.Derivatives[plan].Nodes = append(l.metadata.Derivatives[plan].Nodes, metadata)
		for _, frame := range node.Frames() {
			if seenFrames[frame.Invocation] {
				continue
			}
			seenFrames[frame.Invocation] = true
			site := b.derivativeSite(call, "derivative_snapshot", types.Null(), plan, node.ID())
			l.metadata.Locations[site].DerivativeFrame = uint64(frame.Invocation)
			values := []ValueID{}
			for _, argument := range frame.Arguments {
				l.metadata.Locations[site].Labels = append(l.metadata.Locations[site].Labels, argument.Name)
				values = append(values, b.bindings[argument.Binding])
			}
			b.checked("lo_derivative_snapshot", values, site, false, false)
		}
		b.errorExit = b.gradientFailure(call, plan, node.ID(), tapeCleanup)
		firstTemporary := len(b.f.Values) + 1
		value := b.expression(node.Expression())
		temporaries := []ValueID{}
		for id := firstTemporary; id <= len(b.f.Values); id++ {
			if ValueID(id) != value && b.f.Values[id-1] {
				temporaries = append(temporaries, ValueID(id))
			}
		}
		if len(temporaries) > 0 {
			b.instruction(Instruction{Kind: Release, Inputs: temporaries, Site: b.f.Site})
		}
		b.bindings[node.Binding()] = value
		b.bindingTypes[node.Binding()] = node.Expression().Type()
		b.errorExit = tapeCleanup
		if node.Retained() {
			b.checked("lo_derivative_retain", []ValueID{value}, b.derivativeSite(call, "derivative_retain", types.Null(), plan, node.ID()), false, false)
		}
		for _, binding := range node.Release() {
			b.instruction(Instruction{Kind: Release, Inputs: []ValueID{b.bindings[binding]}, Site: b.f.Site})
			delete(b.bindings, binding)
			delete(b.bindingTypes, binding)
		}
	}
	loss := b.bindings[call.Result()]
	b.checked("lo_derivative_loss", append([]ValueID{loss}, arguments...), b.derivativeSite(call, "derivative_loss", types.Tensor(), plan, 0), true, false)
	for _, selected := range call.Selections() {
		node := nodes[selected.Node-1]
		b.errorExit = b.gradientFailure(call, plan, node.ID(), tapeCleanup)
		b.checked("lo_derivative_zero", []ValueID{b.bindings[node.Binding()]}, b.derivativeSite(call, "derivative_zero", types.Null(), plan, node.ID()), true, false)
	}
	b.errorExit = b.gradientFailure(call, plan, call.ResultNode(), tapeCleanup)
	b.checked("lo_derivative_seed", []ValueID{loss}, b.derivativeSite(call, "derivative_seed", types.Null(), plan, call.ResultNode()), true, false)
	for index := len(nodes) - 1; index >= 0; index-- {
		node := nodes[index]
		if !node.Reverse() || len(node.Edges()) == 0 {
			continue
		}
		values := []ValueID{}
		if _, conversion := node.Expression().(ir.Conversion); !conversion {
			for _, operand := range node.Operands() {
				values = append(values, b.bindings[operand])
			}
			if node.Operation() == "Tensor.sqrt" {
				values = append(values, b.bindings[node.Binding()])
			}
		}
		b.errorExit = b.gradientFailure(call, plan, node.ID(), tapeCleanup)
		b.checked("lo_derivative_reverse", values, b.derivativeSite(call, "derivative_reverse", types.Null(), plan, node.ID()), true, false)
	}
	b.errorExit = tapeCleanup
	result, _ := b.raw("lo_derivative_finish", []ValueID{loss}, b.derivativeSite(call, "derivative_finish", call.Type(), plan, 0), false)
	copy := b.failure(result, tapeCleanup)
	unknown := b.invalid(b.f.Site, tapeCleanup)
	b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{copy, copy, copy, unknown}, Site: b.f.Site})
	b.current = tapeCleanup
	b.instruction(Instruction{Kind: TapeLeave, Site: b.f.Site})
	b.jump(b.bodyCleanup)
	b.current = b.bodyCleanup
	b.instruction(Instruction{Kind: ScopeLeave, Site: b.f.Site})
	b.instruction(Instruction{Kind: ReleaseAll, Site: b.f.Site})
	b.jump(b.finish)
	b.current = b.finish
	b.instruction(Instruction{Kind: ReleaseAll, Site: b.f.Site})
	b.end(Terminator{Kind: Return, Site: b.f.Site})
	return b.f
}

func (b *functionBuilder) gradientFailure(call autodiff.Call, plan uint64, node int, cleanup BlockID) BlockID {
	saved := b.current
	failed := b.block()
	framing := b.block()
	hostFailure := b.block()
	unknown := b.invalid(b.f.Site, cleanup)
	b.current = failed
	b.end(Terminator{Kind: Status, Targets: [4]BlockID{cleanup, framing, hostFailure, unknown}, Site: b.f.Site})
	b.current = hostFailure
	hostResult, _ := b.raw("lo_derivative_host_failure", nil, b.derivativeSite(call, "derivative_host_failure", types.Null(), plan, node), false)
	hostCopy := b.failure(hostResult, cleanup)
	b.end(Terminator{Kind: Status, Result: hostResult, Targets: [4]BlockID{unknown, unknown, hostCopy, unknown}, Site: b.f.Site})
	b.current = framing
	cause := b.value(true)
	b.instruction(Instruction{Kind: OutputValue, Value: cause, Site: b.f.Site})
	result, _ := b.raw("lo_derivative_failure", []ValueID{cause}, b.derivativeSite(call, "derivative_failure", call.Type(), plan, node), false)
	copy := b.failure(result, cleanup)
	invalid := b.invalid(b.f.Site, cleanup)
	b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{invalid, copy, copy, invalid}, Site: b.f.Site})
	b.current = saved
	return failed
}
