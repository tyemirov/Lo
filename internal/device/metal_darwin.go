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
	return Identity{Name: C.GoString(C.lo_metal_name(b.handle)), Profile: MetalTransferProfile,
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
