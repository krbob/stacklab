package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"stacklab/internal/auth"
	"stacklab/internal/imageupdates"
	"stacklab/internal/jobs"
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
	"strings"
	"time"
)

type maintenanceController struct {
	*Handler
}

func (c *maintenanceController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/maintenance/update-stacks", c.withAuth(c.handleUpdateStacksMaintenance))
	mux.HandleFunc("GET /api/maintenance/images", c.withAuth(c.handleMaintenanceImages))
	mux.HandleFunc("GET /api/templates", c.withAuth(c.handleTemplates))
	mux.HandleFunc("GET /api/maintenance/image-updates", c.withAuth(c.handleImageUpdatesList))
	mux.HandleFunc("POST /api/maintenance/image-updates/check", c.withAuth(c.handleImageUpdatesCheck))
	mux.HandleFunc("GET /api/maintenance/networks", c.withAuth(c.handleMaintenanceNetworks))
	mux.HandleFunc("POST /api/maintenance/networks", c.withAuth(c.handleCreateMaintenanceNetwork))
	mux.HandleFunc("DELETE /api/maintenance/networks/{name}", c.withAuth(c.handleDeleteMaintenanceNetwork))
	mux.HandleFunc("GET /api/maintenance/volumes", c.withAuth(c.handleMaintenanceVolumes))
	mux.HandleFunc("POST /api/maintenance/volumes", c.withAuth(c.handleCreateMaintenanceVolume))
	mux.HandleFunc("DELETE /api/maintenance/volumes/{name}", c.withAuth(c.handleDeleteMaintenanceVolume))
	mux.HandleFunc("GET /api/maintenance/prune-preview", c.withAuth(c.handleMaintenancePrunePreview))
	mux.HandleFunc("POST /api/maintenance/prune", c.withAuth(c.handleMaintenancePrune))
}

type maintenanceUpdateStacksRequest struct {
	Target struct {
		Mode             string              `json:"mode"`
		StackIDs         []string            `json:"stack_ids"`
		ExcludedServices map[string][]string `json:"excluded_services"`
	} `json:"target"`
	Options struct {
		PullImages    *bool `json:"pull_images"`
		BuildImages   *bool `json:"build_images"`
		RemoveOrphans *bool `json:"remove_orphans"`
		PruneAfter    struct {
			Enabled        *bool `json:"enabled"`
			IncludeVolumes *bool `json:"include_volumes"`
		} `json:"prune_after"`
	} `json:"options"`
}

type maintenancePruneRequest struct {
	Scope maintenance.PruneScope `json:"scope"`
}

func (h *maintenanceController) handleTemplates(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Templates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list templates.", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleImageUpdatesList(w http.ResponseWriter, r *http.Request) {
	cached := h.imageUpdates.StatusByImage()
	items := make([]store.ImageUpdateStatus, 0, len(cached))
	for _, status := range cached {
		items = append(items, status)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ImageRef < items[j].ImageRef })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleImageUpdatesCheck runs a check_image_updates job synchronously (like
// the other maintenance jobs) with structured per-image progress.
func (h *maintenanceController) handleImageUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	refs, err := h.stackReader.AllImageRefs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list stack images.", nil)
		return
	}

	job, err := h.jobs.StartWithResources(r.Context(), "", "check_image_updates", "local", jobs.ImageUpdatesResource())
	if err != nil {
		if errors.Is(err, jobs.ErrResourceConflict) {
			writeError(w, http.StatusConflict, "conflict", "Another image update check is already running.", nil)
			return
		}
		h.logger.Error("start image update check job failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		return
	}

	// Detached like stack actions: registry lookups can outlive the request
	// (or its proxy), and finalization must never ride a cancelled context.
	h.startWorker(func() { h.runImageUpdateCheckJob(job, refs) })

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *maintenanceController) runImageUpdateCheckJob(job store.Job, refs []string) {
	runCtx, cancel := h.stackActionContext()
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	results := h.imageUpdates.CheckImages(runCtx, refs, func(done, total int, detail string) {
		_ = h.jobs.PublishEventWithProgress(runCtx, job, "job_progress", "Checking image updates.", "", nil, &store.JobProgress{
			Phase:     "check",
			Completed: done,
			Total:     total,
			Unit:      "images",
			Detail:    detail,
		})
	})

	ctx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()

	if runCtx.Err() != nil {
		var err error
		if errors.Is(runCtx.Err(), context.Canceled) {
			_, err = h.jobs.FinishCancelled(ctx, job, "check_image_updates_cancelled", "Image update check was cancelled.")
		} else {
			_, err = h.jobs.FinishTimedOut(ctx, job, "check_image_updates_timeout", "Image update check timed out.")
		}
		if err != nil {
			h.logger.Error("finish image update check failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}
		return
	}

	available := 0
	for _, status := range results {
		if status.State == imageupdates.StateAvailable {
			available++
		}
		_ = h.jobs.PublishEvent(ctx, job, "job_log", "Checked "+status.ImageRef+".", status.State, nil)
	}

	finishedJob, err := h.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		h.logger.Error("finish image update check failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if err := h.audit.RecordJob(ctx, finishedJob, map[string]any{
		"images_checked":    len(results),
		"updates_available": available,
	}); err != nil {
		h.logger.Warn("record image update audit failed", slog.String("err", err.Error()))
	}
}

func (h *maintenanceController) handleUpdateStacksMaintenance(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenanceUpdateStacksRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	options := resolvedMaintenanceOptions{
		PullImages:     boolOrDefault(request.Options.PullImages, true),
		BuildImages:    boolOrDefault(request.Options.BuildImages, true),
		RemoveOrphans:  boolOrDefault(request.Options.RemoveOrphans, !hasMaintenanceServiceExclusions(request.Target.ExcludedServices)),
		PruneAfter:     boolOrDefault(request.Options.PruneAfter.Enabled, false),
		IncludeVolumes: boolOrDefault(request.Options.PruneAfter.IncludeVolumes, false),
	}
	if options.IncludeVolumes && !options.PruneAfter {
		writeError(w, http.StatusBadRequest, "validation_failed", "include_volumes requires prune_after.enabled = true.", nil)
		return
	}

	job, run, err := h.maintenanceJobs.StartUpdate(r.Context(), maintenancejobs.UpdateRequest{
		Target: maintenancejobs.UpdateTarget{
			Mode:             request.Target.Mode,
			StackIDs:         request.Target.StackIDs,
			ExcludedServices: request.Target.ExcludedServices,
		},
		Options: maintenancejobs.UpdateOptions{
			PullImages:     options.PullImages,
			BuildImages:    options.BuildImages,
			RemoveOrphans:  options.RemoveOrphans,
			PruneAfter:     options.PruneAfter,
			IncludeVolumes: options.IncludeVolumes,
		},
		Trigger: "manual",
	}, "local")
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", err.Error(), nil)
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating one of the selected stacks.", nil)
		default:
			h.logger.Error("run maintenance job failed", slog.String("err", err.Error()))
			writeError(w, http.StatusBadRequest, "validation_failed", "Failed to start maintenance update.", nil)
		}
		return
	}

	h.startWorker(func() { h.runMaintenanceUpdateJob(job, run) })

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *maintenanceController) handleMaintenanceImages(w http.ResponseWriter, r *http.Request) {
	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for maintenance images failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance images.", nil)
		return
	}

	filters, ok := parseMaintenanceInventoryFilters(w, r)
	if !ok {
		return
	}

	response, err := h.maintenance.Images(r.Context(), maintenance.ImagesQuery{
		Search:          filters.Search,
		Usage:           filters.Usage,
		Origin:          filters.Origin,
		ManagedStackIDs: managedStackIDs,
	})
	if err != nil {
		if errors.Is(err, maintenance.ErrDockerUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance inventory is unavailable.", nil)
			return
		}
		h.logger.Error("maintenance images failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance images.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleMaintenanceNetworks(w http.ResponseWriter, r *http.Request) {
	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for maintenance networks failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance networks.", nil)
		return
	}

	query, ok := parseMaintenanceInventoryFilters(w, r)
	if !ok {
		return
	}

	response, err := h.maintenance.Networks(r.Context(), maintenance.NetworksQuery{
		Search:          query.Search,
		Usage:           query.Usage,
		Origin:          query.Origin,
		ManagedStackIDs: managedStackIDs,
	})
	if err != nil {
		if errors.Is(err, maintenance.ErrDockerUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance inventory is unavailable.", nil)
			return
		}
		h.logger.Error("maintenance networks failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance networks.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleCreateMaintenanceNetwork(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenance.CreateNetworkRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.maintenance.CreateNetwork(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, maintenance.ErrInvalidName):
			writeError(w, http.StatusBadRequest, "validation_failed", "Network name is invalid.", nil)
		case errors.Is(err, maintenance.ErrAlreadyExists):
			writeError(w, http.StatusConflict, "already_exists", "A Docker network with that name already exists.", nil)
		case errors.Is(err, maintenance.ErrDockerUnavailable):
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance actions are unavailable.", nil)
		default:
			h.logger.Error("create maintenance network failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create Docker network.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	_ = h.audit.RecordSystemEvent(r.Context(), "create_network", "local", "succeeded", finishedAt, &finishedAt, map[string]any{
		"name": response.Name,
	})
	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleDeleteMaintenanceNetwork(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for maintenance network delete failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete Docker network.", nil)
		return
	}

	name := r.PathValue("name")
	response, err := h.maintenance.DeleteNetwork(r.Context(), name, managedStackIDs)
	if err != nil {
		switch {
		case errors.Is(err, maintenance.ErrInvalidName):
			writeError(w, http.StatusBadRequest, "validation_failed", "Network name is invalid.", nil)
		case errors.Is(err, maintenance.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Docker network not found.", nil)
		case errors.Is(err, maintenance.ErrProtectedObject):
			writeError(w, http.StatusConflict, "invalid_state", "Only unused external Docker networks can be removed manually.", nil)
		case errors.Is(err, maintenance.ErrObjectInUse):
			writeError(w, http.StatusConflict, "invalid_state", "Cannot remove a Docker network that is currently in use.", nil)
		case errors.Is(err, maintenance.ErrDockerUnavailable):
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance actions are unavailable.", nil)
		default:
			h.logger.Error("delete maintenance network failed", slog.String("name", name), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete Docker network.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	_ = h.audit.RecordSystemEvent(r.Context(), "delete_network", "local", "succeeded", finishedAt, &finishedAt, map[string]any{
		"name": response.Name,
	})
	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleMaintenanceVolumes(w http.ResponseWriter, r *http.Request) {
	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for maintenance volumes failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance volumes.", nil)
		return
	}

	query, ok := parseMaintenanceInventoryFilters(w, r)
	if !ok {
		return
	}

	response, err := h.maintenance.Volumes(r.Context(), maintenance.VolumesQuery{
		Search:          query.Search,
		Usage:           query.Usage,
		Origin:          query.Origin,
		ManagedStackIDs: managedStackIDs,
	})
	if err != nil {
		if errors.Is(err, maintenance.ErrDockerUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance inventory is unavailable.", nil)
			return
		}
		h.logger.Error("maintenance volumes failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance volumes.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleCreateMaintenanceVolume(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenance.CreateVolumeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.maintenance.CreateVolume(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, maintenance.ErrInvalidName):
			writeError(w, http.StatusBadRequest, "validation_failed", "Volume name is invalid.", nil)
		case errors.Is(err, maintenance.ErrAlreadyExists):
			writeError(w, http.StatusConflict, "already_exists", "A Docker volume with that name already exists.", nil)
		case errors.Is(err, maintenance.ErrDockerUnavailable):
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance actions are unavailable.", nil)
		default:
			h.logger.Error("create maintenance volume failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create Docker volume.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	_ = h.audit.RecordSystemEvent(r.Context(), "create_volume", "local", "succeeded", finishedAt, &finishedAt, map[string]any{
		"name": response.Name,
	})
	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleDeleteMaintenanceVolume(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for maintenance volume delete failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete Docker volume.", nil)
		return
	}

	name := r.PathValue("name")
	response, err := h.maintenance.DeleteVolume(r.Context(), name, managedStackIDs)
	if err != nil {
		switch {
		case errors.Is(err, maintenance.ErrInvalidName):
			writeError(w, http.StatusBadRequest, "validation_failed", "Volume name is invalid.", nil)
		case errors.Is(err, maintenance.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Docker volume not found.", nil)
		case errors.Is(err, maintenance.ErrProtectedObject):
			writeError(w, http.StatusConflict, "invalid_state", "Only unused external Docker volumes can be removed manually.", nil)
		case errors.Is(err, maintenance.ErrObjectInUse):
			writeError(w, http.StatusConflict, "invalid_state", "Cannot remove a Docker volume that is currently in use.", nil)
		case errors.Is(err, maintenance.ErrDockerUnavailable):
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker maintenance actions are unavailable.", nil)
		default:
			h.logger.Error("delete maintenance volume failed", slog.String("name", name), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete Docker volume.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	_ = h.audit.RecordSystemEvent(r.Context(), "delete_volume", "local", "succeeded", finishedAt, &finishedAt, map[string]any{
		"name": response.Name,
	})
	writeJSON(w, http.StatusOK, response)
}

func (h *maintenanceController) handleMaintenancePrunePreview(w http.ResponseWriter, r *http.Request) {
	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for prune preview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load prune preview.", nil)
		return
	}

	response, err := h.maintenance.PrunePreview(r.Context(), maintenance.PrunePreviewQuery{
		Images:            parseOptionalBool(r.URL.Query().Get("images"), true),
		BuildCache:        parseOptionalBool(r.URL.Query().Get("build_cache"), true),
		StoppedContainers: parseOptionalBool(r.URL.Query().Get("stopped_containers"), true),
		Volumes:           parseOptionalBool(r.URL.Query().Get("volumes"), false),
		ManagedStackIDs:   managedStackIDs,
	})
	if err != nil {
		if errors.Is(err, maintenance.ErrDockerUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "docker_unavailable", "Docker prune preview is unavailable.", nil)
			return
		}
		h.logger.Error("maintenance prune preview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load prune preview.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

type maintenanceInventoryFilters struct {
	Search string
	Usage  maintenance.ImageUsage
	Origin maintenance.ImageOrigin
}

func parseMaintenanceInventoryFilters(w http.ResponseWriter, r *http.Request) (maintenanceInventoryFilters, bool) {
	query := maintenanceInventoryFilters{
		Search: strings.TrimSpace(r.URL.Query().Get("q")),
		Usage:  maintenance.ImageUsage(strings.TrimSpace(r.URL.Query().Get("usage"))),
		Origin: maintenance.ImageOrigin(strings.TrimSpace(r.URL.Query().Get("origin"))),
	}
	if query.Usage == "" {
		query.Usage = maintenance.ImageUsageAll
	}
	if query.Origin == "" {
		query.Origin = maintenance.ImageOriginAll
	}
	if query.Usage != maintenance.ImageUsageAll && query.Usage != maintenance.ImageUsageUsed && query.Usage != maintenance.ImageUsageUnused {
		writeError(w, http.StatusBadRequest, "validation_failed", "usage must be one of: all, used, unused.", nil)
		return maintenanceInventoryFilters{}, false
	}
	if query.Origin != maintenance.ImageOriginAll && query.Origin != maintenance.ImageOriginStackManaged && query.Origin != maintenance.ImageOriginExternal {
		writeError(w, http.StatusBadRequest, "validation_failed", "origin must be one of: all, stack_managed, external.", nil)
		return maintenanceInventoryFilters{}, false
	}
	return query, true
}

func (h *maintenanceController) handleMaintenancePrune(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenancePruneRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if !request.Scope.Images && !request.Scope.BuildCache && !request.Scope.StoppedContainers && !request.Scope.Volumes {
		writeError(w, http.StatusBadRequest, "validation_failed", "At least one prune scope must be enabled.", nil)
		return
	}

	managedStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for prune failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to prepare prune workflow.", nil)
		return
	}

	job, run, err := h.maintenanceJobs.StartPrune(r.Context(), maintenancejobs.PruneRequest{
		Scope:   request.Scope,
		Trigger: "manual",
	}, "local", managedStackIDs)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "conflict", "Another global or stack maintenance job is already running.", nil)
		default:
			h.logger.Error("run prune job failed", slog.String("err", err.Error()))
			writeError(w, http.StatusBadRequest, "validation_failed", "Failed to start maintenance prune.", nil)
		}
		return
	}

	h.startWorker(func() { h.runMaintenancePruneJob(job, run) })

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *maintenanceController) runMaintenanceUpdateJob(job store.Job, run maintenancejobs.UpdateRun) {
	if _, err := h.maintenanceJobs.ExecuteUpdate(h.appContext(), job, run); err != nil {
		h.logger.Error("run maintenance update job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
}

func (h *maintenanceController) runMaintenancePruneJob(job store.Job, run maintenancejobs.PruneRun) {
	if _, err := h.maintenanceJobs.ExecutePrune(h.appContext(), job, run); err != nil {
		h.logger.Error("run maintenance prune job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
}

func hasMaintenanceServiceExclusions(excluded map[string][]string) bool {
	for _, serviceNames := range excluded {
		if len(serviceNames) > 0 {
			return true
		}
	}
	return false
}

type resolvedMaintenanceOptions struct {
	PullImages     bool
	BuildImages    bool
	RemoveOrphans  bool
	PruneAfter     bool
	IncludeVolumes bool
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (h *maintenanceController) listManagedStackIDs(ctx context.Context) ([]string, error) {
	list, err := h.stackReader.List(ctx, stacks.ListQuery{})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, item.ID)
	}
	sort.Strings(result)
	return result, nil
}
