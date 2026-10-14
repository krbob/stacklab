package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestOversizedCatalogPreservesArchiveAndExplainsUnavailableInspection(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archive := filepath.Join(dir, "source.tar")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(file)
	for i := 0; i <= maxCatalogEntries; i++ {
		if err = tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("f%06d", i), Mode: 0600, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	source := ManifestSource{Source: Source{ID: "source-a"}, SavedPath: "source.tar", Archive: true}
	size, err := writeSourceIndex(ctx, archive, dir, &source, 1<<30)
	if err != nil || size != 0 || source.IndexPath != "" || source.IndexUnavailableReason == "" {
		t.Fatalf("source=%+v size=%d err=%v", source, size, err)
	}
	info, err := os.Stat(archive)
	if err != nil || info.Size() == 0 {
		t.Fatal("archive lost", err)
	}
	if _, err = restoredIndex(ctx, dir, archive, source); !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("local fallback must explain bounds: %v", err)
	}
	page, err := catalogPage(Manifest{Sources: []ManifestSource{source}}, FileIndex{}, false, CatalogQuery{SourceID: source.Source.ID, Limit: 100})
	if err != nil || page.IndexAvailable || page.IndexUnavailableReason == "" || len(page.Items) != 0 {
		t.Fatalf("misleading catalog %+v %v", page, err)
	}
}
func TestCatalogStoresSpecialMetadataAndDownloadsOrdinaryMembers(t *testing.T) {
	s, _, _ := newTestService(t)
	result := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "ordinary", content: "saved"}, {name: "outside", kind: tar.TypeSymlink, link: "/mnt/library"}, {name: "pipe", kind: tar.TypeFifo}, {name: "privileged", content: "binary", mode: 04755}}, nil)
	page, err := s.RestoredCatalog(context.Background(), result.ID, CatalogQuery{SourceID: "source-a"})
	if err != nil || !page.IndexAvailable || len(page.Items) != 4 {
		t.Fatalf("catalog=%+v err=%v", page, err)
	}
	data, err := s.OpenRestoredFile(context.Background(), result.ID, "source-a", "ordinary")
	if err != nil {
		t.Fatal(err)
	}
	data.Reader.Close()
	archive, err := s.OpenRestoredArchive(context.Background(), result.ID, "source-a")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Reader.Close()
	if archive.SizeBytes <= 0 || archive.Name != "source-a.tar" {
		t.Fatalf("archive %+v", archive)
	}
	encoded, _ := json.Marshal(FileIndex{Version: 1, SourceID: "source-a", Entries: page.Items})
	if _, err = decodeIndex(encoded, ManifestSource{Source: Source{ID: "source-a"}}); err != nil {
		t.Fatal(err)
	}
}
func TestIndexDegradationDoesNotHideArchiveCorruption(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "source.tar")
	if err := os.WriteFile(archive, []byte("not a tar archive"), 0600); err != nil {
		t.Fatal(err)
	}
	source := ManifestSource{Source: Source{ID: "source-a"}}
	if _, err := writeSourceIndex(context.Background(), archive, dir, &source, 1<<20); err == nil || errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("corrupt archive accepted: %v", err)
	}
}
