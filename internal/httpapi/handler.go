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

func (h *Handler) jobFinalizationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (h *Handler) stackActionTimeout() time.Duration {
	if h.cfg.StackActionTimeout <= 0 {
		return 30 * time.Minute
	}
	return h.cfg.StackActionTimeout
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
