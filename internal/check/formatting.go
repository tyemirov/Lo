package check

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func (c *checker) stringFormat(op catalog.Operation, args []syntax.Argument, span source.Span) (expression, diagnostic.Diagnostic) {
	bound, failure := bindArguments(op.Name(), op.Parameters(), args, span)
	if failure != nil {
		return expression{}, failure
	}
	values := make([]expression, len(args))
	for _, position := range bound.positions {
		var expected types.Type
		if position == 0 {
			expected = types.String()
		}
		value, failure := c.processorArgument(bound.byPosition[position], expected)
		if failure != nil {
			return expression{}, failure
		}
		if position == 1 {
			value, failure = c.unwrap(value)
			if failure != nil {
				return expression{}, failure
			}
			if !catalog.AcceptsFormatFields(value.Type()) {
				return expression{}, diagnostic.Mismatch("concrete Record of total String, Int, Float, or Bool fields", value.Type().String(), value.Span())
			}
		}
		values[position] = value
	}
	return boundBuiltin(op, op.Output(), bound, values, span), nil
}
