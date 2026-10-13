package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type catalogFixtureEntry struct {
	name, content, link string
	kind                byte
	mode                int64
	mtime               time.Time
}

func writeCatalogFixture(t *testing.T, filename string, entries []catalogFixtureEntry) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0600
		}
		header := &tar.Header{Name: entry.name, Typeflag: kind, Mode: mode, ModTime: entry.mtime, Linkname: entry.link}
		if kind == tar.TypeReg {
			header.Size = int64(len(entry.content))
		}
		if err = writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err = io.WriteString(writer, entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestSourceCatalogIndexesCapturedBytesAndBounds(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "source.tar")
	stamp := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	writeCatalogFixture(t, archive, []catalogFixtureEntry{{name: "folder/data.txt", content: "captured", mtime: stamp}, {name: "linked", kind: tar.TypeSymlink, link: "folder/data.txt"}})
	source := ManifestSource{Source: Source{ID: "source-a"}, SavedPath: "source.tar", Archive: true}
	if _, err := writeSourceIndex(context.Background(), archive, dir, &source, 1); err == nil {
		t.Fatal("index ignored staging budget")
	}
	size, err := writeSourceIndex(context.Background(), archive, dir, &source, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 || source.IndexVersion != 1 || source.EntryCount != 3 || source.IndexPath != "indexes/source-a.json" {
		t.Fatalf("index metadata: %+v", source)
	}
	data, err := os.ReadFile(filepath.Join(dir, source.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	index, err := decodeIndex(data, source)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("captured"))
	if index.Entries[1].SHA256 != hex.EncodeToString(digest[:]) || !index.Entries[1].ModifiedAt.Equal(stamp) {
		t.Fatalf("captured metadata: %+v", index.Entries[1])
	}
	if _, err = decodeIndex(make([]byte, maxCatalogBytes+1), source); err == nil {
		t.Fatal("oversized index accepted")
	}
	index.Entries[0].Path = "../escape"
	unsafe, _ := json.Marshal(index)
	if _, err = decodeIndex(unsafe, source); err == nil {
		t.Fatal("unsafe index accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = indexArchive(ctx, archive, source.Source.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestCatalogDirectorySearchPaginationAndQueryValidation(t *testing.T) {
	manifest := Manifest{Sources: []ManifestSource{{Source: Source{ID: "source-a"}, SizeBytes: 5120}}}
	index := FileIndex{Entries: []FileEntry{{Path: "folder", Type: "directory"}, {Path: "folder/inside", Type: "file"}, {Path: "alpha", Type: "file"}, {Path: "beta", Type: "file"}}}
	query, err := normalizeCatalogQuery(CatalogQuery{SourceID: "source-a", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	page, err := catalogPage(manifest, index, true, query)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].Path != "alpha" || page.TransferBytes != 5120 {
		t.Fatalf("page: %+v", page)
	}
	query.Search = "INSIDE"
	query.Offset = 0
	page, _ = catalogPage(manifest, index, true, query)
	if page.Total != 1 || page.Items[0].Path != "folder/inside" {
		t.Fatalf("search: %+v", page)
	}
	for _, query = range []CatalogQuery{{Path: "../escape"}, {Path: "/root"}, {Limit: 501}, {Offset: -1}, {SourceID: "../../"}} {
		if _, err = normalizeCatalogQuery(query); err == nil {
			t.Fatalf("accepted %+v", query)
		}
	}
}
func registerCatalogFixture(t *testing.T, s *Service, entries []catalogFixtureEntry, selected []FileSelection) RestoreResult {
	t.Helper()
	target := t.TempDir()
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	target = canonical
	filename := filepath.Join(target, "source.tar")
	writeCatalogFixture(t, filename, entries)
	info, _ := os.Stat(filename)
	manifest := Manifest{Version: 1, CapturedAt: time.Now().UTC(), Sources: []ManifestSource{{Source: Source{ID: "source-a", Kind: "managed_config"}, SavedPath: "source.tar", Archive: true, SizeBytes: info.Size()}}}
	raw, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(target, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	result := RestoreResult{Target: target, Manifest: &manifest, SelectedFiles: selected}
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	result, err = s.ResolveRestore(context.Background(), restoreIdentity(result))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestLegacyRestoreLibraryCatalogDownloadAndPartialScope(t *testing.T) {
	s, _, _ := newTestService(t)
	result := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "folder/data", content: "download exact bytes"}, {name: "other", content: "not selected"}, {name: "link", kind: tar.TypeSymlink, link: "other"}}, nil)
	if result.ID == "" || result.CreatedAt.IsZero() || !result.Available {
		t.Fatalf("legacy result %+v", result)
	}
	page, err := s.RestoredCatalog(context.Background(), result.ID, CatalogQuery{SourceID: "source-a", Path: "folder"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("legacy catalog %+v %v", page, err)
	}
	download, err := s.OpenRestoredFile(context.Background(), result.ID, "source-a", "folder/data")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(download.Reader)
	_ = download.Reader.Close()
	if err != nil || string(got) != "download exact bytes" || download.Name != "data" || download.SizeBytes != int64(len(got)) {
		t.Fatalf("download %q %v", got, err)
	}
	if _, err = s.OpenRestoredFile(context.Background(), result.ID, "source-a", "link"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("link download: %v", err)
	}
	if _, err = s.OpenRestoredFile(context.Background(), result.ID, "source-a", "../escape"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal: %v", err)
	}
	if _, err = s.ResolveRestore(context.Background(), "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unregistered: %v", err)
	}
	result.SelectedFiles = []FileSelection{{SourceID: "source-a", Path: "folder/data"}}
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	page, err = s.RestoredCatalog(context.Background(), result.ID, CatalogQuery{SourceID: "source-a"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Path != "folder" {
		t.Fatalf("partial scope %+v %v", page, err)
	}
	if _, err = s.OpenRestoredFile(context.Background(), result.ID, "source-a", "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unselected download: %v", err)
	}
	if err = os.RemoveAll(result.Target); err != nil {
		t.Fatal(err)
	}
	library, err := s.RestoreLibrary(context.Background())
	if err != nil || len(library) != 1 || library[0].Available || library[0].UnavailableReason == "" {
		t.Fatalf("unavailable library %+v %v", library, err)
	}
}
func TestSelectedFileExtractionRejectsUnsafeEntriesAndPreservesScope(t *testing.T) {
	dir := t.TempDir()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	dir = canonical
	archive := filepath.Join(dir, "source.tar")
	writeCatalogFixture(t, archive, []catalogFixtureEntry{{name: "nested/file", content: "private recovered bytes"}, {name: "other", content: "keep archived"}, {name: "link", kind: tar.TypeSymlink, link: "other"}})
	target := filepath.Join(dir, "files")
	if err = ExtractSelectedFiles(context.Background(), archive, target, []string{"nested/file"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "nested/file"))
	if err != nil || string(got) != "private recovered bytes" {
		t.Fatalf("data %q %v", got, err)
	}
	if _, err = os.Stat(filepath.Join(target, "other")); !os.IsNotExist(err) {
		t.Fatal("unselected file extracted")
	}
	for _, selection := range [][]string{{"link"}, {"missing"}, {"../escape"}, {"nested/file", "nested/file"}} {
		if err = ExtractSelectedFiles(context.Background(), archive, filepath.Join(dir, "rejected"), selection); err == nil {
			t.Fatalf("accepted selection %v", selection)
		}
	}
	malicious := filepath.Join(dir, "unsafe.tar")
	writeCatalogFixture(t, malicious, []catalogFixtureEntry{{name: "../escape", content: "bad"}})
	if err = ExtractSelectedFiles(context.Background(), malicious, filepath.Join(dir, "unsafe"), []string{"anything"}); err == nil {
		t.Fatal("unsafe archive accepted")
	}
}

type indexedNativeCapture struct {
	*fakeCapture
	content string
}

func (c *indexedNativeCapture) Prepare(ctx context.Context, p Plan, id, dir string) (Manifest, error) {
	manifest, err := (&nativeArchiveCapture{fakeCapture: c.fakeCapture}).Prepare(ctx, p, id, dir)
	if err != nil {
		return manifest, err
	}
	if c.content != "" {
		file, openErr := os.Create(filepath.Join(dir, manifest.Sources[0].SavedPath))
		if openErr != nil {
			return manifest, openErr
		}
		writer := tar.NewWriter(file)
		header := &tar.Header{Name: "data.txt", Typeflag: tar.TypeReg, Mode: 0640, Size: int64(len(c.content))}
		err = writer.WriteHeader(header)
		if err == nil {
			_, err = io.WriteString(writer, c.content)
		}
		err = errors.Join(err, writer.Close(), file.Close())
		if err != nil {
			return manifest, err
		}
	}
	_, err = writeSourceIndex(ctx, filepath.Join(dir, manifest.Sources[0].SavedPath), dir, &manifest.Sources[0], 1<<20)
	return manifest, err
}
func TestNativeEncryptedCatalogComparePartialRestoreDownload(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic unavailable")
	}
	s, capture, _ := newTestService(t)
	s.runner = nativeRunner{}
	indexed := &indexedNativeCapture{fakeCapture: capture}
	s.capture = indexed
	d := addDestination(t, s, "Catalog")
	p := addPlan(t, s, d)
	ctx := context.Background()
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_repository_init", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_repository_init", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	runBackup := func() string {
		job, run, startErr := s.StartRun(ctx, p.ID, RunRequest{}, "test")
		if startErr != nil {
			t.Fatal(startErr)
		}
		s.ExecuteRun(ctx, job, run.ID)
		view, _ := s.Overview(ctx)
		for _, candidate := range view.Runs {
			if candidate.ID == run.ID {
				if candidate.State != "succeeded" {
					t.Fatalf("backup: %+v", candidate)
				}
				return candidate.Copies[0].SnapshotID
			}
		}
		t.Fatal("missing run")
		return ""
	}
	first := runBackup()
	page, err := s.SnapshotCatalog(ctx, d.ID, first, CatalogQuery{SourceID: "source-a"})
	if err != nil || !page.IndexAvailable || len(page.Items) != 1 || page.Items[0].Path != "data.txt" {
		t.Fatalf("remote catalog %+v %v", page, err)
	}
	indexed.content = "a different version"
	second := runBackup()
	comparison, err := s.CompareSnapshotFiles(ctx, d.ID, second, first, CatalogQuery{SourceID: "source-a"})
	if err != nil || comparison.Modified != 1 || len(comparison.Items) != 1 || !comparison.Items[0].ContentChanged {
		t.Fatalf("compare %+v %v", comparison, err)
	}
	job, err = s.StartRepositoryJob(ctx, d.ID, "backup_restore", "test")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: second, Files: []FileSelection{{SourceID: "source-a", Path: "data.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.JobID != job.ID || len(result.SelectedFiles) != 1 {
		t.Fatalf("restore result %+v", result)
	}
	download, err := s.OpenRestoredFile(ctx, result.ID, "source-a", "data.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(download.Reader)
	_ = download.Reader.Close()
	if err != nil || !bytes.Equal(data, []byte(indexed.content)) {
		t.Fatalf("native download %q %v", data, err)
	}
	if _, err = os.Stat(filepath.Join(result.Target, "files", "source-a", "data.txt")); err != nil {
		t.Fatal(err)
	}
	// Index bytes are inside the encrypted repository, not visible plaintext in
	// the repository layout. The clear staging copy is expected and private.
	if err = filepath.WalkDir(d.Repository, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(filename, "source-a.json") {
			return errors.New("plaintext source index in repository")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type catalogResponseRunner struct{ responses map[string][]byte }

func (r catalogResponseRunner) Run(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
	for i, arg := range args {
		if arg == "dump" && i+2 < len(args) {
			value, ok := r.responses[args[i+1]+" "+args[i+2]]
			if !ok {
				return nil, errors.New("unknown catalog fixture")
			}
			return value, nil
		}
	}
	return nil, errors.New("unexpected catalog command")
}
func TestComparisonSeparatesContentAndMetadataAndLegacyFallback(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Comparison")
	source := ManifestSource{Source: Source{ID: "source-a"}, SavedPath: "source.tar", Archive: true, IndexPath: "indexes/source-a.json", IndexVersion: 1}
	manifest := Manifest{Version: 1, Sources: []ManifestSource{source}}
	manifestData, _ := json.Marshal(manifest)
	hash := sha256.Sum256([]byte("same"))
	digest := hex.EncodeToString(hash[:])
	before := FileIndex{Version: 1, SourceID: "source-a", Entries: []FileEntry{{Path: "permissions", Type: "file", SHA256: digest, Mode: 0600}, {Path: "removed", Type: "file", SHA256: digest}}}
	after := FileIndex{Version: 1, SourceID: "source-a", Entries: []FileEntry{{Path: "permissions", Type: "file", SHA256: digest, Mode: 0640}, {Path: "added", Type: "file", SHA256: digest}}}
	oldData, _ := json.Marshal(before)
	newData, _ := json.Marshal(after)
	responses := map[string][]byte{"aaaaaaaa /manifest.json": manifestData, "bbbbbbbb /manifest.json": manifestData, "aaaaaaaa /indexes/source-a.json": oldData, "bbbbbbbb /indexes/source-a.json": newData}
	s.runner = catalogResponseRunner{responses: responses}
	result, err := s.CompareSnapshotFiles(context.Background(), d.ID, "bbbbbbbb", "aaaaaaaa", CatalogQuery{SourceID: "source-a"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.Removed != 1 || result.Modified != 1 || len(result.Items) != 3 {
		t.Fatalf("comparison %+v", result)
	}
	change := result.Items[1]
	if change.Path != "permissions" || change.ContentChanged || !change.MetadataChanged {
		t.Fatalf("metadata-only change %+v", change)
	}
	manifest.Sources[0].IndexPath = ""
	manifest.Sources[0].IndexVersion = 0
	legacy, _ := json.Marshal(manifest)
	responses["cccccccc /manifest.json"] = legacy
	page, err := s.SnapshotCatalog(context.Background(), d.ID, "cccccccc", CatalogQuery{SourceID: "source-a"})
	if err != nil || page.IndexAvailable || len(page.Items) != 0 {
		t.Fatalf("legacy snapshot %+v %v", page, err)
	}
	result, err = s.CompareSnapshotFiles(context.Background(), d.ID, "bbbbbbbb", "cccccccc", CatalogQuery{SourceID: "source-a"})
	if err != nil || result.IndexAvailable {
		t.Fatalf("legacy comparison %+v %v", result, err)
	}
	before.Entries = []FileEntry{{Path: "deep/nested/data", Type: "file", SHA256: digest, Mode: 0600}}
	after.Entries = []FileEntry{{Path: "deep/nested/data", Type: "file", SHA256: digest, Mode: 0640}}
	responses["aaaaaaaa /indexes/source-a.json"], _ = json.Marshal(before)
	responses["bbbbbbbb /indexes/source-a.json"], _ = json.Marshal(after)
	result, err = s.CompareSnapshotFiles(context.Background(), d.ID, "bbbbbbbb", "aaaaaaaa", CatalogQuery{SourceID: "source-a"})
	if err != nil || len(result.Items) != 1 || result.Items[0].Path != "deep/nested/data" {
		t.Fatalf("nested source comparison %+v %v", result, err)
	}
}
func TestRecoveredArchiveRemovalAndSymlinkAreRejected(t *testing.T) {
	s, _, _ := newTestService(t)
	result := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "data", content: "original"}}, nil)
	outside := filepath.Join(t.TempDir(), "other.tar")
	writeCatalogFixture(t, outside, []catalogFixtureEntry{{name: "data", content: "outside"}})
	archive := filepath.Join(result.Target, "source.tar")
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.ResolveRestore(context.Background(), result.ID)
	if err != nil || resolved.Available {
		t.Fatalf("missing archive %+v %v", resolved, err)
	}
	if err = os.Symlink(outside, archive); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OpenRestoredFile(context.Background(), result.ID, "source-a", "data"); err == nil {
		t.Fatal("archive symlink followed")
	}
}
