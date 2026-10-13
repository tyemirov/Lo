package check

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func typeExpression(expression syntax.TypeExpression) (types.Type, diagnostic.Diagnostic) {
	switch e := expression.(type) {
	case syntax.FallibleType:
		t, d := typeExpression(e.Base())
		if d != nil {
			return nil, d
		}
		return must(types.NewFallible(t)), nil
	case syntax.RecordType:
		fields := make([]types.Field, len(e.Fields()))
		seen := map[string]bool{}
		for i, f := range e.Fields() {
			if seen[f.Name] {
				return nil, problem(diagnostic.TypeConstraint, "duplicate record field "+f.Name, f.Span)
			}
			seen[f.Name] = true
			t, d := typeExpression(f.Type)
			if d != nil {
				return nil, d
			}
			fields[i] = types.Field{Name: f.Name, Type: t}
		}
		return must(types.NewRecord(fields)), nil
	case syntax.NamedType:
		if e.Name() == "Tensor" {
			if len(e.Arguments()) == 1 {
				if a, ok := e.Arguments()[0].(syntax.NamedType); ok && a.Name() == "FP32" && len(a.Arguments()) == 0 {
					return types.Tensor(), nil
				}
			}
			return nil, problem(diagnostic.TypeConstraint, "Tensor requires exactly the FP32 dtype argument", e.Span())
		}
		args := make([]types.Type, len(e.Arguments()))
		for i, arg := range e.Arguments() {
			t, d := typeExpression(arg)
			if d != nil {
				return nil, d
			}
			args[i] = t
		}
		arity := 0
		switch e.Name() {
		case "List", "Range":
			arity = 1
		case "Map":
			arity = 2
		default:
			if _, ok := types.Named(e.Name()); !ok {
				return nil, unknown("type", e.Name(), e.Span())
			}
		}
		if len(args) != arity {
			return nil, problem(diagnostic.TypeConstraint, fmt.Sprintf("type %s expects %d type arguments", e.Name(), arity), e.Span())
		}
		var t types.Type
		var err error
		switch e.Name() {
		case "List":
			t, err = types.NewList(args[0])
		case "Map":
			t, err = types.NewMap(args[0], args[1])
		case "Range":
			t, err = types.NewRange(args[0])
		default:
			t, _ = types.Named(e.Name())
		}
		if err != nil {
			return nil, problem(diagnostic.TypeConstraint, err.Error(), e.Span())
		}
		return t, nil
	default:
		panic("unknown type syntax")
	}
}
