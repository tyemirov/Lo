package integration_test

import (
	"io"
)

const resourceOnlyPrivateCause = "private-resource-only-close-cause"

type resourceOnlyWriter struct{}

func (resourceOnlyWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (resourceOnlyWriter) Close() error              { return nil }
