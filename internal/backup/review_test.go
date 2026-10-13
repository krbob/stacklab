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
	s.state.Restores[restored] = RestoreResult{DestinationID: d.ID, Target: restored, Manifest: &manifest}
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

func TestRecoveryIDsPreserveReviewAndRejectPartialLegacyBypass(t *testing.T) {
	s, _, _ := newTestService(t)
	capture := &fakeRecovery{}
	s.capture = capture
	destination := addDestination(t, s, "registered")
	target, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeReviewTar(t, filepath.Join(target, "source.tar"), "registered recovery")
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "managed_config"}, SavedPath: "source.tar", Archive: true}}}
	data, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(target, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	result := RestoreResult{ID: "restore_test", DestinationID: destination.ID, SnapshotID: "abc123", Target: target, Manifest: &manifest}
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	request := RestoreRequest{RestoreID: result.ID, Mappings: []RestoreMapping{{SourceID: "source", Target: filepath.Join(t.TempDir(), "live")}}}
	review, err := s.PreviewRestore(context.Background(), destination.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ReviewToken = review.ReviewToken
	if _, err = s.applyReviewedRestore(context.Background(), destination, request); err != nil {
		t.Fatal(err)
	}
	if capture.applied != 1 {
		t.Fatal("ID-based reviewed apply did not reach capture")
	}
	for _, tc := range []RestoreRequest{
		{RestoreID: result.ID, SnapshotID: "different", Mappings: request.Mappings},
		{RestoreID: result.ID, RestoredDirectory: filepath.Join(target, "elsewhere"), Mappings: request.Mappings},
		{RestoreID: result.ID, Test: true, Mappings: request.Mappings},
		{RestoreID: result.ID, Target: filepath.Join(target, "ignored-target"), Mappings: request.Mappings},
		{RestoreID: result.ID, Includes: []string{"/source.tar"}, Mappings: request.Mappings},
	} {
		if _, err = s.PreviewRestore(context.Background(), destination.ID, tc); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mismatch accepted %v", err)
		}
	}
	if _, err = s.PreviewRestore(context.Background(), "other_destination", request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other destination accepted %v", err)
	}
	result.SelectedFiles = []FileSelection{{SourceID: "source", Path: "data"}}
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []RestoreRequest{
		{RestoreID: result.ID, Mappings: request.Mappings},
		{RestoredDirectory: target, Mappings: request.Mappings},
	} {
		if _, err = s.PreviewRestore(context.Background(), destination.ID, tc); !errors.Is(err, ErrInvalid) {
			t.Fatalf("partial recovery accepted %v", err)
		}
	}
}
