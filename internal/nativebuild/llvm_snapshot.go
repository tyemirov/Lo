package nativebuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// snapshotLLVMFile validates the private clone that the build will consume.
// The caller owns destination's private directory and its final removal.
func snapshotLLVMFile(ctx context.Context, source, destination, expectedSHA256 string) (snapshot string, failure error) {
	expected, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(expected) != sha256.Size {
		return "", fmt.Errorf("snapshot %q requires a SHA-256 digest", source)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := cloneLLVMSnapshot(ctx, source, destination); err != nil {
		return "", fmt.Errorf("clone LLVM input %q: %w", source, err)
	}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, os.Remove(destination))
		}
	}()
	input, err := os.Open(destination)
	if err != nil {
		return "", fmt.Errorf("open LLVM snapshot %q: %w", destination, err)
	}
	hasher := sha256.New()
	_, readErr := io.Copy(hasher, llvmSnapshotReader{ctx: ctx, reader: input})
	if err := errors.Join(readErr, input.Close()); err != nil {
		return "", fmt.Errorf("read LLVM snapshot %q: %w", destination, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !bytes.Equal(hasher.Sum(nil), expected) {
		return "", fmt.Errorf("LLVM snapshot %q SHA-256 mismatch", source)
	}
	return destination, nil
}

type llvmSnapshotReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader llvmSnapshotReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
