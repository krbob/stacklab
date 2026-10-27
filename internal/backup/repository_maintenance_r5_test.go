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
		{"fresh absent PID may be in another namespace", "host", 42, now.Add(-20 * time.Second), syscall.ESRCH, false},
		{"just under minimum age", "host", 42, now.Add(-30*time.Minute + time.Nanosecond), syscall.ESRCH, false},
		{"dead local owner at minimum age", "host", 42, now.Add(-30 * time.Minute), syscall.ESRCH, true},
		{"dead local owner old lock", "host", 42, now.Add(-time.Hour), syscall.ESRCH, true},
		{"future timestamp", "host", 42, now.Add(time.Hour), syscall.ESRCH, false},
		{"active old foreign UID", "host", 42, now.Add(-time.Hour), syscall.EPERM, false},
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
			err := validateUnlockOwner(repositoryLock{Hostname: tc.host, PID: tc.pid, Time: tc.at}, "host", now, func(pid int) error { called = true; return tc.probe })
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
	base            CommandRunner
	ids             []string
	metadata        []byte
	unlocks         int
	listError       error
	metadataError   error
	postUnlockError error
	listingSequence [][]string
	lists           int
}

func (r *unlockGuardRunner) Run(ctx context.Context, name string, args, env []string, dir string) ([]byte, error) {
	if commandIn(args, "list") && commandIn(args, "locks") {
		if r.unlocks > 0 && r.postUnlockError != nil {
			return nil, r.postUnlockError
		}
		if r.lists < len(r.listingSequence) {
			ids := r.listingSequence[r.lists]
			r.lists++
			return []byte(strings.Join(ids, "\n")), r.listError
		}
		return []byte(strings.Join(r.ids, "\n")), r.listError
	}
	if commandIn(args, "cat") && commandIn(args, "lock") {
		return r.metadata, r.metadataError
	}
	if commandIn(args, "unlock") {
		r.unlocks++
		return nil, nil
	}
	return r.base.Run(ctx, name, args, env, dir)
}

func TestUnlockRefusesChangedInventory(t *testing.T) {
	host, _ := os.Hostname()
	metadata, _ := json.Marshal(repositoryLock{Hostname: host, PID: 1 << 30, Time: time.Now().Add(-time.Hour)})
	if !errors.Is(syscall.Kill(1<<30, 0), syscall.ESRCH) {
		t.Fatal("fixture requires an absent PID")
	}
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, tc := range []struct {
		name          string
		before, after []string
		allowed       bool
	}{
		{"new lock", nil, []string{a}, false},
		{"removed lock", []string{a}, nil, false},
		{"replaced lock", []string{a}, []string{b}, false},
		{"reordered identical inventory", []string{a, b}, []string{b, a}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, base := newTestService(t)
			d := addDestination(t, s, "Inventory race")
			r := &unlockGuardRunner{base: base, metadata: metadata, listingSequence: [][]string{tc.before, tc.after, nil}}
			s.runner = r
			job, err := s.StartRepositoryJob(context.Background(), d.ID, "backup_unlock", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteRepositoryJob(context.Background(), job, d.ID, "backup_unlock", CheckRequest{}, RestoreRequest{})
			if (err == nil) != tc.allowed || (r.unlocks == 1) != tc.allowed {
				t.Fatalf("changed lock set reached unlock: %v, calls=%d", err, r.unlocks)
			}
		})
	}
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
