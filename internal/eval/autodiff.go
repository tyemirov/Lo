package eval

import (
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/derivative"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"math/big"

	"github.com/MarcoPoloResearchLab/Lo/internal/autodiff"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const MaxTapeBytes uint64 = 32 << 20

func (m *machine) gradient(original ir.BuiltinCall, args []value.Value) (value.Value, error) {
	call := original.(autodiff.Call)
	parameters := call.Parameters()
	f := frame{bindings: map[ir.BindingID]value.Value{}}
	for i, p := range parameters {
		argument := i + 1
		if i == len(parameters)-1 {
			argument = 0
		}
		f.bindings[p.ID] = args[argument]
	}
	nodes := call.Nodes()
	snapshots := map[int]value.Record{}
	tape := map[tensor.Storage]bool{}
	var tapeBytes uint64
	fail := func(err error, n autodiff.Node) (value.Value, error) {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		var cause value.Error
		var runtime execution.Failure
		var numeric tensor.Fault
		switch {
		case errors.As(err, &runtime):
			cause = runtime.Value()
		case errors.As(err, &numeric):
			input := []value.Field{}
			for i, id := range n.Operands() {
				if v, ok := f.bindings[id]; ok {
					input = append(input, value.Field{Name: "argument" + big.NewInt(int64(i)).String(), Value: v})
				}
			}
			cause = value.At(must(value.NewError(numeric.Kind(), numeric.Error(), must(value.NewRecord(input)))), n.Operation(), n.Expression().Span())
		default:
			return nil, execution.Limit("derivative operation: " + err.Error())
		}
		frames := n.Frames()
		for i := len(frames) - 1; i >= 0; i-- {
			frame := frames[i]
			cause = value.WithFrame(cause, frame.Name, cause.Operation(), snapshots[frame.Invocation])
		}
		return must(value.Failure(call.Type().(types.Fallible), cause)), nil
	}
	for _, n := range nodes {
		for _, frame := range n.Frames() {
			if _, ok := snapshots[frame.Invocation]; !ok {
				fields := make([]value.Field, len(frame.Arguments))
				for i, a := range frame.Arguments {
					fields[i] = value.Field{Name: a.Name, Value: value.Snapshot(f.bindings[a.Binding])}
				}
				snapshot := must(value.NewRecord(fields))
				if err := m.ChargeValue(snapshot); err != nil {
					return nil, err
				}
				snapshots[frame.Invocation] = snapshot
			}
		}
		v, err := m.expression(n.Expression(), f)
		if err != nil {
			return fail(err, n)
		}
		f.bindings[n.Binding()] = v
		if n.Retained() {
			if wrapped, ok := v.(value.Fallible); ok {
				v, _ = wrapped.Outcome()
			}
			if t, ok := v.(value.Tensor); ok {
				storage := t.Data().Storage()
				if !tape[storage] {
					if storage.Bytes() > MaxTapeBytes-tapeBytes {
						return nil, execution.Limit("derivative tape bytes")
					}
					tape[storage] = true
					tapeBytes += storage.Bytes()
				}
			}
		}
		for _, id := range n.Release() {
			delete(f.bindings, id)
		}
	}
	loss := f.bindings[call.Result()].(value.Tensor)
	if loss.Data().Shape().Rank() != 0 {
		return runtimeops.OperationFailure(original, args, catalog.InvalidLoss, "loss must return a rank-zero tensor"), nil
	}
	math := runtimeops.Arithmetic(m.Session)
	adjoints := map[int]tensor.Data{}
	for _, s := range call.Selections() {
		n := nodes[s.Node-1]
		input := f.bindings[n.Binding()].(value.Tensor)
		zero, err := math.Constant(input.Data().Shape(), 0)
		if err != nil {
			return fail(err, n)
		}
		adjoints[s.Node] = zero
	}
	seed, err := math.Constant(loss.Data().Shape(), 1)
	if err != nil {
		return fail(err, nodes[call.ResultNode()-1])
	}
	adjoints[call.ResultNode()] = seed
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		g := adjoints[n.ID()]
		if !n.Reverse() || g == nil || len(n.Edges()) == 0 {
			continue
		}
		operands := n.Operands()
		if _, ok := n.Expression().(ir.Conversion); ok {
			for _, edge := range n.Edges() {
				contribution := g
				if previous := adjoints[edge.Node]; previous != nil {
					var err error
					contribution, err = math.Apply(catalog.TensorAdd, previous, contribution, nil)
					if err != nil {
						return fail(err, n)
					}
				}
				adjoints[edge.Node] = contribution
			}
			delete(adjoints, n.ID())
			continue
		}
		a := f.bindings[operands[0]].(value.Tensor).Data()
		var b, result tensor.Data
		var indices []int64
		if len(operands) > 1 {
			switch v := f.bindings[operands[1]].(type) {
			case value.Tensor:
				b = v.Data()
			case value.List:
				for _, v := range v.Items() {
					indices = append(indices, v.(value.Integer).Big().Int64())
				}
			}
		}
		if n.Operation() == catalog.TensorSqrt {
			v, _ := f.bindings[n.Binding()].(value.Fallible).Outcome()
			result = v.(value.Tensor).Data()
		}
		for _, edge := range n.Edges() {
			contribution, err := derivative.VJP(math, n.Operation(), edge.Operand, g, a, b, result, indices)
			if err != nil {
				return fail(err, n)
			}
			if previous := adjoints[edge.Node]; previous != nil {
				contribution, err = math.Apply(catalog.TensorAdd, previous, contribution, nil)
				if err != nil {
					return fail(err, n)
				}
			}
			adjoints[edge.Node] = contribution
		}
		delete(adjoints, n.ID())
	}
	fields := make([]value.Field, len(call.Selections()))
	for i, s := range call.Selections() {
		fields[i] = value.Field{Name: s.Key, Value: must(value.NewTensor(adjoints[s.Node]))}
	}
	gradients := must(value.NewRecord(fields))
	return runtimeops.Success(call.Type(), must(value.NewRecord([]value.Field{{Name: catalog.ParamValue, Value: loss}, {Name: catalog.FieldGradients, Value: gradients}}))), nil
}
