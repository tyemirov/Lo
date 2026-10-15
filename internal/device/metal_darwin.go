//go:build metal && darwin && arm64 && cgo

package device

/*
#cgo CFLAGS: -mmacosx-version-min=27.0
#cgo LDFLAGS: -mmacosx-version-min=27.0 -framework Metal -framework Foundation
#include "metal_bridge.h"
*/
import "C"

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"math"
	"runtime"
	"unsafe"
)

//go:embed metal.metallib
var metalLibrary []byte

type metalBackend struct{ handle *C.LoMetalSession }
type metalBuffer struct {
	handle *C.LoMetalBuffer
	bytes  uint64
}

func metalError(failure *C.LoMetalError) error {
	kind := map[C.int]string{
		C.LO_METAL_UNAVAILABLE: Unavailable,
		C.LO_METAL_PROFILE:     UnsupportedProfile,
		C.LO_METAL_LIBRARY:     LibraryFailed,
		C.LO_METAL_ALLOCATION:  AllocationFailed,
		C.LO_METAL_TRANSFER:    TransferFailed,
		C.LO_METAL_LOST:        Lost,
		C.LO_METAL_EXECUTION:   ExecutionFailed,
	}[failure.code]
	if kind == "" {
		return fmt.Errorf("Metal adapter returned unknown error code %d", failure.code)
	}
	return &fault{kind, fmt.Errorf("%s: %s", Metal, C.GoString(&failure.message[0]))}
}

func openMetal(ctx context.Context) (Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var failure C.LoMetalError
	handle := C.lo_metal_open(unsafe.Pointer(unsafe.SliceData(metalLibrary)), C.size_t(len(metalLibrary)), &failure)
	runtime.KeepAlive(metalLibrary)
	if handle == nil {
		return nil, metalError(&failure)
	}
	if err := ctx.Err(); err != nil {
		C.lo_metal_close(handle)
		return nil, err
	}
	return &metalBackend{handle}, nil
}

func (b *metalBackend) Allocate(ctx context.Context, size uint64) (Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var failure C.LoMetalError
	handle := C.lo_metal_allocate(b.handle, C.uint64_t(size), &failure)
	if handle == nil {
		return nil, metalError(&failure)
	}
	if err := ctx.Err(); err != nil {
		C.lo_metal_release(handle)
		return nil, err
	}
	return &metalBuffer{handle, uint64(C.lo_metal_reserved(handle))}, nil
}

func (b *metalBackend) Close() error {
	C.lo_metal_close(b.handle)
	b.handle = nil
	return nil
}

func (b *metalBackend) Identity() Identity {
	return Identity{Name: C.GoString(C.lo_metal_name(b.handle)), Profile: MetalProfile, System: C.GoString(C.lo_metal_system(b.handle)),
		RegistryID: fmt.Sprintf("%x", uint64(C.lo_metal_registry(b.handle))), KernelSHA256: fmt.Sprintf("%x", sha256.Sum256(metalLibrary))}
}

func (b *metalBuffer) Bytes() uint64 { return b.bytes }

func (b *metalBuffer) Upload(ctx context.Context, values []float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var failure C.LoMetalError
	ok := C.lo_metal_upload(b.handle, unsafe.Pointer(unsafe.SliceData(values)), C.uint64_t(len(values))*4, &failure)
	runtime.KeepAlive(values)
	if ok == 0 {
		return metalError(&failure)
	}
	return ctx.Err()
}

func (b *metalBuffer) Download(ctx context.Context, values []float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var failure C.LoMetalError
	ok := C.lo_metal_download(b.handle, unsafe.Pointer(unsafe.SliceData(values)), C.uint64_t(len(values))*4, &failure)
	runtime.KeepAlive(values)
	if ok == 0 {
		return metalError(&failure)
	}
	return ctx.Err()
}

func (b *metalBuffer) Close() error {
	C.lo_metal_release(b.handle)
	b.handle = nil
	return nil
}

var metalKinds = map[ComputeKind]C.LoMetalWord{
	Add: C.LO_ADD, Subtract: C.LO_SUB, Multiply: C.LO_MUL, Divide: C.LO_DIV, SquareRoot: C.LO_SQRT, Relu: C.LO_RELU,
	Broadcast: C.LO_BROADCAST, Transpose: C.LO_TRANSPOSE, Sum: C.LO_SUM, Mean: C.LO_MEAN, Matmul: C.LO_MATMUL,
	Fill: C.LO_FILL, ReluGradient: C.LO_RELU_GRAD, CheckSqrtGradient: C.LO_SQRT_GRAD,
}

func (b *metalBackend) Supports(op Operation) error {
	if _, ok := metalKinds[op.Kind()]; !ok {
		return &fault{UnsupportedOperation, fmt.Errorf("Metal primitive %d is unavailable", op.Kind())}
	}
	return nil
}
func (b *metalBackend) Compute(ctx context.Context, op Operation, inputs []Buffer, output, status Buffer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	layout := op.Layout()
	command := C.LoMetalOperation{kind: metalKinds[op.Kind()], count: C.LoMetalWord(layout.Count), inputCount: C.LoMetalWord(layout.InputCount), region: C.LoMetalWord(layout.Region), aRank: C.LoMetalWord(layout.ARank), bRank: C.LoMetalWord(layout.BRank), rank: C.LoMetalWord(layout.Rank), scalar: C.LoMetalWord(math.Float32bits(op.Scalar()))}
	for i := 0; i < ComputeRank; i++ {
		command.a[i] = C.LoMetalWord(layout.A[i])
		command.b[i] = C.LoMetalWord(layout.B[i])
		command.output[i] = C.LoMetalWord(layout.Output[i])
		command.axes[i] = C.LoMetalWord(layout.Axes[i])
	}
	var left, right *C.LoMetalBuffer
	if len(inputs) > 0 {
		left = inputs[0].(*metalBuffer).handle
	}
	if len(inputs) > 1 {
		right = inputs[1].(*metalBuffer).handle
	}
	var failure C.LoMetalError
	var numeric C.uint32_t
	ok := C.lo_metal_compute(b.handle, &command, left, right, output.(*metalBuffer).handle, status.(*metalBuffer).handle, &numeric, &failure)
	if ok == 0 {
		return metalError(&failure)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if uint32(numeric) != math.MaxUint32 {
		codes := map[C.uint32_t]NumericCode{C.LO_NUMERIC_NONFINITE: NonFinite, C.LO_NUMERIC_ZERO: ZeroDivisor, C.LO_NUMERIC_DOMAIN: Domain, C.LO_NUMERIC_DERIVATIVE: UndefinedDerivative}
		code, ok := codes[numeric&7]
		if !ok {
			return &fault{ExecutionFailed, fmt.Errorf("invalid Metal numeric status %d", numeric)}
		}
		return &NumericFailure{Code: code, Index: uint32(numeric) >> 3}
	}
	return nil
}
