package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"stacklab/internal/store"
)

func (s *Service) executeCopy(ctx context.Context, job store.Job, run Run, plan Plan, stage string, copy CopyResult, update func(CopyResult, string) error) CopyResult {
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
	if err == nil {
		err = update(copy, "retention")
		if err == nil {
			if s.SchedulingPaused() {
				copy.Retention = "skipped"
				s.event(ctx, job, "Retention skipped while automatic backup operations are paused.")
			} else {
				for _, assignment := range plan.Destinations {
					if assignment.DestinationID == d.ID {
						err = s.retain(ctx, d, plan.ID, assignment.Retention)
						break
					}
				}
				copy.Retention = "succeeded"
				if err != nil {
					copy.Retention = "failed"
				}
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
	return copy
}
