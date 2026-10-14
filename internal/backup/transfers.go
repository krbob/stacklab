package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"stacklab/internal/store"
)

func (s *Service) executeCopy(ctx context.Context, job store.Job, run Run, plan Plan, stage string, copy CopyResult, update func(CopyResult, string) error) CopyResult {
	if verifiedCopy(copy) {
		return s.executeRetention(ctx, job, plan, copy, update)
	}
	started := time.Now()
	copy.State, copy.Error = "running", ""
	copy.StartedAt, copy.CompletedAt = s.timePointer(), nil
	s.mu.Lock()
	d, exists := s.state.Destinations[copy.DestinationID]
	s.mu.Unlock()
	var err error
	if !exists {
		err = ErrNotFound
	} else {
		err = update(copy, "upload")
	}
	if err == nil && copy.SnapshotID == "" {
		s.event(ctx, job, "Uploading recovery set to "+d.Name+".")
		var lastProgressEvent time.Time
		uploadCtx := withTransferProgress(ctx, func(progress transferProgress) {
			copy.BytesProcessed, copy.BytesTotal = progress.BytesProcessed, progress.BytesTotal
			copy.FilesProcessed, copy.FilesTotal = progress.FilesProcessed, progress.FilesTotal
			copy.BytesAdded = progress.BytesAdded
			_ = update(copy, "progress")
			if !lastProgressEvent.IsZero() && time.Since(lastProgressEvent) < 5*time.Second {
				return
			}
			lastProgressEvent = time.Now()
			_ = s.jobs.PublishEventWithProgress(ctx, job, "job_progress", "Uploading recovery set to "+d.Name+".", "", nil, &store.JobProgress{Phase: "backup_upload", Completed: int(progress.BytesProcessed), Total: int(progress.BytesTotal), Unit: "bytes", Detail: d.Name})
		})
		copy.SnapshotID, err = s.upload(uploadCtx, d, plan, run, stage)
		if err == nil {
			copy.UploadedAt = s.timePointer()
		}
	}
	if err == nil {
		err = update(copy, "verify")
		if err == nil {
			err = s.verifyRecoveryObjects(ctx, d, copy.SnapshotID, stage)
		}
		if err == nil {
			copy.Verification, copy.VerifiedAt = recoveryObjectsVerified, s.timePointer()
		} else {
			copy.Verification, copy.VerifiedAt = "unavailable", nil
			if errors.Is(err, errIncompleteSnapshot) {
				copy.Verification, copy.SnapshotID = "failed", ""
			}
		}
	}
	copy.CompletedAt = s.timePointer()
	copy.DurationSeconds += time.Since(started).Seconds()
	copy.State = "succeeded"
	if err != nil {
		copy.State, copy.Error = "failed", err.Error()
	} else {
		s.mu.Lock()
		current := s.state.Destinations[d.ID]
		current.LastSuccessAt, current.Status, current.Error = copy.UploadedAt, "available", ""
		s.state.Destinations[d.ID] = current
		s.mu.Unlock()
	}
	if persistErr := update(copy, "upload"); persistErr != nil {
		copy.State, copy.Error = "failed", fmt.Sprintf("backup state persistence failed: %v", persistErr)
	}
	if verifiedCopy(copy) {
		return s.executeRetention(ctx, job, plan, copy, update)
	}
	return copy
}

// Verified transfer evidence survives maintenance failures and retries. It is
// sufficient even after the bounded snapshot-classification cache evicts it.
func verifiedCopy(copy CopyResult) bool {
	return copy.State == "succeeded" && copy.Verification == recoveryObjectsVerified && copy.VerifiedAt != nil && copy.SnapshotID != ""
}

func copyWorkPending(copy CopyResult) bool {
	return copy.State == "pending" || verifiedCopy(copy) && copy.Retention == "pending"
}

func runNeedsStaging(run Run) bool {
	for _, copy := range run.Copies {
		if copy.State != "succeeded" {
			return true
		}
	}
	return false
}

func (s *Service) executeRetention(ctx context.Context, job store.Job, plan Plan, copy CopyResult, update func(CopyResult, string) error) CopyResult {
	// Automatic maintenance follows the pause policy even when the operator
	// retries it explicitly. Skipping cannot resolve an earlier failure.
	if s.SchedulingPaused() {
		copy.Retention = "skipped"
		if copy.RetentionError != "" {
			copy.Retention = "failed"
		}
		s.event(ctx, job, "Retention skipped while automatic backup operations are paused.")
	} else {
		copy.Retention, copy.RetentionStartedAt, copy.RetentionCompletedAt = "running", s.timePointer(), nil
		copy.RetentionError = ""
		s.event(ctx, job, "Using the verified recovery copy; running retention cleanup.")
		err := update(copy, "retention")
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			s.mu.Lock()
			d, exists := s.state.Destinations[copy.DestinationID]
			// The immutable verification result on this run is durable proof
			// even if its classification was evicted from the bounded cache.
			if _, known := s.snapshotClassificationLocked(copy.DestinationID, copy.SnapshotID); !known {
				s.rememberSnapshotClassificationLocked(copy.DestinationID, copy.SnapshotID, "complete", *copy.VerifiedAt)
			}
			s.mu.Unlock()
			err = ErrNotFound
			if exists {
				for _, assignment := range plan.Destinations {
					if assignment.DestinationID == copy.DestinationID {
						err = s.retain(ctx, d, plan.ID, assignment.Retention)
						break
					}
				}
			}
		}
		if err == nil {
			err = ctx.Err()
		}
		copy.Retention, copy.RetentionError = "succeeded", ""
		copy.RetentionCompletedAt = s.timePointer()
		if err != nil {
			copy.Retention, copy.RetentionError = "failed", err.Error()
			s.event(ctx, job, "Recovery objects remain verified; retention requires attention: "+copy.RetentionError)
		} else {
			s.event(ctx, job, "Retention cleanup completed.")
		}
	}
	if err := update(copy, "retention"); err != nil {
		copy.Retention, copy.RetentionError = "failed", fmt.Sprintf("backup state persistence failed: %v", err)
	}
	return copy
}
