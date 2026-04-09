package main

import (
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

type applyResult struct {
	BackupPath         string   `json:"backup_path,omitempty"`
	RolledBack         bool     `json:"rolled_back"`
	RollbackSucceeded  bool     `json:"rollback_succeeded"`
	ServiceActiveState string   `json:"service_active_state,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		failJSON(fmt.Errorf("usage: stacklab-docker-admin-helper apply --config-path <path> --backup-dir <dir> --unit <unit> --input <file>"))
	}

	switch os.Args[1] {
	case "apply":
		if err := runApply(os.Args[2:]); err != nil {
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
			return fmt.Errorf("restart failed: %v; rollback failed: %w", err, rollbackErr)
		}
		result.RollbackSucceeded = true
		if rollbackRestartErr := restartAndVerify(unitName, &result); rollbackRestartErr != nil {
			emitResult(result)
			return fmt.Errorf("restart failed: %v; rollback restart failed: %w", err, rollbackRestartErr)
		}
		emitResult(result)
		return fmt.Errorf("restart failed and config was rolled back: %w", err)
	}

	emitResult(result)
	return nil
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
