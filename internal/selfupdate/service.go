package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"log/slog"

	"stacklab/internal/audit"
	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/notifications"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

const (
	runtimeStateKey            = "self_update_runtime_v1"
	defaultPackageName         = "stacklab"
	defaultSelfUpdateMessage   = "Stacklab self-update is not configured yet."
	unsupportedInstallMessage  = "Stacklab self-update is only available for APT installs."
	packageManagerErrorMessage = "APT package metadata is unavailable on this host."
)

var (
	ErrUnsupported  = errors.New("self-update is not supported")
	ErrInvalidState = errors.New("invalid self-update state")
)

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type OverviewResponse struct {
	CurrentVersion  string               `json:"current_version"`
	InstallMode     string               `json:"install_mode"`
	Package         PackageStatus        `json:"package"`
	WriteCapability WriteCapability      `json:"write_capability"`
	Runtime         *RuntimeStatus       `json:"runtime,omitempty"`
}

type PackageStatus struct {
	Supported         bool   `json:"supported"`
	Message           string `json:"message,omitempty"`
	Name              string `json:"name"`
	InstalledVersion  string `json:"installed_version,omitempty"`
	CandidateVersion  string `json:"candidate_version,omitempty"`
	ConfiguredChannel string `json:"configured_channel,omitempty"`
	UpdateAvailable   bool   `json:"update_available"`
}

type WriteCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

type RuntimeStatus struct {
	JobID            string     `json:"job_id,omitempty"`
	PendingFinalize  bool       `json:"pending_finalize"`
	RequestedVersion string     `json:"requested_version,omitempty"`
	InstalledVersion string     `json:"installed_version,omitempty"`
	Result           string     `json:"result,omitempty"`
	Message          string     `json:"message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type ApplyRequest struct {
	ExpectedCandidateVersion string `json:"expected_candidate_version,omitempty"`
	RefreshPackageIndex      bool   `json:"refresh_package_index"`
}

type ApplyResponse struct {
	Started bool             `json:"started"`
	Job     store.Job        `json:"job"`
	Package PackageStatus    `json:"package"`
	Runtime *RuntimeStatus   `json:"runtime,omitempty"`
}

type runtimeState struct {
	JobID            string     `json:"job_id,omitempty"`
	RequestedVersion string     `json:"requested_version,omitempty"`
	InstalledVersion string     `json:"installed_version,omitempty"`
	Result           string     `json:"result,omitempty"`
	Message          string     `json:"message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	PendingFinalize  bool       `json:"pending_finalize"`
}

type Service struct {
	cfg           config.Config
	store         *store.Store
	jobs          *jobs.Service
	audit         *audit.Service
	notifications *notifications.Service
	logger        *slog.Logger
	runCommand    commandRunner
	now           func() time.Time
	pollInterval  time.Duration
}

func NewService(cfg config.Config, appStore *store.Store, jobService *jobs.Service, auditService *audit.Service, notificationService *notifications.Service, logger *slog.Logger) *Service {
	return &Service{
		cfg:           cfg,
		store:         appStore,
		jobs:          jobService,
		audit:         auditService,
		notifications: notificationService,
		logger:        logger,
		runCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		now:          func() time.Time { return time.Now().UTC() },
		pollInterval: 15 * time.Second,
	}
}

func (s *Service) StartBackground(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	s.reconcilePendingResult(ctx)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcilePendingResult(ctx)
		}
	}
}

func (s *Service) Overview(ctx context.Context) (OverviewResponse, error) {
	packageStatus := s.inspectPackageStatus(ctx)
	runtimeState, err := s.loadRuntimeState(ctx)
	if err != nil {
		return OverviewResponse{}, err
	}

	return OverviewResponse{
		CurrentVersion:  stacks.AppVersion,
		InstallMode:     detectInstallMode(),
		Package:         packageStatus,
		WriteCapability: s.writeCapability(packageStatus),
		Runtime:         runtimeStatusView(runtimeState),
	}, nil
}

func (s *Service) Apply(ctx context.Context, request ApplyRequest, requestedBy string) (ApplyResponse, error) {
	if !request.RefreshPackageIndex {
		request.RefreshPackageIndex = true
	}

	overview, err := s.Overview(ctx)
	if err != nil {
		return ApplyResponse{}, err
	}
	if !overview.Package.Supported {
		return ApplyResponse{}, ErrUnsupported
	}
	if !overview.WriteCapability.Supported {
		return ApplyResponse{}, ErrUnsupported
	}
	if !overview.Package.UpdateAvailable {
		return ApplyResponse{}, fmt.Errorf("%w: stacklab is already up to date", ErrInvalidState)
	}
	if expected := strings.TrimSpace(request.ExpectedCandidateVersion); expected != "" && overview.Package.CandidateVersion != expected {
		return ApplyResponse{}, fmt.Errorf("%w: available version changed from %s to %s", ErrInvalidState, expected, overview.Package.CandidateVersion)
	}

	active, err := s.jobs.ListActive(ctx)
	if err != nil {
		return ApplyResponse{}, err
	}
	for _, item := range active.Items {
		if item.Action == "self_update_stacklab" {
			return ApplyResponse{}, fmt.Errorf("%w: a Stacklab self-update job is already running", ErrInvalidState)
		}
	}

	job, err := s.jobs.Start(ctx, "", "self_update_stacklab", requestedBy)
	if err != nil {
		return ApplyResponse{}, err
	}
	workflow := buildWorkflow(request.RefreshPackageIndex)
	job, err = s.jobs.UpdateWorkflow(ctx, job, workflow)
	if err != nil {
		job, _ = s.jobs.FinishFailed(ctx, job, "self_update_prepare_failed", err.Error())
		return ApplyResponse{}, err
	}

	startedAt := s.now()
	if err := s.saveRuntimeState(ctx, runtimeState{
		JobID:            job.ID,
		RequestedVersion: overview.Package.CandidateVersion,
		StartedAt:        &startedAt,
		PendingFinalize:  false,
	}); err != nil {
		job, _ = s.jobs.FinishFailed(ctx, job, "self_update_prepare_failed", err.Error())
		return ApplyResponse{}, err
	}

	if err := s.startHelper(job.ID, overview.Package.CandidateVersion, request.RefreshPackageIndex); err != nil {
		job, _ = s.jobs.FinishFailed(ctx, job, "self_update_start_failed", err.Error())
		_ = s.saveRuntimeState(ctx, runtimeState{})
		return ApplyResponse{}, err
	}

	runtime, _ := s.loadRuntimeState(ctx)
	return ApplyResponse{
		Started: true,
		Job:     job,
		Package: overview.Package,
		Runtime: runtimeStatusView(runtime),
	}, nil
}

func (s *Service) inspectPackageStatus(ctx context.Context) PackageStatus {
	status := PackageStatus{
		Name: defaultString(s.cfg.SelfUpdatePackageName, defaultPackageName),
	}

	installedVersion, installed, err := s.installedVersion(ctx, status.Name)
	if err != nil {
		status.Message = packageManagerErrorMessage
		return status
	}
	if !installed {
		switch detectInstallMode() {
		case "tarball":
			status.Message = unsupportedInstallMessage
		default:
			status.Message = packageManagerErrorMessage
		}
		return status
	}

	status.Supported = true
	status.InstalledVersion = installedVersion
	status.CandidateVersion, _ = s.candidateVersion(ctx, status.Name)
	status.ConfiguredChannel = detectConfiguredChannel()
	status.UpdateAvailable = status.CandidateVersion != "" && status.CandidateVersion != status.InstalledVersion
	return status
}

func (s *Service) writeCapability(pkg PackageStatus) WriteCapability {
	if !pkg.Supported {
		if pkg.Message != "" {
			return WriteCapability{Supported: false, Reason: pkg.Message}
		}
		return WriteCapability{Supported: false, Reason: unsupportedInstallMessage}
	}
	helperPath := strings.TrimSpace(s.cfg.SelfUpdateHelperPath)
	if helperPath == "" {
		return WriteCapability{Supported: false, Reason: defaultSelfUpdateMessage}
	}
	if info, err := os.Stat(helperPath); err != nil || info.IsDir() {
		return WriteCapability{Supported: false, Reason: fmt.Sprintf("Stacklab self-update helper is unavailable at %s.", helperPath)}
	}
	if !s.cfg.SelfUpdateUseSudo && os.Geteuid() != 0 {
		return WriteCapability{Supported: false, Reason: "Stacklab self-update helper requires sudo or a root-owned Stacklab service."}
	}
	if s.cfg.SelfUpdateUseSudo {
		probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		output, err := s.runHelperCommand(probeCtx, "probe")
		if err != nil {
			message := strings.TrimSpace(string(output))
			lower := strings.ToLower(message)
			switch {
			case strings.Contains(lower, "no new privileges"):
				return WriteCapability{Supported: false, Reason: "Stacklab self-update helper requires NoNewPrivileges=false in stacklab.service."}
			case strings.Contains(lower, "a password is required"),
				strings.Contains(lower, "not allowed to execute"),
				strings.Contains(lower, "may not run sudo"):
				return WriteCapability{Supported: false, Reason: "Stacklab self-update helper sudoers is not configured correctly."}
			default:
				if message == "" {
					message = "Stacklab self-update helper could not be executed successfully."
				}
				return WriteCapability{Supported: false, Reason: message}
			}
		}
	}
	return WriteCapability{Supported: true}
}

func (s *Service) installedVersion(ctx context.Context, packageName string) (string, bool, error) {
	output, err := s.runCommand(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev}\t${Version}\n", packageName)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", false, nil
		}
		return "", false, err
	}
	line := strings.TrimSpace(string(output))
	if line == "" {
		return "", false, nil
	}
	parts := strings.Fields(line)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "ii") {
		return "", false, nil
	}
	return parts[1], true, nil
}

func (s *Service) candidateVersion(ctx context.Context, packageName string) (string, error) {
	output, err := s.runCommand(ctx, "apt-cache", "policy", packageName)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Candidate:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Candidate:")), nil
		}
	}
	return "", nil
}

func detectInstallMode() string {
	executable, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil {
		executable = resolved
	}
	switch {
	case strings.Contains(executable, "/usr/lib/stacklab/"):
		return "apt"
	case strings.Contains(executable, "/opt/stacklab/app/"):
		return "tarball"
	default:
		return "unknown"
	}
}

func detectConfiguredChannel() string {
	candidates := []string{"/etc/apt/sources.list"}
	if entries, err := filepath.Glob("/etc/apt/sources.list.d/*.list"); err == nil {
		candidates = append(candidates, entries...)
	}

	channels := map[string]struct{}{}
	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			channel := parseAPTChannel(line)
			if channel != "" {
				channels[channel] = struct{}{}
			}
		}
	}

	if len(channels) == 1 {
		for channel := range channels {
			return channel
		}
	}
	if len(channels) > 1 {
		keys := make([]string, 0, len(channels))
		for channel := range channels {
			keys = append(keys, channel)
		}
		sort.Strings(keys)
		return strings.Join(keys, ",")
	}
	return ""
}

func parseAPTChannel(line string) string {
	line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
	if line == "" || !strings.HasPrefix(line, "deb ") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ""
	}
	repoIndex := -1
	for i, field := range fields {
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			repoIndex = i
			break
		}
	}
	if repoIndex == -1 || repoIndex+1 >= len(fields) {
		return ""
	}
	repo := fields[repoIndex]
	if !strings.Contains(repo, "/stacklab/apt") {
		return ""
	}
	channel := strings.TrimSpace(fields[repoIndex+1])
	switch channel {
	case "stable", "nightly":
		return channel
	default:
		return ""
	}
}

func buildWorkflow(refreshPackageIndex bool) []store.JobWorkflowStep {
	steps := make([]store.JobWorkflowStep, 0, 3)
	if refreshPackageIndex {
		steps = append(steps, store.JobWorkflowStep{Action: "apt_update", State: "queued"})
	}
	steps = append(steps,
		store.JobWorkflowStep{Action: "upgrade_package", State: "queued"},
		store.JobWorkflowStep{Action: "verify_restart", State: "queued"},
	)
	return steps
}

func (s *Service) startHelper(jobID, requestedVersion string, refreshPackageIndex bool) error {
	commandName, commandArgs, err := s.helperCommand("run",
		"--db-path", s.cfg.DatabasePath,
		"--job-id", jobID,
		"--package-name", defaultString(s.cfg.SelfUpdatePackageName, defaultPackageName),
		"--health-url", s.cfg.SelfUpdateHealthURL,
		"--service-unit", s.cfg.SystemdUnitName,
		"--runtime-key", runtimeStateKey,
	)
	if err != nil {
		return err
	}
	if requestedVersion != "" {
		commandArgs = append(commandArgs, "--requested-version", requestedVersion)
	}
	if !refreshPackageIndex {
		commandArgs = append(commandArgs, "--skip-apt-update")
	}

	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/null: %w", err)
	}
	defer devnull.Close()

	cmd := exec.Command(commandName, commandArgs...)
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start self-update helper: %w", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

func (s *Service) runHelperCommand(ctx context.Context, args ...string) ([]byte, error) {
	commandName, commandArgs, err := s.helperCommand(args...)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, commandName, commandArgs...)
	return cmd.CombinedOutput()
}

func (s *Service) helperCommand(args ...string) (string, []string, error) {
	helperPath := strings.TrimSpace(s.cfg.SelfUpdateHelperPath)
	if helperPath == "" {
		return "", nil, ErrUnsupported
	}
	if s.cfg.SelfUpdateUseSudo {
		return "sudo", append([]string{helperPath}, args...), nil
	}
	return helperPath, args, nil
}

func (s *Service) reconcilePendingResult(ctx context.Context) {
	state, err := s.loadRuntimeState(ctx)
	if err != nil || !state.PendingFinalize || state.JobID == "" {
		return
	}

	job, err := s.jobs.Get(ctx, state.JobID)
	if err != nil {
		return
	}

	if s.audit != nil {
		details := map[string]any{
			"install_mode":       "apt",
			"requested_version":  state.RequestedVersion,
			"installed_version":  state.InstalledVersion,
			"result_message":     state.Message,
			"configured_channel": detectConfiguredChannel(),
		}
		_ = s.audit.RecordJob(ctx, job, details)
	}
	if s.notifications != nil {
		_ = s.notifications.DispatchJob(ctx, job)
	}

	state.PendingFinalize = false
	if err := s.saveRuntimeState(ctx, state); err != nil && s.logger != nil {
		s.logger.Warn("persist self-update runtime reconciliation failed", slog.String("err", err.Error()))
	}
}

func runtimeStatusView(state runtimeState) *RuntimeStatus {
	if state.JobID == "" && state.Result == "" && state.RequestedVersion == "" && !state.PendingFinalize {
		return nil
	}
	return &RuntimeStatus{
		JobID:            state.JobID,
		PendingFinalize:  state.PendingFinalize,
		RequestedVersion: state.RequestedVersion,
		InstalledVersion: state.InstalledVersion,
		Result:           state.Result,
		Message:          state.Message,
		StartedAt:        state.StartedAt,
		FinishedAt:       state.FinishedAt,
	}
}

func (s *Service) loadRuntimeState(ctx context.Context) (runtimeState, error) {
	raw, ok, err := s.store.AppSetting(ctx, runtimeStateKey)
	if err != nil {
		return runtimeState{}, err
	}
	if !ok {
		return runtimeState{}, nil
	}
	var state runtimeState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return runtimeState{}, fmt.Errorf("parse self-update runtime state: %w", err)
	}
	return state, nil
}

func (s *Service) saveRuntimeState(ctx context.Context, state runtimeState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal self-update runtime state: %w", err)
	}
	return s.store.SetAppSetting(ctx, runtimeStateKey, string(payload), s.now())
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
