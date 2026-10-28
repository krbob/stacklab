package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUnlockDiagnosticSeparatesGuardFromExecutedUnlock(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		unreadable, remaining, unverifiable bool
		code                                string
		calls                               int
	}{
		{"unreadable or vanished metadata", true, false, false, "unlock_refused", 0},
		{"remaining locks", false, true, false, "unlock_incomplete", 1},
		{"postflight unavailable", false, false, true, "unlock_incomplete", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, base := newTestService(t)
			d := addDestination(t, s, "Unlock outcome")
			r := &unlockGuardRunner{base: base, listingSequence: [][]string{nil, nil, nil}}
			id := strings.Repeat("a", 64)
			if tc.unreadable {
				r.listingSequence[0] = []string{id}
				r.metadataError = errors.New("SECRET backend error")
			}
			if tc.remaining {
				r.listingSequence[2] = []string{id}
			}
			if tc.unverifiable {
				r.postUnlockError = errors.New("SECRET backend error")
			}
			s.runner = r
			ctx := context.Background()
			job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
			var diagnostic *repositoryCommandError
			if !errors.As(err, &diagnostic) || diagnostic.Code != tc.code || r.unlocks != tc.calls {
				t.Fatalf("err=%v unlock calls=%d", err, r.unlocks)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("private diagnostic leaked")
			}
		})
	}
}

func TestUnlockMetadataPreservesOperationalDiagnosis(t *testing.T) {
	for _, code := range []string{"unavailable", "credentials_rejected", "permission_denied", "cancelled", "timeout", "repository_missing"} {
		t.Run(code, func(t *testing.T) {
			s, _, base := newTestService(t)
			d := addDestination(t, s, "Unlock access")
			r := &unlockGuardRunner{base: base, ids: []string{strings.Repeat("a", 64)}, metadataError: &repositoryCommandError{Code: code, message: "classified access failure"}}
			s.runner = r
			ctx := context.Background()
			job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
			var diagnostic *repositoryCommandError
			if !errors.As(err, &diagnostic) || diagnostic.Code != code || r.unlocks != 0 {
				t.Fatalf("wrong preflight diagnosis or unlock executed: %v, calls=%d", err, r.unlocks)
			}
		})
	}
}
