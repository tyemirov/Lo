package nativebuild

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/MarcoPoloResearchLab/Lo/internal/metalconfig"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimebundle"
)

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func metalInputs() ([]byte, []byte, *metalconfig.Identity, error) {
	archive, data, err := runtimebundle.Metal()
	if err != nil {
		return nil, nil, nil, err
	}
	var manifest metalconfig.Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, nil, nil, fmt.Errorf("read Metal manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, nil, nil, fmt.Errorf("Metal manifest has trailing data")
	}
	expected := metalconfig.PinnedIdentity()
	expected.KernelSHA256 = manifest.Identity.KernelSHA256
	if len(expected.KernelSHA256) != 64 || !reflect.DeepEqual(expected, manifest.Identity) {
		return nil, nil, nil, fmt.Errorf("Metal manifest does not match the pinned numeric profile")
	}
	paths := metalconfig.RuntimeFiles()
	if len(manifest.Files) != len(paths) {
		return nil, nil, nil, fmt.Errorf("Metal manifest has an incorrect file set")
	}
	for _, path := range paths {
		if len(manifest.Files[path]) != 64 {
			return nil, nil, nil, fmt.Errorf("Metal manifest is missing %s", path)
		}
	}
	if manifest.Files[metalconfig.LibraryPath] != expected.KernelSHA256 {
		return nil, nil, nil, fmt.Errorf("Metal kernel identity differs from the manifest")
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read Metal runtime: %w", err)
	}
	seen := map[string]bool{}
	for _, file := range reader.File {
		if seen[file.Name] || manifest.Files[file.Name] == "" {
			return nil, nil, nil, fmt.Errorf("unexpected Metal runtime file %q", file.Name)
		}
		input, err := file.Open()
		if err != nil {
			return nil, nil, nil, err
		}
		contents, readErr := io.ReadAll(input)
		if err := errors.Join(readErr, input.Close()); err != nil {
			return nil, nil, nil, err
		}
		if digestBytes(contents) != manifest.Files[file.Name] {
			return nil, nil, nil, fmt.Errorf("Metal runtime digest differs for %s", file.Name)
		}
		seen[file.Name] = true
	}
	if len(seen) != len(paths) {
		return nil, nil, nil, fmt.Errorf("Metal runtime is incomplete")
	}
	return archive, data, &manifest.Identity, nil
}
