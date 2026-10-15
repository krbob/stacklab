package backup

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestOldMaintenanceRetryCannotSupersedeNewerTransferFailure(t *testing.T) {
	for _, outcome := range []string{"succeeded", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			s, _, runner := newTestService(t)
			now := time.Now().UTC()
			s.now = func() time.Time { return now }
			d := addDestination(t, s, "Chronology")
			p := addPlan(t, s, d)
			old := retentionFailureRun(t, s, runner, p)
			now = now.Add(time.Minute)
			runner.failRepo = d.Repository
			job, newer, err := s.StartRun(context.Background(), p.ID, RunRequest{}, "test")
			if err != nil {
				t.Fatal(err)
			}
			s.ExecuteRun(context.Background(), job, newer.ID)
			assertTransferFailure := func() {
				t.Helper()
				copy := protectionFor(t, s).Copies[0]
				if copy.LastRunID != newer.ID || copy.LastError != "simulated destination unavailable" {
					t.Fatalf("maintenance hid newer transfer failure: %+v", copy)
				}
			}
			assertTransferFailure()
			now = now.Add(time.Minute)
			runner.failRepo, runner.failRetention = "", outcome == "failed"
			job, _, err = s.StartRetry(context.Background(), old.ID, RunRequest{}, "test")
			if err != nil {
				t.Fatal(err)
			}
			assertTransferFailure()
			s.ExecuteRun(context.Background(), job, old.ID)
			assertTransferFailure()
			if got := s.state.Runs[old.ID].Copies[0].Retention; got != outcome {
				t.Fatalf("maintenance result=%s want=%s", got, outcome)
			}
			// Durable independent timelines survive removal of bounded history.
			delete(s.state.Runs, old.ID)
			delete(s.state.Runs, newer.ID)
			if err := s.persist(context.Background()); err != nil {
				t.Fatal(err)
			}
			s = NewService(s.cfg, s.jobs, s.capture)
			s.now = func() time.Time { return now }
			assertTransferFailure()
		})
	}
}

func TestCopyAttemptMigrationSeparatesLegacyMaintenanceChronology(t *testing.T) {
	for _, retention := range []string{"succeeded", "failed", "running"} {
		t.Run(retention, func(t *testing.T) {
			s, _, _ := newTestService(t)
			d := addDestination(t, s, "Legacy chronology")
			p := addPlan(t, s, d)
			now := s.now()
			oldTime, newerTime := now.Add(-2*time.Hour), now.Add(-time.Hour)
			old := completedTestRun(p, d, "old", oldTime, oldTime)
			old.CompletedAt, old.Copies[0].CompletedAt = &now, &oldTime
			old.Copies[0].Retention = retention
			old.Copies[0].RetentionCompletedAt = &now
			if retention != "succeeded" {
				old.Copies[0].RetentionError = "previous retention failure"
			}
			if retention == "running" {
				old.State, old.Copies[0].RetentionCompletedAt = "running", nil
			}
			newer := completedTestRun(p, d, "newer", newerTime, newerTime)
			newer.State, newer.Copies[0].State = "failed", "failed"
			newer.Copies[0].Error = "newer transfer failed"
			newer.Copies[0].CompletedAt = &newerTime
			newer.Copies[0].SnapshotID, newer.Copies[0].Verification = "", "unavailable"
			if err := s.saveRun(old, p); err != nil {
				t.Fatal(err)
			}
			if err := s.saveRun(newer, p); err != nil {
				t.Fatal(err)
			}
			key := checkpointKey(p.ID, d.ID)
			checkpoint := clone(s.state.CopyCheckpoints[key])
			s.state.CopyAttemptsVersion = 0
			s.state.MaintenanceAttempts = nil
			s.state.CopyAttempts[key] = copyAttempt{RunID: old.ID, CompletedAt: now, Error: old.Copies[0].RetentionError}
			if err := s.persist(context.Background()); err != nil {
				t.Fatal(err)
			}
			s = NewService(s.cfg, s.jobs, s.capture)
			copy := protectionFor(t, s).Copies[0]
			if copy.LastRunID != newer.ID || copy.LastError != newer.Copies[0].Error || s.state.CopyAttemptsVersion != 1 {
				t.Fatalf("legacy maintenance timestamp still dominates: %+v", copy)
			}
			maintenance := s.state.MaintenanceAttempts[key]
			if maintenance.RunID != old.ID || maintenance.Error != old.Copies[0].RetentionError || !reflect.DeepEqual(s.state.CopyCheckpoints[key], checkpoint) {
				t.Fatalf("migration lost maintenance diagnostic or rewrote historical proof: %+v", maintenance)
			}
		})
	}
}

func TestCopyAttemptMigrationTreatsOrphanedResultsConservatively(t *testing.T) {
	for _, orphanError := range []string{"", "unresolved historical failure"} {
		t.Run(orphanError, func(t *testing.T) {
			s, _, _ := newTestService(t)
			d := addDestination(t, s, "Pruned history")
			p := addPlan(t, s, d)
			now := s.now()
			previous := now.Add(-time.Hour)
			run := completedTestRun(p, d, "retained", previous, previous)
			run.State, run.Copies[0].State, run.Copies[0].Error = "failed", "failed", "retained transfer failure"
			if err := s.saveRun(run, p); err != nil {
				t.Fatal(err)
			}
			key := checkpointKey(p.ID, d.ID)
			s.state.CopyAttemptsVersion = 0
			s.state.CopyAttempts[key] = copyAttempt{RunID: "pruned", CompletedAt: now, Error: orphanError}
			if err := s.persist(context.Background()); err != nil {
				t.Fatal(err)
			}
			s = NewService(s.cfg, s.jobs, s.capture)
			want := orphanError
			if want == "" {
				want = "retained transfer failure"
			}
			if got := protectionFor(t, s).Copies[0].LastError; got != want {
				t.Fatalf("orphaned summary hid a failure: got=%q want=%q", got, want)
			}
		})
	}
}

func TestRepositoryCheckHoldWithholdsUsableProtectionWithoutRewritingProof(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "Unresolved check")
	p := addPlan(t, s, d)
	now := s.now()
	run := completedTestRun(p, d, "verified", now, now)
	if err := s.saveRun(run, p); err != nil {
		t.Fatal(err)
	}
	key := checkpointKey(p.ID, d.ID)
	checkpoint := clone(s.state.CopyCheckpoints[key])
	d.CheckStatus = "verified"
	d.DataCheck = CheckEvidence{Status: "verified", Scope: "all", CheckedAt: &now}
	d.CheckHold = &CheckHold{Scope: "metadata", CheckedAt: &now, Error: "repository contains errors"}
	s.state.Destinations[d.ID] = d
	copy := protectionFor(t, s).Copies[0]
	if copy.State != "unverified" || copy.IntegrityStatus != "unverified" || copy.EvidenceUsable || copy.LastError == "" || protectionFor(t, s).CurrentCopies != 0 {
		t.Fatalf("unresolved check retained usable evidence: %+v", copy)
	}
	if !reflect.DeepEqual(s.state.CopyCheckpoints[key], checkpoint) || !reflect.DeepEqual(s.state.Runs[run.ID].Copies, run.Copies) || !reflect.DeepEqual(s.state.Destinations[d.ID], d) {
		t.Fatal("health projection rewrote historical transfer/check proof")
	}
	d.CheckHold = nil
	s.state.Destinations[d.ID] = d
	if copy := protectionFor(t, s).Copies[0]; !copy.EvidenceUsable || copy.State != "manual" {
		t.Fatalf("resolved check did not release original evidence: %+v", copy)
	}
}
