package dockeradmin

import (
	"time"

	"stacklab/internal/fsmeta"
)

type OverviewResponse struct {
	Service      ServiceStatus    `json:"service"`
	Engine       EngineStatus     `json:"engine"`
	DaemonConfig DaemonConfigMeta `json:"daemon_config"`
}

type ServiceStatus struct {
	Manager       string     `json:"manager"`
	Supported     bool       `json:"supported"`
	UnitName      string     `json:"unit_name"`
	LoadState     string     `json:"load_state"`
	ActiveState   string     `json:"active_state"`
	SubState      string     `json:"sub_state"`
	UnitFileState string     `json:"unit_file_state"`
	FragmentPath  string     `json:"fragment_path"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	Message       *string    `json:"message,omitempty"`
}

type EngineStatus struct {
	Available      bool    `json:"available"`
	Version        string  `json:"version"`
	APIVersion     string  `json:"api_version"`
	ComposeVersion string  `json:"compose_version"`
	RootDir        string  `json:"root_dir"`
	Driver         string  `json:"driver"`
	LoggingDriver  string  `json:"logging_driver"`
	CgroupDriver   string  `json:"cgroup_driver"`
	Message        *string `json:"message,omitempty"`
}

type DaemonConfigSummary struct {
	DNS                []string `json:"dns"`
	RegistryMirrors    []string `json:"registry_mirrors"`
	InsecureRegistries []string `json:"insecure_registries"`
	LogDriver          string   `json:"log_driver"`
	DataRoot           string   `json:"data_root"`
	LiveRestore        *bool    `json:"live_restore,omitempty"`
}

type DaemonConfigMeta struct {
	Path           string              `json:"path"`
	Exists         bool                `json:"exists"`
	Permissions    *fsmeta.Permissions `json:"permissions,omitempty"`
	SizeBytes      *int64              `json:"size_bytes,omitempty"`
	ModifiedAt     *time.Time          `json:"modified_at,omitempty"`
	ValidJSON      bool                `json:"valid_json"`
	ParseError     *string             `json:"parse_error,omitempty"`
	ConfiguredKeys []string            `json:"configured_keys"`
	Summary        DaemonConfigSummary `json:"summary"`
}

type DaemonConfigResponse struct {
	DaemonConfigMeta
	Content *string `json:"content,omitempty"`
}
