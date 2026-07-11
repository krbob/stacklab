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

type dockerAdminValidateRequest struct {
	Settings   dockeradmin.ManagedSettings `json:"settings"`
	RemoveKeys []string                    `json:"remove_keys"`
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
