package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) managedRestore(result RestoreResult) bool {
	base := filepath.Join(s.directory, "restores")
	target := filepath.Clean(result.Target)
	relative, err := filepath.Rel(base, target)
	return err == nil && relative != "." && !strings.Contains(relative, string(filepath.Separator)) && safeID.MatchString(relative)
}

// DeleteRestore removes a result from the library. Only Stacklab's direct
// restore directories are deleted; custom locations remain operator-owned.
func (s *Service) DeleteRestore(ctx context.Context, id string) error {
	if !safeID.MatchString(id) {
		return fmt.Errorf("%w: invalid recovery result ID", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	if s.configurationBusyLocked() {
		return ErrBusy
	}
	var target string
	var result RestoreResult
	for key, item := range s.state.Restores {
		if restoreIdentity(item) == id {
			target = key
			result = item
			break
		}
	}
	if target == "" {
		return ErrNotFound
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.managedRestore(result) {
		if err := s.deleteManagedRestore(result); err != nil {
			return err
		}
	}
	delete(s.state.Restores, target)
	order := s.state.RestoreOrder[:0]
	for _, key := range s.state.RestoreOrder {
		if key != target {
			order = append(order, key)
		}
	}
	s.state.RestoreOrder = order
	return s.persist(ctx)
}

func (s *Service) deleteManagedRestore(result RestoreResult) error {
	base := filepath.Join(s.directory, "restores")
	canonical, err := filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	if canonical != base {
		return fmt.Errorf("%w: recovery storage must not be a symlink", ErrInvalid)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(result.Target)
	info, err := root.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("%w: recovery directory changed; inspect it manually", ErrInvalid)
	}
	if err == nil {
		if err = root.RemoveAll(name); err != nil {
			return err
		}
	}
	return nil
}

// OpenRestoredArchive downloads the original bytes for operator-controlled
// offline recovery, including metadata prohibited by automatic extraction.
func (s *Service) OpenRestoredArchive(ctx context.Context, id, sourceID string) (FileDownload, error) {
	recovered, manifest, err := s.restoreCatalogManifest(ctx, id)
	if err != nil {
		return FileDownload{}, err
	}
	if len(recovered.SelectedFiles) > 0 {
		return FileDownload{}, fmt.Errorf("%w: archive downloads require complete-source recovery", ErrInvalid)
	}
	source, err := catalogSource(manifest, sourceID)
	if err != nil {
		return FileDownload{}, err
	}
	archivePath, err := ManifestArchivePath(recovered.Target, source)
	if err != nil {
		return FileDownload{}, err
	}
	root, err := os.OpenRoot(recovered.Target)
	if err != nil {
		return FileDownload{}, err
	}
	defer root.Close()
	relative, err := filepath.Rel(recovered.Target, archivePath)
	if err != nil {
		return FileDownload{}, err
	}
	before, err := root.Lstat(relative)
	if err != nil || !before.Mode().IsRegular() {
		return FileDownload{}, fmt.Errorf("%w: recovered archive is not a regular file", ErrInvalid)
	}
	file, err := root.Open(relative)
	if err != nil {
		return FileDownload{}, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		file.Close()
		return FileDownload{}, fmt.Errorf("%w: recovered archive changed", ErrInvalid)
	}
	return FileDownload{Reader: &archiveDownload{Reader: io.LimitReader(contextReader{ctx: ctx, reader: file}, after.Size()), file: file}, Name: source.Source.ID + ".tar", SizeBytes: after.Size(), ModifiedAt: after.ModTime()}, nil
}
