package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"stacklab/internal/audit"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/configworkspace"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/hostinfo"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
	"stacklab/internal/terminal"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	cfg         config.Config
	logger      *slog.Logger
	mux         *http.ServeMux
	auth        *auth.Service
	audit       *audit.Service
	jobs        *jobs.Service
	terminals   *terminal.Service
	stackReader *stacks.ServiceReader
	hostInfo    hostInfoReader
	configFiles configWorkspaceReader
	gitStatus   gitWorkspaceReader
}

type hostInfoReader interface {
	Overview(ctx context.Context) (hostinfo.OverviewResponse, error)
	StacklabLogs(ctx context.Context, query hostinfo.LogsQuery) (hostinfo.StacklabLogsResponse, error)
}

type configWorkspaceReader interface {
	Tree(ctx context.Context, currentPath string) (configworkspace.TreeResponse, error)
	File(ctx context.Context, filePath string) (configworkspace.FileResponse, error)
	SaveFile(ctx context.Context, request configworkspace.SaveFileRequest) (configworkspace.SaveFileResponse, error)
}

type gitWorkspaceReader interface {
	Status(ctx context.Context) (gitworkspace.StatusResponse, error)
	Diff(ctx context.Context, requestedPath string) (gitworkspace.DiffResponse, error)
}

func NewHandler(cfg config.Config, logger *slog.Logger, authService *auth.Service, auditService *audit.Service, jobService *jobs.Service) (http.Handler, error) {
	handler := &Handler{
		cfg:    cfg,
		logger: logger,
		mux:    http.NewServeMux(),
		auth:   authService,
		audit:  auditService,
		jobs:   jobService,
		terminals: terminal.NewService(logger, terminal.Config{
			MaxSessionsPerOwner: 5,
			IdleTimeout:         30 * time.Minute,
			DetachGracePeriod:   time.Minute,
		}, func(event terminal.LifecycleEvent) {
			details := map[string]any{
				"container_id": event.ContainerID,
			}
			if event.Reason != "" {
				details["reason"] = event.Reason
			}
			action := "terminal_" + event.Type
			result := "succeeded"
			_ = auditService.RecordTerminalEvent(context.Background(), event.StackID, event.SessionID, event.ContainerID, "local", action, result, details)
		}),
		stackReader: stacks.NewServiceReader(cfg, logger),
		hostInfo:    hostinfo.NewService(cfg, time.Now().UTC()),
		configFiles: configworkspace.NewService(cfg),
		gitStatus:   gitworkspace.NewService(cfg),
	}

	handler.registerRoutes()

	return handler.withLogging(handler.mux), nil
}

func (h *Handler) registerRoutes() {
	h.mux.HandleFunc("GET /api/health", h.handleHealth)
	h.mux.HandleFunc("GET /api/ws", h.handleWebSocket)
	h.mux.HandleFunc("GET /api/session", h.handleSession)
	h.mux.HandleFunc("POST /api/auth/login", h.handleLogin)
	h.mux.HandleFunc("POST /api/auth/logout", h.withAuth(h.handleLogout))
	h.mux.HandleFunc("GET /api/meta", h.withAuth(h.handleMeta))
	h.mux.HandleFunc("GET /api/host/overview", h.withAuth(h.handleHostOverview))
	h.mux.HandleFunc("GET /api/host/stacklab-logs", h.withAuth(h.handleStacklabLogs))
	h.mux.HandleFunc("GET /api/config/workspace/tree", h.withAuth(h.handleConfigWorkspaceTree))
	h.mux.HandleFunc("GET /api/config/workspace/file", h.withAuth(h.handleConfigWorkspaceFile))
	h.mux.HandleFunc("PUT /api/config/workspace/file", h.withAuth(h.handlePutConfigWorkspaceFile))
	h.mux.HandleFunc("GET /api/git/workspace/status", h.withAuth(h.handleGitWorkspaceStatus))
	h.mux.HandleFunc("GET /api/git/workspace/diff", h.withAuth(h.handleGitWorkspaceDiff))
	h.mux.HandleFunc("GET /api/stacks", h.withAuth(h.handleListStacks))
	h.mux.HandleFunc("POST /api/stacks", h.withAuth(h.handleCreateStack))
	h.mux.HandleFunc("GET /api/stacks/{stackId}", h.withAuth(h.handleGetStack))
	h.mux.HandleFunc("DELETE /api/stacks/{stackId}", h.withAuth(h.handleDeleteStack))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/definition", h.withAuth(h.handleGetDefinition))
	h.mux.HandleFunc("PUT /api/stacks/{stackId}/definition", h.withAuth(h.handlePutDefinition))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/resolved-config", h.withAuth(h.handleGetResolvedConfig))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/resolved-config", h.withAuth(h.handlePostResolvedConfig))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/actions/{action}", h.withAuth(h.handleRunStackAction))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/audit", h.withAuth(h.handleListStackAudit))
	h.mux.HandleFunc("GET /api/audit", h.withAuth(h.handleListAudit))
	h.mux.HandleFunc("POST /api/settings/password", h.withAuth(h.handleUpdatePassword))
	h.mux.HandleFunc("GET /api/jobs/{jobId}", h.withAuth(h.handleGetJob))
	h.mux.HandleFunc("/api/", h.withAuth(h.handleAPINotImplemented))
	h.mux.HandleFunc("/", h.handleFrontend)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": stacks.AppVersion,
	})
}

func (h *Handler) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.auth.AuthenticateRequest(r.Context(), r)
	if err != nil {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user": map[string]any{
			"id":           session.UserID,
			"display_name": "Local Operator",
		},
		"features": map[string]any{
			"host_shell": false,
		},
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	session, err := h.auth.Login(r.Context(), request.Password, r.UserAgent(), auth.ClientIP(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid password.", nil)
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "auth_not_configured", "Authentication is not configured yet.", nil)
		default:
			h.logger.Error("login failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to authenticate.", nil)
		}
		return
	}

	http.SetCookie(w, h.auth.SessionCookie(session))
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	cookie, err := r.Cookie(h.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	if err := h.auth.Logout(r.Context(), cookie.Value); err != nil {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	http.SetCookie(w, h.auth.ClearSessionCookie())
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
	})
}

func (h *Handler) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.stackReader.Meta(r.Context()))
}

func (h *Handler) handleHostOverview(w http.ResponseWriter, r *http.Request) {
	response, err := h.hostInfo.Overview(r.Context())
	if err != nil {
		h.logger.Error("host overview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load host overview.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleStacklabLogs(w http.ResponseWriter, r *http.Request) {
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 200, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "limit must be a positive integer.", nil)
		return
	}

	response, err := h.hostInfo.StacklabLogs(r.Context(), hostinfo.LogsQuery{
		Limit:  limit,
		Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
		Level:  strings.TrimSpace(r.URL.Query().Get("level")),
		Search: strings.TrimSpace(r.URL.Query().Get("q")),
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

func (h *Handler) handleConfigWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	response, err := h.configFiles.Tree(r.Context(), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace path was not found.", nil)
		case errors.Is(err, configworkspace.ErrPathNotDirectory):
			writeError(w, http.StatusBadRequest, "path_not_directory", "Path is not a directory.", nil)
		default:
			h.logger.Error("config workspace tree failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load config workspace tree.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleConfigWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	response, err := h.configFiles.File(r.Context(), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace file was not found.", nil)
		case errors.Is(err, configworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Path is not a file.", nil)
		default:
			h.logger.Error("config workspace file failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load config workspace file.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handlePutConfigWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request configworkspace.SaveFileRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	response, err := h.configFiles.SaveFile(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace path was not found.", nil)
		case errors.Is(err, configworkspace.ErrPathNotDirectory):
			writeError(w, http.StatusBadRequest, "path_not_directory", "Parent path is not a directory.", nil)
		case errors.Is(err, configworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Path is not a file.", nil)
		case errors.Is(err, configworkspace.ErrBinaryNotEditable):
			writeError(w, http.StatusConflict, "binary_not_editable", "Binary files cannot be edited in the browser.", nil)
		default:
			h.logger.Error("save config workspace file failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save config workspace file.", nil)
		}
		return
	}

	details := map[string]any{
		"path": response.Path,
		"type": "text_file",
	}
	if stackID := deriveConfigWorkspaceStackID(response.Path); stackID != nil {
		details["stack_id"] = *stackID
	}
	if err := h.audit.RecordConfigFileSave(r.Context(), response.Path, deriveConfigWorkspaceStackID(response.Path), "local", details); err != nil {
		h.logger.Warn("record save_config_file audit failed", slog.String("path", response.Path), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGitWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	response, err := h.gitStatus.Status(r.Context())
	if err != nil {
		h.logger.Error("git workspace status failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Git workspace status.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGitWorkspaceDiff(w http.ResponseWriter, r *http.Request) {
	response, err := h.gitStatus.Diff(r.Context(), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the Git workspace.", nil)
		case errors.Is(err, gitworkspace.ErrInvalidManagedPath):
			writeError(w, http.StatusBadRequest, "validation_failed", "Path must be under stacks/ or config/.", nil)
		case errors.Is(err, gitworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Changed file was not found.", nil)
		default:
			h.logger.Error("git workspace diff failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Git diff.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleListStacks(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleGetStack(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Get(r.Context(), r.PathValue("stackId"))
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
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

func (h *Handler) handleCreateStack(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.CreateStackRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}
	if !stacks.IsValidStackID(request.StackID) {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Stack ID must use lowercase ASCII letters, digits, and dashes.", nil)
		return
	}
	if err := h.stackReader.EnsureCreateStackAvailable(r.Context(), request.StackID); err != nil {
		switch {
		case errors.Is(err, stacks.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "Stack ID already exists.", nil)
		default:
			h.logger.Error("preflight create stack failed", slog.String("stack_id", request.StackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create stack.", nil)
		}
		return
	}

	job, err := h.jobs.Start(r.Context(), request.StackID, "create_stack", "local")
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start create stack job failed", slog.String("stack_id", request.StackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	workflow := createWorkflowSteps(request.DeployAfterCreate)
	job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Creating stack files.", "", workflowStepRef(workflow, 0))

	if err := h.stackReader.CreateStack(r.Context(), request); err != nil {
		workflow = markWorkflowFailed(workflow, 0)
		job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)
		job, _ = h.jobs.FinishFailed(r.Context(), job, "create_stack_failed", err.Error())
		_ = h.audit.RecordStackJob(r.Context(), job)

		switch {
		case errors.Is(err, stacks.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "Stack ID already exists.", nil)
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
		upErr := h.stackReader.RunAction(r.Context(), request.StackID, "up")
		if upErr != nil {
			workflow = markWorkflowFailed(workflow, 1)
			job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)
			job, _ = h.jobs.FinishFailed(r.Context(), job, "create_stack_failed", upErr.Error())
			_ = h.audit.RecordStackJob(r.Context(), job)
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
			return
		}
		workflow = markWorkflowSucceeded(workflow, 1)
		job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)
		_ = h.jobs.PublishEvent(r.Context(), job, "job_step_finished", "Started stack runtime.", "", workflowStepRef(workflow, 1))
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

func (h *Handler) handleGetDefinition(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Definition(r.Context(), r.PathValue("stackId"))
	if err != nil {
		switch {
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

func (h *Handler) handleDeleteStack(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.DeleteStackRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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
		default:
			h.logger.Error("preflight delete stack failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to remove stack.", nil)
		}
		return
	}

	stackID := r.PathValue("stackId")
	job, err := h.jobs.Start(r.Context(), stackID, "remove_stack_definition", "local")
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start delete stack job failed", slog.String("stack_id", stackID), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	workflow := deleteWorkflowSteps(request)
	if len(workflow) > 0 {
		workflow = markWorkflowRunning(workflow, 0)
		job, _ = h.jobs.UpdateWorkflow(r.Context(), job, workflow)
		_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Starting delete workflow step.", "", workflowStepRef(workflow, 0))
	}

	stepIndex := 0
	if request.RemoveRuntime {
		if failed := h.runDeleteStep(r.Context(), &job, &workflow, stepIndex, func(ctx context.Context) error {
			return h.stackReader.RemoveRuntime(ctx, stackID)
		}); failed {
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
			return
		}
		stepIndex++
	}
	if request.RemoveDefinition {
		if failed := h.runDeleteStep(r.Context(), &job, &workflow, stepIndex, func(ctx context.Context) error {
			return h.stackReader.RemoveDefinition(ctx, stackID)
		}); failed {
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
			return
		}
		stepIndex++
	}
	if request.RemoveConfig {
		if failed := h.runDeleteStep(r.Context(), &job, &workflow, stepIndex, func(context.Context) error {
			return h.stackReader.RemoveConfigDir(stackID)
		}); failed {
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
			return
		}
		stepIndex++
	}
	if request.RemoveData {
		if failed := h.runDeleteStep(r.Context(), &job, &workflow, stepIndex, func(context.Context) error {
			return h.stackReader.RemoveDataDir(stackID)
		}); failed {
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
			return
		}
		stepIndex++
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish delete stack job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
		return
	}

	if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
		h.logger.Warn("record delete stack audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handlePutDefinition(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.UpdateDefinitionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	job, err := h.jobs.Start(r.Context(), r.PathValue("stackId"), "save_definition", "local")
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start save_definition job failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	_ = h.jobs.PublishEvent(r.Context(), job, "job_progress", "Saving stack definition.", "", nil)
	preview, saveErr := h.stackReader.SaveDefinition(r.Context(), r.PathValue("stackId"), request)
	if saveErr != nil {
		job, _ = h.jobs.FinishFailed(r.Context(), job, "save_definition_failed", saveErr.Error())
		if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
			h.logger.Warn("record failed save_definition audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}
		switch {
		case errors.Is(saveErr, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(saveErr, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack definition cannot be updated in this state.", nil)
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

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleGetResolvedConfig(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	switch source {
	case "", "current":
		source = "current"
	case "last_valid":
		writeError(w, http.StatusNotImplemented, "not_implemented", "last_valid resolved config is not implemented yet.", nil)
		return
	default:
		writeError(w, http.StatusBadRequest, "validation_failed", "Unsupported resolved config source.", nil)
		return
	}

	response, err := h.stackReader.ResolvedConfigCurrent(r.Context(), r.PathValue("stackId"), source)
	if err != nil {
		switch {
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

func (h *Handler) handlePostResolvedConfig(w http.ResponseWriter, r *http.Request) {
	var request stacks.ResolvedConfigRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	response, err := h.stackReader.ResolvedConfigDraft(r.Context(), r.PathValue("stackId"), request)
	if err != nil {
		switch {
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

func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("jobId"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		default:
			h.logger.Error("get job failed", slog.String("job_id", r.PathValue("jobId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load job.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleListStackAudit(w http.ResponseWriter, r *http.Request) {
	if _, err := h.stackReader.Get(r.Context(), r.PathValue("stackId")); err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		default:
			h.logger.Error("stack audit stack lookup failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit entries.", nil)
		}
		return
	}

	response, err := h.audit.List(
		r.Context(),
		r.PathValue("stackId"),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		parseLimit(r.URL.Query().Get("limit")),
	)
	if err != nil {
		h.logger.Error("list stack audit failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit entries.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleListAudit(w http.ResponseWriter, r *http.Request) {
	response, err := h.audit.List(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("stack_id")),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		parseLimit(r.URL.Query().Get("limit")),
	)
	if err != nil {
		h.logger.Error("list audit failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit entries.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleRunStackAction(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct{}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	stackID := r.PathValue("stackId")
	action := r.PathValue("action")

	job, err := h.jobs.Start(r.Context(), stackID, action, "local")
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start stack action job failed", slog.String("stack_id", stackID), slog.String("action", action), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	_ = h.jobs.PublishEvent(r.Context(), job, "job_progress", "Running stack action "+action+".", "", nil)
	actionErr := h.stackReader.RunAction(r.Context(), stackID, action)
	if actionErr != nil {
		job, finishErr := h.jobs.FinishFailed(r.Context(), job, "stack_action_failed", actionErr.Error())
		if finishErr != nil {
			h.logger.Error("finish stack action job failed", slog.String("job_id", job.ID), slog.String("err", finishErr.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
			return
		}

		if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
			h.logger.Warn("record failed stack action audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		}

		switch {
		case errors.Is(actionErr, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(actionErr, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Action is not allowed for this stack state.", nil)
		case errors.Is(actionErr, stacks.ErrUnsupportedAction):
			writeError(w, http.StatusBadRequest, "validation_failed", "Unsupported stack action.", nil)
		default:
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
		}
		return
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish stack action job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
		return
	}

	if err := h.audit.RecordStackJob(r.Context(), job); err != nil {
		h.logger.Warn("record stack action audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	requestedAt := time.Now().UTC()

	if err := h.auth.UpdatePassword(r.Context(), request.CurrentPassword, request.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "unauthorized", "Current password is invalid.", nil)
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "auth_not_configured", "Authentication is not configured yet.", nil)
		default:
			h.logger.Error("update password failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update password.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	if err := h.audit.RecordSystemEvent(r.Context(), "update_password", "local", "succeeded", requestedAt, &finishedAt, nil); err != nil {
		h.logger.Warn("record password update audit failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updated": true,
	})
}

func (h *Handler) handleAPINotImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "This API endpoint is not implemented yet.", nil)
}

func (h *Handler) handleFrontend(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()

		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		h.logger.Info("http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Duration("duration", time.Since(startedAt)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	flusher, ok := r.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"details": details,
		},
	})
}

func (h *Handler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := h.auth.AuthenticateRequest(r.Context(), r); err != nil {
			http.SetCookie(w, h.auth.ClearSessionCookie())
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
			return
		}

		next(w, r)
	}
}

func (h *Handler) decorateStackListWithAudit(ctx context.Context, response *stacks.StackListResponse, sortBy string) error {
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

func (h *Handler) decorateStackDetailWithAudit(ctx context.Context, response *stacks.StackDetailResponse) error {
	lastActions, err := h.audit.LastActions(ctx, []string{response.Stack.ID})
	if err != nil {
		return err
	}
	response.Stack.LastAction = lastActions[response.Stack.ID]
	return nil
}

func parseLimit(value string) int {
	if strings.TrimSpace(value) == "" {
		return 50
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 50
	}
	return parsed
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

func deriveConfigWorkspaceStackID(relativePath string) *string {
	firstSegment := strings.Split(strings.TrimSpace(relativePath), "/")[0]
	if !stacks.IsValidStackID(firstSegment) {
		return nil
	}
	stackID := firstSegment
	return &stackID
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

func markWorkflowRunning(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = "running"
	}
	return steps
}

func markWorkflowSucceeded(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = "succeeded"
	}
	return steps
}

func markWorkflowFailed(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = "failed"
	}
	return steps
}

func (h *Handler) runDeleteStep(ctx context.Context, job *store.Job, workflow *[]store.JobWorkflowStep, index int, run func(context.Context) error) bool {
	if err := run(ctx); err != nil {
		if len(*workflow) > 0 {
			*workflow = markWorkflowFailed(*workflow, index)
			updatedJob, updateErr := h.jobs.UpdateWorkflow(ctx, *job, *workflow)
			if updateErr == nil {
				*job = updatedJob
			}
		}
		failedJob, finishErr := h.jobs.FinishFailed(ctx, *job, "remove_stack_failed", err.Error())
		if finishErr == nil {
			*job = failedJob
		}
		_ = h.audit.RecordStackJob(ctx, *job)
		return true
	}

	*workflow = markWorkflowSucceeded(*workflow, index)
	_ = h.jobs.PublishEvent(ctx, *job, "job_step_finished", "Finished delete workflow step.", "", workflowStepRef(*workflow, index))
	if index+1 < len(*workflow) {
		*workflow = markWorkflowRunning(*workflow, index+1)
		_ = h.jobs.PublishEvent(ctx, *job, "job_step_started", "Starting delete workflow step.", "", workflowStepRef(*workflow, index+1))
	}
	updatedJob, err := h.jobs.UpdateWorkflow(ctx, *job, *workflow)
	if err == nil {
		*job = updatedJob
	}

	return false
}

func workflowStepRef(steps []store.JobWorkflowStep, index int) *store.JobEventStep {
	if index < 0 || index >= len(steps) {
		return nil
	}

	return &store.JobEventStep{
		Index:  index + 1,
		Total:  len(steps),
		Action: steps[index].Action,
	}
}
