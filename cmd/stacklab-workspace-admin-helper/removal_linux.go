package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var removalStackID = regexp.MustCompile("^[a-z0-9]+(-[a-z0-9]+)*$")

func repairRemovalDirectories(root, target, strategy string, uid, gid int, result *repairResult) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	scope, id, ok := strings.Cut(rel, "/")
	if !ok || (scope != "config" && scope != "data") || !removalStackID.MatchString(id) {
		return errors.New("removal repair requires a single stack config or data directory")
	}
	base, err := os.Open(root)
	if err != nil {
		return err
	}
	defer base.Close()
	parent, err := openRemovalDir(base, scope, false)
	if err != nil {
		return err
	}
	defer parent.Close()
	dir, err := openRemovalDir(parent, id, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	return repairRemovalTree(dir, strategy, uid, gid, result)
}

// Pin each directory before granting access. A container may have created
// links, mounts or special files in its data directory.
func openRemovalDir(parent *os.File, name string, sameMount bool) (*os.File, error) {
	how := unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS}
	if sameMount {
		how.Resolve |= unix.RESOLVE_NO_XDEV
	}
	fd, err := unix.Openat2(int(parent.Fd()), name, &how)
	if err != nil {
		return nil, &os.PathError{Op: "open removal directory", Path: filepath.Join(parent.Name(), name), Err: err}
	}
	return os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name)), nil
}

func repairRemovalTree(dir *os.File, strategy string, uid, gid int, result *repairResult) error {
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	if strategy == repairStrategyACL {
		entry := fmt.Sprintf("u:%d:rwx", uid)
		if int(info.Sys().(*syscall.Stat_t).Uid) == uid {
			entry = "u::rwx"
		}
		cmd := exec.Command("setfacl", "-m", entry, "/proc/self/fd/3")
		cmd.ExtraFiles = []*os.File{dir}
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("set directory ACL on %s: %w: %s", dir.Name(), err, strings.TrimSpace(string(output)))
		}
	} else {
		if err := dir.Chown(uid, gid); err != nil {
			return err
		}
		if err := dir.Chmod(info.Mode().Perm() | 0700 | (info.Mode() & (os.ModeSticky | os.ModeSetgid))); err != nil {
			return err
		}
	}

	result.ChangedItems++
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child, err := openRemovalDir(dir, entry.Name(), true)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		err = repairRemovalTree(child, strategy, uid, gid, result)
		child.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
