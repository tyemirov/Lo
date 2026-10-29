package adapter

import (
	"fmt"
	"os"
	"syscall"

	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func nativeFileIdentity(info os.FileInfo) (value.FileIdentity, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return value.FileIdentity{}, 0, fmt.Errorf("filesystem identity unavailable for %q", info.Name())
	}
	return value.FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, uint64(stat.Nlink), nil
}

func nativeFileMetadata(info os.FileInfo) (fileMetadata, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileMetadata{}, fmt.Errorf("filesystem metadata unavailable for %q", info.Name())
	}
	accessed, err := fileUnixNanoseconds(stat.Atimespec.Sec, stat.Atimespec.Nsec)
	if err != nil {
		return fileMetadata{}, fmt.Errorf("access time: %w", err)
	}
	changed, err := fileUnixNanoseconds(stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
	if err != nil {
		return fileMetadata{}, fmt.Errorf("change time: %w", err)
	}
	modified := info.ModTime().UnixNano()
	if !fileTimeRepresentable(info.ModTime(), modified) {
		return fileMetadata{}, fmt.Errorf("modification time exceeds Unix nanosecond range")
	}
	return fileMetadata{owner: uint32(stat.Uid), accessed: accessed, modified: modified, changed: changed}, nil
}
