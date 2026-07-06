//go:build !linux

package hostinfo

import "syscall"

func statfsBlockSize(stats syscall.Statfs_t) uint64 {
	return uint64(stats.Bsize)
}
