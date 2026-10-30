package nativeplan

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type typeMetadata struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type parameterMetadata struct {
	Name string `json:"name"`
	Type uint64 `json:"type"`
}
type flowMetadata struct {
	Identity   string              `json:"identity"`
	ID         uint64              `json:"id"`
	Name       string              `json:"name"`
	Parameters []parameterMetadata `json:"parameters"`
	Output     uint64              `json:"output"`
	Handler    *uint64             `json:"handler"`
	Location   uint64              `json:"location"`
}
type programMetadata struct {
	ABI         string               `json:"abi"`
	HasResult   bool                 `json:"has_result"`
	Source      sourceMetadata       `json:"source"`
	Types       []typeMetadata       `json:"types"`
	Locations   []Location           `json:"locations"`
	Flows       []flowMetadata       `json:"flows"`
	Derivatives []derivativeMetadata `json:"derivatives"`
}
type lowering struct {
	ctx                context.Context
	program            ir.Program
	metadata           programMetadata
	typeIDs            map[string]uint64
	flowIDs            map[identity.FlowID]uint64
	flows              map[identity.FlowID]ir.Flow
	metadataLowerBound int
	files              []string
	spans              []metadataSpan
	siteSpans          []uint64
	fileIDs            map[string]uint64
	spanIDs            map[metadataSpanKey]uint64
	err                error
	synthetic          []Function
}
type functionBuilder struct {
	l             *lowering
	f             Function
	current       BlockID
	bindings      map[ir.BindingID]ValueID
	bindingTypes  map[ir.BindingID]types.Type
	parameters    []ValueID
	bodyCleanup   BlockID
	leaveFlow     BlockID
	finish        BlockID
	errorExit     BlockID
	resultValues  map[ResultID]ValueID
	invalidBlocks map[BlockID]BlockID
}

// Lower verifies the checked graph and produces an immutable native plan.
func Lower(ctx context.Context, program ir.Program) (Plan, error) {
	if ctx == nil || program == nil {
		return nil, fmt.Errorf("native lowering requires a context and checked program")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("native lowering: %w", err)
	}
	l := &lowering{ctx: ctx, program: program, typeIDs: map[string]uint64{}, flowIDs: map[identity.FlowID]uint64{}, flows: map[identity.FlowID]ir.Flow{}, files: []string{}, spans: []metadataSpan{}, fileIDs: map[string]uint64{}, spanIDs: map[metadataSpanKey]uint64{}}
	l.metadata.ABI = ABIIdentity
	l.metadata.HasResult = program.ResultType() != nil
	l.metadata.Source.Path = program.Source().Path()
	l.metadata.Source.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(program.Source().Text())))
	l.metadata.Types = []typeMetadata{}
	l.metadata.Locations = []Location{}
	l.metadata.Flows = []flowMetadata{}
	l.metadata.Derivatives = []derivativeMetadata{}
	for i, flow := range program.Flows() {
		if flow == nil {
			return nil, fmt.Errorf("native lowering: absent flow")
		}
		if _, exists := l.flows[flow.ID()]; exists {
			return nil, fmt.Errorf("native lowering: repeated flow %s", flow.ID())
		}
		l.flowIDs[flow.ID()] = uint64(i)
		l.flows[flow.ID()] = flow
	}
	for i, flow := range program.Flows() {
		meta := flowMetadata{Identity: string(flow.ID()), ID: uint64(i), Name: flow.Name(), Parameters: []parameterMetadata{}, Output: l.typeID(flow.Signature().Output())}
		for _, param := range flow.Parameters() {
			meta.Parameters = append(meta.Parameters, parameterMetadata{param.Name, l.typeID(param.Type)})
		}
		if flow.Handler() != "" {
			handler, ok := l.flows[flow.Handler()]
			if !ok {
				return nil, fmt.Errorf("native lowering: missing handler %s", flow.Handler())
			}
			if len(handler.Parameters()) != 1 || handler.Parameters()[0].Type.Kind() != types.ErrorKind || !types.Equal(handler.Signature().Output(), flow.Signature().Output()) {
				return nil, fmt.Errorf("native lowering: inconsistent handler for %s", flow.Name())
			}
			id := l.flowIDs[flow.Handler()]
			meta.Handler = &id
		}
		meta.Location = l.site(flow.Span(), "flow", flow.Signature().Output(), flow.Name())
		l.metadata.Flows = append(l.metadata.Flows, meta)
	}
	module := Module{SourcePath: program.Source().Path()}
	span := program.Source().Span(0, len(program.Source().Text()))
	module.Functions = append(module.Functions, l.function(nil, program.Body(), span))
	for _, flow := range program.Flows() {
		if l.err != nil {
			break
		}
		module.Functions = append(module.Functions, l.function(flow, flow.Body(), flow.Span()))
	}
	module.Functions = append(module.Functions, l.synthetic...)
	if l.err != nil {
		return nil, l.err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("native lowering: %w", err)
	}
	metadata, err := l.encodeMetadata()
	if err != nil {
		return nil, fmt.Errorf("native metadata: %w", err)
	}
	if len(metadata) > MaxModuleBytes {
		return nil, fmt.Errorf("%w: metadata exceeds %d bytes", ErrLimit, MaxModuleBytes)
	}
	module.Metadata = metadata
	module.Locations = append([]Location(nil), l.metadata.Locations...)
	if err := verify(ctx, module); err != nil {
		return nil, fmt.Errorf("verify native plan: %w", err)
	}
	return &verifiedPlan{module}, nil
}

func (l *lowering) typeID(t types.Type) uint64 {
	if t == nil {
		t = types.Null()
	}
	if id, ok := l.typeIDs[t.String()]; ok {
		return id
	}
	id := uint64(len(l.metadata.Types))
	l.typeIDs[t.String()] = id
	l.metadata.Types = append(l.metadata.Types, typeMetadata{id, t.String(), string(t.Kind())})
	return id
}
func (l *lowering) site(span source.Span, action string, t types.Type, owner string) uint64 {
	if l.err == nil {
		if err := l.ctx.Err(); err != nil {
			l.err = fmt.Errorf("native lowering: %w", err)
		}
	}
	if span == nil {
		l.err = fmt.Errorf("native lowering: missing source location")
		return 0
	}
	id := uint64(len(l.metadata.Locations))
	l.siteSpans = append(l.siteSpans, l.internSpan(span))
	l.metadata.Locations = append(l.metadata.Locations, Location{ID: id, File: span.File(), Start: span.Start(), End: span.End(), Action: action, Type: l.typeID(t), Labels: []string{}, Owner: owner, Arguments: []ArgumentMetadata{}})
	l.accountMetadata(len(`{"span":0,"action":"","type":0,"owner":"","flow":0,"payload":{}}`))
	l.accountMetadata(len(action))
	l.accountMetadata(len(owner))
	return id
}
func (b *functionBuilder) site(span source.Span, action string, t types.Type) uint64 {
	id := b.l.site(span, action, t, b.f.Name)
	if b.f.Flow >= 0 {
		b.l.metadata.Locations[id].Flow = uint64(b.f.Flow)
	}
	return id
}
func (b *functionBuilder) block() BlockID {
	id := BlockID(len(b.f.Blocks))
	b.f.Blocks = append(b.f.Blocks, Block{ID: id})
	return id
}
func (b *functionBuilder) instruction(i Instruction) {
	b.f.Blocks[b.current].Instructions = append(b.f.Blocks[b.current].Instructions, i)
}
func (b *functionBuilder) end(t Terminator) { b.f.Blocks[b.current].End = t }
func (b *functionBuilder) jump(target BlockID) {
	b.end(Terminator{Kind: Jump, Targets: [4]BlockID{target}, Site: b.f.Site})
}
func (b *functionBuilder) value(owned bool) ValueID {
	b.f.Values = append(b.f.Values, owned)
	return ValueID(len(b.f.Values))
}
func (b *functionBuilder) raw(symbol string, args []ValueID, site uint64, caller bool) (ResultID, ValueID) {
	b.f.Results++
	result := ResultID(b.f.Results)
	value := b.value(true)
	b.resultValues[result] = value
	b.instruction(Instruction{Kind: Call, Symbol: symbol, Inputs: args, Site: site, Result: result, Value: value, CallerLocation: caller})
	return result, value
}
func (b *functionBuilder) failure(result ResultID, exit BlockID) BlockID {
	saved := b.current
	target := b.block()
	payload, host := b.block(), b.block()
	b.current = target
	unknown := b.invalid(b.f.Site, exit)
	b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{payload, payload, host, unknown}, Site: b.f.Site})
	b.current = payload
	b.adopt(result, b.f.Site)
	b.instruction(Instruction{Kind: CopyResult, Result: result, Site: b.f.Site, Payload: true})
	b.jump(exit)
	b.current = host
	b.instruction(Instruction{Kind: CopyResult, Result: result, Site: b.f.Site})
	b.jump(exit)
	b.current = saved
	return target
}
func (b *functionBuilder) adopt(result ResultID, site uint64) {
	b.instruction(Instruction{Kind: AdoptResult, Result: result, Value: b.resultValues[result], Site: site})
}
func (b *functionBuilder) invalid(site uint64, exit BlockID) BlockID {
	if existing, ok := b.invalidBlocks[exit]; ok {
		return existing
	}
	saved := b.current
	target := b.block()
	if b.invalidBlocks == nil {
		b.invalidBlocks = map[BlockID]BlockID{}
	}
	b.invalidBlocks[exit] = target
	b.current = target
	result, _ := b.raw(InvalidStatusSymbol, nil, site, false)
	b.instruction(Instruction{Kind: CopyResult, Result: result, Site: site})
	b.jump(exit)
	b.current = saved
	return target
}
func (b *functionBuilder) checked(symbol string, args []ValueID, site uint64, recoverable bool, caller bool) ValueID {
	result, value := b.raw(symbol, args, site, caller)
	return b.checkedResult(result, value, site, recoverable)
}
func (b *functionBuilder) checkedResult(result ResultID, value ValueID, site uint64, recoverable bool) ValueID {
	ok := b.block()
	failure := b.failure(result, b.errorExit)
	unknown := b.invalid(site, b.errorExit)
	recover := unknown
	if recoverable {
		recover = failure
	}
	b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{ok, recover, failure, unknown}, Site: site})
	b.current = ok
	b.adopt(result, site)
	return value
}
func (b *functionBuilder) call(symbol string, args []ValueID, span source.Span, t types.Type, site uint64) ValueID {
	if t.Kind() != types.FallibleKind {
		return b.checked(symbol, args, site, false, false)
	}
	result, value := b.raw(symbol, args, site, false)
	success, errorArm, join := b.block(), b.block(), b.block()
	failure := b.failure(result, b.errorExit)
	unknown := b.invalid(site, b.errorExit)
	b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{success, errorArm, failure, unknown}, Site: site})
	b.current = success
	b.adopt(result, site)
	good := b.checked("lo_convert_success", []ValueID{value}, b.site(span, "convert_success", t), false, false)
	goodBlock := b.current
	b.jump(join)
	b.current = errorArm
	b.adopt(result, site)
	bad := b.checked("lo_convert_failure", []ValueID{value}, b.site(span, "convert_failure", t), false, false)
	badBlock := b.current
	b.jump(join)
	b.current = join
	output := b.value(true)
	b.instruction(Instruction{Kind: Phi, Value: output, Inputs: []ValueID{good, bad}, Predecessors: []BlockID{goodBlock, badBlock}, Site: site})
	return output
}
func (l *lowering) function(flow ir.Flow, body []ir.Statement, span source.Span) Function {
	b := &functionBuilder{l: l, bindings: map[ir.BindingID]ValueID{}, bindingTypes: map[ir.BindingID]types.Type{}, resultValues: map[ResultID]ValueID{}}
	b.f = Function{Symbol: "lo_entry", Name: "<top>", Flow: -1}
	if flow != nil {
		b.f.Flow = int(l.flowIDs[flow.ID()])
		b.f.Symbol = fmt.Sprintf("lo_flow_%d", b.f.Flow)
		b.f.Name = flow.Name()
		b.f.Arity = len(flow.Parameters())
	}
	b.f.Site = b.site(span, "body", nil)
	b.block()
	b.finish = b.block()
	b.leaveFlow = b.block()
	b.bodyCleanup = b.block()
	b.errorExit = b.finish
	admit := b.block()
	invalid := b.invalid(b.f.Site, b.finish)
	b.end(Terminator{Kind: Arity, Targets: [4]BlockID{admit, invalid}, Site: b.f.Site})
	b.current = admit
	if flow != nil {
		b.checked(FlowEnterSymbol, nil, b.site(span, "flow_enter", nil), false, true)
		b.errorExit = b.leaveFlow
	}
	b.checked(ScopeEnterSymbol, nil, b.site(span, "scope_enter", nil), false, false)
	b.errorExit = b.bodyCleanup
	if flow != nil {
		for index, param := range flow.Parameters() {
			id := b.value(false)
			b.instruction(Instruction{Kind: Parameter, Value: id, Index: uint64(index), Site: b.site(param.Span, "parameter", param.Type)})
			b.parameters = append(b.parameters, id)
			b.bindings[param.ID] = id
			b.bindingTypes[param.ID] = param.Type
		}
	}
	returned := false
	for _, statement := range body {
		if l.err != nil {
			break
		}
		b.checked(StepSymbol, nil, b.site(statement.Span(), "statement_step", nil), false, false)
		switch statement := statement.(type) {
		case ir.Binding:
			if _, exists := b.bindings[statement.ID()]; exists {
				l.err = fmt.Errorf("native lowering: repeated binding %s", statement.ID())
				break
			}
			value := b.expression(statement.Value())
			b.bindings[statement.ID()] = value
			b.bindingTypes[statement.ID()] = statement.Value().Type()
		case ir.Commit:
			value := b.expression(statement.Target())
			receipt := b.checked(CommitSymbol, []ValueID{value}, b.site(statement.Span(), "commit", statement.ResultType()), true, false)
			if statement.ID() != "" {
				if _, exists := b.bindings[statement.ID()]; exists {
					l.err = fmt.Errorf("native lowering: repeated commit binding")
				}
				b.bindings[statement.ID()] = receipt
				b.bindingTypes[statement.ID()] = statement.ResultType()
			}
		case ir.Return:
			value := b.expression(statement.Value())
			site := b.site(statement.Span(), "cause", statement.Value().Type())
			symbol := CauseSymbol
			if flow != nil && flow.Signature().Output().Kind() == types.FallibleKind {
				symbol = "lo_convert_propagate"
			}
			result, _ := b.raw(symbol, []ValueID{value}, site, false)
			copy := b.failure(result, b.bodyCleanup)
			unknown := b.invalid(site, b.bodyCleanup)
			b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{copy, copy, copy, unknown}, Site: site})
			returned = true
		default:
			l.err = fmt.Errorf("%w: statement %T", ErrUnsupported, statement)
		}
	}
	if !returned {
		site := b.site(span, "literal", types.Null())
		b.l.metadata.Locations[site].Text = "null"
		result, _ := b.raw(LiteralSymbol, nil, site, false)
		copy := b.failure(result, b.bodyCleanup)
		invalid := b.invalid(site, b.bodyCleanup)
		b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{copy, invalid, copy, invalid}, Site: site})
	}
	b.current = b.bodyCleanup
	b.instruction(Instruction{Kind: ScopeLeave, Site: b.f.Site})
	b.instruction(Instruction{Kind: ReleaseAll, Site: b.f.Site})
	if flow == nil {
		b.jump(b.finish)
	} else {
		framing := b.block()
		unknown := b.invalid(b.f.Site, b.leaveFlow)
		b.end(Terminator{Kind: Status, Targets: [4]BlockID{b.leaveFlow, framing, b.leaveFlow, unknown}, Site: b.f.Site})
		b.current = framing
		cause := b.value(true)
		b.instruction(Instruction{Kind: OutputValue, Value: cause, Site: b.f.Site})
		args := append([]ValueID{cause}, b.parameters...)
		result, framed := b.raw(FlowErrorSymbol, args, b.f.Site, true)
		b.f.Blocks[b.current].Instructions[len(b.f.Blocks[b.current].Instructions)-1].Index = uint64(b.f.Flow)
		handled := b.block()
		failed := b.failure(result, b.leaveFlow)
		bad := b.invalid(b.f.Site, b.leaveFlow)
		b.end(Terminator{Kind: Status, Result: result, Targets: [4]BlockID{bad, handled, failed, bad}, Site: b.f.Site})
		b.current = handled
		b.adopt(result, b.f.Site)
		if flow.Handler() == "" {
			b.instruction(Instruction{Kind: CopyResult, Result: result, Site: b.f.Site, Payload: true})
			b.jump(b.leaveFlow)
		} else {
			b.errorExit = b.leaveFlow
			handlerResult, _ := b.raw(fmt.Sprintf("lo_flow_%d", l.flowIDs[flow.Handler()]), []ValueID{framed}, b.f.Site, true)
			ok := b.failure(handlerResult, b.leaveFlow)
			host := b.failure(handlerResult, b.leaveFlow)
			invalid := b.invalid(b.f.Site, b.leaveFlow)
			b.end(Terminator{Kind: Status, Result: handlerResult, Targets: [4]BlockID{ok, invalid, host, invalid}, Site: b.f.Site})
		}
	}
	b.current = b.leaveFlow
	if flow != nil {
		b.instruction(Instruction{Kind: FlowLeave, Site: b.f.Site})
	}
	b.jump(b.finish)
	b.current = b.finish
	b.instruction(Instruction{Kind: ReleaseAll, Site: b.f.Site})
	b.end(Terminator{Kind: Return, Site: b.f.Site})
	return b.f
}

func (b *functionBuilder) expression(expression ir.Expression) ValueID {
	if b.l.err != nil {
		return 0
	}
	span := expression.Span()
	b.checked(StepSymbol, nil, b.site(span, "expression_step", expression.Type()), false, false)
	var result ValueID
	switch expr := expression.(type) {
	case autodiff.Call:
		result = b.gradient(expr)
	case ir.Literal:
		site := b.site(span, "literal", expr.Type())
		meta := &b.l.metadata.Locations[site]
		meta.Text = expr.Text()
		switch expr.Type().Kind() {
		case types.IntKind:
			if _, ok := new(big.Int).SetString(expr.Text(), 10); !ok {
				b.l.err = fmt.Errorf("native lowering: invalid integer literal")
			}
		case types.FloatKind:
			number, err := strconv.ParseFloat(expr.Text(), 64)
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
				b.l.err = fmt.Errorf("native lowering: invalid finite Float literal")
			} else {
				meta.FloatBits = fmt.Sprintf("%016x", math.Float64bits(number))
			}
		case types.BoolKind:
			if expr.Text() != "true" && expr.Text() != "false" {
				b.l.err = fmt.Errorf("native lowering: invalid Boolean literal")
			}
		case types.StringKind, types.NullKind:
		default:
			b.l.err = fmt.Errorf("%w: literal type %s", ErrUnsupported, expr.Type())
		}
		result = b.checked(LiteralSymbol, nil, site, false, false)
	case ir.Reference:
		input, ok := b.bindings[expr.Binding()]
		if !ok || !types.Equal(b.bindingTypes[expr.Binding()], expr.Type()) {
			b.l.err = fmt.Errorf("native lowering: unresolved or inconsistent binding %s", expr.Binding())
			return 0
		}
		result = b.value(true)
		b.instruction(Instruction{Kind: Alias, Value: result, Inputs: []ValueID{input}, Site: b.site(span, "reference", expr.Type())})
	case ir.Projection:
		field, ok := types.FieldType(expr.Input().Type(), expr.Field())
		if !ok || !types.Equal(field, expr.Type()) {
			b.l.err = fmt.Errorf("native lowering: inconsistent projection %s", expr.Field())
			return 0
		}
		input := b.expression(expr.Input())
		site := b.site(span, "project", expr.Type())
		b.l.metadata.Locations[site].Field = expr.Field()
		result = b.checked("lo_project", []ValueID{input}, site, false, false)
	case ir.Conversion:
		input := b.expression(expr.Input())
		names := map[ir.ConversionKind]string{ir.Success: "success", ir.Failure: "failure", ir.Propagate: "propagate", ir.RecordView: "record_view", ir.ScalarListView: "scalar_list_view", ir.FallibleRecordView: "fallible_record_view"}
		name, ok := names[expr.Conversion()]
		if !ok {
			b.l.err = fmt.Errorf("%w: conversion %d", ErrUnsupported, expr.Conversion())
			return 0
		}
		result = b.checked("lo_convert_"+name, []ValueID{input}, b.site(span, "convert_"+name, expr.Type()), expr.Conversion() == ir.Propagate, false)
	case ir.FlowCall:
		flow, ok := b.l.flows[expr.Flow()]
		if !ok || len(flow.Parameters()) != len(expr.Arguments()) || !types.Equal(flow.Signature().Output(), expr.Type()) {
			b.l.err = fmt.Errorf("native lowering: inconsistent flow call %s", expr.Flow())
			return 0
		}
		args := make([]ValueID, len(expr.Arguments()))
		seen := make([]bool, len(args))
		for _, arg := range expr.Arguments() {
			position := arg.Position()
			if position < 0 || position >= len(args) || seen[position] || !types.Equal(flow.Parameters()[position].Type, arg.Value().Type()) {
				b.l.err = fmt.Errorf("native lowering: inconsistent arguments for %s", flow.Name())
				return 0
			}
			seen[position] = true
			args[position] = b.expression(arg.Value())
		}
		site := b.site(span, "flow_call", expr.Type())
		b.l.metadata.Locations[site].Flow = b.l.flowIDs[expr.Flow()]
		result = b.call(fmt.Sprintf("lo_flow_%d", b.l.flowIDs[expr.Flow()]), args, span, expr.Type(), site)
	case ir.BuiltinCall:
		op, ok := catalog.Lookup(expr.Operation().Name())
		if !ok || op.Rule() != expr.Operation().Rule() {
			b.l.err = fmt.Errorf("native lowering: unknown catalog operation")
			return 0
		}
		if err := validateArguments(op, expr.Arguments(), b.l.flows); err != nil {
			b.l.err = err
			return 0
		}
		if literals := integerListLiterals(expr); len(literals) != 0 {
			result = b.literalSequence(expr, literals)
			break
		}
		args := make([]ValueID, len(expr.Arguments()))
		metadata := make([]ArgumentMetadata, len(expr.Arguments()))
		for _, argument := range expr.Arguments() {
			switch arg := argument.(type) {
			case ir.ValueArgument:
				args[arg.Position()] = b.expression(arg.Value())
				metadata[arg.Position()] = ArgumentMetadata{Kind: "value", Type: b.l.typeID(arg.Value().Type())}
			case ir.FlowArgument:
				site := b.site(arg.Span(), "flow_ref", nil)
				b.l.metadata.Locations[site].Flow = b.l.flowIDs[arg.Flow()]
				args[arg.Position()] = b.checked("lo_flow_ref", nil, site, false, false)
				metadata[arg.Position()] = ArgumentMetadata{Kind: "flow", Flow: b.l.flowIDs[arg.Flow()]}
			default:
				b.l.err = fmt.Errorf("%w: argument %T", ErrUnsupported, arg)
			}
		}
		site := b.site(span, "operation", expr.Type())
		b.l.metadata.Locations[site].Operation = op.Name()
		b.l.metadata.Locations[site].Labels = append([]string{}, expr.Labels()...)
		b.l.metadata.Locations[site].Arguments = metadata
		result = b.call(OperationSymbol(op.Name()), args, span, expr.Type(), site)
	case ir.Match:
		selector := b.expression(expr.Selector())
		join := b.block()
		inputs := []ValueID{}
		preds := []BlockID{}
		for _, arm := range expr.Cases() {
			if _, ok := arm.(ir.DefaultCase); !ok {
				var condition ValueID
				switch arm := arm.(type) {
				case ir.ExactCase:
					head := b.expression(arm.Head())
					condition = b.checked("lo_match", []ValueID{selector, head}, b.site(arm.Span(), "match", types.Bool()), false, false)
				case ir.PredicateCase:
					flow, ok := b.l.flows[arm.Flow()]
					if !ok || len(flow.Parameters()) != 1 || !types.Equal(flow.Parameters()[0].Type, expr.Selector().Type()) || flow.Signature().Output().Kind() != types.BoolKind {
						b.l.err = fmt.Errorf("native lowering: inconsistent predicate")
						return 0
					}
					site := b.site(arm.Span(), "predicate", types.Bool())
					b.l.metadata.Locations[site].Flow = b.l.flowIDs[arm.Flow()]
					condition = b.checked(fmt.Sprintf("lo_flow_%d", b.l.flowIDs[arm.Flow()]), []ValueID{selector}, site, false, false)
				default:
					b.l.err = fmt.Errorf("%w: case %T", ErrUnsupported, arm)
					return 0
				}
				yes, no := b.block(), b.block()
				b.end(Terminator{Kind: Boolean, Value: condition, Targets: [4]BlockID{yes, no}, Site: b.site(arm.Span(), "case", expr.Type())})
				b.current = yes
				value := b.expression(arm.Body())
				inputs = append(inputs, value)
				preds = append(preds, b.current)
				b.jump(join)
				b.current = no
			} else {
				value := b.expression(arm.Body())
				inputs = append(inputs, value)
				preds = append(preds, b.current)
				b.jump(join)
			}
		}
		b.current = join
		result = b.value(true)
		b.instruction(Instruction{Kind: Phi, Value: result, Inputs: inputs, Predecessors: preds, Site: b.site(span, "match_result", expr.Type())})
	default:
		b.l.err = fmt.Errorf("%w: expression %T", ErrUnsupported, expr)
		return 0
	}
	if b.l.err != nil {
		return 0
	}
	b.checked(ChargeSymbol, []ValueID{result}, b.site(span, "charge_value", expression.Type()), false, false)
	return result
}

func integerListLiterals(call ir.BuiltinCall) []ir.Literal {
	if call.Operation().Name() != "List.new" || len(call.Arguments()) == 0 || len(call.Labels()) != 0 {
		return nil
	}
	literals := make([]ir.Literal, len(call.Arguments()))
	for index, argument := range call.Arguments() {
		value, ok := argument.(ir.ValueArgument)
		if !ok || value.Position() != index {
			return nil
		}
		literal, ok := value.Value().(ir.Literal)
		if !ok || literal.Type().Kind() != types.IntKind {
			return nil
		}
		literals[index] = literal
	}
	return literals
}

func (b *functionBuilder) literalSequence(call ir.BuiltinCall, literals []ir.Literal) ValueID {
	count := uint64(len(literals))
	// Reserve exactly the former step/literal/charge owners. The final List.new
	// owner is a separate ordinary result immediately after this range.
	if count > uint64(math.MaxUint32)/3 || uint64(len(b.f.Values))+3*count >= uint64(math.MaxUint32) {
		b.l.err = fmt.Errorf("%w: literal ownership range", ErrLimit)
		return 0
	}
	base := ValueID(len(b.f.Values) + 1)
	triples := make([]LiteralTriple, len(literals))
	arguments := make([]ArgumentMetadata, len(literals))
	for index, literal := range literals {
		span := literal.Span()
		step := b.site(span, "expression_step", literal.Type())
		if b.l.err != nil {
			return 0
		}
		site := b.site(span, "literal", literal.Type())
		if b.l.err != nil {
			return 0
		}
		b.l.metadata.Locations[site].Text = literal.Text()
		if _, ok := new(big.Int).SetString(literal.Text(), 10); !ok {
			b.l.err = fmt.Errorf("native lowering: invalid integer literal")
			return 0
		}
		charge := b.site(span, "charge_value", literal.Type())
		if b.l.err != nil {
			return 0
		}
		triples[index] = LiteralTriple{Step: step, Literal: site, Charge: charge}
		arguments[index] = ArgumentMetadata{Kind: "value", Type: b.l.typeID(literal.Type())}
		for phase := 0; phase < 3; phase++ {
			b.value(true)
		}
	}
	site := b.site(call.Span(), "operation", call.Type())
	metadata := &b.l.metadata.Locations[site]
	metadata.Operation = "List.new"
	metadata.Labels = append([]string{}, call.Labels()...)
	metadata.Arguments = arguments
	b.f.Results++
	result := ResultID(b.f.Results)
	value := b.value(true)
	b.resultValues[result] = value
	b.instruction(Instruction{Kind: LiteralSequence, Symbol: OperationSymbol("List.new"), Result: result, Value: value, Site: site, OwnedBase: base, Literals: triples})
	return b.checkedResult(result, value, site, false)
}

func validateArguments(op catalog.Operation, args []ir.Argument, flows map[identity.FlowID]ir.Flow) error {
	count := len(args)
	variadic := op.Rule() == catalog.StringConcat || op.Rule() == catalog.ListConstructor || op.Rule() == catalog.MapConstructor || op.Rule() == catalog.RecordConstructor
	processor := op.Rule() == catalog.ListProcessor || op.Rule() == catalog.FlowProcessor
	guarded := op.Rule() == catalog.CheckedRemovalConstructor
	if !variadic && !processor && !guarded && count != len(op.Parameters()) {
		return fmt.Errorf("native lowering: %s requires %d arguments, got %d", op.Name(), len(op.Parameters()), count)
	}
	minimum := 2
	if op.Name() == catalog.ListFold || op.Name() == catalog.ListUnfold || op.Rule() == catalog.FlowProcessor {
		minimum = 3
	}
	if (processor || guarded) && count < minimum {
		return fmt.Errorf("native lowering: incomplete callback arguments for %s", op.Name())
	}
	seen := make([]bool, count)
	for _, arg := range args {
		if arg == nil || arg.Position() < 0 || arg.Position() >= count || seen[arg.Position()] {
			return fmt.Errorf("native lowering: repeated or invalid argument position for %s", op.Name())
		}
		seen[arg.Position()] = true
		switch arg := arg.(type) {
		case ir.ValueArgument:
			if (processor || guarded) && arg.Position() == count-1 {
				return fmt.Errorf("native lowering: missing callback for %s", op.Name())
			}
			if op.Rule() == catalog.Fixed && op.Parameters()[arg.Position()].Type != nil && !types.Equal(op.Parameters()[arg.Position()].Type, arg.Value().Type()) {
				return fmt.Errorf("native lowering: inconsistent argument type for %s", op.Name())
			}
		case ir.FlowArgument:
			flow, ok := flows[arg.Flow()]
			if !ok {
				return fmt.Errorf("native lowering: missing callback %s", arg.Flow())
			}
			if guarded {
				if arg.Position() != count-1 || len(flow.Parameters()) != count || !types.Equal(flow.Signature().Output(), types.Bool()) || flow.Effects().Reads || flow.Effects().Commits {
					return fmt.Errorf("native lowering: invalid removal predicate for %s", op.Name())
				}
				for index, parameter := range flow.Parameters() {
					if index >= count-2 && !types.Equal(parameter.Type, types.FileEntry()) {
						return fmt.Errorf("native lowering: invalid removal predicate entry type")
					}
					if index < count-2 {
						matched := false
						for _, candidate := range args {
							if value, ok := candidate.(ir.ValueArgument); ok && value.Position() == index+1 {
								matched = types.Equal(parameter.Type, value.Value().Type())
							}
						}
						if !matched {
							return fmt.Errorf("native lowering: invalid removal predicate context type")
						}
					}
				}
			} else if processor {
				arity := count - 1
				if op.Name() == catalog.ListUnfold {
					arity = count - 2
				}
				if arg.Position() != count-1 || len(flow.Parameters()) != arity {
					return fmt.Errorf("native lowering: inconsistent callback arity for %s", op.Name())
				}
			} else if op.Rule() != catalog.ServerConstructor {
				return fmt.Errorf("native lowering: unexpected callback for %s", op.Name())
			}
		default:
			return fmt.Errorf("%w: argument %T", ErrUnsupported, arg)
		}
	}
	return nil
}
