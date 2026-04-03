package stacks

import "time"

type RuntimeState string
type ConfigState string
type ActivityState string
type ServiceMode string

const (
	RuntimeStateDefined  RuntimeState = "defined"
	RuntimeStateRunning  RuntimeState = "running"
	RuntimeStatePartial  RuntimeState = "partial"
	RuntimeStateStopped  RuntimeState = "stopped"
	RuntimeStateError    RuntimeState = "error"
	RuntimeStateOrphaned RuntimeState = "orphaned"

	ConfigStateUnknown ConfigState = "unknown"
	ConfigStateInSync  ConfigState = "in_sync"
	ConfigStateDrifted ConfigState = "drifted"
	ConfigStateInvalid ConfigState = "invalid"

	ActivityStateIdle   ActivityState = "idle"
	ActivityStateLocked ActivityState = "locked"

	ServiceModeImage  ServiceMode = "image"
	ServiceModeBuild  ServiceMode = "build"
	ServiceModeHybrid ServiceMode = "hybrid"
)

type ListQuery struct {
	Search string
	Sort   string
}

type SessionResponse struct {
	Authenticated bool         `json:"authenticated"`
	User          *SessionUser `json:"user,omitempty"`
	Features      FeatureFlags `json:"features"`
}

type SessionUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type FeatureFlags struct {
	HostShell bool `json:"host_shell"`
}

type MetaResponse struct {
	App         AppMeta         `json:"app"`
	Environment EnvironmentMeta `json:"environment"`
	Docker      DockerMeta      `json:"docker"`
	Features    FeatureFlags    `json:"features"`
}

type AppMeta struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type EnvironmentMeta struct {
	StackRoot string `json:"stack_root"`
	Platform  string `json:"platform"`
}

type DockerMeta struct {
	EngineVersion  string `json:"engine_version"`
	ComposeVersion string `json:"compose_version"`
}

type StackListResponse struct {
	Items   []StackListItem  `json:"items"`
	Summary StackListSummary `json:"summary"`
}

type StackListItem struct {
	StackHeader
	ServiceCount ServiceCount `json:"service_count"`
	LastAction   *LastAction  `json:"last_action"`
}

type StackListSummary struct {
	StackCount     int             `json:"stack_count"`
	RunningCount   int             `json:"running_count"`
	StoppedCount   int             `json:"stopped_count"`
	ErrorCount     int             `json:"error_count"`
	DefinedCount   int             `json:"defined_count"`
	OrphanedCount  int             `json:"orphaned_count"`
	ContainerCount ContainerRollup `json:"container_count"`
}

type ContainerRollup struct {
	Running int `json:"running"`
	Total   int `json:"total"`
}

type StackDetailResponse struct {
	Stack StackDetail `json:"stack"`
}

type StackDetail struct {
	StackHeader
	RootPath         string            `json:"root_path"`
	ComposeFilePath  string            `json:"compose_file_path"`
	EnvFilePath      string            `json:"env_file_path"`
	ConfigPath       string            `json:"config_path"`
	DataPath         string            `json:"data_path"`
	Capabilities     StackCapabilities `json:"capabilities"`
	AvailableActions []string          `json:"available_actions"`
	Services         []Service         `json:"services"`
	Containers       []Container       `json:"containers"`
	LastDeployedAt   *time.Time        `json:"last_deployed_at"`
	LastAction       *LastAction       `json:"last_action"`
}

type StackHeader struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	DisplayState  RuntimeState  `json:"display_state"`
	RuntimeState  RuntimeState  `json:"runtime_state"`
	ConfigState   ConfigState   `json:"config_state"`
	ActivityState ActivityState `json:"activity_state"`
	HealthSummary HealthSummary `json:"health_summary"`
}

type HealthSummary struct {
	HealthyContainerCount       int `json:"healthy_container_count"`
	UnhealthyContainerCount     int `json:"unhealthy_container_count"`
	UnknownHealthContainerCount int `json:"unknown_health_container_count"`
}

type StackCapabilities struct {
	CanEditDefinition bool `json:"can_edit_definition"`
	CanViewLogs       bool `json:"can_view_logs"`
	CanViewStats      bool `json:"can_view_stats"`
	CanOpenTerminal   bool `json:"can_open_terminal"`
}

type ServiceCount struct {
	Defined int `json:"defined"`
	Running int `json:"running"`
}

type Service struct {
	Name               string        `json:"name"`
	Mode               ServiceMode   `json:"mode"`
	ImageRef           *string       `json:"image_ref"`
	BuildContext       *string       `json:"build_context"`
	DockerfilePath     *string       `json:"dockerfile_path"`
	Ports              []PortMapping `json:"ports"`
	Volumes            []VolumeMount `json:"volumes"`
	DependsOn          []string      `json:"depends_on"`
	HealthcheckPresent bool          `json:"healthcheck_present"`
}

type Container struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	ServiceName  string        `json:"service_name"`
	Status       string        `json:"status"`
	HealthStatus *string       `json:"health_status"`
	StartedAt    *time.Time    `json:"started_at"`
	ImageID      string        `json:"image_id"`
	ImageRef     string        `json:"image_ref"`
	Ports        []PortMapping `json:"ports"`
	Networks     []string      `json:"networks"`
}

type PortMapping struct {
	Published int    `json:"published"`
	Target    int    `json:"target"`
	Protocol  string `json:"protocol"`
}

type VolumeMount struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type LastAction struct {
	Action     string    `json:"action"`
	Result     string    `json:"result"`
	FinishedAt time.Time `json:"finished_at"`
}

type StackDefinitionResponse struct {
	StackID     string               `json:"stack_id"`
	Files       StackDefinitionFiles `json:"files"`
	ConfigState ConfigState          `json:"config_state"`
}

type StackDefinitionFiles struct {
	ComposeYAML ComposeYAMLFile `json:"compose_yaml"`
	Env         EnvFile         `json:"env"`
}

type ComposeYAMLFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type EnvFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

type ResolvedConfigRequest struct {
	ComposeYAML string `json:"compose_yaml"`
	Env         string `json:"env"`
}

type ResolvedConfigResponse struct {
	StackID string       `json:"stack_id"`
	Valid   bool         `json:"valid"`
	Content string       `json:"content,omitempty"`
	Error   *ErrorDetail `json:"error,omitempty"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}
