package adapter

import (
	"context"
	"errors"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func fileObserver(boundary execution.Boundary, operation string, path value.Text) (execution.FileObserver, error) {
	observer, ok := boundary.(execution.FileObserver)
	if !ok {
		return nil, execution.HostFailure(operation, path.Text(), errors.New("File boundary has no observation adapter"))
	}
	return observer, nil
}

func (b *httpBoundary) Directory(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileDirectory, path)
	if err != nil {
		return nil, err
	}
	return observer.Directory(ctx, path)
}
func (b *httpBoundary) Scan(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileScan, path)
	if err != nil {
		return nil, err
	}
	return observer.Scan(ctx, path)
}
func (b *processBoundary) Directory(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileDirectory, path)
	if err != nil {
		return nil, err
	}
	return observer.Directory(ctx, path)
}
func (b *processBoundary) Scan(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileScan, path)
	if err != nil {
		return nil, err
	}
	return observer.Scan(ctx, path)
}
func (b *processLifecycleBoundary) Directory(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileDirectory, path)
	if err != nil {
		return nil, err
	}
	return observer.Directory(ctx, path)
}
func (b *processLifecycleBoundary) Scan(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileScan, path)
	if err != nil {
		return nil, err
	}
	return observer.Scan(ctx, path)
}
