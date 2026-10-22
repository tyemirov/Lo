//go:build !darwin && !linux

package nativebuild

import (
	"context"
	"errors"
)

func cloneLLVMSnapshot(_ context.Context, _, _ string) error {
	return errors.New("LLVM toolchain snapshots require a Darwin or Linux host")
}
