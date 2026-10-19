package tensorfacts

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func usesTensors(p ir.Program) bool {
	for _, f := range p.Flows() {
		if tensorType(f.Signature().Output()) {
			return true
		}
		for _, parameter := range f.Parameters() {
			if tensorType(parameter.Type) {
				return true
			}
		}
		if tensorBody(f.Body()) {
			return true
		}
	}
	return tensorBody(p.Body())
}
func tensorType(t types.Type) bool {
	if t.Kind() == types.TensorKind {
		return true
	}
	switch x := t.(type) {
	case types.TensorBundle:
		return true
	case types.OptimizerState:
		return true
	case types.Fallible:
		return tensorType(x.Base())
	case types.List:
		return tensorType(x.Element())
	case types.Map:
		return tensorType(x.Value())
	case types.Record:
		for _, f := range x.Fields() {
			if tensorType(f.Type) {
				return true
			}
		}
	}
	return false
}
func tensorBody(body []ir.Statement) bool {
	for _, s := range body {
		switch x := s.(type) {
		case ir.Binding:
			if tensorExpression(x.Value()) {
				return true
			}
		case ir.Return:
			if tensorExpression(x.Value()) {
				return true
			}
		case ir.Commit:
			if tensorExpression(x.Target()) || tensorType(x.ResultType()) {
				return true
			}
		}
	}
	return false
}
func tensorExpression(e ir.Expression) bool {
	if tensorType(e.Type()) {
		return true
	}
	switch x := e.(type) {
	case ir.Conversion:
		return tensorExpression(x.Input())
	case ir.Projection:
		return tensorExpression(x.Input())
	case ir.FlowCall:
		for _, arg := range x.Arguments() {
			if tensorExpression(arg.Value()) {
				return true
			}
		}
	case ir.BuiltinCall:
		for _, arg := range x.Arguments() {
			if v, ok := arg.(ir.ValueArgument); ok && tensorExpression(v.Value()) {
				return true
			}
		}
	case ir.Match:
		if tensorExpression(x.Selector()) {
			return true
		}
		for _, arm := range x.Cases() {
			if tensorExpression(arm.Body()) {
				return true
			}
			if exact, ok := arm.(ir.ExactCase); ok && tensorExpression(exact.Head()) {
				return true
			}
		}
	}
	return false
}
