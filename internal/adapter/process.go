package adapter

import (
	"context"
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type processBoundary struct{ execution.Boundary }

// WithProcess rejects command activation until a host execution profile is qualified.
func WithProcess(boundary execution.Boundary) (execution.Boundary, error) {
	if boundary == nil {
		return nil, fmt.Errorf("Process adapter requires a boundary")
	}
	return &processBoundary{boundary}, nil
}

func (b *processBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	if _, ok := target.(value.ProcessCommand); ok {
		return nil, execution.ProcessUnavailable()
	}
	return b.Boundary.Commit(ctx, target)
}
