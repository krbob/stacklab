package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"stacklab/internal/dockerregistryauth"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/hostinfo"
	"stacklab/internal/imageupdates"
	"stacklab/internal/jobs"
	"stacklab/internal/limitedio"
	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/notifications"
	"stacklab/internal/requestid"
	"stacklab/internal/scheduler"
	"stacklab/internal/selfupdate"
	"stacklab/internal/servicemetrics"
	"stacklab/internal/stacks"
	"stacklab/internal/stackworkspace"
	"stacklab/internal/store"
	"stacklab/internal/terminal"
	"stacklab/internal/workspacerepair"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	appCtx          context.Context
	workers         WorkerAdmitter
	cfg             config.Config
	logger          *slog.Logger
	mux             *http.ServeMux
	served          http.Handler
	auth            *auth.Service
	audit           *audit.Service
	jobs            *jobs.Service
	terminals       *terminal.Service
	stackReader     *stacks.ServiceReader
	imageUpdates    *imageupdates.Service
	hostInfo        HostInfoReader
	dockerAdmin     DockerAdminReader
	dockerRegistry  DockerRegistryReader
	configFiles     ConfigWorkspaceReader
	stackFiles      StackWorkspaceReader
	gitStatus       GitWorkspaceReader
	maintenance     MaintenanceReader
	maintenanceJobs *maintenancejobs.Service
	notifications   NotificationsManager
	schedules       SchedulerManager
	selfUpdate      SelfUpdateManager
	readinessChecks []ReadinessCheck
	serviceMetrics  *servicemetrics.Collector

	wsMu          sync.Mutex
	wsClosing     bool
	wsConnections map[*wsConnection]struct{}
	wsWG          sync.WaitGroup
}

// WorkerAdmitter submits request-triggered asynchronous work to the runtime
// owner without giving the HTTP layer control over runtime shutdown.
type WorkerAdmitter interface {
	Go(func(context.Context)) bool
}

type HostInfoReader interface {
	Overview(ctx context.Context) (hostinfo.OverviewResponse, error)
	Metrics(ctx context.Context, query hostinfo.MetricsQuery) (hostinfo.MetricsResponse, error)
	StacklabLogs(ctx context.Context, query hostinfo.LogsQuery) (hostinfo.StacklabLogsResponse, error)
	GetSettings(ctx context.Context) (hostinfo.SettingsResponse, error)
	UpdateSettings(ctx context.Context, request hostinfo.UpdateSettingsRequest) (hostinfo.SettingsResponse, error)
}

type DockerAdminReader interface {
	Overview(ctx context.Context) (dockeradmin.OverviewResponse, error)
	DaemonConfig(ctx context.Context) (dockeradmin.DaemonConfigResponse, error)
	ValidateManagedConfig(ctx context.Context, request dockeradmin.ValidateManagedConfigRequest) (dockeradmin.ValidateManagedConfigResponse, error)
	ApplyManagedConfig(ctx context.Context, request dockeradmin.ApplyManagedConfigRequest) (dockeradmin.ApplyManagedConfigResult, error)
}

type DockerRegistryReader interface {
	Status(ctx context.Context) (dockerregistryauth.StatusResponse, error)
	Login(ctx context.Context, request dockerregistryauth.LoginRequest) (string, error)
	Logout(ctx context.Context, request dockerregistryauth.LogoutRequest) (string, error)
}

type ConfigWorkspaceReader interface {
	Tree(ctx context.Context, currentPath string) (configworkspace.TreeResponse, error)
	File(ctx context.Context, filePath string) (configworkspace.FileResponse, error)
	SaveFile(ctx context.Context, request configworkspace.SaveFileRequest) (configworkspace.SaveFileResponse, error)
	RepairPermissions(ctx context.Context, request configworkspace.RepairPermissionsRequest) (configworkspace.RepairPermissionsResponse, error)
}

type StackWorkspaceReader interface {
	Tree(ctx context.Context, stackID, currentPath string) (stackworkspace.TreeResponse, error)
	File(ctx context.Context, stackID, filePath string) (stackworkspace.FileResponse, error)
	SaveFile(ctx context.Context, stackID string, request stackworkspace.SaveFileRequest) (stackworkspace.SaveFileResponse, error)
	RepairPermissions(ctx context.Context, stackID string, request stackworkspace.RepairPermissionsRequest) (stackworkspace.RepairPermissionsResponse, error)
}

type GitWorkspaceReader interface {
	Status(ctx context.Context) (gitworkspace.StatusResponse, error)
	Diff(ctx context.Context, requestedPath string) (gitworkspace.DiffResponse, error)
	Commit(ctx context.Context, request gitworkspace.CommitRequest) (gitworkspace.CommitResponse, error)
	Push(ctx context.Context) (gitworkspace.PushResponse, error)
}

type MaintenanceReader interface {
	Images(ctx context.Context, query maintenance.ImagesQuery) (maintenance.ImagesResponse, error)
	Networks(ctx context.Context, query maintenance.NetworksQuery) (maintenance.NetworksResponse, error)
	CreateNetwork(ctx context.Context, request maintenance.CreateNetworkRequest) (maintenance.CreateNetworkResponse, error)
	DeleteNetwork(ctx context.Context, name string, managedStackIDs []string) (maintenance.DeleteNetworkResponse, error)
	Volumes(ctx context.Context, query maintenance.VolumesQuery) (maintenance.VolumesResponse, error)
	CreateVolume(ctx context.Context, request maintenance.CreateVolumeRequest) (maintenance.CreateVolumeResponse, error)
	DeleteVolume(ctx context.Context, name string, managedStackIDs []string) (maintenance.DeleteVolumeResponse, error)
	PrunePreview(ctx context.Context, query maintenance.PrunePreviewQuery) (maintenance.PrunePreviewResponse, error)
	RunPruneStep(ctx context.Context, action string, managedStackIDs []string) (string, error)
}

type NotificationsManager interface {
	GetSettings(ctx context.Context) (notifications.SettingsResponse, error)
	UpdateSettings(ctx context.Context, request notifications.UpdateSettingsRequest) (notifications.SettingsResponse, error)
	SendTest(ctx context.Context, request notifications.TestRequest) (notifications.TestResponse, error)
}

type SchedulerManager interface {
	GetSettings(ctx context.Context) (scheduler.SettingsResponse, error)
	UpdateSettings(ctx context.Context, request scheduler.UpdateSettingsRequest) (scheduler.SettingsResponse, error)
}

type SelfUpdateManager interface {
	Overview(ctx context.Context) (selfupdate.OverviewResponse, error)
	Apply(ctx context.Context, request selfupdate.ApplyRequest, requestedBy string) (selfupdate.ApplyResponse, error)
}

// Dependencies contains process-owned services required by the HTTP transport.
// NewHandler validates and stores them but never constructs or starts them.
type Dependencies struct {
	RuntimeContext  context.Context
	Workers         WorkerAdmitter
	Auth            *auth.Service
	Audit           *audit.Service
	Jobs            *jobs.Service
	Terminals       *terminal.Service
	StackReader     *stacks.ServiceReader
	ImageUpdates    *imageupdates.Service
	HostInfo        HostInfoReader
	DockerAdmin     DockerAdminReader
	DockerRegistry  DockerRegistryReader
	ConfigFiles     ConfigWorkspaceReader
	StackFiles      StackWorkspaceReader
	GitStatus       GitWorkspaceReader
	Maintenance     MaintenanceReader
	MaintenanceJobs *maintenancejobs.Service
	Notifications   NotificationsManager
	Schedules       SchedulerManager
	SelfUpdate      SelfUpdateManager
	ReadinessChecks []ReadinessCheck
	ServiceMetrics  *servicemetrics.Collector
}

func NewHandler(cfg config.Config, logger *slog.Logger, dependencies Dependencies) (*Handler, error) {
	if err := dependencies.validate(logger); err != nil {
		return nil, err
	}
	handler := &Handler{
		appCtx:          dependencies.RuntimeContext,
		workers:         dependencies.Workers,
		cfg:             cfg,
		logger:          logger,
		mux:             http.NewServeMux(),
		auth:            dependencies.Auth,
		audit:           dependencies.Audit,
		jobs:            dependencies.Jobs,
		terminals:       dependencies.Terminals,
		stackReader:     dependencies.StackReader,
		imageUpdates:    dependencies.ImageUpdates,
		hostInfo:        dependencies.HostInfo,
		dockerAdmin:     dependencies.DockerAdmin,
		dockerRegistry:  dependencies.DockerRegistry,
		configFiles:     dependencies.ConfigFiles,
		stackFiles:      dependencies.StackFiles,
		gitStatus:       dependencies.GitStatus,
		maintenance:     dependencies.Maintenance,
		maintenanceJobs: dependencies.MaintenanceJobs,
		notifications:   dependencies.Notifications,
		schedules:       dependencies.Schedules,
		selfUpdate:      dependencies.SelfUpdate,
		readinessChecks: append([]ReadinessCheck(nil), dependencies.ReadinessChecks...),
		serviceMetrics:  dependencies.ServiceMetrics,
		wsConnections:   map[*wsConnection]struct{}{},
	}

	handler.registerRoutes()
	handler.served = handler.withRequestID(handler.withLogging(handler.withSecurityHeaders(handler.mux)))

	return handler, nil
}

func (d Dependencies) validate(logger *slog.Logger) error {
	required := []struct {
		name    string
		missing bool
	}{
		{name: "logger", missing: logger == nil},
		{name: "runtime context", missing: d.RuntimeContext == nil},
		{name: "worker admitter", missing: d.Workers == nil},
		{name: "auth service", missing: d.Auth == nil},
		{name: "audit service", missing: d.Audit == nil},
		{name: "job service", missing: d.Jobs == nil},
		{name: "terminal service", missing: d.Terminals == nil},
		{name: "stack reader", missing: d.StackReader == nil},
		{name: "image update service", missing: d.ImageUpdates == nil},
		{name: "host info service", missing: d.HostInfo == nil},
		{name: "Docker admin service", missing: d.DockerAdmin == nil},
		{name: "Docker registry service", missing: d.DockerRegistry == nil},
		{name: "config workspace service", missing: d.ConfigFiles == nil},
		{name: "stack workspace service", missing: d.StackFiles == nil},
		{name: "Git workspace service", missing: d.GitStatus == nil},
		{name: "maintenance service", missing: d.Maintenance == nil},
		{name: "maintenance jobs service", missing: d.MaintenanceJobs == nil},
		{name: "notifications service", missing: d.Notifications == nil},
		{name: "scheduler service", missing: d.Schedules == nil},
		{name: "self-update service", missing: d.SelfUpdate == nil},
		{name: "readiness checks", missing: len(d.ReadinessChecks) == 0},
		{name: "service metrics", missing: d.ServiceMetrics == nil},
	}
	for _, dependency := range required {
		if dependency.missing {
			return fmt.Errorf("httpapi: %s dependency is required", dependency.name)
		}
	}
	for index, check := range d.ReadinessChecks {
		if strings.TrimSpace(check.Name) == "" || check.Check == nil {
			return fmt.Errorf("httpapi: readiness check %d must have a name and function", index)
		}
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.served == nil {
		h.served = h.withRequestID(h.withLogging(h.withSecurityHeaders(h.mux)))
	}
	h.served.ServeHTTP(w, r)
}

func (h *Handler) registerRoutes() {
	controllers := []routeController{
		&authController{Handler: h},
		&systemController{Handler: h},
		&workspaceController{Handler: h},
		&maintenanceController{Handler: h},
		&stackController{Handler: h},
		&operationsController{Handler: h},
		&settingsController{Handler: h},
	}
	for _, controller := range controllers {
		controller.registerRoutes(h.mux)
	}
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

func (h *Handler) handleHostMetrics(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleStacklabLogs(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleDockerRegistryStatus(w http.ResponseWriter, r *http.Request) {
	response, err := h.dockerRegistry.Status(r.Context())
	if err != nil {
		h.logger.Error("docker registry status failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Docker registry auth status.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleDockerRegistryLogin(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleDockerRegistryLogout(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handlePutStackWorkspaceFile(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleRepairStackWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
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

type dockerAdminValidateRequest struct {
	Settings   dockeradmin.ManagedSettings `json:"settings"`
	RemoveKeys []string                    `json:"remove_keys"`
}

func (h *Handler) handleTemplates(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Templates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list templates.", nil)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleImageUpdatesList(w http.ResponseWriter, r *http.Request) {
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
func (h *Handler) handleImageUpdatesCheck(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) runImageUpdateCheckJob(job store.Job, refs []string) {
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

func (h *Handler) handleUpdateStacksMaintenance(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) runMaintenanceUpdateJob(job store.Job, run maintenancejobs.UpdateRun) {
	if _, err := h.maintenanceJobs.ExecuteUpdate(h.appContext(), job, run); err != nil {
		h.logger.Error("run maintenance update job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
}

func (h *Handler) runMaintenancePruneJob(job store.Job, run maintenancejobs.PruneRun) {
	if _, err := h.maintenanceJobs.ExecutePrune(h.appContext(), job, run); err != nil {
		h.logger.Error("run maintenance prune job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
}

func (h *Handler) handleDockerAdminValidateDaemonConfig(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleDockerAdminApplyDaemonConfig(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) runDockerDaemonApplyJob(job store.Job, workflow []store.JobWorkflowStep, request dockerAdminValidateRequest) {
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

func (h *Handler) handleCreateStack(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) runCreateStackDeployJob(job store.Job, workflow []store.JobWorkflowStep, stackID string) {
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

func (h *Handler) handleGetDefinition(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleDeleteStack(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handlePutDefinition(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleGetResolvedConfig(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handlePostResolvedConfig(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleRunStackAction(w http.ResponseWriter, r *http.Request) {
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
		h.serviceMetrics.RequestStarted()

		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}
		defer func() {
			h.serviceMetrics.RequestFinished(time.Since(startedAt), recorder.status)
		}()

		next.ServeHTTP(recorder, r)
		duration := time.Since(startedAt)

		if h.logger != nil {
			h.logger.Info("http request",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Duration("duration", duration),
			)
		}
	})
}

func (h *Handler) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestid.Resolve(r.Header.Get(requestid.Header))
		w.Header().Set(requestid.Header, id)
		next.ServeHTTP(w, r.WithContext(requestid.WithContext(r.Context(), id)))
	})
}

func (h *Handler) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Referrer-Policy", "same-origin")
		headers.Set("X-Frame-Options", "DENY")

		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

const (
	maxJSONBodyBytes      int64 = 16 << 20
	maxLoginJSONBodyBytes int64 = 4 << 10
)

var errRequestBodyTooLarge = errors.New("request body too large")

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

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONWithLimit(w, r, destination, maxJSONBodyBytes)
}

func decodeJSONWithLimit(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(destination)
	if err == nil {
		return nil
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return errRequestBodyTooLarge
	}
	return err
}

func writeDecodeJSONError(w http.ResponseWriter, err error) {
	writeDecodeJSONErrorWithLimit(w, err, maxJSONBodyBytes)
}

func writeDecodeJSONErrorWithLimit(w http.ResponseWriter, err error, maxBytes int64) {
	if errors.Is(err, errRequestBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large.", map[string]any{
			"max_bytes": maxBytes,
		})
		return
	}
	writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
}

func writeContentTooLargeError(w http.ResponseWriter, err error) {
	details := map[string]any{}
	if maxBytes, ok := limitedio.MaxBytes(err); ok {
		details["max_bytes"] = maxBytes
	}
	writeError(w, http.StatusRequestEntityTooLarge, "content_too_large", "Content exceeds the safe processing limit.", details)
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
		session, err := h.auth.AuthenticateRequest(r.Context(), r)
		if err != nil {
			h.writeSessionAuthenticationError(w, err)
			return
		}
		http.SetCookie(w, h.auth.SessionCookie(session))

		next(w, r)
	}
}

func (h *Handler) writeSessionAuthenticationError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrUnauthorized) {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}
	h.logger.Error("validate session failed", slog.String("err", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal_error", "Failed to validate session.", nil)
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

func (h *Handler) appContext() context.Context {
	if h.appCtx == nil {
		return context.Background()
	}
	return h.appCtx
}

func (h *Handler) startWorker(run func()) {
	if run == nil {
		return
	}
	if h.workers == nil {
		run()
		return
	}
	if h.workers.Go(func(context.Context) { run() }) {
		return
	}

	// Shutdown won the admission race. Execute on the request goroutine with
	// the already-cancelled app context so the job can still use its bounded,
	// independent finalization path instead of being left in running state.
	run()
}

// Shutdown closes transport-owned hijacked connections. The composition root
// owns cancellation and shutdown of injected services and workers.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.closeWebSockets()
	return h.waitForWebSockets(ctx)
}

func (h *Handler) stackActionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(h.appContext(), h.stackActionTimeout())
}

func (h *Handler) dockerRegistryAuthContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(h.appContext(), h.dockerRegistryAuthTimeout())
}

func (h *Handler) jobFinalizationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (h *Handler) stackActionTimeout() time.Duration {
	if h.cfg.StackActionTimeout <= 0 {
		return 30 * time.Minute
	}
	return h.cfg.StackActionTimeout
}

func (h *Handler) dockerRegistryAuthTimeout() time.Duration {
	if h.cfg.DockerRegistryAuthTimeout <= 0 {
		return 5 * time.Minute
	}
	return h.cfg.DockerRegistryAuthTimeout
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
	return markWorkflowState(steps, index, "running")
}

func markWorkflowSucceeded(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	return markWorkflowState(steps, index, "succeeded")
}

func markWorkflowFailed(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	return markWorkflowState(steps, index, "failed")
}

func markWorkflowState(steps []store.JobWorkflowStep, index int, state string) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = state
	}
	return steps
}

func (h *Handler) runDeleteStackJob(job store.Job, workflow []store.JobWorkflowStep, stackID string, request stacks.DeleteStackRequest) {
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

func (h *Handler) finishDeleteStackFailure(job store.Job, workflow []store.JobWorkflowStep, index int, runContextErr, runErr error) {
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

func stackActionWorkflow(stackID, action string) []store.JobWorkflowStep {
	return []store.JobWorkflowStep{{
		Action:        action,
		State:         "running",
		TargetStackID: stackID,
	}}
}

func (h *Handler) validateStackActionRequest(ctx context.Context, stackID, action string) error {
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

func (h *Handler) runStackActionJob(job store.Job, workflow []store.JobWorkflowStep) {
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

func hasMaintenanceServiceExclusions(excluded map[string][]string) bool {
	for _, serviceNames := range excluded {
		if len(serviceNames) > 0 {
			return true
		}
	}
	return false
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

func (h *Handler) runDockerRegistryLoginJob(job store.Job, workflow []store.JobWorkflowStep, request dockerregistryauth.LoginRequest) {
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

func (h *Handler) runDockerRegistryLogoutJob(job store.Job, workflow []store.JobWorkflowStep, request dockerregistryauth.LogoutRequest) {
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

func (h *Handler) finishDockerRegistryJobSuccess(ctx context.Context, action string, job store.Job, workflow []store.JobWorkflowStep, step *store.JobEventStep, details map[string]any) {
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

func (h *Handler) finishDockerRegistryJobFailure(ctx context.Context, action string, job store.Job, workflow []store.JobWorkflowStep, step *store.JobEventStep, terminalState, errorCode, errorMessage, stepMessage string, details map[string]any) {
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

func (h *Handler) recordDockerRegistryAudit(ctx context.Context, action string, job store.Job, details map[string]any) error {
	return h.audit.RecordSystemEvent(ctx, action, job.RequestedBy, job.State, job.RequestedAt, job.FinishedAt, details)
}

func (h *Handler) finishTerminalJob(ctx context.Context, job store.Job, terminalState, errorCode, errorMessage string) (store.Job, error) {
	switch terminalState {
	case "timed_out":
		return h.jobs.FinishTimedOut(ctx, job, errorCode, errorMessage)
	case "cancelled":
		return h.jobs.FinishCancelled(ctx, job, errorCode, errorMessage)
	default:
		return h.jobs.FinishFailed(ctx, job, errorCode, errorMessage)
	}
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

func splitProgressOutput(raw string) []string {
	lines := strings.Split(raw, "\n")
	trimmed := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		trimmed = append(trimmed, line)
	}
	return trimmed
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
