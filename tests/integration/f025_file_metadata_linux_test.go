package integration_test

import (
	"os"
	"syscall"
)

func f025NativeMetadata(info os.FileInfo) (uint32, int64, int64) {
	stat := info.Sys().(*syscall.Stat_t)
	return stat.Uid, stat.Atim.Sec*1_000_000_000 + stat.Atim.Nsec, stat.Ctim.Sec*1_000_000_000 + stat.Ctim.Nsec
}

func f025ChangedMetadata(info os.FileInfo, field string) os.FileInfo {
	stat := *info.Sys().(*syscall.Stat_t)
	switch field {
	case "owner_uid":
		stat.Uid ^= 1
	case "accessed_unix_ns":
		stat.Atim.Sec++
	case "changed_unix_ns":
		stat.Ctim.Sec++
	case "link_count":
		stat.Nlink++
	}
	return f025MetadataInfo{FileInfo: info, native: &stat}
}
