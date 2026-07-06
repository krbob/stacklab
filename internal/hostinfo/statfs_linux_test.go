//go:build linux

package hostinfo

import (
	"syscall"
	"testing"
)

func TestStatfsBlockSizePrefersFragmentSize(t *testing.T) {
	t.Parallel()

	stats := syscall.Statfs_t{Bsize: 4096, Frsize: 1024}
	if got := statfsBlockSize(stats); got != 1024 {
		t.Fatalf("statfsBlockSize() = %d, want 1024", got)
	}
}

func TestStatfsBlockSizeFallsBackToBlockSize(t *testing.T) {
	t.Parallel()

	stats := syscall.Statfs_t{Bsize: 4096}
	if got := statfsBlockSize(stats); got != 4096 {
		t.Fatalf("statfsBlockSize() = %d, want 4096", got)
	}
}
