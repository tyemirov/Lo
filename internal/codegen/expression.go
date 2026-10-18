package codegen

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"math"
	"strconv"
	"strings"
)

func (b *block) expression(expr ir.Expression) string {
	b.step()
	v := b.g.fresh()
	b.line("var %s value.Value", v)
	switch e := expr.(type) {
	case ir.Literal:
		switch e.Type().Kind() {
		case types.IntKind:
			if integer, err := strconv.ParseInt(e.Text(), 10, 64); err == nil {
				b.line("%s=value.Int64(%d)", v, integer)
			} else {
				b.line("%s,err=runtimeops.ParseInt(%q)", v, e.Text())
				b.check()
			}
		case types.StringKind:
			b.line("%s,err=value.NewText(types.String(),%q)", v, e.Text())
			b.check()
		case types.FloatKind:
			n, err := strconv.ParseFloat(e.Text(), 64)
			if err != nil {
				b.g.err = err
			}
			b.line("%s,err=value.NewFloat(math.Float64frombits(%d))", v, math.Float64bits(n))
			b.check()
		case types.BoolKind:
			b.line("%s=value.Bool(%s)", v, e.Text())
		case types.NullKind:
			b.line("%s=value.Null()", v)
		}
	case ir.Reference:
		name, ok := b.bindings[e.Binding()]
		if !ok {
			b.g.err = fmt.Errorf("missing native binding %s", e.Binding())
			name = "nil"
		}
		b.line("%s=%s", v, name)
	case ir.Projection:
		input := b.expression(e.Input())
		b.line("%s=value.Project(%s,%q)", v, input, e.Field())
	case ir.Conversion:
		input := b.expression(e.Input())
		t := b.g.typeOf(e.Type())
		switch e.Conversion() {
		case ir.Success:
			b.line("%s,err=value.Success(%s,%s)", v, t, input)
		case ir.Failure:
			b.line("%s,err=value.Failure(%s,%s.(value.Error))", v, t, input)
		case ir.Propagate:
			b.line("%s,err=runtimeops.Propagate(%s)", v, input)
		case ir.RecordView:
			b.line("%s=value.RecordView(%s.(value.Record))", v, input)
		case ir.FallibleRecordView:
			b.line("%s,err=runtimeops.FallibleRecord(%s,%s.(value.Fallible))", v, t, input)
		case ir.ScalarListView:
			b.line("%s,err=runtimeops.ScalarList(%s,%s.(value.Text))", v, t, input)
		default:
			b.g.err = fmt.Errorf("missing native conversion %d", e.Conversion())
		}
		b.check()
	case ir.FlowCall:
		args := make([]string, len(e.Arguments()))
		for _, a := range e.Arguments() {
			args[a.Position()] = b.expression(a.Value())
		}
		b.line("%s,err=%s", v, b.invoke(e.Flow(), args, e.Span()))
		b.check()
	case ir.Match:
		selector := b.expression(e.Selector())
		matched := b.g.fresh()
		b.line("%s:=false", matched)
		for _, arm := range e.Cases() {
			b.line("if !%s{", matched)
			condition := "true"
			switch arm := arm.(type) {
			case ir.ExactCase:
				head := b.expression(arm.Head())
				condition = "runtimeops.Match(" + selector + "," + head + ")"
			case ir.PredicateCase:
				test := b.g.fresh()
				b.line("%s,err:=%s", test, b.invoke(arm.Flow(), []string{selector}, arm.Span()))
				b.check()
				condition = test + ".(value.Boolean).Bool()"
			}
			b.line("if %s{", condition)
			result := b.expression(arm.Body())
			b.line("%s=%s;%s=true", v, result, matched)
			b.line("}}")
		}
	case ir.BuiltinCall:
		args := make([]string, len(e.Arguments()))
		argumentList := ""
		flows := map[int]identity.FlowID{}
		for i := range args {
			args[i] = "nil"
		}
		arguments := e.Arguments()
		valueArguments := make([]ir.ValueArgument, 0, len(arguments))
		for _, argument := range arguments {
			switch argument := argument.(type) {
			case ir.ValueArgument:
				valueArguments = append(valueArguments, argument)
			case ir.FlowArgument:
				flows[argument.Position()] = argument.Flow()
			}
		}
		if len(arguments) > argumentChunkSize {
			argumentList = b.arguments(valueArguments, len(args))
			for _, argument := range valueArguments {
				args[argument.Position()] = fmt.Sprintf("%s[%d]", argumentList, argument.Position())
			}
		} else {
			for _, argument := range valueArguments {
				args[argument.Position()] = b.expression(argument.Value())
			}
		}
		call := b.g.call(e)
		op := e.Operation().Name()
		if derivative, ok := e.(autodiff.Call); ok {
			b.line("%s,err=p.%s(s,%s)", v, b.g.gradient(derivative, call), strings.Join(args, ","))
		} else if e.Operation().Rule() == catalog.ListProcessor || e.Operation().Rule() == catalog.FlowProcessor {
			b.line("%s,err=p.%s(s,%s)", v, b.g.list(e, call, flows[len(args)-1], b.name), strings.Join(args[:len(args)-1], ","))
		} else {
			function, ok := operationFunctions[op]
			if !ok {
				b.g.err = fmt.Errorf("missing native operation %s", op)
				function = "Missing"
			}
			flowText := "nil"
			if len(flows) > 0 {
				pairs := []string{}
				for i := range args {
					if id, ok := flows[i]; ok {
						pairs = append(pairs, fmt.Sprintf("%d:identity.FlowID(%q)", i, id))
					}
				}
				flowText = "map[int]identity.FlowID{" + strings.Join(pairs, ",") + "}"
			}
			if argumentList == "" {
				argumentList = "[]value.Value{" + strings.Join(args, ",") + "}"
			}
			b.line("%s,err=runtimeops.%s(s,%s,%s,%s)", v, function, call, argumentList, flowText)
		}
		b.check()
	default:
		b.g.err = fmt.Errorf("missing native expression %T", e)
	}
	b.line("if err=s.ChargeValue(%s);err!=nil{return nil,err}", v)
	return v
}
func (b *block) invoke(id identity.FlowID, args []string, span source.Span) string {
	f := b.g.flowByID[id]
	values := []string{}
	for i, param := range f.Parameters() {
		values = append(values, args[i]+".("+valueType(param.Type)+")")
	}
	suffix := ""
	if len(values) > 0 {
		suffix = "," + strings.Join(values, ",")
	}
	return "p." + b.g.flows[id] + "(s," + b.g.span(span) + suffix + ")"
}
func (g *generator) call(call ir.BuiltinCall) string {
	labels := []string{}
	for _, label := range call.Labels() {
		labels = append(labels, strconv.Quote(label))
	}
	// The operation identity is checked at the compiler boundary.
	op := g.add("catalog.Operation", fmt.Sprintf("operation(%q)", call.Operation().Name()))
	return g.add("runtimeops.Call", fmt.Sprintf("runtimeops.NewCall(%s,%s,%s,[]string{%s})", g.typeOf(call.Type()), g.span(call.Span()), op, strings.Join(labels, ",")))
}
