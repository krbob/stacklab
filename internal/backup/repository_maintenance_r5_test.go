package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestUnlockOwnerGuard(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, host string
		pid        int
		at         time.Time
		probe      error
		allowed    bool
	}{
		{"active same UID", "host", 42, now, nil, false},
		{"active foreign UID", "host", 42, now, syscall.EPERM, false},
		{"active old lock", "host", 42, now.Add(-time.Hour), nil, false},
		{"dead local owner", "host", 42, now, syscall.ESRCH, true},
		{"remote old lock", "remote", 42, now.Add(-time.Hour), syscall.ESRCH, false},
		{"missing host", "", 42, now, syscall.ESRCH, false},
		{"missing time", "host", 42, time.Time{}, syscall.ESRCH, false},
		{"zero pid", "host", 0, now, syscall.ESRCH, false},
		{"process group pid", "host", -1, now, syscall.ESRCH, false},
		{"overflow pid", "host", int(1<<32 + 42), now, syscall.ESRCH, false},
		{"uncertain probe", "host", 42, now, syscall.EIO, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			err := validateUnlockOwner(repositoryLock{Hostname: tc.host, PID: tc.pid, Time: tc.at}, "host", func(pid int) error { called = true; return tc.probe })
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
			if (tc.pid <= 0 || int64(tc.pid) > 1<<31-1) && called {
				t.Fatal("invalid PID reached signal probe")
			}
		})
	}
}

type unlockGuardRunner struct {
	base      CommandRunner
	ids       []string
	metadata  []byte
	unlocks   int
	listError error
}

func (r *unlockGuardRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if commandIn(args, "list") && commandIn(args, "locks") {
		return []byte(strings.Join(r.ids, "\n")), r.listError
	}
	if commandIn(args, "cat") && commandIn(args, "lock") {
		return r.metadata, nil
	}
	if commandIn(args, "unlock") {
		r.unlocks++
		return nil, nil
	}
	return r.base.Run(ctx, name, args, env, dir)
}
func TestUnlockRefusesMalformedOrLiveLocksBeforeResticUnlock(t *testing.T) {
	host, _ := os.Hostname()
	live, _ := json.Marshal(repositoryLock{Hostname: host, PID: os.Getpid(), Time: time.Now().Add(-time.Hour)})
	for _, tc := range []struct {
		name      string
		ids       []string
		metadata  []byte
		listError error
	}{
		{name: "invalid inventory", ids: []string{"{}"}},
		{name: "read failure", listError: errors.New("inventory unavailable")},
		{name: "invalid metadata", ids: []string{strings.Repeat("a", 64)}, metadata: []byte(`{"pid":null}`)},
		{name: "live aged owner", ids: []string{strings.Repeat("a", 64)}, metadata: live},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, base := newTestService(t)
			d := addDestination(t, s, "Guard")
			r := &unlockGuardRunner{base: base, ids: tc.ids, metadata: tc.metadata, listError: tc.listError}
			s.runner = r
			job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_unlock", "test")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
			if err == nil || r.unlocks != 0 {
				t.Fatalf("unsafe unlock called: calls=%d err=%v", r.unlocks, err)
			}
			if s.state.Destinations[d.ID].LastOperationStatus != "failed" {
				t.Fatal("guard refusal not recorded")
			}
		})
	}
}

func TestPruneAcknowledgementInvalidatesWhenUnresolvedEvidenceChanges(t *testing.T) {
	for _, mode := range []string{"new failure time", "new scope", "hold"} {
		t.Run(mode, func(t *testing.T) {
			s, _, runner := newTestService(t)
			d := addDestination(t, s, "Review revision")
			at := time.Now().UTC()
			d.MetadataCheck = CheckEvidence{Status: "failed", Scope: "metadata", CheckedAt: &at, Error: "corrupt index (integrity_failed)"}
			normalizeCheckEvidence(&d)
			s.state.Destinations[d.ID] = d
			ack := PruneRequest{AcknowledgeIntegrityFailure: true, AcknowledgementReviewToken: repositoryReviewFingerprint(d)}
			job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_prune", "operator", ack)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "new failure time":
				later := at.Add(time.Minute)
				d.MetadataCheck.CheckedAt = &later
			case "new scope":
				d.DataCheck = CheckEvidence{Status: "failed", Scope: "all", CheckedAt: &at, Error: "bad pack (integrity_failed)"}
			case "hold":
				d.CheckHold = &CheckHold{Scope: "all", CheckedAt: &at, Error: "unresolved repository diagnosis"}
			}
			normalizeCheckEvidence(&d)
			s.state.Destinations[d.ID] = d
			_, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_prune", CheckRequest{}, RestoreRequest{}, ack)
			if !errors.Is(err, ErrMaintenanceAcknowledgement) || runner.count("prune", d.Repository) != 0 {
				t.Fatalf("stale review accepted: %v", err)
			}
			if _, err = s.StartRepositoryJob(context.Background(), d.ID, "backup_prune", "operator", ack); !errors.Is(err, ErrMaintenanceAcknowledgement) {
				t.Fatalf("stale admission accepted: %v", err)
			}
		})
	}
}
