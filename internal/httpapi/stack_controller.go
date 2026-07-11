package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"stacklab/internal/auth"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"stacklab/internal/stackworkspace"
	"stacklab/internal/store"
	"stacklab/internal/workspacerepair"
	"strings"
	"sync"
	"time"
)

type stackController struct {
	*Handler
}

func (c *stackController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stacks", c.withAuth(c.handleListStacks))
	mux.HandleFunc("POST /api/stacks", c.withAuth(c.handleCreateStack))
	mux.HandleFunc("GET /api/stacks/{stackId}", c.withAuth(c.handleGetStack))
	mux.HandleFunc("DELETE /api/stacks/{stackId}", c.withAuth(c.handleDeleteStack))
	mux.HandleFunc("GET /api/stacks/{stackId}/definition", c.withAuth(c.handleGetDefinition))
	mux.HandleFunc("PUT /api/stacks/{stackId}/definition", c.withAuth(c.handlePutDefinition))
	mux.HandleFunc("GET /api/stacks/{stackId}/workspace/tree", c.withAuth(c.handleStackWorkspaceTree))
	mux.HandleFunc("GET /api/stacks/{stackId}/workspace/file", c.withAuth(c.handleStackWorkspaceFile))
	mux.HandleFunc("PUT /api/stacks/{stackId}/workspace/file", c.withAuth(c.handlePutStackWorkspaceFile))
	mux.HandleFunc("POST /api/stacks/{stackId}/workspace/repair-permissions", c.withAuth(c.handleRepairStackWorkspacePermissions))
	mux.HandleFunc("GET /api/stacks/{stackId}/resolved-config", c.withAuth(c.handleGetResolvedConfig))
	mux.HandleFunc("POST /api/stacks/{stackId}/resolved-config", c.withAuth(c.handlePostResolvedConfig))
	mux.HandleFunc("POST /api/stacks/{stackId}/actions/{action}", c.withAuth(c.handleRunStackAction))
}

func (h *stackController) handleStackWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackFiles.Tree(r.Context(), r.PathValue("stackId"), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, stackworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the stack workspace.", nil)
		case errors.Is(err, stackworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack workspace path was not found.", nil)
		case errors.Is(err, stackworkspace.ErrPathNotDirectory):
			writeError(w, http.StatusBadRequest, "path_not_directory", "Path is not a directory.", nil)
		case errors.Is(err, stackworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Stack workspace path is not readable by the Stacklab service.", nil)
		default:
			h.logger.Error("stack workspace tree failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack workspace tree.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleStackWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackFiles.File(r.Context(), r.PathValue("stackId"), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, stackworkspace.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stackworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the stack workspace.", nil)
		case errors.Is(err, stackworkspace.ErrReservedPath):
			writeError(w, http.StatusConflict, "invalid_state", "compose.yaml and .env are managed through the stack editor.", nil)
		case errors.Is(err, stackworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack workspace file was not found.", nil)
		case errors.Is(err, stackworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Path is not a file.", nil)
		case errors.Is(err, stackworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Stack workspace file is not readable by the Stacklab service.", nil)
		default:
			h.logger.Error("stack workspace file failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack workspace file.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handlePutStackWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stackworkspace.SaveFileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	stackID := r.PathValue("stackId")
	response, err := h.stackFiles.SaveFile(r.Context(), stackID, request)
	if err != nil {
		switch {
		case errors.Is(err, stackworkspace.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stackworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the stack workspace.", nil)
		case errors.Is(err, stackworkspace.ErrReservedPath):
			writeError(w, http.StatusConflict, "invalid_state", "compose.yaml and .env are managed through the stack editor.", nil)
		case errors.Is(err, stackworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack workspace path was not found.", nil)
		case errors.Is(err, stackworkspace.ErrPathNotDirectory):
			writeError(w, http.StatusBadRequest, "path_not_directory", "Parent path is not a directory.", nil)
		case errors.Is(err, stackworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Path is not a file.", nil)
		case errors.Is(err, stackworkspace.ErrBinaryNotEditable):
			writeError(w, http.StatusConflict, "binary_not_editable", "Binary files cannot be edited in the browser.", nil)
		case errors.Is(err, stackworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Stack workspace file cannot be edited due to file permissions.", nil)
		case errors.Is(err, stackworkspace.ErrConflict):
			writeError(w, http.StatusConflict, "edit_conflict", "File changed on disk. Reload it before saving again.", nil)
		default:
			h.logger.Error("save stack workspace file failed", slog.String("stack_id", stackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save stack workspace file.", nil)
		}
		return
	}

	details := map[string]any{
		"path": response.Path,
		"type": "text_file",
	}
	if err := h.audit.RecordStackFileSave(r.Context(), stackID, response.Path, "local", details); err != nil {
		h.logger.Warn("record save_stack_file audit failed", slog.String("stack_id", stackID), slog.String("path", response.Path), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleRepairStackWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stackworkspace.RepairPermissionsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	stackID := r.PathValue("stackId")
	response, err := h.stackFiles.RepairPermissions(r.Context(), stackID, request)
	if err != nil {
		switch {
		case errors.Is(err, stackworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the stack workspace.", nil)
		case errors.Is(err, stackworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack workspace path was not found.", nil)
		case errors.Is(err, workspacerepair.ErrUnsupported):
			writeError(w, http.StatusNotImplemented, "not_implemented", "Workspace permission repair is not configured yet.", nil)
		default:
			h.logger.Error("repair stack workspace permissions failed", slog.String("stack_id", stackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to repair stack workspace permissions.", nil)
		}
		return
	}

	details := map[string]any{
		"path":          response.Path,
		"recursive":     response.Recursive,
		"changed_items": response.ChangedItems,
	}
	if len(response.Warnings) > 0 {
		details["warnings"] = response.Warnings
	}
	if err := h.audit.RecordStackPermissionRepair(r.Context(), stackID, response.Path, "local", details); err != nil {
		h.logger.Warn("record repair_stack_workspace_permissions audit failed", slog.String("stack_id", stackID), slog.String("path", response.Path), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleListStacks(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.List(r.Context(), stacks.ListQuery{
		Search: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))),
		Sort:   strings.TrimSpace(r.URL.Query().Get("sort")),
	})
	if err != nil {
		h.logger.Error("list stacks failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stacks.", nil)
		return
	}

	if err := h.decorateStackListWithAudit(r.Context(), &response, strings.TrimSpace(r.URL.Query().Get("sort"))); err != nil {
		h.logger.Error("decorate stack list with audit failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stacks.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleGetStack(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Get(r.Context(), r.PathValue("stackId"))
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack root is not a managed directory.", nil)
		default:
			h.logger.Error("get stack failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack.", nil)
		}
		return
	}

	if err := h.decorateStackDetailWithAudit(r.Context(), &response); err != nil {
		h.logger.Error("decorate stack detail with audit failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleCreateStack(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.CreateStackRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if !stacks.IsValidStackID(request.StackID) {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Stack ID must use lowercase ASCII letters, digits, and dashes.", nil)
		return
	}
	if err := stacks.ValidateDefinitionContent(request.ComposeYAML, request.Env); err != nil {
		writeContentTooLargeError(w, err)
		return
	}
	if err := h.stackReader.EnsureCreateStackAvailable(r.Context(), request.StackID); err != nil {
		switch {
		case errors.Is(err, stacks.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stacks.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "Stack ID already exists.", nil)
		default:
			h.logger.Error("preflight create stack failed", slog.String("stack_id", request.StackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create stack.", nil)
		}
		return
	}

	workflow := createWorkflowSteps(request.DeployAfterCreate)
	job, err := h.jobs.StartWithWorkflow(r.Context(), request.StackID, "create_stack", "local", workflow)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start create stack job failed", slog.String("stack_id", request.StackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Creating stack files.", "", workflowStepRef(workflow, 0))

	if err := h.stackReader.CreateStack(r.Context(), request); err != nil {
		workflow = markWorkflowFailed(workflow, 0)
		job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)
		job, _ = h.jobs.FinishFailed(r.Context(), job, "create_stack_failed", err.Error())
		_ = h.audit.RecordStackJob(r.Context(), job)

		switch {
		case errors.Is(err, stacks.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stacks.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "Stack ID already exists.", nil)
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack template was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", err.Error(), nil)
		default:
			h.logger.Error("create stack failed", slog.String("stack_id", request.StackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create stack.", nil)
		}
		return
	}

	workflow = markWorkflowSucceeded(workflow, 0)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_finished", "Created stack files.", "", workflowStepRef(workflow, 0))
	if request.DeployAfterCreate {
		workflow = markWorkflowRunning(workflow, 1)
		_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting stack runtime.", "", workflowStepRef(workflow, 1))
	}
	job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)

	if request.DeployAfterCreate {
		h.startWorker(func() { h.runCreateStackDeployJob(job, workflow, request.StackID) })
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
		return
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish create stack job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
		return
	}

	if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
		h.logger.Warn("record create stack audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *stackController) runCreateStackDeployJob(job store.Job, workflow []store.JobWorkflowStep, stackID string) {
	runCtx, cancel := h.stackActionContext()
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	upErr := h.stackReader.RunAction(runCtx, stackID, "up")

	ctx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()
	step := workflowStepRef(workflow, 1)

	if upErr != nil {
		workflow = markWorkflowFailed(workflow, 1)
		if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
			job = updatedJob
		} else {
			h.logger.Warn("update failed create stack workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}
		_ = h.jobs.PublishEvent(ctx, job, "job_step_finished", "Failed to start stack runtime.", "", step)
		failedJob, finishErr := h.jobs.FinishFailed(ctx, job, "create_stack_failed", upErr.Error())
		if finishErr != nil {
			h.logger.Error("finish create stack job failed", slog.String("job_id", job.ID), slog.String("err", finishErr.Error()))
			return
		}
		if err := h.audit.RecordStackJob(ctx, failedJob); err != nil {
			h.logger.Warn("record create stack audit failed", slog.String("job_id", failedJob.ID), slog.String("err", err.Error()))
		}
		return
	}

	deployedAt := time.Now().UTC()
	if err := h.stackReader.RecordDeployBaseline(ctx, stackID, job.ID, deployedAt); err != nil {
		h.logger.Warn("record deploy baseline failed", slog.String("stack_id", stackID), slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	workflow = markWorkflowSucceeded(workflow, 1)
	if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
		job = updatedJob
	} else {
		h.logger.Warn("update successful create stack workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	_ = h.jobs.PublishEvent(ctx, job, "job_step_finished", "Started stack runtime.", "", step)

	finishedJob, err := h.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		h.logger.Error("finish create stack job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if err := h.audit.RecordStackJob(ctx, finishedJob); err != nil {
		h.logger.Warn("record create stack audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
}

func (h *stackController) handleGetDefinition(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Definition(r.Context(), r.PathValue("stackId"))
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack definition is not available for this stack state.", nil)
		default:
			h.logger.Error("get definition failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack definition.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleDeleteStack(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.DeleteStackRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if !request.RemoveRuntime && !request.RemoveDefinition && !request.RemoveConfig && !request.RemoveData {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "At least one removal flag must be true.", nil)
		return
	}
	if _, err := h.stackReader.Get(r.Context(), r.PathValue("stackId")); err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack root is not a managed directory.", nil)
		default:
			h.logger.Error("preflight delete stack failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to remove stack.", nil)
		}
		return
	}

	stackID := r.PathValue("stackId")
	workflow := deleteWorkflowSteps(request)
	if len(workflow) > 0 {
		workflow = markWorkflowRunning(workflow, 0)
	}
	job, err := h.jobs.StartWithWorkflow(r.Context(), stackID, "remove_stack_definition", "local", workflow)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start delete stack job failed", slog.String("stack_id", stackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	if len(workflow) > 0 {
		step := workflowStepRef(workflow, 0)
		_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting delete workflow step.", "", step)
		_ = h.jobs.PublishEventWithProgress(r.Context(), job, "job_progress", "Removing selected stack resources.", "", step, &store.JobProgress{
			Phase:     workflow[0].Action,
			Completed: 0,
			Total:     len(workflow),
			Unit:      "steps",
			Detail:    "Starting " + workflow[0].Action + ".",
		})
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})

	// The destructive workflow is detached from the request and starts only
	// after the accepted response has been written. A client or proxy disconnect
	// must not cancel Docker cleanup halfway through or leave the job running.
	h.startWorker(func() { h.runDeleteStackJob(job, workflow, stackID, request) })
}

func (h *stackController) handlePutDefinition(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.UpdateDefinitionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if err := stacks.ValidateDefinitionContent(request.ComposeYAML, request.Env); err != nil {
		writeContentTooLargeError(w, err)
		return
	}

	job, err := h.jobs.Start(r.Context(), r.PathValue("stackId"), "save_definition", "local")
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start save_definition job failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	_ = h.jobs.PublishEvent(r.Context(), job, "job_progress", "Saving stack definition.", "", nil)
	preview, definition, saveErr := h.stackReader.SaveDefinition(r.Context(), r.PathValue("stackId"), request)
	if saveErr != nil {
		job, _ = h.jobs.FinishFailed(r.Context(), job, "save_definition_failed", saveErr.Error())
		if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
			h.logger.Warn("record failed save_definition audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}
		switch {
		case errors.Is(saveErr, stacks.ErrContentTooLarge):
			writeContentTooLargeError(w, saveErr)
		case errors.Is(saveErr, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(saveErr, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack definition cannot be updated in this state.", nil)
		case errors.Is(saveErr, stacks.ErrConflict):
			writeError(w, http.StatusConflict, "edit_conflict", "Stack definition changed on disk. Reload it before saving again.", nil)
		default:
			h.logger.Error("save definition failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", saveErr.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save stack definition.", nil)
		}
		return
	}

	if request.ValidateAfterSave && !preview.Valid {
		h.logger.Warn("saved invalid stack definition", slog.String("stack_id", r.PathValue("stackId")), slog.String("message", preview.Error.Message))
		_ = h.jobs.PublishEvent(r.Context(), job, "job_warning", preview.Error.Message, "", nil)
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish save_definition job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
		return
	}

	if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
		h.logger.Warn("record save_definition audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}

	payload := map[string]any{"job": job, "definition": definition}
	writeJSON(w, http.StatusOK, payload)
}

func (h *stackController) handleGetResolvedConfig(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	switch source {
	case "", "current":
		source = "current"
	case "last_valid":
	default:
		writeError(w, http.StatusBadRequest, "validation_failed", "Unsupported resolved config source.", nil)
		return
	}

	response, err := h.stackReader.ResolvedConfigCurrent(r.Context(), r.PathValue("stackId"), source)
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Resolved config is not available for this stack state.", nil)
		default:
			h.logger.Error("get resolved config failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve config.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handlePostResolvedConfig(w http.ResponseWriter, r *http.Request) {
	var request stacks.ResolvedConfigRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if err := stacks.ValidateDefinitionContent(request.ComposeYAML, request.Env); err != nil {
		writeContentTooLargeError(w, err)
		return
	}

	response, err := h.stackReader.ResolvedConfigDraft(r.Context(), r.PathValue("stackId"), request)
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Resolved config is not available for this stack state.", nil)
		default:
			h.logger.Error("post resolved config failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve draft config.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *stackController) handleRunStackAction(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct{}
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	stackID := r.PathValue("stackId")
	action := r.PathValue("action")
	if err := h.validateStackActionRequest(r.Context(), stackID, action); err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Action is not allowed for this stack state.", nil)
		case errors.Is(err, stacks.ErrUnsupportedAction):
			writeError(w, http.StatusBadRequest, "validation_failed", "Unsupported stack action.", nil)
		default:
			h.logger.Error("validate stack action failed", slog.String("stack_id", stackID), slog.String("action", action), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to validate stack action.", nil)
		}
		return
	}

	workflow := stackActionWorkflow(stackID, action)
	job, err := h.jobs.StartWithWorkflow(r.Context(), stackID, action, "local", workflow)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start stack action job failed", slog.String("stack_id", stackID), slog.String("action", action), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	step := workflowStepRef(workflow, 0)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting stack action "+action+".", "", step)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_progress", "Running stack action "+action+".", "", step)

	h.startWorker(func() { h.runStackActionJob(job, workflow) })

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *stackController) decorateStackListWithAudit(ctx context.Context, response *stacks.StackListResponse, sortBy string) error {
	if len(response.Items) == 0 {
		return nil
	}

	stackIDs := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		stackIDs = append(stackIDs, item.ID)
	}

	lastActions, err := h.audit.LastActions(ctx, stackIDs)
	if err != nil {
		return err
	}

	for i := range response.Items {
		response.Items[i].LastAction = lastActions[response.Items[i].ID]
	}

	if sortBy == "last_action" {
		sort.Slice(response.Items, func(i, j int) bool {
			left := response.Items[i].LastAction
			right := response.Items[j].LastAction
			switch {
			case left == nil && right == nil:
				return response.Items[i].Name < response.Items[j].Name
			case left == nil:
				return false
			case right == nil:
				return true
			case !left.FinishedAt.Equal(right.FinishedAt):
				return left.FinishedAt.After(right.FinishedAt)
			default:
				return response.Items[i].Name < response.Items[j].Name
			}
		})
	}

	return nil
}

func (h *stackController) decorateStackDetailWithAudit(ctx context.Context, response *stacks.StackDetailResponse) error {
	lastActions, err := h.audit.LastActions(ctx, []string{response.Stack.ID})
	if err != nil {
		return err
	}
	response.Stack.LastAction = lastActions[response.Stack.ID]
	return nil
}

func createWorkflowSteps(deployAfterCreate bool) []store.JobWorkflowStep {
	steps := []store.JobWorkflowStep{{Action: "create_stack", State: "running"}}
	if deployAfterCreate {
		steps = append(steps, store.JobWorkflowStep{Action: "up", State: "queued"})
	}
	return steps
}

func deleteWorkflowSteps(request stacks.DeleteStackRequest) []store.JobWorkflowStep {
	steps := make([]store.JobWorkflowStep, 0, 4)
	if request.RemoveRuntime {
		steps = append(steps, store.JobWorkflowStep{Action: "down", State: "queued"})
	}
	if request.RemoveDefinition {
		steps = append(steps, store.JobWorkflowStep{Action: "remove_stack_definition", State: "queued"})
	}
	if request.RemoveConfig {
		steps = append(steps, store.JobWorkflowStep{Action: "remove_config", State: "queued"})
	}
	if request.RemoveData {
		steps = append(steps, store.JobWorkflowStep{Action: "remove_data", State: "queued"})
	}
	return steps
}

func (h *stackController) runDeleteStackJob(job store.Job, workflow []store.JobWorkflowStep, stackID string, request stacks.DeleteStackRequest) {
	runCtx, cancel := h.stackActionContext()
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	steps := make([]func(context.Context) error, 0, len(workflow))
	if request.RemoveRuntime {
		steps = append(steps, func(ctx context.Context) error {
			return h.stackReader.RemoveRuntime(ctx, stackID)
		})
	}
	if request.RemoveDefinition {
		steps = append(steps, func(ctx context.Context) error {
			return h.stackReader.RemoveDefinition(ctx, stackID)
		})
	}
	if request.RemoveConfig {
		steps = append(steps, func(context.Context) error {
			return h.stackReader.RemoveConfigDir(stackID)
		})
	}
	if request.RemoveData {
		steps = append(steps, func(context.Context) error {
			return h.stackReader.RemoveDataDir(stackID)
		})
	}

	for index, run := range steps {
		if err := runCtx.Err(); err != nil {
			h.finishDeleteStackFailure(job, workflow, index, err, err)
			return
		}
		if err := run(runCtx); err != nil {
			h.finishDeleteStackFailure(job, workflow, index, runCtx.Err(), err)
			return
		}

		workflow = markWorkflowSucceeded(workflow, index)
		if index+1 < len(workflow) {
			workflow = markWorkflowRunning(workflow, index+1)
		}
		if updatedJob, err := h.jobs.UpdateWorkflow(runCtx, job, workflow); err == nil {
			job = updatedJob
		} else {
			h.logger.Warn("update delete stack workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}
		step := workflowStepRef(workflow, index)
		_ = h.jobs.PublishEvent(runCtx, job, "job_step_finished", "Finished delete workflow step.", "", step)
		_ = h.jobs.PublishEventWithProgress(runCtx, job, "job_progress", "Removed selected stack resource.", "", step, &store.JobProgress{
			Phase:     workflow[index].Action,
			Completed: index + 1,
			Total:     len(workflow),
			Unit:      "steps",
			Detail:    "Finished " + workflow[index].Action + ".",
		})
		if index+1 < len(workflow) {
			nextStep := workflowStepRef(workflow, index+1)
			_ = h.jobs.PublishEvent(runCtx, job, "job_step_started", "Starting delete workflow step.", "", nextStep)
		}
	}

	ctx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()
	finishedJob, err := h.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		h.logger.Error("finish delete stack job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if err := h.audit.RecordStackJob(ctx, finishedJob); err != nil {
		h.logger.Warn("record delete stack audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
}

func (h *stackController) finishDeleteStackFailure(job store.Job, workflow []store.JobWorkflowStep, index int, runContextErr, runErr error) {
	ctx, cancel := h.jobFinalizationContext()
	defer cancel()

	terminalState := "failed"
	errorCode := "remove_stack_failed"
	errorMessage := runErr.Error()
	stepMessage := "Delete workflow step failed."
	if errors.Is(runContextErr, context.DeadlineExceeded) || errors.Is(runErr, context.DeadlineExceeded) {
		terminalState = "timed_out"
		errorCode = "remove_stack_timed_out"
		errorMessage = "Stack removal timed out."
		stepMessage = "Delete workflow step timed out."
	} else if errors.Is(runContextErr, context.Canceled) || errors.Is(runErr, context.Canceled) {
		terminalState = "cancelled"
		errorCode = "remove_stack_cancelled"
		errorMessage = "Stack removal was cancelled."
		stepMessage = "Delete workflow step was cancelled."
	}

	workflow = markWorkflowState(workflow, index, terminalState)
	if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
		job = updatedJob
	} else {
		h.logger.Warn("update failed delete stack workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	eventJob := job
	eventJob.State = terminalState
	_ = h.jobs.PublishEvent(ctx, eventJob, "job_step_finished", stepMessage, "", workflowStepRef(workflow, index))

	finishedJob, err := h.finishTerminalJob(ctx, job, terminalState, errorCode, errorMessage)
	if err != nil {
		h.logger.Error("finish delete stack job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if err := h.audit.RecordStackJob(ctx, finishedJob); err != nil {
		h.logger.Warn("record delete stack audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
}

func stackActionWorkflow(stackID, action string) []store.JobWorkflowStep {
	return []store.JobWorkflowStep{{
		Action:        action,
		State:         "running",
		TargetStackID: stackID,
	}}
}

func (h *stackController) validateStackActionRequest(ctx context.Context, stackID, action string) error {
	if !isSupportedStackAction(action) {
		return stacks.ErrUnsupportedAction
	}

	detail, err := h.stackReader.Get(ctx, stackID)
	if err != nil {
		return err
	}
	if !stackActionAllowed(detail.Stack.AvailableActions, action) {
		return stacks.ErrInvalidState
	}
	return nil
}

func isSupportedStackAction(action string) bool {
	switch action {
	case "validate", "up", "down", "stop", "restart", "pull", "build", "recreate":
		return true
	default:
		return false
	}
}

func stackActionAllowed(actions []string, action string) bool {
	for _, candidate := range actions {
		if candidate == action {
			return true
		}
	}
	return false
}

var stackActionProgressUnits = map[string]string{
	"pull":     "layers",
	"build":    "steps",
	"up":       "services",
	"recreate": "services",
	"restart":  "services",
	"stop":     "services",
}

func (h *stackController) runStackActionJob(job store.Job, workflow []store.JobWorkflowStep) {
	runCtx, cancel := h.stackActionContext()
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	step := workflowStepRef(workflow, 0)

	// Live output: batch streamed lines so a chatty build publishes at most a
	// couple of job_log events per second instead of one per line.
	const logFlushInterval = 700 * time.Millisecond
	var logMu sync.Mutex
	var pendingLines []string
	lastFlush := time.Now()
	streamedLines := false
	flushLogs := func(force bool) {
		logMu.Lock()
		if len(pendingLines) == 0 || (!force && time.Since(lastFlush) < logFlushInterval) {
			logMu.Unlock()
			return
		}
		batch := strings.Join(pendingLines, "\n")
		pendingLines = nil
		lastFlush = time.Now()
		logMu.Unlock()
		_ = h.jobs.PublishEvent(runCtx, job, "job_log", "", batch, step)
	}

	unit := stackActionProgressUnits[job.Action]
	if unit == "" {
		unit = "items"
	}
	output, actionErr := h.stackReader.RunActionStreaming(runCtx, job.StackID, job.Action,
		func(progress stacks.StepProgress) {
			_ = h.jobs.PublishEventWithProgress(runCtx, job, "job_progress", "Running stack action "+job.Action+".", "", step, &store.JobProgress{
				Phase:     job.Action,
				Completed: progress.Completed,
				Total:     progress.Total,
				Unit:      unit,
				Detail:    progress.Detail,
			})
		},
		func(line string) {
			logMu.Lock()
			pendingLines = append(pendingLines, line)
			logMu.Unlock()
			streamedLines = true
			flushLogs(false)
		},
	)
	flushLogs(true)

	ctx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()
	// Fallback for non-streaming paths (validate, down, container actions):
	// publish the collected output once at the end, as before.
	if !streamedLines {
		for _, line := range splitProgressOutput(output) {
			_ = h.jobs.PublishEvent(ctx, job, "job_log", line, "", step)
		}
	}

	if actionErr != nil {
		terminalState, errorCode, errorMessage, stepMessage := stackActionFailure(runCtx, h.stackActionTimeout(), actionErr)
		workflow = markWorkflowState(workflow, 0, terminalState)
		if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
			job = updatedJob
		} else {
			h.logger.Warn("update failed stack action workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}

		failedEventJob := job
		failedEventJob.State = terminalState
		_ = h.jobs.PublishEvent(ctx, failedEventJob, "job_step_finished", stepMessage, "", step)

		failedJob, finishErr := h.finishTerminalJob(ctx, job, terminalState, errorCode, errorMessage)
		if finishErr != nil {
			h.logger.Error("finish stack action job failed", slog.String("job_id", job.ID), slog.String("err", finishErr.Error()))
			return
		}
		if err := h.audit.RecordStackJob(ctx, failedJob); err != nil {
			h.logger.Warn("record failed stack action audit failed", slog.String("job_id", failedJob.ID), slog.String("err", err.Error()))
		}
		return
	}

	workflow = markWorkflowSucceeded(workflow, 0)
	if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
		job = updatedJob
	} else {
		h.logger.Warn("update successful stack action workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	_ = h.jobs.PublishEvent(ctx, job, "job_step_finished", "Finished stack action.", "", step)

	finishedJob, err := h.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		h.logger.Error("finish stack action job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if stackActionUpdatesDeployBaseline(finishedJob.Action) {
		deployedAt := time.Now().UTC()
		if finishedJob.FinishedAt != nil {
			deployedAt = *finishedJob.FinishedAt
		}
		if err := h.stackReader.RecordDeployBaseline(ctx, finishedJob.StackID, finishedJob.ID, deployedAt); err != nil {
			h.logger.Warn("record deploy baseline failed", slog.String("stack_id", finishedJob.StackID), slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
		}
	}
	if stackActionInvalidatesImageUpdates(finishedJob.Action) {
		if err := h.stackReader.InvalidateImageUpdateStatus(ctx, finishedJob.StackID, nil); err != nil {
			h.logger.Warn("invalidate image update status failed", slog.String("stack_id", finishedJob.StackID), slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
		}
	}

	if err := h.audit.RecordStackJob(ctx, finishedJob); err != nil {
		h.logger.Warn("record stack action audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
}

func stackActionUpdatesDeployBaseline(action string) bool {
	return action == "up" || action == "recreate"
}

func stackActionInvalidatesImageUpdates(action string) bool {
	return action == "pull" || action == "build"
}

func stackActionFailure(ctx context.Context, timeout time.Duration, err error) (terminalState, errorCode, errorMessage, stepMessage string) {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return "timed_out", "stack_action_timed_out", "Stack action timed out after " + timeout.String() + ".", "Stack action timed out."
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		return "cancelled", "stack_action_cancelled", "Stack action was cancelled.", "Stack action cancelled."
	default:
		return "failed", "stack_action_failed", err.Error(), "Stack action failed."
	}
}
