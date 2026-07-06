//go:build linux

package hostinfo

import "syscall"

func statfsBlockSize(stats syscall.Statfs_t) uint64 {
	if stats.Frsize > 0 {
		return uint64(stats.Frsize)
	}
	return uint64(stats.Bsize)
}
