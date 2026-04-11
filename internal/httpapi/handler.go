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
	"stacklab/internal/dockeradmin"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/hostinfo"
	"stacklab/internal/jobs"
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/notifications"
	"stacklab/internal/scheduler"
	"stacklab/internal/selfupdate"
	"stacklab/internal/stacks"
	"stacklab/internal/stackworkspace"
	"stacklab/internal/store"
	"stacklab/internal/terminal"
	"stacklab/internal/workspacerepair"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	cfg             config.Config
	logger          *slog.Logger
	mux             *http.ServeMux
	auth            *auth.Service
	audit           *audit.Service
	jobs            *jobs.Service
	terminals       *terminal.Service
	stackReader     *stacks.ServiceReader
	hostInfo        hostInfoReader
	dockerAdmin     dockerAdminReader
	configFiles     configWorkspaceReader
	stackFiles      stackWorkspaceReader
	gitStatus       gitWorkspaceReader
	maintenance     maintenanceReader
	maintenanceJobs *maintenancejobs.Service
	notifications   notificationsManager
	schedules       schedulerManager
	selfUpdate      selfUpdateManager
}

type hostInfoReader interface {
	Overview(ctx context.Context) (hostinfo.OverviewResponse, error)
	StacklabLogs(ctx context.Context, query hostinfo.LogsQuery) (hostinfo.StacklabLogsResponse, error)
}

type dockerAdminReader interface {
	Overview(ctx context.Context) (dockeradmin.OverviewResponse, error)
	DaemonConfig(ctx context.Context) (dockeradmin.DaemonConfigResponse, error)
	ValidateManagedConfig(ctx context.Context, request dockeradmin.ValidateManagedConfigRequest) (dockeradmin.ValidateManagedConfigResponse, error)
	ApplyManagedConfig(ctx context.Context, request dockeradmin.ApplyManagedConfigRequest) (dockeradmin.ApplyManagedConfigResult, error)
}

type configWorkspaceReader interface {
	Tree(ctx context.Context, currentPath string) (configworkspace.TreeResponse, error)
	File(ctx context.Context, filePath string) (configworkspace.FileResponse, error)
	SaveFile(ctx context.Context, request configworkspace.SaveFileRequest) (configworkspace.SaveFileResponse, error)
	RepairPermissions(ctx context.Context, request configworkspace.RepairPermissionsRequest) (configworkspace.RepairPermissionsResponse, error)
}

type stackWorkspaceReader interface {
	Tree(ctx context.Context, stackID, currentPath string) (stackworkspace.TreeResponse, error)
	File(ctx context.Context, stackID, filePath string) (stackworkspace.FileResponse, error)
	SaveFile(ctx context.Context, stackID string, request stackworkspace.SaveFileRequest) (stackworkspace.SaveFileResponse, error)
	RepairPermissions(ctx context.Context, stackID string, request stackworkspace.RepairPermissionsRequest) (stackworkspace.RepairPermissionsResponse, error)
}

type gitWorkspaceReader interface {
	Status(ctx context.Context) (gitworkspace.StatusResponse, error)
	Diff(ctx context.Context, requestedPath string) (gitworkspace.DiffResponse, error)
	Commit(ctx context.Context, request gitworkspace.CommitRequest) (gitworkspace.CommitResponse, error)
	Push(ctx context.Context) (gitworkspace.PushResponse, error)
}

type maintenanceReader interface {
	Images(ctx context.Context, query maintenance.ImagesQuery) (maintenance.ImagesResponse, error)
	Networks(ctx context.Context, query maintenance.NetworksQuery) (maintenance.NetworksResponse, error)
	CreateNetwork(ctx context.Context, request maintenance.CreateNetworkRequest) (maintenance.CreateNetworkResponse, error)
	DeleteNetwork(ctx context.Context, name string, managedStackIDs []string) (maintenance.DeleteNetworkResponse, error)
	Volumes(ctx context.Context, query maintenance.VolumesQuery) (maintenance.VolumesResponse, error)
	CreateVolume(ctx context.Context, request maintenance.CreateVolumeRequest) (maintenance.CreateVolumeResponse, error)
	DeleteVolume(ctx context.Context, name string, managedStackIDs []string) (maintenance.DeleteVolumeResponse, error)
	PrunePreview(ctx context.Context, query maintenance.PrunePreviewQuery) (maintenance.PrunePreviewResponse, error)
	RunPruneStep(ctx context.Context, action string) (string, error)
}

type notificationsManager interface {
	GetSettings(ctx context.Context) (notifications.SettingsResponse, error)
	UpdateSettings(ctx context.Context, request notifications.UpdateSettingsRequest) (notifications.SettingsResponse, error)
	SendTest(ctx context.Context, request notifications.TestRequest) (notifications.TestResponse, error)
}

type schedulerManager interface {
	GetSettings(ctx context.Context) (scheduler.SettingsResponse, error)
	UpdateSettings(ctx context.Context, request scheduler.UpdateSettingsRequest) (scheduler.SettingsResponse, error)
}

type selfUpdateManager interface {
	Overview(ctx context.Context) (selfupdate.OverviewResponse, error)
	Apply(ctx context.Context, request selfupdate.ApplyRequest, requestedBy string) (selfupdate.ApplyResponse, error)
}

func NewHandler(cfg config.Config, logger *slog.Logger, authService *auth.Service, auditService *audit.Service, jobService *jobs.Service, notificationsService notificationsManager, scheduleService schedulerManager, selfUpdateService selfUpdateManager) (http.Handler, error) {
	stackReader := stacks.NewServiceReader(cfg, logger)
	maintenanceService := maintenance.NewService()
	handler := &Handler{
		cfg:           cfg,
		logger:        logger,
		mux:           http.NewServeMux(),
		auth:          authService,
		audit:         auditService,
		jobs:          jobService,
		notifications: notificationsService,
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
		stackReader:     stackReader,
		hostInfo:        hostinfo.NewService(cfg, time.Now().UTC()),
		dockerAdmin:     dockeradmin.NewService(cfg),
		configFiles:     configworkspace.NewService(cfg),
		stackFiles:      stackworkspace.NewService(cfg),
		gitStatus:       gitworkspace.NewService(cfg),
		maintenance:     maintenanceService,
		maintenanceJobs: maintenancejobs.NewService(logger, jobService, auditService, stackReader, maintenanceService),
		schedules:       scheduleService,
		selfUpdate:      selfUpdateService,
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
	h.mux.HandleFunc("GET /api/docker/admin/overview", h.withAuth(h.handleDockerAdminOverview))
	h.mux.HandleFunc("GET /api/docker/admin/daemon-config", h.withAuth(h.handleDockerAdminDaemonConfig))
	h.mux.HandleFunc("POST /api/docker/admin/daemon-config/validate", h.withAuth(h.handleDockerAdminValidateDaemonConfig))
	h.mux.HandleFunc("POST /api/docker/admin/daemon-config/apply", h.withAuth(h.handleDockerAdminApplyDaemonConfig))
	h.mux.HandleFunc("GET /api/stacklab/update/overview", h.withAuth(h.handleStacklabUpdateOverview))
	h.mux.HandleFunc("POST /api/stacklab/update/apply", h.withAuth(h.handleStacklabUpdateApply))
	h.mux.HandleFunc("GET /api/config/workspace/tree", h.withAuth(h.handleConfigWorkspaceTree))
	h.mux.HandleFunc("GET /api/config/workspace/file", h.withAuth(h.handleConfigWorkspaceFile))
	h.mux.HandleFunc("PUT /api/config/workspace/file", h.withAuth(h.handlePutConfigWorkspaceFile))
	h.mux.HandleFunc("POST /api/config/workspace/repair-permissions", h.withAuth(h.handleRepairConfigWorkspacePermissions))
	h.mux.HandleFunc("GET /api/git/workspace/status", h.withAuth(h.handleGitWorkspaceStatus))
	h.mux.HandleFunc("GET /api/git/workspace/diff", h.withAuth(h.handleGitWorkspaceDiff))
	h.mux.HandleFunc("POST /api/git/workspace/commit", h.withAuth(h.handleGitWorkspaceCommit))
	h.mux.HandleFunc("POST /api/git/workspace/push", h.withAuth(h.handleGitWorkspacePush))
	h.mux.HandleFunc("POST /api/maintenance/update-stacks", h.withAuth(h.handleUpdateStacksMaintenance))
	h.mux.HandleFunc("GET /api/maintenance/images", h.withAuth(h.handleMaintenanceImages))
	h.mux.HandleFunc("GET /api/maintenance/networks", h.withAuth(h.handleMaintenanceNetworks))
	h.mux.HandleFunc("POST /api/maintenance/networks", h.withAuth(h.handleCreateMaintenanceNetwork))
	h.mux.HandleFunc("DELETE /api/maintenance/networks/{name}", h.withAuth(h.handleDeleteMaintenanceNetwork))
	h.mux.HandleFunc("GET /api/maintenance/volumes", h.withAuth(h.handleMaintenanceVolumes))
	h.mux.HandleFunc("POST /api/maintenance/volumes", h.withAuth(h.handleCreateMaintenanceVolume))
	h.mux.HandleFunc("DELETE /api/maintenance/volumes/{name}", h.withAuth(h.handleDeleteMaintenanceVolume))
	h.mux.HandleFunc("GET /api/maintenance/prune-preview", h.withAuth(h.handleMaintenancePrunePreview))
	h.mux.HandleFunc("POST /api/maintenance/prune", h.withAuth(h.handleMaintenancePrune))
	h.mux.HandleFunc("GET /api/jobs/active", h.withAuth(h.handleListActiveJobs))
	h.mux.HandleFunc("GET /api/jobs/{jobId}/events", h.withAuth(h.handleListJobEvents))
	h.mux.HandleFunc("GET /api/stacks", h.withAuth(h.handleListStacks))
	h.mux.HandleFunc("POST /api/stacks", h.withAuth(h.handleCreateStack))
	h.mux.HandleFunc("GET /api/stacks/{stackId}", h.withAuth(h.handleGetStack))
	h.mux.HandleFunc("DELETE /api/stacks/{stackId}", h.withAuth(h.handleDeleteStack))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/definition", h.withAuth(h.handleGetDefinition))
	h.mux.HandleFunc("PUT /api/stacks/{stackId}/definition", h.withAuth(h.handlePutDefinition))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/workspace/tree", h.withAuth(h.handleStackWorkspaceTree))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/workspace/file", h.withAuth(h.handleStackWorkspaceFile))
	h.mux.HandleFunc("PUT /api/stacks/{stackId}/workspace/file", h.withAuth(h.handlePutStackWorkspaceFile))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/workspace/repair-permissions", h.withAuth(h.handleRepairStackWorkspacePermissions))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/resolved-config", h.withAuth(h.handleGetResolvedConfig))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/resolved-config", h.withAuth(h.handlePostResolvedConfig))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/actions/{action}", h.withAuth(h.handleRunStackAction))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/audit", h.withAuth(h.handleListStackAudit))
	h.mux.HandleFunc("GET /api/audit", h.withAuth(h.handleListAudit))
	h.mux.HandleFunc("GET /api/settings/notifications", h.withAuth(h.handleGetNotificationSettings))
	h.mux.HandleFunc("PUT /api/settings/notifications", h.withAuth(h.handleUpdateNotificationSettings))
	h.mux.HandleFunc("POST /api/settings/notifications/test", h.withAuth(h.handleSendNotificationTest))
	h.mux.HandleFunc("GET /api/settings/maintenance-schedules", h.withAuth(h.handleGetMaintenanceSchedules))
	h.mux.HandleFunc("PUT /api/settings/maintenance-schedules", h.withAuth(h.handleUpdateMaintenanceSchedules))
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

func (h *Handler) handleDockerAdminOverview(w http.ResponseWriter, r *http.Request) {
	response, err := h.dockerAdmin.Overview(r.Context())
	if err != nil {
		h.logger.Error("docker admin overview failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Docker admin overview.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleDockerAdminDaemonConfig(w http.ResponseWriter, r *http.Request) {
	response, err := h.dockerAdmin.DaemonConfig(r.Context())
	if err != nil {
		h.logger.Error("docker daemon config failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Docker daemon config.", nil)
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
		case errors.Is(err, configworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Config workspace path is not readable by the Stacklab service.", nil)
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
		case errors.Is(err, configworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Config workspace file is not readable by the Stacklab service.", nil)
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
		case errors.Is(err, configworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Config workspace file cannot be edited due to file permissions.", nil)
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

func (h *Handler) handleRepairConfigWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request configworkspace.RepairPermissionsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	response, err := h.configFiles.RepairPermissions(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace path was not found.", nil)
		case errors.Is(err, workspacerepair.ErrUnsupported):
			writeError(w, http.StatusNotImplemented, "not_implemented", "Workspace permission repair is not configured yet.", nil)
		default:
			h.logger.Error("repair config workspace permissions failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to repair config workspace permissions.", nil)
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
	if err := h.audit.RecordConfigPermissionRepair(r.Context(), response.Path, "local", details); err != nil {
		h.logger.Warn("record repair_config_workspace_permissions audit failed", slog.String("path", response.Path), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleStackWorkspaceTree(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleStackWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackFiles.File(r.Context(), r.PathValue("stackId"), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
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

func (h *Handler) handlePutStackWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stackworkspace.SaveFileRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	stackID := r.PathValue("stackId")
	response, err := h.stackFiles.SaveFile(r.Context(), stackID, request)
	if err != nil {
		switch {
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

func (h *Handler) handleRepairStackWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stackworkspace.RepairPermissionsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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

func (h *Handler) handleGitWorkspaceCommit(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request gitworkspace.CommitRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	response, err := h.gitStatus.Commit(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the Git workspace.", nil)
		case errors.Is(err, gitworkspace.ErrInvalidManagedPath), errors.Is(err, gitworkspace.ErrValidation):
			writeError(w, http.StatusBadRequest, "validation_failed", "Commit request is invalid.", nil)
		case errors.Is(err, gitworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Selected changed file was not found.", nil)
		case errors.Is(err, gitworkspace.ErrConflictedSelection):
			writeError(w, http.StatusConflict, "conflicted_files_selected", "Resolve conflicted files before committing.", nil)
		case errors.Is(err, gitworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Selected files could not be staged due to permissions.", nil)
		case errors.Is(err, gitworkspace.ErrNothingToCommit):
			writeError(w, http.StatusConflict, "nothing_to_commit", "Selected files have no commit-ready changes.", nil)
		default:
			h.logger.Error("git workspace commit failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create Git commit.", nil)
		}
		return
	}

	details := map[string]any{
		"paths":             response.Paths,
		"path_count":        len(response.Paths),
		"commit":            response.Commit,
		"summary":           response.Summary,
		"remaining_changes": response.RemainingChanges,
	}
	if err := h.audit.RecordGitCommit(r.Context(), "local", response.Commit, response.Summary, response.Paths, response.RemainingChanges, details); err != nil {
		h.logger.Warn("record git commit audit failed", slog.String("commit", response.Commit), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGitWorkspacePush(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	response, err := h.gitStatus.Push(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrUpstreamNotConfigured):
			writeError(w, http.StatusConflict, "upstream_not_configured", "Current branch has no configured upstream.", nil)
		case errors.Is(err, gitworkspace.ErrAuthFailed):
			writeError(w, http.StatusBadGateway, "git_auth_failed", "Push failed due to remote authentication.", nil)
		case errors.Is(err, gitworkspace.ErrPushRejected):
			writeError(w, http.StatusConflict, "push_rejected", "Remote rejected the push.", nil)
		default:
			h.logger.Error("git workspace push failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to push Git changes.", nil)
		}
		return
	}

	details := map[string]any{
		"remote":        response.Remote,
		"branch":        response.Branch,
		"upstream_name": response.UpstreamName,
		"head_commit":   response.HeadCommit,
		"pushed":        response.Pushed,
		"ahead_count":   response.AheadCount,
		"behind_count":  response.BehindCount,
	}
	if err := h.audit.RecordGitPush(r.Context(), "local", response.Remote, response.Branch, response.UpstreamName, response.HeadCommit, response.Pushed, response.AheadCount, response.BehindCount, details); err != nil {
		h.logger.Warn("record git push audit failed", slog.String("branch", response.Branch), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

type maintenanceUpdateStacksRequest struct {
	Target struct {
		Mode     string   `json:"mode"`
		StackIDs []string `json:"stack_ids"`
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

type dockerAdminValidateRequest struct {
	Settings   dockeradmin.ManagedSettings `json:"settings"`
	RemoveKeys []string                    `json:"remove_keys"`
}

func (h *Handler) handleUpdateStacksMaintenance(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenanceUpdateStacksRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	options := resolvedMaintenanceOptions{
		PullImages:     boolOrDefault(request.Options.PullImages, true),
		BuildImages:    boolOrDefault(request.Options.BuildImages, true),
		RemoveOrphans:  boolOrDefault(request.Options.RemoveOrphans, true),
		PruneAfter:     boolOrDefault(request.Options.PruneAfter.Enabled, false),
		IncludeVolumes: boolOrDefault(request.Options.PruneAfter.IncludeVolumes, false),
	}
	if options.IncludeVolumes && !options.PruneAfter {
		writeError(w, http.StatusBadRequest, "validation_failed", "include_volumes requires prune_after.enabled = true.", nil)
		return
	}

	job, err := h.maintenanceJobs.RunUpdate(r.Context(), maintenancejobs.UpdateRequest{
		Target: maintenancejobs.UpdateTarget{
			Mode:     request.Target.Mode,
			StackIDs: request.Target.StackIDs,
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
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating one of the selected stacks.", nil)
		default:
			h.logger.Error("run maintenance job failed", slog.String("err", err.Error()))
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleMaintenanceImages(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleMaintenanceNetworks(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleCreateMaintenanceNetwork(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenance.CreateNetworkRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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

func (h *Handler) handleDeleteMaintenanceNetwork(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleMaintenanceVolumes(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleCreateMaintenanceVolume(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenance.CreateVolumeRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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

func (h *Handler) handleDeleteMaintenanceVolume(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleMaintenancePrunePreview(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleMaintenancePrune(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request maintenancePruneRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}
	if !request.Scope.Images && !request.Scope.BuildCache && !request.Scope.StoppedContainers && !request.Scope.Volumes {
		writeError(w, http.StatusBadRequest, "validation_failed", "At least one prune scope must be enabled.", nil)
		return
	}

	lockStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for prune failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to prepare prune workflow.", nil)
		return
	}

	job, err := h.maintenanceJobs.RunPrune(r.Context(), maintenancejobs.PruneRequest{
		Scope:   request.Scope,
		Trigger: "manual",
	}, "local", lockStackIDs)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "conflict", "Another global or stack maintenance job is already running.", nil)
		default:
			h.logger.Error("run prune job failed", slog.String("err", err.Error()))
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleDockerAdminValidateDaemonConfig(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request dockerAdminValidateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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

func (h *Handler) handleDockerAdminApplyDaemonConfig(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request dockerAdminValidateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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

	lockStackIDs, err := h.listManagedStackIDs(r.Context())
	if err != nil {
		h.logger.Error("list managed stacks for docker daemon apply failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to prepare Docker daemon apply workflow.", nil)
		return
	}

	job, err := h.jobs.StartWithLocks(r.Context(), "", "apply_docker_daemon_config", "local", lockStackIDs)
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "conflict", "Another global or stack maintenance job is already running.", nil)
		default:
			h.logger.Error("start docker daemon apply job failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	workflow := []store.JobWorkflowStep{
		{Action: "validate_config", State: "running"},
		{Action: "apply_and_restart", State: "queued"},
		{Action: "verify_recovery", State: "queued"},
	}
	updatedJob, updateErr := h.jobs.UpdateWorkflow(r.Context(), job, workflow)
	if updateErr != nil {
		h.logger.Error("initialize docker daemon apply workflow failed", slog.String("job_id", job.ID), slog.String("err", updateErr.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to initialize Docker daemon apply workflow.", nil)
		return
	}
	job = updatedJob
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

	applyResult, err := h.dockerAdmin.ApplyManagedConfig(r.Context(), dockeradmin.ApplyManagedConfigRequest{
		Settings:   request.Settings,
		RemoveKeys: request.RemoveKeys,
	})
	if applyResult.BackupPath != "" {
		_ = h.jobs.PublishEvent(r.Context(), job, "job_log", "Created Docker daemon config backup.", applyResult.BackupPath, workflowStepRef(workflow, 1))
	}
	for _, warning := range applyResult.Warnings {
		_ = h.jobs.PublishEvent(r.Context(), job, "job_warning", warning, "", workflowStepRef(workflow, 1))
	}
	if err != nil {
		workflow = markWorkflowFailed(workflow, 1)
		if updatedJob, updateErr := h.jobs.UpdateWorkflow(r.Context(), job, workflow); updateErr == nil {
			job = updatedJob
		}
		errorCode := "docker_daemon_apply_failed"
		message := err.Error()
		if errors.Is(err, dockeradmin.ErrApplyUnsupported) {
			errorCode = "not_implemented"
			message = "Docker daemon apply is not configured yet."
		}
		job, _ = h.jobs.FinishFailed(r.Context(), job, errorCode, message)
		_ = h.audit.RecordJob(r.Context(), job, dockerDaemonApplyAuditDetails(request, applyResult))
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
		return
	}

	workflow = markWorkflowSucceeded(workflow, 1)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_finished", "Applied Docker daemon config and restarted Docker.", "", workflowStepRef(workflow, 1))
	workflow = markWorkflowRunning(workflow, 2)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_started", "Verifying Docker recovery.", "", workflowStepRef(workflow, 2))
	if updatedJob, updateErr := h.jobs.UpdateWorkflow(r.Context(), job, workflow); updateErr == nil {
		job = updatedJob
	}

	overview, verifyErr := h.dockerAdmin.Overview(r.Context())
	if verifyErr != nil || !overview.Engine.Available || (overview.Service.Supported && overview.Service.ActiveState != "active") {
		workflow = markWorkflowFailed(workflow, 2)
		if updatedJob, updateErr := h.jobs.UpdateWorkflow(r.Context(), job, workflow); updateErr == nil {
			job = updatedJob
		}
		message := "Docker daemon restart completed but recovery verification failed."
		if verifyErr != nil {
			message = verifyErr.Error()
		}
		job, _ = h.jobs.FinishFailed(r.Context(), job, "docker_daemon_verify_failed", message)
		_ = h.audit.RecordJob(r.Context(), job, dockerDaemonApplyAuditDetails(request, applyResult))
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
		return
	}

	workflow = markWorkflowSucceeded(workflow, 2)
	_ = h.jobs.PublishEvent(r.Context(), job, "job_step_finished", "Verified Docker recovery.", "", workflowStepRef(workflow, 2))
	if updatedJob, updateErr := h.jobs.UpdateWorkflow(r.Context(), job, workflow); updateErr == nil {
		job = updatedJob
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish docker daemon apply job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize Docker daemon apply job.", nil)
		return
	}
	if err := h.audit.RecordJob(r.Context(), job, dockerDaemonApplyAuditDetails(request, applyResult)); err != nil {
		h.logger.Warn("record docker daemon apply audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleStacklabUpdateOverview(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleStacklabUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	if h.selfUpdate == nil {
		writeError(w, http.StatusServiceUnavailable, "self_update_unavailable", "Stacklab self-update is unavailable.", nil)
		return
	}

	var request selfupdate.ApplyRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
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

func (h *Handler) handleListActiveJobs(w http.ResponseWriter, r *http.Request) {
	response, err := h.jobs.ListActive(r.Context())
	if err != nil {
		h.logger.Error("list active jobs failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load active jobs.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleListJobEvents(w http.ResponseWriter, r *http.Request) {
	response, err := h.jobs.Events(r.Context(), r.PathValue("jobId"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		default:
			h.logger.Error("list job events failed", slog.String("job_id", r.PathValue("jobId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load job events.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
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

func (h *Handler) handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if h.notifications == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Notifications are not configured yet.", nil)
		return
	}

	response, err := h.notifications.GetSettings(r.Context())
	if err != nil {
		h.logger.Error("get notification settings failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load notification settings.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleUpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	if h.notifications == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Notifications are not configured yet.", nil)
		return
	}

	var request notifications.UpdateSettingsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := h.notifications.UpdateSettings(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, notifications.ErrInvalidConfig):
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		default:
			h.logger.Error("update notification settings failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update notification settings.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	if err := h.audit.RecordSystemEvent(r.Context(), "update_notification_settings", "local", "succeeded", requestedAt, &finishedAt, map[string]any{
		"enabled":    response.Enabled,
		"configured": response.Configured,
	}); err != nil {
		h.logger.Warn("record notification settings audit failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleSendNotificationTest(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	if h.notifications == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented", "Notifications are not configured yet.", nil)
		return
	}

	var request notifications.TestRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := h.notifications.SendTest(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, notifications.ErrInvalidConfig):
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		default:
			h.logger.Warn("send notification test failed", slog.String("err", err.Error()))
			writeError(w, http.StatusBadGateway, "delivery_failed", "Failed to deliver the test notification.", nil)
		}
		return
	}

	finishedAt := time.Now().UTC()
	if err := h.audit.RecordSystemEvent(r.Context(), "send_notification_test", "local", "succeeded", requestedAt, &finishedAt, nil); err != nil {
		h.logger.Warn("record notification test audit failed", slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGetMaintenanceSchedules(w http.ResponseWriter, r *http.Request) {
	response, err := h.schedules.GetSettings(r.Context())
	if err != nil {
		h.logger.Error("get maintenance schedules failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load maintenance schedules.", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleUpdateMaintenanceSchedules(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request scheduler.UpdateSettingsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	requestedAt := time.Now().UTC()
	response, err := h.schedules.UpdateSettings(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}

	finishedAt := time.Now().UTC()
	if err := h.audit.RecordSystemEvent(r.Context(), "update_maintenance_schedules", "local", "succeeded", requestedAt, &finishedAt, map[string]any{
		"update_enabled": response.Update.Enabled,
		"prune_enabled":  response.Prune.Enabled,
	}); err != nil {
		h.logger.Warn("record maintenance schedules audit failed", slog.String("err", err.Error()))
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
		Index:         index + 1,
		Total:         len(steps),
		Action:        steps[index].Action,
		TargetStackID: steps[index].TargetStackID,
	}
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

func parseOptionalBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func (h *Handler) listManagedStackIDs(ctx context.Context) ([]string, error) {
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
