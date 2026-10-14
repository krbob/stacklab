//go:build !linux

package backup

import "os"

// Production runs on Linux. Other platforms use the descriptor validation and
// chmod in writableOwnedDirectories for readable directories.
func relaxOwnedRestoreDirectory(_ *os.Root, _ string, _ os.FileInfo) error { return nil }
