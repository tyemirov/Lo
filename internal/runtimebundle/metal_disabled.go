//go:build !metal || !darwin || !arm64 || !cgo

package runtimebundle

import "fmt"

// Metal rejects a backend absent from this compiler.
func Metal() ([]byte, []byte, error) {
	return nil, nil, fmt.Errorf("Metal artifacts require the Metal-enabled Lo compiler")
}
