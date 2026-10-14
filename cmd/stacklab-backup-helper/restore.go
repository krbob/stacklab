package main

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"stacklab/internal/atomicfile"
	"stacklab/internal/backup"
)

var restoreSpoolRoot = "/var/lib/stacklab-backup-helper"

type preparedRestore struct {
	Target            string   `json:"target"`
	Mode              string   `json:"mode"`
	Phase             string   `json:"phase"`
	Rollback          string   `json:"rollback,omitempty"`
	Moved             []string `json:"moved,omitempty"`
	AuthorizationRoot string   `json:"authorization_root,omitempty"`
	RootMetadata      bool     `json:"root_metadata,omitempty"`
	SourceScope       string   `json:"source_scope,omitempty"`
}

func validateRestoreSpool(allowMissing bool) error {
	// The dedicated spool must not be beneath service-writable state. Check the
	// complete existing path before creation; the installation creates its root.
	for current := filepath.Clean(restoreSpoolRoot); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
				return errors.New("restore spool and its ancestors must be root-owned directories without group or other write access")
			}
		} else if !allowMissing || !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if !allowMissing {
		info, err := os.Lstat(restoreSpoolRoot)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0700 {
			return errors.New("restore spool must have mode 0700; repair its permissions before recovery")
		}
	}
	return nil
}

func privateRestoreSpool() error {
	if err := validateRestoreSpool(true); err != nil {
		return err
	}
	if err := os.MkdirAll(restoreSpoolRoot, 0700); err != nil {
		return err
	}
	if err := os.Chmod(restoreSpoolRoot, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(restoreSpoolRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		directory, pathErr := preparedPath(entry.Name())
		if pathErr != nil || !entry.IsDir() {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil || time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(directory, "state.json"))
		if errors.Is(readErr, os.ErrNotExist) {
			// A killed preparation may leave only a partial archive. No target writes
			// can begin until its durable prepared state exists.
			_ = os.RemoveAll(directory)
			continue
		}
		if readErr != nil {
			continue
		}
		var state preparedRestore
		if json.Unmarshal(data, &state) == nil && (state.Phase == "prepared" || state.Phase == "completed" || state.Phase == "rolled_back") {
			_ = os.RemoveAll(directory)
		}
	}
	return nil
}

func preparedPath(token string) (string, error) {
	if len(token) != 48 {
		return "", errors.New("invalid prepared recovery token")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("invalid prepared recovery token")
	}
	return filepath.Join(restoreSpoolRoot, token), nil
}

func savePrepared(directory string, state preparedRestore) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicfile.WriteBytesMode(filepath.Join(directory, "state.json"), data, ".state-*", 0600)
}

func validatePreparedTarget(ctx context.Context, archive, target, mode string) error {
	if mode != "replace" && mode != "merge" {
		return errors.New("restore mode must be replace or merge")
	}
	if info, err := os.Lstat(target); err == nil && !info.IsDir() {
		return errors.New("restore target must be a directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if mode == "replace" {
		if err := rejectNestedMounts(target); err != nil {
			return err
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		for _, entry := range entries {
			if token, found := strings.CutPrefix(entry.Name(), ".stacklab-rollback-"); found {
				if _, tokenErr := preparedPath(token); tokenErr == nil {
					return errors.New("target contains retained rollback data; inspect and resolve the previous recovery before replacing it")
				}
			}
		}
	}
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	archiveInfo, err := input.Stat()
	if err != nil {
		return err
	}
	ancestor := target
	for {
		if _, err = os.Stat(ancestor); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return errors.New("restore target has no existing parent")
		}
		ancestor = parent
	}
	var space unix.Statfs_t
	if err = unix.Statfs(ancestor, &space); err != nil {
		return err
	}
	available := uint64(space.Bavail) * uint64(space.Bsize)
	if available < 128<<20 || uint64(archiveInfo.Size()) > available-(128<<20) {
		return errors.New("insufficient free space for recovery and rollback (128 MiB reserve required)")
	}
	validationTarget := target
	if mode == "replace" {
		validationTarget = ""
	}
	return backup.ValidateArchive(helperContextReader{ctx: ctx, reader: input}, validationTarget)
}

func prepareRestore(ctx context.Context, input io.Reader, size int64, target, mode, sourceScope string) (token string, err error) {
	if err = validateRestoreScope(target, mode, sourceScope, nil); err != nil {
		return "", err
	}
	if size < 512 || size > 1<<50 {
		return "", errors.New("invalid archive size")
	}
	if err = privateRestoreSpool(); err != nil {
		return "", err
	}
	var stat unix.Statfs_t
	if err = unix.Statfs(restoreSpoolRoot, &stat); err != nil {
		return "", err
	}
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	if available < 128<<20 || uint64(size)*2 > available-(128<<20) {
		return "", errors.New("insufficient disk space for immutable recovery archive and extraction metadata (128 MiB reserve required)")
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	token = hex.EncodeToString(random)
	directory, _ := preparedPath(token)
	if err = os.Mkdir(directory, 0700); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	archive := filepath.Join(directory, "source.tar")
	output, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.CopyN(output, helperContextReader{ctx: ctx, reader: input}, size)
	syncErr := output.Sync()
	closeErr := output.Close()
	if err = errors.Join(copyErr, syncErr, closeErr); err != nil {
		return "", err
	}
	var extra [1]byte
	if n, readErr := input.Read(extra[:]); n != 0 || readErr != io.EOF {
		return "", errors.New("archive stream does not match the declared size")
	}
	if mode == "replace" {
		source, openErr := os.Open(archive)
		if openErr != nil {
			return "", openErr
		}
		hasRoot := false
		reader := tar.NewReader(helperContextReader{ctx: ctx, reader: source})
		for {
			header, readErr := reader.Next()
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				source.Close()
				return "", readErr
			}
			if filepath.Clean(header.Name) == "." && header.Typeflag == tar.TypeDir {
				hasRoot = true
				break
			}
		}
		if closeErr := source.Close(); closeErr != nil {
			return "", closeErr
		}
		if !hasRoot {
			return "", errors.New("complete-source replacement requires a directory archive; use merge for a single-file archive")
		}
	}
	if err = validatePreparedTarget(ctx, archive, target, mode); err != nil {
		return "", err
	}
	if err = savePrepared(directory, preparedRestore{Target: target, Mode: mode, SourceScope: sourceScope, Phase: "prepared"}); err != nil {
		return "", err
	}
	return token, nil
}

func runPreparedRestore(ctx context.Context, action string, args []string) error {
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	var token, target, volume, mode, sourceScope string
	var size int64
	flags.StringVar(&token, "token", "", "prepared recovery token")
	flags.StringVar(&target, "path", "", "approved target directory")
	flags.StringVar(&volume, "volume", "", "Docker local volume")
	flags.StringVar(&mode, "mode", "replace", "replace or merge")
	flags.StringVar(&sourceScope, "source-scope", "", "complete or filtered capture scope")
	flags.Int64Var(&size, "size", 0, "exact archive size")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	roots, stateRoot, err := trustedRoots()
	if err != nil {
		return err
	}
	if err = privateRestoreSpool(); err != nil {
		return err
	}
	if action == "prepare-restore" {
		if (target == "") == (volume == "") || token != "" {
			return errors.New("prepare requires exactly one target and no token")
		}
		if volume != "" {
			target, err = volumePath(volume)
		} else {
			target, err = authorizePath(target, roots, true)
			if err != nil && isLocalVolumePath(target) {
				err = nil
			}
		}
		if err != nil {
			return err
		}
		if err = rejectRecoveryPrivateTarget(target, stateRoot); err != nil {
			return err
		}
		if err = validateRestoreScope(target, mode, sourceScope, roots); err != nil {
			return err
		}
		if err = validateAuthorizationRoot(approvedRecoveryRoot(target, roots)); err != nil {
			return err
		}
		token, err = prepareRestore(ctx, os.Stdin, size, target, mode, sourceScope)
		if err == nil {
			_, err = fmt.Fprintln(os.Stdout, token)
		}
		return err
	}
	if target != "" || volume != "" || size != 0 || sourceScope != "" {
		return errors.New("prepared operation accepts only a token")
	}
	directory, err := preparedPath(token)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil {
		return err
	}
	var state preparedRestore
	if err = json.Unmarshal(data, &state); err != nil {
		return err
	}
	if action == "release-restore" {
		if state.Phase != "prepared" && state.Phase != "completed" && state.Phase != "rolled_back" {
			return fmt.Errorf("interrupted recovery retained for inspection at %s", directory)
		}
		return os.RemoveAll(directory)
	}
	if _, err = authorizePath(state.Target, roots, true); err != nil && !isLocalVolumePath(state.Target) {
		return err
	}
	if err = rejectRecoveryPrivateTarget(state.Target, stateRoot); err != nil {
		return err
	}
	if err = validateRestoreScope(state.Target, state.Mode, state.SourceScope, roots); err != nil {
		return err
	}
	archive := filepath.Join(directory, "source.tar")
	if state.Phase != "prepared" {
		return fmt.Errorf("recovery is %s; inspect %s before retrying", state.Phase, directory)
	}
	if err = validatePreparedTarget(ctx, archive, state.Target, state.Mode); err != nil {
		return err
	}
	if err = validateAuthorizationRoot(approvedRecoveryRoot(state.Target, roots)); err != nil {
		return err
	}
	if action == "check-restore" {
		return nil
	}
	state.AuthorizationRoot = approvedRecoveryRoot(state.Target, roots)
	return applyPreparedRestore(ctx, directory, archive, &state)
}
