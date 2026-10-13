package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeRecovery struct {
	fakeCapture
	applied int
	running bool
}

func (f *fakeRecovery) PreviewRestore(_ context.Context, _ Manifest, mappings []RestoreMapping) (RestorePreview, error) {
	return RestorePreview{Mappings: mappings, Writers: []Writer{{ContainerID: "container", Running: f.running}}}, nil
}
func (f *fakeRecovery) ApplyRestore(context.Context, Manifest, []RestoreMapping, string, string, bool) error {
	f.applied++
	return nil
}
func writeReviewTar(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(file)
	if err = archive.WriteHeader(&tar.Header{Name: "data", Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = archive.Write([]byte(content))
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestRecoveryReviewBindsArchiveWritersAndConsumesToken(t *testing.T) {
	s, _, _ := newTestService(t)
	capture := &fakeRecovery{running: true}
	s.capture = capture
	d := addDestination(t, s, "restore")
	restored := t.TempDir()
	resolved, err := filepath.EvalSymlinks(restored)
	if err != nil {
		t.Fatal(err)
	}
	restored = resolved
	archive := filepath.Join(restored, "source.tar")
	writeReviewTar(t, archive, "original")
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config"}, SavedPath: "source.tar", Archive: true}}}
	data, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(restored, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s.state.Restores[restored] = RestoreResult{Target: restored, Manifest: &manifest}
	request := RestoreRequest{RestoredDirectory: restored, Mappings: []RestoreMapping{{SourceID: "source", Target: filepath.Join(t.TempDir(), "output")}}}
	review, err := s.PreviewRestore(context.Background(), d.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ReviewToken = review.ReviewToken
	writeReviewTar(t, archive, "tampered")
	if _, err = s.applyReviewedRestore(context.Background(), d, request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered archive accepted: %v", err)
	}
	if capture.applied != 0 {
		t.Fatal("applied tampered archive")
	}
	review, err = s.PreviewRestore(context.Background(), d.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ReviewToken = review.ReviewToken
	capture.running = false
	if _, err = s.applyReviewedRestore(context.Background(), d, request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed writers accepted: %v", err)
	}
	review, err = s.PreviewRestore(context.Background(), d.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ReviewToken = review.ReviewToken
	if _, err = s.applyReviewedRestore(context.Background(), d, request); err != nil {
		t.Fatal(err)
	}
	if capture.applied != 1 {
		t.Fatal("valid recovery not applied")
	}
	if _, err = s.applyReviewedRestore(context.Background(), d, request); !errors.Is(err, ErrInvalid) {
		t.Fatal("review token was reusable")
	}
}
func TestManifestOnlyFromRegisteredRestoreAndNoSymlink(t *testing.T) {
	s, _, _ := newTestService(t)
	target := t.TempDir()
	if _, err := s.ReadRestoreManifest(context.Background(), target); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(elsewhere, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(target, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(target); err == nil {
		t.Fatal("manifest symlink accepted")
	}
}
