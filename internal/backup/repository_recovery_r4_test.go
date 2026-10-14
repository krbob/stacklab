package backup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRepositoryCheckDiagnosticsDistinguishDataFromTransport(t *testing.T) {
	pack := strings.Repeat("a", 64)
	for _, test := range []struct{ name, diagnostic, code string }{
		{"summary alone", "Fatal: repository contains errors", "operation_failed"},
		{"SFTP reset", "download error: read tcp: connection reset by peer\nFatal: repository contains errors", "unavailable"},
		{"SFTP EOF", "pack " + pack + " contains 1 errors: [partial download error: EOF]\nFatal: repository contains errors", "unavailable"},
		{"S3 HTTP", "Load(<data/abcdef>): unexpected status code: 503\nFatal: repository contains errors", "unavailable"},
		{"S3 reason", "Load(<data/abcdef>): ServiceUnavailable\nFatal: repository contains errors", "unavailable"},
		{"config transport", "unable to open config file: dial tcp: connection refused", "unavailable"},
		{"hash", "pack " + pack + " contains 1 errors: [unexpected pack id " + pack + "]\nFatal: repository contains errors", "integrity_failed"},
		{"mixed corruption and network", "download error: EOF\npack " + pack + ": ciphertext verification failed\nFatal: repository contains errors", "integrity_failed"},
		{"mixed checksum and network", "connection reset by peer\ninvalid data returned\nFatal: repository contains errors", "integrity_failed"},
		{"mixed corruption and local failure", "no space left on device\npack " + pack + ": ciphertext verification failed\nFatal: repository contains errors", "integrity_failed"},
		{"missing pack", "pack " + pack + ": does not exist\nFatal: repository contains errors", "integrity_failed"},
		{"truncated pack", "pack " + pack + ": unexpected file size: got 500, expected 1000", "integrity_failed"},
		{"partial listing", "List(data) failed: EOF\npack " + pack + ": does not exist\nFatal: repository contains errors", "unavailable"},
		{"denied listing", "List(data) failed: permission denied\npack " + pack + ": does not exist\nFatal: repository contains errors", "permission_denied"},
		{"SSH identity", "ssh host key does not match pinned key", "operation_failed"},
		{"credentials", "Fatal: wrong password", "credentials_rejected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := classifyCommandError("restic", errors.New("exit"), test.diagnostic+" https://user:SECRET@example.test")
			var commandErr *repositoryCommandError
			if !errors.As(err, &commandErr) || commandErr.Code != test.code || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "example.test") {
				t.Fatalf("classification=%v, want %s without diagnostic leakage", err, test.code)
			}
			at := time.Now().UTC()
			d := Destination{}
			recordCheckAttempt(&d, "all", at, nil)
			prior := d.DataCheck
			recordCheckAttempt(&d, "all", at.Add(time.Hour), err)
			if d.LastCheckAttemptStatus != "failed" || d.LastCheckAttemptError != err.Error() {
				t.Fatalf("missing failed attempt: %+v", d)
			}
			if test.code == "integrity_failed" {
				if d.DataCheck.Status != "failed" || d.CheckStatus != "failed" {
					t.Fatalf("corruption not retained: %+v", d)
				}
			} else if !reflect.DeepEqual(d.DataCheck, prior) {
				t.Fatalf("inconclusive attempt changed evidence: before=%+v after=%+v", prior, d.DataCheck)
			}
		})
	}
}

func TestKnownLegacyNonIntegrityFailureMigrationAndRoundThreeRepair(t *testing.T) {
	for _, code := range []string{"unavailable", "timeout", "cancelled", "repository_locked", "repository_missing", "credentials_rejected", "permission_denied", "storage_full"} {
		for _, alreadyMigrated := range []bool{false, true} {
			t.Run(code+"/migrated="+map[bool]string{false: "no", true: "yes"}[alreadyMigrated], func(t *testing.T) {
				s, _, runner := newTestService(t)
				d := addDestination(t, s, "Legacy transport")
				succeededAt, failedAt := time.Now().UTC().Add(-2*time.Hour), time.Now().UTC().Add(-time.Hour)
				d.CheckStatus, d.CheckError = "failed", "repository attempt failed ("+code+")"
				d.LastCheckAt, d.LastCheckPolicy, d.LastCheckAttemptAt = &succeededAt, "all", &failedAt
				d.MetadataCheck, d.DataCheck = CheckEvidence{}, CheckEvidence{}
				if alreadyMigrated {
					d.MetadataCheck = CheckEvidence{Status: "unknown", Scope: "unknown"}
					d.DataCheck = CheckEvidence{Status: "failed", Scope: "unknown", CheckedAt: &failedAt, Error: d.CheckError}
				}
				s.state.Destinations[d.ID] = d
				if err := s.persist(context.Background()); err != nil {
					t.Fatal(err)
				}
				fresh := NewService(s.cfg, s.jobs, s.capture)
				fresh.runner = runner
				repaired := fresh.state.Destinations[d.ID]
				if repaired.CheckStatus != "verified" || repaired.MetadataCheck.Status != "verified" || repaired.DataCheck.Status != "verified" || !repaired.DataCheck.CheckedAt.Equal(succeededAt) || repaired.LastCheckAttemptStatus != "failed" || repaired.LastCheckAttemptError != d.CheckError {
					t.Fatalf("known transient failure was kept as corruption: %+v", repaired)
				}
				normalized := clone(repaired)
				normalizeCheckEvidence(&normalized)
				if !reflect.DeepEqual(repaired, normalized) {
					t.Fatal("migration is not idempotent")
				}
				job, err := fresh.StartRepositoryJob(context.Background(), d.ID, "backup_prune", "scheduler")
				if err != nil {
					t.Fatalf("transient history still blocks scheduled maintenance: %v", err)
				}
				fresh.finish(context.Background(), job, nil)
				fresh.releaseRepositories(job.ID)
			})
		}
	}
}

func TestLegacyRepairDoesNotInventProofOrEraseRealFailureOrNewerAttempt(t *testing.T) {
	for _, message := range []string{"ambiguous old failure", "check failed (operation_failed)", "check failed (integrity_failed)"} {
		d := Destination{CheckStatus: "failed", CheckError: message}
		normalizeCheckEvidence(&d)
		if d.DataCheck.Status != "failed" || d.DataCheck.Scope != "unknown" || d.CheckError != message {
			t.Fatalf("ambiguous evidence erased: %+v", d)
		}
	}
	d := Destination{CheckStatus: "failed", CheckError: "failed (timeout)", LastCheckPolicy: "all"}
	normalizeCheckEvidence(&d)
	if d.CheckStatus != "unknown" || d.MetadataCheck.Status != "unknown" || d.DataCheck.Status != "unknown" {
		t.Fatalf("invented success without timestamp: %+v", d)
	}
	at := time.Now().UTC()
	d = Destination{CheckStatus: "failed", CheckError: "failed (unavailable)", LastCheckAt: &at, LastCheckPolicy: "metadata", LastCheckAttemptStatus: "succeeded", LastCheckAttemptPolicy: "metadata", DataCheck: CheckEvidence{Status: "failed", Scope: "unknown", Error: "failed (unavailable)"}, MetadataCheck: CheckEvidence{Status: "verified", Scope: "metadata", CheckedAt: &at}}
	normalizeCheckEvidence(&d)
	if d.CheckStatus != "verified" || d.DataCheck.Status != "unknown" || d.LastCheckAttemptStatus != "succeeded" {
		t.Fatalf("repair overwrote newer attempt or invented data proof: %+v", d)
	}
	d = Destination{CheckStatus: "failed", CheckError: "failed (timeout)", DataCheck: CheckEvidence{Status: "failed", Scope: "all", Error: "real corruption (integrity_failed)"}}
	normalizeCheckEvidence(&d)
	if d.DataCheck.Status != "failed" || d.DataCheck.Scope != "all" || d.CheckError != "real corruption (integrity_failed)" {
		t.Fatalf("real scoped failure erased: %+v", d)
	}
}

func TestPruneRequiresExplicitAcknowledgementAtAdmissionAndExecution(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Corrupt repository")
	d.DataCheck = CheckEvidence{Status: "failed", Scope: "all", Error: "corrupt pack (integrity_failed)"}
	normalizeCheckEvidence(&d)
	s.state.Destinations[d.ID] = d
	ctx := context.Background()
	for _, requestedBy := range []string{"operator", "scheduler"} {
		if _, err := s.StartRepositoryJob(ctx, d.ID, "backup_prune", requestedBy); !errors.Is(err, ErrBusy) {
			t.Fatalf("%s admitted without acknowledgement: %v", requestedBy, err)
		}
	}
	ack := PruneRequest{AcknowledgeIntegrityFailure: true}
	if _, err := s.StartRepositoryJob(ctx, d.ID, "backup_prune", "scheduler", ack); !errors.Is(err, ErrBusy) {
		t.Fatalf("scheduler bypassed integrity gate: %v", err)
	}
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_prune", "operator", ack)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_prune", CheckRequest{}, RestoreRequest{}); !errors.Is(err, ErrBusy) || runner.count("prune", d.Repository) != 0 {
		t.Fatalf("execution bypassed acknowledgement: %v", err)
	}
	job, err = s.StartRepositoryJob(ctx, d.ID, "backup_prune", "operator", ack)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_prune", CheckRequest{}, RestoreRequest{}, ack); err != nil {
		t.Fatal(err)
	}
	if runner.count("prune", d.Repository) != 1 || s.state.Destinations[d.ID].DataCheck.Status != "failed" {
		t.Fatal("acknowledged maintenance did not run exactly once or erased proof")
	}
	events, err := s.jobs.ReplayEvents(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		found = found || strings.Contains(event.Message, "acknowledged unresolved repository integrity failure")
	}
	if !found {
		t.Fatal("operator acknowledgement is missing from durable job events")
	}
}

func TestUnlockUsesNormalRepositoryReservationAndNeverForcesLocks(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Locked repository")
	ctx := context.Background()
	_, release, err := s.beginRepositoryRead(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "operator"); !errors.Is(err, ErrBusy) {
		t.Fatalf("unlock raced repository reader: %v", err)
	}
	release()
	job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRepositoryJob(ctx, d.ID, "backup_check", "operator"); !errors.Is(err, ErrBusy) {
		t.Fatalf("unlock did not reserve repository: %v", err)
	}
	if _, err := s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	if runner.count("unlock", d.Repository) != 1 || len(runner.calls) != 1 || commandIn(runner.calls[0], "--remove-all") {
		t.Fatalf("unexpected unlock commands: %v", runner.calls)
	}
	saved, err := s.jobs.Get(ctx, job.ID)
	if err != nil || saved.State != "succeeded" || saved.Action != "backup_unlock" || saved.RequestedBy != "operator" {
		t.Fatalf("missing audited unlock job: %+v %v", saved, err)
	}
	if s.state.Destinations[d.ID].LastOperation != "backup_unlock" || s.state.Destinations[d.ID].CheckStatus != "unknown" {
		t.Fatal("unlock did not record outcome independently of integrity")
	}
}
