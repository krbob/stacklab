//go:build linux || darwin

package backup

import (
	"errors"
	"golang.org/x/sys/unix"
)

func checkCaptureSpace(path string, budget int64) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return err
	}
	// Keep a reserve for SQLite, process state and filesystem bookkeeping. The
	// streaming byte limit enforces the plan budget without pre-copy estimates.
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	if free < 64*1024*1024 || budget > 0 && uint64(budget) > free-64*1024*1024 {
		return errors.New("insufficient free space for backup staging (64 MiB reserve required)")
	}
	return nil
}
