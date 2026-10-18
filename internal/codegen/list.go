package codegen

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"strings"
)

func (g *generator) list(call ir.BuiltinCall, metadata string, callback identity.FlowID, flowName string) string {
	name := "list" + g.fresh()
	b := &block{g: g, name: flowName}
	args, params := []string{}, []string{}
	for i := 0; i < len(call.Arguments())-1; i++ {
		v := fmt.Sprintf("a%d", i)
		args = append(args, v)
		params = append(params, v+" value.Value")
	}
	op := call.Operation().Name()
	contextStart := 1
	b.line("var err error;_ = err;var state value.Value;var items []value.Value;var count int;output:=[]value.Value{};_ = output;_ = items")
	if op == catalog.ListUnfold {
		contextStart = 2
		b.line("state=a0;n:=a1.(value.Integer).Big();if n.Sign()>0{if !n.IsInt64()||n.Int64()>execution.MaxItems{return nil,execution.Limit(\"unfold elements\")};count=int(n.Int64())}")
	} else {
		b.line("items=a0.(value.List).Items();count=len(items)")
		if op == catalog.ListFold || op == catalog.FlowFold {
			contextStart = 2
			b.line("state=a1")
		}
	}
	if op == catalog.FlowFold {
		b.line("var progress execution.FlowProgress;progress,err=s.RecordFlowProgress(count,0,state);if err!=nil{return nil,err};defer func(){resultErr=execution.WithFlowProgress(resultErr,progress)}()")
	}
	b.line("for index:=0;index<count;index++{")
	b.step()
	cbargs := append([]string{}, args[contextStart:]...)
	if op == catalog.ListFold || op == catalog.FlowFold || op == catalog.ListUnfold {
		cbargs = append(cbargs, "state")
	}
	element := "nil"
	if op != catalog.ListUnfold {
		cbargs = append(cbargs, "items[index]")
		element = "items[index]"
	}
	b.line("v,err:=%s", b.invoke(callback, cbargs, call.Span()))
	b.check()
	b.line("if wrapped,ok:=v.(value.Fallible);ok{var cause value.Error;v,cause=wrapped.Outcome();if cause!=nil{return runtimeops.CallbackFailure(%s,cause,%q,index,%s,state),nil}}", metadata, flowName, element)
	switch op {
	case catalog.ListMap:
		b.line("output=append(output,v)")
	case catalog.ListFilter:
		b.line("if v.(value.Boolean).Bool(){output=append(output,items[index])}")
	case catalog.ListFold, catalog.FlowFold:
		b.line("state=v")
	case catalog.ListFlatMap:
		b.line("extra:=v.(value.List).Items();if len(extra)>execution.MaxItems-len(output){return nil,execution.Limit(\"list elements\")};output=append(output,extra...)")
	case catalog.ListUnfold:
		b.line("record:=v.(value.Record);output=append(output,record.Field(\"value\"));scope.Retain(record.Field(\"value\"));state=record.Field(\"next_state\")")
	}
	if op == catalog.ListFold || op == catalog.FlowFold || op == catalog.ListUnfold {
		b.line("scope.Keep(state)")
	}
	if op == catalog.FlowFold {
		b.line("next,recordErr:=s.RecordFlowProgress(count,index+1,state);if recordErr!=nil{return nil,recordErr};progress=next")
	}
	b.line("}")
	if op == catalog.ListFold || op == catalog.FlowFold {
		b.line("return runtimeops.Success(%s,state),nil", g.typeOf(call.Type()))
	} else {
		out := call.Type()
		if ft, ok := out.(types.Fallible); ok {
			out = ft.Base()
		}
		b.line("result,err:=runtimeops.List(%s,output)", g.typeOf(out))
		b.check()
		b.line("return runtimeops.Success(%s,result),nil", g.typeOf(call.Type()))
	}
	fmt.Fprintf(&g.functions, "func(p *program)%s(s execution.Session,%s)(resultValue value.Value,resultErr error){scope:=s.Scope();defer func(){scope.Close(resultValue)}();%s}\n", name, strings.Join(params, ","), b.out.String())
	return name
}
