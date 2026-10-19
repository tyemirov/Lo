package eval

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (m *machine) listProcessor(call ir.BuiltinCall, args []value.Value, callback identity.FlowID, f frame) (resultValue value.Value, resultErr error) {
	scope := m.Scope()
	defer func() { scope.Close(resultValue) }()
	op := call.Operation().Name()
	contextStart := 1
	var state value.Value
	var items []value.Value
	var count int
	if op == catalog.ListUnfold {
		contextStart, state = 2, args[0]
		n := args[1].(value.Integer).Big()
		if n.Sign() > 0 {
			if !n.IsInt64() || n.Int64() > execution.MaxItems {
				return nil, execution.Limit("unfold elements")
			}
			count = int(n.Int64())
		}
	} else {
		items = args[0].(value.List).Items()
		count = len(items)
		if op == catalog.ListFold || op == catalog.FlowFold {
			contextStart, state = 2, args[1]
		}
	}
	context := args[contextStart : len(args)-1]
	var progress execution.FlowProgress
	if op == catalog.FlowFold {
		var err error
		progress, err = m.RecordFlowProgress(count, 0, state)
		if err != nil {
			return nil, err
		}
		defer func() {
			resultErr = execution.WithFlowProgress(resultErr, progress)
		}()
	}
	output := []value.Value{}
	for index := 0; index < count; index++ {
		if err := m.Step(); err != nil {
			return nil, err
		}
		params := append([]value.Value{}, context...)
		if state != nil {
			params = append(params, state)
		}
		if op != catalog.ListUnfold {
			params = append(params, items[index])
		}
		v, err := m.invoke(callback, params, call.Span())
		if err != nil {
			return nil, err
		}
		if fv, ok := v.(value.Fallible); ok {
			var cause value.Error
			v, cause = fv.Outcome()
			if cause != nil {
				if op == catalog.FlowFold {
					cause = value.WithFlowProgress(cause, progress.Record())
				}
				fields := []value.Field{{Name: "index", Value: value.Int64(int64(index))}}
				if op != catalog.ListUnfold {
					fields = append(fields, value.Field{Name: "element", Value: items[index]})
				}
				if state != nil {
					fields = append(fields, value.Field{Name: "state", Value: state})
				}
				cause = value.WithFrame(cause, f.name, op, must(value.NewRecord(fields)))
				return must(value.Failure(call.Type().(types.Fallible), value.At(cause, op, call.Span()))), nil
			}
		}
		switch op {
		case catalog.ListMap:
			output = append(output, v)
		case catalog.ListFilter:
			if v.(value.Boolean).Bool() {
				output = append(output, items[index])
			}
		case catalog.ListFold, catalog.FlowFold:
			state = v
		case catalog.ListFlatMap:
			extra := v.(value.List).Items()
			if len(extra) > execution.MaxItems-len(output) {
				return nil, execution.Limit("list elements")
			}
			output = append(output, extra...)
		case catalog.ListUnfold:
			r := v.(value.Record)
			output = append(output, r.Field("value"))
			scope.Retain(r.Field("value"))
			state = r.Field("next_state")
		}
		if op == catalog.ListFold || op == catalog.FlowFold || op == catalog.ListUnfold {
			scope.Keep(state)
		}
		if op == catalog.FlowFold {
			next, err := m.RecordFlowProgress(count, index+1, state)
			if err != nil {
				return nil, err
			}
			progress = next
		}
	}
	if op == catalog.ListFold || op == catalog.FlowFold {
		return runtimeops.Success(call.Type(), state), nil
	}
	outType := call.Type()
	if ft, ok := outType.(types.Fallible); ok {
		outType = ft.Base()
	}
	result, err := runtimeops.List(outType.(types.List), output)
	if err != nil {
		return nil, err
	}
	return runtimeops.Success(call.Type(), result), nil
}
