package backup

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// O_PATH pins even a mode-000 directory without needing to read it. The procfs
// descriptor link lets chmod operate on that exact inode on kernels predating
// fchmodat2(AT_EMPTY_PATH); no mutable directory entry is followed for mutation.
func relaxOwnedRestoreDirectory(parent *os.Root, name string, expected os.FileInfo) error {
	file, err := parent.OpenFile(name, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := actual.Sys().(*syscall.Stat_t)
	if !ok || !actual.IsDir() || int(stat.Uid) != os.Geteuid() || !os.SameFile(expected, actual) {
		return fmt.Errorf("%w: recovery directory ownership or identity changed; inspect it manually", ErrInvalid)
	}
	return os.Chmod(fmt.Sprintf("/proc/self/fd/%d", file.Fd()), actual.Mode().Perm()|0700)
}
