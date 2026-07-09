package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultDockerDaemonConfig = "/etc/docker/daemon.json"
	defaultDockerUnitName     = "docker.service"
	defaultDockerAdminBackup  = "/var/lib/stacklab/docker-admin"
	defaultStacklabDataDir    = "/var/lib/stacklab"
)

var stacklabEnvFilePath = "/etc/stacklab/stacklab.env"

type emittedError struct {
	error
}

type applyResult struct {
	BackupPath         string   `json:"backup_path,omitempty"`
	RolledBack         bool     `json:"rolled_back"`
	RollbackSucceeded  bool     `json:"rollback_succeeded"`
	ServiceActiveState string   `json:"service_active_state,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}

type dockerAdminPolicy struct {
	ConfigPath string
	BackupDir  string
	UnitName   string
}

func main() {
	if len(os.Args) < 2 {
		failJSON(fmt.Errorf("usage: stacklab-docker-admin-helper apply --config-path <path> --backup-dir <dir> --unit <unit> --input <file>"))
	}

	switch os.Args[1] {
	case "apply":
		if err := runApply(os.Args[2:]); err != nil {
			var emitted *emittedError
			if errors.As(err, &emitted) {
				os.Exit(1)
			}
			failJSON(err)
		}
	default:
		failJSON(fmt.Errorf("unknown subcommand %q", os.Args[1]))
	}
}

func runApply(args []string) error {
	var configPath string
	var backupDir string
	var unitName string
	var inputPath string

	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&configPath, "config-path", "", "absolute path to daemon.json")
	flags.StringVar(&backupDir, "backup-dir", "", "absolute path to backup directory")
	flags.StringVar(&unitName, "unit", "docker.service", "systemd unit name")
	flags.StringVar(&inputPath, "input", "", "path to validated daemon.json content")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if !filepath.IsAbs(configPath) || !filepath.IsAbs(backupDir) {
		return errors.New("config-path and backup-dir must be absolute paths")
	}
	if strings.TrimSpace(inputPath) == "" {
		return errors.New("input is required")
	}
	if err := validateApplyPolicy(configPath, backupDir, unitName); err != nil {
		return err
	}

	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read input file: %w", err)
	}
	if !json.Valid(content) {
		return errors.New("input file does not contain valid JSON")
	}

	result := applyResult{}
	previousContent, previousExists, err := backupExistingConfig(configPath, backupDir, &result)
	if err != nil {
		return err
	}

	if err := atomicWrite(configPath, content, 0o644); err != nil {
		return fmt.Errorf("write daemon config: %w", err)
	}

	if err := restartAndVerify(unitName, &result); err != nil {
		result.Warnings = append(result.Warnings, "Docker restart failed; attempting rollback.")
		result.RolledBack = true
		if rollbackErr := rollbackConfig(configPath, previousContent, previousExists); rollbackErr != nil {
			result.RollbackSucceeded = false
			emitResult(result)
			return &emittedError{fmt.Errorf("restart failed: %v; rollback failed: %w", err, rollbackErr)}
		}
		result.RollbackSucceeded = true
		if rollbackRestartErr := restartAndVerify(unitName, &result); rollbackRestartErr != nil {
			emitResult(result)
			return &emittedError{fmt.Errorf("restart failed: %v; rollback restart failed: %w", err, rollbackRestartErr)}
		}
		emitResult(result)
		return &emittedError{fmt.Errorf("restart failed and config was rolled back: %w", err)}
	}

	emitResult(result)
	return nil
}

func validateApplyPolicy(configPath, backupDir, unitName string) error {
	policy, err := loadDockerAdminPolicy()
	if err != nil {
		return err
	}

	cleanConfig, err := cleanAbsolutePath(configPath)
	if err != nil {
		return fmt.Errorf("config-path is invalid: %w", err)
	}
	cleanBackup, err := cleanAbsolutePath(backupDir)
	if err != nil {
		return fmt.Errorf("backup-dir is invalid: %w", err)
	}
	if cleanConfig != policy.ConfigPath {
		return fmt.Errorf("config-path %q is not allowed", configPath)
	}
	if cleanBackup != policy.BackupDir {
		return fmt.Errorf("backup-dir %q is not allowed", backupDir)
	}
	if strings.TrimSpace(unitName) != policy.UnitName {
		return fmt.Errorf("unit %q is not allowed", unitName)
	}
	return nil
}

func loadDockerAdminPolicy() (dockerAdminPolicy, error) {
	values, err := loadStacklabEnvValues(stacklabEnvFilePath)
	if err != nil {
		return dockerAdminPolicy{}, err
	}

	backupDir := strings.TrimSpace(values["STACKLAB_DOCKER_ADMIN_BACKUP_DIR"])
	if backupDir == "" {
		dataDir := strings.TrimSpace(values["STACKLAB_DATA_DIR"])
		if dataDir == "" {
			dataDir = defaultStacklabDataDir
		}
		backupDir = filepath.Join(dataDir, "docker-admin")
	}

	configPath, err := cleanAbsolutePath(valueOrDefault(values["STACKLAB_DOCKER_DAEMON_CONFIG_PATH"], defaultDockerDaemonConfig))
	if err != nil {
		return dockerAdminPolicy{}, fmt.Errorf("configured Docker daemon config path is invalid: %w", err)
	}
	cleanBackup, err := cleanAbsolutePath(backupDir)
	if err != nil {
		return dockerAdminPolicy{}, fmt.Errorf("configured Docker admin backup directory is invalid: %w", err)
	}
	unitName := strings.TrimSpace(valueOrDefault(values["STACKLAB_DOCKER_SYSTEMD_UNIT"], defaultDockerUnitName))
	if unitName == "" || strings.ContainsAny(unitName, "/\x00") {
		return dockerAdminPolicy{}, fmt.Errorf("configured Docker unit %q is invalid", unitName)
	}

	return dockerAdminPolicy{
		ConfigPath: configPath,
		BackupDir:  cleanBackup,
		UnitName:   unitName,
	}, nil
}

func loadStacklabEnvValues(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func cleanAbsolutePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	return filepath.Clean(path), nil
}

func backupExistingConfig(configPath, backupDir string, result *applyResult) ([]byte, bool, error) {
	info, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, os.MkdirAll(backupDir, 0o755)
		}
		return nil, false, fmt.Errorf("stat current daemon config: %w", err)
	}
	if info.IsDir() {
		return nil, false, errors.New("daemon config path is a directory")
	}

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("create backup directory: %w", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, false, fmt.Errorf("read current daemon config: %w", err)
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("daemon-%s.json", time.Now().UTC().Format("20060102T150405Z")))
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		return nil, false, fmt.Errorf("write daemon config backup: %w", err)
	}
	result.BackupPath = backupPath
	return content, true, nil
}

func atomicWrite(targetPath string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tempFile, err := os.CreateTemp(dir, ".stacklab-daemon-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, targetPath)
}

func restartAndVerify(unitName string, result *applyResult) error {
	restart := exec.Command("systemctl", "restart", unitName)
	if output, err := restart.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", unitName, err, strings.TrimSpace(string(output)))
	}

	show := exec.Command("systemctl", "show", unitName, "--property=ActiveState", "--value")
	output, err := show.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl show %s: %w: %s", unitName, err, strings.TrimSpace(string(output)))
	}
	activeState := strings.TrimSpace(string(output))
	result.ServiceActiveState = activeState
	if activeState != "active" {
		return fmt.Errorf("docker service active state is %q", activeState)
	}
	return nil
}

func rollbackConfig(configPath string, previousContent []byte, previousExists bool) error {
	if !previousExists {
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove daemon config after failed apply: %w", err)
		}
		return nil
	}
	return atomicWrite(configPath, previousContent, 0o644)
}

func emitResult(result applyResult) {
	encoded, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintf(os.Stdout, "{\"rolled_back\":%t,\"rollback_succeeded\":%t}\n", result.RolledBack, result.RollbackSucceeded)
		return
	}
	fmt.Fprintln(os.Stdout, string(encoded))
}

func failJSON(err error) {
	result := applyResult{
		RolledBack:        false,
		RollbackSucceeded: false,
		Warnings:          []string{err.Error()},
	}
	emitResult(result)
	os.Exit(1)
}
