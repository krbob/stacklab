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

func TestAutomaticPauseKeepsMonitoringAndEmitsDurableTransitions(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	d := addDestination(t, s, "offsite")
	p := addPlan(t, s, d)
	due := now.Add(-2 * time.Hour)
	p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
	p.Destinations[0].ScheduleActivatedAt = &due
	p.Destinations[0].LastScheduledAt = &due
	s.state.Plans[p.ID] = p
	s.recoveryError = "original writer is missing"
	if err := s.persist(ctx); err != nil {
		t.Fatal(err)
	}
	alerts := []OperationalAlert{}
	notify := func(_ context.Context, alert OperationalAlert) error { alerts = append(alerts, alert); return nil }
	s.SetOperationalNotifier(notify)
	view := s.Policy()
	if view.Paused || !view.EffectivePaused || len(view.PauseReasons) != 1 || view.PauseReasons[0].Code != "service_recovery_required" {
		t.Fatalf("user intent mixed with effective pause: %+v", view)
	}
	if !errors.Is(s.schedulingPauseErrorLocked(), ErrBusy) {
		t.Fatal("unsafe manual capture is not an actionable conflict")
	}
	s.observeProtection(ctx)
	s.observeProtection(ctx)
	events := map[string]int{}
	for _, alert := range alerts {
		events[alert.Event]++
	}
	if events["backup_protection_paused"] != 1 || events["backup_copy_overdue"] != 1 {
		t.Fatalf("paused monitoring = %+v", alerts)
	}
	restarted := NewService(s.cfg, s.jobs, s.capture)
	restarted.now, restarted.recoveryError = s.now, s.recoveryError
	restarted.SetOperationalNotifier(notify)
	restarted.observeProtection(ctx)
	if len(alerts) != 2 {
		t.Fatalf("restart repeated delivered alerts: %+v", alerts)
	}
	restarted.recoveryError = ""
	restarted.observeProtection(ctx)
	if len(alerts) != 3 || alerts[2].Event != "backup_protection_resumed" {
		t.Fatalf("missing resume transition: %+v", alerts)
	}
	stamp := now
	restarted.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)] = copyCheckpoint{CapturedAt: stamp, SnapshotID: "abcdef12", Selection: selectionFingerprint(p), Verification: recoveryObjectsVerified, SnapshotStatus: "present", SnapshotCheckedAt: &stamp}
	d.Status, d.CheckStatus = "available", "failed"
	restarted.state.Destinations[d.ID] = d
	restarted.observeProtection(ctx)
	if len(alerts) != 3 {
		t.Fatal("corrupt evidence falsely resolved overdue")
	}
	d.CheckStatus = "verified"
	restarted.state.Destinations[d.ID] = d
	restarted.observeProtection(ctx)
	restarted.observeProtection(ctx)
	if len(alerts) != 4 || alerts[3].Event != "backup_copy_resolved" {
		t.Fatalf("resolution not delivered exactly once: %+v", alerts)
	}
}

func TestMalformedPolicyAlertDoesNotRewritePolicyAndRetriesDelivery(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	malformed := "{not valid policy"
	if err := s.jobs.Store().SetAppSetting(ctx, policySettingKey, malformed, s.now()); err != nil {
		t.Fatal(err)
	}
	restored := NewService(s.cfg, s.jobs, s.capture)
	attempts := 0
	restored.SetOperationalNotifier(func(_ context.Context, alert OperationalAlert) error {
		attempts++
		if alert.Event != "backup_protection_paused" || alert.PauseReasons[0].Code != "policy_error" {
			t.Fatalf("bad transition: %+v", alert)
		}
		if attempts == 1 {
			return errors.New("webhook unavailable")
		}
		return nil
	})
	restored.observeProtection(ctx)
	restored.observeProtection(ctx)
	restored.observeProtection(ctx)
	if attempts != 2 {
		t.Fatalf("delivery attempts=%d", attempts)
	}
	raw, _, err := s.jobs.Store().AppSetting(ctx, policySettingKey)
	if err != nil || raw != malformed {
		t.Fatalf("monitor overwrote broken policy: %q %v", raw, err)
	}
}

func TestLegacyHostReviewIsNotUserPauseAndPersistsOnce(t *testing.T) {
	s, _, _ := newTestService(t)
	d := addDestination(t, s, "legacy")
	addPlan(t, s, d)
	s.policy.HostIdentity = ""
	if err := s.persistPolicyLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := NewService(s.cfg, s.jobs, s.capture)
	policy := restored.Policy()
	if policy.Paused || !policy.EffectivePaused || !policy.HostReviewRequired || policy.PauseReasons[0].Code != "host_review_required" {
		t.Fatalf("legacy review intent: %+v", policy)
	}
	if _, err := restored.SavePolicy(context.Background(), PolicyInput{AcknowledgeHost: true, OverdueGraceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	again := NewService(s.cfg, s.jobs, s.capture)
	if again.Policy().EffectivePaused {
		t.Fatal("normal subsequent upgrade requires host review again")
	}
	if _, err := again.SavePolicy(context.Background(), PolicyInput{Paused: true, OverdueGraceMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	if err := again.schedulingPauseErrorLocked(); err != nil {
		t.Fatalf("explicit automatic pause rejected manual capture: %v", err)
	}
}

func TestInitializationFailureStillNotifiesWithoutOpeningBackupState(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	if err := s.jobs.Store().SetAppSetting(ctx, settingKey, "{broken backup state", s.now()); err != nil {
		t.Fatal(err)
	}
	broken := NewService(s.cfg, s.jobs, s.capture)
	alerts := []OperationalAlert{}
	broken.SetOperationalNotifier(func(_ context.Context, alert OperationalAlert) error { alerts = append(alerts, alert); return nil })
	broken.observeProtection(ctx)
	broken.observeProtection(ctx)
	if len(alerts) != 1 || alerts[0].Event != "backup_protection_paused" || alerts[0].PauseReasons[0].Code != "initialization_error" {
		t.Fatalf("initialization failure was silent: %+v", alerts)
	}
}

func TestProtectionAlertBatchIsBoundedAndFairAcrossFailedDeliveries(t *testing.T) {
	s, _, _ := newTestService(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	d := addDestination(t, s, "scheduled")
	base := addPlan(t, s, d)
	s.state.Plans = map[string]Plan{}
	due := now.Add(-2 * time.Hour)
	for index := 0; index < 25; index++ {
		plan := clone(base)
		plan.ID = string(rune('a' + index))
		plan.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
		plan.Destinations[0].ScheduleActivatedAt = &due
		s.state.Plans[plan.ID] = plan
	}
	seen := map[string]bool{}
	attempts := 0
	s.SetOperationalNotifier(func(_ context.Context, alert OperationalAlert) error {
		attempts++
		seen[alert.PlanID] = true
		return errors.New("channel down")
	})
	s.observeProtection(context.Background())
	if attempts != 16 {
		t.Fatalf("unbounded delivery batch: %d", attempts)
	}
	s.observeProtection(context.Background())
	if len(seen) != 25 {
		t.Fatalf("later plans starved: %d of25", len(seen))
	}
	attempts = 0
	s.SetOperationalNotifier(func(ctx context.Context, _ OperationalAlert) error { attempts++; <-ctx.Done(); return ctx.Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	s.observeProtection(ctx)
	if attempts != 1 || time.Since(started) > time.Second {
		t.Fatalf("cancelled observation kept delivering: attempts=%d elapsed=%s", attempts, time.Since(started))
	}
}

func TestMalformedGraceKeepsPublicPolicyValid(t *testing.T) {
	s, _, _ := newTestService(t)
	if err := s.jobs.Store().SetAppSetting(context.Background(), policySettingKey, `{"overdue_grace_minutes":9223372036854775807}`, s.now()); err != nil {
		t.Fatal(err)
	}
	broken := NewService(s.cfg, s.jobs, s.capture)
	policy := broken.Policy()
	if !policy.EffectivePaused || policy.OverdueGraceMinutes != 60 {
		t.Fatalf("invalid grace escaped into API/monitoring: %+v", policy)
	}
}
