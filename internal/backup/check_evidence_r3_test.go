package backup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type checkEvidenceRunner struct {
	base CommandRunner
	err  error
}

func (r *checkEvidenceRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if commandIn(args, "check") {
		return []byte(`{}`), r.err
	}
	return r.base.Run(ctx, name, args, env, dir)
}

func runEvidenceCheck(t *testing.T, s *Service, d Destination, policy string, wantErr bool) Destination {
	t.Helper()
	job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_check", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_check", CheckRequest{ReadData: policy}, RestoreRequest{})
	if (err != nil) != wantErr {
		t.Fatalf("check %s error=%v, wantErr=%v", policy, err, wantErr)
	}
	return s.state.Destinations[d.ID]
}

func TestIntegrityFailureSurvivesLightChecksRestartAndTransportFailure(t *testing.T) {
	s, _, base := newTestService(t)
	d := addDestination(t, s, "Integrity")
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	runner := &checkEvidenceRunner{base: base, err: &repositoryCommandError{Code: "integrity_failed", message: "corrupt pack"}}
	s.runner = runner
	d = runEvidenceCheck(t, s, d, "all", true)
	failedAt := *d.DataCheck.CheckedAt
	if d.DataCheck.Status != "failed" || d.CheckStatus != "failed" {
		t.Fatalf("missing data failure: %+v", d)
	}
	runner.err = nil
	for _, scope := range []string{"metadata", "subset"} {
		now = now.Add(time.Minute)
		d = runEvidenceCheck(t, s, d, scope, false)
		if d.MetadataCheck.Status != "verified" || d.DataCheck.Status != "failed" || d.CheckStatus != "failed" || d.CheckError != "corrupt pack" || !d.DataCheck.CheckedAt.Equal(failedAt) || d.LastCheckAttemptStatus != "succeeded" || d.LastCheckAttemptPolicy != scope {
			t.Fatalf("%s erased failure or hid attempt: %+v", scope, d)
		}
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	fresh.runner, fresh.now = runner, s.now
	if d = fresh.state.Destinations[d.ID]; d.DataCheck.Status != "failed" || d.CheckError != "corrupt pack" {
		t.Fatalf("restart lost integrity evidence: %+v", d)
	}
	for _, failure := range []error{commandContextError(context.DeadlineExceeded), commandContextError(context.Canceled), &repositoryCommandError{Code: "unavailable", message: "offline"}, errors.New("local execution failed")} {
		runner.err = failure
		now = now.Add(time.Minute)
		d = runEvidenceCheck(t, fresh, d, "all", true)
		if d.DataCheck.Status != "failed" || d.CheckError != "corrupt pack" || !d.DataCheck.CheckedAt.Equal(failedAt) || d.LastCheckAttemptError != failure.Error() {
			t.Fatalf("failed attempt changed proof: %+v", d)
		}
	}
	runner.err = nil
	d = runEvidenceCheck(t, fresh, d, "all", false)
	if d.DataCheck.Status != "verified" || d.MetadataCheck.Status != "verified" || d.CheckStatus != "verified" || d.CheckError != "" {
		t.Fatalf("full check did not resolve corruption: %+v", d)
	}
	fullAt := *d.DataCheck.CheckedAt
	now = now.Add(time.Hour)
	d = runEvidenceCheck(t, fresh, d, "subset", false)
	if d.DataCheck.Status != "verified" || d.DataCheck.Scope != "all" || !d.DataCheck.CheckedAt.Equal(fullAt) || d.LastCheckAttemptPolicy != "subset" {
		t.Fatalf("sample replaced stronger full-check proof: %+v", d)
	}
	runner.err = commandContextError(context.DeadlineExceeded)
	d = runEvidenceCheck(t, fresh, d, "metadata", true)
	if d.CheckStatus != "verified" || d.DataCheck.Status != "verified" || d.LastCheckAttemptStatus != "failed" {
		t.Fatalf("transport failure invented corruption: %+v", d)
	}
}

func TestLegacyFailedCheckMigrationRequiresFullCheck(t *testing.T) {
	s, _, base := newTestService(t)
	d := addDestination(t, s, "Legacy evidence")
	d.MetadataCheck, d.DataCheck = CheckEvidence{}, CheckEvidence{}
	d.CheckStatus, d.CheckError, d.LastCheckPolicy = "failed", "old failure with unknown scope", "metadata"
	s.state.Destinations[d.ID] = d
	if err := s.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh := NewService(s.cfg, s.jobs, s.capture)
	fresh.runner = base
	d = fresh.state.Destinations[d.ID]
	if d.DataCheck.Status != "failed" || d.DataCheck.Scope != "unknown" {
		t.Fatalf("unsafe legacy migration: %+v", d)
	}
	for _, scope := range []string{"metadata", "subset"} {
		d = runEvidenceCheck(t, fresh, d, scope, false)
		if d.CheckStatus != "failed" {
			t.Fatalf("%s cleared unknown-scope legacy failure", scope)
		}
	}
	d = runEvidenceCheck(t, fresh, d, "all", false)
	if d.CheckStatus != "verified" {
		t.Fatal("full data check did not clear migrated failure")
	}
}

func TestIntegrityFailureBlocksAutomaticDestruction(t *testing.T) {
	s, _, base := newTestService(t)
	d := addDestination(t, s, "Preserve")
	p := addPlan(t, s, d)
	d.CheckStatus, d.CheckError = "failed", "corrupt pack"
	s.state.Destinations[d.ID] = d
	if err := s.retain(context.Background(), d, p.ID, Retention{Daily: 1}); err == nil || len(base.calls) != 0 {
		t.Fatalf("retention ran with known corruption: %v calls=%v", err, base.calls)
	}
	if _, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_prune", "scheduler"); !errors.Is(err, ErrBusy) {
		t.Fatalf("scheduled prune admitted: %v", err)
	}
	job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_prune", "operator")
	if err != nil {
		t.Fatal("explicit operator prune unexpectedly blocked", err)
	}
	s.finish(context.Background(), job, nil)
	s.releaseRepositories(job.ID)
}
