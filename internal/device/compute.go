package device

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// ComputeKind is the closed set of FP32 device primitives.
type ComputeKind uint32

const (
	Add ComputeKind = iota + 1
	Subtract
	Multiply
	Divide
	SquareRoot
	Relu
	Broadcast
	Transpose
	Sum
	Mean
	Matmul
	Fill
	ReluGradient
	CheckSqrtGradient
)

const (
	MetalProfile    = "metal-fp32"
	ExecutionFailed = "Device.ExecutionFailed"
	EventCompute    = "compute"
	ComputeRank     = 8
)

// Layout is validated shape metadata, with no tensor payload or handles.
type Layout struct {
	Count, InputCount, Region uint32
	ARank, BRank, Rank        uint32
	A, B, Output, Axes        [ComputeRank]uint32
}

// Operation cannot contain an unknown primitive or an invalid scalar.
type Operation interface {
	Kind() ComputeKind
	Layout() Layout
	Scalar() float32
	operation()
}
type operation struct {
	kind   ComputeKind
	layout Layout
	scalar float32
}

func (operation) operation()          {}
func (o operation) Kind() ComputeKind { return o.kind }
func (o operation) Layout() Layout    { return o.layout }
func (o operation) Scalar() float32   { return o.scalar }

// NewOperation admits a descriptor at the device boundary. Tensor shape
// compatibility is already established by the shared tensor planner.
func NewOperation(kind ComputeKind, layout Layout, scalar float32) (Operation, error) {
	if kind < Add || kind > CheckSqrtGradient {
		return nil, &fault{UnsupportedOperation, fmt.Errorf("unknown compute primitive %d", kind)}
	}
	if layout.ARank > ComputeRank || layout.BRank > ComputeRank || layout.Rank > ComputeRank {
		return nil, &fault{UnsupportedOperation, fmt.Errorf("compute rank exceeds %d", ComputeRank)}
	}
	if math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
		return nil, fmt.Errorf("compute scalar must be finite FP32")
	}
	return operation{kind, layout, scalar}, nil
}

// NumericCode describes the first failing output element without its payload.
type NumericCode uint32

const (
	NonFinite NumericCode = iota + 1
	ZeroDivisor
	Domain
	UndefinedDerivative
)

// NumericFailure carries only the error category and row-major output index.
type NumericFailure struct {
	Code  NumericCode
	Index uint32
}

func (f *NumericFailure) Error() string {
	return fmt.Sprintf("numeric failure %d at row-major element %d", f.Code, f.Index)
}

func (*softwareBackend) Supports(Operation) error {
	return &fault{UnsupportedOperation, fmt.Errorf("simulation backend implements transfers only")}
}
func (*softwareBackend) Compute(context.Context, Operation, []Buffer, Buffer, Buffer) error {
	return &fault{UnsupportedOperation, fmt.Errorf("simulation backend implements transfers only")}
}

// Compute admits the operation before output allocation or submission. Status
// storage uses the workspace budget and never contains tensor values.
func (s *session) Compute(ctx context.Context, location string, op Operation, inputs []Allocation) (Allocation, error) {
	if err := s.Open(ctx, location); err != nil {
		return nil, err
	}
	if err := s.backend.Supports(op); err != nil {
		return nil, s.backendFault(ctx, UnsupportedOperation, err)
	}
	if location == Metal && s.identity.Profile != MetalProfile {
		return nil, &fault{UnsupportedProfile, fmt.Errorf("Metal computation requires %s", MetalProfile)}
	}
	buffers := make([]Buffer, len(inputs))
	for i, input := range inputs {
		a := input.receipt()
		if a.owner != s || a.location != location {
			return nil, &fault{Mismatch, fmt.Errorf("compute inputs require the same invocation and location")}
		}
		buffers[i] = a.buffer
	}
	count := op.Layout().Count
	bytes := uint64(count) * 4
	if op.Kind() == CheckSqrtGradient {
		bytes = 0
	}
	output, err := s.Allocate(ctx, location, bytes)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			s.Release(output)
		}
	}()
	if count == 0 {
		complete = true
		return output, nil
	}
	if err = s.CheckBudget(WorkspaceStorage, 8); err != nil {
		return nil, err
	}
	status, err := s.Allocate(ctx, location, 4)
	if err != nil {
		return nil, err
	}
	defer s.Release(status)
	if err = s.Classify([]Allocation{status}, WorkspaceStorage); err != nil {
		return nil, err
	}
	err = s.backend.Compute(ctx, op, buffers, output.receipt().buffer, status.receipt().buffer)
	if err != nil {
		if cancelled := ctx.Err(); cancelled != nil {
			return nil, s.backendFault(ctx, ExecutionFailed, err)
		}
		var numeric *NumericFailure
		if errors.As(err, &numeric) {
			return nil, err
		}
		return nil, s.backendFault(ctx, ExecutionFailed, err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	s.emit(EventCompute, output.receipt())
	complete = true
	return output, nil
}
