package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateBackupMaintainsParentAndCaptureExclusion(t *testing.T) {
	ctx := context.Background()
	s := NewService(openJobsTestStore(t))
	a, b, shared := StackResource("a"), StackResource("b"), StackResource("shared-writer")
	repo := BackupRepositoryResource("repo")
	parent, err := s.StartWithResources(ctx, "", "update_stacks", "operator", a, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartBackupForUpdate(ctx, parent.ID, "unknown", repo); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("unleased stack: %v", err)
	}
	if _, err = s.StartBackupForUpdate(ctx, parent.ID, "a", DockerDaemonResource()); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("invalid child lease: %v", err)
	}
	for _, resource := range []Resource{shared, repo} {
		other, err := s.StartWithResources(ctx, "", "other", "operator", resource)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.StartBackupForUpdate(ctx, parent.ID, "a", repo); !errors.Is(err, ErrResourceConflict) {
			t.Fatalf("bypassed %s: %v", resource, err)
		}
		if _, err = s.FinishSucceeded(ctx, other); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := s.StartBackupForUpdate(ctx, parent.ID, "a", repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartWithResources(ctx, "", "restart", "other", shared); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("unprotected shared writer: %v", err)
	}
	if err = s.ReplaceResources(backup.ID, repo); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Resource{a, b, repo} {
		if _, err = s.StartWithResources(ctx, "", "other", "test", r); !errors.Is(err, ErrResourceConflict) {
			t.Fatalf("lost lease %s on transfer: %v", r, err)
		}
	}
	if _, err = s.StartWithResources(ctx, "", "restart", "other", shared); err != nil {
		t.Fatalf("shared writer blocked after capture: %v", err)
	}
	if _, err = s.FinishSucceeded(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartWithResources(ctx, "", "restart", "operator", a); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("finishing child released parent's lease: %v", err)
	}
	if _, err = s.FinishSucceeded(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartBackupForUpdate(ctx, parent.ID, "a", repo); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("finished parent: %v", err)
	}
}
