package integration_test

import (
	"context"

	"math"
)

// This executor is an independent model of the descriptor contract. It does not
// call Lo's CPU tensor evaluator or perform implicit buffer transfers.

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

// Reconstruct a source coordinate from each output and reduction coordinate.
