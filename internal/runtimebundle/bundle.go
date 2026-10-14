// Package runtimebundle contains the runtime sources from this compiler build.
package runtimebundle

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed runtime.zip
var archive []byte

// Bytes returns an independent copy of the pinned source archive.
func Bytes() []byte  { return append([]byte(nil), archive...) }
func Digest() string { sum := sha256.Sum256(archive); return hex.EncodeToString(sum[:]) }
