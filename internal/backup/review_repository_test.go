package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type resolvedVolumeRecovery struct {
	fakeRecovery
	target string
}

func (f *resolvedVolumeRecovery) PreviewRestore(ctx context.Context, manifest Manifest, mappings []RestoreMapping) (RestorePreview, error) {
	preview, err := f.fakeRecovery.PreviewRestore(ctx, manifest, mappings)
	preview.Mappings = append([]RestoreMapping(nil), preview.Mappings...)
	for index := range preview.Mappings {
		if preview.Mappings[index].VolumeName != "" {
			preview.Mappings[index].Target = f.target
		}
	}
	return preview, err
}

func repositoryReviewFixture(t *testing.T, s *Service, destination Destination) RestoreRequest {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeReviewTar(t, filepath.Join(path, "source.tar"), "original")
	manifest := Manifest{Version: 1, Sources: []ManifestSource{{Source: Source{ID: "source", Kind: "named_volume"}, SavedPath: "source.tar", Archive: true}}}
	raw, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(path, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	result := RestoreResult{ID: "restore_repository_guard", DestinationID: destination.ID, SnapshotID: "aabbccdd", Target: path, Manifest: &manifest}
	if err = s.registerRestoreResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	return RestoreRequest{RestoreID: result.ID, Mappings: []RestoreMapping{{SourceID: "source", VolumeName: "resolved-volume"}}}
}

func TestResolvedRecoveryTargetsExcludeEveryLocalRepository(t *testing.T) {
	for _, relation := range []string{"same", "parent", "child", "disjoint"} {
		t.Run(relation, func(t *testing.T) {
			s, _, _ := newTestService(t)
			d := addDestination(t, s, "Recovery source")
			other := addDestination(t, s, "Separate local repository")
			target := other.Repository
			switch relation {
			case "parent":
				target = filepath.Dir(other.Repository)
			case "child":
				target = filepath.Join(other.Repository, "nested")
			case "disjoint":
				target = other.Repository + "-other"
			}
			capture := &resolvedVolumeRecovery{target: target}
			s.capture = capture
			request := repositoryReviewFixture(t, s, d)
			review, err := s.PreviewRestore(context.Background(), d.ID, request)
			if relation != "disjoint" {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("%s repository overlap accepted: %v", relation, err)
				}
				if capture.applied != 0 {
					t.Fatal("rejected mapping reached apply")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			request.ReviewToken = review.ReviewToken
			if _, err = s.applyReviewedRestore(context.Background(), d, request); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecoveryRechecksRepositoryAddedAfterReview(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Recovery source")
	target, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	capture := &resolvedVolumeRecovery{target: target}
	s.capture = capture
	request := repositoryReviewFixture(t, s, d)
	review, err := s.PreviewRestore(context.Background(), d.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	device, err := FilesystemIdentity(target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveDestination(context.Background(), "", DestinationInput{Name: "New protected repository", Kind: "local", Repository: filepath.Join(target, "repository"), ExpectedFilesystem: device, Secrets: &Secrets{Password: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	request.ReviewToken = review.ReviewToken
	if _, err = s.applyReviewedRestore(context.Background(), d, request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("repository added after review was overwritten: %v", err)
	}
	if capture.applied != 0 {
		t.Fatal("unsafe apply reached capture")
	}
}
