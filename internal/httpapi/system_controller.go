package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"stacklab/internal/auth"
	"stacklab/internal/dockeradmin"
	"stacklab/internal/dockerregistryauth"
	"stacklab/internal/hostinfo"
	"stacklab/internal/jobs"
	"stacklab/internal/selfupdate"
	"stacklab/internal/store"
	"strconv"
	"strings"
	"time"
)

type systemController struct {
	*Handler
}

func (c *systemController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/live", c.handleLive)
	mux.HandleFunc("GET /api/ready", c.handleReady)
	mux.HandleFunc("GET /api/health", c.handleReady)
	mux.HandleFunc("GET /api/ws", c.handleWebSocket)
	mux.HandleFunc("GET /api/meta", c.withAuth(c.handleMeta))
	mux.HandleFunc("GET /api/service/metrics", c.withAuth(c.handleServiceMetrics))
	mux.HandleFunc("GET /api/host/overview", c.withAuth(c.handleHostOverview))
	mux.HandleFunc("GET /api/host/metrics", c.withAuth(c.handleHostMetrics))
	mux.HandleFunc("GET /api/host/stacklab-logs", c.withAuth(c.handleStacklabLogs))
	mux.HandleFunc("GET /api/docker/admin/overview", c.withAuth(c.handleDockerAdminOverview))
	mux.HandleFunc("GET /api/docker/admin/daemon-config", c.withAuth(c.handleDockerAdminDaemonConfig))
	mux.HandleFunc("POST /api/docker/admin/daemon-config/validate", c.withAuth(c.handleDockerAdminValidateDaemonConfig))
	mux.HandleFunc("POST /api/docker/admin/daemon-config/apply", c.withAuth(c.handleDockerAdminApplyDaemonConfig))
	mux.HandleFunc("GET /api/docker/registries", c.withAuth(c.handleDockerRegistryStatus))
	mux.HandleFunc("POST /api/docker/registries/login", c.withAuth(c.handleDockerRegistryLogin))
	mux.HandleFunc("POST /api/docker/registries/logout", c.withAuth(c.handleDockerRegistryLogout))
	mux.HandleFunc("GET /api/stacklab/update/overview", c.withAuth(c.handleStacklabUpdateOverview))
	mux.HandleFunc("POST /api/stacklab/update/apply", c.withAuth(c.handleStacklabUpdateApply))
	mux.HandleFunc("/api/", c.withAuth(c.handleAPINotImplemented))
	mux.HandleFunc("/", c.handleFrontend)
}

func (h *systemController) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.stackReader.Meta(r.Context()))
}

func (h *systemController) handleHostOverview(w http.ResponseWriter, r *http.Request) {
	response, err := h.hostInfo.Overview(r.Context())
	if err != nil {
		h.logger.Error("host overview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load host overview.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleHostMetrics(w http.ResponseWriter, r *http.Request) {
	query := hostinfo.MetricsQuery{}
	if rawSince := strings.TrimSpace(r.URL.Query().Get("since")); rawSince != "" {
		since, err := time.Parse(time.RFC3339Nano, rawSince)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_failed", "since must be an RFC3339 timestamp.", nil)
			return
		}
		since = since.UTC()
		query.Since = &since
	}

	response, err := h.hostInfo.Metrics(r.Context(), query)
	if err != nil {
		h.logger.Error("host metrics failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load host metrics.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleStacklabLogs(w http.ResponseWriter, r *http.Request) {
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 200, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "limit must be a positive integer.", nil)
		return
	}

	response, err := h.hostInfo.StacklabLogs(r.Context(), hostinfo.LogsQuery{
		Limit:             limit,
		Cursor:            strings.TrimSpace(r.URL.Query().Get("cursor")),
		Level:             strings.TrimSpace(r.URL.Query().Get("level")),
		Search:            strings.TrimSpace(r.URL.Query().Get("q")),
		IncludeHTTPAccess: parseOptionalBool(r.URL.Query().Get("include_http"), false),
	})
	if err != nil {
		if errors.Is(err, hostinfo.ErrLogsUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Stacklab service logs are unavailable.", nil)
			return
		}
		h.logger.Error("stacklab logs failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Stacklab service logs.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleDockerAdminOverview(w http.ResponseWriter, r *http.Request) {
	response, err := h.dockerAdmin.Overview(r.Context())
	if err != nil {
		h.logger.Error("docker admin overview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Docker admin overview.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleDockerAdminDaemonConfig(w http.ResponseWriter, r *http.Request) {
	response, err := h.dockerAdmin.DaemonConfig(r.Context())
	if err != nil {
		h.logger.Error("docker daemon config failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Docker daemon config.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleDockerRegistryStatus(w http.ResponseWriter, r *http.Request) {
	response, err := h.dockerRegistry.Status(r.Context())
	if err != nil {
		h.logger.Error("docker registry status failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Docker registry auth status.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleDockerRegistryLogin(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request dockerregistryauth.LoginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if err := validateDockerRegistryLoginRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}

	workflow := []store.JobWorkflowStep{{Action: "docker_login", State: "running"}}
	job, err := h.jobs.StartWithResourcesAndWorkflow(r.Context(), "", "docker_registry_login", "local", workflow, jobs.DockerRegistryResource())
	if err != nil {
		if errors.Is(err, jobs.ErrResourceConflict) {
			writeError(w, http.StatusConflict, "conflict", "Another Docker registry or stack mutation is already running.", nil)
			return
		}
		h.logger.Error("start docker registry login job failed", slog.String("registry", request.Registry), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		return
	}

	step := workflowStepRef(workflow, 0)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting Docker registry login.", "", step)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_progress", "Logging in to "+request.Registry+".", "", step)

	h.startWorker(func() { h.runDockerRegistryLoginJob(job, workflow, request) })
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *systemController) handleDockerRegistryLogout(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request dockerregistryauth.LogoutRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if err := validateDockerRegistryLogoutRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}

	workflow := []store.JobWorkflowStep{{Action: "docker_logout", State: "running"}}
	job, err := h.jobs.StartWithResourcesAndWorkflow(r.Context(), "", "docker_registry_logout", "local", workflow, jobs.DockerRegistryResource())
	if err != nil {
		if errors.Is(err, jobs.ErrResourceConflict) {
			writeError(w, http.StatusConflict, "conflict", "Another Docker registry or stack mutation is already running.", nil)
			return
		}
		h.logger.Error("start docker registry logout job failed", slog.String("registry", request.Registry), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		return
	}

	step := workflowStepRef(workflow, 0)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting Docker registry logout.", "", step)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_progress", "Logging out from "+request.Registry+".", "", step)

	h.startWorker(func() { h.runDockerRegistryLogoutJob(job, workflow, request) })
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

type dockerAdminValidateRequest struct {
	Settings   dockeradmin.ManagedSettings `json:"settings"`
	RemoveKeys []string                    `json:"remove_keys"`
}

func (h *systemController) handleDockerAdminValidateDaemonConfig(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request dockerAdminValidateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.dockerAdmin.ValidateManagedConfig(r.Context(), dockeradmin.ValidateManagedConfigRequest{
		Settings:   request.Settings,
		RemoveKeys: request.RemoveKeys,
	})
	if err != nil {
		switch {
		case errors.Is(err, dockeradmin.ErrInvalidManagedInput):
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		case errors.Is(err, dockeradmin.ErrUnreadableConfig):
			writeError(w, http.StatusConflict, "permission_denied", "Docker daemon config is not readable by the Stacklab service user.", nil)
		case errors.Is(err, dockeradmin.ErrInvalidDaemonConfig):
			writeError(w, http.StatusConflict, "invalid_state", "Docker daemon config contains invalid JSON and cannot be managed safely.", nil)
		default:
			h.logger.Error("docker daemon config validate failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to validate Docker daemon config changes.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleDockerAdminApplyDaemonConfig(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request dockerAdminValidateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	validateResponse, err := h.dockerAdmin.ValidateManagedConfig(r.Context(), dockeradmin.ValidateManagedConfigRequest{
		Settings:   request.Settings,
		RemoveKeys: request.RemoveKeys,
	})
	if err != nil {
		switch {
		case errors.Is(err, dockeradmin.ErrInvalidManagedInput):
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		case errors.Is(err, dockeradmin.ErrUnreadableConfig):
			writeError(w, http.StatusConflict, "permission_denied", "Docker daemon config is not readable by the Stacklab service user.", nil)
		case errors.Is(err, dockeradmin.ErrInvalidDaemonConfig):
			writeError(w, http.StatusConflict, "invalid_state", "Docker daemon config contains invalid JSON and cannot be managed safely.", nil)
		default:
			h.logger.Error("docker daemon config apply preflight failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to validate Docker daemon apply request.", nil)
		}
		return
	}
	if !validateResponse.WriteCapability.Supported {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Docker daemon apply is not configured yet.", nil)
		return
	}

	workflow := []store.JobWorkflowStep{
		{Action: "validate_config", State: "running"},
		{Action: "apply_and_restart", State: "queued"},
		{Action: "verify_recovery", State: "queued"},
	}
	job, err := h.jobs.StartWithResourcesAndWorkflow(r.Context(), "", "apply_docker_daemon_config", "local", workflow, jobs.DockerDaemonResource())
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrResourceConflict):
			writeError(w, http.StatusConflict, "conflict", "Another global or stack maintenance job is already running.", nil)
		default:
			h.logger.Error("start docker daemon apply job failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting Docker daemon config validation.", "", workflowStepRef(workflow, 0))
	if len(validateResponse.ChangedKeys) > 0 {
		_ = h.jobs.PublishEvent(r.Context(), job, "job_log", "Validated Docker daemon config preview.", strings.Join(validateResponse.ChangedKeys, ", "), workflowStepRef(workflow, 0))
	}
	if len(validateResponse.Warnings) > 0 {
		for _, warning := range validateResponse.Warnings {
			_ = h.jobs.PublishEvent(r.Context(), job, "job_warning", warning, "", workflowStepRef(workflow, 0))
		}
	}
	workflow = markWorkflowSucceeded(workflow, 0)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_finished", "Finished Docker daemon config validation.", "", workflowStepRef(workflow, 0))
	workflow = markWorkflowRunning(workflow, 1)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Applying Docker daemon config and restarting Docker.", "", workflowStepRef(workflow, 1))
	if updatedJob, updateErr := h.jobs.UpdateWorkflow(r.Context(), job, workflow); updateErr == nil {
		job = updatedJob
	}

	h.startWorker(func() { h.runDockerDaemonApplyJob(job, workflow, request) })

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *systemController) runDockerDaemonApplyJob(job store.Job, workflow []store.JobWorkflowStep, request dockerAdminValidateRequest) {
	ctx, cancel := context.WithCancel(h.appContext())
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	applyResult, err := h.dockerAdmin.ApplyManagedConfig(ctx, dockeradmin.ApplyManagedConfigRequest{
		Settings:   request.Settings,
		RemoveKeys: request.RemoveKeys,
	})
	if applyResult.BackupPath != "" {
		_ = h.jobs.PublishEvent(ctx, job, "job_log", "Created Docker daemon config backup.", applyResult.BackupPath, workflowStepRef(workflow, 1))
	}
	for _, warning := range applyResult.Warnings {
		_ = h.jobs.PublishEvent(ctx, job, "job_warning", warning, "", workflowStepRef(workflow, 1))
	}
	if err != nil {
		terminalState := "failed"
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			terminalState = "cancelled"
		}
		finishCtx, finishCancel := h.jobFinalizationContext()
		defer finishCancel()
		workflow = markWorkflowState(workflow, 1, terminalState)
		if updatedJob, updateErr := h.jobs.UpdateWorkflow(finishCtx, job, workflow); updateErr == nil {
			job = updatedJob
		}
		errorCode := "docker_daemon_apply_failed"
		message := err.Error()
		if errors.Is(err, dockeradmin.ErrApplyUnsupported) {
			errorCode = "not_implemented"
			message = "Docker daemon apply is not configured yet."
		} else if terminalState == "cancelled" {
			errorCode = "docker_daemon_apply_cancelled"
			message = "Docker daemon apply was cancelled."
		}
		failedJob, finishErr := h.finishTerminalJob(finishCtx, job, terminalState, errorCode, message)
		if finishErr != nil {
			h.logger.Error("finish docker daemon apply job failed", slog.String("job_id", job.ID), slog.String("err", finishErr.Error()))
			return
		}
		if err := h.audit.RecordJob(finishCtx, failedJob, dockerDaemonApplyAuditDetails(request, applyResult)); err != nil {
			h.logger.Warn("record docker daemon apply audit failed", slog.String("job_id", failedJob.ID), slog.String("err", err.Error()))
		}
		return
	}

	workflow = markWorkflowSucceeded(workflow, 1)
	_ = h.jobs.PublishEvent(ctx, job, "job_step_finished", "Applied Docker daemon config and restarted Docker.", "", workflowStepRef(workflow, 1))
	workflow = markWorkflowRunning(workflow, 2)
	_ = h.jobs.PublishEvent(ctx, job, "job_step_started", "Verifying Docker recovery.", "", workflowStepRef(workflow, 2))
	if updatedJob, updateErr := h.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
		job = updatedJob
	}

	overview, verifyErr := h.dockerAdmin.Overview(ctx)
	if verifyErr != nil || !overview.Engine.Available || (overview.Service.Supported && overview.Service.ActiveState != "active") {
		finishCtx, finishCancel := h.jobFinalizationContext()
		defer finishCancel()
		workflow = markWorkflowFailed(workflow, 2)
		if updatedJob, updateErr := h.jobs.UpdateWorkflow(finishCtx, job, workflow); updateErr == nil {
			job = updatedJob
		}
		message := "Docker daemon restart completed but recovery verification failed."
		if verifyErr != nil {
			message = verifyErr.Error()
		}
		failedJob, finishErr := h.jobs.FinishFailed(finishCtx, job, "docker_daemon_verify_failed", message)
		if finishErr != nil {
			h.logger.Error("finish docker daemon apply job failed", slog.String("job_id", job.ID), slog.String("err", finishErr.Error()))
			return
		}
		if err := h.audit.RecordJob(finishCtx, failedJob, dockerDaemonApplyAuditDetails(request, applyResult)); err != nil {
			h.logger.Warn("record docker daemon apply audit failed", slog.String("job_id", failedJob.ID), slog.String("err", err.Error()))
		}
		return
	}

	workflow = markWorkflowSucceeded(workflow, 2)
	_ = h.jobs.PublishEvent(ctx, job, "job_step_finished", "Verified Docker recovery.", "", workflowStepRef(workflow, 2))
	if updatedJob, updateErr := h.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
		job = updatedJob
	}

	finishCtx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()
	job, err = h.jobs.FinishSucceeded(finishCtx, job)
	if err != nil {
		h.logger.Error("finish docker daemon apply job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if err := h.audit.RecordJob(finishCtx, job, dockerDaemonApplyAuditDetails(request, applyResult)); err != nil {
		h.logger.Warn("record docker daemon apply audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
}

func (h *systemController) handleStacklabUpdateOverview(w http.ResponseWriter, r *http.Request) {
	if h.selfUpdate == nil {
		writeError(w, http.StatusServiceUnavailable, "self_update_unavailable", "Stacklab self-update is unavailable.", nil)
		return
	}

	response, err := h.selfUpdate.Overview(r.Context())
	if err != nil {
		h.logger.Error("stacklab self-update overview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Stacklab update status.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleStacklabUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	if h.selfUpdate == nil {
		writeError(w, http.StatusServiceUnavailable, "self_update_unavailable", "Stacklab self-update is unavailable.", nil)
		return
	}

	var request selfupdate.ApplyRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.selfUpdate.Apply(r.Context(), request, "local")
	if err != nil {
		switch {
		case errors.Is(err, selfupdate.ErrUnsupported):
			writeError(w, http.StatusServiceUnavailable, "self_update_unavailable", "Stacklab self-update is unavailable.", nil)
		case errors.Is(err, selfupdate.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", err.Error(), nil)
		default:
			h.logger.Error("stacklab self-update apply failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to start Stacklab self-update.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *systemController) handleAPINotImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "This API endpoint is not implemented yet.", nil)
}

func (h *systemController) handleFrontend(w http.ResponseWriter, r *http.Request) {
	requestPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
	if requestPath != "" && requestPath != "." {
		candidate := filepath.Join(h.cfg.FrontendDistDir, requestPath)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			http.ServeFile(w, r, candidate)
			return
		}
	}

	indexPath := filepath.Join(h.cfg.FrontendDistDir, "index.html")
	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, indexPath)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Stacklab backend is running. Frontend assets have not been built yet.\n"))
}

func parseOptionalPositiveInt(value string, fallback, max int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid integer")
	}
	if parsed > max {
		return max, nil
	}
	return parsed, nil
}

func (h *systemController) dockerRegistryAuthContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(h.appContext(), h.dockerRegistryAuthTimeout())
}

func (h *systemController) dockerRegistryAuthTimeout() time.Duration {
	if h.cfg.DockerRegistryAuthTimeout <= 0 {
		return 5 * time.Minute
	}
	return h.cfg.DockerRegistryAuthTimeout
}

func validateDockerRegistryLoginRequest(request dockerregistryauth.LoginRequest) error {
	if strings.TrimSpace(request.Registry) == "" {
		return errors.New("registry is required")
	}
	if strings.TrimSpace(request.Username) == "" {
		return errors.New("username is required")
	}
	if request.Password == "" {
		return errors.New("password is required")
	}
	return nil
}

func validateDockerRegistryLogoutRequest(request dockerregistryauth.LogoutRequest) error {
	if strings.TrimSpace(request.Registry) == "" {
		return errors.New("registry is required")
	}
	return nil
}

func (h *systemController) runDockerRegistryLoginJob(job store.Job, workflow []store.JobWorkflowStep, request dockerregistryauth.LoginRequest) {
	runCtx, cancel := h.dockerRegistryAuthContext()
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	step := workflowStepRef(workflow, 0)
	output, runErr := h.dockerRegistry.Login(runCtx, request)

	ctx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()
	for _, line := range splitProgressOutput(output) {
		_ = h.jobs.PublishEvent(ctx, job, "job_log", line, "", step)
	}

	if runErr != nil {
		terminalState, errorCode, errorMessage, stepMessage := dockerRegistryFailure(runCtx, h.dockerRegistryAuthTimeout(), "docker_registry_login", runErr)
		h.finishDockerRegistryJobFailure(ctx, "docker_registry_login", job, workflow, step, terminalState, errorCode, errorMessage, stepMessage, map[string]any{
			"registry": request.Registry,
			"username": request.Username,
		})
		return
	}

	h.finishDockerRegistryJobSuccess(ctx, "docker_registry_login", job, workflow, step, map[string]any{
		"registry": request.Registry,
		"username": request.Username,
	})
}

func (h *systemController) runDockerRegistryLogoutJob(job store.Job, workflow []store.JobWorkflowStep, request dockerregistryauth.LogoutRequest) {
	runCtx, cancel := h.dockerRegistryAuthContext()
	defer cancel()
	unregisterCancel := h.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()

	step := workflowStepRef(workflow, 0)
	output, runErr := h.dockerRegistry.Logout(runCtx, request)

	ctx, finishCancel := h.jobFinalizationContext()
	defer finishCancel()
	for _, line := range splitProgressOutput(output) {
		_ = h.jobs.PublishEvent(ctx, job, "job_log", line, "", step)
	}

	if runErr != nil {
		terminalState, errorCode, errorMessage, stepMessage := dockerRegistryFailure(runCtx, h.dockerRegistryAuthTimeout(), "docker_registry_logout", runErr)
		h.finishDockerRegistryJobFailure(ctx, "docker_registry_logout", job, workflow, step, terminalState, errorCode, errorMessage, stepMessage, map[string]any{
			"registry": request.Registry,
		})
		return
	}

	h.finishDockerRegistryJobSuccess(ctx, "docker_registry_logout", job, workflow, step, map[string]any{
		"registry": request.Registry,
	})
}

func (h *systemController) finishDockerRegistryJobSuccess(ctx context.Context, action string, job store.Job, workflow []store.JobWorkflowStep, step *store.JobEventStep, details map[string]any) {
	workflow = markWorkflowSucceeded(workflow, 0)
	if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
		job = updatedJob
	} else {
		h.logger.Warn("update successful docker registry workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	_ = h.jobs.PublishEvent(ctx, job, "job_step_finished", "Finished Docker registry auth step.", "", step)

	finishedJob, err := h.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		h.logger.Error("finish docker registry job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	if err := h.recordDockerRegistryAudit(ctx, action, finishedJob, details); err != nil {
		h.logger.Warn("record docker registry audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
}

func (h *systemController) finishDockerRegistryJobFailure(ctx context.Context, action string, job store.Job, workflow []store.JobWorkflowStep, step *store.JobEventStep, terminalState, errorCode, errorMessage, stepMessage string, details map[string]any) {
	workflow = markWorkflowState(workflow, 0, terminalState)
	if updatedJob, err := h.jobs.UpdateWorkflow(ctx, job, workflow); err == nil {
		job = updatedJob
	} else {
		h.logger.Warn("update failed docker registry workflow failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	failedEventJob := job
	failedEventJob.State = terminalState
	_ = h.jobs.PublishEvent(ctx, failedEventJob, "job_step_finished", stepMessage, "", step)

	finishedJob, err := h.finishTerminalJob(ctx, job, terminalState, errorCode, errorMessage)
	if err != nil {
		h.logger.Error("finish docker registry job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		return
	}
	details["error_code"] = errorCode
	details["error_message"] = errorMessage
	if auditErr := h.recordDockerRegistryAudit(ctx, action, finishedJob, details); auditErr != nil {
		h.logger.Warn("record docker registry audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", auditErr.Error()))
	}
}

func (h *systemController) recordDockerRegistryAudit(ctx context.Context, action string, job store.Job, details map[string]any) error {
	return h.audit.RecordSystemEvent(ctx, action, job.RequestedBy, job.State, job.RequestedAt, job.FinishedAt, details)
}

func dockerRegistryFailure(ctx context.Context, timeout time.Duration, action string, err error) (terminalState, errorCode, errorMessage, stepMessage string) {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return "timed_out", action + "_timed_out", "Docker registry auth timed out after " + timeout.String() + ".", "Docker registry auth step timed out."
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		return "cancelled", action + "_cancelled", "Docker registry auth was cancelled.", "Docker registry auth step cancelled."
	default:
		return "failed", action + "_failed", err.Error(), "Docker registry auth step failed."
	}
}

func dockerDaemonApplyAuditDetails(request dockerAdminValidateRequest, result dockeradmin.ApplyManagedConfigResult) map[string]any {
	details := map[string]any{
		"managed_keys": supportedDockerManagedKeys(),
	}
	if len(request.RemoveKeys) > 0 {
		details["remove_keys"] = request.RemoveKeys
	}
	if request.Settings.DNS != nil {
		details["dns"] = *request.Settings.DNS
	}
	if request.Settings.RegistryMirrors != nil {
		details["registry_mirrors"] = *request.Settings.RegistryMirrors
	}
	if request.Settings.InsecureRegistries != nil {
		details["insecure_registries"] = *request.Settings.InsecureRegistries
	}
	if request.Settings.LiveRestore != nil {
		details["live_restore"] = *request.Settings.LiveRestore
	}
	if len(result.ChangedKeys) > 0 {
		details["changed_keys"] = result.ChangedKeys
	}
	if result.BackupPath != "" {
		details["backup_path"] = result.BackupPath
	}
	if result.RolledBack {
		details["rolled_back"] = true
		details["rollback_succeeded"] = result.RollbackSucceeded
	}
	if result.ServiceActiveState != "" {
		details["service_active_state"] = result.ServiceActiveState
	}
	if len(result.Warnings) > 0 {
		details["warnings"] = result.Warnings
	}
	return details
}

func supportedDockerManagedKeys() []string {
	return []string{"dns", "registry_mirrors", "insecure_registries", "live_restore"}
}
