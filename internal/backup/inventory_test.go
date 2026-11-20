package backup

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRepositorySetupRefreshesInventoryWithoutInventingIntegrityProof(t *testing.T) {
	for _, action := range []string{"backup_repository_init", "backup_destination_test"} {
		for _, listingFails := range []bool{false, true} {
			name := action + "/healthy inventory"
			if listingFails {
				name = action + "/unavailable inventory"
			}
			t.Run(name, func(t *testing.T) {
				s, _, runner := newTestService(t)
				d := addDestination(t, s, "Setup")
				old := time.Now().UTC().Add(-time.Hour)
				now := old.Add(time.Hour)
				s.now = func() time.Time { return now }
				d.LastInventoryAt, d.LastInventoryAttemptAt = &old, &old
				d.InventoryError = "Previous attempt: repository missing"
				s.state.Destinations[d.ID] = d
				if listingFails {
					s.runner = failOperationRunner{wrapped: runner, command: "snapshots"}
				}
				job, err := s.StartRepositoryJob(context.Background(), d.ID, action, "tester")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.ExecuteRepositoryJob(context.Background(), job, d.ID, action, CheckRequest{}, RestoreRequest{}); err != nil {
					t.Fatalf("successful setup was poisoned by inventory: %v", err)
				}
				// Read persisted state, not just the transient result of the job.
				fresh := NewService(s.cfg, s.jobs, s.capture)
				if fresh.initErr != nil {
					t.Fatal(fresh.initErr)
				}
				got := fresh.state.Destinations[d.ID]
				if got.Status != "available" || got.LastOperationStatus != "succeeded" || got.Error != "" {
					t.Fatalf("setup availability/result: %+v", got)
				}
				if got.LastInventoryAttemptAt == nil || !got.LastInventoryAttemptAt.Equal(now) {
					t.Fatalf("setup did not attempt a new inventory: %+v", got)
				}
				wantInventoryAt := now
				if listingFails {
					wantInventoryAt = old
					if !strings.Contains(got.InventoryError, "simulated snapshots failure") {
						t.Fatalf("new inventory failure was lost: %+v", got)
					}
				} else if got.InventoryError != "" {
					t.Fatalf("successful inventory kept stale error: %+v", got)
				}
				if got.LastInventoryAt == nil || !got.LastInventoryAt.Equal(wantInventoryAt) {
					t.Fatalf("last confirmed inventory changed incorrectly: %+v", got)
				}
				if got.LastCheckAt != nil || got.MetadataCheck.Status != "unknown" || got.DataCheck.Status != "unknown" {
					t.Fatalf("setup invented integrity evidence: %+v", got)
				}
			})
		}
	}
}

func TestFailedRepositorySetupKeepsInventoryEvidence(t *testing.T) {
	for action, command := range map[string]string{"backup_repository_init": "init", "backup_destination_test": "cat"} {
		t.Run(action, func(t *testing.T) {
			s, _, runner := newTestService(t)
			d := addDestination(t, s, "Unavailable")
			old := time.Now().UTC().Add(-time.Hour)
			d.LastInventoryAt, d.LastInventoryAttemptAt = &old, &old
			d.InventoryError = "Previous inventory unavailable"
			s.state.Destinations[d.ID] = d
			s.runner = failOperationRunner{wrapped: runner, command: command}
			job, err := s.StartRepositoryJob(context.Background(), d.ID, action, "tester")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ExecuteRepositoryJob(context.Background(), job, d.ID, action, CheckRequest{}, RestoreRequest{}); err == nil {
				t.Fatal("expected setup failure")
			}
			got := s.state.Destinations[d.ID]
			if got.LastInventoryAt == nil || !got.LastInventoryAt.Equal(old) || got.LastInventoryAttemptAt == nil || !got.LastInventoryAttemptAt.Equal(old) || got.InventoryError != d.InventoryError || runner.count("snapshots", "") != 0 {
				t.Fatalf("failed setup changed inventory evidence: %+v", got)
			}
		})
	}
}

func TestNativeInitializationRefreshesMissingInventory(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic is not installed")
	}
	s, _, _ := newTestService(t)
	s.runner = nativeRunner{}
	d := addDestination(t, s, "New repository")
	ctx := context.Background()
	var commandErr *repositoryCommandError
	if err := s.refreshInventory(ctx, d); !errors.As(err, &commandErr) || commandErr.Code != "repository_missing" {
		t.Fatalf("expected missing repository before initialization, got %v", err)
	}
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_repository_init", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_repository_init", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	got := s.state.Destinations[d.ID]
	if got.Status != "available" || got.LastInventoryAt == nil || got.InventoryError != "" {
		t.Fatalf("initialized native repository still shows missing inventory: %+v", got)
	}
	if got.LastCheckAt != nil || got.MetadataCheck.Status != "unknown" || got.DataCheck.Status != "unknown" {
		t.Fatalf("native initialization invented integrity evidence: %+v", got)
	}
}
