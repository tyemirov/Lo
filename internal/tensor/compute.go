package tensor

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

var computeKinds = map[string]device.ComputeKind{
	catalog.TensorAdd: device.Add, catalog.TensorSub: device.Subtract, catalog.TensorMul: device.Multiply,
	catalog.TensorDiv: device.Divide, catalog.TensorSqrt: device.SquareRoot, catalog.TensorRelu: device.Relu,
	catalog.TensorBroadcast: device.Broadcast, catalog.TensorTranspose: device.Transpose,
	catalog.TensorSum: device.Sum, catalog.TensorMean: device.Mean, catalog.TensorMatmul: device.Matmul,
}

func computeLayout(output Shape, a, b Data) device.Layout {
	layout := device.Layout{Count: uint32(output.Count()), Rank: uint32(output.Rank())}
	for i, d := range output.Dims() {
		layout.Output[i] = uint32(d)
	}
	if a != nil {
		layout.ARank = uint32(a.Shape().Rank())
		layout.InputCount = uint32(a.Shape().Count())
		for i, d := range a.Shape().Dims() {
			layout.A[i] = uint32(d)
		}
	}
	if b != nil {
		layout.BRank = uint32(b.Shape().Rank())
		for i, d := range b.Shape().Dims() {
			layout.B[i] = uint32(d)
		}
	}
	return layout
}
func compute(ctx context.Context, resources device.Session, location string, kind device.ComputeKind, layout device.Layout, scalar float32, shape Shape, inputs ...Data) (Data, error) {
	if location != device.Metal {
		f, _ := device.NewFault(device.UnsupportedOperation, fmt.Errorf("tensor arithmetic is unavailable on %s", location))
		return nil, f
	}
	operation, err := device.NewOperation(kind, layout, scalar)
	if err != nil {
		return nil, err
	}
	allocations := make([]device.Allocation, len(inputs))
	for i, input := range inputs {
		allocations[i] = input.Storage().Allocation()
	}
	allocation, err := resources.Compute(ctx, location, operation, allocations)
	if err != nil {
		var failure *device.NumericFailure
		if errors.As(err, &failure) {
			kinds := map[device.NumericCode]string{device.NonFinite: catalog.NumericNonFinite, device.ZeroDivisor: catalog.NumericZero, device.Domain: catalog.NumericDomain, device.UndefinedDerivative: catalog.UndefinedDerivative}
			return nil, numeric(kinds[failure.Code], int(failure.Index), "valid finite metal-fp32 arithmetic")
		}
		return nil, err
	}
	return &deviceData{shape, &deviceStorage{allocation}}, nil
}
func executeDevice(ctx context.Context, resources device.Session, p *plan, a, b Data) (Data, error) {
	if a.Location() != device.Metal {
		f, _ := device.NewFault(device.UnsupportedOperation, fmt.Errorf("tensor arithmetic is unavailable on %s", a.Location()))
		return nil, f
	}
	if p.op == catalog.TensorReshape || (p.op == catalog.TensorSum || p.op == catalog.TensorMean) && len(p.axes) == 0 {
		return &deviceData{p.shape, a.(*deviceData).deviceStorage}, nil
	}
	if p.op == catalog.TensorMean && p.region == 0 {
		return nil, fault{catalog.EmptyReduction, "nonempty mean reduction region", fmt.Sprint(a.Shape().Dims())}
	}
	layout := computeLayout(p.shape, a, b)
	layout.Region = uint32(p.region)
	for i, axis := range p.axes {
		if p.op == catalog.TensorTranspose {
			layout.Axes[i] = uint32(axis)
		} else {
			layout.Axes[axis] = 1
		}
	}
	inputs := []Data{a}
	if b != nil {
		inputs = append(inputs, b)
	}
	return compute(ctx, resources, a.Location(), computeKinds[p.op], layout, 0, p.shape, inputs...)
}

// Constant constructs a tensor at the selected location without a transfer.
func Constant(ctx context.Context, resources device.Session, shape Shape, location string, v float32) (Data, error) {
	if location == device.CPU {
		return Construct(ctx, resources, shape, func(int) float64 { return float64(v) })
	}
	return compute(ctx, resources, location, device.Fill, computeLayout(shape, nil, nil), v, shape)
}
