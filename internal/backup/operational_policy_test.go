package backup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveredHostPausesSchedulingUntilExplicitReview(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	s.policy.HostIdentity = "different-host"
	if err := s.persistPolicyLocked(ctx); err != nil {
		t.Fatal(err)
	}
	restored := NewService(s.cfg, s.jobs, s.capture)
	if !restored.SchedulingPaused() || !restored.Policy().HostReviewRequired {
		t.Fatalf("host move did not pause: %+v", restored.Policy())
	}
	if _, err := restored.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unreviewed resume: %v", err)
	}
	if _, err := restored.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60, AcknowledgeHost: true}); err != nil {
		t.Fatal(err)
	}
	if restored.SchedulingPaused() {
		t.Fatal("reviewed host remains paused")
	}
	again := NewService(s.cfg, s.jobs, s.capture)
	if again.SchedulingPaused() {
		t.Fatal("acknowledgement did not persist")
	}
}
func TestPolicyHeartbeatIsWriteOnlyAndMalformedStateFailsClosed(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	target := "https://monitor.example/push/private-token"
	view, err := s.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60, HeartbeatURL: &target})
	if err != nil || !view.HeartbeatConfigured {
		t.Fatal(view, err)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "private-token") {
		t.Fatal("heartbeat secret exposed")
	}
	blank := ""
	_, err = s.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60, HeartbeatURL: &blank})
	if err != nil || s.policy.HeartbeatURL != target {
		t.Fatal("blank did not preserve", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "https://user:secret@example/x", "https://example/x#fragment", "http:///missing-host"} {
		if _, err = s.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60, HeartbeatURL: &bad}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %s: %v", bad, err)
		}
	}
	if err = s.jobs.Store().SetAppSetting(ctx, policySettingKey, "null", s.now()); err != nil {
		t.Fatal(err)
	}
	restored := NewService(s.cfg, s.jobs, s.capture)
	if !restored.SchedulingPaused() {
		t.Fatal("null policy should not silently enable scheduling")
	}
}
func TestOverdueGraceDeduplicationAndExternalProtectionHeartbeat(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 10, 30, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	d := addDestination(t, s, "offsite")
	p := addPlan(t, s, d)
	due := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
	p.Destinations[0].LastScheduledAt = &due
	p.Destinations[0].ScheduleActivatedAt = &due
	s.state.Plans[p.ID] = p
	alerts := []OperationalAlert{}
	s.SetOperationalNotifier(func(_ context.Context, a OperationalAlert) error { alerts = append(alerts, a); return nil })
	var hits atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer endpoint.Close()
	target := endpoint.URL + "/private-monitor-token"
	if _, err := s.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60, HeartbeatURL: &target}); err != nil {
		t.Fatal(err)
	}
	s.observeProtection(ctx)
	if len(alerts) != 0 || hits.Load() != 0 {
		t.Fatal("premature overdue or false heartbeat")
	}
	now = now.Add(time.Hour)
	s.observeProtection(ctx)
	s.observeProtection(ctx)
	if len(alerts) != 1 || alerts[0].PlanID != p.ID || alerts[0].DestinationName != "offsite" || !alerts[0].DueAt.Equal(due) {
		t.Fatalf("alerts %+v", alerts)
	}
	stamp := now
	s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)] = copyCheckpoint{CapturedAt: stamp, SnapshotID: "abcdef12", Selection: selectionFingerprint(p), Verification: recoveryObjectsVerified, SnapshotStatus: "present", SnapshotCheckedAt: &stamp}
	d.Status = "available"
	d.CheckStatus = "verified"
	s.state.Destinations[d.ID] = d
	s.observeProtection(ctx)
	if hits.Load() != 1 || s.Policy().LastHeartbeatAt == nil || len(s.policy.Overdue) != 0 {
		t.Fatalf("heartbeat hits=%d policy=%+v", hits.Load(), s.Policy())
	}
	s.observeProtection(ctx)
	if hits.Load() != 1 {
		t.Fatal("duplicate heartbeat within interval")
	}
	now = now.Add(time.Minute)
	d.CheckStatus = "failed"
	s.state.Destinations[d.ID] = d
	s.state.Runs["active"] = persistedRun{Run: Run{ID: "active", PlanID: p.ID, State: "running", CreatedAt: now, Copies: []CopyResult{{DestinationID: d.ID, State: "running"}}}}
	s.observeProtection(ctx)
	if hits.Load() != 1 {
		t.Fatal("running transfer hid failed integrity evidence from heartbeat")
	}
}

func TestNewScheduleDoesNotAlertForAnOccurrenceBeforeActivation(t *testing.T) {
	s, _, _ := newTestService(t)
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	d := addDestination(t, s, "scheduled")
	p := addPlan(t, s, d)
	p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
	activated := now
	p.Destinations[0].ScheduleActivatedAt = &activated
	s.state.Plans[p.ID] = p
	alerts := 0
	s.SetOperationalNotifier(func(context.Context, OperationalAlert) error { alerts++; return nil })
	s.observeProtection(context.Background())
	if alerts != 0 {
		t.Fatal("new schedule alerted about an occurrence before activation")
	}
	now = now.Add(24 * time.Hour)
	s.observeProtection(context.Background())
	if alerts != 1 {
		t.Fatalf("missed first scheduled occurrence: alerts=%d", alerts)
	}
}

func TestLegacyPlansRequireOneTimeHostReview(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "legacy")
	addPlan(t, s, d)
	s.policy.HostIdentity = ""
	if err := s.persistPolicyLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := NewService(s.cfg, s.jobs, s.capture)
	if !restored.SchedulingPaused() || !restored.Policy().HostReviewRequired {
		t.Fatal("legacy state without host identity started automatic work")
	}
}
func TestHeartbeatDoesNotFollowRedirectsOrExposeSecret(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer redirect.Close()
	if err := sendProtectionHeartbeat(context.Background(), redirect.URL+"/secret"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("redirect=%v", err)
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect followed")
	}
}
