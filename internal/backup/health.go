package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Protection describes saved data separately from configuration and repository
// availability. A retry never makes the original capture newer.
type PlanProtection struct {
	Activity       string           `json:"activity"`
	PlanID         string           `json:"plan_id"`
	PlanName       string           `json:"plan_name"`
	State          string           `json:"state"`
	ExpectedCopies int              `json:"expected_copies"`
	CurrentCopies  int              `json:"current_copies"`
	LastCaptureAt  *time.Time       `json:"last_capture_at,omitempty"`
	NextRunAt      *time.Time       `json:"next_run_at,omitempty"`
	LatestRunID    string           `json:"latest_run_id,omitempty"`
	Copies         []CopyProtection `json:"copies"`
}

type CopyProtection struct {
	Activity           string     `json:"activity"`
	DestinationID      string     `json:"destination_id"`
	State              string     `json:"state"`
	LastCaptureAt      *time.Time `json:"last_capture_at,omitempty"`
	LastUploadedAt     *time.Time `json:"last_uploaded_at,omitempty"`
	SnapshotID         string     `json:"snapshot_id,omitempty"`
	NextRunAt          *time.Time `json:"next_run_at,omitempty"`
	DueAt              *time.Time `json:"due_at,omitempty"`
	LastRunID          string     `json:"last_run_id,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	EvidenceUsable     bool       `json:"evidence_usable"`
	SnapshotStatus     string     `json:"snapshot_status"`
	SnapshotCheckedAt  *time.Time `json:"snapshot_checked_at,omitempty"`
	IntegrityStatus    string     `json:"integrity_status"`
	AvailabilityStatus string     `json:"availability_status"`
	LastRestoreTestAt  *time.Time `json:"last_restore_test_at,omitempty"`
}

type copyCheckpoint struct {
	CapturedAt        time.Time  `json:"captured_at"`
	UploadedAt        *time.Time `json:"uploaded_at,omitempty"`
	SnapshotID        string     `json:"snapshot_id"`
	RunID             string     `json:"run_id"`
	Selection         string     `json:"selection"`
	Verification      string     `json:"verification,omitempty"`
	SnapshotStatus    string     `json:"snapshot_status,omitempty"`
	SnapshotCheckedAt *time.Time `json:"snapshot_checked_at,omitempty"`
}

type Preflight struct {
	Sources  []ManifestSource `json:"sources"`
	Writers  []Writer         `json:"writers"`
	Warnings []string         `json:"warnings"`
}

func selectionFingerprint(plan Plan) string {
	sources := append([]Source(nil), plan.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	data, _ := json.Marshal(struct {
		Consistency string
		Sources     []Source
	}{plan.Consistency, sources})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func checkpointKey(planID, destinationID string) string { return planID + "/" + destinationID }

func (s *Service) recordCheckpointsLocked(run Run, plan Plan) {
	if run.CapturedAt == nil {
		return
	}
	if s.state.CopyCheckpoints == nil {
		s.state.CopyCheckpoints = map[string]copyCheckpoint{}
	}
	for _, copy := range run.Copies {
		if _, exists := s.state.Plans[run.PlanID]; !exists {
			continue
		}
		if _, exists := s.state.Destinations[copy.DestinationID]; !exists {
			continue
		}
		if copy.SnapshotID == "" || copy.Verification != recoveryObjectsVerified && copy.Verification != "snapshot_metadata_verified" {
			continue
		}
		key := checkpointKey(run.PlanID, copy.DestinationID)
		old, ok := s.state.CopyCheckpoints[key]
		oldFuture := old.CapturedAt.After(s.now().Add(clockSkewTolerance))
		candidateFuture := run.CapturedAt.After(s.now().Add(clockSkewTolerance))
		if ok && (!oldFuture && candidateFuture || oldFuture == candidateFuture && old.CapturedAt.After(*run.CapturedAt)) {
			continue
		}
		if ok && old.SnapshotID == copy.SnapshotID && old.RunID == run.ID && (copy.VerifiedAt == nil || old.SnapshotCheckedAt != nil && !copy.VerifiedAt.After(*old.SnapshotCheckedAt)) {
			continue
		}
		checkpoint := copyCheckpoint{CapturedAt: *run.CapturedAt, UploadedAt: copy.UploadedAt, SnapshotID: copy.SnapshotID, RunID: run.ID, Selection: selectionFingerprint(plan), Verification: copy.Verification, SnapshotStatus: "unknown"}
		if copy.Verification == recoveryObjectsVerified && copy.VerifiedAt != nil {
			checkpoint.SnapshotStatus = "present"
			checkpoint.SnapshotCheckedAt = copy.VerifiedAt
		}
		s.state.CopyCheckpoints[key] = checkpoint
		if copy.Verification == recoveryObjectsVerified {
			rememberSnapshotEvidence(&s.state.VerifiedSnapshots, snapshotEvidenceKey(copy.DestinationID, copy.SnapshotID), s.now())
		}
	}
}

func (s *Service) protectionLocked(plan Plan) PlanProtection {
	now := s.now()
	result := PlanProtection{PlanID: plan.ID, PlanName: plan.Name, State: "healthy", Activity: "idle", Copies: []CopyProtection{}}
	var latest *persistedRun
	for _, run := range s.state.Runs {
		if run.PlanID == plan.ID {
			if run.State == "running" || run.State == "queued" && result.Activity == "idle" {
				result.Activity = run.State
			}
		}
		if run.PlanID == plan.ID && (latest == nil || newerRun(run.Run, latest.Run, now)) {
			item := run
			latest = &item
		}
	}
	if latest != nil {
		result.LatestRunID = latest.ID
	}
	allManual := true
	attention := false
	for _, assignment := range plan.Destinations {
		destination := s.state.Destinations[assignment.DestinationID]
		copy := CopyProtection{DestinationID: assignment.DestinationID, State: "unprotected", Activity: "idle", SnapshotStatus: "unknown", IntegrityStatus: destination.CheckStatus, AvailabilityStatus: destination.Status}
		if copy.IntegrityStatus == "" {
			copy.IntegrityStatus = "unknown"
		}
		if copy.AvailabilityStatus == "" {
			copy.AvailabilityStatus = "unknown"
		}
		checkpoint, saved := s.state.CopyCheckpoints[checkpointKey(plan.ID, assignment.DestinationID)]
		if saved {
			copy.LastCaptureAt = &checkpoint.CapturedAt
			copy.LastUploadedAt = checkpoint.UploadedAt
			copy.SnapshotID = checkpoint.SnapshotID
			copy.LastRunID = checkpoint.RunID
			copy.SnapshotStatus, copy.SnapshotCheckedAt = checkpoint.SnapshotStatus, checkpoint.SnapshotCheckedAt
			if copy.SnapshotStatus == "" {
				copy.SnapshotStatus = "unknown"
			}
			copy.LastRestoreTestAt = s.restoreTestedAtLocked(destination, checkpoint.SnapshotID)
			if result.LastCaptureAt == nil || result.LastCaptureAt.Before(checkpoint.CapturedAt) {
				t := checkpoint.CapturedAt
				result.LastCaptureAt = &t
			}
			copy.State = "current"
		}
		if assignment.Schedule.Frequency == "manual" || assignment.Schedule.Frequency == "" {
			if saved {
				copy.State = "manual"
			}
		} else {
			if assignment.Enabled {
				allManual = false
			}
			if due, ok := latestDue(assignment.Schedule, now); ok {
				copy.DueAt = &due
				if saved && checkpoint.CapturedAt.Before(due) {
					copy.State = "overdue"
				}
			}
			if next, err := PreviewSchedule(assignment.Schedule, now, 1); err == nil && len(next) > 0 {
				copy.NextRunAt = &next[0]
				if plan.Enabled && assignment.Enabled && (result.NextRunAt == nil || result.NextRunAt.After(next[0])) {
					t := next[0]
					result.NextRunAt = &t
				}
			}
		}
		if saved && checkpoint.Selection != selectionFingerprint(plan) {
			copy.State = "outdated"
		}
		if saved {
			switch {
			case checkpoint.CapturedAt.After(now.Add(clockSkewTolerance)) || assignment.LastScheduledAt != nil && assignment.LastScheduledAt.After(now.Add(clockSkewTolerance)):
				copy.State, copy.LastError = "clock_skew", "The host clock moved backwards; this recovery point cannot establish current protection."
			case checkpoint.SnapshotStatus == "missing":
				copy.State = "missing"
			case destination.CheckStatus == "failed":
				copy.State, copy.LastError = "integrity_failed", destination.CheckError
			case destination.Status == "error":
				copy.State, copy.LastError = "unavailable", destination.Error
			case destination.InventoryError != "":
				copy.State, copy.LastError = "unverified", destination.InventoryError
			case checkpoint.Verification != recoveryObjectsVerified || checkpoint.SnapshotCheckedAt == nil:
				copy.State = "unverified"
			case checkpoint.SnapshotCheckedAt.After(now.Add(clockSkewTolerance)):
				copy.State = "clock_skew"
			case now.Sub(*checkpoint.SnapshotCheckedAt) > 24*time.Hour:
				copy.State = "unverified"
			}
		}
		// Latest attempt is useful diagnostic evidence even while an older verified
		// copy is still within its expected schedule.
		var latestCopyRun *persistedRun
		var latestAttempt CopyResult
		for _, run := range s.state.Runs {
			if run.PlanID != plan.ID {
				continue
			}
			for _, attempt := range run.Copies {
				if attempt.DestinationID != assignment.DestinationID {
					continue
				}
				// A retry keeps its original capture time. Activity therefore scans
				// every active attempt independently from newest protection evidence.
				if run.State == "running" || run.State == "queued" {
					if attempt.State == "running" || run.CaptureState == "running" {
						copy.Activity = "running"
					} else if attempt.State == "pending" && copy.Activity == "idle" {
						copy.Activity = "queued"
					}
				}
				if latestCopyRun == nil || newerRun(run.Run, latestCopyRun.Run, now) {
					item := run
					latestCopyRun = &item
					latestAttempt = attempt
				}
			}
		}
		copy.EvidenceUsable = saved && copy.SnapshotStatus == "present" && (copy.State == "current" || copy.State == "manual" || copy.State == "overdue")
		usable := copy.EvidenceUsable && (copy.State == "current" || copy.State == "manual")
		if latestCopyRun != nil {
			copy.LastRunID = latestCopyRun.ID
			if copy.LastError == "" {
				copy.LastError = latestAttempt.Error
				if latestCopyRun.CaptureState == "failed" {
					copy.LastError = latestCopyRun.Error
				}
			}
		}
		if !plan.Enabled || !assignment.Enabled {
			copy.State = "disabled"
			copy.NextRunAt = nil
			copy.DueAt = nil
		} else {
			result.ExpectedCopies++
			if usable {
				result.CurrentCopies++
			}
			if copy.State != "current" && copy.State != "manual" || copy.LastError != "" {
				attention = true
			}
		}
		result.Copies = append(result.Copies, copy)
	}
	if !plan.Enabled || result.ExpectedCopies == 0 {
		result.State = "disabled"
		result.NextRunAt = nil
	} else if result.CurrentCopies == 0 && result.LastCaptureAt == nil {
		result.State = "unprotected"
	} else if attention {
		result.State = "attention"
	} else if allManual {
		result.State = "manual"
	}
	return result
}

func (s *Service) Preflight(ctx context.Context, planID string) (Preflight, error) {
	s.mu.Lock()
	plan, ok := s.state.Plans[planID]
	s.mu.Unlock()
	if !ok {
		return Preflight{}, ErrNotFound
	}
	previewer, ok := s.capture.(interface {
		Preview(context.Context, Plan) ([]ManifestSource, error)
	})
	if !ok {
		return Preflight{}, fmt.Errorf("capture preview unavailable")
	}
	sources, err := previewer.Preview(ctx, plan)
	if err != nil {
		return Preflight{}, err
	}
	s.mu.Lock()
	err = s.validateSourceRepositories(sources)
	s.mu.Unlock()
	if err != nil {
		return Preflight{}, err
	}
	result := Preflight{Sources: sources, Writers: []Writer{}, Warnings: []string{}}
	if plan.Consistency == "stop" {
		result.Writers = uniqueRunningWriters(sources)
	} else {
		result.Warnings = append(result.Warnings, "Live capture does not guarantee database consistency; use prepared dumps or stop writers.")
	}
	return result, nil
}

func newerRun(candidate, previous Run, now time.Time) bool {
	candidateFuture := candidate.CreatedAt.After(now.Add(clockSkewTolerance))
	previousFuture := previous.CreatedAt.After(now.Add(clockSkewTolerance))
	if candidateFuture != previousFuture {
		return !candidateFuture
	}
	if candidate.CreatedAt.Equal(previous.CreatedAt) {
		return candidate.ID > previous.ID
	}
	return candidate.CreatedAt.After(previous.CreatedAt)
}
