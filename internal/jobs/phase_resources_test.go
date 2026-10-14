package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestRepositoryResourcesAndAtomicPhaseTransition(t *testing.T) {
	s := NewService(openJobsTestStore(t))
	ctx := context.Background()
	repo := BackupRepositoryResource("repo-a")
	upload, err := s.StartWithResources(ctx, "", "backup", "test", GlobalResource(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartWithResources(ctx, "other", "restart", "test", StackResource("other")); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("capture not exclusive: %v", err)
	}
	if err = s.ReplaceResources(upload.ID, repo); err != nil {
		t.Fatal(err)
	}
	mutation, err := s.StartWithResources(ctx, "other", "restart", "test", StackResource("other"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceResources(upload.ID, GlobalResource(), repo); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("apply ignored host mutation: %v", err)
	}
	if _, err = s.StartWithResources(ctx, "", "check", "test", repo); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("failed transition lost repository lock")
	}
	other, err := s.StartWithResources(ctx, "", "check", "test", BackupRepositoryResource("repo-b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartDraining(ctx, "self_update_stacklab", "test", SelfUpdateResource()); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("self update ignored active transfers")
	}
	_, _ = s.FinishSucceeded(ctx, other)
	_, _ = s.FinishSucceeded(ctx, mutation)
	if err = s.ReplaceResources(upload.ID, GlobalResource(), repo); err != nil {
		t.Fatal(err)
	}
	_, _ = s.FinishSucceeded(ctx, upload)
	if err = s.ReplaceResources(upload.ID, repo); err == nil {
		t.Fatal("terminal job reacquired resources")
	}
}
