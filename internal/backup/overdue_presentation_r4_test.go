package backup

import (
	"context"
	"testing"
	"time"
)

func TestOverdueTimelineIsIndependentOfEvidenceStatus(t *testing.T) {
	for _, state := range []string{"current", "missing", "integrity_failed", "unavailable", "unverified", "unprotected"} {
		t.Run(state, func(t *testing.T) {
			s, _, _ := newTestService(t)
			now := parseTestTime(t, "2026-10-16T10:30:00Z")
			s.now = func() time.Time { return now }
			d := addDestination(t, s, "Late recovery point")
			p := addPlan(t, s, d)
			activated := parseTestTime(t, "2026-10-13T09:00:00Z")
			p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
			p.Destinations[0].ScheduleActivatedAt = &activated
			s.state.Plans[p.ID] = p
			captured := parseTestTime(t, "2026-10-13T10:00:00Z")
			checkpoint := copyCheckpoint{CapturedAt: captured, SnapshotID: "abcdef12", Selection: selectionFingerprint(p), Verification: recoveryObjectsVerified, SnapshotStatus: "present", SnapshotCheckedAt: &now}
			if state == "missing" {
				checkpoint.SnapshotStatus = "missing"
			}
			if state == "unverified" {
				checkpoint.Verification = "snapshot_metadata_verified"
			}
			if state == "integrity_failed" {
				d.DataCheck = CheckEvidence{Status: "failed", Scope: "all", Error: "corrupt pack"}
				d.CheckStatus, d.CheckError = "failed", "corrupt pack"
			}
			if state == "unavailable" {
				d.Status, d.Error = "error", "offline"
			}
			s.state.Destinations[d.ID] = d
			wantDue := parseTestTime(t, "2026-10-14T10:00:00Z")
			if state != "unprotected" {
				s.state.CopyCheckpoints[checkpointKey(p.ID, d.ID)] = checkpoint
			} else {
				wantDue = parseTestTime(t, "2026-10-13T10:00:00Z")
			}
			copy := protectionFor(t, s).Copies[0]
			wantSince := wantDue.Add(time.Hour)
			if copy.FirstOutstandingDue == nil || !copy.FirstOutstandingDue.Equal(wantDue) || copy.OverdueSince == nil || !copy.OverdueSince.Equal(wantSince) {
				t.Fatalf("evidence state hid overdue timeline: %+v", copy)
			}
			wantState := state
			if state == "current" {
				wantState = "overdue"
			}
			if copy.State != wantState {
				t.Fatalf("timeline overwrote evidence state: got=%s want=%s", copy.State, wantState)
			}
			var overdue *OperationalAlert
			s.SetOperationalNotifier(func(_ context.Context, alert OperationalAlert) error {
				if alert.Event == "backup_copy_overdue" {
					overdue = &alert
				}
				return nil
			})
			s.observeProtection(context.Background())
			if overdue == nil || !overdue.DueAt.Equal(*copy.FirstOutstandingDue) {
				t.Fatalf("overview and observer disagree: copy=%+v alert=%+v", copy, overdue)
			}
		})
	}
}

func TestOverdueTimelineRespectsGraceAndDisabledSchedules(t *testing.T) {
	s, _, _ := newTestService(t)
	now := parseTestTime(t, "2026-10-14T10:30:00Z")
	s.now = func() time.Time { return now }
	d := addDestination(t, s, "Grace window")
	p := addPlan(t, s, d)
	activated := parseTestTime(t, "2026-10-14T09:00:00Z")
	p.Destinations[0].Schedule = Schedule{Frequency: "daily", Time: "10:00", Timezone: "UTC"}
	p.Destinations[0].ScheduleActivatedAt = &activated
	s.state.Plans[p.ID] = p
	copy := protectionFor(t, s).Copies[0]
	if copy.FirstOutstandingDue == nil || copy.OverdueSince != nil {
		t.Fatalf("grace window not represented: %+v", copy)
	}
	now = parseTestTime(t, "2026-10-14T11:00:00Z")
	if copy = protectionFor(t, s).Copies[0]; copy.OverdueSince == nil || !copy.OverdueSince.Equal(now) {
		t.Fatalf("grace boundary absent: %+v", copy)
	}
	for _, disabled := range []string{"plan", "assignment", "manual"} {
		candidate := clone(p)
		switch disabled {
		case "plan":
			candidate.Enabled = false
		case "assignment":
			candidate.Destinations[0].Enabled = false
		case "manual":
			candidate.Destinations[0].Schedule = Schedule{Frequency: "manual"}
		}
		s.state.Plans[p.ID] = candidate
		if copy = protectionFor(t, s).Copies[0]; copy.FirstOutstandingDue != nil || copy.OverdueSince != nil {
			t.Fatalf("%s schedule shows an active deadline: %+v", disabled, copy)
		}
	}
}
