//go:build linux

package nativebuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

func cloneLLVMSnapshot(ctx context.Context, source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.Create(destination)
	if err != nil {
		return errors.Join(err, input.Close())
	}
	_, copyErr := io.Copy(output, llvmSnapshotReader{ctx: ctx, reader: input})
	if err := errors.Join(copyErr, input.Close(), output.Close()); err != nil {
		removal := os.Remove(destination)
		return fmt.Errorf("copy LLVM input: %w", errors.Join(err, removal))
	}
	return nil
}
