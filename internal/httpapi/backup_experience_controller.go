package httpapi

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"stacklab/internal/backup"
	"stacklab/internal/stacks"
)

type backupStackCoverage struct {
	StackID   string          `json:"stack_id"`
	StackName string          `json:"stack_name"`
	Sources   []backup.Source `json:"sources"`
	Plans     []backup.Plan   `json:"plans"`
	Error     string          `json:"error,omitempty"`
}

func (c *backupController) coverage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	overview, err := c.backups.Overview(ctx)
	if err != nil {
		c.fail(w, err)
		return
	}
	listed, err := c.stackReader.List(ctx, stacks.ListQuery{})
	if err != nil {
		c.fail(w, err)
		return
	}
	items := []backupStackCoverage{{StackName: "Stacklab recovery and configuration"}}
	for _, stack := range listed.Items {
		items = append(items, backupStackCoverage{StackID: stack.ID, StackName: stack.Name})
	}
	for i := range items {
		item := &items[i]
		item.Plans = []backup.Plan{}
		item.Sources, err = c.backupCapture.Discover(ctx, item.StackID)
		if err != nil {
			item.Sources = []backup.Source{}
			// Discovery can contain command output; keep failures actionable without
			// exposing resolved Compose environment or credentials in a summary.
			item.Error = "Source discovery failed. Check the stack definition and Docker access."
		}
		for _, plan := range overview.Plans {
			for _, source := range plan.Sources {
				if source.StackID == item.StackID {
					item.Plans = append(item.Plans, plan)
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stacks": items})
}

func (c *backupController) schedulePreview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Schedule backup.Schedule `json:"schedule"`
		Count    *int            `json:"count,omitempty"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if input.Schedule.Frequency == "" {
		c.fail(w, fmt.Errorf("%w: schedule.frequency is required", backup.ErrInvalid))
		return
	}
	count := 5
	if input.Count != nil {
		count = *input.Count
	}
	if count < 1 || count > 10 {
		c.fail(w, fmt.Errorf("%w: schedule preview count must be 1 to 10", backup.ErrInvalid))
		return
	}
	preview, err := c.backups.SchedulePreview(input.Schedule, count)
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (c *backupController) preflight(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	preview, err := c.backups.Preflight(ctx, r.PathValue("planId"))
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func backupCatalogQuery(r *http.Request) (backup.CatalogQuery, error) {
	values := r.URL.Query()
	query := backup.CatalogQuery{SourceID: values.Get("source_id"), Path: values.Get("path"), Search: values.Get("search"), Limit: 100}
	for name, target := range map[string]*int{"offset": &query.Offset, "limit": &query.Limit} {
		if !values.Has(name) {
			continue
		}
		value, err := strconv.Atoi(values.Get(name))
		if err != nil || value < 0 || name == "limit" && (value < 1 || value > 500) {
			return query, fmt.Errorf("%w: invalid catalog %s", backup.ErrInvalid, name)
		}
		*target = value
	}
	return query, nil
}

func (c *backupController) snapshotCatalog(w http.ResponseWriter, r *http.Request) {
	query, err := backupCatalogQuery(r)
	if err != nil {
		c.fail(w, err)
		return
	}
	page, err := c.backups.SnapshotCatalog(r.Context(), r.PathValue("destinationId"), r.PathValue("snapshotId"), query)
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (c *backupController) compareSnapshots(w http.ResponseWriter, r *http.Request) {
	query, err := backupCatalogQuery(r)
	if err != nil {
		c.fail(w, err)
		return
	}
	page, err := c.backups.CompareSnapshotFiles(r.Context(), r.PathValue("destinationId"), r.PathValue("snapshotId"), r.URL.Query().Get("base_snapshot_id"), query)
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (c *backupController) restoreLibrary(w http.ResponseWriter, r *http.Request) {
	items, err := c.backups.RestoreLibrary(r.Context())
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (c *backupController) restoredCatalog(w http.ResponseWriter, r *http.Request) {
	query, err := backupCatalogQuery(r)
	if err != nil {
		c.fail(w, err)
		return
	}
	page, err := c.backups.RestoredCatalog(r.Context(), r.PathValue("restoreId"), query)
	if err != nil {
		c.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (c *backupController) downloadRestoredFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	file, err := c.backups.OpenRestoredFile(ctx, r.PathValue("restoreId"), r.URL.Query().Get("source_id"), r.URL.Query().Get("path"))
	if err != nil {
		c.fail(w, err)
		return
	}
	defer file.Reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The reader is context-aware. A disconnected client closes the archive and
	// cannot leave a separate restore process running.
	_, _ = io.CopyN(w, file.Reader, file.SizeBytes)
}
