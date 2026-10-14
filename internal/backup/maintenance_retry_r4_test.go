package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func retentionFailureRun(t *testing.T, s *Service, runner *recordingRunner, plan Plan) Run {
	t.Helper()
	runner.failRetention = true
	job, run, err := s.StartRun(context.Background(), plan.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	return clone(s.state.Runs[run.ID].Run)
}

func transferEvidence(copy CopyResult) CopyResult {
	copy.Retention, copy.RetentionError = "", ""
	copy.RetentionStartedAt, copy.RetentionCompletedAt = nil, nil
	return copy
}

func TestMaintenanceRetryNeedsNoStagingAndPreservesTransferEvidence(t *testing.T) {
	for _, outcome := range []string{"succeeded", "failed", "cancelled", "paused"} {
		t.Run(outcome, func(t *testing.T) {
			s, capture, runner := newTestService(t)
			d := addDestination(t, s, "Maintenance retry")
			p := addPlan(t, s, d)
			run := retentionFailureRun(t, s, runner, p)
			original := run.Copies[0]
			stage := filepath.Join(s.directory, "staging", run.ID)
			if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("verified copies retained unnecessary staging: %v", err)
			}
			listings, dumps := runner.count("ls", d.Repository), runner.count("dump", d.Repository)
			future := run.ExpiresAt.Add(time.Hour)
			s.now = func() time.Time { return future }
			stored := s.state.Runs[run.ID]
			stored.Manifest = nil // maintenance does not read the capture manifest
			s.state.Runs[run.ID] = stored
			runner.failRetention = outcome == "failed"
			s.policy.Paused = outcome == "paused"
			job, queued, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
			if err != nil {
				t.Fatal(err)
			}
			if queued.Phase != "retention" || queued.Copies[0].Retention != "pending" || !reflect.DeepEqual(transferEvidence(original), transferEvidence(queued.Copies[0])) {
				t.Fatalf("maintenance admission rewrote transfer: %+v", queued)
			}
			protection := protectionFor(t, s).Copies[0]
			if protection.Activity != "queued" || protection.LastError != original.RetentionError {
				t.Fatalf("pending maintenance erased previous diagnostic: %+v", protection)
			}
			ctx := context.Background()
			if outcome == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			s.ExecuteRun(ctx, job, run.ID)
			done := s.state.Runs[run.ID]
			copy := done.Copies[0]
			if !reflect.DeepEqual(transferEvidence(original), transferEvidence(copy)) {
				t.Fatalf("maintenance changed successful transfer evidence: before=%+v after=%+v", original, copy)
			}
			if outcome == "succeeded" {
				if done.State != "succeeded" || copy.Retention != "succeeded" || copy.RetentionError != "" || protectionFor(t, s).Copies[0].LastError != "" {
					t.Fatalf("successful retention did not resolve its error: %+v", done)
				}
			} else if done.State != "failed" || copy.Retention != "failed" || copy.RetentionError == "" {
				t.Fatalf("maintenance failure lost: %+v", done)
			}
			if outcome != "paused" && (copy.RetentionStartedAt == nil || copy.RetentionCompletedAt == nil) {
				t.Fatal("maintenance timestamps absent")
			}
			if runner.count("ls", d.Repository) != listings || runner.count("dump", d.Repository) != dumps || runner.count("backup", d.Repository) != 1 || capture.calls != 1 {
				t.Fatal("maintenance retry performed capture, upload, or copy verification")
			}
		})
	}
}

func TestMaintenanceRetryReservesRepositoryAndSurvivesInterruption(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Interrupted maintenance")
	p := addPlan(t, s, d)
	run := retentionFailureRun(t, s, runner, p)
	_, queued, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_check", "test"); !errors.Is(err, ErrBusy) {
		t.Fatalf("maintenance did not reserve repository: %v", err)
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	fresh.runner = runner
	if err := fresh.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	copy := fresh.state.Runs[run.ID].Copies[0]
	if !reflect.DeepEqual(transferEvidence(queued.Copies[0]), transferEvidence(copy)) || copy.Retention != "failed" || !strings.Contains(copy.RetentionError, "interrupted") || copy.RetentionCompletedAt == nil {
		t.Fatalf("restart lost transfer evidence or maintenance failure: %+v", copy)
	}
}

func TestMaintenanceRetryUsesDurableVerificationAfterClassificationEviction(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Evicted classification")
	p := addPlan(t, s, d)
	run := retentionFailureRun(t, s, runner, p)
	copy := run.Copies[0]
	s.state.SnapshotClassifications = nil
	retention := &classifiedRetentionRunner{
		snapshots: []Snapshot{{ID: copy.SnapshotID, Time: *run.CapturedAt}},
		failID:    copy.SnapshotID,
	}
	s.runner = retention
	job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if result := s.state.Runs[run.ID]; result.State != "succeeded" || len(retention.listings) != 0 || len(retention.dumps) != 0 {
		t.Fatalf("cache eviction discarded durable verification: %+v listings=%v", result, retention.listings)
	}
}

func TestMaintenanceSubsetKeepsStagingForOtherFailedTransfers(t *testing.T) {
	s, _, runner := newTestService(t)
	a, b := addDestination(t, s, "Retention"), addDestination(t, s, "Upload")
	p := addPlan(t, s, a, b)
	runner.failRepo = b.Repository
	run := retentionFailureRun(t, s, runner, p)
	stage := filepath.Join(s.directory, "staging", run.ID)
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("failed transfer lost staging: %v", err)
	}
	runner.failRetention = false
	job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{DestinationIDs: []string{a.ID}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("maintenance subset deleted staging needed by unselected transfer: %v", err)
	}
	runner.failRepo = ""
	job, _, err = s.StartRetry(context.Background(), run.ID, RunRequest{DestinationIDs: []string{b.ID}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	if done := s.state.Runs[run.ID]; done.State != "succeeded" || runner.count("backup", a.Repository) != 1 || runner.count("backup", b.Repository) != 2 {
		t.Fatalf("mixed retry failed: %+v", done)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("finished transfers retained staging: %v", err)
	}
}

func TestExpiredMixedRetryAllowsOnlyMaintenanceSubset(t *testing.T) {
	s, _, runner := newTestService(t)
	a, b := addDestination(t, s, "Retention"), addDestination(t, s, "Upload")
	p := addPlan(t, s, a, b)
	runner.failRepo = b.Repository
	run := retentionFailureRun(t, s, runner, p)
	future := run.ExpiresAt.Add(time.Hour)
	s.now = func() time.Time { return future }
	if err := os.RemoveAll(filepath.Join(s.directory, "staging", run.ID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{}, "test"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired transfer admitted: %v", err)
	}
	runner.failRetention = false
	job, _, err := s.StartRetry(context.Background(), run.ID, RunRequest{DestinationIDs: []string{a.ID}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.ExecuteRun(context.Background(), job, run.ID)
	copies := s.state.Runs[run.ID].Copies
	if copies[0].Retention != "succeeded" || copies[1].State != "failed" || runner.count("backup", b.Repository) != 1 {
		t.Fatalf("maintenance subset unexpectedly retried upload: %+v", copies)
	}
}
