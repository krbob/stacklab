package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stacklab/internal/limitedio"
)

// FileDownload owns an open archive and streams only the selected regular file.
// Callers must close Reader even when the HTTP client disconnects.
type FileDownload struct {
	Reader     io.ReadCloser
	Name       string
	SizeBytes  int64
	ModifiedAt time.Time
}

type archiveDownload struct {
	io.Reader
	file *os.File
}

func (r *archiveDownload) Close() error { return r.file.Close() }

func restoreIdentity(result RestoreResult) string {
	if result.ID != "" {
		return result.ID
	}
	digest := sha256.Sum256([]byte(result.Target))
	return "restore_" + hex.EncodeToString(digest[:16])
}
func normalizeRestoreResult(result RestoreResult) RestoreResult {
	result.ID = restoreIdentity(result)
	if result.CreatedAt.IsZero() && result.Manifest != nil {
		result.CreatedAt = result.Manifest.CapturedAt
	}
	result.Available = false
	result.UnavailableReason = "Recovered data is unavailable; restore the snapshot again."
	canonical, err := filepath.EvalSymlinks(result.Target)
	if err == nil && canonical == filepath.Clean(result.Target) {
		if manifest, loadErr := LoadManifest(result.Target); loadErr == nil {
			result.Available = true
			result.UnavailableReason = ""
			for _, id := range result.SourceIDs {
				source, sourceErr := catalogSource(manifest, id)
				if sourceErr == nil {
					_, sourceErr = ManifestArchivePath(result.Target, source)
				}
				if sourceErr != nil {
					result.Available = false
					result.UnavailableReason = "A recovered source archive is unavailable; restore the snapshot again."
					break
				}
			}
			if len(result.SourceIDs) == 0 {
				// Legacy registrations did not record the selected source set.
				// Some absent archives may be intentional, but none remaining is
				// still an unavailable recovery result, not an empty success.
				hadArchives, availableArchive := false, false
				for _, source := range manifest.Sources {
					if !source.Archive {
						continue
					}
					hadArchives = true
					if _, sourceErr := ManifestArchivePath(result.Target, source); sourceErr == nil {
						availableArchive = true
					}
				}
				if hadArchives && !availableArchive {
					result.Available = false
					result.UnavailableReason = "Recovered source archives are unavailable; restore the snapshot again."
				}
			}
		}
	}
	return result
}
func (s *Service) registerRestoreResult(ctx context.Context, result RestoreResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Restores == nil {
		s.state.Restores = map[string]RestoreResult{}
	}
	result.ID = restoreIdentity(result)
	if _, exists := s.state.Restores[filepath.Clean(result.Target)]; !exists && len(s.state.Restores) >= 200 {
		return fmt.Errorf("%w: recovery library is full; remove an old recovery result first", ErrInvalid)
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = s.now()
	}
	if len(result.SourceIDs) == 0 && result.Manifest != nil {
		for _, source := range result.Manifest.Sources {
			if _, err := ManifestArchivePath(result.Target, source); err == nil {
				result.SourceIDs = append(result.SourceIDs, source.Source.ID)
			}
		}
	}
	target := filepath.Clean(result.Target)
	if _, exists := s.state.Restores[target]; !exists {
		s.state.RestoreOrder = append(s.state.RestoreOrder, target)
	}
	s.state.Restores[target] = clone(result)
	return s.persist(ctx)
}
func (s *Service) RestoreLibrary(ctx context.Context) ([]RestoreResult, error) {
	s.mu.Lock()
	if s.initErr != nil {
		s.mu.Unlock()
		return nil, s.initErr
	}
	results := make([]RestoreResult, 0, len(s.state.Restores))
	for _, result := range s.state.Restores {
		results = append(results, clone(result))
	}
	s.mu.Unlock()
	for index := range results {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results[index] = normalizeRestoreResult(results[index])
		results[index].Managed = s.managedRestore(results[index])
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].CreatedAt.Equal(results[j].CreatedAt) {
			return results[i].ID < results[j].ID
		}
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	return results, nil
}
func (s *Service) ResolveRestore(ctx context.Context, id string) (RestoreResult, error) {
	if !safeID.MatchString(id) {
		return RestoreResult{}, fmt.Errorf("%w: invalid recovery result ID", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return RestoreResult{}, s.initErr
	}
	for _, result := range s.state.Restores {
		if restoreIdentity(result) == id {
			normalized := normalizeRestoreResult(clone(result))
			normalized.Managed = s.managedRestore(normalized)
			return normalized, nil
		}
	}
	return RestoreResult{}, ErrNotFound
}
func (s *Service) restoreCatalogManifest(ctx context.Context, id string) (RestoreResult, Manifest, error) {
	result, err := s.ResolveRestore(ctx, id)
	if err != nil {
		return result, Manifest{}, err
	}
	if !result.Available {
		return result, Manifest{}, fmt.Errorf("%w: recovered data was removed; restore the snapshot again", ErrNotFound)
	}
	manifest, err := LoadManifest(result.Target)
	if err != nil {
		return result, manifest, err
	}
	if err = validateCatalogManifest(manifest); err != nil {
		return result, manifest, err
	}
	availableSources := []ManifestSource{}
	for _, source := range manifest.Sources {
		if !source.Archive {
			continue
		}
		if _, pathErr := ManifestArchivePath(result.Target, source); pathErr == nil {
			availableSources = append(availableSources, source)
		} else if !errors.Is(pathErr, os.ErrNotExist) {
			return result, manifest, pathErr
		}
	}
	manifest.Sources = availableSources
	return result, manifest, nil
}
func restoreAllowsFile(result RestoreResult, sourceID, filename string) bool {
	if len(result.SelectedFiles) == 0 {
		return true
	}
	for _, selection := range result.SelectedFiles {
		if selection.SourceID == sourceID && selection.Path == filename {
			return true
		}
	}
	return false
}
func filterRecoveredIndex(index FileIndex, result RestoreResult) FileIndex {
	if len(result.SelectedFiles) == 0 {
		return index
	}
	entries := make([]FileEntry, 0, len(result.SelectedFiles))
	for _, entry := range index.Entries {
		allowed := restoreAllowsFile(result, index.SourceID, entry.Path)
		if !allowed && entry.Type == "directory" {
			for _, selection := range result.SelectedFiles {
				if selection.SourceID == index.SourceID && strings.HasPrefix(selection.Path, entry.Path+"/") {
					allowed = true
					break
				}
			}
		}
		if allowed {
			entries = append(entries, entry)
		}
	}
	index.Entries = entries
	return index
}
func (s *Service) RestoredCatalog(ctx context.Context, id string, query CatalogQuery) (CatalogPage, error) {
	query, err := normalizeCatalogQuery(query)
	if err != nil {
		return CatalogPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, manifest, err := s.restoreCatalogManifest(ctx, id)
	if err != nil {
		return CatalogPage{}, err
	}
	index := FileIndex{}
	if query.SourceID != "" {
		source, sourceErr := catalogSource(manifest, query.SourceID)
		if sourceErr != nil {
			return CatalogPage{}, sourceErr
		}
		archive, archiveErr := ManifestArchivePath(result.Target, source)
		if archiveErr != nil {
			return CatalogPage{}, archiveErr
		}
		index, err = restoredIndex(ctx, result.Target, archive, source)
		if errors.Is(err, ErrIndexUnavailable) {
			page, pageErr := catalogPage(manifest, FileIndex{}, false, query)
			page.IndexUnavailableReason = err.Error()
			return page, pageErr
		}
		if err != nil {
			return CatalogPage{}, err
		}
		index = filterRecoveredIndex(index, result)
	}
	return catalogPage(manifest, index, true, query)
}

// Indexes affect browsing only. Downloads and reviewed apply independently
// validate the actual archive bytes. Legacy indexes are generated once inside
// the private recovery directory, without modifying the repository manifest.
func restoredIndex(ctx context.Context, target, archive string, source ManifestSource) (FileIndex, error) {
	if source.IndexPath == "" && source.IndexUnavailableReason != "" {
		return FileIndex{}, fmt.Errorf("%w: %s", ErrIndexUnavailable, source.IndexUnavailableReason)
	}
	indexPath := filepath.Join(target, "indexes", source.Source.ID+".json")
	info, err := os.Lstat(indexPath)
	if err == nil {
		canonical, canonicalErr := filepath.EvalSymlinks(indexPath)
		if canonicalErr != nil || canonical != indexPath || !info.Mode().IsRegular() || info.Size() > maxCatalogBytes {
			return FileIndex{}, errors.New("recovered index must be a bounded regular file without symlinks")
		}
		data, readErr := limitedio.ReadFile(indexPath, maxCatalogBytes)
		if readErr != nil {
			return FileIndex{}, readErr
		}
		return decodeIndex(data, source)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return FileIndex{}, err
	}
	index, err := indexArchive(ctx, archive, source.Source.ID)
	if err != nil {
		return index, err
	}
	data, err := json.Marshal(index)
	if err != nil || len(data) > maxCatalogBytes {
		return index, fmt.Errorf("%w: recovered index exceeds supported bounds", ErrIndexUnavailable)
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return index, err
	}
	defer root.Close()
	if err = root.MkdirAll("indexes", 0700); err != nil {
		return index, err
	}
	directoryInfo, err := root.Lstat("indexes")
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return index, errors.New("recovered index directory must not be a symlink")
	}
	temporary := "indexes/." + newID("catalog_")
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return index, err
	}
	defer root.Remove(temporary)
	_, writeErr := file.Write(data)
	if err = errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return index, err
	}
	return index, root.Rename(temporary, "indexes/"+source.Source.ID+".json")
}

func (s *Service) OpenRestoredFile(ctx context.Context, id, sourceID, filename string) (FileDownload, error) {
	var result FileDownload
	filename, err := catalogPath(filename, false)
	if err != nil {
		return result, err
	}
	recovered, manifest, err := s.restoreCatalogManifest(ctx, id)
	if err != nil {
		return result, err
	}
	if !restoreAllowsFile(recovered, sourceID, filename) {
		return result, ErrNotFound
	}
	source, err := catalogSource(manifest, sourceID)
	if err != nil {
		return result, err
	}
	archivePath, err := ManifestArchivePath(recovered.Target, source)
	if err != nil {
		return result, err
	}
	root, err := os.OpenRoot(recovered.Target)
	if err != nil {
		return result, err
	}
	defer root.Close()
	relative, err := filepath.Rel(recovered.Target, archivePath)
	if err != nil {
		return result, err
	}
	before, err := root.Lstat(relative)
	if err != nil || !before.Mode().IsRegular() {
		return result, errors.New("recovered archive must be a regular file")
	}
	file, err := root.Open(relative)
	if err != nil {
		return result, err
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return result, errors.New("recovered archive changed during access")
	}
	if _, err = ValidateCapturedArchive(contextReader{ctx: ctx, reader: file}); err != nil {
		return result, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	reader := tar.NewReader(contextReader{ctx: ctx, reader: file})
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return result, ErrNotFound
		}
		if nextErr != nil {
			return result, nextErr
		}
		if path.Clean(header.Name) != filename {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return result, fmt.Errorf("%w: only regular files can be downloaded", ErrInvalid)
		}
		result = FileDownload{Reader: &archiveDownload{Reader: io.LimitReader(reader, header.Size), file: file}, Name: path.Base(filename), SizeBytes: header.Size, ModifiedAt: header.ModTime}
		success = true
		return result, nil
	}
}

// ExtractSelectedFiles materializes only regular entries in a new private
// directory. Whole archives remain separate for inspection, never applied as a
// misleading complete-source restore after a partial file selection.
func ExtractSelectedFiles(ctx context.Context, archivePath, target string, paths []string) error {
	if len(paths) == 0 || len(paths) > 100 {
		return fmt.Errorf("%w: select between 1 and 100 regular files", ErrInvalid)
	}
	wanted := map[string]bool{}
	for _, filename := range paths {
		name, err := catalogPath(filename, false)
		if err != nil || name != filename {
			return fmt.Errorf("%w: invalid selected file path", ErrInvalid)
		}
		if wanted[name] {
			return fmt.Errorf("%w: duplicate selected file", ErrInvalid)
		}
		wanted[name] = true
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = ValidateCapturedArchive(contextReader{ctx: ctx, reader: file}); err != nil {
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := tar.NewReader(contextReader{ctx: ctx, reader: file})
	found := map[string]bool{}
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		name := path.Clean(header.Name)
		if !wanted[name] {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("%w: selected entry is not a regular file", ErrInvalid)
		}
		if found[name] {
			return fmt.Errorf("%w: source archive contains duplicate paths", ErrInvalid)
		}
		found[name] = true
	}
	if len(found) != len(wanted) {
		return fmt.Errorf("%w: selected file is absent from the source archive", ErrNotFound)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil || parent != filepath.Clean(filepath.Dir(target)) {
		return fmt.Errorf("%w: isolated extraction parent must not contain symlinks", ErrInvalid)
	}
	if err = os.Mkdir(target, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	reader = tar.NewReader(contextReader{ctx: ctx, reader: file})
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		name := path.Clean(header.Name)
		if !wanted[name] {
			continue
		}
		if err = root.MkdirAll(filepath.FromSlash(path.Dir(name)), 0700); err != nil {
			return err
		}
		output, openErr := root.OpenFile(filepath.FromSlash(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if openErr != nil {
			return openErr
		}
		_, copyErr := io.Copy(output, reader)
		closeErr := output.Close()
		if err = errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		// Inspection files deliberately remain private; preserved ownership/ACLs
		// remain available in the untouched source archive for reviewed recovery.
	}
}
