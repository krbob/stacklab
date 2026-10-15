package backup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func inconclusiveCheck() error {
	return classifyCommandError("restic", errors.New("exit"), "Fatal: repository contains errors")
}

func TestInconclusiveCheckHoldPersistsWithoutReplacingProof(t *testing.T) {
	s, _, runner := newTestService(t)
	d := addDestination(t, s, "Unresolved check")
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	recordCheckAttempt(&d, "all", at, nil)
	metadata, data := clone(d.MetadataCheck), clone(d.DataCheck)
	recordCheckAttempt(&d, "all", at.Add(time.Minute), inconclusiveCheck())
	if d.CheckHold == nil || d.CheckHold.Scope != "all" || d.CheckStatus != "unverified" || !repositoryMaintenanceBlocked(d) || d.MaintenanceReviewToken == "" {
		t.Fatalf("inconclusive check has no maintenance hold: %+v", d)
	}
	if !reflect.DeepEqual(metadata, d.MetadataCheck) || !reflect.DeepEqual(data, d.DataCheck) {
		t.Fatal("inconclusive check replaced historical proof")
	}
	token, hold := d.MaintenanceReviewToken, clone(*d.CheckHold)
	for _, err := range []error{commandContextError(context.Canceled), commandContextError(context.DeadlineExceeded), &repositoryCommandError{Code: "unavailable", message: "offline"}, &repositoryCommandError{Code: "repository_locked", message: "locked"}, errors.New("local execution failed")} {
		recordCheckAttempt(&d, "all", at.Add(2*time.Minute), err)
		if d.MaintenanceReviewToken != token || !reflect.DeepEqual(*d.CheckHold, hold) || !reflect.DeepEqual(d.DataCheck, data) {
			t.Fatalf("transient failure replaced hold or proof: %+v", d)
		}
	}
	s.state.Destinations[d.ID] = d
	if err := s.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened := NewService(s.cfg, s.jobs, s.capture)
	reopened.runner = runner
	d = reopened.state.Destinations[d.ID]
	if d.CheckHold == nil || d.MaintenanceReviewToken != token || !reflect.DeepEqual(*d.CheckHold, hold) {
		t.Fatalf("hold did not survive restart: %+v", d)
	}
	for _, policy := range []string{"metadata", "subset"} {
		recordCheckAttempt(&d, policy, at.Add(3*time.Minute), nil)
		if d.CheckHold == nil || d.CheckStatus != "unverified" || d.MaintenanceReviewToken != token {
			t.Fatalf("%s incorrectly resolved full-read hold: %+v", policy, d)
		}
	}
	recordCheckAttempt(&d, "all", at.Add(4*time.Minute), nil)
	if d.CheckHold != nil || d.MaintenanceReviewToken != "" || d.CheckStatus != "verified" || repositoryMaintenanceBlocked(d) {
		t.Fatalf("full check did not resolve hold: %+v", d)
	}
}

func TestCheckHoldRequiredScopeAndCorruptionPrecedence(t *testing.T) {
	at := time.Now().UTC()
	for _, failedScope := range []string{"metadata", "subset", "all"} {
		for _, successScope := range []string{"metadata", "subset", "all"} {
			t.Run(failedScope+"/"+successScope, func(t *testing.T) {
				d := Destination{ID: "scope"}
				recordCheckAttempt(&d, "all", at, nil)
				recordCheckAttempt(&d, failedScope, at.Add(time.Minute), inconclusiveCheck())
				if d.CheckHold.Scope != requiredCheckScope(failedScope) {
					t.Fatalf("wrong required check scope: %+v", d.CheckHold)
				}
				recordCheckAttempt(&d, successScope, at.Add(2*time.Minute), nil)
				cleared := failedScope == "metadata" || successScope == "all"
				if (d.CheckHold == nil) != cleared {
					t.Fatalf("unexpected scope resolution: %+v", d.CheckHold)
				}
			})
		}
	}
	d := Destination{ID: "precedence"}
	recordCheckAttempt(&d, "all", at, inconclusiveCheck())
	hold, token := clone(*d.CheckHold), d.MaintenanceReviewToken
	recordCheckAttempt(&d, "metadata", at.Add(time.Minute), inconclusiveCheck())
	if !reflect.DeepEqual(*d.CheckHold, hold) || d.MaintenanceReviewToken != token {
		t.Fatal("weaker hold displaced unresolved full read")
	}
	recordCheckAttempt(&d, "metadata", at.Add(2*time.Minute), &repositoryCommandError{Code: "integrity_failed", message: "corrupt metadata"})
	if d.CheckStatus != "failed" || d.CheckHold == nil || d.MaintenanceReviewToken == token {
		t.Fatalf("corruption did not take precedence without erasing hold: %+v", d)
	}
	recordCheckAttempt(&d, "metadata", at.Add(3*time.Minute), nil)
	if d.CheckStatus != "unverified" || d.MaintenanceReviewToken != token {
		t.Fatal("resolving metadata corruption erased independent full-read hold")
	}
}

func TestMaintenanceReviewFingerprintTracksOnlyUnresolvedConditions(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 123, time.UTC)
	d := Destination{ID: "destination"}
	recordCheckAttempt(&d, "all", at, inconclusiveCheck())
	original := d.MaintenanceReviewToken
	if len(original) != 64 {
		t.Fatalf("invalid fingerprint: %q", original)
	}
	changed := clone(d)
	changed.Name, changed.LastOperationError = "renamed", "unavailable"
	local := at.In(time.FixedZone("fixture", 3600))
	changed.CheckHold.CheckedAt = &local
	if repositoryReviewFingerprint(changed) != original {
		t.Fatal("presentation, attempt, or timezone changed condition identity")
	}
	for _, modify := range []func(*Destination){
		func(d *Destination) { d.ID = "another-destination" },
		func(d *Destination) { d.CheckHold.Scope = "metadata" },
		func(d *Destination) { d.CheckHold.Error = "another unresolved condition" },
		func(d *Destination) { newer := at.Add(time.Second); d.CheckHold.CheckedAt = &newer },
		func(d *Destination) {
			d.MetadataCheck = CheckEvidence{Status: "failed", Scope: "metadata", CheckedAt: &at, Error: "corruption"}
		},
		func(d *Destination) {
			d.DataCheck = CheckEvidence{Status: "failed", Scope: "all", CheckedAt: &at, Error: "corruption"}
		},
	} {
		changed := clone(d)
		modify(&changed)
		if repositoryReviewFingerprint(changed) == original {
			t.Fatal("changed unresolved condition retained acknowledgement token")
		}
	}
}

func TestCheckHoldMigrationIsSpecificAndDoesNotInventHistory(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	older := at.Add(-time.Hour)
	message := "restic: repository check was inconclusive; inspect repository access and retry a full data check before relying on it (operation_failed)"
	for _, test := range []struct {
		name, message, status string
		successAt             *time.Time
		want                  bool
	}{
		{"known older diagnostic", message, "failed", &older, true},
		{"transport", "restic: destination connection failed (unavailable)", "failed", &older, false},
		{"generic operation", "restic: operation failed (operation_failed)", "failed", &older, false},
		{"newer success", message, "failed", &at, false},
		{"already successful attempt", message, "succeeded", &older, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			attemptAt := at.Add(-time.Minute)
			d := Destination{ID: "legacy", CheckStatus: "verified", LastCheckAt: test.successAt, LastCheckPolicy: "all", LastCheckAttemptAt: &attemptAt, LastCheckAttemptPolicy: "subset", LastCheckAttemptStatus: test.status, LastCheckAttemptError: test.message}
			normalizeCheckEvidence(&d)
			if (d.CheckHold != nil) != test.want || d.DataCheck.Status != "verified" {
				t.Fatalf("incorrect migration: %+v", d)
			}
			if test.want && (d.CheckHold.Scope != "all" || !d.CheckHold.CheckedAt.Equal(attemptAt)) {
				t.Fatal("migration invented or narrowed hold history")
			}
			before := clone(d)
			normalizeCheckEvidence(&d)
			if !reflect.DeepEqual(before, d) {
				t.Fatal("migration is not idempotent")
			}
		})
	}
	d := Destination{CheckStatus: "failed", CheckError: "ambiguous legacy failure (operation_failed)"}
	normalizeCheckEvidence(&d)
	if d.LastCheckAttemptStatus != "failed" || d.LastCheckAttemptPolicy != "unknown" || d.LastCheckAttemptAt != nil || d.CheckHold != nil || !strings.Contains(d.CheckError, "ambiguous") {
		t.Fatalf("legacy attempt backfill invented proof or timestamps: %+v", d)
	}
	for _, scope := range []string{"metadata", "subset", "all", "unknown"} {
		d := Destination{ID: "later-light-check", CheckStatus: "verified", LastCheckAt: &at, LastCheckPolicy: "metadata", LastCheckAttemptAt: &older, LastCheckAttemptPolicy: scope, LastCheckAttemptStatus: "failed", LastCheckAttemptError: message}
		normalizeCheckEvidence(&d)
		if (d.CheckHold == nil) != (scope == "metadata") {
			t.Fatalf("later metadata success incorrectly resolved %s historical attempt: %+v", scope, d)
		}
	}
}
