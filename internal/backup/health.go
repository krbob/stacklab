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
	DestinationID  string     `json:"destination_id"`
	State          string     `json:"state"`
	LastCaptureAt  *time.Time `json:"last_capture_at,omitempty"`
	LastUploadedAt *time.Time `json:"last_uploaded_at,omitempty"`
	SnapshotID     string     `json:"snapshot_id,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	LastRunID      string     `json:"last_run_id,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type copyCheckpoint struct {
	CapturedAt time.Time  `json:"captured_at"`
	UploadedAt *time.Time `json:"uploaded_at,omitempty"`
	SnapshotID string     `json:"snapshot_id"`
	RunID      string     `json:"run_id"`
	Selection  string     `json:"selection"`
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
		if copy.SnapshotID == "" || copy.Verification != "snapshot_metadata_verified" {
			continue
		}
		key := checkpointKey(run.PlanID, copy.DestinationID)
		old, ok := s.state.CopyCheckpoints[key]
		if ok && old.CapturedAt.After(*run.CapturedAt) {
			continue
		}
		s.state.CopyCheckpoints[key] = copyCheckpoint{CapturedAt: *run.CapturedAt, UploadedAt: copy.UploadedAt, SnapshotID: copy.SnapshotID, RunID: run.ID, Selection: selectionFingerprint(plan)}
	}
}

func (s *Service) protectionLocked(plan Plan) PlanProtection {
	now := s.now()
	result := PlanProtection{PlanID: plan.ID, PlanName: plan.Name, State: "healthy", Copies: []CopyProtection{}}
	var latest *persistedRun
	for _, run := range s.state.Runs {
		if run.PlanID == plan.ID && (latest == nil || run.CreatedAt.After(latest.CreatedAt)) {
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
		copy := CopyProtection{DestinationID: assignment.DestinationID, State: "unprotected"}
		checkpoint, saved := s.state.CopyCheckpoints[checkpointKey(plan.ID, assignment.DestinationID)]
		if saved {
			copy.LastCaptureAt = &checkpoint.CapturedAt
			copy.LastUploadedAt = checkpoint.UploadedAt
			copy.SnapshotID = checkpoint.SnapshotID
			copy.LastRunID = checkpoint.RunID
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
		// Latest attempt is useful diagnostic evidence even while an older verified
		// copy is still within its expected schedule.
		var latestCopyRun *persistedRun
		for _, run := range s.state.Runs {
			if run.PlanID != plan.ID {
				continue
			}
			for _, attempt := range run.Copies {
				if attempt.DestinationID == assignment.DestinationID && (latestCopyRun == nil || run.CreatedAt.After(latestCopyRun.CreatedAt)) {
					item := run
					latestCopyRun = &item
					copy.LastError = attempt.Error
					if run.CaptureState == "failed" {
						copy.LastError = run.Error
					}
				}
			}
		}
		usable := copy.State == "current" || copy.State == "manual"
		if latestCopyRun != nil {
			copy.LastRunID = latestCopyRun.ID
		}
		if latestCopyRun != nil && (latestCopyRun.State == "running" || latestCopyRun.State == "queued") {
			copy.State = "running"
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
	} else if latest != nil && (latest.State == "running" || latest.State == "queued") {
		result.State = "running"
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
