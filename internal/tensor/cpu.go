package tensor

import (
	"context"
	"fmt"
	"math"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

type Data interface {
	Shape() Shape
	Location() string
	Storage() Storage
	data()
}
type Storage interface {
	Bytes() uint64
	Allocation() device.Allocation
	storage()
}
type storage struct {
	elements   []float32
	allocation device.Allocation
}

func (*storage) storage()                        {}
func (s *storage) Bytes() uint64                 { return s.allocation.Bytes() }
func (s *storage) Allocation() device.Allocation { return s.allocation }

type data struct {
	shape Shape
	*storage
}

func (*data) data()              {}
func (d *data) Shape() Shape     { return d.shape }
func (d *data) At(i int) float32 { return d.elements[i] }
func (d *data) Storage() Storage { return d.storage }
func (d *data) Location() string { return device.CPU }

// HostData exposes numeric elements only for CPU tensors.
type HostData interface {
	Data
	At(int) float32
}

// Construct receives a validated shape and indexed input from the runtime edge.
// The caller reserves the work and value budgets before this allocation.
func Construct(ctx context.Context, resources device.Session, s Shape, input func(int) float64) (Data, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allocation, err := resources.Allocate(ctx, device.CPU, 4*s.Count())
	if err != nil {
		return nil, err
	}
	out := allocation.Host()
	complete := false
	defer func() {
		if !complete {
			resources.Release(allocation)
		}
	}()
	for i := range out {
		if err := poll(ctx, i); err != nil {
			return nil, err
		}
		v := float32(input(i))
		if !finite(v) {
			return nil, numeric(catalog.NumericNonFinite, i, "finite FP32 conversion")
		}
		out[i] = v
	}
	complete = true
	return &data{s, &storage{out, allocation}}, nil
}

func Execute(ctx context.Context, resources device.Session, operation Plan, a, b Data) (Data, error) {
	p := operation.(*plan)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.op == catalog.TensorReshape || (p.op == catalog.TensorSum || p.op == catalog.TensorMean) && len(p.axes) == 0 {
		return &data{p.shape, a.(*data).storage}, nil
	}
	if p.op == catalog.TensorMean && p.region == 0 {
		return nil, fault{catalog.EmptyReduction, "nonempty mean reduction region", fmt.Sprint(a.Shape().Dims())}
	}
	allocation, err := resources.Allocate(ctx, device.CPU, 4*p.shape.Count())
	if err != nil {
		return nil, err
	}
	out := allocation.Host()
	complete := false
	defer func() {
		if !complete {
			resources.Release(allocation)
		}
	}()
	if len(out) == 0 {
		complete = true
		return &data{p.shape, &storage{out, allocation}}, nil
	}
	ad := a.Shape().Dims()
	od := p.shape.Dims()
	var bd []int64
	if b != nil {
		bd = b.Shape().Dims()
	}
	switch p.op {
	case catalog.TensorMatmul:
		k, n := int(ad[1]), int(bd[1])
		for i := range out {
			if err := poll(ctx, i); err != nil {
				return nil, err
			}
			for j := 0; j < k; j++ {
				if err := poll(ctx, j); err != nil {
					return nil, err
				}
				product := float32(a.(HostData).At((i/n)*k+j) * b.(HostData).At(j*n+i%n))
				if !finite(product) {
					return nil, numeric(catalog.NumericNonFinite, i, fmt.Sprintf("finite product at contracted index %d", j))
				}
				out[i] = float32(out[i] + product)
				if !finite(out[i]) {
					return nil, numeric(catalog.NumericNonFinite, i, fmt.Sprintf("finite sum at contracted index %d", j))
				}
			}
		}
	case catalog.TensorSum, catalog.TensorMean:
		selected := make([]bool, len(ad))
		for _, axis := range p.axes {
			selected[axis] = true
		}
		for i := 0; i < int(a.Shape().Count()); i++ {
			if err := poll(ctx, i); err != nil {
				return nil, err
			}
			index, stride, rest := 0, 1, i
			for axis := len(ad) - 1; axis >= 0; axis-- {
				coordinate := rest % int(ad[axis])
				rest /= int(ad[axis])
				if !selected[axis] {
					index += coordinate * stride
					stride *= int(ad[axis])
				}
			}
			out[index] = float32(out[index] + a.(HostData).At(i))
			if !finite(out[index]) {
				return nil, numeric(catalog.NumericNonFinite, i, "finite reduction intermediate")
			}
		}
		if p.op == catalog.TensorMean {
			for i := range out {
				if err := poll(ctx, i); err != nil {
					return nil, err
				}
				out[i] = float32(out[i] / float32(p.region))
				if !finite(out[i]) {
					return nil, numeric(catalog.NumericNonFinite, i, "finite mean")
				}
			}
		}
	case catalog.TensorTranspose:
		strides := make([]int, len(ad))
		stride := 1
		for i := len(ad) - 1; i >= 0; i-- {
			strides[i] = stride
			stride *= int(ad[i])
		}
		for i := range out {
			if err := poll(ctx, i); err != nil {
				return nil, err
			}
			index, rest := 0, i
			for axis := len(od) - 1; axis >= 0; axis-- {
				index += (rest % int(od[axis])) * strides[p.axes[axis]]
				rest /= int(od[axis])
			}
			out[i] = a.(HostData).At(index)
		}
	default:
		for i := range out {
			if err := poll(ctx, i); err != nil {
				return nil, err
			}
			x := a.(HostData).At(broadcastIndex(i, od, ad))
			var y, z float32
			if b != nil {
				y = b.(HostData).At(broadcastIndex(i, od, bd))
			}
			switch p.op {
			case catalog.TensorAdd:
				z = float32(x + y)
			case catalog.TensorSub:
				z = float32(x - y)
			case catalog.TensorMul:
				z = float32(x * y)
			case catalog.TensorDiv:
				if y == 0 {
					return nil, numeric(catalog.NumericZero, i, "nonzero divisor")
				}
				z = float32(x / y)
			case catalog.TensorSqrt:
				if x < 0 {
					return nil, numeric(catalog.NumericDomain, i, "nonnegative square-root input")
				}
				z = float32(math.Sqrt(float64(x)))
			case catalog.TensorRelu:
				if x > 0 {
					z = x
				}
			case catalog.TensorBroadcast:
				z = x
			default:
				panic("unknown CPU tensor operation")
			}
			if !finite(z) {
				return nil, numeric(catalog.NumericNonFinite, i, "finite FP32 result")
			}
			out[i] = z
		}
	}
	complete = true
	return &data{p.shape, &storage{out, allocation}}, nil
}

func ReluDerivative(ctx context.Context, resources device.Session, gradient, input Data) (Data, error) {
	allocation, err := resources.Allocate(ctx, device.CPU, 4*input.Shape().Count())
	if err != nil {
		return nil, err
	}
	out := allocation.Host()
	complete := false
	defer func() {
		if !complete {
			resources.Release(allocation)
		}
	}()
	for i := range out {
		if err := poll(ctx, i); err != nil {
			return nil, err
		}
		if input.(HostData).At(i) > 0 {
			out[i] = gradient.(HostData).At(i)
		}
	}
	complete = true
	return &data{input.Shape(), &storage{out, allocation}}, nil
}
func CheckSqrtDerivative(ctx context.Context, input Data) error {
	for i := 0; i < int(input.Shape().Count()); i++ {
		if err := poll(ctx, i); err != nil {
			return err
		}
		if input.(HostData).At(i) == 0 {
			return numeric(catalog.UndefinedDerivative, i, "nonzero active square-root input")
		}
	}
	return nil
}
func broadcastIndex(index int, out, in []int64) int {
	offset, stride := 0, 1
	for i, j := len(out)-1, len(in)-1; j >= 0; i, j = i-1, j-1 {
		coordinate := index % int(out[i])
		index /= int(out[i])
		if in[j] != 1 {
			offset += coordinate * stride
		}
		stride *= int(in[j])
	}
	return offset
}
func poll(ctx context.Context, index int) error {
	if index%1024 == 0 {
		return ctx.Err()
	}
	return nil
}
func finite(v float32) bool { return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v)) }
func numeric(kind string, index int, expected string) error {
	return fault{kind, expected, fmt.Sprintf("invalid value at row-major element %d", index)}
}
