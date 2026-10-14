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

	"stacklab/internal/atomicfile"
)

const maxCatalogEntries = 100000
const maxCatalogBytes = 12 << 20

// ErrIndexUnavailable describes a bounded inspection limitation, not damaged data.
var ErrIndexUnavailable = errors.New("source file index unavailable")

// FileIndex is stored with the source archive inside the encrypted snapshot.
// It describes captured bytes, never a later traversal of a live source.
type FileIndex struct {
	Version  int         `json:"version"`
	SourceID string      `json:"source_id"`
	Entries  []FileEntry `json:"entries"`
}

type FileEntry struct {
	Path           string    `json:"path"`
	Type           string    `json:"type"`
	SizeBytes      int64     `json:"size_bytes"`
	ModifiedAt     time.Time `json:"modified_at"`
	SHA256         string    `json:"sha256,omitempty"`
	MetadataSHA256 string    `json:"metadata_sha256,omitempty"`
	LinkTarget     string    `json:"link_target,omitempty"`
	Mode           int64     `json:"mode"`
	UID            int       `json:"uid"`
	GID            int       `json:"gid"`
}

type FileSelection struct {
	SourceID string `json:"source_id"`
	Path     string `json:"path"`
}

type CatalogQuery struct {
	SourceID string
	Path     string
	Search   string
	Offset   int
	Limit    int
}

type CatalogPage struct {
	Items                  []FileEntry      `json:"items"`
	Sources                []ManifestSource `json:"sources"`
	SourceID               string           `json:"source_id,omitempty"`
	Path                   string           `json:"path"`
	Total                  int              `json:"total"`
	Offset                 int              `json:"offset"`
	Limit                  int              `json:"limit"`
	IndexAvailable         bool             `json:"index_available"`
	IndexUnavailableReason string           `json:"index_unavailable_reason,omitempty"`
	TransferBytes          int64            `json:"transfer_bytes"`
}

type FileChange struct {
	Path            string     `json:"path"`
	Change          string     `json:"change"`
	Before          *FileEntry `json:"before,omitempty"`
	After           *FileEntry `json:"after,omitempty"`
	ContentChanged  bool       `json:"content_changed"`
	MetadataChanged bool       `json:"metadata_changed"`
}

type FileComparison struct {
	Items                  []FileChange `json:"items"`
	Total                  int          `json:"total"`
	Offset                 int          `json:"offset"`
	Limit                  int          `json:"limit"`
	IndexAvailable         bool         `json:"index_available"`
	IndexUnavailableReason string       `json:"index_unavailable_reason,omitempty"`
	Added                  int          `json:"added"`
	Removed                int          `json:"removed"`
	Modified               int          `json:"modified"`
}

func catalogPath(value string, allowRoot bool) (string, error) {
	if len(value) > 4096 || strings.ContainsAny(value, "\\\x00\r\n") || path.IsAbs(value) {
		return "", fmt.Errorf("%w: invalid relative file path", ErrInvalid)
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: file path escapes source", ErrInvalid)
	}
	if clean == "." {
		if allowRoot {
			return "", nil
		}
		return "", fmt.Errorf("%w: select a file", ErrInvalid)
	}
	return clean, nil
}

func normalizeCatalogQuery(query CatalogQuery) (CatalogQuery, error) {
	var err error
	query.Path, err = catalogPath(query.Path, true)
	if err != nil {
		return query, err
	}
	if query.SourceID != "" && !safeID.MatchString(query.SourceID) || len(query.Search) > 256 || strings.ContainsAny(query.Search, "\x00\r\n") || query.Offset < 0 || query.Limit < 0 || query.Limit > 500 {
		return query, fmt.Errorf("%w: invalid catalog query", ErrInvalid)
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	return query, nil
}

func indexArchive(ctx context.Context, archivePath, sourceID string) (FileIndex, error) {
	result := FileIndex{Version: 1, SourceID: sourceID, Entries: []FileEntry{}}
	if !safeID.MatchString(sourceID) {
		return result, fmt.Errorf("%w: invalid source identity", ErrInvalid)
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return result, errors.New("source archive must be a regular file")
	}
	if _, err = ValidateCapturedArchive(contextReader{ctx: ctx, reader: file}); err != nil {
		return result, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	reader := tar.NewReader(contextReader{ctx: ctx, reader: file})
	entries := map[string]FileEntry{}
	explicit := map[string]bool{}
	encodedBytes := 0
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return result, nextErr
		}
		if path.Clean(header.Name) == "." {
			continue
		}
		name, nameErr := catalogPath(header.Name, false)
		if nameErr != nil {
			return result, fmt.Errorf("%w: a captured path cannot be represented in the file browser", ErrIndexUnavailable)
		}
		if explicit[name] {
			return result, errors.New("source archive contains duplicate paths")
		}
		explicit[name] = true
		entry := FileEntry{Path: name, ModifiedAt: header.ModTime.UTC(), Mode: header.Mode, UID: header.Uid, GID: header.Gid}
		metadata := map[string]string{}
		for key, value := range header.Xattrs {
			metadata["xattr:"+key] = value
		}
		for key, value := range header.PAXRecords {
			if strings.Contains(key, ".acl.") || strings.Contains(key, ".xattr.") {
				metadata[key] = value
			}
		}
		if len(metadata) > 0 {
			encoded, _ := json.Marshal(metadata)
			digest := sha256.Sum256(encoded)
			entry.MetadataSHA256 = hex.EncodeToString(digest[:])
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			entry.Type = "file"
			entry.SizeBytes = header.Size
			hash := sha256.New()
			if _, err = io.Copy(hash, reader); err != nil {
				return result, err
			}
			entry.SHA256 = hex.EncodeToString(hash.Sum(nil))
		case tar.TypeDir:
			entry.Type = "directory"
		case tar.TypeSymlink:
			entry.Type = "symlink"
			entry.LinkTarget = header.Linkname
		case tar.TypeLink:
			entry.Type = "hardlink"
			entry.LinkTarget = path.Clean(header.Linkname)
		case tar.TypeFifo:
			entry.Type = "fifo"
		case tar.TypeChar:
			entry.Type = "character_device"
		case tar.TypeBlock:
			entry.Type = "block_device"
		default:
			return result, fmt.Errorf("%w: unsupported indexed file type", ErrIndexUnavailable)
		}
		entries[name] = entry
		encoded, _ := json.Marshal(entry)
		encodedBytes += len(encoded) + 1
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, exists := entries[parent]; !exists {
				entries[parent] = FileEntry{Path: parent, Type: "directory"}
				encodedBytes += len(parent) + 200
			}
		}
		if len(entries) > maxCatalogEntries || encodedBytes > maxCatalogBytes-1024 {
			return result, fmt.Errorf("%w: source exceeds 100000 entries or 12 MiB of index metadata", ErrIndexUnavailable)
		}
	}
	for _, entry := range entries {
		result.Entries = append(result.Entries, entry)
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, nil
}

func writeSourceIndex(ctx context.Context, archivePath, stagingDir string, source *ManifestSource, remaining int64) (int64, error) {
	index, err := indexArchive(ctx, archivePath, source.Source.ID)
	if errors.Is(err, ErrIndexUnavailable) {
		source.IndexUnavailableReason = err.Error()
		source.IndexPath, source.IndexVersion, source.EntryCount = "", 0, 0
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(index)
	if err != nil {
		return 0, err
	}
	if len(data) > maxCatalogBytes || int64(len(data)) > remaining {
		source.IndexUnavailableReason = "Source index exceeds the available staging budget or 12 MiB; the complete archive is preserved."
		source.IndexPath, source.IndexVersion, source.EntryCount = "", 0, 0
		return 0, nil
	}
	relative := "indexes/" + source.Source.ID + ".json"
	if err = os.MkdirAll(filepath.Join(stagingDir, "indexes"), 0700); err != nil {
		return 0, err
	}
	if err = atomicfile.WriteBytesMode(filepath.Join(stagingDir, filepath.FromSlash(relative)), data, ".index-*", 0600); err != nil {
		return 0, err
	}
	source.IndexUnavailableReason = ""
	source.IndexPath = relative
	source.IndexVersion = 1
	source.EntryCount = len(index.Entries)
	return int64(len(data)), nil
}

func decodeIndex(data []byte, source ManifestSource) (FileIndex, error) {
	var index FileIndex
	if len(data) > maxCatalogBytes {
		return index, errors.New("source index exceeds 12 MiB")
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, errors.New("invalid source file index")
	}
	if index.Version != 1 || index.SourceID != source.Source.ID || len(index.Entries) > maxCatalogEntries {
		return index, errors.New("unsupported or oversized source file index")
	}
	seen := map[string]bool{}
	for _, entry := range index.Entries {
		name, err := catalogPath(entry.Path, false)
		if err != nil || name != entry.Path || seen[name] || entry.SizeBytes < 0 || entry.SizeBytes > 1<<60 || entry.Mode < 0 || entry.Mode&^07777 != 0 || entry.UID < 0 || entry.GID < 0 {
			return index, errors.New("invalid source index entry")
		}
		seen[name] = true
		if entry.MetadataSHA256 != "" {
			if len(entry.MetadataSHA256) != 64 {
				return index, errors.New("invalid source metadata digest")
			}
			if _, err := hex.DecodeString(entry.MetadataSHA256); err != nil {
				return index, errors.New("invalid source metadata digest")
			}
		}
		switch entry.Type {
		case "file":
			if len(entry.SHA256) != 64 {
				return index, errors.New("invalid source content digest")
			}
			if _, err = hex.DecodeString(entry.SHA256); err != nil {
				return index, errors.New("invalid source content digest")
			}
		case "directory", "fifo", "character_device", "block_device":
		case "symlink", "hardlink":
			if len(entry.LinkTarget) > 4096 || strings.ContainsAny(entry.LinkTarget, "\x00\r\n") {
				return index, errors.New("invalid indexed link")
			}
		default:
			return index, errors.New("unsupported source index entry")
		}
	}
	return index, nil
}

func validateCatalogManifest(manifest Manifest) error {
	if manifest.Version != 1 {
		return fmt.Errorf("%w: unsupported recovery manifest", errUnsupportedSnapshot)
	}
	if len(manifest.Sources) > 10000 {
		return fmt.Errorf("%w: too many sources in recovery manifest", errInvalidSnapshot)
	}
	seen := map[string]bool{}
	for _, source := range manifest.Sources {
		if !safeID.MatchString(source.Source.ID) || seen[source.Source.ID] || source.SizeBytes < 0 || source.SizeBytes > 1<<60 {
			return fmt.Errorf("%w: invalid source in recovery manifest", errInvalidSnapshot)
		}
		seen[source.Source.ID] = true
		relative, err := catalogPath(source.SavedPath, false)
		if err != nil || relative != source.SavedPath {
			return fmt.Errorf("%w: invalid saved source path", errInvalidSnapshot)
		}
		if source.IndexPath != "" && source.IndexVersion != 1 {
			return fmt.Errorf("%w: unsupported source file index", errUnsupportedSnapshot)
		}
		if source.IndexPath != "" && source.IndexPath != "indexes/"+source.Source.ID+".json" {
			return fmt.Errorf("%w: invalid source file index path", errInvalidSnapshot)
		}
	}
	return nil
}

func (s *Service) snapshotManifest(ctx context.Context, d Destination, id string) (Manifest, error) {
	var manifest Manifest
	if !snapshotID.MatchString(id) {
		return manifest, fmt.Errorf("%w: invalid snapshot ID", ErrInvalid)
	}
	output, err := s.restic(ctx, d, "", "dump", id, "/manifest.json")
	if err != nil {
		return manifest, err
	}
	if len(output) > 16<<20 || json.Unmarshal(output, &manifest) != nil {
		return manifest, fmt.Errorf("%w: invalid snapshot recovery manifest", errInvalidSnapshot)
	}
	if err = validateCatalogManifest(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}
func catalogSource(manifest Manifest, id string) (ManifestSource, error) {
	for _, source := range manifest.Sources {
		if source.Source.ID == id {
			return source, nil
		}
	}
	return ManifestSource{}, ErrNotFound
}
func (s *Service) snapshotIndex(ctx context.Context, d Destination, snapshot string, source ManifestSource) (FileIndex, bool, error) {
	if source.IndexPath == "" {
		return FileIndex{}, false, nil
	}
	output, err := s.restic(ctx, d, "", "dump", snapshot, "/"+source.IndexPath)
	if err != nil {
		return FileIndex{}, false, err
	}
	index, err := decodeIndex(output, source)
	return index, err == nil, err
}
func entryMatches(entryPath string, query CatalogQuery) bool {
	if query.Path != "" && !strings.HasPrefix(entryPath, query.Path+"/") {
		return false
	}
	if query.Search != "" {
		return strings.Contains(strings.ToLower(entryPath), strings.ToLower(query.Search))
	}
	parent := path.Dir(entryPath)
	if parent == "." {
		parent = ""
	}
	return parent == query.Path
}
func catalogPage(manifest Manifest, index FileIndex, available bool, query CatalogQuery) (CatalogPage, error) {
	page := CatalogPage{Items: []FileEntry{}, Sources: manifest.Sources, SourceID: query.SourceID, Path: query.Path, Offset: query.Offset, Limit: query.Limit, IndexAvailable: available}
	if page.Sources == nil {
		page.Sources = []ManifestSource{}
	}
	if query.SourceID == "" {
		for _, source := range manifest.Sources {
			if page.TransferBytes > 1<<60-source.SizeBytes {
				return page, errors.New("source transfer size overflow")
			}
			page.TransferBytes += source.SizeBytes
		}
		return page, nil
	}
	source, err := catalogSource(manifest, query.SourceID)
	if err != nil {
		return page, err
	}
	page.TransferBytes = source.SizeBytes
	if !available {
		page.IndexUnavailableReason = source.IndexUnavailableReason
		if page.IndexUnavailableReason == "" {
			page.IndexUnavailableReason = "This snapshot has no file index. Isolated recovery may build a bounded local index."
		}
	}
	filtered := []FileEntry{}
	for _, entry := range index.Entries {
		if entryMatches(entry.Path, query) {
			filtered = append(filtered, entry)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if (filtered[i].Type == "directory") != (filtered[j].Type == "directory") {
			return filtered[i].Type == "directory"
		}
		return filtered[i].Path < filtered[j].Path
	})
	page.Total = len(filtered)
	start := min(query.Offset, page.Total)
	end := start + min(query.Limit, page.Total-start)
	page.Items = filtered[start:end]
	return page, nil
}

func (s *Service) SnapshotCatalog(ctx context.Context, destinationID, id string, query CatalogQuery) (CatalogPage, error) {
	if !snapshotID.MatchString(id) {
		return CatalogPage{}, fmt.Errorf("%w: invalid snapshot ID", ErrInvalid)
	}
	query, err := normalizeCatalogQuery(query)
	if err != nil {
		return CatalogPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	d, release, err := s.beginRepositoryRead(ctx, destinationID)
	if err != nil {
		return CatalogPage{}, err
	}
	defer release()
	manifest, err := s.snapshotManifest(ctx, d, id)
	if err != nil {
		return CatalogPage{}, err
	}
	index := FileIndex{}
	available := false
	if query.SourceID != "" {
		source, sourceErr := catalogSource(manifest, query.SourceID)
		if sourceErr != nil {
			return CatalogPage{}, sourceErr
		}
		index, available, err = s.snapshotIndex(ctx, d, id, source)
		if err != nil {
			return CatalogPage{}, err
		}
	} else {
		available = true
		for _, source := range manifest.Sources {
			if source.IndexPath == "" {
				available = false
			}
		}
	}
	return catalogPage(manifest, index, available, query)
}

func (s *Service) CompareSnapshotFiles(ctx context.Context, destinationID, id, baseID string, query CatalogQuery) (FileComparison, error) {
	query, err := normalizeCatalogQuery(query)
	result := FileComparison{Items: []FileChange{}, Offset: query.Offset, Limit: query.Limit}
	if err != nil {
		return result, err
	}
	if query.SourceID == "" {
		return result, fmt.Errorf("%w: source_id is required for comparison", ErrInvalid)
	}
	if !snapshotID.MatchString(id) || !snapshotID.MatchString(baseID) {
		return result, fmt.Errorf("%w: snapshot and base_snapshot_id must be hexadecimal IDs", ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	d, release, err := s.beginRepositoryRead(ctx, destinationID)
	if err != nil {
		return result, err
	}
	defer release()
	current, err := s.snapshotManifest(ctx, d, id)
	if err != nil {
		return result, err
	}
	base, err := s.snapshotManifest(ctx, d, baseID)
	if err != nil {
		return result, err
	}
	currentSource, currentErr := catalogSource(current, query.SourceID)
	baseSource, baseErr := catalogSource(base, query.SourceID)
	if currentErr != nil && baseErr != nil {
		return result, ErrNotFound
	}
	currentIndex := FileIndex{}
	baseIndex := FileIndex{}
	currentAvailable, baseAvailable := true, true
	if currentErr == nil {
		currentIndex, currentAvailable, err = s.snapshotIndex(ctx, d, id, currentSource)
		if err != nil {
			return result, err
		}
	}
	if baseErr == nil {
		baseIndex, baseAvailable, err = s.snapshotIndex(ctx, d, baseID, baseSource)
		if err != nil {
			return result, err
		}
	}
	result.IndexAvailable = currentAvailable && baseAvailable
	if !result.IndexAvailable {
		result.IndexUnavailableReason = "Comparison requires complete indexes for both sources."
		return result, nil
	}
	before := map[string]FileEntry{}
	after := map[string]FileEntry{}
	names := map[string]bool{}
	for _, entry := range baseIndex.Entries {
		before[entry.Path] = entry
		names[entry.Path] = true
	}
	for _, entry := range currentIndex.Entries {
		after[entry.Path] = entry
		names[entry.Path] = true
	}
	changes := []FileChange{}
	for name := range names {
		old, had := before[name]
		fresh, has := after[name]
		change := FileChange{Path: name}
		if !had {
			change.Change = "added"
			change.After = &fresh
			result.Added++
		} else if !has {
			change.Change = "removed"
			change.Before = &old
			result.Removed++
		} else {
			change.ContentChanged = old.Type != fresh.Type || old.SizeBytes != fresh.SizeBytes || old.SHA256 != fresh.SHA256 || old.LinkTarget != fresh.LinkTarget
			change.MetadataChanged = !old.ModifiedAt.Equal(fresh.ModifiedAt) || old.Mode != fresh.Mode || old.UID != fresh.UID || old.GID != fresh.GID || old.MetadataSHA256 != fresh.MetadataSHA256
			if !change.ContentChanged && !change.MetadataChanged {
				continue
			}
			change.Change = "modified"
			change.Before = &old
			change.After = &fresh
			result.Modified++
		}
		// Comparison covers the selected subtree, unlike directory browsing.
		// A root comparison must include changes nested in any child directory.
		within := query.Path == "" || name == query.Path || strings.HasPrefix(name, query.Path+"/")
		matchesSearch := query.Search == "" || strings.Contains(strings.ToLower(name), strings.ToLower(query.Search))
		if within && matchesSearch {
			changes = append(changes, change)
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	result.Total = len(changes)
	start := min(query.Offset, result.Total)
	end := start + min(query.Limit, result.Total-start)
	result.Items = changes[start:end]
	return result, nil
}
