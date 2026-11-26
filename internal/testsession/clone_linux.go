package testsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

func cloneFile(ctx context.Context, source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.Create(destination)
	if err != nil {
		return errors.Join(err, input.Close())
	}
	_, copyErr := io.Copy(output, contextReader{ctx, input})
	if err := errors.Join(copyErr, input.Close(), output.Close()); err != nil {
		return fmt.Errorf("copy fixture input: %w", errors.Join(err, os.Remove(destination)))
	}
	return nil
}

func retainFrontend(ctx context.Context, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Link(source, destination)
}
