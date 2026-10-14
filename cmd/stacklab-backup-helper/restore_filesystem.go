package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"stacklab/internal/backup"
)

func applyPreparedRestore(ctx context.Context, directory, archive string, state *preparedRestore) (err error) {
	root, pinned, err := pinRestoreTarget(state.Target, state.AuthorizationRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	defer pinned.Close()
	if state.Mode == "replace" {
		if err = saveRootMetadata(ctx, directory, state.Target, pinned); err != nil {
			return err
		}
		state.RootMetadata = true
	}
	state.Phase = "writing"
	rollbackName := ""
	if state.Mode == "replace" {
		state.Phase = "moving"
		entries, readErr := fs.ReadDir(root.FS(), ".")
		if readErr != nil {
			return readErr
		}
		rollbackName = ".stacklab-rollback-" + filepath.Base(directory)
		state.Rollback = filepath.Join(state.Target, rollbackName)
		if _, statErr := root.Lstat(rollbackName); !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("rollback directory already exists")
		}
		for _, entry := range entries {
			state.Moved = append(state.Moved, entry.Name())
		}
	}
	if err = savePrepared(directory, *state); err != nil {
		return err
	}
	if state.Mode == "replace" {
		if err = root.Mkdir(rollbackName, 0700); err != nil {
			return err
		}
		for _, name := range state.Moved {
			if err = root.Rename(name, filepath.Join(rollbackName, name)); err != nil {
				return fmt.Errorf("replacement interrupted; rollback content retained at %s: %w", state.Rollback, err)
			}
		}
		for _, path := range []string{".", rollbackName} {
			dir, openErr := root.Open(path)
			if openErr != nil {
				return openErr
			}
			syncErr := dir.Sync()
			closeErr := dir.Close()
			if err = errors.Join(syncErr, closeErr); err != nil {
				return err
			}
		}
		state.Phase = "writing"
		if err = savePrepared(directory, *state); err != nil {
			return err
		}
	}
	err = extractPinned(ctx, archive, state.Target, pinned)
	if err != nil {
		if state.Mode == "replace" {
			rollbackErr := rollbackPrepared(directory, state, root, pinned)
			if rollbackErr == nil {
				state.Phase = "rolled_back"
				rollbackErr = savePrepared(directory, *state)
			}
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	if state.Rollback != "" {
		if err = root.RemoveAll(rollbackName); err != nil {
			return fmt.Errorf("restored data is complete; remove rollback at %s after inspection: %w", state.Rollback, err)
		}
	}
	if state.Mode == "replace" {
		if err = restoreArchiveRootMetadata(ctx, directory, archive, state.Target, pinned); err != nil {
			return err
		}
	}
	state.Phase = "completed"
	return savePrepared(directory, *state)
}

func rollbackPrepared(directory string, state *preparedRestore, root *os.Root, pinned *os.File) error {
	rollbackName := filepath.Base(state.Rollback)
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != rollbackName {
			if err = root.RemoveAll(entry.Name()); err != nil {
				return err
			}
		}
	}
	entries, err = fs.ReadDir(root.FS(), rollbackName)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err = root.Rename(filepath.Join(rollbackName, entry.Name()), entry.Name()); err != nil {
			return err
		}
	}
	if err = root.Remove(rollbackName); err != nil {
		return err
	}
	if state.RootMetadata {
		restoreCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return extractPinned(restoreCtx, filepath.Join(directory, "root.tar"), state.Target, pinned)
	}
	return nil
}

func extractPinned(ctx context.Context, archive, target string, pinned *os.File) error {
	if runtime.GOOS == "linux" {
		return backup.ExtractArchivePinned(ctx, archive, pinned, true)
	}
	return backup.ExtractArchive(ctx, archive, target, true)
}

func approvedRecoveryRoot(target string, roots []string) string {
	best := ""
	for _, root := range roots {
		root = filepath.Clean(root)
		if beneath(root, target) && len(root) > len(best) {
			best = root
		}
	}
	// The caller independently verified a local Docker volume for paths outside
	// the configured roots. Such a volume already exists and is its own boundary.
	if best == "" {
		return target
	}
	return best
}

func pinRestoreTarget(target, authorizationRoot string) (*os.Root, *os.File, error) {
	if authorizationRoot == "" {
		authorizationRoot = target
	}
	base, err := os.OpenRoot(authorizationRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("approved recovery root must already exist: %w", err)
	}
	defer base.Close()
	baseFile, err := base.Open(".")
	if err != nil {
		return nil, nil, err
	}
	defer baseFile.Close()
	if err = validatePinnedPath(baseFile, authorizationRoot); err != nil {
		return nil, nil, err
	}
	relative, err := filepath.Rel(authorizationRoot, target)
	if err != nil {
		return nil, nil, err
	}
	if err = base.MkdirAll(relative, 0700); err != nil {
		return nil, nil, err
	}
	root, err := base.OpenRoot(relative)
	if err != nil {
		return nil, nil, err
	}
	pinned, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	if err = validatePinnedPath(pinned, target); err != nil {
		root.Close()
		pinned.Close()
		return nil, nil, err
	}
	return root, pinned, nil
}

func validatePinnedPath(file *os.File, path string) error {
	if runtime.GOOS == "linux" {
		actual, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", file.Fd()))
		if err != nil {
			return err
		}
		if actual != filepath.Clean(path) {
			return errors.New("restore directory changed during authorization")
		}
	} else {
		opened, err := file.Stat()
		if err != nil {
			return err
		}
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(opened, current) {
			return errors.New("restore directory changed during authorization")
		}
	}
	return nil
}

// Moving children preserves the mountpoint itself. Nested mounts have separate
// ownership/lifecycle and cannot be included in a content replacement.
func rejectNestedMounts(target string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := filepath.Clean(unescape.Replace(fields[4]))
		if mount != filepath.Clean(target) && beneath(target, mount) {
			return fmt.Errorf("replace target contains nested mount %s; map its data separately", mount)
		}
	}
	return nil
}

func saveRootMetadata(ctx context.Context, directory, target string, pinned *os.File) error {
	executable := "/usr/bin/tar"
	if runtime.GOOS != "linux" {
		if found, err := exec.LookPath("gtar"); err == nil {
			executable = found
		}
	}
	tarTarget := target
	if runtime.GOOS == "linux" {
		tarTarget = "/proc/self/fd/3"
	}
	cmd := exec.CommandContext(ctx, executable, "--create", "--file="+filepath.Join(directory, "root.tar"), "--format=pax", "--acls", "--xattrs", "--numeric-owner", "--no-recursion", "--directory="+tarTarget, "--", ".")
	if runtime.GOOS == "linux" {
		cmd.ExtraFiles = []*os.File{pinned}
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("capture rollback directory metadata: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func restoreArchiveRootMetadata(ctx context.Context, directory, archive, target string, pinned *os.File) error {
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	reader := tar.NewReader(helperContextReader{ctx: ctx, reader: input})
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if filepath.Clean(header.Name) != "." || header.Typeflag != tar.TypeDir {
			continue
		}
		rootArchive := filepath.Join(directory, "source-root.tar")
		output, createErr := os.OpenFile(rootArchive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return createErr
		}
		writer := tar.NewWriter(output)
		writeErr := writer.WriteHeader(header)
		archiveErr := writer.Close()
		closeErr := output.Close()
		if err = errors.Join(writeErr, archiveErr, closeErr); err != nil {
			return err
		}
		return extractPinned(ctx, rootArchive, target, pinned)
	}
}

func rejectRecoveryPrivateTarget(target, stateRoot string) error {
	for _, private := range []string{stateRoot, restoreSpoolRoot} {
		if beneath(private, target) || beneath(target, private) {
			return errors.New("online recovery cannot overwrite private Stacklab state or immutable helper archives")
		}
	}
	return nil
}

func validateAuthorizationRoot(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return fmt.Errorf("approved recovery root must already exist: %w", err)
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return validatePinnedPath(file, path)
}
