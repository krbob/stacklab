package dockeradmin

import (
	"time"

	"stacklab/internal/fsmeta"
)

type OverviewResponse struct {
	Service         ServiceStatus    `json:"service"`
	Engine          EngineStatus     `json:"engine"`
	DaemonConfig    DaemonConfigMeta `json:"daemon_config"`
	WriteCapability WriteCapability  `json:"write_capability"`
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
	Path            string              `json:"path"`
	Exists          bool                `json:"exists"`
	Permissions     *fsmeta.Permissions `json:"permissions,omitempty"`
	SizeBytes       *int64              `json:"size_bytes,omitempty"`
	ModifiedAt      *time.Time          `json:"modified_at,omitempty"`
	ValidJSON       bool                `json:"valid_json"`
	ParseError      *string             `json:"parse_error,omitempty"`
	ConfiguredKeys  []string            `json:"configured_keys"`
	Summary         DaemonConfigSummary `json:"summary"`
	WriteCapability WriteCapability     `json:"write_capability"`
}

type DaemonConfigResponse struct {
	DaemonConfigMeta
	Content *string `json:"content,omitempty"`
}

type WriteCapability struct {
	Supported   bool     `json:"supported"`
	Reason      *string  `json:"reason,omitempty"`
	ManagedKeys []string `json:"managed_keys"`
}

type ManagedSettings struct {
	DNS                *[]string `json:"dns,omitempty"`
	RegistryMirrors    *[]string `json:"registry_mirrors,omitempty"`
	InsecureRegistries *[]string `json:"insecure_registries,omitempty"`
	LiveRestore        *bool     `json:"live_restore,omitempty"`
}

type ValidateManagedConfigRequest struct {
	Settings   ManagedSettings `json:"settings"`
	RemoveKeys []string        `json:"remove_keys,omitempty"`
}

type DaemonConfigPreview struct {
	Path           string              `json:"path"`
	Content        string              `json:"content"`
	ConfiguredKeys []string            `json:"configured_keys"`
	Summary        DaemonConfigSummary `json:"summary"`
}

type ValidateManagedConfigResponse struct {
	WriteCapability WriteCapability     `json:"write_capability"`
	ChangedKeys     []string            `json:"changed_keys"`
	RequiresRestart bool                `json:"requires_restart"`
	Warnings        []string            `json:"warnings"`
	Preview         DaemonConfigPreview `json:"preview"`
}

type ApplyManagedConfigRequest struct {
	Settings   ManagedSettings `json:"settings"`
	RemoveKeys []string        `json:"remove_keys,omitempty"`
}

type ApplyManagedConfigResult struct {
	ChangedKeys        []string `json:"changed_keys"`
	BackupPath         string   `json:"backup_path,omitempty"`
	RolledBack         bool     `json:"rolled_back"`
	RollbackSucceeded  bool     `json:"rollback_succeeded"`
	ServiceActiveState string   `json:"service_active_state,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}
