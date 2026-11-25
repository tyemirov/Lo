package testsession

import (
	"context"

	"golang.org/x/sys/unix"
)

func cloneFile(ctx context.Context, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return unix.Clonefile(source, destination, 0)
}

func retainFrontend(ctx context.Context, source, destination string) error {
	return cloneFile(ctx, source, destination)
}
