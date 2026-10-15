// Package codegen lowers checked programs into direct Go control flow.
package codegen

import (
	"bytes"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

const MaxSourceBytes = 32 << 20

type metadata struct{ name, kind, expression string }
type generator struct {
	program   ir.Program
	flows     map[identity.FlowID]string
	flowByID  map[identity.FlowID]ir.Flow
	metadata  []metadata
	types     map[string]string
	functions strings.Builder
	next      int
	err       error
}
type block struct {
	g        *generator
	out      strings.Builder
	bindings map[ir.BindingID]string
	name     string
}

func Generate(program ir.Program, packageName string) ([]byte, error) {
	if program == nil || !token.IsIdentifier(packageName) {
		return nil, fmt.Errorf("generation requires a checked program and package name")
	}
	g := &generator{program: program, flows: map[identity.FlowID]string{}, flowByID: map[identity.FlowID]ir.Flow{}, types: map[string]string{}}
	for i, f := range program.Flows() {
		g.flows[f.ID()] = fmt.Sprintf("flow%d", i)
		g.flowByID[f.ID()] = f
	}
	top := &block{g: g, bindings: map[ir.BindingID]string{}}
	top.body(program.Body())
	fmt.Fprintf(&g.functions, "func(p *program)top(s execution.Session)(resultValue value.Value,resultErr error){scope:=s.Scope();defer func(){scope.Close(resultValue)}();var err error;_ = err\n%s}\n", top.out.String())
	for _, f := range program.Flows() {
		g.flow(f)
	}
	handlers := []string{}
	for _, f := range program.Flows() {
		arguments, params := []string{}, []string{}
		for i, parameter := range f.Parameters() {
			arguments = append(arguments, fmt.Sprintf("args[%d].(%s)", i, valueType(parameter.Type)))
			params = append(params, "{Name:"+strconv.Quote(parameter.Name)+",Type:"+g.typeOf(parameter.Type)+"}")
		}
		suffix := ""
		if len(arguments) > 0 {
			suffix = "," + strings.Join(arguments, ",")
		}
		handlers = append(handlers, fmt.Sprintf("{ID:identity.FlowID(%q),Name:%q,Parameters:[]types.Parameter{%s},Span:%s,Call:func(s execution.Session,args []value.Value,span source.Span)(value.Value,error){return p.%s(s,span%s)}}", f.ID(), f.Name(), strings.Join(params, ","), g.span(f.Span()), g.flows[f.ID()], suffix))
	}
	if g.err != nil {
		return nil, g.err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "package %s\n", packageName)
	out.WriteString(`import("fmt";"math";"github.com/MarcoPoloResearchLab/Lo/internal/catalog";"github.com/MarcoPoloResearchLab/Lo/internal/derivative";"github.com/MarcoPoloResearchLab/Lo/internal/device";"github.com/MarcoPoloResearchLab/Lo/internal/tensor";"github.com/MarcoPoloResearchLab/Lo/internal/execution";"github.com/MarcoPoloResearchLab/Lo/internal/identity";"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops";"github.com/MarcoPoloResearchLab/Lo/internal/source";"github.com/MarcoPoloResearchLab/Lo/internal/types";"github.com/MarcoPoloResearchLab/Lo/internal/value")
func operation(name string)(catalog.Operation,error){op,ok:=catalog.Lookup(name);if !ok{return nil,fmt.Errorf("unknown compiled operation %s",name)};return op,nil}
var _=math.Float64frombits
var _=fmt.Errorf
var _=catalog.Lookup
var _=derivative.VJP
var _=device.CPU
var _ tensor.Data
var _ identity.FlowID
var _=runtimeops.Match
var _ types.Type
var _ source.Span
type program struct{
`)
	for _, m := range g.metadata {
		fmt.Fprintf(&out, "%s %s\n", m.name, m.kind)
	}
	out.WriteString("}\nfunc New()(execution.Program,error){p:=&program{};var err error;_ = err\n")
	for _, m := range g.metadata {
		fmt.Fprintf(&out, "p.%s,err=%s;if err!=nil{return nil,fmt.Errorf(\"initialize compiled metadata: %%w\",err)}\n", m.name, m.expression)
	}
	fmt.Fprintf(&out, "return execution.NewProgram(p.top,[]execution.Handler{%s})}\n", strings.Join(handlers, ","))
	out.WriteString(g.functions.String())
	if out.Len() > MaxSourceBytes {
		return nil, fmt.Errorf("generated source exceeds %d bytes", MaxSourceBytes)
	}
	set := token.NewFileSet()
	tree, err := parser.ParseFile(set, "program.go", out.String(), parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("generated Go syntax: %w", err)
	}
	var result bytes.Buffer
	if err := format.Node(&result, set, tree); err != nil {
		return nil, fmt.Errorf("format generated Go: %w", err)
	}
	if result.Len() > MaxSourceBytes {
		return nil, fmt.Errorf("formatted source exceeds %d bytes", MaxSourceBytes)
	}
	return result.Bytes(), nil
}
func (g *generator) fresh() string { g.next++; return fmt.Sprintf("v%d", g.next) }
func (g *generator) add(kind, expression string) string {
	name := fmt.Sprintf("m%d", len(g.metadata))
	g.metadata = append(g.metadata, metadata{name, kind, expression})
	return "p." + name
}
func (g *generator) span(span source.Span) string {
	return g.add("source.Span", fmt.Sprintf("source.NewSpan(%q,source.Position{Offset:%d,Line:%d,Column:%d},source.Position{Offset:%d,Line:%d,Column:%d})", span.File(), span.Start().Offset, span.Start().Line, span.Start().Column, span.End().Offset, span.End().Line, span.End().Column))
}
func (g *generator) typeOf(t types.Type) string {
	if name, ok := g.types[t.String()]; ok {
		return name
	}
	atoms := map[types.Kind]string{types.IntKind: "Int", types.StringKind: "String", types.BoolKind: "Bool", types.NullKind: "Null", types.ScalarKind: "Scalar", types.BytesKind: "Bytes", types.FloatKind: "Float", types.TensorKind: "Tensor", types.DeviceKind: "Device", types.ErrorKind: "Error", types.PathKind: "Path", types.RequestKind: "Request", types.ResponseKind: "Response", types.ServerKind: "Server", types.EffectKind: "Effect", types.AnyRecordKind: "AnyRecord", types.SGDConfigKind: "SGDConfig", types.AdamWConfigKind: "AdamWConfig"}
	if atom, ok := atoms[t.Kind()]; ok {
		return "types." + atom + "()"
	}
	var kind, expression string
	switch t := t.(type) {
	case types.List:
		kind = "types.List"
		expression = "types.NewList(" + g.typeOf(t.Element()) + ")"
	case types.Map:
		kind = "types.Map"
		expression = "types.NewMap(" + g.typeOf(t.Key()) + "," + g.typeOf(t.Value()) + ")"
	case types.Range:
		kind = "types.Range"
		expression = "types.NewRange(" + g.typeOf(t.Element()) + ")"
	case types.Fallible:
		kind = "types.Fallible"
		expression = "types.NewFallible(" + g.typeOf(t.Base()) + ")"
	case types.Record:
		kind = "types.Record"
		fields := []string{}
		for _, f := range t.Fields() {
			fields = append(fields, "{Name:"+strconv.Quote(f.Name)+",Type:"+g.typeOf(f.Type)+"}")
		}
		expression = "types.NewRecord([]types.Field{" + strings.Join(fields, ",") + "})"
	case types.TensorBundle:
		kind = "types.TensorBundle"
		expression = fmt.Sprintf("types.NewTensorBundle(types.Kind(%q),%s)", t.Kind(), g.typeOf(t.Schema()))
	case types.OptimizerState:
		kind = "types.OptimizerState"
		expression = fmt.Sprintf("types.NewOptimizerState(types.Kind(%q),%s)", t.Kind(), g.typeOf(t.Schema()))
	default:
		g.err = fmt.Errorf("missing native type %s", t)
		return "nil"
	}
	name := g.add(kind, expression)
	g.types[t.String()] = name
	return name
}
func valueType(t types.Type) string {
	switch t.Kind() {
	case types.IntKind:
		return "value.Integer"
	case types.FloatKind:
		return "value.Float"
	case types.StringKind, types.ScalarKind, types.PathKind:
		return "value.Text"
	case types.BoolKind:
		return "value.Boolean"
	case types.ListKind:
		return "value.List"
	case types.MapKind:
		return "value.Map"
	case types.RecordKind, types.AnyRecordKind:
		return "value.Record"
	case types.TensorKind:
		return "value.Tensor"
	case types.BytesKind:
		return "value.Bytes"
	case types.FallibleKind:
		return "value.Fallible"
	case types.ErrorKind:
		return "value.Error"
	case types.RequestKind, types.ResponseKind:
		return "value.HTTPValue"
	case types.ParametersKind, types.GradientsKind:
		return "value.TensorBundle"
	case types.SGDStateKind, types.AdamWStateKind:
		return "value.OptimizerState"
	default:
		return "value.Value"
	}
}
func (b *block) line(text string, args ...any) { fmt.Fprintf(&b.out, text+"\n", args...) }
func (b *block) check()                        { b.line("if err!=nil{return nil,err}") }
func (b *block) step()                         { b.line("if err=s.Step();err!=nil{return nil,err}") }
func (b *block) body(statements []ir.Statement) {
	returned := false
	for _, statement := range statements {
		b.step()
		switch s := statement.(type) {
		case ir.Binding:
			v := b.expression(s.Value())
			b.bindings[s.ID()] = v
			b.line("_ = %s", v)
		case ir.Return:
			v := b.expression(s.Value())
			b.line("return %s,nil", v)
			returned = true
		case ir.Commit:
			v := b.expression(s.Target())
			b.line("if err=runtimeops.Commit(s,%s.(value.Target),%s);err!=nil{return nil,err}", v, b.g.span(s.Span()))
		default:
			b.g.err = fmt.Errorf("unknown checked statement %T", s)
		}
	}
	if !returned {
		b.line("return nil,nil")
	}
}
func (g *generator) flow(f ir.Flow) {
	b := &block{g: g, bindings: map[ir.BindingID]string{}, name: f.Name()}
	params, args, fields := []string{}, []string{}, []string{}
	for i, param := range f.Parameters() {
		name := fmt.Sprintf("arg%d", i)
		params = append(params, name+" "+valueType(param.Type))
		args = append(args, name)
		b.bindings[param.ID] = name
		fields = append(fields, fmt.Sprintf("{Name:%q,Value:%s}", param.Name, name))
	}
	suffix := ""
	arguments := ""
	if len(params) > 0 {
		suffix = "," + strings.Join(params, ",")
		arguments = "," + strings.Join(args, ",")
	}
	name := g.flows[f.ID()]
	b.body(f.Body())
	fmt.Fprintf(&g.functions, "func(p *program)%sBody(s execution.Session%s)(resultValue value.Value,resultErr error){scope:=s.Scope();defer func(){scope.Close(resultValue)}();var err error;_ = err\n%s}\n", name, suffix, b.out.String())
	fmt.Fprintf(&g.functions, "func(p *program)%s(s execution.Session,span source.Span%s)(value.Value,error){if err:=s.Enter();err!=nil{return nil,err};defer s.Leave();result,err:=p.%sBody(s%s);cause,host:=runtimeops.Cause(result,err);if host!=nil{return nil,host};if cause==nil{return result,nil};input,err:=value.NewRecord([]value.Field{%s});if err!=nil{return nil,err};cause=value.WithFrame(cause,%q,cause.Operation(),input)\n", name, suffix, name, arguments, strings.Join(fields, ","), f.Name())
	if f.Handler() != "" {
		fmt.Fprintf(&g.functions, "return p.%s(s,span,cause)}\n", g.flows[f.Handler()])
	} else if ft, ok := f.Signature().Output().(types.Fallible); ok {
		fmt.Fprintf(&g.functions, "return value.Failure(%s,value.At(cause,%q,span))}\n", g.typeOf(ft), f.Name())
	} else {
		fmt.Fprintln(&g.functions, "return nil,fmt.Errorf(\"unhandled failure in compiled total flow\")} ")
	}
}
