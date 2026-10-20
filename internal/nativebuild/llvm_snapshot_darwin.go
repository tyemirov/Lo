package nativebuild

import "golang.org/x/sys/unix"

func cloneLLVMSnapshot(source, destination string) error {
	return unix.Clonefile(source, destination, 0)
}
