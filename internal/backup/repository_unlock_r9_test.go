package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUnlockNativeDiagnosticOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, text, code string
		preflight        bool
	}{
		{"vanished metadata", "Load(<lock/aaaaaaaa>, 0, 0) failed: open /repo/locks/aaaaaaaa: no such file or directory", "unlock_refused", true},
		{"unknown unlock outcome", "Error: unsupported lock response SECRET", "unlock_incomplete", false},
		{"unlock access", "Load(<lock/aaaaaaaa>, 0, 0) failed: EOF", "unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, base := newTestService(t)
			d := addDestination(t, s, "Outcome")
			args := []string{"unlock"}
			if tc.preflight {
				args = []string{"cat", "lock"}
			}
			nativeErr := classifyCommandError("restic", errors.New("exit"), tc.text, args...)
			runner := &unlockGuardRunner{base: base}
			if tc.preflight {
				runner.ids = []string{strings.Repeat("a", 64)}
				runner.metadataError = nativeErr
			} else {
				runner.unlockError = nativeErr
			}
			s.runner = runner
			ctx := context.Background()
			job, err := s.StartRepositoryJob(ctx, d.ID, "backup_unlock", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteRepositoryJob(ctx, job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
			var diagnostic *repositoryCommandError
			if !errors.As(err, &diagnostic) || diagnostic.Code != tc.code {
				t.Fatalf("outcome: %v", err)
			}
			if tc.preflight && runner.unlocks != 0 {
				t.Fatal("unsafe unlock attempted")
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("raw diagnostic leaked")
			}
		})
	}
}
