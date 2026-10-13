package tensorfacts

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const MaxVisits = 100_000

// Unknown shape and size use JSON null; rank-zero shapes use an empty array.
type Metadata struct {
	Dtype        string   `json:"dtype"`
	Profile      string   `json:"profile"`
	Device       string   `json:"device"`
	Layout       string   `json:"layout"`
	Shape        []string `json:"shape"`
	StorageBytes *uint64  `json:"storage_bytes"`
}

func Unknown() *Metadata {
	return &Metadata{Dtype: tensor.Dtype, Profile: tensor.Profile, Device: tensor.CPU, Layout: "row-major"}
}

type fact struct {
	key    [32]byte
	Number string
	List   bool
	Items  []fact
	Fields map[string]fact
	Shape  tensor.Shape
}

// Each immutable fact has a fixed-size structural key. Shared nested lists
// therefore do not expand into an exponential cache key.
func seal(f fact) fact {
	if f.Number == "" && !f.List && f.Fields == nil && f.Shape == nil {
		return fact{}
	}
	h := sha256.New()
	fmt.Fprintf(h, "n%d:%s;l%t:%d;", len(f.Number), f.Number, f.List, len(f.Items))
	for _, item := range f.Items {
		h.Write(item.key[:])
	}
	if f.Fields != nil {
		names := make([]string, 0, len(f.Fields))
		for name := range f.Fields {
			names = append(names, name)
		}
		slices.Sort(names)
		fmt.Fprintf(h, "r%d:", len(names))
		for _, name := range names {
			fmt.Fprintf(h, "%d:%s", len(name), name)
			child := f.Fields[name]
			h.Write(child.key[:])
		}
	}
	if f.Shape != nil {
		fmt.Fprintf(h, "s%v", f.Shape.Dims())
	}
	copy(f.key[:], h.Sum(nil))
	return f
}

type analyzer struct {
	flows  map[ir.FlowID]ir.Flow
	memo   map[string]fact
	facts  map[ir.Expression]fact
	visits int
	err    diagnostic.Diagnostic
}

func Analyze(p ir.Program) (map[ir.Expression]*Metadata, diagnostic.Diagnostic) {
	if !usesTensors(p) {
		return map[ir.Expression]*Metadata{}, nil
	}
	a := &analyzer{flows: map[ir.FlowID]ir.Flow{}, memo: map[string]fact{}, facts: map[ir.Expression]fact{}}
	for _, f := range p.Flows() {
		a.flows[f.ID()] = f
	}
	for _, f := range p.Flows() {
		a.invoke(f.ID(), make([]fact, len(f.Parameters())))
	}
	a.body(p.Body(), map[ir.BindingID]fact{})
	if a.err != nil {
		return nil, a.err
	}
	out := map[ir.Expression]*Metadata{}
	for e, f := range a.facts {
		t := e.Type()
		if ft, ok := t.(types.Fallible); ok {
			t = ft.Base()
		}
		if t.Kind() != types.TensorKind {
			continue
		}
		m := Unknown()
		if f.Shape != nil {
			m.Shape = make([]string, f.Shape.Rank())
			for i, d := range f.Shape.Dims() {
				m.Shape[i] = strconv.FormatInt(d, 10)
			}
			if f.Shape.Count() <= math.MaxUint64/4 {
				size := 4 * f.Shape.Count()
				m.StorageBytes = &size
			}
		}
		out[e] = m
	}
	return out, nil
}
func (a *analyzer) invoke(id ir.FlowID, args []fact) fact {
	if a.err != nil {
		return fact{}
	}
	var cache strings.Builder
	cache.WriteString(string(id) + ":")
	for _, arg := range args {
		cache.Write(arg.key[:])
	}
	key := cache.String()
	if f, ok := a.memo[key]; ok {
		return f
	}
	f := a.flows[id]
	env := map[ir.BindingID]fact{}
	for i, p := range f.Parameters() {
		env[p.ID] = args[i]
	}
	result := a.body(f.Body(), env)
	if f.Handler() != "" {
		result = join(result, a.invoke(f.Handler(), []fact{{}}))
	}
	a.memo[key] = result
	return result
}
func (a *analyzer) body(body []ir.Statement, env map[ir.BindingID]fact) fact {
	for _, s := range body {
		switch x := s.(type) {
		case ir.Binding:
			env[x.ID()] = a.expression(x.Value(), env)
		case ir.Return:
			return a.expression(x.Value(), env)
		case ir.Commit:
			a.expression(x.Target(), env)
		}
		if a.err != nil {
			break
		}
	}
	return fact{}
}
func (a *analyzer) expression(e ir.Expression, env map[ir.BindingID]fact) (result fact) {
	if a.err != nil {
		return
	}
	a.visits++
	if a.visits > MaxVisits {
		a.err = diagnostic.New(diagnostic.TensorAnalysisLimit, diagnostic.Check, "tensor metadata analysis exceeds 100000 expression visits", e.Span())
		return
	}
	defer func() {
		result = seal(result)
		if old, ok := a.facts[e]; ok {
			a.facts[e] = join(old, result)
		} else {
			a.facts[e] = result
		}
	}()
	switch x := e.(type) {
	case ir.Literal:
		if x.Type().Kind() == types.IntKind {
			result.Number = x.Text()
		}
	case ir.Reference:
		result = env[x.Binding()]
	case ir.Conversion:
		input := a.expression(x.Input(), env)
		if x.Conversion() != ir.Failure {
			result = input
		}
	case ir.Projection:
		result = a.expression(x.Input(), env).Fields[x.Field()]
	case ir.FlowCall:
		args := make([]fact, len(x.Arguments()))
		for _, arg := range x.Arguments() {
			args[arg.Position()] = a.expression(arg.Value(), env)
		}
		result = a.invoke(x.Flow(), args)
		if a.err != nil {
			a.err = diagnostic.WithRelated(a.err, x.Span())
		}
	case ir.Match:
		a.expression(x.Selector(), env)
		for i, c := range x.Cases() {
			if exact, ok := c.(ir.ExactCase); ok {
				a.expression(exact.Head(), env)
			}
			f := a.expression(c.Body(), env)
			if i == 0 {
				result = f
			} else {
				result = join(result, f)
			}
		}
	case ir.BuiltinCall:
		args := make([]fact, len(x.Arguments()))
		for _, arg := range x.Arguments() {
			if v, ok := arg.(ir.ValueArgument); ok {
				args[v.Position()] = a.expression(v.Value(), env)
			}
		}
		result = a.builtin(x, args)
	}
	return
}
func (a *analyzer) builtin(call ir.BuiltinCall, args []fact) fact {
	op := call.Operation().Name()
	failure := func(err error) {
		var f tensor.Fault
		if errors.As(err, &f) && (f.Kind() == catalog.InvalidShape || f.Kind() == catalog.ShapeMismatch) {
			a.err = diagnostic.ShapeMismatch(op, f.Expected(), f.Actual(), call.Span())
		}
	}
	switch op {
	case catalog.ListNew:
		return fact{List: true, Items: args}
	case catalog.IntNew:
		return args[0]
	case catalog.RecordNew:
		fields := map[string]fact{}
		for i, n := range call.Labels() {
			fields[n] = args[i]
		}
		return fact{Fields: fields}
	case catalog.Merge:
		if args[0].Fields == nil || args[1].Fields == nil {
			return fact{}
		}
		fields := map[string]fact{}
		for _, v := range args {
			for k, f := range v.Fields {
				fields[k] = f
			}
		}
		return fact{Fields: fields}
	case catalog.TensorNew:
		dims, known := integers(args[1])
		if !known {
			return fact{}
		}
		s, err := tensor.NewShape(dims)
		if err != nil {
			failure(err)
			return fact{}
		}
		if s.Limits() == nil && args[0].List {
			if err = tensor.ValidateCount(s, len(args[0].Items)); err != nil {
				failure(err)
				return fact{}
			}
		}
		return fact{Shape: s}
	case catalog.TensorShape:
		if args[0].Shape != nil {
			items := make([]fact, args[0].Shape.Rank())
			for i, d := range args[0].Shape.Dims() {
				items[i] = seal(fact{Number: strconv.FormatInt(d, 10)})
			}
			return fact{List: true, Items: items}
		}
	case catalog.TensorToFloat:
		if args[0].Shape != nil {
			if err := tensor.ValidateScalar(args[0].Shape); err != nil {
				failure(err)
			}
		}
	case catalog.TensorRank:
		if args[0].Shape != nil {
			return fact{Number: strconv.Itoa(args[0].Shape.Rank())}
		}
	case catalog.TensorSqrt, catalog.TensorRelu:
		return fact{Shape: args[0].Shape}
	case catalog.TensorAdd, catalog.TensorSub, catalog.TensorMul, catalog.TensorDiv, catalog.TensorMatmul:
		if args[0].Shape == nil || args[1].Shape == nil {
			return fact{}
		}
		p, err := tensor.Prepare(op, args[0].Shape, args[1].Shape, nil)
		if err != nil {
			failure(err)
			return fact{}
		}
		return fact{Shape: p.Shape()}
	case catalog.TensorReshape, catalog.TensorBroadcast, catalog.TensorTranspose, catalog.TensorSum, catalog.TensorMean:
		dims, known := integers(args[1])
		if !known {
			return fact{}
		}
		if args[0].Shape == nil {
			if op == catalog.TensorReshape || op == catalog.TensorBroadcast {
				s, err := tensor.NewShape(dims)
				if err != nil {
					failure(err)
					return fact{}
				}
				return fact{Shape: s}
			}
			if _, err := tensor.ValidateAxes(op, dims, nil); err != nil {
				failure(err)
			}
			return fact{}
		}
		p, err := tensor.Prepare(op, args[0].Shape, nil, dims)
		if err != nil {
			failure(err)
			return fact{}
		}
		return fact{Shape: p.Shape()}
	}
	return fact{}
}
func integers(f fact) ([]*big.Int, bool) {
	if !f.List {
		return nil, false
	}
	result := make([]*big.Int, len(f.Items))
	for i, v := range f.Items {
		n, ok := new(big.Int).SetString(v.Number, 10)
		if !ok {
			return nil, false
		}
		result[i] = n
	}
	return result, true
}
func join(a, b fact) fact {
	if a.key == b.key {
		return a
	}
	f := fact{}
	if a.Number == b.Number {
		f.Number = a.Number
	}
	if a.Shape != nil && b.Shape != nil && slices.Equal(a.Shape.Dims(), b.Shape.Dims()) {
		f.Shape = a.Shape
	}
	if a.List && b.List && len(a.Items) == len(b.Items) {
		f.List = true
		f.Items = make([]fact, len(a.Items))
		for i := range f.Items {
			f.Items[i] = join(a.Items[i], b.Items[i])
		}
	}
	if a.Fields != nil && b.Fields != nil {
		f.Fields = map[string]fact{}
		for name, v := range a.Fields {
			if w, ok := b.Fields[name]; ok {
				f.Fields[name] = join(v, w)
			}
		}
	}
	return seal(f)
}
