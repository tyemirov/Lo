//go:build !darwin

package nativebuild

import "errors"

// F017 retains Linux-host compiler distribution qualification. The current
// Darwin-host distribution supports both Darwin and Linux executable targets.
func cloneLLVMSnapshot(_, _ string) error {
	return errors.New("LLVM toolchain snapshots require a Darwin host")
}
