package stacks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"stacklab/internal/atomicfile"
	"stacklab/internal/config"
	"stacklab/internal/store"

	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound          = errors.New("stack not found")
	ErrInvalidState      = errors.New("invalid state")
	ErrConflict          = errors.New("conflict")
	ErrUnsupportedAction = errors.New("unsupported action")
	ErrDockerUnavailable = errors.New("docker unavailable")
	stackIDRegexp        = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	composeCLIMu         sync.Mutex
	composeCLICached     *composeCLI
)

var (
	AppVersion = "0.1.0-dev"
	AppCommit  = "dev"
)

func ResetComposeCLICacheForTests() {
	composeCLIMu.Lock()
	defer composeCLIMu.Unlock()
	composeCLICached = nil
}

type composeCLI struct {
	command string
	prefix  []string
}

type ServiceReader struct {
	cfg                         config.Config
	store                       *store.Store
	logger                      *slog.Logger
	hostShell                   bool
	stats                       *StatsCollector
	updateStatus                func() map[string]ImageUpdateState
	cacheUpdateStatuses         func([]store.ImageUpdateStatus)
	definitionWarningMu         sync.Mutex
	definitionWarningLog        map[string]string
	afterScanDefinitionsForTest func()
}

func (s *ServiceReader) AttachStore(appStore *store.Store) {
	s.store = appStore
}

// AttachStatsCollector wires the host-level stats collector; list responses
// enrich items from its snapshot when present.
func (s *ServiceReader) AttachStatsCollector(collector *StatsCollector) {
	s.stats = collector
}

// AttachUpdateStatus wires the per-image update state provider (Slice B);
// list responses roll it up per stack when present.
func (s *ServiceReader) AttachUpdateStatus(provider func() map[string]ImageUpdateState) {
	s.updateStatus = provider
}

// AttachUpdateStatusCacheUpdater wires a cache update hook used when stack
// actions persist image update invalidations outside the imageupdates service.
func (s *ServiceReader) AttachUpdateStatusCacheUpdater(updater func([]store.ImageUpdateStatus)) {
	s.cacheUpdateStatuses = updater
}

// AllImageRefs returns the unique image references used by managed stacks
// (image and hybrid services only — build-mode services have no registry tag).
func (s *ServiceReader) AllImageRefs(ctx context.Context) ([]string, error) {
	allStacks, err := s.readStacks(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	refs := make([]string, 0, len(allStacks))
	for _, stack := range allStacks {
		for _, service := range stack.Services {
			if service.ImageRef == nil || *service.ImageRef == "" {
				continue
			}
			if _, ok := seen[*service.ImageRef]; ok {
				continue
			}
			seen[*service.ImageRef] = struct{}{}
			refs = append(refs, *service.ImageRef)
		}
	}
	sort.Strings(refs)
	return refs, nil
}

// rollupUpdates aggregates per-image update states into the stack-level
// summary. nil when nothing about the stack's images is known yet.
func rollupUpdates(services []Service, statusByImage map[string]ImageUpdateState) *StackUpdates {
	if len(statusByImage) == 0 {
		return nil
	}

	known := 0
	upToDate := 0
	withUpdates := 0
	var checkedAt time.Time
	for _, service := range services {
		if service.ImageRef == nil {
			continue
		}
		status, ok := statusByImage[*service.ImageRef]
		if !ok {
			continue
		}
		known++
		if status.CheckedAt.After(checkedAt) {
			checkedAt = status.CheckedAt
		}
		switch status.State {
		case "available":
			withUpdates++
		case "up_to_date":
			upToDate++
		}
	}
	if known == 0 {
		return nil
	}

	state := "unknown"
	if withUpdates > 0 {
		state = "available"
	} else if upToDate == known {
		state = "up_to_date"
	}
	return &StackUpdates{State: state, ServicesWithUpdates: withUpdates, CheckedAt: checkedAt}
}

type MaintenanceStepOptions struct {
	RemoveOrphans bool
	ServiceNames  []string
}

func NewServiceReader(cfg config.Config, logger *slog.Logger) *ServiceReader {
	return &ServiceReader{
		cfg:                  cfg,
		logger:               logger,
		definitionWarningLog: map[string]string{},
	}
}

func (s *ServiceReader) Session() SessionResponse {
	return SessionResponse{
		Authenticated: true,
		User: &SessionUser{
			ID:          "local",
			DisplayName: "Local Operator",
		},
		Features: FeatureFlags{
			HostShell: s.hostShell,
		},
	}
}

func (s *ServiceReader) Meta(ctx context.Context) MetaResponse {
	return MetaResponse{
		App: AppMeta{
			Name:    "Stacklab",
			Version: AppVersion,
		},
		Environment: EnvironmentMeta{
			StackRoot: s.cfg.RootDir,
			Platform:  runtime.GOOS + "-" + runtime.GOARCH,
		},
		Docker: DockerMeta{
			EngineVersion:  detectDockerEngineVersion(ctx),
			ComposeVersion: detectComposeVersion(ctx),
		},
		Features: FeatureFlags{
			HostShell: s.hostShell,
		},
	}
}

func (s *ServiceReader) List(ctx context.Context, query ListQuery) (StackListResponse, error) {
	allStacks, err := s.readStacks(ctx)
	if err != nil {
		return StackListResponse{}, err
	}

	items := make([]StackListItem, 0, len(allStacks))
	summary := StackListSummary{}

	var statsByProject map[string]StackStats
	if s.stats != nil {
		statsByProject = s.stats.Snapshot()
	}
	var updateStatusByImage map[string]ImageUpdateState
	if s.updateStatus != nil {
		updateStatusByImage = s.updateStatus()
	}

	for _, stack := range allStacks {
		if query.Search != "" && !strings.Contains(stack.ID, strings.ToLower(query.Search)) {
			continue
		}

		var stackStats *StackStats
		if sample, ok := statsByProject[stack.ID]; ok && stack.runningContainerCount() > 0 {
			stackStats = &sample
		}

		items = append(items, StackListItem{
			StackHeader:  stack.header(),
			ServiceCount: ServiceCount{Defined: len(stack.Services), Running: stack.runningServiceCount()},
			LastAction:   nil,
			Stats:        stackStats,
			Updates:      rollupUpdates(stack.Services, updateStatusByImage),
		})

		summary.StackCount++
		switch stack.RuntimeState {
		case RuntimeStateRunning:
			summary.RunningCount++
		case RuntimeStateStopped:
			summary.StoppedCount++
		case RuntimeStateError:
			summary.ErrorCount++
		case RuntimeStateDefined:
			summary.DefinedCount++
		case RuntimeStateOrphaned:
			summary.OrphanedCount++
		}
		summary.ContainerCount.Total += len(stack.Containers)
		summary.ContainerCount.Running += stack.runningContainerCount()
	}

	sortStackListItems(items, query.Sort)

	return StackListResponse{
		Items:   items,
		Summary: summary,
	}, nil
}

func (s *ServiceReader) Get(ctx context.Context, stackID string) (StackDetailResponse, error) {
	if !IsValidStackID(stackID) {
		return StackDetailResponse{}, ErrNotFound
	}

	allStacks, err := s.readStacks(ctx)
	if err != nil {
		return StackDetailResponse{}, err
	}

	for _, stack := range allStacks {
		if stack.ID != stackID {
			continue
		}

		return StackDetailResponse{
			Stack: StackDetail{
				StackHeader:      stack.header(),
				RootPath:         stack.RootPath,
				ComposeFilePath:  stack.ComposeFilePath,
				EnvFilePath:      stack.EnvFilePath,
				ConfigPath:       stack.ConfigPath,
				DataPath:         stack.DataPath,
				Capabilities:     stack.capabilities(),
				AvailableActions: stack.availableActions(),
				Services:         stack.Services,
				Containers:       stack.Containers,
				LastDeployedAt:   stack.LastDeployedAt,
				LastAction:       nil,
			},
		}, nil
	}

	return StackDetailResponse{}, ErrNotFound
}

func (s *ServiceReader) Definition(ctx context.Context, stackID string) (StackDefinitionResponse, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return StackDefinitionResponse{}, err
	}
	if stack.RuntimeState == RuntimeStateOrphaned {
		return StackDefinitionResponse{}, ErrInvalidState
	}

	composeInfo, err := os.Stat(stack.ComposeFilePath)
	if err != nil {
		return StackDefinitionResponse{}, fmt.Errorf("stat compose file: %w", err)
	}
	composeContent, err := os.ReadFile(stack.ComposeFilePath)
	if err != nil {
		return StackDefinitionResponse{}, fmt.Errorf("read compose file: %w", err)
	}

	envContent := ""
	envExists := false
	var envModifiedAt *time.Time
	if envInfo, err := os.Stat(stack.EnvFilePath); err == nil {
		envExists = true
		modifiedAt := envInfo.ModTime().UTC()
		envModifiedAt = &modifiedAt
		envBytes, err := os.ReadFile(stack.EnvFilePath)
		if err != nil {
			return StackDefinitionResponse{}, fmt.Errorf("read env file: %w", err)
		}
		envContent = string(envBytes)
	} else if !os.IsNotExist(err) {
		return StackDefinitionResponse{}, fmt.Errorf("stat env file: %w", err)
	}

	configState := stack.ConfigState
	preview := s.resolveCurrent(ctx, stack)
	if !preview.Valid {
		configState = ConfigStateInvalid
	}

	return StackDefinitionResponse{
		StackID: stack.ID,
		Files: StackDefinitionFiles{
			ComposeYAML: ComposeYAMLFile{
				Path:       stack.ComposeFilePath,
				Content:    string(composeContent),
				ModifiedAt: composeInfo.ModTime().UTC(),
			},
			Env: EnvFile{
				Path:       stack.EnvFilePath,
				Content:    envContent,
				Exists:     envExists,
				ModifiedAt: envModifiedAt,
			},
		},
		ConfigState: configState,
	}, nil
}

func (s *ServiceReader) ResolvedConfigCurrent(ctx context.Context, stackID string, source string) (ResolvedConfigResponse, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	if stack.RuntimeState == RuntimeStateOrphaned {
		return ResolvedConfigResponse{}, ErrInvalidState
	}
	if source == "last_valid" {
		return s.resolveLastValid(ctx, stack)
	}

	return s.resolveCurrent(ctx, stack), nil
}

func (s *ServiceReader) resolveLastValid(ctx context.Context, stack discoveredStack) (ResolvedConfigResponse, error) {
	if s.store == nil {
		return ResolvedConfigResponse{}, ErrInvalidState
	}
	baseline, ok, err := s.store.StackDeployBaseline(ctx, stack.ID)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	if !ok {
		return ResolvedConfigResponse{}, ErrInvalidState
	}

	envPath, cleanup, err := writeTempEnvFile(s.cfg.DataDir, baseline.Env)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	defer cleanup()

	content, resolveErr := runComposeConfig(ctx, stack.RootPath, "-", envPath, baseline.ComposeYAML)
	if resolveErr != nil {
		return ResolvedConfigResponse{
			StackID: stack.ID,
			Valid:   false,
			Error: &ErrorDetail{
				Code:    "validation_failed",
				Message: resolveErr.Error(),
				Details: nil,
			},
		}, nil
	}

	return ResolvedConfigResponse{
		StackID:  stack.ID,
		Valid:    true,
		Content:  content,
		Warnings: LintCompose([]byte(baseline.ComposeYAML)),
	}, nil
}

func (s *ServiceReader) ResolvedConfigDraft(ctx context.Context, stackID string, request ResolvedConfigRequest) (ResolvedConfigResponse, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	if stack.RuntimeState == RuntimeStateOrphaned {
		return ResolvedConfigResponse{}, ErrInvalidState
	}

	envPath, cleanup, err := writeTempEnvFile(s.cfg.DataDir, request.Env)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	defer cleanup()

	content, resolveErr := runComposeConfig(ctx, stack.RootPath, "-", envPath, request.ComposeYAML)
	if resolveErr != nil {
		return ResolvedConfigResponse{
			StackID: stack.ID,
			Valid:   false,
			Error: &ErrorDetail{
				Code:    "validation_failed",
				Message: resolveErr.Error(),
				Details: nil,
			},
		}, nil
	}

	return ResolvedConfigResponse{
		StackID:  stack.ID,
		Valid:    true,
		Content:  content,
		Warnings: LintCompose([]byte(request.ComposeYAML)),
	}, nil
}

func (s *ServiceReader) SaveDefinition(ctx context.Context, stackID string, request UpdateDefinitionRequest) (ResolvedConfigResponse, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	if stack.RuntimeState == RuntimeStateOrphaned {
		return ResolvedConfigResponse{}, ErrInvalidState
	}

	if request.ExpectedRevision != nil {
		if err := ensureDefinitionRevision(stack, *request.ExpectedRevision); err != nil {
			return ResolvedConfigResponse{}, err
		}
	}

	if err := writeFileAtomic(stack.ComposeFilePath, request.ComposeYAML); err != nil {
		return ResolvedConfigResponse{}, fmt.Errorf("write compose file: %w", err)
	}

	if err := writeEnvFile(stack.EnvFilePath, request.Env); err != nil {
		return ResolvedConfigResponse{}, fmt.Errorf("write env file: %w", err)
	}

	if !request.ValidateAfterSave {
		return ResolvedConfigResponse{
			StackID: stack.ID,
			Valid:   true,
		}, nil
	}

	refreshedStack, err := s.findStack(ctx, stackID)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	return s.resolveCurrent(ctx, refreshedStack), nil
}

func (s *ServiceReader) EnsureCreateStackAvailable(ctx context.Context, stackID string) error {
	if !IsValidStackID(stackID) {
		return ErrInvalidState
	}

	if _, err := s.findStack(ctx, stackID); err == nil {
		return ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	paths := stackPaths(s.cfg.RootDir, stackID)
	for _, path := range []string{paths.RootPath, paths.ConfigPath, paths.DataPath} {
		exists, err := pathExists(path)
		if err != nil {
			return err
		}
		if exists {
			return ErrConflict
		}
	}

	return nil
}

func (s *ServiceReader) CreateStack(ctx context.Context, request CreateStackRequest) error {
	if err := s.EnsureCreateStackAvailable(ctx, request.StackID); err != nil {
		return err
	}
	if strings.TrimSpace(request.TemplateID) != "" {
		rendered, err := s.RenderTemplate(ctx, request.TemplateID, request.Variables)
		if err != nil {
			return err
		}
		request.ComposeYAML = rendered
	}

	paths := stackPaths(s.cfg.RootDir, request.StackID)

	if err := os.MkdirAll(paths.RootPath, 0o755); err != nil {
		return fmt.Errorf("create stack root: %w", err)
	}
	if request.CreateConfigDir {
		if err := os.MkdirAll(paths.ConfigPath, 0o755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	if request.CreateDataDir {
		if err := os.MkdirAll(paths.DataPath, 0o755); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}
	}
	if err := writeFileAtomic(paths.ComposeFilePath, request.ComposeYAML); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}
	if err := writeEnvFile(paths.EnvFilePath, request.Env); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	return nil
}

func (s *ServiceReader) DeleteStack(ctx context.Context, stackID string, request DeleteStackRequest) error {
	if request.RemoveRuntime {
		if err := s.RemoveRuntime(ctx, stackID); err != nil {
			return err
		}
	}
	if request.RemoveDefinition {
		if err := s.RemoveDefinition(ctx, stackID); err != nil {
			return err
		}
	}
	if request.RemoveConfig {
		if err := s.RemoveConfigDir(stackID); err != nil {
			return err
		}
	}
	if request.RemoveData {
		if err := s.RemoveDataDir(stackID); err != nil {
			return err
		}
	}

	return nil
}

func (s *ServiceReader) RemoveRuntime(ctx context.Context, stackID string) error {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return err
	}
	if err := ensureDockerRuntimeAvailable(ctx); err != nil {
		return err
	}
	return s.removeRuntime(ctx, stack)
}

func (s *ServiceReader) RemoveDefinition(ctx context.Context, stackID string) error {
	paths := stackPaths(s.cfg.RootDir, stackID)
	if err := removeFileIfExists(paths.ComposeFilePath); err != nil {
		return fmt.Errorf("remove compose file: %w", err)
	}
	if err := removeFileIfExists(paths.EnvFilePath); err != nil {
		return fmt.Errorf("remove env file: %w", err)
	}
	if err := removeDirIfEmpty(paths.RootPath); err != nil {
		return fmt.Errorf("remove stack root: %w", err)
	}
	if s.store != nil {
		if err := s.store.DeleteStackDeployBaseline(ctx, stackID); err != nil {
			return err
		}
	}
	return nil
}

func (s *ServiceReader) RecordDeployBaseline(ctx context.Context, stackID, jobID string, deployedAt time.Time) error {
	if s.store == nil {
		return nil
	}
	if !IsValidStackID(stackID) {
		return ErrNotFound
	}
	paths := stackPaths(s.cfg.RootDir, stackID)
	composeBytes, err := os.ReadFile(paths.ComposeFilePath)
	if err != nil {
		return fmt.Errorf("read compose file for deploy baseline: %w", err)
	}
	env := ""
	envExists := false
	if envBytes, err := os.ReadFile(paths.EnvFilePath); err == nil {
		env = string(envBytes)
		envExists = true
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read env file for deploy baseline: %w", err)
	}
	return s.store.UpsertStackDeployBaseline(ctx, store.StackDeployBaseline{
		StackID:        stackID,
		ComposeSHA256:  contentSHA256(string(composeBytes)),
		EnvSHA256:      contentSHA256(env),
		ComposeYAML:    string(composeBytes),
		Env:            env,
		EnvExists:      envExists,
		LastDeployedAt: deployedAt.UTC(),
		LastJobID:      jobID,
	})
}

func (s *ServiceReader) InvalidateImageUpdateStatus(ctx context.Context, stackID string, serviceNames []string) error {
	if s.store == nil {
		return nil
	}
	refs, err := s.imageRefsForStack(ctx, stackID, serviceNames)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	statuses := make([]store.ImageUpdateStatus, 0, len(refs))
	for _, ref := range refs {
		status := store.ImageUpdateStatus{
			ImageRef:  ref,
			State:     "unknown",
			CheckedAt: now,
		}
		if err := s.store.UpsertImageUpdateStatus(ctx, status); err != nil {
			return err
		}
		statuses = append(statuses, status)
	}
	if s.cacheUpdateStatuses != nil {
		s.cacheUpdateStatuses(statuses)
	}
	return nil
}

func (s *ServiceReader) imageRefsForStack(ctx context.Context, stackID string, serviceNames []string) ([]string, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return nil, err
	}
	targeted := map[string]struct{}{}
	for _, serviceName := range serviceNames {
		targeted[serviceName] = struct{}{}
	}
	refs := []string{}
	seen := map[string]struct{}{}
	for _, service := range stack.Services {
		if service.ImageRef == nil || *service.ImageRef == "" {
			continue
		}
		if len(targeted) > 0 {
			if _, ok := targeted[service.Name]; !ok {
				continue
			}
		}
		if _, ok := seen[*service.ImageRef]; ok {
			continue
		}
		seen[*service.ImageRef] = struct{}{}
		refs = append(refs, *service.ImageRef)
	}
	sort.Strings(refs)
	return refs, nil
}

func (s *ServiceReader) RemoveConfigDir(stackID string) error {
	paths := stackPaths(s.cfg.RootDir, stackID)
	if err := removeDirIfExists(paths.ConfigPath); err != nil {
		return fmt.Errorf("remove config dir: %w", err)
	}
	return nil
}

func (s *ServiceReader) RemoveDataDir(stackID string) error {
	paths := stackPaths(s.cfg.RootDir, stackID)
	if err := removeDirIfExists(paths.DataPath); err != nil {
		return fmt.Errorf("remove data dir: %w", err)
	}
	return nil
}

func (s *ServiceReader) RunAction(ctx context.Context, stackID, action string) error {
	_, err := s.RunActionWithOutput(ctx, stackID, action)
	return err
}

func (s *ServiceReader) RunActionWithOutput(ctx context.Context, stackID, action string) (string, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return "", err
	}

	switch action {
	case "validate", "up", "down", "stop", "restart", "pull", "build", "recreate":
	default:
		return "", ErrUnsupportedAction
	}

	if !containsString(stack.availableActions(), action) {
		return "", ErrInvalidState
	}

	switch action {
	case "validate":
		resolved := s.resolveCurrent(ctx, stack)
		if !resolved.Valid {
			return "", fmt.Errorf("validate current config: %s", resolved.Error.Message)
		}
		return "", nil
	case "up":
		return s.runComposeActionOutput(ctx, stack, "up", "-d")
	case "down":
		return s.removeRuntimeOutput(ctx, stack)
	case "stop":
		if stack.ConfigState == ConfigStateInvalid {
			return s.runContainerActionOutput(ctx, stack, "stop")
		}
		return s.runComposeActionOutput(ctx, stack, "stop")
	case "restart":
		if stack.ConfigState == ConfigStateInvalid {
			return s.runContainerActionOutput(ctx, stack, "restart")
		}
		return s.runComposeActionOutput(ctx, stack, "restart")
	case "pull":
		return s.runComposeActionOutput(ctx, stack, "pull")
	case "build":
		return s.runComposeActionOutput(ctx, stack, "build")
	case "recreate":
		return s.runComposeActionOutput(ctx, stack, "up", "-d", "--force-recreate")
	default:
		return "", ErrUnsupportedAction
	}
}

func (s *ServiceReader) RunMaintenanceStep(ctx context.Context, stackID, action string, options MaintenanceStepOptions) (string, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return "", err
	}
	if !containsString(stack.availableActions(), "up") {
		return "", ErrInvalidState
	}

	switch action {
	case "pull":
		return s.runComposeActionOutput(ctx, stack, "pull", options.ServiceNames...)
	case "build":
		return s.runComposeActionOutput(ctx, stack, "build", options.ServiceNames...)
	case "up":
		args := []string{"-d"}
		if options.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
		args = append(args, options.ServiceNames...)
		return s.runComposeActionOutput(ctx, stack, "up", args...)
	default:
		return "", ErrUnsupportedAction
	}
}

func (s *ServiceReader) MaintenanceNeedsBuild(ctx context.Context, stackID string) (bool, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return false, err
	}
	for _, service := range stack.Services {
		if service.Mode == ServiceModeBuild || service.Mode == ServiceModeHybrid {
			return true, nil
		}
	}
	return false, nil
}

func IsValidStackID(value string) bool {
	return stackIDRegexp.MatchString(value)
}

func (s *ServiceReader) findStack(ctx context.Context, stackID string) (discoveredStack, error) {
	if !IsValidStackID(stackID) {
		return discoveredStack{}, ErrNotFound
	}

	allStacks, err := s.readStacks(ctx)
	if err != nil {
		return discoveredStack{}, err
	}
	for _, stack := range allStacks {
		if stack.ID == stackID {
			return stack, nil
		}
	}
	return discoveredStack{}, ErrNotFound
}

type discoveredStack struct {
	ID               string
	RootPath         string
	ComposeFilePath  string
	EnvFilePath      string
	ConfigPath       string
	DataPath         string
	Services         []Service
	Metadata         *StackMetadata
	Containers       []Container
	RuntimeState     RuntimeState
	ConfigState      ConfigState
	ActivityState    ActivityState
	HealthSummary    HealthSummary
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastDeployedAt   *time.Time
	DefinitionExists bool
}

type definitionSnapshot struct {
	Services      []Service
	Metadata      *StackMetadata
	ConfigState   ConfigState
	ComposeYAML   string
	ComposeSHA256 string
	Env           string
	EnvExists     bool
	EnvSHA256     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type dockerInspectContainer struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status    string `json:"Status"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	NetworkSettings struct {
		Ports    map[string][]dockerPortBinding `json:"Ports"`
		Networks map[string]any                 `json:"Networks"`
	} `json:"NetworkSettings"`
}

type dockerPortBinding struct {
	HostPort string `json:"HostPort"`
}

func (s *ServiceReader) readStacks(ctx context.Context) ([]discoveredStack, error) {
	definedStacks, err := s.scanDefinitions()
	if err != nil {
		return nil, err
	}
	if s.afterScanDefinitionsForTest != nil {
		s.afterScanDefinitionsForTest()
	}
	baselines, err := s.loadDeployBaselines(ctx)
	if err != nil {
		return nil, err
	}

	runtimeStacks := s.scanRuntime(ctx)
	ids := make(map[string]struct{}, len(definedStacks)+len(runtimeStacks))
	for id := range definedStacks {
		ids[id] = struct{}{}
	}
	for id := range runtimeStacks {
		ids[id] = struct{}{}
	}

	stacks := make([]discoveredStack, 0, len(ids))
	for id := range ids {
		stack := discoveredStack{
			ID:              id,
			RootPath:        filepath.Join(s.cfg.RootDir, "stacks", id),
			ComposeFilePath: filepath.Join(s.cfg.RootDir, "stacks", id, "compose.yaml"),
			EnvFilePath:     filepath.Join(s.cfg.RootDir, "stacks", id, ".env"),
			ConfigPath:      filepath.Join(s.cfg.RootDir, "config", id),
			DataPath:        filepath.Join(s.cfg.RootDir, "data", id),
			Services:        []Service{},
			Containers:      []Container{},
			ConfigState:     ConfigStateUnknown,
			ActivityState:   ActivityStateIdle,
		}

		if definition, ok := definedStacks[id]; ok {
			stack.DefinitionExists = true
			stack.Services = definition.Services
			stack.Metadata = definition.Metadata
			stack.ConfigState = definition.ConfigState
			if baseline, ok := baselines[id]; ok {
				deployedAt := baseline.LastDeployedAt
				stack.LastDeployedAt = &deployedAt
				if stack.ConfigState != ConfigStateInvalid {
					composeSHA, envSHA, ok := s.currentDefinitionHashes(stack.ComposeFilePath, stack.EnvFilePath)
					if ok && baseline.ComposeSHA256 == composeSHA && baseline.EnvSHA256 == envSHA {
						stack.ConfigState = ConfigStateInSync
					} else {
						stack.ConfigState = ConfigStateDrifted
					}
				}
			}
			stack.CreatedAt = definition.CreatedAt
			stack.UpdatedAt = definition.UpdatedAt
		} else if baseline, ok := baselines[id]; ok {
			deployedAt := baseline.LastDeployedAt
			stack.LastDeployedAt = &deployedAt
		}

		if runtime, ok := runtimeStacks[id]; ok {
			stack.Containers = runtime
			stack.HealthSummary = calculateHealthSummary(runtime)
			runtimeCreatedAt, runtimeUpdatedAt := runtimeTimeRange(runtime)
			stack.CreatedAt, stack.UpdatedAt = mergeTimes(stack.CreatedAt, stack.UpdatedAt, runtimeCreatedAt, runtimeUpdatedAt)
		}

		stack.RuntimeState = deriveRuntimeState(stack.DefinitionExists, stack.Services, stack.Containers)
		if stack.RuntimeState == RuntimeStateOrphaned {
			stack.ConfigState = ConfigStateUnknown
		}
		if stack.CreatedAt.IsZero() {
			stack.CreatedAt = stack.UpdatedAt
		}
		if stack.UpdatedAt.IsZero() {
			stack.UpdatedAt = stack.CreatedAt
		}
		if stack.CreatedAt.IsZero() {
			stack.CreatedAt = time.Now().UTC()
			stack.UpdatedAt = stack.CreatedAt
		}

		stacks = append(stacks, stack)
	}

	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].ID < stacks[j].ID
	})

	return stacks, nil
}

func (s *ServiceReader) loadDeployBaselines(ctx context.Context) (map[string]store.StackDeployBaseline, error) {
	result := map[string]store.StackDeployBaseline{}
	if s.store == nil {
		return result, nil
	}
	items, err := s.store.ListStackDeployBaselines(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.StackID] = item
	}
	return result, nil
}

func contentSHA256(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func (s *ServiceReader) currentDefinitionHashes(composePath, envPath string) (string, string, bool) {
	composeBytes, err := os.ReadFile(composePath)
	if err != nil {
		return "", "", false
	}
	envContent := ""
	if envBytes, err := os.ReadFile(envPath); err == nil {
		envContent = string(envBytes)
	} else if err != nil && !os.IsNotExist(err) {
		return "", "", false
	}
	return contentSHA256(string(composeBytes)), contentSHA256(envContent), true
}

func (s *ServiceReader) scanDefinitions() (map[string]definitionSnapshot, error) {
	result := make(map[string]definitionSnapshot)
	stacksRoot := filepath.Join(s.cfg.RootDir, "stacks")
	entries, err := os.ReadDir(stacksRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("read stacks directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || !IsValidStackID(entry.Name()) {
			continue
		}

		stackRoot := filepath.Join(stacksRoot, entry.Name())
		composePath := filepath.Join(stackRoot, "compose.yaml")
		composeInfo, err := os.Stat(composePath)
		if err != nil {
			continue
		}

		envPath := filepath.Join(stackRoot, ".env")
		envInfo, envErr := os.Stat(envPath)
		dirInfo, dirErr := os.Stat(stackRoot)
		createdAt, updatedAt := timeRange(dirInfo, dirErr, composeInfo, nil, envInfo, envErr)

		content, err := os.ReadFile(composePath)
		if err != nil {
			s.logDefinitionWarning("failed to read compose file", entry.Name(), "read", err)
			result[entry.Name()] = definitionSnapshot{
				ConfigState: ConfigStateInvalid,
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
			}
			continue
		}
		s.clearDefinitionWarning(entry.Name(), "read")

		envContent := ""
		envExists := false
		if envBytes, err := os.ReadFile(envPath); err == nil {
			envExists = true
			envContent = string(envBytes)
			s.clearDefinitionWarning(entry.Name(), "env")
		} else if err != nil && !os.IsNotExist(err) {
			s.logDefinitionWarning("failed to read env file", entry.Name(), "env", err)
			result[entry.Name()] = definitionSnapshot{
				ConfigState: ConfigStateInvalid,
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
			}
			continue
		} else {
			s.clearDefinitionWarning(entry.Name(), "env")
		}

		services, metadata, parseErr := parseComposeServices(stackRoot, content)
		configState := ConfigStateUnknown
		if parseErr != nil {
			s.logDefinitionWarning("failed to parse compose file", entry.Name(), "parse", parseErr)
			configState = ConfigStateInvalid
		} else {
			s.clearDefinitionWarning(entry.Name(), "parse")
		}

		result[entry.Name()] = definitionSnapshot{
			Services:      services,
			Metadata:      metadata,
			ConfigState:   configState,
			ComposeYAML:   string(content),
			ComposeSHA256: contentSHA256(string(content)),
			Env:           envContent,
			EnvExists:     envExists,
			EnvSHA256:     contentSHA256(envContent),
			CreatedAt:     createdAt,
			UpdatedAt:     updatedAt,
		}
	}

	return result, nil
}

func (s *ServiceReader) logDefinitionWarning(message, stackID, kind string, err error) {
	if s.logger == nil || err == nil {
		return
	}
	args := []any{slog.String("stack_id", stackID), slog.String("err", err.Error())}
	if s.definitionWarningChanged(stackID, kind, err.Error()) {
		s.logger.Warn(message, args...)
		return
	}
	s.logger.Debug(message, args...)
}

func (s *ServiceReader) definitionWarningChanged(stackID, kind, signature string) bool {
	key := stackID + ":" + kind
	s.definitionWarningMu.Lock()
	defer s.definitionWarningMu.Unlock()
	if s.definitionWarningLog == nil {
		s.definitionWarningLog = map[string]string{}
	}
	if s.definitionWarningLog[key] == signature {
		return false
	}
	s.definitionWarningLog[key] = signature
	return true
}

func (s *ServiceReader) clearDefinitionWarning(stackID, kind string) {
	key := stackID + ":" + kind
	s.definitionWarningMu.Lock()
	delete(s.definitionWarningLog, key)
	s.definitionWarningMu.Unlock()
}

func (s *ServiceReader) scanRuntime(ctx context.Context) map[string][]Container {
	result := make(map[string][]Container)
	ids, err := listDockerContainerIDs(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to list docker containers", slog.String("err", err.Error()))
		}
		return result
	}
	if len(ids) == 0 {
		return result
	}

	inspected, err := inspectDockerContainers(ctx, ids)
	if err != nil {
		s.logger.Warn("failed to inspect docker containers", slog.String("err", err.Error()))
		return result
	}

	for _, inspectedContainer := range inspected {
		projectID := inspectedContainer.Config.Labels["com.docker.compose.project"]
		serviceName := inspectedContainer.Config.Labels["com.docker.compose.service"]
		if !IsValidStackID(projectID) || serviceName == "" {
			continue
		}

		networks := make([]string, 0, len(inspectedContainer.NetworkSettings.Networks))
		for name := range inspectedContainer.NetworkSettings.Networks {
			networks = append(networks, name)
		}
		sort.Strings(networks)

		ports := make([]PortMapping, 0, len(inspectedContainer.NetworkSettings.Ports))
		for exposedPort, bindings := range inspectedContainer.NetworkSettings.Ports {
			target, protocol := parseExposedPort(exposedPort)
			if target == 0 {
				continue
			}
			if len(bindings) == 0 {
				ports = append(ports, PortMapping{
					Published: target,
					Target:    target,
					Protocol:  protocol,
				})
				continue
			}
			for _, binding := range bindings {
				published, err := strconv.Atoi(binding.HostPort)
				if err != nil {
					continue
				}
				ports = append(ports, PortMapping{
					Published: published,
					Target:    target,
					Protocol:  protocol,
				})
			}
		}
		ports = normalizePortMappings(ports)

		var healthStatus *string
		if inspectedContainer.State.Health != nil {
			health := inspectedContainer.State.Health.Status
			healthStatus = &health
		}

		var startedAt *time.Time
		if inspectedContainer.State.StartedAt != "" && !strings.HasPrefix(inspectedContainer.State.StartedAt, "0001-01-01") {
			if parsed, err := time.Parse(time.RFC3339Nano, inspectedContainer.State.StartedAt); err == nil {
				parsed = parsed.UTC()
				startedAt = &parsed
			}
		}

		result[projectID] = append(result[projectID], Container{
			ID:           inspectedContainer.ID,
			Name:         strings.TrimPrefix(inspectedContainer.Name, "/"),
			ServiceName:  serviceName,
			Status:       normalizeContainerStatus(inspectedContainer.State.Status),
			HealthStatus: healthStatus,
			StartedAt:    startedAt,
			ImageID:      inspectedContainer.Image,
			ImageRef:     inspectedContainer.Config.Image,
			Ports:        ports,
			Networks:     networks,
		})
	}

	for projectID := range result {
		sort.Slice(result[projectID], func(i, j int) bool {
			return result[projectID][i].Name < result[projectID][j].Name
		})
	}

	return result
}

func ensureDockerRuntimeAvailable(ctx context.Context) error {
	_, err := listDockerContainerIDs(ctx)
	return err
}

func listDockerContainerIDs(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=com.docker.compose.project")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%w: %s", ErrDockerUnavailable, message)
	}

	ids := strings.Fields(strings.TrimSpace(string(output)))
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}

func inspectDockerContainers(ctx context.Context, ids []string) ([]dockerInspectContainer, error) {
	args := append([]string{"inspect"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var inspected []dockerInspectContainer
	if err := json.Unmarshal(output, &inspected); err != nil {
		return nil, err
	}
	return inspected, nil
}

func normalizeContainerStatus(state string) string {
	switch state {
	case "created", "running", "restarting", "paused", "exited", "dead":
		return state
	default:
		return "created"
	}
}

type composeDefinition struct {
	Services  map[string]composeService `yaml:"services"`
	XStacklab *composeXStacklab         `yaml:"x-stacklab"`
}

type composeXStacklab struct {
	Icon  string `yaml:"icon"`
	Links []struct {
		Label string `yaml:"label"`
		URL   string `yaml:"url"`
	} `yaml:"links"`
}

var metadataIconPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

const metadataMaxLinks = 8

// parseStackMetadata validates the x-stacklab block into StackMetadata.
// Anything invalid is dropped field-by-field rather than failing the stack.
func parseStackMetadata(x *composeXStacklab) *StackMetadata {
	if x == nil {
		return nil
	}

	meta := StackMetadata{}
	if metadataIconPattern.MatchString(x.Icon) {
		meta.Icon = x.Icon
	}
	for _, link := range x.Links {
		if len(meta.Links) == metadataMaxLinks {
			break
		}
		label := strings.TrimSpace(link.Label)
		parsed, err := url.Parse(strings.TrimSpace(link.URL))
		if err != nil || label == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		meta.Links = append(meta.Links, StackMetaLink{Label: label, URL: parsed.String()})
	}

	if meta.Icon == "" && len(meta.Links) == 0 {
		return nil
	}
	return &meta
}

type composeService struct {
	Image       string `yaml:"image"`
	Build       any    `yaml:"build"`
	Ports       []any  `yaml:"ports"`
	Volumes     []any  `yaml:"volumes"`
	DependsOn   any    `yaml:"depends_on"`
	Healthcheck any    `yaml:"healthcheck"`
}

func parseComposeServices(stackRoot string, content []byte) ([]Service, *StackMetadata, error) {
	var doc composeDefinition
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, nil, err
	}

	metadata := parseStackMetadata(doc.XStacklab)

	if len(doc.Services) == 0 {
		return []Service{}, metadata, nil
	}

	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	services := make([]Service, 0, len(names))
	for _, name := range names {
		raw := doc.Services[name]
		mode, imageRef, buildContext, dockerfilePath := parseServiceSource(stackRoot, raw.Image, raw.Build)
		services = append(services, Service{
			Name:               name,
			Mode:               mode,
			ImageRef:           imageRef,
			BuildContext:       buildContext,
			DockerfilePath:     dockerfilePath,
			Ports:              parseComposePorts(raw.Ports),
			Volumes:            parseComposeVolumes(stackRoot, raw.Volumes),
			DependsOn:          parseDependsOn(raw.DependsOn),
			HealthcheckPresent: raw.Healthcheck != nil,
		})
	}

	return services, metadata, nil
}

func parseServiceSource(stackRoot, image string, build any) (ServiceMode, *string, *string, *string) {
	var imageRef *string
	if image != "" {
		imageCopy := image
		imageRef = &imageCopy
	}

	var buildContext *string
	var dockerfilePath *string
	switch value := build.(type) {
	case string:
		resolved := resolvePath(stackRoot, value)
		buildContext = &resolved
	case map[string]any:
		if contextValue, ok := value["context"].(string); ok && contextValue != "" {
			resolved := resolvePath(stackRoot, contextValue)
			buildContext = &resolved
			if dockerfileValue, ok := value["dockerfile"].(string); ok && dockerfileValue != "" {
				resolvedDockerfile := dockerfileValue
				if !filepath.IsAbs(resolvedDockerfile) {
					resolvedDockerfile = filepath.Join(resolved, dockerfileValue)
				}
				dockerfilePath = &resolvedDockerfile
			}
		}
	}

	switch {
	case imageRef != nil && buildContext != nil:
		return ServiceModeHybrid, imageRef, buildContext, dockerfilePath
	case buildContext != nil:
		return ServiceModeBuild, nil, buildContext, dockerfilePath
	default:
		return ServiceModeImage, imageRef, nil, nil
	}
}

func parseComposePorts(values []any) []PortMapping {
	ports := make([]PortMapping, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			port, ok := parseShortPortMapping(typed)
			if ok {
				ports = append(ports, port)
			}
		case int:
			ports = append(ports, PortMapping{
				Published: typed,
				Target:    typed,
				Protocol:  "tcp",
			})
		case map[string]any:
			target, okTarget := toInt(typed["target"])
			published, okPublished := toInt(typed["published"])
			if !okTarget {
				continue
			}
			if !okPublished {
				published = target
			}
			protocol, _ := typed["protocol"].(string)
			if protocol == "" {
				protocol = "tcp"
			}
			ports = append(ports, PortMapping{
				Published: published,
				Target:    target,
				Protocol:  protocol,
			})
		}
	}

	return normalizePortMappings(ports)
}

type portMappingIdentity struct {
	Published int
	Target    int
	Protocol  string
}

func normalizePortMappings(ports []PortMapping) []PortMapping {
	if len(ports) == 0 {
		return ports
	}
	seen := make(map[portMappingIdentity]struct{}, len(ports))
	result := make([]PortMapping, 0, len(ports))
	for _, port := range ports {
		protocol := strings.ToLower(strings.TrimSpace(port.Protocol))
		if protocol == "" {
			protocol = "tcp"
		}
		identity := portMappingIdentity{
			Published: port.Published,
			Target:    port.Target,
			Protocol:  protocol,
		}
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		port.Protocol = identity.Protocol
		result = append(result, port)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Published == result[j].Published {
			if result[i].Target == result[j].Target {
				return result[i].Protocol < result[j].Protocol
			}
			return result[i].Target < result[j].Target
		}
		return result[i].Published < result[j].Published
	})
	return result
}

func parseShortPortMapping(value string) (PortMapping, bool) {
	protocol := "tcp"
	parts := strings.SplitN(value, "/", 2)
	if len(parts) == 2 && parts[1] != "" {
		protocol = parts[1]
	}

	portSpec := parts[0]
	tokens := strings.Split(portSpec, ":")
	last := tokens[len(tokens)-1]
	target, err := strconv.Atoi(last)
	if err != nil {
		return PortMapping{}, false
	}

	published := target
	if len(tokens) >= 2 {
		if parsed, err := strconv.Atoi(tokens[len(tokens)-2]); err == nil {
			published = parsed
		}
	}

	return PortMapping{
		Published: published,
		Target:    target,
		Protocol:  protocol,
	}, true
}

func parseComposeVolumes(stackRoot string, values []any) []VolumeMount {
	volumes := make([]VolumeMount, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			tokens := strings.Split(typed, ":")
			if len(tokens) == 1 {
				volumes = append(volumes, VolumeMount{
					Source: "",
					Target: tokens[0],
				})
				continue
			}

			volumes = append(volumes, VolumeMount{
				Source: resolveMaybeRelativePath(stackRoot, tokens[0]),
				Target: tokens[1],
			})
		case map[string]any:
			source, _ := typed["source"].(string)
			target, _ := typed["target"].(string)
			if target == "" {
				continue
			}
			volumes = append(volumes, VolumeMount{
				Source: resolveMaybeRelativePath(stackRoot, source),
				Target: target,
			})
		}
	}

	return volumes
}

func parseDependsOn(value any) []string {
	switch typed := value.(type) {
	case []any:
		depends := make([]string, 0, len(typed))
		for _, item := range typed {
			if name, ok := item.(string); ok && name != "" {
				depends = append(depends, name)
			}
		}
		sort.Strings(depends)
		return depends
	case map[string]any:
		depends := make([]string, 0, len(typed))
		for name := range typed {
			depends = append(depends, name)
		}
		sort.Strings(depends)
		return depends
	default:
		return []string{}
	}
}

func resolvePath(root, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(root, value))
}

func resolveMaybeRelativePath(root, value string) string {
	switch {
	case value == "":
		return ""
	case strings.HasPrefix(value, "/"):
		return value
	case strings.HasPrefix(value, "."):
		return resolvePath(root, value)
	default:
		return value
	}
}

func toInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func deriveRuntimeState(definitionExists bool, services []Service, containers []Container) RuntimeState {
	if !definitionExists && len(containers) > 0 {
		return RuntimeStateOrphaned
	}
	if len(containers) == 0 {
		return RuntimeStateDefined
	}

	statusByService := make(map[string]string)
	for _, container := range containers {
		priority := serviceStatusPriority(container.Status)
		current := statusByService[container.ServiceName]
		if current == "" || priority > serviceStatusPriority(current) {
			statusByService[container.ServiceName] = container.Status
		}
	}

	hasError := false
	runningServices := 0
	for _, status := range statusByService {
		switch status {
		case "restarting", "dead":
			hasError = true
		case "running":
			runningServices++
		}
	}
	if hasError {
		return RuntimeStateError
	}
	if runningServices == 0 {
		return RuntimeStateStopped
	}
	if len(services) > 0 && runningServices < len(services) {
		return RuntimeStatePartial
	}
	if len(services) == 0 && runningServices < len(statusByService) {
		return RuntimeStatePartial
	}
	return RuntimeStateRunning
}

func serviceStatusPriority(status string) int {
	switch status {
	case "restarting", "dead":
		return 5
	case "running":
		return 4
	case "paused":
		return 3
	case "created":
		return 2
	case "exited":
		return 1
	default:
		return 0
	}
}

func calculateHealthSummary(containers []Container) HealthSummary {
	summary := HealthSummary{}
	for _, container := range containers {
		switch {
		case container.HealthStatus != nil && *container.HealthStatus == "healthy":
			summary.HealthyContainerCount++
		case container.HealthStatus != nil && *container.HealthStatus == "unhealthy":
			summary.UnhealthyContainerCount++
		default:
			summary.UnknownHealthContainerCount++
		}
	}
	return summary
}

func runtimeTimeRange(containers []Container) (time.Time, time.Time) {
	var createdAt time.Time
	var updatedAt time.Time
	for _, container := range containers {
		if container.StartedAt != nil {
			if createdAt.IsZero() || container.StartedAt.Before(createdAt) {
				createdAt = *container.StartedAt
			}
			if updatedAt.IsZero() || container.StartedAt.After(updatedAt) {
				updatedAt = *container.StartedAt
			}
		}
	}
	return createdAt, updatedAt
}

func mergeTimes(existingCreatedAt, existingUpdatedAt, candidateCreatedAt, candidateUpdatedAt time.Time) (time.Time, time.Time) {
	if existingCreatedAt.IsZero() || (!candidateCreatedAt.IsZero() && candidateCreatedAt.Before(existingCreatedAt)) {
		existingCreatedAt = candidateCreatedAt
	}
	if existingUpdatedAt.IsZero() || (!candidateUpdatedAt.IsZero() && candidateUpdatedAt.After(existingUpdatedAt)) {
		existingUpdatedAt = candidateUpdatedAt
	}
	return existingCreatedAt, existingUpdatedAt
}

func timeRange(values ...any) (time.Time, time.Time) {
	var createdAt time.Time
	var updatedAt time.Time
	for i := 0; i+1 < len(values); i += 2 {
		info, ok := values[i].(os.FileInfo)
		if !ok || info == nil {
			continue
		}
		err, _ := values[i+1].(error)
		if err != nil {
			continue
		}
		modTime := info.ModTime().UTC()
		if createdAt.IsZero() || modTime.Before(createdAt) {
			createdAt = modTime
		}
		if updatedAt.IsZero() || modTime.After(updatedAt) {
			updatedAt = modTime
		}
	}
	return createdAt, updatedAt
}

func sortStackListItems(items []StackListItem, sortBy string) {
	stateOrder := map[RuntimeState]int{
		RuntimeStateError:    0,
		RuntimeStatePartial:  1,
		RuntimeStateRunning:  2,
		RuntimeStateStopped:  3,
		RuntimeStateDefined:  4,
		RuntimeStateOrphaned: 5,
	}

	sort.Slice(items, func(i, j int) bool {
		switch sortBy {
		case "state":
			left := stateOrder[items[i].RuntimeState]
			right := stateOrder[items[j].RuntimeState]
			if left != right {
				return left < right
			}
		}
		return items[i].Name < items[j].Name
	})
}

func detectComposeVersion(ctx context.Context) string {
	command, args := composeVersionCommand(ctx)
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(output))
}

func composeVersionCommand(ctx context.Context) (string, []string) {
	cli := detectComposeCLI(ctx)
	args := append([]string{}, cli.prefix...)
	args = append(args, "version", "--short")
	return cli.command, args
}

func detectComposeCLI(ctx context.Context) composeCLI {
	composeCLIMu.Lock()
	defer composeCLIMu.Unlock()

	if composeCLICached != nil {
		return *composeCLICached
	}

	candidates := []composeCLI{
		{command: "docker", prefix: []string{"compose"}},
		{command: "docker-compose"},
	}
	for _, candidate := range candidates {
		args := append([]string{}, candidate.prefix...)
		args = append(args, "version")
		cmd := exec.CommandContext(ctx, candidate.command, args...)
		if err := cmd.Run(); err == nil {
			resolved := candidate
			composeCLICached = &resolved
			return resolved
		}
	}

	fallback := composeCLI{command: "docker", prefix: []string{"compose"}}
	composeCLICached = &fallback
	return fallback
}

func detectDockerEngineVersion(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(output))
}

func writeTempEnvFile(dataDir, content string) (string, func(), error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", func() {}, fmt.Errorf("create data dir for temp env: %w", err)
	}

	file, err := os.CreateTemp(dataDir, "stacklab-preview-*.env")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp env file: %w", err)
	}

	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", func() {}, fmt.Errorf("write temp env file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", func() {}, fmt.Errorf("close temp env file: %w", err)
	}

	return file.Name(), func() {
		_ = os.Remove(file.Name())
	}, nil
}

func writeFileAtomic(path, content string) error {
	return atomicfile.WriteString(path, content, ".stacklab-*")
}

func ensureDefinitionRevision(stack discoveredStack, expected DefinitionRevision) error {
	composeInfo, err := os.Stat(stack.ComposeFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrConflict
		}
		return fmt.Errorf("stat compose file before save: %w", err)
	}
	if !composeInfo.ModTime().UTC().Equal(expected.ComposeModifiedAt.UTC()) {
		return ErrConflict
	}

	envInfo, err := os.Stat(stack.EnvFilePath)
	if expected.EnvModifiedAt == nil {
		if err == nil {
			return ErrConflict
		}
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat env file before save: %w", err)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return ErrConflict
		}
		return fmt.Errorf("stat env file before save: %w", err)
	}
	if !envInfo.ModTime().UTC().Equal(expected.EnvModifiedAt.UTC()) {
		return ErrConflict
	}
	return nil
}

func writeEnvFile(path, content string) error {
	if content == "" {
		if _, err := os.Stat(path); err == nil {
			return writeFileAtomic(path, "")
		} else if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}

	return writeFileAtomic(path, content)
}

func runComposeConfig(ctx context.Context, projectDir, composeArg, envPath, stdinContent string) (string, error) {
	command, args := composeCommand(ctx, projectDir, composeArg, envPath)
	args = append(args, "config")

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = projectDir
	if composeArg == "-" {
		cmd.Stdin = strings.NewReader(stdinContent)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}

	return string(output), nil
}

func (s *ServiceReader) runComposeActionOutput(ctx context.Context, stack discoveredStack, action string, extraArgs ...string) (string, error) {
	command, args := composeCommand(ctx, stack.RootPath, stack.ComposeFilePath, stack.EnvFilePath)
	args = append(args, action)
	args = append(args, extraArgs...)

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = stack.RootPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return strings.TrimSpace(string(output)), errors.New(message)
	}

	return strings.TrimSpace(string(output)), nil
}

func (s *ServiceReader) removeRuntime(ctx context.Context, stack discoveredStack) error {
	_, err := s.removeRuntimeOutput(ctx, stack)
	return err
}

func (s *ServiceReader) removeRuntimeOutput(ctx context.Context, stack discoveredStack) (string, error) {
	if len(stack.Containers) == 0 {
		return "", nil
	}
	if stack.RuntimeState == RuntimeStateOrphaned || stack.ConfigState == ConfigStateInvalid {
		return s.runContainerActionOutput(ctx, stack, "rm", "-f")
	}
	return s.runComposeActionOutput(ctx, stack, "down", "--remove-orphans")
}

func (s *ServiceReader) runContainerActionOutput(ctx context.Context, stack discoveredStack, action string, extraArgs ...string) (string, error) {
	containerIDs := make([]string, 0, len(stack.Containers))
	for _, container := range stack.Containers {
		containerIDs = append(containerIDs, container.ID)
	}
	if len(containerIDs) == 0 {
		return "", nil
	}

	args := []string{action}
	args = append(args, extraArgs...)
	args = append(args, containerIDs...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return strings.TrimSpace(string(output)), errors.New(message)
	}

	return strings.TrimSpace(string(output)), nil
}

func composeCommand(ctx context.Context, projectDir, composePath, envPath string) (string, []string) {
	cli := detectComposeCLI(ctx)
	args := append([]string{}, cli.prefix...)
	args = append(args, "--project-directory", projectDir, "-f", composePath)
	if fileExists(envPath) {
		args = append(args, "--env-file", envPath)
	}
	return cli.command, args
}

type stackPathSet struct {
	RootPath        string
	ComposeFilePath string
	EnvFilePath     string
	ConfigPath      string
	DataPath        string
}

func (s *ServiceReader) resolveCurrent(ctx context.Context, stack discoveredStack) ResolvedConfigResponse {
	envPath := ""
	if fileExists(stack.EnvFilePath) {
		envPath = stack.EnvFilePath
	}

	content, err := runComposeConfig(ctx, stack.RootPath, stack.ComposeFilePath, envPath, "")
	if err != nil {
		return ResolvedConfigResponse{
			StackID: stack.ID,
			Valid:   false,
			Error: &ErrorDetail{
				Code:    "validation_failed",
				Message: err.Error(),
				Details: nil,
			},
		}
	}

	var warnings []ComposeWarning
	if raw, readErr := os.ReadFile(stack.ComposeFilePath); readErr == nil {
		warnings = LintCompose(raw)
	}

	return ResolvedConfigResponse{
		StackID:  stack.ID,
		Valid:    true,
		Content:  content,
		Warnings: warnings,
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, err
	}
}

func stackPaths(rootDir, stackID string) stackPathSet {
	return stackPathSet{
		RootPath:        filepath.Join(rootDir, "stacks", stackID),
		ComposeFilePath: filepath.Join(rootDir, "stacks", stackID, "compose.yaml"),
		EnvFilePath:     filepath.Join(rootDir, "stacks", stackID, ".env"),
		ConfigPath:      filepath.Join(rootDir, "config", stackID),
		DataPath:        filepath.Join(rootDir, "data", stackID),
	}
}

func removeFileIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func removeDirIfExists(path string) error {
	err := os.RemoveAll(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func removeDirIfEmpty(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
		return nil
	}
	return err
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseExposedPort(value string) (int, string) {
	parts := strings.SplitN(value, "/", 2)
	target, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, ""
	}

	protocol := "tcp"
	if len(parts) == 2 && parts[1] != "" {
		protocol = parts[1]
	}
	return target, protocol
}

func (s discoveredStack) header() StackHeader {
	return StackHeader{
		ID:            s.ID,
		Name:          s.ID,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
		DisplayState:  s.RuntimeState,
		RuntimeState:  s.RuntimeState,
		ConfigState:   s.ConfigState,
		ActivityState: s.ActivityState,
		HealthSummary: s.HealthSummary,
		Metadata:      s.Metadata,
	}
}

func (s discoveredStack) runningServiceCount() int {
	serviceNames := make(map[string]struct{})
	for _, container := range s.Containers {
		if container.Status == "running" {
			serviceNames[container.ServiceName] = struct{}{}
		}
	}
	return len(serviceNames)
}

func (s discoveredStack) runningContainerCount() int {
	count := 0
	for _, container := range s.Containers {
		if container.Status == "running" {
			count++
		}
	}
	return count
}

func (s discoveredStack) capabilities() StackCapabilities {
	return StackCapabilities{
		CanEditDefinition: s.RuntimeState != RuntimeStateOrphaned,
		CanViewLogs:       true,
		CanViewStats:      true,
		CanOpenTerminal:   true,
	}
}

func (s discoveredStack) availableActions() []string {
	switch {
	case s.RuntimeState == RuntimeStateOrphaned:
		return []string{"down"}
	case s.ConfigState == ConfigStateInvalid:
		actions := []string{"validate", "save_definition"}
		if len(s.Containers) > 0 {
			actions = append(actions, "stop", "down")
		}
		return actions
	case s.RuntimeState == RuntimeStateDefined:
		return []string{"validate", "up", "pull", "build", "save_definition", "remove_stack_definition"}
	case s.RuntimeState == RuntimeStateStopped:
		return []string{"validate", "up", "down", "pull", "build", "save_definition", "remove_stack_definition"}
	default:
		return []string{"validate", "up", "restart", "stop", "down", "pull", "build", "recreate", "save_definition", "remove_stack_definition"}
	}
}
