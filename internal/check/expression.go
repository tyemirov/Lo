package check

import (
	"math"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) expression(expr syntax.Expression, expected types.Type) (expression, diagnostic.Diagnostic) {
	value, d := c.rawExpression(expr, expected)
	if d != nil {
		return expression{}, d
	}
	return c.coerce(value, expected)
}
func (c *checker) rawExpression(expr syntax.Expression, expected types.Type) (expression, diagnostic.Diagnostic) {
	switch e := expr.(type) {
	case syntax.Literal:
		var t types.Type
		switch e.LiteralKind() {
		case syntax.IntLiteral:
			t = types.Int()
		case syntax.StringLiteral:
			t = types.String()
		case syntax.BoolLiteral:
			t = types.Bool()
		case syntax.NullLiteral:
			t = types.Null()
		case syntax.FloatLiteral:
			n, err := strconv.ParseFloat(e.Text(), 64)
			if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
				return expression{}, problem(diagnostic.InvalidLiteral, "Float requires a finite binary64 value", e.Span())
			}
			t = types.Float()
		default:
			panic("unknown literal kind")
		}
		return expression{node: must(ir.NewLiteral(t, e.Text(), e.Span()))}, nil
	case syntax.Reference:
		return c.reference(e.Name(), e.Span())
	case syntax.Projection:
		parts := e.Parts()
		value, d := c.reference(parts[0], e.Span())
		if d != nil {
			return expression{}, d
		}
		for _, field := range parts[1:] {
			value, d = c.unwrap(value)
			if d != nil {
				return expression{}, d
			}
			t, ok := types.FieldType(value.Type(), field)
			if !ok {
				return expression{}, problem(diagnostic.UnknownField, "type "+value.Type().String()+" has no field "+field, e.Span())
			}
			value.node = must(ir.NewProjection(t, value.node, field, e.Span()))
		}
		return value, nil
	case syntax.Call:
		return c.call(e.Name(), e.Arguments(), baseType(expected), e.Span())
	case syntax.Pipe:
		args := append([]syntax.Argument{{Value: e.Left(), Span: e.Left().Span()}}, e.Arguments()...)
		return c.call(e.Name(), args, baseType(expected), e.Span())
	case syntax.Match:
		return c.match(e, expected)
	default:
		panic("unknown expression syntax")
	}
}
func (c *checker) stringValue(node ir.Expression) (string, bool) {
	switch n := node.(type) {
	case ir.Literal:
		if n.Type().Kind() == types.StringKind {
			return n.Text(), true
		}
	case ir.Reference:
		if value, ok := c.module.definitions[n.Binding()]; ok {
			return c.stringValue(value)
		}
	case ir.BuiltinCall:
		switch n.Operation().Name() {
		case catalog.StringNew, catalog.Upper, catalog.Lower:
			s, ok := c.stringValue(n.Arguments()[0].(ir.ValueArgument).Value())
			if !ok {
				return "", false
			}
			switch n.Operation().Name() {
			case catalog.Upper:
				s = strings.ToUpper(s)
			case catalog.Lower:
				s = strings.ToLower(s)
			}
			return s, true
		case catalog.Concat:
			var b strings.Builder
			for _, arg := range n.Arguments() {
				s, ok := c.stringValue(arg.(ir.ValueArgument).Value())
				if !ok {
					return "", false
				}
				b.WriteString(s)
			}
			return b.String(), true
		}
	}
	return "", false
}
