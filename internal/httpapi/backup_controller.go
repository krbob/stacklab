package httpapi

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"stacklab/internal/auth"
	"stacklab/internal/backup"
	"stacklab/internal/jobs"
	"stacklab/internal/store"
)

type backupController struct{ *Handler }

func (c *backupController) registerRoutes(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		"GET /api/backups/recovery":                                                    c.recoveryJournals,
		"POST /api/backups/recovery/{runId}":                                           c.resolveRecovery,
		"GET /api/backups/policy":                                                      c.policy,
		"PUT /api/backups/policy":                                                      c.savePolicy,
		"DELETE /api/backups/restores/{restoreId}":                                     c.deleteRestore,
		"GET /api/backups/restores/{restoreId}/archive":                                c.downloadRestoredArchive,
		"GET /api/backups/coverage":                                                    c.coverage,
		"POST /api/backups/schedule-preview":                                           c.schedulePreview,
		"GET /api/backups/plans/{planId}/preflight":                                    c.preflight,
		"POST /api/backups/destinations/{destinationId}/prune":                         c.repositoryJob,
		"POST /api/backups/destinations/{destinationId}/unlock":                        c.repositoryJob,
		"GET /api/backups/destinations/{destinationId}/snapshots/{snapshotId}/files":   c.snapshotCatalog,
		"GET /api/backups/destinations/{destinationId}/snapshots/{snapshotId}/compare": c.compareSnapshots,
		"GET /api/backups/restores":                                                    c.restoreLibrary,
		"GET /api/backups/restores/{restoreId}/files":                                  c.restoredCatalog,
		"GET /api/backups/restores/{restoreId}/download":                               c.downloadRestoredFile,
		"GET /api/backups/restore-manifest":                                            c.restoreManifest,
		"POST /api/backups/destinations/{destinationId}/restore-preview":               c.restorePreview,
		"GET /api/backups/filesystem":                                                  c.filesystem,
		"GET /api/backups":                                                             c.overview,
		"GET /api/backups/sources":                                                     c.sources,
		"GET /api/stacks/{stackId}/backups":                                            c.stackCoverage,
		"POST /api/backups/destinations":                                               c.saveDestination,
		"PUT /api/backups/destinations/{destinationId}":                                c.saveDestination,
		"DELETE /api/backups/destinations/{destinationId}":                             c.deleteDestination,
		"GET /api/backups/destinations/{destinationId}/snapshots":                      c.snapshots,
		"POST /api/backups/destinations/{destinationId}/test":                          c.repositoryJob,
		"POST /api/backups/destinations/{destinationId}/initialize":                    c.repositoryJob,
		"POST /api/backups/destinations/{destinationId}/check":                         c.repositoryJob,
		"POST /api/backups/destinations/{destinationId}/restore":                       c.repositoryJob,
		"POST /api/backups/plans":                                                      c.savePlan,
		"PUT /api/backups/plans/{planId}":                                              c.savePlan,
		"DELETE /api/backups/plans/{planId}":                                           c.deletePlan,
		"POST /api/backups/plans/{planId}/run":                                         c.run,
		"POST /api/backups/runs/{runId}/retry":                                         c.run,
	}
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, c.withAuth(c.guard(handler)))
	}
}

func (c *backupController) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && !auth.SameOrigin(r) {
			writeError(w, 403, "forbidden", "Cross-origin request rejected.", nil)
			return
		}
		if c.backups == nil || c.backupCapture == nil {
			writeError(w, 503, "backup_unavailable", "Backup service is unavailable.", nil)
			return
		}
		next(w, r)
	}
}
func (c *backupController) fail(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "backup_failed"
	switch {
	case errors.Is(err, backup.ErrInvalid):
		status, code = 400, "validation_failed"
	case errors.Is(err, backup.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, backup.ErrBusy), errors.Is(err, jobs.ErrResourceConflict), errors.Is(err, backup.ErrExpired):
		status, code = 409, "conflict"
	}
	message := err.Error()
	if status == 500 {
		message = "Backup operation failed; check service dependencies, repository access, and job details."
	}
	writeError(w, status, code, message, nil)
}
func (c *backupController) overview(w http.ResponseWriter, r *http.Request) {
	v, e := c.backups.Overview(r.Context())
	if e != nil {
		c.fail(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (c *backupController) sources(w http.ResponseWriter, r *http.Request) {
	v, e := c.backupCapture.Discover(r.Context(), r.URL.Query().Get("stack_id"))
	if e != nil {
		c.fail(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": v})
}
func (c *backupController) stackCoverage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("stackId")
	sources, e := c.backupCapture.Discover(r.Context(), id)
	if e != nil {
		c.fail(w, e)
		return
	}
	overview, e := c.backups.Overview(r.Context())
	if e != nil {
		c.fail(w, e)
		return
	}
	plans := []backup.Plan{}
	for _, p := range overview.Plans {
		for _, s := range p.Sources {
			if s.StackID == id {
				plans = append(plans, p)
				break
			}
		}
	}
	writeJSON(w, 200, map[string]any{"sources": sources, "plans": plans})
}
func (c *backupController) record(r *http.Request, action, id string) {
	now := time.Now().UTC()
	_ = c.audit.RecordSystemEvent(r.Context(), action, "local", "succeeded", now, &now, map[string]any{"backup_id": id})
}
func (c *backupController) saveDestination(w http.ResponseWriter, r *http.Request) {
	var input backup.DestinationInput
	if e := decodeJSON(w, r, &input); e != nil {
		writeDecodeJSONError(w, e)
		return
	}
	v, e := c.backups.SaveDestination(r.Context(), r.PathValue("destinationId"), input)
	if e != nil {
		c.fail(w, e)
		return
	}
	c.record(r, "backup_destination_save", v.ID)
	writeJSON(w, 200, map[string]any{"destination": v})
}
func (c *backupController) deleteDestination(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("destinationId")
	if e := c.backups.DeleteDestination(r.Context(), id); e != nil {
		c.fail(w, e)
		return
	}
	c.record(r, "backup_destination_delete", id)
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
func (c *backupController) savePlan(w http.ResponseWriter, r *http.Request) {
	var input backup.Plan
	if e := decodeJSON(w, r, &input); e != nil {
		writeDecodeJSONError(w, e)
		return
	}
	v, e := c.backups.SavePlan(r.Context(), r.PathValue("planId"), input)
	if e != nil {
		c.fail(w, e)
		return
	}
	c.record(r, "backup_plan_save", v.ID)
	writeJSON(w, 200, map[string]any{"plan": v})
}
func (c *backupController) deletePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("planId")
	if e := c.backups.DeletePlan(r.Context(), id); e != nil {
		c.fail(w, e)
		return
	}
	c.record(r, "backup_plan_delete", id)
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
func (c *backupController) snapshots(w http.ResponseWriter, r *http.Request) {
	v, e := c.backups.Snapshots(r.Context(), r.PathValue("destinationId"))
	if e != nil {
		c.fail(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": v})
}
func (c *backupController) run(w http.ResponseWriter, r *http.Request) {
	var input backup.RunRequest
	if e := decodeJSON(w, r, &input); e != nil {
		writeDecodeJSONError(w, e)
		return
	}
	var job store.Job
	var run backup.Run
	var err error
	if id := r.PathValue("runId"); id != "" {
		job, run, err = c.backups.StartRetry(r.Context(), id, input, "local")
	} else {
		job, run, err = c.backups.StartRun(r.Context(), r.PathValue("planId"), input, "local")
	}
	if err != nil {
		c.fail(w, err)
		return
	}
	c.startWorker(func() { c.backups.ExecuteRun(c.appContext(), job, run.ID) })
	writeJSON(w, 202, map[string]any{"job": job, "run": run})
}
func (c *backupController) repositoryJob(w http.ResponseWriter, r *http.Request) {
	var check backup.CheckRequest
	var prune backup.PruneRequest
	var restore backup.RestoreRequest
	action := "backup_destination_test"
	switch r.Pattern {
	case "POST /api/backups/destinations/{destinationId}/prune":
		action = "backup_prune"
		if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
			if e := decodeJSON(w, r, &prune); e != nil {
				writeDecodeJSONError(w, e)
				return
			}
		}
	case "POST /api/backups/destinations/{destinationId}/unlock":
		action = "backup_unlock"
		// No force-unlock flag is accepted, including from non-UI clients.
		if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
			if e := decodeJSON(w, r, &struct{}{}); e != nil {
				writeDecodeJSONError(w, e)
				return
			}
		}
	case "POST /api/backups/destinations/{destinationId}/initialize":
		action = "backup_repository_init"
	case "POST /api/backups/destinations/{destinationId}/check":
		action = "backup_check"
		if e := decodeJSON(w, r, &check); e != nil {
			writeDecodeJSONError(w, e)
			return
		}
	case "POST /api/backups/destinations/{destinationId}/restore":
		action = "backup_restore"
		if e := decodeJSON(w, r, &restore); e != nil {
			writeDecodeJSONError(w, e)
			return
		}
	}
	id := r.PathValue("destinationId")
	if err := c.backups.ValidateRepositoryRequest(r.Context(), id, action, check, restore); err != nil {
		c.fail(w, err)
		return
	}
	job, e := c.backups.StartRepositoryJob(r.Context(), id, action, "local", prune)
	if e != nil {
		c.fail(w, e)
		return
	}
	c.startWorker(func() {
		_, _ = c.backups.ExecuteRepositoryJob(c.appContext(), job, id, action, check, restore, prune)
	})
	writeJSON(w, 202, map[string]any{"job": job})
}

func (c *backupController) filesystem(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if !filepath.IsAbs(path) {
		writeError(w, 400, "validation_failed", "An absolute existing directory is required.", nil)
		return
	}
	device, err := backup.FilesystemIdentity(path)
	if err != nil {
		writeError(w, 400, "validation_failed", "Filesystem is unavailable; use an existing repository directory or its parent.", nil)
		return
	}
	writeJSON(w, 200, map[string]string{"device": device})
}

func (c *backupController) restoreManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := c.backups.ReadRestoreManifest(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"manifest": manifest})
}
func (c *backupController) restorePreview(w http.ResponseWriter, r *http.Request) {
	var input backup.RestoreRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	preview, err := c.backups.PreviewRestore(ctx, r.PathValue("destinationId"), input)
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, 200, preview)
}
