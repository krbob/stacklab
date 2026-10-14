package backup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOverdueRemainsOutstandingAcrossOccurrencesAndRestart(t *testing.T) {
	for _, notifications := range []bool{false, true} {
		t.Run(map[bool]string{false: "notifications_disabled", true: "notifications_enabled"}[notifications], func(t *testing.T) {
			s, _, _ := newTestService(t)
			ctx := context.Background()
			now := parseTestTime(t, "2026-10-14T11:01:00Z")
			s.now = func() time.Time { return now }
			d := addDestination(t, s, "scheduled")
			p := addPlan(t, s, d)
			activated := now.AddDate(0, 0, -3)
			p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
			p.Destinations[0].ScheduleActivatedAt = &activated
			s.state.Plans[p.ID] = p
			captured := parseTestTime(t, "2026-10-13T10:30:00Z")
			checked := now
			s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)] = copyCheckpoint{CapturedAt: captured, SnapshotID: "abcdef12", Selection: selectionFingerprint(p), Verification: recoveryObjectsVerified, SnapshotStatus: "present", SnapshotCheckedAt: &checked}
			var hits atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusNoContent) }))
			defer endpoint.Close()
			target := endpoint.URL
			if _, err := s.SavePolicy(ctx, PolicyInput{OverdueGraceMinutes: 60, HeartbeatURL: &target}); err != nil {
				t.Fatal(err)
			}
			alerts := []OperationalAlert{}
			notify := func(_ context.Context, alert OperationalAlert) error { alerts = append(alerts, alert); return nil }
			if notifications {
				s.SetOperationalNotifier(notify)
			}
			s.observeProtection(ctx)
			if err := s.persist(ctx); err != nil {
				t.Fatal(err)
			}
			s = NewService(s.cfg, s.jobs, s.capture)
			s.now = func() time.Time { return now }
			if notifications {
				s.SetOperationalNotifier(notify)
			}
			for _, stamp := range []string{"2026-10-15T10:30:00Z", "2026-10-15T11:30:00Z", "2026-10-16T10:30:00Z"} {
				now = parseTestTime(t, stamp)
				checkpoint := s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)]
				checked = now
				checkpoint.SnapshotCheckedAt = &checked
				s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)] = checkpoint
				s.observeProtection(ctx)
				if hits.Load() != 0 {
					t.Fatalf("heartbeat claimed recovery at %s without a new capture", stamp)
				}
			}
			if notifications && (len(alerts) != 1 || alerts[0].Event != "backup_copy_overdue" || !alerts[0].DueAt.Equal(parseTestTime(t, "2026-10-14T10:00:00Z"))) {
				t.Fatalf("calendar rollover changed overdue transition: %+v", alerts)
			}
			checkpoint := s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)]
			checkpoint.CapturedAt = now
			s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)] = checkpoint
			s.observeProtection(ctx)
			if hits.Load() != 1 || notifications && (len(alerts) != 2 || alerts[1].Event != "backup_copy_resolved") {
				t.Fatalf("new usable capture failed to recover protection: hits=%d alerts=%+v", hits.Load(), alerts)
			}
		})
	}
}

func TestFirstOutstandingDueUsesCalendarActivationAndValidClockEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, captured, activated, now, want string
		schedule                             Schedule
	}{
		{"interval_multiple_misses", "2026-10-13T10:30:00Z", "2026-10-10T00:00:00Z", "2026-10-15T10:30:00Z", "2026-10-13T12:00:00Z", Schedule{Frequency: "interval", IntervalHours: 4}},
		{"activation_at_occurrence", "", "2026-10-14T10:00:00Z", "2026-10-15T10:30:00Z", "2026-10-14T10:00:00Z", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
		{"activation_after_occurrence", "2026-10-13T12:00:00Z", "2026-10-14T20:00:00Z", "2026-10-15T11:30:00Z", "2026-10-15T10:00:00Z", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
		{"capture_at_occurrence", "2026-10-14T10:00:00Z", "2026-10-13T10:00:00Z", "2026-10-16T11:30:00Z", "2026-10-15T10:00:00Z", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
		{"new_schedule_not_due", "", "2026-10-14T20:00:00Z", "2026-10-14T21:30:00Z", "", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
		{"dst_missing_hour", "2026-03-28T10:00:00Z", "", "2026-03-31T10:00:00Z", "2026-03-30T00:30:00Z", Schedule{Frequency: "daily", Time: "02:30", Timezone: "Europe/Warsaw"}},
		{"dst_weekly_offset", "2026-10-24T10:00:00Z", "", "2026-11-03T10:00:00Z", "2026-10-26T08:30:00Z", Schedule{Frequency: "weekly", Time: "09:30", Weekday: 1, Timezone: "Europe/Warsaw"}},
		{"future_capture_does_not_defer", "2026-10-20T10:00:00Z", "2026-10-13T10:00:00Z", "2026-10-14T10:30:00Z", "2026-10-13T10:00:00Z", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
		{"future_activation_does_not_defer", "2026-10-12T10:00:00Z", "2026-10-20T10:00:00Z", "2026-10-14T10:30:00Z", "2026-10-13T10:00:00Z", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
		{"legacy_missing_evidence", "", "", "2026-10-14T10:30:00Z", "2026-10-13T10:00:00Z", Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assignment := Assignment{Schedule: tc.schedule}
			var captured *time.Time
			if tc.captured != "" {
				value := parseTestTime(t, tc.captured)
				captured = &value
			}
			if tc.activated != "" {
				value := parseTestTime(t, tc.activated)
				assignment.ScheduleActivatedAt = &value
			}
			got, ok := firstOutstandingDue(assignment, captured, parseTestTime(t, tc.now), time.Hour)
			if tc.want == "" {
				if ok {
					t.Fatalf("future occurrence already due: %s", got)
				}
			} else if !ok || !got.Equal(parseTestTime(t, tc.want)) {
				t.Fatalf("due=%s exists=%v want=%s", got, ok, tc.want)
			}
		})
	}
}

func TestCompletedCopyFailureSurvivesNewRunAndRetryUntilSuccess(t *testing.T) {
	s, _, _ := newTestService(t)
	now := parseTestTime(t, "2026-10-14T11:00:00Z")
	s.now = func() time.Time { return now }
	d := addDestination(t, s, "copy")
	p := addPlan(t, s, d)
	success := completedTestRun(p, d, "good", now.Add(-time.Hour), now.Add(-time.Hour))
	if err := s.saveRun(success, p); err != nil {
		t.Fatal(err)
	}
	failed := completedTestRun(p, d, "failed", now, now)
	failed.State, failed.Copies[0].State, failed.Copies[0].Error = "failed", "failed", "repository unavailable"
	failed.Copies[0].SnapshotID, failed.Copies[0].Verification = "", "failed"
	failed.Copies[0].CompletedAt = &now
	if err := s.saveRun(failed, p); err != nil {
		t.Fatal(err)
	}
	assertFailure := func() {
		t.Helper()
		view := protectionFor(t, s)
		if view.State != "attention" || view.Copies[0].LastError != "repository unavailable" || view.Copies[0].LastRunID != failed.ID {
			t.Fatalf("settled error disappeared: %+v", view)
		}
	}
	assertFailure()
	active := completedTestRun(p, d, "newer", now.Add(time.Minute), now)
	active.State, active.Copies[0].State, active.Copies[0].SnapshotID = "running", "running", ""
	if err := s.saveRun(active, p); err != nil {
		t.Fatal(err)
	}
	assertFailure()
	if view := protectionFor(t, s); view.Activity != "running" || view.Copies[0].Activity != "running" {
		t.Fatal("active replacement is not visible")
	}
	// A retry mutates the original run instead of creating new history.
	failed.State, failed.Copies[0].State, failed.Copies[0].Error = "running", "pending", ""
	failed.Copies[0].CompletedAt = nil
	if err := s.saveRun(failed, p); err != nil {
		t.Fatal(err)
	}
	assertFailure()
	delete(s.state.Runs, "failed")
	if err := s.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	s = NewService(s.cfg, s.jobs, s.capture)
	s.now = func() time.Time { return now }
	assertFailure()
	now = now.Add(2 * time.Minute)
	finished := now
	active.State, active.Copies[0].State, active.Copies[0].CompletedAt = "succeeded", "succeeded", &finished
	if err := s.saveRun(active, p); err != nil {
		t.Fatal(err)
	}
	if view := protectionFor(t, s); view.State != "manual" || view.Copies[0].LastError != "" {
		t.Fatalf("completed success did not clear earlier failure: %+v", view)
	}
}

func TestRetentionFailureKeepsUsableCopyAndAttention(t *testing.T) {
	s, _, _ := newTestService(t)
	now := time.Now().UTC()
	d := addDestination(t, s, "copy")
	p := addPlan(t, s, d)
	run := completedTestRun(p, d, "maintenance_failed", now, now)
	run.State, run.Copies[0].Retention, run.Copies[0].RetentionError = "failed", "failed", "retention unavailable"
	if err := s.saveRun(run, p); err != nil {
		t.Fatal(err)
	}
	view := protectionFor(t, s)
	if view.State != "attention" || !view.Copies[0].EvidenceUsable || view.CurrentCopies != 1 || view.Copies[0].LastError != "retention unavailable" {
		t.Fatalf("maintenance confused with copy failure: %+v", view)
	}
}

type readinessTestCapture struct {
	fakeRecovery
	checks, recoveries int
	err                error
}

func (c *readinessTestCapture) CheckRecoveryHelper(context.Context) error { c.checks++; return c.err }
func (c *readinessTestCapture) Recover(context.Context) error             { c.recoveries++; return nil }

func TestHelperReadinessUsesHealthyCacheAndQuickFailureRetry(t *testing.T) {
	s, _, _ := newTestService(t)
	capture := &readinessTestCapture{}
	s.capture = capture
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	s.RefreshHelperReadiness(context.Background())
	now = now.Add(14 * time.Minute)
	s.RefreshHelperReadiness(context.Background())
	if capture.checks != 1 || !s.helperReady {
		t.Fatal("healthy helper was needlessly reprobed")
	}
	now = now.Add(time.Minute)
	capture.err = errors.New("sudo denied")
	s.RefreshHelperReadiness(context.Background())
	if capture.checks != 2 || s.helperReady {
		t.Fatal("expired healthy readiness was not checked")
	}
	now = now.Add(59 * time.Second)
	s.RefreshHelperReadiness(context.Background())
	if capture.checks != 2 {
		t.Fatal("failed helper was retried before one minute")
	}
	now = now.Add(time.Second)
	capture.err = nil
	s.RefreshHelperReadiness(context.Background())
	if capture.checks != 3 || !s.helperReady || s.helperError != "" {
		t.Fatal("fixed helper readiness did not recover promptly")
	}
	now = now.Add(-time.Hour)
	s.RefreshHelperReadiness(context.Background())
	if capture.checks != 4 {
		t.Fatal("backwards clock suppressed readiness refresh")
	}
}

func TestRecoveryPreviewUpdatesReadinessAndReconcileDoesNotSpendWriterBudget(t *testing.T) {
	s, _, _ := newTestService(t)
	capture := &readinessTestCapture{err: errors.New("sudo denied")}
	s.capture = capture
	if err := s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if capture.recoveries != 1 || capture.checks != 0 {
		t.Fatal("helper probe consumed writer recovery startup budget")
	}
	if _, _, err := s.recoveryPreview(context.Background(), "missing", RestoreRequest{}); !errors.Is(err, ErrBusy) || s.helperReady {
		t.Fatalf("bad helper readiness not reported: %v", err)
	}
	capture.err = nil
	_, _, _ = s.recoveryPreview(context.Background(), "missing", RestoreRequest{})
	if !s.helperReady || s.helperError != "" {
		t.Fatal("successful preview probe left stale helper failure")
	}
}

func TestServiceRecoveryBlocksApplyAdmissionButAllowsIsolatedRestore(t *testing.T) {
	s, _, _ := newTestService(t)
	r := registerCatalogFixture(t, s, []catalogFixtureEntry{{name: "data", content: "test"}}, nil)
	s.recoveryError = "writer state needs inspection"
	for _, request := range []RestoreRequest{{RestoreID: r.ID}, {RestoredDirectory: r.Target}} {
		if err := s.ValidateRepositoryRequest(context.Background(), r.DestinationID, "backup_restore", CheckRequest{}, request); !errors.Is(err, ErrBusy) {
			t.Fatalf("apply was admitted with outstanding service recovery: %v", err)
		}
	}
	if err := s.ValidateRepositoryRequest(context.Background(), r.DestinationID, "backup_restore", CheckRequest{}, RestoreRequest{SnapshotID: "aabbccddeeff0011"}); err != nil {
		t.Fatalf("service recovery blocked isolated retrieval: %v", err)
	}
	s.recoveryError, s.policy.Paused = "", true
	if err := s.ValidateRepositoryRequest(context.Background(), r.DestinationID, "backup_restore", CheckRequest{}, RestoreRequest{RestoreID: r.ID}); err != nil {
		t.Fatalf("operator automatic pause blocked explicit apply: %v", err)
	}
	active, err := s.jobs.Store().ListActiveJobs(context.Background())
	if err != nil || len(active) != 0 {
		t.Fatalf("admission validation created jobs: %v %v", active, err)
	}
}

func TestMissingWriterReasonCountsTrimmedUnicodeCharacters(t *testing.T) {
	capture, _, _ := captureFixture(t)
	journal := newStopJournal("unicode", []Writer{{ContainerID: "missing", Running: true}})
	journal.Phases["missing"] = "stopped"
	if err := capture.saveStopJournal(journal); err != nil {
		t.Fatal(err)
	}
	containers, unavailable, mutations := []captureContainer{}, false, []string{}
	capture.command = journalInventoryCommand(t, &containers, &unavailable, &mutations)
	for _, count := range []int{4, 7, 1001} {
		input := RecoveryResolution{Action: "acknowledge_missing", Inspected: true, Reason: " \n" + strings.Repeat("界", count) + " "}
		if err := capture.CheckRecoveryResolution(context.Background(), journal.RunID, input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %d Unicode characters: %v", count, err)
		}
	}
	for _, count := range []int{8, 1000} {
		input := RecoveryResolution{Action: "acknowledge_missing", Inspected: true, Reason: " \n" + strings.Repeat("界", count) + " "}
		if err := capture.CheckRecoveryResolution(context.Background(), journal.RunID, input); err != nil {
			t.Fatalf("rejected %d Unicode characters: %v", count, err)
		}
	}
	input := RecoveryResolution{Action: "acknowledge_missing", Inspected: true, Reason: "  Sprawdzono kontener i dane.  "}
	if err := capture.ResolveRecoveryWithInput(context.Background(), journal.RunID, input); err != nil {
		t.Fatal(err)
	}
	record, err := os.ReadFile(filepath.Join(capture.cfg.DataDir, "backup", "recovery-history", filepath.Base(capture.journalPath(journal.RunID))))
	if err != nil || !strings.Contains(string(record), `"reason":"Sprawdzono kontener i dane."`) {
		t.Fatalf("reason not normalized in durable audit: %s %v", record, err)
	}
}

func TestRecoveryMappingWarnsAboutChangedTargetAndManagedScope(t *testing.T) {
	root := t.TempDir()
	source := ManifestSource{Source: Source{ID: "definition", Kind: "stack_definition", StackID: "media"}, OriginalPath: filepath.Join(root, "stacks", "media")}
	for _, tc := range []struct {
		target, mode string
		warnings     int
	}{
		{filepath.Join(root, "stacks", "media"), "replace", 0},
		{filepath.Join(root, "stacks", "photos"), "replace", 2},
		{filepath.Join(root, "config", "media"), "replace", 2},
		{filepath.Join(root, "stacks", "photos"), "merge", 1},
		{filepath.Join(root, "stacks-other", "photos"), "merge", 0},
	} {
		warnings := restoreMappingWarnings(root, source, RestoreMapping{SourceID: source.Source.ID, Target: tc.target, Mode: tc.mode})
		if len(warnings) != tc.warnings {
			t.Fatalf("target=%s mode=%s warnings=%v", tc.target, tc.mode, warnings)
		}
	}
}
