package dockeradmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/fsmeta"
)

const (
	defaultDockerUnitName       = "docker.service"
	defaultDockerDaemonConfig   = "/etc/docker/daemon.json"
	systemdManagerName          = "systemd"
	unsupportedManagerMessage   = "systemd status is unavailable on this host"
	dockerUnavailableDefaultMsg = "Docker Engine metadata is unavailable."
	writeUnsupportedMessage     = "Managed Docker daemon apply is not configured yet."
)

var (
	ErrInvalidDaemonConfig = errors.New("docker daemon config is invalid")
	ErrUnreadableConfig    = errors.New("docker daemon config is unreadable")
	ErrInvalidManagedInput = errors.New("invalid managed docker config request")
	ErrApplyUnsupported    = errors.New("docker daemon apply is not supported")
)

var supportedManagedKeys = []string{
	"dns",
	"registry_mirrors",
	"insecure_registries",
	"live_restore",
}

var managedKeyMap = map[string]string{
	"dns":                 "dns",
	"registry_mirrors":    "registry-mirrors",
	"insecure_registries": "insecure-registries",
	"live_restore":        "live-restore",
}

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Service struct {
	dockerUnitName   string
	daemonConfigPath string
	helperPath       string
	backupDir        string
	useSudo          bool
	runCommand       commandRunner
}

func NewService(cfg config.Config) *Service {
	dockerUnitName := strings.TrimSpace(cfg.DockerSystemdUnitName)
	if dockerUnitName == "" {
		dockerUnitName = defaultDockerUnitName
	}

	daemonConfigPath := strings.TrimSpace(cfg.DockerDaemonConfigPath)
	if daemonConfigPath == "" {
		daemonConfigPath = defaultDockerDaemonConfig
	}

	return &Service{
		dockerUnitName:   dockerUnitName,
		daemonConfigPath: daemonConfigPath,
		helperPath:       strings.TrimSpace(cfg.DockerAdminHelperPath),
		backupDir:        strings.TrimSpace(cfg.DockerAdminBackupDir),
		useSudo:          cfg.DockerAdminUseSudo,
		runCommand:       defaultCommandRunner,
	}
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s *Service) Overview(ctx context.Context) (OverviewResponse, error) {
	configResponse, err := s.DaemonConfig(ctx)
	if err != nil {
		return OverviewResponse{}, err
	}

	return OverviewResponse{
		Service:         s.readServiceStatus(ctx),
		Engine:          s.readEngineStatus(ctx),
		DaemonConfig:    configResponse.DaemonConfigMeta,
		WriteCapability: s.writeCapability(ctx),
	}, nil
}

func (s *Service) DaemonConfig(ctx context.Context) (DaemonConfigResponse, error) {
	_ = ctx

	response := DaemonConfigResponse{
		DaemonConfigMeta: DaemonConfigMeta{
			Path:            s.daemonConfigPath,
			ValidJSON:       true,
			ConfiguredKeys:  []string{},
			WriteCapability: s.writeCapability(ctx),
			Summary: DaemonConfigSummary{
				DNS:                []string{},
				RegistryMirrors:    []string{},
				InsecureRegistries: []string{},
			},
		},
	}

	info, err := os.Stat(s.daemonConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return response, nil
		}
		return response, err
	}

	response.Exists = true
	sizeBytes := info.Size()
	modifiedAt := info.ModTime().UTC()
	permissions := fsmeta.Inspect(s.daemonConfigPath, info)
	response.SizeBytes = &sizeBytes
	response.ModifiedAt = &modifiedAt
	response.Permissions = &permissions

	if !permissions.Readable {
		return response, nil
	}

	content, err := os.ReadFile(s.daemonConfigPath)
	if err != nil {
		return response, err
	}
	contentString := string(content)
	response.Content = &contentString

	trimmed := strings.TrimSpace(contentString)
	if trimmed == "" {
		return response, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		response.ValidJSON = false
		message := err.Error()
		response.ParseError = &message
		return response, nil
	}

	response.ConfiguredKeys = sortedKeys(raw)
	response.Summary = summarizeDaemonConfig(raw)

	return response, nil
}

func (s *Service) ValidateManagedConfig(ctx context.Context, request ValidateManagedConfigRequest) (ValidateManagedConfigResponse, error) {
	if !hasManagedChanges(request) {
		return ValidateManagedConfigResponse{}, fmt.Errorf("%w: no managed Docker settings were provided", ErrInvalidManagedInput)
	}
	if err := validateManagedRequest(request); err != nil {
		return ValidateManagedConfigResponse{}, err
	}

	base, current, err := s.loadEditableConfig(ctx)
	if err != nil {
		return ValidateManagedConfigResponse{}, err
	}

	merged := cloneRawMap(current)
	applyManagedSettings(merged, request.Settings)
	applyManagedKeyRemovals(merged, request.RemoveKeys)

	content, err := marshalDaemonConfig(merged)
	if err != nil {
		return ValidateManagedConfigResponse{}, err
	}

	return ValidateManagedConfigResponse{
		WriteCapability: s.writeCapability(ctx),
		ChangedKeys:     changedManagedKeys(current, merged),
		RequiresRestart: true,
		Warnings: []string{
			"Applying Docker daemon settings requires a Docker restart.",
		},
		Preview: DaemonConfigPreview{
			Path:           base.Path,
			Content:        content,
			ConfiguredKeys: sortedKeys(merged),
			Summary:        summarizeDaemonConfig(merged),
		},
	}, nil
}

func (s *Service) ApplyManagedConfig(ctx context.Context, request ApplyManagedConfigRequest) (ApplyManagedConfigResult, error) {
	preview, err := s.ValidateManagedConfig(ctx, ValidateManagedConfigRequest(request))
	if err != nil {
		return ApplyManagedConfigResult{}, err
	}
	if !preview.WriteCapability.Supported {
		return ApplyManagedConfigResult{}, ErrApplyUnsupported
	}

	inputFile, err := os.CreateTemp("", "stacklab-daemon-config-*.json")
	if err != nil {
		return ApplyManagedConfigResult{}, fmt.Errorf("create daemon config temp file: %w", err)
	}
	inputPath := inputFile.Name()
	defer os.Remove(inputPath)
	if _, err := inputFile.WriteString(preview.Preview.Content); err != nil {
		_ = inputFile.Close()
		return ApplyManagedConfigResult{}, fmt.Errorf("write daemon config temp file: %w", err)
	}
	if err := inputFile.Close(); err != nil {
		return ApplyManagedConfigResult{}, fmt.Errorf("close daemon config temp file: %w", err)
	}

	output, runErr := s.runHelperCommand(ctx,
		"apply",
		"--config-path", s.daemonConfigPath,
		"--backup-dir", s.backupDir,
		"--unit", s.dockerUnitName,
		"--input", inputPath,
	)

	result, parseErr := parseHelperApplyOutput(output)
	if parseErr != nil {
		if runErr != nil {
			return ApplyManagedConfigResult{}, fmt.Errorf("docker admin helper failed: %w: %s", runErr, strings.TrimSpace(string(output)))
		}
		return ApplyManagedConfigResult{}, parseErr
	}
	result.ChangedKeys = preview.ChangedKeys
	if len(preview.Warnings) > 0 {
		result.Warnings = append(result.Warnings, preview.Warnings...)
	}
	if runErr != nil {
		if result.RolledBack {
			result.Warnings = append(result.Warnings, "Docker daemon apply failed and rollback was attempted.")
		}
		return result, fmt.Errorf("docker admin helper failed: %w", runErr)
	}

	return result, nil
}

func (s *Service) readServiceStatus(ctx context.Context) ServiceStatus {
	status := ServiceStatus{
		Manager:   systemdManagerName,
		UnitName:  s.dockerUnitName,
		Supported: true,
	}

	output, err := s.runCommand(ctx, "systemctl", "show", s.dockerUnitName,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--property=UnitFileState",
		"--property=FragmentPath",
		"--property=ExecMainStartTimestampUSec",
	)
	properties := parseSystemctlShow(output)

	status.LoadState = properties["LoadState"]
	status.ActiveState = properties["ActiveState"]
	status.SubState = properties["SubState"]
	status.UnitFileState = properties["UnitFileState"]
	status.FragmentPath = properties["FragmentPath"]
	if startedAt := parseSystemdMicroTimestamp(properties["ExecMainStartTimestampUSec"]); startedAt != nil {
		status.StartedAt = startedAt
	}

	if len(properties) > 0 {
		return status
	}

	status.Supported = false
	message := commandFailureMessage(err, output, unsupportedManagerMessage)
	status.Message = &message
	return status
}

func (s *Service) readEngineStatus(ctx context.Context) EngineStatus {
	status := EngineStatus{
		ComposeVersion: s.detectComposeVersion(ctx),
	}

	versionOutput, versionErr := s.runCommand(ctx, "docker", "version", "--format", "{{json .Server}}")
	infoOutput, infoErr := s.runCommand(ctx, "docker", "info", "--format", "{{json .}}")

	var version struct {
		Version    string `json:"Version"`
		APIVersion string `json:"APIVersion"`
	}
	versionOK := json.Unmarshal(versionOutput, &version) == nil

	var info struct {
		DockerRootDir string `json:"DockerRootDir"`
		Driver        string `json:"Driver"`
		LoggingDriver string `json:"LoggingDriver"`
		CgroupDriver  string `json:"CgroupDriver"`
	}
	infoOK := json.Unmarshal(infoOutput, &info) == nil

	if versionOK {
		status.Version = version.Version
		status.APIVersion = version.APIVersion
	}
	if infoOK {
		status.RootDir = info.DockerRootDir
		status.Driver = info.Driver
		status.LoggingDriver = info.LoggingDriver
		status.CgroupDriver = info.CgroupDriver
	}

	status.Available = versionOK || infoOK
	if status.Available {
		return status
	}

	message := commandFailureMessage(versionErr, versionOutput, "")
	if strings.TrimSpace(message) == "" {
		message = commandFailureMessage(infoErr, infoOutput, dockerUnavailableDefaultMsg)
	}
	status.Message = &message
	return status
}

func (s *Service) detectComposeVersion(ctx context.Context) string {
	output, err := s.runCommand(ctx, "docker", "compose", "version", "--short")
	if err == nil {
		return strings.TrimSpace(string(output))
	}

	output, err = s.runCommand(ctx, "docker-compose", "version", "--short")
	if err == nil {
		return strings.TrimSpace(string(output))
	}

	return ""
}

func parseSystemctlShow(output []byte) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[parts[0]] = parts[1]
	}
	return values
}

func parseSystemdMicroTimestamp(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return nil
	}

	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}

	timestamp := time.UnixMicro(micros).UTC()
	return &timestamp
}

func commandFailureMessage(err error, output []byte, fallback string) string {
	text := strings.TrimSpace(string(output))
	if text != "" {
		return text
	}
	if err != nil {
		return err.Error()
	}
	return fallback
}

func sortedKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func summarizeDaemonConfig(values map[string]json.RawMessage) DaemonConfigSummary {
	summary := DaemonConfigSummary{
		DNS:                []string{},
		RegistryMirrors:    []string{},
		InsecureRegistries: []string{},
	}

	decodeStringSlice(values["dns"], &summary.DNS)
	decodeStringSlice(values["registry-mirrors"], &summary.RegistryMirrors)
	decodeStringSlice(values["insecure-registries"], &summary.InsecureRegistries)
	decodeString(values["log-driver"], &summary.LogDriver)
	decodeString(values["data-root"], &summary.DataRoot)
	decodeBool(values["live-restore"], &summary.LiveRestore)

	return summary
}

func (s *Service) writeCapability(ctx context.Context) WriteCapability {
	response := WriteCapability{
		Supported:   false,
		ManagedKeys: append([]string(nil), supportedManagedKeys...),
	}
	if s.helperPath == "" {
		reason := writeUnsupportedMessage
		response.Reason = &reason
		return response
	}
	if strings.TrimSpace(s.backupDir) == "" {
		reason := "Docker admin backup directory is not configured."
		response.Reason = &reason
		return response
	}
	if info, err := os.Stat(s.helperPath); err != nil || info.IsDir() {
		reason := fmt.Sprintf("Docker admin helper is unavailable at %s.", s.helperPath)
		response.Reason = &reason
		return response
	}
	if s.useSudo {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		output, err := s.runHelperCommand(probeCtx)
		if err != nil {
			message := strings.TrimSpace(string(output))
			lower := strings.ToLower(message)
			switch {
			case strings.Contains(lower, "no new privileges"):
				reason := "Docker admin helper requires NoNewPrivileges=false in stacklab.service."
				response.Reason = &reason
				return response
			case strings.Contains(lower, "a password is required"),
				strings.Contains(lower, "not allowed to execute"),
				strings.Contains(lower, "may not run sudo"):
				reason := "Docker admin helper sudoers is not configured correctly."
				response.Reason = &reason
				return response
			}
		}
	}
	response.Supported = true
	return response
}

func hasManagedChanges(request ValidateManagedConfigRequest) bool {
	if len(request.RemoveKeys) > 0 {
		return true
	}
	return request.Settings.DNS != nil ||
		request.Settings.RegistryMirrors != nil ||
		request.Settings.InsecureRegistries != nil ||
		request.Settings.LiveRestore != nil
}

func validateManagedRequest(request ValidateManagedConfigRequest) error {
	seen := map[string]struct{}{}
	for _, key := range request.RemoveKeys {
		normalized := strings.TrimSpace(key)
		if _, ok := managedKeyMap[normalized]; !ok {
			return fmt.Errorf("%w: remove_keys contains unsupported key %q", ErrInvalidManagedInput, key)
		}
		if _, ok := seen[normalized]; ok {
			return fmt.Errorf("%w: remove_keys contains duplicate key %q", ErrInvalidManagedInput, key)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func (s *Service) loadEditableConfig(ctx context.Context) (DaemonConfigResponse, map[string]json.RawMessage, error) {
	response, err := s.DaemonConfig(ctx)
	if err != nil {
		return DaemonConfigResponse{}, nil, err
	}
	if response.Exists && response.Permissions != nil && !response.Permissions.Readable {
		return response, nil, ErrUnreadableConfig
	}
	if !response.ValidJSON {
		return response, nil, ErrInvalidDaemonConfig
	}
	if response.Content == nil || strings.TrimSpace(*response.Content) == "" {
		return response, map[string]json.RawMessage{}, nil
	}

	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*response.Content), &values); err != nil {
		return response, nil, ErrInvalidDaemonConfig
	}
	return response, values, nil
}

func cloneRawMap(values map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		clonedValue := append(json.RawMessage(nil), value...)
		cloned[key] = clonedValue
	}
	return cloned
}

func applyManagedSettings(values map[string]json.RawMessage, settings ManagedSettings) {
	if settings.DNS != nil {
		values[managedKeyMap["dns"]] = mustMarshalJSON(*settings.DNS)
	}
	if settings.RegistryMirrors != nil {
		values[managedKeyMap["registry_mirrors"]] = mustMarshalJSON(*settings.RegistryMirrors)
	}
	if settings.InsecureRegistries != nil {
		values[managedKeyMap["insecure_registries"]] = mustMarshalJSON(*settings.InsecureRegistries)
	}
	if settings.LiveRestore != nil {
		values[managedKeyMap["live_restore"]] = mustMarshalJSON(*settings.LiveRestore)
	}
}

func applyManagedKeyRemovals(values map[string]json.RawMessage, removeKeys []string) {
	for _, key := range removeKeys {
		if daemonKey, ok := managedKeyMap[strings.TrimSpace(key)]; ok {
			delete(values, daemonKey)
		}
	}
}

func changedManagedKeys(before, after map[string]json.RawMessage) []string {
	changed := make([]string, 0, len(supportedManagedKeys))
	for _, managedKey := range supportedManagedKeys {
		daemonKey := managedKeyMap[managedKey]
		if strings.TrimSpace(string(before[daemonKey])) != strings.TrimSpace(string(after[daemonKey])) {
			changed = append(changed, managedKey)
		}
	}
	return changed
}

func marshalDaemonConfig(values map[string]json.RawMessage) (string, error) {
	if len(values) == 0 {
		return "{}\n", nil
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal docker daemon config: %w", err)
	}
	return string(encoded) + "\n", nil
}

func mustMarshalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal managed docker daemon value: %v", err))
	}
	return encoded
}

func (s *Service) runHelperCommand(ctx context.Context, args ...string) ([]byte, error) {
	if s.useSudo {
		sudoArgs := append([]string{"-n", "--", s.helperPath}, args...)
		return s.runCommand(ctx, "sudo", sudoArgs...)
	}
	return s.runCommand(ctx, s.helperPath, args...)
}

type helperApplyOutput struct {
	BackupPath         string   `json:"backup_path"`
	RolledBack         bool     `json:"rolled_back"`
	RollbackSucceeded  bool     `json:"rollback_succeeded"`
	ServiceActiveState string   `json:"service_active_state"`
	Warnings           []string `json:"warnings"`
}

func parseHelperApplyOutput(output []byte) (ApplyManagedConfigResult, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ApplyManagedConfigResult{}, errors.New("docker admin helper produced empty output")
	}

	var decoded helperApplyOutput
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		found := false
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var candidate helperApplyOutput
			if json.Unmarshal([]byte(line), &candidate) == nil {
				decoded = candidate
				found = true
			}
		}
		if !found {
			return ApplyManagedConfigResult{}, fmt.Errorf("parse docker admin helper output: %w", err)
		}
	}

	return ApplyManagedConfigResult{
		BackupPath:         decoded.BackupPath,
		RolledBack:         decoded.RolledBack,
		RollbackSucceeded:  decoded.RollbackSucceeded,
		ServiceActiveState: decoded.ServiceActiveState,
		Warnings:           append([]string(nil), decoded.Warnings...),
	}, nil
}

func decodeStringSlice(raw json.RawMessage, target *[]string) {
	if len(raw) == 0 {
		return
	}
	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err == nil {
		*target = decoded
	}
}

func decodeString(raw json.RawMessage, target *string) {
	if len(raw) == 0 {
		return
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err == nil {
		*target = decoded
	}
}

func decodeBool(raw json.RawMessage, target **bool) {
	if len(raw) == 0 {
		return
	}
	var decoded bool
	if err := json.Unmarshal(raw, &decoded); err == nil {
		*target = &decoded
	}
}
