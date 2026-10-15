package tensor

import (
	"context"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

type deviceData struct {
	shape Shape
	*deviceStorage
}
type deviceStorage struct{ allocation device.Allocation }

func (*deviceData) data()                              {}
func (*deviceStorage) storage()                        {}
func (d *deviceData) Shape() Shape                     { return d.shape }
func (d *deviceData) Location() string                 { return d.allocation.Location() }
func (d *deviceData) Storage() Storage                 { return d.deviceStorage }
func (s *deviceStorage) Bytes() uint64                 { return s.allocation.Bytes() }
func (s *deviceStorage) Allocation() device.Allocation { return s.allocation }

// Transfer copies numeric storage only through the selected device boundary.
func Transfer(ctx context.Context, resources device.Session, source Data, location string) (Data, error) {
	if source.Location() == location {
		return source, nil
	}
	if source.Location() != device.CPU && location != device.CPU {
		return nil, fmt.Errorf("transfer between compute locations is not supported")
	}
	target, err := resources.Allocate(ctx, location, 4*source.Shape().Count())
	if err != nil {
		return nil, err
	}
	if err = resources.Transfer(ctx, source.Storage().Allocation(), target); err != nil {
		resources.Release(target)
		return nil, err
	}
	if location == device.CPU {
		return &data{source.Shape(), &storage{target.Host(), target}}, nil
	}
	return &deviceData{source.Shape(), &deviceStorage{target}}, nil
}

// Weight includes location metadata without device addresses or handles.
func Weight(shape Shape, location string) uint64 {
	return shape.Weight() - uint64(len(device.CPU)) + uint64(len(location))
}
