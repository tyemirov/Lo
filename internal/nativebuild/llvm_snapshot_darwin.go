package nativebuild

import (
	"context"
	"golang.org/x/sys/unix"
)

func cloneLLVMSnapshot(_ context.Context, source, destination string) error {
	return unix.Clonefile(source, destination, 0)
}
