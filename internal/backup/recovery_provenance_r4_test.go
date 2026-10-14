package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryProvenanceSurvivesInventoryRemovalAndChecksSelectedSource(t *testing.T) {
	s, _, _ := newTestService(t)
	s.capture = &fakeRecovery{}
	d := addDestination(t, s, "provenance")
	result := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "data", content: "complete source"}}, nil)
	result.DestinationID, result.SnapshotID = d.ID, strings.Repeat("a", 64)
	result.RecoveryStatus = ""
	s.rememberSnapshotClassificationLocked(d.ID, result.SnapshotID, "incomplete", s.now())
	if err := s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	delete(s.state.SnapshotClassifications, snapshotEvidenceKey(d.ID, result.SnapshotID))
	saved, err := s.ResolveRestore(context.Background(), result.ID)
	if err != nil || saved.RecoveryStatus != "incomplete" || saved.ClassifiedAt == nil {
		t.Fatalf("lost durable provenance: %+v %v", saved, err)
	}
	request := RestoreRequest{RestoreID: result.ID, Mappings: []RestoreMapping{{SourceID: "source-a", Target: filepath.Join(t.TempDir(), "live"), Mode: "replace"}}}
	review, err := s.PreviewRestore(context.Background(), d.ID, request)
	if err != nil || review.Preview.RecoveryStatus != "incomplete" || len(review.Preview.Warnings) == 0 {
		t.Fatalf("intact selected source rejected or provenance lost: %+v %v", review, err)
	}
	// A syntactically valid replacement archive is still incomplete relative to
	// the recorded source; tar validation alone cannot establish completeness.
	writeReviewTar(t, filepath.Join(result.Target, "source.tar"), strings.Repeat("changed", 2048))
	if _, err = s.PreviewRestore(context.Background(), d.ID, request); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "size does not match") {
		t.Fatalf("changed archive size accepted: %v", err)
	}
}

func TestLegacyRecoveryUnknownSizeRequiresNonReplacement(t *testing.T) {
	s, _, _ := newTestService(t)
	s.capture = &fakeRecovery{}
	d := addDestination(t, s, "legacy")
	result := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "data", content: "legacy"}}, nil)
	result.DestinationID = d.ID
	result.Manifest.Sources[0].SizeBytes = 0
	raw, _ := json.Marshal(result.Manifest)
	if err := os.WriteFile(filepath.Join(result.Target, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	request := RestoreRequest{RestoreID: result.ID, Mappings: []RestoreMapping{{SourceID: "source-a", Target: filepath.Join(t.TempDir(), "live"), Mode: "replace"}}}
	if _, err := s.PreviewRestore(context.Background(), d.ID, request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown source size permitted replacement: %v", err)
	}
	request.Mappings[0].Mode = "merge"
	review, err := s.PreviewRestore(context.Background(), d.ID, request)
	if err != nil || review.Preview.RecoveryStatus != "unknown" || len(review.Preview.Warnings) < 2 {
		t.Fatalf("legacy explicit merge not explained: %+v %v", review, err)
	}
}

type selectionIndexRunner struct {
	manifest Manifest
	index    FileIndex
}

func (r selectionIndexRunner) Run(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
	if args[len(args)-1] == "/manifest.json" {
		return json.Marshal(r.manifest)
	}
	return json.Marshal(r.index)
}

func TestSelectedHardlinkRejectedBeforeJobAdmission(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "indexed")
	archive := filepath.Join(t.TempDir(), "source.tar")
	writeCatalogFixture(t, archive, []catalogFixtureEntry{{name: "regular", content: "data"}, {name: "alias", kind: tar.TypeLink, link: "regular"}})
	index, err := indexArchive(context.Background(), archive, "source-a")
	if err != nil {
		t.Fatal(err)
	}
	s.runner = selectionIndexRunner{Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source-a"}, SavedPath: "source.tar", Archive: true, IndexPath: "indexes/source-a.json", IndexVersion: 1, EntryCount: len(index.Entries)}}}, index}
	request := RestoreRequest{SnapshotID: strings.Repeat("a", 64), Files: []FileSelection{{SourceID: "source-a", Path: "alias"}}}
	if err = s.ValidateRepositoryRequest(context.Background(), d.ID, "backup_restore", CheckRequest{}, request); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "whole source") {
		t.Fatalf("hardlink admission: %v", err)
	}
	request.Files[0].Path = "regular"
	if err = s.ValidateRepositoryRequest(context.Background(), d.ID, "backup_restore", CheckRequest{}, request); err != nil {
		t.Fatalf("regular file rejected: %v", err)
	}
	if len(s.repositoryJobs) != 0 || len(s.repositoryReads) != 0 {
		t.Fatal("selection validation leaked a repository reservation")
	}
}
