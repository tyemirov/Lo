package codegen

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"strings"
)

func (g *generator) gradient(call autodiff.Call, metadata string) string {
	name := "gradient" + g.fresh()
	b := &block{g: g, bindings: map[ir.BindingID]string{}}
	params, args := []string{}, []string{}
	for i := range call.Arguments() {
		v := fmt.Sprintf("a%d", i)
		params = append(params, v+" value.Value")
		args = append(args, v)
	}
	for i, p := range call.Parameters() {
		argument := i + 1
		if i == len(call.Parameters())-1 {
			argument = 0
		}
		b.bindings[p.ID] = args[argument]
	}
	b.line("for _,arg:=range []value.Value{%s}{if !value.HostPublishable(arg){return runtimeops.OperationFailure(%s,[]value.Value{%s},device.UnsupportedOperation,\"device autodiff requires the selected device backend\"),nil}}", strings.Join(args, ","), metadata, strings.Join(args, ","))
	nodes := call.Nodes()
	b.line("var err error;_ = err;tape:=runtimeops.NewTape(s);defer tape.Close();maths:=runtimeops.Arithmetic(s);adjoints:=make([]tensor.Data,%d)", len(nodes)+1)
	seenFrames := map[int]bool{}
	for _, node := range nodes {
		for _, frame := range node.Frames() {
			if seenFrames[frame.Invocation] {
				continue
			}
			seenFrames[frame.Invocation] = true
			fields := []string{}
			for _, a := range frame.Arguments {
				fields = append(fields, fmt.Sprintf("{Name:%q,Value:%s}", a.Name, b.bindings[a.Binding]))
			}
			b.line("if err=tape.Snapshot(%d,[]value.Field{%s});err!=nil{return nil,err}", frame.Invocation, strings.Join(fields, ","))
		}
		inner := &block{g: g, bindings: b.bindings}
		v := inner.expression(node.Expression())
		inner.line("return %s,nil", v)
		name := g.fresh()
		b.line("%s,err:=func()(value.Value,error){%s}()", name, inner.out.String())
		b.line("if err!=nil{%s}", b.gradientFailure(call, node))
		b.bindings[node.Binding()] = name
		b.line("_ = %s", name)
		if node.Retained() {
			b.line("if err=tape.Retain(%s);err!=nil{return nil,err}", name)
		}
		for _, id := range node.Release() {
			b.line("%s=nil", b.bindings[id])
		}
	}
	loss := b.bindings[call.Result()]
	b.line("loss:=%s.(value.Tensor);if loss.Data().Shape().Rank()!=0{return runtimeops.OperationFailure(%s,[]value.Value{%s},catalog.InvalidLoss,\"loss must return a rank-zero tensor\"),nil}", loss, metadata, strings.Join(args, ","))
	for _, selected := range call.Selections() {
		node := nodes[selected.Node-1]
		b.line("adjoints[%d],err=maths.Constant(%s.(value.Tensor).Data().Shape(),0);if err!=nil{%s}", selected.Node, b.bindings[node.Binding()], b.gradientFailure(call, node))
	}
	b.line("adjoints[%d],err=maths.Constant(loss.Data().Shape(),1);if err!=nil{%s}", call.ResultNode(), b.gradientFailure(call, nodes[call.ResultNode()-1]))
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		if !node.Reverse() || len(node.Edges()) == 0 {
			continue
		}
		b.line("if gradient:=adjoints[%d];gradient!=nil{", node.ID())
		if _, conversion := node.Expression().(ir.Conversion); conversion {
			for _, edge := range node.Edges() {
				b.line("{contribution:=gradient;if previous:=adjoints[%d];previous!=nil{contribution,err=maths.Apply(catalog.TensorAdd,previous,contribution,nil);if err!=nil{%s}};adjoints[%d]=contribution}", edge.Node, b.gradientFailure(call, node), edge.Node)
			}
		} else {
			operands := node.Operands()
			b.line("left:=%s.(value.Tensor).Data();var right,result tensor.Data;var indices []int64", b.bindings[operands[0]])
			if len(operands) > 1 {
				b.line("switch v:=%s.(type){case value.Tensor:right=v.Data();case value.List:for _,item:=range v.Items(){indices=append(indices,item.(value.Integer).Big().Int64())}}", b.bindings[operands[1]])
			}
			if node.Operation() == catalog.TensorSqrt {
				b.line("v,_:=%s.(value.Fallible).Outcome();result=v.(value.Tensor).Data()", b.bindings[node.Binding()])
			}
			for _, edge := range node.Edges() {
				b.line("{contribution,err:=derivative.VJP(maths,%q,%d,gradient,left,right,result,indices);if err!=nil{%s};if previous:=adjoints[%d];previous!=nil{contribution,err=maths.Apply(catalog.TensorAdd,previous,contribution,nil);if err!=nil{%s}};adjoints[%d]=contribution}", node.Operation(), edge.Operand, b.gradientFailure(call, node), edge.Node, b.gradientFailure(call, node), edge.Node)
			}
		}
		b.line("adjoints[%d]=nil}", node.ID())
	}
	b.line("fields:=[]value.Field{}")
	for _, selected := range call.Selections() {
		b.line("{v,err:=value.NewTensor(adjoints[%d]);if err!=nil{return nil,err};fields=append(fields,value.Field{Name:%q,Value:v})}", selected.Node, selected.Key)
	}
	b.line("gradients,err:=value.NewRecord(fields)")
	b.check()
	b.line("result,err:=value.NewRecord([]value.Field{{Name:catalog.ParamValue,Value:loss},{Name:catalog.FieldGradients,Value:gradients}})")
	b.check()
	b.line("return runtimeops.Success(%s,result),nil", g.typeOf(call.Type()))
	fmt.Fprintf(&g.functions, "func(p *program)%s(s execution.Session,%s)(resultValue value.Value,resultErr error){scope:=s.Scope();defer func(){scope.Close(resultValue)}();%s}\n", name, strings.Join(params, ","), b.out.String())
	return name
}
func (b *block) gradientFailure(call autodiff.Call, node autodiff.Node) string {
	operands, frames := []string{}, []string{}
	for _, id := range node.Operands() {
		name, ok := b.bindings[id]
		if !ok {
			name = "nil"
		}
		operands = append(operands, name)
	}
	for _, frame := range node.Frames() {
		frames = append(frames, fmt.Sprintf("{Invocation:%d,Name:%q}", frame.Invocation, frame.Name))
	}
	return fmt.Sprintf("return tape.Fail(err,%s,%q,%s,[]value.Value{%s},[]runtimeops.GradientFrame{%s})", b.g.typeOf(call.Type()), node.Operation(), b.g.span(node.Expression().Span()), strings.Join(operands, ","), strings.Join(frames, ","))
}
