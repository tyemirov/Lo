package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) list(op catalog.Operation, args []syntax.Argument, expected types.Type, span source.Span) (expression, diagnostic.Diagnostic) {
	if d := positional(args); d != nil {
		return expression{}, d
	}
	var element types.Type
	if list, ok := expected.(types.List); ok {
		element = list.Element()
	}
	values := make([]expression, len(args))
	nodes := make([]ir.Argument, len(args))
	for i, arg := range args {
		v, d := c.expression(arg.Value, element)
		if d != nil {
			return expression{}, d
		}
		if element == nil {
			element = v.Type()
		}
		values[i] = v
		nodes[i] = must(ir.ValueArg(i, v.node))
	}
	if element == nil {
		return expression{}, problem(diagnostic.MissingType, "empty List.new requires an expected List type", span)
	}
	return builtinResult(op, must(types.NewList(element)), nodes, nil, span, values...), nil
}
func (c *checker) record(op catalog.Operation, args []syntax.Argument, expected types.Type, span source.Span) (expression, diagnostic.Diagnostic) {
	fields := make([]types.Field, len(args))
	nodes := make([]ir.Argument, len(args))
	values := make([]expression, len(args))
	labels := make([]string, len(args))
	seen := map[string]bool{}
	for i, arg := range args {
		if arg.Name == "" {
			return expression{}, problem(diagnostic.UnknownArg, "Record.new requires named fields", arg.Span)
		}
		if seen[arg.Name] {
			return expression{}, problem(diagnostic.DuplicateArg, "duplicate field "+arg.Name, arg.Span)
		}
		seen[arg.Name] = true
		var fieldType types.Type
		if expected != nil {
			fieldType, _ = types.FieldType(expected, arg.Name)
		}
		value, d := c.expression(arg.Value, fieldType)
		if d != nil {
			return expression{}, d
		}
		fields[i] = types.Field{Name: arg.Name, Type: value.Type()}
		nodes[i] = must(ir.ValueArg(i, value.node))
		values[i] = value
		labels[i] = arg.Name
	}
	return builtinResult(op, must(types.NewRecord(fields)), nodes, labels, span, values...), nil
}
func mapEntry(key, value types.Type) types.Type {
	return must(types.NewRecord([]types.Field{{Name: "key", Type: key}, {Name: "value", Type: value}}))
}
func (c *checker) mapValue(op catalog.Operation, args []syntax.Argument, expected types.Type, span source.Span) (expression, diagnostic.Diagnostic) {
	if d := positional(args); d != nil {
		return expression{}, d
	}
	var key, value, entryType types.Type
	if m, ok := expected.(types.Map); ok {
		key = m.Key()
		value = m.Value()
		entryType = mapEntry(key, value)
	}
	values := make([]expression, len(args))
	nodes := make([]ir.Argument, len(args))
	for i, arg := range args {
		entry, d := c.expression(arg.Value, entryType)
		if d != nil {
			return expression{}, d
		}
		entry, d = c.unwrap(entry)
		if d != nil {
			return expression{}, d
		}
		r, ok := entry.Type().(types.Record)
		if !ok || len(r.Fields()) != 2 {
			return expression{}, diagnostic.Mismatch("Record{key: KeyType, value: ValueType}", entry.Type().String(), entry.Span())
		}
		k, hasKey := types.FieldType(r, "key")
		v, hasValue := types.FieldType(r, "value")
		if !hasKey || !hasValue {
			return expression{}, diagnostic.Mismatch("Record{key: KeyType, value: ValueType}", r.String(), entry.Span())
		}
		if !types.Comparable(k) {
			return expression{}, problem(diagnostic.TypeConstraint, "map key must be equality-comparable", entry.Span())
		}
		if key == nil {
			key = k
			value = v
			entryType = mapEntry(key, value)
		}
		values[i] = entry
		nodes[i] = must(ir.ValueArg(i, entry.node))
	}
	if key == nil {
		return expression{}, problem(diagnostic.MissingType, "empty Map.new requires an expected Map type", span)
	}
	return builtinResult(op, must(types.NewMap(key, value)), nodes, nil, span, values...), nil
}
func (c *checker) asList(value expression, element types.Type) (expression, diagnostic.Diagnostic) {
	v, d := c.unwrap(value)
	if d != nil {
		return expression{}, d
	}
	if v.Type().Kind() == types.StringKind {
		v.node = must(ir.NewConversion(must(types.NewList(types.Scalar())), v.node, ir.ScalarListView))
	}
	list, ok := v.Type().(types.List)
	if !ok {
		return expression{}, diagnostic.Mismatch("List", v.Type().String(), v.Span())
	}
	if element != nil && !types.Equal(list.Element(), element) {
		return expression{}, diagnostic.Mismatch(must(types.NewList(element)).String(), list.String(), v.Span())
	}
	return v, nil
}
func (c *checker) listProcessor(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	if d := positional(args); d != nil {
		return expression{}, d
	}
	minimum := 2
	if op.Name() == catalog.ListFold || op.Name() == catalog.FlowFold || op.Name() == catalog.FlowWorklist || op.Name() == catalog.ListUnfold {
		minimum = 3
	}
	if len(args) < minimum {
		return expression{}, problem(diagnostic.ArgumentCount, op.Name()+" requires values and a final flow reference", span)
	}
	f, d := c.flowArgument(args[len(args)-1])
	if d != nil {
		return expression{}, d
	}
	if effects := f.graph.Effects(); (effects.Reads || effects.Commits) && op.Rule() != catalog.FlowProcessor {
		return expression{}, problem(diagnostic.EffectfulCallback, "list callback "+f.declaration.Name()+" can read host state or activate a commit", args[len(args)-1].Span)
	}
	params := f.signature.Parameters()
	paramCount := len(args) - 1
	if op.Name() == catalog.ListUnfold {
		paramCount--
	}
	if len(params) != paramCount {
		return expression{}, problem(diagnostic.ArgumentCount, "list callback has the wrong parameter count", args[len(args)-1].Span)
	}
	values := make([]expression, len(args)-1)
	nodes := make([]ir.Argument, len(args))
	contextStart := 1
	if minimum == 3 {
		contextStart = 2
	}
	contextCount := len(args) - contextStart - 1
	// Argument positions describe the processor signature; callback parameters put context first.
	for i := range values {
		var t types.Type
		switch {
		case i >= contextStart:
			t = params[i-contextStart].Type
		case op.Name() == catalog.ListUnfold && i == 0:
			t = params[len(params)-1].Type
		case op.Name() == catalog.ListUnfold && i == 1:
			t = types.Int()
		case i == 0:
			t = must(types.NewList(params[len(params)-1].Type))
		case i == 1 && (op.Name() == catalog.ListFold || op.Rule() == catalog.FlowProcessor):
			t = params[contextCount].Type
		}
		var v expression
		var d diagnostic.Diagnostic
		if op.Name() == catalog.FlowWorklist && i == 0 {
			v, d = c.expression(args[i].Value, t)
		} else {
			v, d = c.processorArgument(args[i], t)
		}
		if d != nil {
			return expression{}, d
		}
		if i == 0 && op.Name() != catalog.ListUnfold {
			v, d = c.asList(v, params[len(params)-1].Type)
			if d != nil {
				return expression{}, d
			}
		}
		values[i] = v
		nodes[i] = must(ir.ValueArg(i, v.node))
	}
	output := baseType(f.signature.Output())
	var result types.Type
	switch op.Name() {
	case catalog.ListMap:
		result = must(types.NewList(output))
	case catalog.ListFilter:
		if !types.Equal(output, types.Bool()) {
			return expression{}, diagnostic.Mismatch(types.Bool().String(), f.signature.Output().String(), args[len(args)-1].Span)
		}
		result = values[0].Type()
	case catalog.ListFold, catalog.FlowFold:
		if !types.Equal(output, values[1].Type()) {
			return expression{}, diagnostic.Mismatch(values[1].Type().String(), f.signature.Output().String(), args[len(args)-1].Span)
		}
		result = values[1].Type()
	case catalog.FlowWorklist:
		if values[1].Type().Kind() == types.FallibleKind {
			return expression{}, diagnostic.Mismatch("non-fallible worklist state", values[1].Type().String(), args[1].Span)
		}
		expected := must(types.NewRecord([]types.Field{
			{Name: "state", Type: values[1].Type()},
			{Name: "items", Type: values[0].Type()},
			{Name: "stop", Type: types.Bool()},
		}))
		if !types.Equal(output, expected) {
			return expression{}, diagnostic.Mismatch(expected.String(), f.signature.Output().String(), args[len(args)-1].Span)
		}
		result = values[1].Type()
	case catalog.ListFlatMap:
		if output.Kind() != types.ListKind {
			return expression{}, diagnostic.Mismatch("List", f.signature.Output().String(), args[len(args)-1].Span)
		}
		result = output
	case catalog.ListUnfold:
		record, ok := output.(types.Record)
		if !ok || len(record.Fields()) != 2 {
			return expression{}, diagnostic.Mismatch("Record{value: ElementType, next_state: StateType}", output.String(), args[len(args)-1].Span)
		}
		element, hasValue := types.FieldType(record, "value")
		state, hasState := types.FieldType(record, "next_state")
		if !hasValue || !hasState {
			return expression{}, diagnostic.Mismatch("Record{value: ElementType, next_state: StateType}", output.String(), args[len(args)-1].Span)
		}
		if !types.Equal(state, values[0].Type()) {
			return expression{}, diagnostic.Mismatch(values[0].Type().String(), state.String(), args[len(args)-1].Span)
		}
		result = must(types.NewList(element))
	default:
		panic("unknown list processor")
	}
	if f.signature.Output().Kind() == types.FallibleKind {
		result = must(types.NewFallible(result))
	}
	nodes[len(args)-1] = must(ir.FlowArg(len(args)-1, identity.FlowID(f.entry.ID()), args[len(args)-1].Span))
	value := builtinResult(op, result, nodes, nil, span, values...)
	value.effects = value.effects.With(f.graph.Effects())
	return value, nil
}
