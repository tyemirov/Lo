package check

import (
	"fmt"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type arguments struct {
	source     []syntax.Argument
	positions  []int
	byPosition []syntax.Argument
}

func bindArguments(name string, parameters []types.Parameter, args []syntax.Argument, span source.Span) (arguments, diagnostic.Diagnostic) {
	result := arguments{source: args, positions: make([]int, len(args)), byPosition: make([]syntax.Argument, len(parameters))}
	seen := make([]bool, len(parameters))
	named := false
	for i, arg := range args {
		pos := i
		if arg.Name != "" {
			named = true
			pos = -1
			for j, p := range parameters {
				if p.Name == arg.Name {
					pos = j
					break
				}
			}
			if pos < 0 {
				return arguments{}, problem(diagnostic.UnknownArg, "unknown argument "+arg.Name+" for "+name, arg.Span)
			}
		} else if named {
			return arguments{}, problem(diagnostic.ArgumentOrder, "positional argument follows a named argument", arg.Span)
		}
		if pos >= len(parameters) {
			return arguments{}, problem(diagnostic.ArgumentCount, fmt.Sprintf("%s expects %d arguments", name, len(parameters)), span)
		}
		if seen[pos] {
			return arguments{}, problem(diagnostic.DuplicateArg, "duplicate argument "+parameters[pos].Name, arg.Span)
		}
		seen[pos] = true
		result.positions[i] = pos
		result.byPosition[pos] = arg
	}
	if len(args) != len(parameters) {
		return arguments{}, problem(diagnostic.ArgumentCount, fmt.Sprintf("%s expects %d arguments, got %d", name, len(parameters), len(args)), span)
	}
	return result, nil
}
func positional(args []syntax.Argument) diagnostic.Diagnostic {
	for _, arg := range args {
		if arg.Name != "" {
			return problem(diagnostic.UnknownArg, "operation accepts positional arguments only", arg.Span)
		}
	}
	return nil
}
func builtinResult(op catalog.Operation, out types.Type, args []ir.Argument, labels []string, span source.Span, values ...expression) expression {
	result := expression{node: must(ir.NewBuiltin(out, op, args, labels, span))}
	for _, value := range values {
		result = result.with(value)
	}
	if op.Boundary() == catalog.Read {
		result.effects.Reads = true
	}
	return result
}
func boundBuiltin(op catalog.Operation, out types.Type, args arguments, values []expression, span source.Span) expression {
	nodes := make([]ir.Argument, len(args.source))
	for i, pos := range args.positions {
		nodes[i] = must(ir.ValueArg(pos, values[pos].node))
	}
	return builtinResult(op, out, nodes, nil, span, values...)
}
func (c *checker) processorArgument(arg syntax.Argument, expected types.Type) (expression, diagnostic.Diagnostic) {
	value, d := c.rawExpression(arg.Value, expected)
	if d != nil {
		return expression{}, d
	}
	if list, ok := expected.(types.List); ok && types.Equal(list.Element(), types.Scalar()) {
		if types.Equal(baseType(value.Type()), types.String()) {
			value, d = c.unwrap(value)
			if d != nil {
				return expression{}, d
			}
			value.node = must(ir.NewConversion(expected, value.node, ir.ScalarListView))
		}
	}
	return c.coerce(value, expected)
}
func (c *checker) call(name string, args []syntax.Argument, expected types.Type, span source.Span) (expression, diagnostic.Diagnostic) {
	if op, ok := catalog.Lookup(name); ok {
		return c.builtin(op, args, expected, span)
	}
	f, d := c.namedFlow(name, span)
	if d != nil {
		return expression{}, d
	}
	bound, d := bindArguments(name, f.signature.Parameters(), args, span)
	if d != nil {
		return expression{}, d
	}
	result := expression{effects: f.graph.Effects()}
	values := make([]ir.ValueArgument, len(args))
	parameters := f.signature.Parameters()
	for i, arg := range bound.source {
		pos := bound.positions[i]
		v, d := c.expression(arg.Value, parameters[pos].Type)
		if d != nil {
			return expression{}, d
		}
		values[i] = must(ir.ValueArg(pos, v.node))
		result = result.with(v)
	}
	result.node = must(ir.NewFlowCall(f.signature.Output(), ir.FlowID(f.entry.ID()), values, span))
	return result, nil
}
func (c *checker) builtin(op catalog.Operation, args []syntax.Argument, expected types.Type, span source.Span) (expression, diagnostic.Diagnostic) {
	switch op.Rule() {
	case catalog.ListConstructor:
		return c.list(op, args, expected, span)
	case catalog.MapConstructor:
		return c.mapValue(op, args, expected, span)
	case catalog.RecordConstructor:
		return c.record(op, args, expected, span)
	case catalog.StringConcat:
		if d := positional(args); d != nil {
			return expression{}, d
		}
		values := make([]expression, len(args))
		nodes := make([]ir.Argument, len(args))
		for i, arg := range args {
			v, d := c.expression(arg.Value, types.String())
			if d != nil {
				return expression{}, d
			}
			values[i] = v
			nodes[i] = must(ir.ValueArg(i, v.node))
		}
		return builtinResult(op, types.String(), nodes, nil, span, values...), nil
	case catalog.ListProcessor:
		return c.listProcessor(op, args, span)
	}
	bound, d := bindArguments(op.Name(), op.Parameters(), args, span)
	if d != nil {
		return expression{}, d
	}
	values := make([]expression, len(args))
	check := func(pos int, t types.Type) diagnostic.Diagnostic {
		v, d := c.processorArgument(bound.byPosition[pos], t)
		if d != nil {
			return d
		}
		values[pos] = v
		return nil
	}
	total := func(pos int) diagnostic.Diagnostic {
		if d := check(pos, nil); d != nil {
			return d
		}
		v, d := c.unwrap(values[pos])
		if d != nil {
			return d
		}
		values[pos] = v
		return nil
	}
	var out types.Type
	switch op.Rule() {
	case catalog.Fixed:
		for _, pos := range bound.positions {
			if d := check(pos, op.Parameters()[pos].Type); d != nil {
				return expression{}, d
			}
		}
		out = op.Output()
	case catalog.ScalarConstructor:
		if d := total(0); d != nil {
			return expression{}, d
		}
		if values[0].Type().Kind() != types.ScalarKind {
			if values[0].Type().Kind() != types.StringKind {
				return expression{}, diagnostic.Mismatch("Scalar or constant String", values[0].Type().String(), values[0].Span())
			}
			value, known := c.stringValue(values[0].node)
			if !known || utf8.RuneCountInString(value) != 1 {
				return expression{}, problem(diagnostic.InvalidLiteral, "Scalar.new requires one statically known Unicode scalar", values[0].Span())
			}
		}
		out = types.Scalar()
	case catalog.RangeConstructor:
		if d := total(0); d != nil {
			return expression{}, d
		}
		if !types.Ordered(values[0].Type()) {
			return expression{}, problem(diagnostic.TypeConstraint, "range endpoints must be ordered-comparable", values[0].Span())
		}
		if d := check(1, values[0].Type()); d != nil {
			return expression{}, d
		}
		for _, pos := range []int{2, 3} {
			if d := check(pos, types.Bool()); d != nil {
				return expression{}, d
			}
		}
		out = must(types.NewRange(values[0].Type()))
	case catalog.RecordMerge:
		fields := map[string]types.Type{}
		for pos := range 2 {
			if d := total(pos); d != nil {
				return expression{}, d
			}
			record, ok := values[pos].Type().(types.Record)
			if !ok {
				return expression{}, diagnostic.Mismatch("exact Record", values[pos].Type().String(), values[pos].Span())
			}
			for _, field := range record.Fields() {
				fields[field.Name] = field.Type
			}
		}
		shape := make([]types.Field, 0, len(fields))
		for name, t := range fields {
			shape = append(shape, types.Field{Name: name, Type: t})
		}
		out = must(types.NewRecord(shape))
	case catalog.MapLookup:
		if d := total(0); d != nil {
			return expression{}, d
		}
		m, ok := values[0].Type().(types.Map)
		if !ok {
			return expression{}, diagnostic.Mismatch("Map", values[0].Type().String(), values[0].Span())
		}
		if d := check(1, m.Key()); d != nil {
			return expression{}, d
		}
		out = types.Bool()
		if op.Name() == catalog.Get {
			out = fallibleType(m.Value())
		}
	case catalog.RangeProcessor:
		if d := total(0); d != nil {
			return expression{}, d
		}
		r, ok := values[0].Type().(types.Range)
		if !ok {
			return expression{}, diagnostic.Mismatch("Range", values[0].Type().String(), values[0].Span())
		}
		if op.Name() == catalog.Contains {
			if d := check(1, r.Element()); d != nil {
				return expression{}, d
			}
		}
		out = types.Bool()
	case catalog.ListLength:
		if d := total(0); d != nil {
			return expression{}, d
		}
		value, d := c.asList(values[0], nil)
		if d != nil {
			return expression{}, d
		}
		values[0] = value
		out = types.Int()
	case catalog.ServerConstructor:
		if d := check(0, types.Int()); d != nil {
			return expression{}, d
		}
		handler, d := c.flowArgument(bound.byPosition[1])
		if d != nil {
			return expression{}, d
		}
		parameters := handler.signature.Parameters()
		if len(parameters) != 1 {
			return expression{}, problem(diagnostic.ArgumentCount, "HTTP handler requires one request parameter", bound.byPosition[1].Span)
		}
		if !types.Equal(parameters[0].Type, types.Request()) {
			return expression{}, diagnostic.Mismatch(types.Request().String(), parameters[0].Type.String(), bound.byPosition[1].Span)
		}
		if !types.Equal(baseType(handler.signature.Output()), types.Response()) {
			return expression{}, diagnostic.Mismatch("HTTP.Response or HTTP.Response!", handler.signature.Output().String(), bound.byPosition[1].Span)
		}
		onError, d := c.flowArgument(bound.byPosition[2])
		if d != nil {
			return expression{}, d
		}
		if d := c.callback(onError, []types.Type{types.Error()}, types.Response(), bound.byPosition[2].Span); d != nil {
			return expression{}, d
		}
		nodes := make([]ir.Argument, len(args))
		for i, pos := range bound.positions {
			switch pos {
			case 0:
				nodes[i] = must(ir.ValueArg(0, values[0].node))
			case 1:
				nodes[i] = must(ir.FlowArg(1, ir.FlowID(handler.entry.ID()), bound.byPosition[1].Span))
			case 2:
				nodes[i] = must(ir.FlowArg(2, ir.FlowID(onError.entry.ID()), bound.byPosition[2].Span))
			}
		}
		return builtinResult(op, types.Server(), nodes, nil, span, values[0]), nil
	default:
		panic("unimplemented canonical catalog rule")
	}
	return boundBuiltin(op, out, bound, values, span), nil
}
