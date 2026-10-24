// Package llvmemit emits LLVM 21 modules from verified native plans.
package llvmemit

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
)

type output struct {
	ctx  context.Context
	text strings.Builder
	err  error
}

func (o *output) write(format string, args ...any) {
	if o.err != nil {
		return
	}
	if err := o.ctx.Err(); err != nil {
		o.err = fmt.Errorf("LLVM emission: %w", err)
		return
	}
	text := fmt.Sprintf(format, args...)
	if len(text) > nativeplan.MaxModuleBytes-o.text.Len() {
		o.err = fmt.Errorf("%w: native module exceeds %d bytes", nativeplan.ErrLimit, nativeplan.MaxModuleBytes)
		return
	}
	o.text.WriteString(text)
}
func quoted(text string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	out.WriteByte('"')
	for _, value := range []byte(text) {
		if value >= 32 && value < 127 && value != '"' && value != '\\' {
			out.WriteByte(value)
		} else {
			out.WriteByte('\\')
			out.WriteByte(hex[value>>4])
			out.WriteByte(hex[value&15])
		}
	}
	out.WriteByte('"')
	return out.String()
}

// Emit produces a complete LLVM module without invoking compiler tools.
func Emit(ctx context.Context, plan nativeplan.Plan, target string) ([]byte, error) {
	return emit(ctx, plan, target, nil)
}

// EmitArtifact includes the selected executable configuration in its program metadata.
func EmitArtifact(ctx context.Context, plan nativeplan.Plan, target string, artifact json.RawMessage) ([]byte, error) {
	return emit(ctx, plan, target, artifact)
}

func emit(ctx context.Context, plan nativeplan.Plan, target string, artifact json.RawMessage) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("LLVM emission requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("LLVM emission: %w", err)
	}
	triples := map[string]string{"darwin/arm64": "aarch64-apple-darwin", "linux/arm64": "aarch64-unknown-linux-gnu"}
	triple, ok := triples[target]
	if !ok {
		return nil, fmt.Errorf("unsupported native LLVM target %q", target)
	}
	module, err := nativeplan.Read(plan)
	if err != nil {
		return nil, err
	}
	if artifact != nil {
		module.Metadata, err = nativeplan.WithArtifactMetadata(ctx, module.Metadata, artifact)
		if err != nil {
			return nil, fmt.Errorf("LLVM artifact metadata: %w", err)
		}
	}

	out := &output{ctx: ctx}
	debug := newDebug(module)
	out.write("; Lo native lowering; ABI %s; LLVM 21\nsource_filename = %s\ntarget triple = %s\n\n%%lo.result = type { i32, i32, ptr }\n\n", nativeplan.ABIIdentity, quoted(module.SourcePath), quoted(triple))
	out.write("@lo_program_metadata = constant [%d x i8] c\"", len(module.Metadata))
	const hex = "0123456789ABCDEF"
	for start := 0; start < len(module.Metadata); {
		end := start + 4096
		if end > len(module.Metadata) {
			end = len(module.Metadata)
		}
		var chunk strings.Builder
		for _, value := range module.Metadata[start:end] {
			if value >= 32 && value < 127 && value != '"' && value != '\\' {
				chunk.WriteByte(value)
			} else {
				chunk.WriteByte('\\')
				chunk.WriteByte(hex[value>>4])
				chunk.WriteByte(hex[value&15])
			}
		}
		out.write("%s", chunk.String())
		start = end
	}
	var callbacks []nativeplan.Function
	for _, function := range module.Functions {
		if function.Flow >= 0 {
			callbacks = append(callbacks, function)
		}
	}
	out.write("\", align 1\n@lo_program_metadata_size = constant i64 %d\n@lo_program_flow_count = constant i64 %d\n", len(module.Metadata), len(callbacks))
	out.write("@lo_program_flows = constant [%d x ptr] ", len(callbacks))
	if len(callbacks) == 0 {
		out.write("zeroinitializer\n\n")
	} else {
		parts := []string{}
		for _, f := range callbacks {
			parts = append(parts, "ptr @"+f.Symbol)
		}
		out.write("[%s]\n\n", strings.Join(parts, ", "))
	}
	defined := map[string]bool{}
	for _, f := range module.Functions {
		defined[f.Symbol] = true
	}
	symbols := map[string]bool{}
	for _, f := range module.Functions {
		for _, block := range f.Blocks {
			for _, i := range block.Instructions {
				if i.Kind == nativeplan.Call && !defined[i.Symbol] {
					symbols[i.Symbol] = true
				}
				if i.Kind == nativeplan.LiteralSequence {
					for _, symbol := range []string{nativeplan.StepSymbol, nativeplan.LiteralSymbol, nativeplan.ChargeSymbol, nativeplan.OperationSymbol("List.new")} {
						symbols[symbol] = true
					}
				}
			}
		}
	}
	ordered := []string{}
	for symbol := range symbols {
		ordered = append(ordered, symbol)
	}
	slices.Sort(ordered)
	for _, symbol := range ordered {
		if symbol == nativeplan.FlowErrorSymbol {
			out.write("declare void @%s(ptr, i64, ptr, i64, i64, ptr)\n", symbol)
		} else {
			out.write("declare void @%s(ptr, ptr, i64, i64, ptr)\n", symbol)
		}
	}
	out.write("declare void @lo_value_retain(ptr)\ndeclare void @lo_value_release(ptr)\ndeclare void @lo_scope_leave(ptr, ptr, i64)\ndeclare void @lo_flow_leave(ptr)\ndeclare i32 @lo_boolean_value(ptr)\n\n")
	out.write("declare void @lo_derivative_leave(ptr)\n\n")
	out.write("declare void @llvm.memset.p0.i64(ptr nocapture writeonly, i8, i64, i1 immarg)\n\n")
	out.write(`define internal void @lo_result_copy_value(ptr %%out, ptr %%result) #0 {
entry:
  %%old_pointer = getelementptr %%lo.result, ptr %%out, i32 0, i32 2
  %%old = load ptr, ptr %%old_pointer, align 8
  %%value_pointer = getelementptr %%lo.result, ptr %%result, i32 0, i32 2
  %%value = load ptr, ptr %%value_pointer, align 8
  call void @lo_value_retain(ptr %%value)
  call void @lo_value_release(ptr %%old)
  %%copy = load %%lo.result, ptr %%result, align 8
  store %%lo.result %%copy, ptr %%out, align 8
  ret void
}
define internal void @lo_result_copy_host(ptr %%out, ptr %%result) #0 {
entry:
  %%old_pointer = getelementptr %%lo.result, ptr %%out, i32 0, i32 2
  %%old = load ptr, ptr %%old_pointer, align 8
  call void @lo_value_release(ptr %%old)
  %%code_pointer = getelementptr %%lo.result, ptr %%result, i32 0, i32 1
  %%code = load i32, ptr %%code_pointer, align 4
  %%copy = insertvalue %%lo.result { i32 2, i32 0, ptr null }, i32 %%code, 1
  store %%lo.result %%copy, ptr %%out, align 8
  ret void
}
define internal i1 @lo_result_transfer(ptr %%out, ptr %%result, ptr %%owned) #0 {
entry:
  %%status_pointer = getelementptr %%lo.result, ptr %%result, i32 0, i32 0
  %%status = load i32, ptr %%status_pointer, align 4
  switch i32 %%status, label %%unknown [ i32 0, label %%payload i32 1, label %%payload i32 2, label %%host ]
payload:
  %%value_pointer = getelementptr %%lo.result, ptr %%result, i32 0, i32 2
  %%value = load ptr, ptr %%value_pointer, align 8
  store ptr %%value, ptr %%owned, align 8
  call void @lo_result_copy_value(ptr %%out, ptr %%result)
  ret i1 true
host:
  call void @lo_result_copy_host(ptr %%out, ptr %%result)
  ret i1 true
unknown:
  ret i1 false
}


`)

	for _, function := range module.Functions {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				if instruction.Kind == nativeplan.LiteralSequence {
					emitLiteralSequence(out, debug, module, function, instruction)
				}
			}
		}
		emitFunction(out, debug, module, function)
	}
	out.write("attributes #0 = { noinline \"fp-contract\"=\"off\" \"frame-pointer\"=\"all\" }\n")
	out.write("attributes #1 = { noinline optnone \"fp-contract\"=\"off\" \"frame-pointer\"=\"all\" }\n\n")
	debug.emit(out)
	if out.err != nil {
		return nil, out.err
	}
	return []byte(out.text.String()), nil
}

type functionEmitter struct {
	o          *output
	debug      *debugInfo
	module     nativeplan.Module
	f          nativeplan.Function
	serial     uint64
	subprogram int
	edges      map[nativeplan.BlockID][]string
	groups     map[nativeplan.ValueID]nativeplan.Instruction
	endLabels  map[nativeplan.BlockID]string
	block      nativeplan.BlockID
	label      string
}

func literalSymbol(function nativeplan.Function, instruction nativeplan.Instruction) string {
	return fmt.Sprintf("lo_literals_%s_%d", function.Symbol, instruction.Result)
}

func releaseLabel(block nativeplan.BlockID, base nativeplan.ValueID) string {
	return fmt.Sprintf("release_b%d_g%d", block, base)
}

func emitLiteralSequence(out *output, debug *debugInfo, module nativeplan.Module, function nativeplan.Function, instruction nativeplan.Instruction) {
	symbol := literalSymbol(function, instruction)
	count := len(instruction.Literals)
	out.write("@%s_sites = private unnamed_addr constant [%d x { i64, i64, i64 }] [", symbol, count)
	for index, triple := range instruction.Literals {
		if index != 0 {
			out.write(", ")
		}
		out.write("{ i64, i64, i64 } { i64 %d, i64 %d, i64 %d }", triple.Step, triple.Literal, triple.Charge)
	}
	out.write("], align 8\n")
	e := &functionEmitter{o: out, debug: debug, module: module}
	// The private helper has no Lo flow identity. Its static instruction
	// locations use the enclosing List.new; original dynamic sites stay exact.
	e.subprogram = debug.function(nativeplan.Function{Symbol: symbol, Name: "<literal sequence>"}, module.Locations[instruction.Site])
	out.write("define internal void @%s(ptr %%context, ptr %%owned, ptr %%arguments, ptr %%out) #0 !dbg !%d {\nentry:\n", symbol, e.subprogram)
	site := instruction.Site
	e.line(site, "%%scratch = alloca %%lo.result, align 8")
	e.line(site, "%%status_pointer = getelementptr %%lo.result, ptr %%scratch, i32 0, i32 0")
	e.line(site, "%%value_pointer = getelementptr %%lo.result, ptr %%scratch, i32 0, i32 2")
	e.line(site, "br label %%items")
	out.write("items:\n")
	e.line(site, "%%index = phi i64 [ 0, %%entry ], [ %%next, %%charge_adopt ]")
	e.line(site, "%%triple = getelementptr [%d x { i64, i64, i64 }], ptr @%s_sites, i64 0, i64 %%index", count, symbol)
	e.line(site, "%%argument = getelementptr ptr, ptr %%arguments, i64 %%index")
	e.line(site, "%%owned_index = mul i64 %%index, 3")
	for phase, name := range []string{"step", "literal", "charge"} {
		if phase != 0 {
			out.write("%s:\n", name)
		}
		e.line(site, "%%%s_site_pointer = getelementptr { i64, i64, i64 }, ptr %%triple, i32 0, i32 %d", name, phase)
		e.line(site, "%%%s_site = load i64, ptr %%%s_site_pointer, align 8", name, name)
		e.line(site, "store %%lo.result zeroinitializer, ptr %%scratch, align 8")
		arguments, arity := "null", 0
		if phase == 2 {
			arguments, arity = "%argument", 1
		}
		symbol := []string{nativeplan.StepSymbol, nativeplan.LiteralSymbol, nativeplan.ChargeSymbol}[phase]
		e.line(site, "call void @%s(ptr %%context, ptr %s, i64 %d, i64 %%%s_site, ptr %%scratch)", symbol, arguments, arity, name)
		e.line(site, "%%%s_status = load i32, ptr %%status_pointer, align 4", name)
		e.line(site, "switch i32 %%%s_status, label %%invalid [ i32 0, label %%%s_adopt i32 2, label %%host ]", name, name)
		out.write("%s_adopt:\n", name)
		e.line(site, "%%%s_value = load ptr, ptr %%value_pointer, align 8", name)
		e.line(site, "%%%s_index = add i64 %%owned_index, %d", name, phase)
		e.line(site, "%%%s_owner = getelementptr ptr, ptr %%owned, i64 %%%s_index", name, name)
		e.line(site, "store ptr %%%s_value, ptr %%%s_owner, align 8", name, name)
		if phase == 1 {
			e.line(site, "store ptr %%literal_value, ptr %%argument, align 8")
		}
		if phase < 2 {
			e.line(site, "br label %%%s", []string{"literal", "charge"}[phase])
		}
	}
	e.line(site, "%%next = add i64 %%index, 1")
	e.line(site, "%%more = icmp ult i64 %%next, %d", count)
	e.line(site, "br i1 %%more, label %%items, label %%finish, !llvm.loop !%d", debug.loop())
	out.write("finish:\n")
	e.line(site, "call void @%s(ptr %%context, ptr %%arguments, i64 %d, i64 %d, ptr %%out)", nativeplan.OperationSymbol("List.new"), count, site)
	e.line(site, "ret void")
	out.write("host:\n")
	e.line(site, "%%code_pointer = getelementptr %%lo.result, ptr %%scratch, i32 0, i32 1")
	e.line(site, "%%code = load i32, ptr %%code_pointer, align 4")
	e.line(site, "%%host_result = insertvalue %%lo.result { i32 2, i32 0, ptr null }, i32 %%code, 1")
	e.line(site, "store %%lo.result %%host_result, ptr %%out, align 8")
	e.line(site, "ret void")
	out.write("invalid:\n")
	e.line(site, "%%invalid_status = load i32, ptr %%status_pointer, align 4")
	e.line(site, "%%invalid_result = insertvalue %%lo.result zeroinitializer, i32 %%invalid_status, 0")
	e.line(site, "store %%lo.result %%invalid_result, ptr %%out, align 8")
	e.line(site, "ret void")
	out.write("}\n\n")
}

func (e *functionEmitter) fresh() string { e.serial++; return fmt.Sprintf("%%x%d", e.serial) }
func (e *functionEmitter) dbg(site uint64) string {
	return fmt.Sprintf(", !dbg !%d", e.debug.location(e.subprogram, e.module.Locations[site]))
}
func (e *functionEmitter) line(site uint64, format string, args ...any) {
	e.o.write("  %s%s\n", fmt.Sprintf(format, args...), e.dbg(site))
}
func (e *functionEmitter) result(id nativeplan.ResultID) string {
	if id == 0 {
		return "%out"
	}
	return fmt.Sprintf("%%r%d", id)
}
func (e *functionEmitter) field(id nativeplan.ResultID, index int) string {
	value := e.fresh()
	e.o.write("  %s = getelementptr %%lo.result, ptr %s, i32 0, i32 %d\n", value, e.result(id), index)
	return value
}
func (e *functionEmitter) payload(id nativeplan.ResultID) string {
	field := e.field(id, 2)
	value := e.fresh()
	e.o.write("  %s = load ptr, ptr %s, align 8\n", value, field)
	return value
}
func (e *functionEmitter) value(id nativeplan.ValueID) string {
	value := e.fresh()
	e.o.write("  %s = load ptr, ptr %%v%d, align 8\n", value, id)
	return value
}
func (e *functionEmitter) owned(id nativeplan.ValueID, value string, site uint64) {
	e.line(site, "call void @lo_value_retain(ptr %s)", value)
	e.line(site, "store ptr %s, ptr %%v%d, align 8", value, id)
}

type failureTransfer struct {
	result  nativeplan.ResultID
	owned   nativeplan.ValueID
	exit    nativeplan.BlockID
	invalid nativeplan.BlockID
}

// failureTransfers recognizes only the verifier's isolated status-copy shape.
func failureTransfers(f nativeplan.Function) (map[nativeplan.BlockID]failureTransfer, map[nativeplan.BlockID]bool) {
	predecessors := make([][]nativeplan.BlockID, len(f.Blocks))
	phiPredecessors := map[nativeplan.BlockID]bool{}
	for _, block := range f.Blocks {
		var targets []nativeplan.BlockID
		switch block.End.Kind {
		case nativeplan.Jump:
			targets = block.End.Targets[:1]
		case nativeplan.Status:
			targets = block.End.Targets[:]
		case nativeplan.Boolean, nativeplan.Arity:
			targets = block.End.Targets[:2]
		}
		for _, target := range targets {
			if !slices.Contains(predecessors[target], block.ID) {
				predecessors[target] = append(predecessors[target], block.ID)
			}
		}
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.Phi {
				for _, predecessor := range instruction.Predecessors {
					phiPredecessors[predecessor] = true
				}
			}
		}
	}
	transfers := map[nativeplan.BlockID]failureTransfer{}
	removed := map[nativeplan.BlockID]bool{}
	for _, block := range f.Blocks {
		end := block.End
		if len(block.Instructions) != 0 || end.Kind != nativeplan.Status || end.Result == 0 || end.Targets[0] != end.Targets[1] {
			continue
		}
		payload, host := f.Blocks[end.Targets[0]], f.Blocks[end.Targets[2]]
		if payload.ID == host.ID || payload.ID == end.Targets[3] || host.ID == end.Targets[3] ||
			len(predecessors[payload.ID]) != 1 || predecessors[payload.ID][0] != block.ID ||
			len(predecessors[host.ID]) != 1 || predecessors[host.ID][0] != block.ID ||
			phiPredecessors[payload.ID] || phiPredecessors[host.ID] ||
			len(payload.Instructions) != 2 || len(host.Instructions) != 1 ||
			payload.End.Kind != nativeplan.Jump || host.End.Kind != nativeplan.Jump || payload.End.Targets[0] != host.End.Targets[0] {
			continue
		}
		adopt, copyValue, copyHost := payload.Instructions[0], payload.Instructions[1], host.Instructions[0]
		if adopt.Kind != nativeplan.AdoptResult || copyValue.Kind != nativeplan.CopyResult || !copyValue.Payload ||
			copyHost.Kind != nativeplan.CopyResult || copyHost.Payload || adopt.Result != end.Result ||
			copyValue.Result != end.Result || copyHost.Result != end.Result ||
			adopt.Site != end.Site || copyValue.Site != end.Site || copyHost.Site != end.Site {
			continue
		}
		transfers[block.ID] = failureTransfer{end.Result, adopt.Value, payload.End.Targets[0], end.Targets[3]}
		removed[payload.ID], removed[host.ID] = true, true
	}
	return transfers, removed
}

func emitFunction(out *output, debug *debugInfo, module nativeplan.Module, f nativeplan.Function) {
	e := &functionEmitter{o: out, debug: debug, module: module, f: f, edges: map[nativeplan.BlockID][]string{}, groups: map[nativeplan.ValueID]nativeplan.Instruction{}, endLabels: map[nativeplan.BlockID]string{}}
	e.subprogram = debug.function(f, module.Locations[f.Site])
	var lastGroup nativeplan.ValueID
	for _, block := range f.Blocks {
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.LiteralSequence {
				e.groups[instruction.OwnedBase] = instruction
				if instruction.OwnedBase > lastGroup {
					lastGroup = instruction.OwnedBase
				}
			}
		}
	}
	for _, block := range f.Blocks {
		e.endLabels[block.ID] = fmt.Sprintf("b%d", block.ID)
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.ReleaseAll && lastGroup != 0 {
				e.endLabels[block.ID] = releaseLabel(block.ID, lastGroup) + "_done"
			}
		}
	}
	for _, block := range f.Blocks {
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.Phi {
				for index, pred := range instruction.Predecessors {
					e.edges[pred] = append(e.edges[pred], fmt.Sprintf("%%phi_edge_%d_%d = load ptr, ptr %%v%d, align 8", instruction.Value, index, instruction.Inputs[index]))
				}
			}
		}
	}
	// Large acyclic ownership graphs make scalar optimizer passes expensive.
	// Keep their checked sequence intact and optimize bounded functions normally.
	const maxOptimizedBlocks = 1024
	attributes := 0
	if len(f.Blocks) > maxOptimizedBlocks {
		attributes = 1
	}
	out.write("define void @%s(ptr %%context, ptr %%arguments, i64 %%argc, i64 %%callsite, ptr %%out) #%d !dbg !%d {\nentry:\n", f.Symbol, attributes, e.subprogram)
	for index := 0; index < len(f.Values); {
		if group, ok := e.groups[nativeplan.ValueID(index+1)]; ok {
			count := 3 * len(group.Literals)
			out.write("  %%sequence_owned%d = alloca [%d x ptr], align 8\n", group.OwnedBase, count)
			// The verifier bounds 3*N to ValueID and accounts all function
			// storage; 8*(3*N) is consequently representable in i64.
			e.line(f.Site, "call void @llvm.memset.p0.i64(ptr align 8 %%sequence_owned%d, i8 0, i64 %d, i1 false)", group.OwnedBase, uint64(count)*8)
			out.write("  %%sequence_args%d = alloca [%d x ptr], align 8\n", group.Result, len(group.Literals))
			index += count
			continue
		}
		out.write("  %%v%d = alloca ptr, align 8\n  store ptr null, ptr %%v%d, align 8\n", index+1, index+1)
		index++
	}
	for index := uint32(1); index <= f.Results; index++ {
		out.write("  %%r%d = alloca %%lo.result, align 8\n  store %%lo.result zeroinitializer, ptr %%r%d, align 8\n", index, index)
	}
	for _, block := range f.Blocks {
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.Call && len(instruction.Inputs) > 0 {
				out.write("  %%args%d = alloca [%d x ptr], align 8\n", instruction.Result, len(instruction.Inputs))
			}
		}
	}
	out.write("  store %%lo.result zeroinitializer, ptr %%out, align 8\n  br label %%b0\n")
	transfers, removed := failureTransfers(f)
	for _, block := range f.Blocks {
		if removed[block.ID] {
			continue
		}
		out.write("b%d:\n", block.ID)
		e.block, e.label = block.ID, fmt.Sprintf("b%d", block.ID)
		if transfer, folded := transfers[block.ID]; folded {
			valid := e.fresh()
			e.line(block.End.Site, "%s = call i1 @lo_result_transfer(ptr %%out, ptr %s, ptr %%v%d)", valid, e.result(transfer.result), transfer.owned)
			e.line(block.End.Site, "br i1 %s, label %%b%d, label %%b%d", valid, transfer.exit, transfer.invalid)
			continue
		}
		// LLVM phi instructions precede every other instruction in their block.
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.Phi {
				parts := []string{}
				for index, pred := range instruction.Predecessors {
					parts = append(parts, fmt.Sprintf("[ %%phi_edge_%d_%d, %%%s ]", instruction.Value, index, e.endLabels[pred]))
				}
				e.line(instruction.Site, "%%phi%d = phi ptr %s", instruction.Value, strings.Join(parts, ", "))
			}
		}
		for _, instruction := range block.Instructions {
			e.instruction(instruction)
		}
		for _, edge := range e.edges[block.ID] {
			out.write("  %s\n", edge)
		}
		e.terminator(block.End)
	}
	out.write("}\n\n")
}

func (e *functionEmitter) instruction(i nativeplan.Instruction) {
	switch i.Kind {
	case nativeplan.LiteralSequence:
		e.line(i.Site, "call void @%s(ptr %%context, ptr %%sequence_owned%d, ptr %%sequence_args%d, ptr %s)", literalSymbol(e.f, i), i.OwnedBase, i.Result, e.result(i.Result))
	case nativeplan.Call:
		arguments := "null"
		if len(i.Inputs) > 0 {
			arguments = fmt.Sprintf("%%args%d", i.Result)
			for index, id := range i.Inputs {
				pointer := e.fresh()
				value := e.value(id)
				e.line(i.Site, "%s = getelementptr [%d x ptr], ptr %s, i64 0, i64 %d", pointer, len(i.Inputs), arguments, index)
				e.line(i.Site, "store ptr %s, ptr %s, align 8", value, pointer)
			}
		}
		location := fmt.Sprint(i.Site)
		if i.CallerLocation {
			location = "%callsite"
		}
		if i.Symbol == nativeplan.FlowErrorSymbol {
			e.line(i.Site, "call void @%s(ptr %%context, i64 %d, ptr %s, i64 %d, i64 %s, ptr %s)", i.Symbol, i.Index, arguments, len(i.Inputs), location, e.result(i.Result))
		} else {
			e.line(i.Site, "call void @%s(ptr %%context, ptr %s, i64 %d, i64 %s, ptr %s)", i.Symbol, arguments, len(i.Inputs), location, e.result(i.Result))
		}
	case nativeplan.AdoptResult:
		value := e.payload(i.Result)
		e.line(i.Site, "store ptr %s, ptr %%v%d, align 8", value, i.Value)
	case nativeplan.Parameter:
		pointer := e.fresh()
		e.line(i.Site, "%s = getelementptr ptr, ptr %%arguments, i64 %d", pointer, i.Index)
		value := e.fresh()
		e.line(i.Site, "%s = load ptr, ptr %s, align 8", value, pointer)
		e.line(i.Site, "store ptr %s, ptr %%v%d, align 8", value, i.Value)
	case nativeplan.Alias:
		e.owned(i.Value, e.value(i.Inputs[0]), i.Site)
	case nativeplan.Phi:
		e.owned(i.Value, fmt.Sprintf("%%phi%d", i.Value), i.Site)
	case nativeplan.OutputValue:
		e.owned(i.Value, e.payload(0), i.Site)
	case nativeplan.CopyResult:
		helper := "lo_result_copy_host"
		if i.Payload {
			helper = "lo_result_copy_value"
		}
		e.line(i.Site, "call void @%s(ptr %%out, ptr %s)", helper, e.result(i.Result))
	case nativeplan.ScopeLeave:
		e.line(i.Site, "call void @lo_scope_leave(ptr %%context, ptr %s, i64 %d)", e.payload(0), i.Site)
	case nativeplan.FlowLeave:
		e.line(i.Site, "call void @lo_flow_leave(ptr %%context)")
	case nativeplan.TapeLeave:
		e.line(i.Site, "call void @lo_derivative_leave(ptr %%context)")
	case nativeplan.Release:
		for _, id := range i.Inputs {
			value := e.value(id)
			e.line(i.Site, "call void @lo_value_release(ptr %s)", value)
			e.line(i.Site, "store ptr null, ptr %%v%d, align 8", id)
		}
	case nativeplan.ReleaseAll:
		for index := 0; index < len(e.f.Values); {
			if group, ok := e.groups[nativeplan.ValueID(index+1)]; ok {
				e.releaseGroup(group, i.Site)
				index += 3 * len(group.Literals)
				continue
			}
			owned := e.f.Values[index]
			if owned {
				id := nativeplan.ValueID(index + 1)
				value := e.value(id)
				e.line(i.Site, "call void @lo_value_release(ptr %s)", value)
				e.line(i.Site, "store ptr null, ptr %%v%d, align 8", id)
			}
			index++
		}
	}
}

func (e *functionEmitter) releaseGroup(group nativeplan.Instruction, site uint64) {
	prefix := releaseLabel(e.block, group.OwnedBase)
	e.line(site, "br label %%%s_loop", prefix)
	e.o.write("%s_loop:\n", prefix)
	index, next := e.fresh(), e.fresh()
	e.line(site, "%s = phi i64 [ 0, %%%s ], [ %s, %%%s_loop ]", index, e.label, next, prefix)
	pointer, value, again := e.fresh(), e.fresh(), e.fresh()
	e.line(site, "%s = getelementptr ptr, ptr %%sequence_owned%d, i64 %s", pointer, group.OwnedBase, index)
	e.line(site, "%s = load ptr, ptr %s, align 8", value, pointer)
	e.line(site, "call void @lo_value_release(ptr %s)", value)
	e.line(site, "store ptr null, ptr %s, align 8", pointer)
	e.line(site, "%s = add i64 %s, 1", next, index)
	e.line(site, "%s = icmp ult i64 %s, %d", again, next, 3*len(group.Literals))
	e.line(site, "br i1 %s, label %%%s_loop, label %%%s_done, !llvm.loop !%d", again, prefix, prefix, e.debug.loop())
	e.o.write("%s_done:\n", prefix)
	e.label = prefix + "_done"
}
func (e *functionEmitter) terminator(t nativeplan.Terminator) {
	switch t.Kind {
	case nativeplan.Jump:
		e.line(t.Site, "br label %%b%d", t.Targets[0])
	case nativeplan.Return:
		e.line(t.Site, "ret void")
	case nativeplan.Arity:
		condition := e.fresh()
		e.line(t.Site, "%s = icmp eq i64 %%argc, %d", condition, e.f.Arity)
		e.line(t.Site, "br i1 %s, label %%b%d, label %%b%d", condition, t.Targets[0], t.Targets[1])
	case nativeplan.Boolean:
		value := e.value(t.Value)
		boolean := e.fresh()
		e.line(t.Site, "%s = call i32 @lo_boolean_value(ptr %s)", boolean, value)
		condition := e.fresh()
		e.line(t.Site, "%s = icmp ne i32 %s, 0", condition, boolean)
		e.line(t.Site, "br i1 %s, label %%b%d, label %%b%d", condition, t.Targets[0], t.Targets[1])
	case nativeplan.Status:
		pointer := e.field(t.Result, 0)
		status := e.fresh()
		e.line(t.Site, "%s = load i32, ptr %s, align 4", status, pointer)
		e.line(t.Site, "switch i32 %s, label %%b%d [ i32 0, label %%b%d i32 1, label %%b%d i32 2, label %%b%d ]", status, t.Targets[3], t.Targets[0], t.Targets[1], t.Targets[2])
	}
}

type debugInfo struct {
	lines     []string
	files     map[string]int
	locations map[string]int
	unroll    int
}

func (d *debugInfo) loop() int {
	if d.unroll == 0 {
		d.unroll = d.add("!{!\"llvm.loop.unroll.disable\"}")
	}
	id := d.add("")
	d.lines[id] = fmt.Sprintf("distinct !{!%d, !%d}", id, d.unroll)
	return id
}

func newDebug(module nativeplan.Module) *debugInfo {
	d := &debugInfo{lines: make([]string, 6), files: map[string]int{}, locations: map[string]int{}}
	path := module.SourcePath
	name, dir := filepath.Base(path), filepath.Dir(path)
	d.files[path] = 1
	d.lines[0] = "distinct !DICompileUnit(language: DW_LANG_C, file: !1, producer: \"Lo lo-native-2\", isOptimized: false, runtimeVersion: 0, emissionKind: FullDebug)"
	d.lines[1] = fmt.Sprintf("!DIFile(filename: %s, directory: %s)", quoted(name), quoted(dir))
	d.lines[2] = "!{i32 2, !\"Dwarf Version\", i32 4}"
	d.lines[3] = "!{i32 2, !\"Debug Info Version\", i32 3}"
	d.lines[4] = "!DISubroutineType(types: !5)"
	d.lines[5] = "!{null}"
	return d
}
func (d *debugInfo) add(text string) int {
	id := len(d.lines)
	d.lines = append(d.lines, text)
	return id
}
func (d *debugInfo) file(path string) int {
	if id, ok := d.files[path]; ok {
		return id
	}
	id := d.add(fmt.Sprintf("!DIFile(filename: %s, directory: %s)", quoted(filepath.Base(path)), quoted(filepath.Dir(path))))
	d.files[path] = id
	return id
}
func (d *debugInfo) function(f nativeplan.Function, site nativeplan.Location) int {
	file := d.file(site.File)
	return d.add(fmt.Sprintf("distinct !DISubprogram(name: %s, linkageName: %s, scope: !%d, file: !%d, line: %d, type: !4, scopeLine: %d, spFlags: DISPFlagDefinition, unit: !0)", quoted(f.Name), quoted(f.Symbol), file, file, site.Start.Line, site.Start.Line))
}
func (d *debugInfo) location(subprogram int, site nativeplan.Location) int {
	key := fmt.Sprintf("%d:%d", subprogram, site.ID)
	if id, ok := d.locations[key]; ok {
		return id
	}
	scope := subprogram
	if site.File != "" {
		scope = d.add(fmt.Sprintf("!DILexicalBlockFile(scope: !%d, file: !%d, discriminator: 0)", subprogram, d.file(site.File)))
	}
	column := site.Start.Column
	// LLVM stores debug columns in 16 bits; zero denotes an unknown column.
	if column >= 1<<16 {
		column = 0
	}
	id := d.add(fmt.Sprintf("!DILocation(line: %d, column: %d, scope: !%d)", site.Start.Line, column, scope))
	d.locations[key] = id
	return id
}
func (d *debugInfo) emit(out *output) {
	out.write("!llvm.dbg.cu = !{!0}\n!llvm.module.flags = !{!2, !3}\n")
	for id, line := range d.lines {
		out.write("!%d = %s\n", id, line)
	}
}
