package httpapi

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"stacklab/internal/backup"
)

func (c *backupController) policy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, c.backups.Policy())
}
func (c *backupController) savePolicy(w http.ResponseWriter, r *http.Request) {
	var input backup.PolicyInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	result, err := c.backups.SavePolicy(r.Context(), input)
	if err != nil {
		c.fail(w, err)
		return
	}
	c.record(r, "backup_policy_save", "")
	writeJSON(w, http.StatusOK, result)
}
func (c *backupController) deleteRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("restoreId")
	if err := c.backups.DeleteRestore(r.Context(), id); err != nil {
		c.fail(w, err)
		return
	}
	c.record(r, "backup_restore_delete", id)
	w.WriteHeader(http.StatusNoContent)
}
func (c *backupController) downloadRestoredArchive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	file, err := c.backups.OpenRestoredArchive(ctx, r.PathValue("restoreId"), r.URL.Query().Get("source_id"))
	if err != nil {
		c.fail(w, err)
		return
	}
	defer file.Reader.Close()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.CopyN(w, file.Reader, file.SizeBytes)
}

func (c *backupController) recoveryJournals(w http.ResponseWriter, r *http.Request) {
	items, err := c.backupCapture.RecoveryJournals(r.Context())
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (c *backupController) resolveRecovery(w http.ResponseWriter, r *http.Request) {
	var input backup.RecoveryResolution
	if err := decodeJSON(w, r, &input); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	runID := r.PathValue("runId")
	job, err := c.backups.StartRecoveryResolution(r.Context(), runID, input)
	if err != nil {
		c.fail(w, err)
		return
	}
	now := time.Now().UTC()
	_ = c.audit.RecordSystemEvent(r.Context(), "backup_recovery_resolution", "local", "accepted", now, nil, map[string]any{"run_id": runID, "job_id": job.ID, "action": input.Action, "inspected": input.Inspected, "reason": input.Reason})
	c.startWorker(func() { c.backups.ExecuteRecoveryResolution(c.appContext(), job, runID, input) })
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}
