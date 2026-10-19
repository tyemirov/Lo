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
