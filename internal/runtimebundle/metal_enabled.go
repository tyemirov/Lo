//go:build metal && darwin && arm64 && cgo

package runtimebundle

import _ "embed"

//go:embed metal.zip
var metalArchive []byte

//go:embed metal-manifest.json
var metalManifest []byte

// Metal returns the explicitly packaged Metal runtime and its manifest.
func Metal() ([]byte, []byte, error) {
	return append([]byte(nil), metalArchive...), append([]byte(nil), metalManifest...), nil
}
