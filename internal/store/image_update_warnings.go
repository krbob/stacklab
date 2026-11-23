package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ImageUpdateWarning identifies running images left behind by a maintenance
// attempt. Different Compose services can share an image reference.
type ImageUpdateWarning struct {
	StackID               string
	ProjectName           string
	ServiceName           string
	ImageRef              string
	DefinitionFingerprint string
	PreviousImageIDs      []string
	CandidateImageID      string
	CandidateRemoteDigest string
	Outcome               string
	JobID                 string
	AttemptedAt           time.Time
}

func (w ImageUpdateWarning) validate(stackID string) error {
	if (w.StackID != "" && w.StackID != stackID) || w.ServiceName == "" || w.JobID == "" || w.AttemptedAt.IsZero() {
		return errors.New("incomplete or inconsistent image update warning")
	}
	if len(w.PreviousImageIDs) == 0 || (w.CandidateImageID == "" && w.CandidateRemoteDigest == "") {
		return errors.New("image update warning requires previous images and a known candidate")
	}
	seen := make(map[string]bool, len(w.PreviousImageIDs))
	for _, id := range w.PreviousImageIDs {
		if id == "" || seen[id] || id == w.CandidateImageID {
			return errors.New("previous image IDs must be nonempty, unique and distinct from the candidate")
		}
		seen[id] = true
	}
	return nil
}

// UpdateImageWarnings saves the outcomes for a stack while maintenance holds
// its resource lease. Only explicitly resolved services lose an earlier warning.
func (s *Store) UpdateImageWarnings(ctx context.Context, stackID string, warnings []ImageUpdateWarning, clearServices []string) error {
	if len(warnings) == 0 && len(clearServices) == 0 {
		return nil
	}
	if stackID == "" {
		return errors.New("image update warnings require a stack")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range clearServices {
		if _, err := tx.ExecContext(ctx, `DELETE FROM image_update_warnings WHERE stack_id = ? AND service_name = ?`, stackID, name); err != nil {
			return fmt.Errorf("clear image update warning: %w", err)
		}
	}
	for _, w := range warnings {
		if err := w.validate(stackID); err != nil {
			return err
		}
		imageIDs, err := json.Marshal(w.PreviousImageIDs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO image_update_warnings
			(stack_id, project_name, service_name, image_ref, definition_fingerprint, previous_image_ids, candidate_image_id, candidate_remote_digest, outcome, job_id, attempted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(stack_id, service_name) DO UPDATE SET
			  project_name = excluded.project_name, image_ref = excluded.image_ref,
			  definition_fingerprint = excluded.definition_fingerprint, previous_image_ids = excluded.previous_image_ids,
			  candidate_image_id = excluded.candidate_image_id, candidate_remote_digest = excluded.candidate_remote_digest, outcome = excluded.outcome,
			  job_id = excluded.job_id, attempted_at = excluded.attempted_at`,
			stackID, w.ProjectName, w.ServiceName, w.ImageRef, w.DefinitionFingerprint, string(imageIDs), w.CandidateImageID, w.CandidateRemoteDigest, w.Outcome, w.JobID, w.AttemptedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("save image update warning: %w", err)
		}
	}
	return tx.Commit()
}

// ListImageWarnings reads one indexed job lookup per warning in a single
// query. The warning outlives job-history retention, so the link is optional.
func (s *Store) ListImageWarnings(ctx context.Context) (map[string][]ImageUpdateWarning, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.stack_id, w.project_name, w.service_name, w.image_ref, w.definition_fingerprint,
		  w.previous_image_ids, w.candidate_image_id, w.candidate_remote_digest, w.outcome, COALESCE(j.id, ''), w.attempted_at
		FROM image_update_warnings w LEFT JOIN jobs j ON j.id = w.job_id
		ORDER BY w.stack_id, w.service_name`)
	if err != nil {
		return nil, fmt.Errorf("list image update warnings: %w", err)
	}
	defer rows.Close()
	result := map[string][]ImageUpdateWarning{}
	for rows.Next() {
		var w ImageUpdateWarning
		var imageIDs []byte
		var attemptedAt string
		if err := rows.Scan(&w.StackID, &w.ProjectName, &w.ServiceName, &w.ImageRef, &w.DefinitionFingerprint, &imageIDs, &w.CandidateImageID, &w.CandidateRemoteDigest, &w.Outcome, &w.JobID, &attemptedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(imageIDs, &w.PreviousImageIDs); err != nil {
			return nil, fmt.Errorf("decode previous image IDs: %w", err)
		}
		w.AttemptedAt, err = time.Parse(time.RFC3339Nano, attemptedAt)
		if err != nil {
			return nil, fmt.Errorf("parse image update attempt time: %w", err)
		}
		result[w.StackID] = append(result[w.StackID], w)
	}
	return result, rows.Err()
}
