package autodiff

import (
	"math/big"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
)

// Arithmetic owns resource admission and checked primitive execution.
// A native backend can execute the same derivative plan through this boundary.
type Arithmetic interface {
	Apply(string, tensor.Data, tensor.Data, []*big.Int) (tensor.Data, error)
	Constant(tensor.Shape, float32) (tensor.Data, error)
	Relu(tensor.Data, tensor.Data) (tensor.Data, error)
	CheckSqrt(tensor.Data) error
}
type calculation struct {
	arithmetic Arithmetic
	err        error
}

func (c *calculation) apply(op string, a, b tensor.Data, indices []int64) tensor.Data {
	if c.err != nil {
		return nil
	}
	var values []*big.Int
	for _, n := range indices {
		values = append(values, big.NewInt(n))
	}
	v, err := c.arithmetic.Apply(op, a, b, values)
	c.err = err
	return v
}
func (c *calculation) scalar(v float32) tensor.Data {
	if c.err != nil {
		return nil
	}
	s := must(tensor.NewShape(nil))
	x, err := c.arithmetic.Constant(s, v)
	c.err = err
	return x
}
func (c *calculation) negate(v tensor.Data) tensor.Data {
	return c.apply(catalog.TensorMul, c.scalar(-1), v, nil)
}
func (c *calculation) unbroadcast(g tensor.Data, shape tensor.Shape) tensor.Data {
	if c.err != nil {
		return nil
	}
	input, output := shape.Dims(), g.Shape().Dims()
	if slices.Equal(input, output) {
		return g
	}
	axes := []int64{}
	shift := len(output) - len(input)
	for i, d := range output {
		if i < shift || input[i-shift] == 1 && d != 1 {
			axes = append(axes, int64(i))
		}
	}
	if len(axes) > 0 {
		g = c.apply(catalog.TensorSum, g, nil, axes)
	}
	return c.apply(catalog.TensorReshape, g, nil, input)
}

// VJP evaluates only the requested active operand edge. The caller invokes
// edges in operand order and nodes in reverse stable forward order.
func VJP(arithmetic Arithmetic, op string, operand int, g, a, b, result tensor.Data, indices []int64) (tensor.Data, error) {
	c := &calculation{arithmetic: arithmetic}
	var out tensor.Data
	switch op {
	case catalog.TensorAdd:
		out = g
	case catalog.TensorSub:
		out = g
		if operand == 1 {
			out = c.negate(g)
		}
	case catalog.TensorMul:
		other := b
		if operand == 1 {
			other = a
		}
		out = c.apply(catalog.TensorMul, g, other, nil)
	case catalog.TensorDiv:
		out = c.apply(catalog.TensorDiv, g, b, nil)
		if operand == 1 {
			out = c.negate(out)
			ratio := c.apply(catalog.TensorDiv, a, b, nil)
			out = c.apply(catalog.TensorMul, out, ratio, nil)
		}
	case catalog.TensorSqrt:
		c.err = arithmetic.CheckSqrt(a)
		denominator := c.apply(catalog.TensorMul, c.scalar(2), result, nil)
		out = c.apply(catalog.TensorDiv, g, denominator, nil)
	case catalog.TensorRelu:
		out, c.err = arithmetic.Relu(g, a)
	case catalog.TensorMatmul:
		if operand == 0 {
			bt := c.apply(catalog.TensorTranspose, b, nil, []int64{1, 0})
			out = c.apply(catalog.TensorMatmul, g, bt, nil)
		} else {
			at := c.apply(catalog.TensorTranspose, a, nil, []int64{1, 0})
			out = c.apply(catalog.TensorMatmul, at, g, nil)
		}
	case catalog.TensorBroadcast:
		out = c.unbroadcast(g, a.Shape())
	case catalog.TensorReshape:
		out = c.apply(catalog.TensorReshape, g, nil, a.Shape().Dims())
	case catalog.TensorTranspose:
		inverse := make([]int64, len(indices))
		for i, axis := range indices {
			inverse[axis] = int64(i)
		}
		out = c.apply(catalog.TensorTranspose, g, nil, inverse)
	case catalog.TensorSum, catalog.TensorMean:
		shape := a.Shape().Dims()
		count := int64(1)
		for _, axis := range indices {
			count *= shape[axis]
			shape[axis] = 1
		}
		out = g
		if op == catalog.TensorMean && len(indices) > 0 {
			out = c.apply(catalog.TensorDiv, g, c.scalar(float32(count)), nil)
		}
		if len(indices) > 0 {
			out = c.apply(catalog.TensorReshape, out, nil, shape)
			out = c.apply(catalog.TensorBroadcast, out, nil, a.Shape().Dims())
		}
	default:
		panic("missing derivative rule: " + op)
	}
	if op == catalog.TensorAdd || op == catalog.TensorSub || op == catalog.TensorMul || op == catalog.TensorDiv {
		shape := a.Shape()
		if operand == 1 {
			shape = b.Shape()
		}
		out = c.unbroadcast(out, shape)
	}
	return out, c.err
}
