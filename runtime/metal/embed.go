//go:build metal && darwin && arm64 && cgo

// Package metal contains the prepared Metal kernel library.
package metal

import _ "embed"

//go:embed metal.metallib
var library []byte

// Library returns the prepared kernel library.
func Library() []byte { return append([]byte(nil), library...) }
