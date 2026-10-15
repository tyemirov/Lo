//go:build !metal || !darwin || !arm64 || !cgo

package device

import (
	"context"
	"fmt"
)

func openMetal(context.Context) (Backend, error) {
	return nil, &fault{Unavailable, fmt.Errorf("backend %s is not present in this build", Metal)}
}
