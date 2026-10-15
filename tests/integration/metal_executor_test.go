package integration_test

import (
	"context"
	"fmt"
	"math"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

// This executor is an independent model of the descriptor contract. It does not
// call Lo's CPU tensor evaluator or perform implicit buffer transfers.
type metalModel struct {
	profile           string
	before            func(device.Operation) error
	operations        []device.ComputeKind
	allocations       int
	reservation       func(int, uint64) uint64
	allocationFailure func(int, uint64) error
	support           func(device.Operation) error
}

func (m *metalModel) Open(ctx context.Context, selector string) (device.Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selector != device.Metal {
		return nil, fmt.Errorf("unexpected model selector %s", selector)
	}
	return m, nil
}
func (m *metalModel) Identity() device.Identity {
	profile := m.profile
	if profile == "" {
		profile = device.MetalProfile
	}
	return device.Identity{Name: "independent Metal descriptor model", Profile: profile}
}
func (*metalModel) Close() error { return nil }
func (m *metalModel) Allocate(ctx context.Context, bytes uint64) (device.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.allocations++
	if m.allocationFailure != nil {
		if err := m.allocationFailure(m.allocations, bytes); err != nil {
			return nil, err
		}
	}
	reserved := bytes * 2
	if m.reservation != nil {
		reserved = m.reservation(m.allocations, bytes)
	}
	return &metalModelBuffer{make([]float32, bytes/4), reserved}, nil
}
func (m *metalModel) Supports(op device.Operation) error {
	if m.support != nil {
		return m.support(op)
	}
	if op.Kind() < device.Add || op.Kind() > device.CheckSqrtGradient {
		return fmt.Errorf("unsupported model operation")
	}
	return nil
}

type metalModelBuffer struct {
	values   []float32
	reserved uint64
}

func (b *metalModelBuffer) Bytes() uint64 { return b.reserved }
func (b *metalModelBuffer) Upload(ctx context.Context, values []float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copy(b.values, values)
	return nil
}
func (b *metalModelBuffer) Download(ctx context.Context, values []float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copy(values, b.values)
	return nil
}
func (b *metalModelBuffer) Close() error { b.values = nil; return nil }
func modelNormal(value float64) float32 {
	result := float32(value)
	if math.Float32bits(result)&0x7f800000 == 0 {
		return 0
	}
	return result
}
func modelCoordinates(index uint32, dims []uint32) []uint32 {
	out := make([]uint32, len(dims))
	for i := len(dims) - 1; i >= 0; i-- {
		out[i] = index % dims[i]
		index /= dims[i]
	}
	return out
}
func modelIndex(coordinates, dims []uint32) uint32 {
	var index uint32
	shift := len(coordinates) - len(dims)
	for i, d := range dims {
		coordinate := coordinates[i+shift]
		if d == 1 {
			coordinate = 0
		}
		index = index*d + coordinate
	}
	return index
}
func (m *metalModel) Compute(ctx context.Context, op device.Operation, inputs []device.Buffer, output, status device.Buffer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.operations = append(m.operations, op.Kind())
	if m.before != nil {
		if err := m.before(op); err != nil {
			return err
		}
	}
	layout := op.Layout()
	out := output.(*metalModelBuffer).values
	var a, b []float32
	if len(inputs) > 0 {
		a = inputs[0].(*metalModelBuffer).values
	}
	if len(inputs) > 1 {
		b = inputs[1].(*metalModelBuffer).values
	}
	ad, bd, od := layout.A[:layout.ARank], layout.B[:layout.BRank], layout.Output[:layout.Rank]
	for index := uint32(0); index < layout.Count; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fail := func(code device.NumericCode) error { return &device.NumericFailure{Code: code, Index: index} }
		var result float32
		coordinates := modelCoordinates(index, od)
		switch op.Kind() {
		case device.Fill:
			out[index] = op.Scalar()
			continue
		case device.Broadcast:
			out[index] = a[modelIndex(coordinates, ad)]
			continue
		case device.Transpose:
			original := make([]uint32, len(ad))
			for i, coordinate := range coordinates {
				original[layout.Axes[i]] = coordinate
			}
			out[index] = a[modelIndex(original, ad)]
			continue
		case device.CheckSqrtGradient:
			if modelNormal(float64(a[index])) == 0 {
				return fail(device.UndefinedDerivative)
			}
			continue
		case device.ReluGradient:
			if modelNormal(float64(a[index])) > 0 {
				result = modelNormal(float64(b[index]))
			}
		case device.Sum, device.Mean:
			// Reconstruct a source coordinate from each output and reduction coordinate.
			var reduced []uint32
			for axis, dim := range ad {
				if layout.Axes[axis] != 0 {
					reduced = append(reduced, dim)
				}
			}
			for r := uint32(0); r < layout.Region; r++ {
				rc := modelCoordinates(r, reduced)
				source := make([]uint32, len(ad))
				keep, remove := 0, 0
				for axis := range ad {
					if layout.Axes[axis] != 0 {
						source[axis] = rc[remove]
						remove++
					} else {
						source[axis] = coordinates[keep]
						keep++
					}
				}
				result = modelNormal(float64(result) + float64(modelNormal(float64(a[modelIndex(source, ad)]))))
				if math.IsInf(float64(result), 0) || math.IsNaN(float64(result)) {
					return fail(device.NonFinite)
				}
			}
			if op.Kind() == device.Mean {
				result = modelNormal(float64(result) / float64(float32(layout.Region)))
			}
		case device.Matmul:
			for k := uint32(0); k < ad[1]; k++ {
				product := modelNormal(float64(modelNormal(float64(a[coordinates[0]*ad[1]+k]))) * float64(modelNormal(float64(b[k*bd[1]+coordinates[1]]))))
				if math.IsInf(float64(product), 0) {
					return fail(device.NonFinite)
				}
				result = modelNormal(float64(result) + float64(product))
				if math.IsInf(float64(result), 0) {
					return fail(device.NonFinite)
				}
			}
		default:
			x := float64(modelNormal(float64(a[modelIndex(coordinates, ad)])))
			var y float64
			if b != nil {
				y = float64(modelNormal(float64(b[modelIndex(coordinates, bd)])))
			}
			switch op.Kind() {
			case device.Add:
				result = modelNormal(x + y)
			case device.Subtract:
				result = modelNormal(x - y)
			case device.Multiply:
				result = modelNormal(x * y)
			case device.Divide:
				if y == 0 {
					return fail(device.ZeroDivisor)
				}
				result = modelNormal(x / y)
			case device.SquareRoot:
				if x < 0 {
					return fail(device.Domain)
				}
				result = modelNormal(math.Sqrt(x))
			case device.Relu:
				if x > 0 {
					result = float32(x)
				}
			default:
				return fmt.Errorf("unsupported model primitive %d", op.Kind())
			}
		}
		if math.IsInf(float64(result), 0) || math.IsNaN(float64(result)) {
			return fail(device.NonFinite)
		}
		out[index] = result
	}
	return nil
}
