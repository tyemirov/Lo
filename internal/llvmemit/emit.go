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
		var metadata map[string]json.RawMessage
		if err := json.Unmarshal(module.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("LLVM artifact metadata: %w", err)
		}
		metadata["artifact"] = artifact
		module.Metadata, err = json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("LLVM artifact metadata: %w", err)
		}
		if len(module.Metadata) > nativeplan.MaxModuleBytes {
			return nil, fmt.Errorf("%w: artifact metadata exceeds %d bytes", nativeplan.ErrLimit, nativeplan.MaxModuleBytes)
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
			chunk.WriteByte('\\')
			chunk.WriteByte(hex[value>>4])
			chunk.WriteByte(hex[value&15])
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
	for _, function := range module.Functions {
		emitFunction(out, debug, module, function)
	}
	out.write("attributes #0 = { noinline \"fp-contract\"=\"off\" \"frame-pointer\"=\"all\" }\n\n")
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

func emitFunction(out *output, debug *debugInfo, module nativeplan.Module, f nativeplan.Function) {
	e := &functionEmitter{o: out, debug: debug, module: module, f: f, edges: map[nativeplan.BlockID][]string{}}
	e.subprogram = debug.function(f, module.Locations[f.Site])
	for _, block := range f.Blocks {
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.Phi {
				for index, pred := range instruction.Predecessors {
					e.edges[pred] = append(e.edges[pred], fmt.Sprintf("%%phi_edge_%d_%d = load ptr, ptr %%v%d, align 8", instruction.Value, index, instruction.Inputs[index]))
				}
			}
		}
	}
	out.write("define void @%s(ptr %%context, ptr %%arguments, i64 %%argc, i64 %%callsite, ptr %%out) #0 !dbg !%d {\nentry:\n", f.Symbol, e.subprogram)
	for index := range f.Values {
		out.write("  %%v%d = alloca ptr, align 8\n  store ptr null, ptr %%v%d, align 8\n", index+1, index+1)
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
	for _, block := range f.Blocks {
		out.write("b%d:\n", block.ID)
		// LLVM phi instructions precede every other instruction in their block.
		for _, instruction := range block.Instructions {
			if instruction.Kind == nativeplan.Phi {
				parts := []string{}
				for index, pred := range instruction.Predecessors {
					parts = append(parts, fmt.Sprintf("[ %%phi_edge_%d_%d, %%b%d ]", instruction.Value, index, pred))
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
		old := e.payload(0)
		if i.Payload {
			value := e.payload(i.Result)
			e.line(i.Site, "call void @lo_value_retain(ptr %s)", value)
			e.line(i.Site, "call void @lo_value_release(ptr %s)", old)
			copy := e.fresh()
			e.line(i.Site, "%s = load %%lo.result, ptr %s, align 8", copy, e.result(i.Result))
			e.line(i.Site, "store %%lo.result %s, ptr %%out, align 8", copy)
		} else {
			e.line(i.Site, "call void @lo_value_release(ptr %s)", old)
			codePointer := e.field(i.Result, 1)
			code := e.fresh()
			e.line(i.Site, "%s = load i32, ptr %s, align 4", code, codePointer)
			statusOut := e.field(0, 0)
			codeOut := e.field(0, 1)
			valueOut := e.field(0, 2)
			e.line(i.Site, "store i32 2, ptr %s, align 4", statusOut)
			e.line(i.Site, "store i32 %s, ptr %s, align 4", code, codeOut)
			e.line(i.Site, "store ptr null, ptr %s, align 8", valueOut)
		}
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
		for index, owned := range e.f.Values {
			if owned {
				id := nativeplan.ValueID(index + 1)
				value := e.value(id)
				e.line(i.Site, "call void @lo_value_release(ptr %s)", value)
				e.line(i.Site, "store ptr null, ptr %%v%d, align 8", id)
			}
		}
	}
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
}

func newDebug(module nativeplan.Module) *debugInfo {
	d := &debugInfo{lines: make([]string, 6), files: map[string]int{}, locations: map[string]int{}}
	path := module.SourcePath
	name, dir := filepath.Base(path), filepath.Dir(path)
	d.files[path] = 1
	d.lines[0] = "distinct !DICompileUnit(language: DW_LANG_C, file: !1, producer: \"Lo lo-native-1\", isOptimized: false, runtimeVersion: 0, emissionKind: FullDebug)"
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
