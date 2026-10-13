package tensor

import (
	"fmt"
	"math"
	"math/big"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

const (
	Dtype        = "fp32"
	Profile      = "cpu-fp32-strict"
	CPU          = "cpu"
	CUDA         = "cuda:0"
	Simulation   = "cuda-sim:0"
	MaxRank      = 8
	MaxDimension = 1_000_000
	MaxElements  = 1_000_000
)

type Fault interface {
	error
	Kind() string
	Expected() string
	Actual() string
	fault()
}
type fault struct{ kind, expected, actual string }

func (fault) fault()             {}
func (f fault) Kind() string     { return f.kind }
func (f fault) Expected() string { return f.expected }
func (f fault) Actual() string   { return f.actual }
func (f fault) Error() string    { return fmt.Sprintf("expected %s, got %s", f.expected, f.actual) }
func invalid(expected string, actual any) error {
	return fault{catalog.InvalidShape, expected, fmt.Sprint(actual)}
}
func mismatch(expected string, actual any) error {
	return fault{catalog.ShapeMismatch, expected, fmt.Sprint(actual)}
}

type Shape interface {
	Dims() []int64
	Rank() int
	Count() uint64
	Weight() uint64
	Limits() error
	shape()
}
type shape struct {
	dims  []int64
	count uint64
}

func (shape) shape()          {}
func (s shape) Dims() []int64 { return slices.Clone(s.dims) }
func (s shape) Rank() int     { return len(s.dims) }
func (s shape) Count() uint64 { return s.count }
func (s shape) Weight() uint64 {
	return 16 + 4*s.count + 8*uint64(s.Rank()) + uint64(len(Dtype)+len(CPU))
}
func (s shape) Limits() error {
	if len(s.dims) > MaxRank {
		return fmt.Errorf("tensor rank exceeds %d", MaxRank)
	}
	for _, d := range s.dims {
		if d > MaxDimension {
			return fmt.Errorf("tensor dimension exceeds %d", MaxDimension)
		}
	}
	if s.count > MaxElements {
		return fmt.Errorf("tensor elements exceed %d", MaxElements)
	}
	return nil
}

func NewShape(input []*big.Int) (Shape, error) {
	dims := make([]int64, len(input))
	empty := false
	for i, d := range input {
		if d == nil {
			return nil, fmt.Errorf("absent dimension %d", i)
		}
		if d.Sign() < 0 || !d.IsInt64() {
			return nil, invalid("nonnegative signed 64-bit dimensions", input)
		}
		dims[i] = d.Int64()
		empty = empty || dims[i] == 0
	}
	if empty {
		return shape{dims, 0}, nil
	}
	count := uint64(1)
	for _, d := range dims {
		if uint64(d) > math.MaxInt64/count {
			return nil, invalid("element count within signed 64-bit range", input)
		}
		count *= uint64(d)
	}
	return shape{dims, count}, nil
}
func fromDims(dims []int64) (Shape, error) {
	input := make([]*big.Int, len(dims))
	for i, d := range dims {
		input[i] = big.NewInt(d)
	}
	return NewShape(input)
}
func ValidateCount(s Shape, n int) error {
	if s.Count() != uint64(n) {
		return invalid(fmt.Sprintf("%d elements for shape %v", s.Count(), s.Dims()), n)
	}
	return nil
}

// Plan contains validated shape rules and work charges; no numeric storage.
type Plan interface {
	Shape() Shape
	Steps() uint64
	plan()
}
type plan struct {
	op            string
	shape         Shape
	axes          []int
	steps, region uint64
}

func (*plan) plan()           {}
func (p *plan) Shape() Shape  { return p.shape }
func (p *plan) Steps() uint64 { return p.steps }

func Prepare(op string, a, b Shape, indices []*big.Int) (Plan, error) {
	p := &plan{op: op, shape: a, region: 1}
	var err error
	switch op {
	case catalog.TensorAdd, catalog.TensorSub, catalog.TensorMul, catalog.TensorDiv:
		p.shape, err = broadcast(a, b)
	case catalog.TensorSqrt, catalog.TensorRelu:
	case catalog.TensorMatmul:
		ad, bd := a.Dims(), b.Dims()
		if a.Rank() != 2 || b.Rank() != 2 {
			return nil, mismatch("rank-two [m,k] and [k,n]", fmt.Sprintf("%v and %v", ad, bd))
		}
		if ad[1] != bd[0] {
			return nil, mismatch(fmt.Sprintf("right shape [%d,n] for left %v", ad[1], ad), bd)
		}
		p.shape, err = fromDims([]int64{ad[0], bd[1]})
	case catalog.TensorReshape, catalog.TensorBroadcast:
		p.shape, err = NewShape(indices)
		if err != nil {
			return nil, err
		}
		if op == catalog.TensorReshape {
			if a.Count() != p.shape.Count() {
				return nil, mismatch(fmt.Sprintf("%d elements for reshape of %v", a.Count(), a.Dims()), p.shape.Dims())
			}
		} else {
			s, e := broadcast(a, p.shape)
			if e != nil {
				return nil, e
			}
			if !slices.Equal(s.Dims(), p.shape.Dims()) {
				return nil, mismatch(fmt.Sprintf("broadcast target compatible with %v", a.Dims()), p.shape.Dims())
			}
		}
	case catalog.TensorTranspose, catalog.TensorSum, catalog.TensorMean:
		rank := a.Rank()
		p.axes, err = ValidateAxes(op, indices, &rank)
		if err != nil {
			return nil, err
		}
		seen := make([]bool, rank)
		for _, axis := range p.axes {
			seen[axis] = true
		}
		dims := []int64{}
		ad := a.Dims()
		if op == catalog.TensorTranspose {
			for _, axis := range p.axes {
				dims = append(dims, ad[axis])
			}
		} else {
			regionDims := []int64{}
			for i, d := range ad {
				if seen[i] {
					regionDims = append(regionDims, d)
				} else {
					dims = append(dims, d)
				}
			}
			r, e := fromDims(regionDims)
			if e != nil {
				return nil, e
			}
			p.region = r.Count()
		}
		p.shape, err = fromDims(dims)
	default:
		panic("unknown tensor plan operation")
	}
	if err != nil {
		return nil, err
	}
	// Limits precede work-count multiplication; counts are at most one million.
	if err = p.shape.Limits(); err != nil {
		return nil, err
	}
	switch op {
	case catalog.TensorReshape:
	case catalog.TensorMatmul:
		k := uint64(a.Dims()[1])
		if k != 0 && p.shape.Count() > math.MaxUint64/2/k {
			return nil, fmt.Errorf("tensor work count exceeds unsigned 64-bit range")
		}
		p.steps = 2 * p.shape.Count() * k
	case catalog.TensorSum, catalog.TensorMean:
		p.steps = a.Count()
		if op == catalog.TensorMean {
			p.steps += p.shape.Count()
		}
	default:
		p.steps = p.shape.Count()
	}
	return p, nil
}
func broadcast(a, b Shape) (Shape, error) {
	ad, bd := a.Dims(), b.Dims()
	dims := make([]int64, max(len(ad), len(bd)))
	for i := 1; i <= len(dims); i++ {
		x, y := int64(1), int64(1)
		if i <= len(ad) {
			x = ad[len(ad)-i]
		}
		if i <= len(bd) {
			y = bd[len(bd)-i]
		}
		switch {
		case x == y:
			dims[len(dims)-i] = x
		case x == 1:
			dims[len(dims)-i] = y
		case y == 1:
			dims[len(dims)-i] = x
		default:
			return nil, mismatch(fmt.Sprintf("broadcast dimensions compatible with %v", ad), bd)
		}
	}
	return fromDims(dims)
}

// A nil rank validates axis invariants that do not depend on the input shape.
func ValidateAxes(op string, indices []*big.Int, rank *int) ([]int, error) {
	axes := make([]int, len(indices))
	seen := map[int64]bool{}
	for i, n := range indices {
		if n.Sign() < 0 || !n.IsInt64() {
			return nil, invalid("nonnegative signed 64-bit axis", indices)
		}
		x := n.Int64()
		if rank != nil && x >= int64(*rank) {
			return nil, invalid(fmt.Sprintf("axis in [0,%d)", *rank), indices)
		}
		if seen[x] || op != catalog.TensorTranspose && i > 0 && int64(axes[i-1]) >= x {
			return nil, invalid("unique axes; reduction axes in increasing order", indices)
		}
		axes[i] = int(x)
		seen[x] = true
	}
	if rank != nil && op == catalog.TensorTranspose && len(indices) != *rank {
		return nil, invalid(fmt.Sprintf("permutation of %d axes", *rank), indices)
	}
	return axes, nil
}
func ValidateScalar(s Shape) error {
	if s.Rank() != 0 {
		return mismatch("rank-zero shape []", s.Dims())
	}
	return nil
}
