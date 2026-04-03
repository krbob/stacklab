package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"stacklab/internal/config"

	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound     = errors.New("stack not found")
	ErrInvalidState = errors.New("invalid state")
	stackIDRegexp   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

const AppVersion = "0.1.0-dev"

type ServiceReader struct {
	cfg       config.Config
	logger    *slog.Logger
	hostShell bool
}

func NewServiceReader(cfg config.Config, logger *slog.Logger) *ServiceReader {
	return &ServiceReader{
		cfg:    cfg,
		logger: logger,
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

	for _, stack := range allStacks {
		if query.Search != "" && !strings.Contains(stack.ID, strings.ToLower(query.Search)) {
			continue
		}

		items = append(items, StackListItem{
			StackHeader:  stack.header(),
			ServiceCount: ServiceCount{Defined: len(stack.Services), Running: stack.runningServiceCount()},
			LastAction:   nil,
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
				LastDeployedAt:   nil,
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

	composeContent, err := os.ReadFile(stack.ComposeFilePath)
	if err != nil {
		return StackDefinitionResponse{}, fmt.Errorf("read compose file: %w", err)
	}

	envContent := ""
	envExists := false
	if envBytes, err := os.ReadFile(stack.EnvFilePath); err == nil {
		envExists = true
		envContent = string(envBytes)
	} else if !os.IsNotExist(err) {
		return StackDefinitionResponse{}, fmt.Errorf("read env file: %w", err)
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
				Path:    stack.ComposeFilePath,
				Content: string(composeContent),
			},
			Env: EnvFile{
				Path:    stack.EnvFilePath,
				Content: envContent,
				Exists:  envExists,
			},
		},
		ConfigState: configState,
	}, nil
}

func (s *ServiceReader) ResolvedConfigCurrent(ctx context.Context, stackID string, source string) (ResolvedConfigResponse, error) {
	if source == "last_valid" {
		return ResolvedConfigResponse{}, fmt.Errorf("last_valid source is not implemented")
	}

	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return ResolvedConfigResponse{}, err
	}
	if stack.RuntimeState == RuntimeStateOrphaned {
		return ResolvedConfigResponse{}, ErrInvalidState
	}

	return s.resolveCurrent(ctx, stack), nil
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
		StackID: stack.ID,
		Valid:   true,
		Content: content,
	}, nil
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
	Containers       []Container
	RuntimeState     RuntimeState
	ConfigState      ConfigState
	ActivityState    ActivityState
	HealthSummary    HealthSummary
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DefinitionExists bool
}

type definitionSnapshot struct {
	Services    []Service
	ConfigState ConfigState
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
			stack.ConfigState = definition.ConfigState
			stack.CreatedAt = definition.CreatedAt
			stack.UpdatedAt = definition.UpdatedAt
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
			s.logger.Warn("failed to read compose file", slog.String("stack_id", entry.Name()), slog.String("err", err.Error()))
			result[entry.Name()] = definitionSnapshot{
				ConfigState: ConfigStateInvalid,
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
			}
			continue
		}

		services, parseErr := parseComposeServices(stackRoot, content)
		configState := ConfigStateUnknown
		if parseErr != nil {
			s.logger.Warn("failed to parse compose file", slog.String("stack_id", entry.Name()), slog.String("err", parseErr.Error()))
			configState = ConfigStateInvalid
		}

		result[entry.Name()] = definitionSnapshot{
			Services:    services,
			ConfigState: configState,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		}
	}

	return result, nil
}

func (s *ServiceReader) scanRuntime(ctx context.Context) map[string][]Container {
	result := make(map[string][]Container)
	ids := listDockerContainerIDs(ctx)
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
		sort.Slice(ports, func(i, j int) bool {
			if ports[i].Published == ports[j].Published {
				return ports[i].Target < ports[j].Target
			}
			return ports[i].Published < ports[j].Published
		})

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

func listDockerContainerIDs(ctx context.Context) []string {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=com.docker.compose.project")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	ids := strings.Fields(strings.TrimSpace(string(output)))
	if len(ids) == 0 {
		return nil
	}
	return ids
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
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string `yaml:"image"`
	Build       any    `yaml:"build"`
	Ports       []any  `yaml:"ports"`
	Volumes     []any  `yaml:"volumes"`
	DependsOn   any    `yaml:"depends_on"`
	Healthcheck any    `yaml:"healthcheck"`
}

func parseComposeServices(stackRoot string, content []byte) ([]Service, error) {
	var doc composeDefinition
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}

	if len(doc.Services) == 0 {
		return []Service{}, nil
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

	return services, nil
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

	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Published == ports[j].Published {
			return ports[i].Target < ports[j].Target
		}
		return ports[i].Published < ports[j].Published
	})
	return ports
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
	cmd := exec.CommandContext(ctx, "docker", "compose", "version", "--short")
	output, err := cmd.Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(output))
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

func runComposeConfig(ctx context.Context, projectDir, composeArg, envPath, stdinContent string) (string, error) {
	args := []string{"compose", "--project-directory", projectDir, "-f", composeArg}
	if envPath != "" {
		args = append(args, "--env-file", envPath)
	}
	args = append(args, "config")

	cmd := exec.CommandContext(ctx, "docker", args...)
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

func (s *ServiceReader) resolveCurrent(ctx context.Context, stack discoveredStack) ResolvedConfigResponse {
	envPath := ""
	if _, err := os.Stat(stack.EnvFilePath); err == nil {
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

	return ResolvedConfigResponse{
		StackID: stack.ID,
		Valid:   true,
		Content: content,
	}
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
	default:
		return []string{"validate", "up", "restart", "stop", "down", "pull", "build", "recreate", "save_definition", "remove_stack_definition"}
	}
}
