package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateApplyPolicyRejectsUnexpectedConfigPath(t *testing.T) {
	withDockerAdminEnv(t, "")

	err := validateApplyPolicy("/tmp/pwn.json", defaultDockerAdminBackup, defaultDockerUnitName)
	if err == nil || !strings.Contains(err.Error(), "config-path") {
		t.Fatalf("validateApplyPolicy() error = %v, want config-path rejection", err)
	}
}

func TestValidateApplyPolicyRejectsUnexpectedBackupDir(t *testing.T) {
	withDockerAdminEnv(t, "")

	err := validateApplyPolicy(defaultDockerDaemonConfig, "/tmp/backups", defaultDockerUnitName)
	if err == nil || !strings.Contains(err.Error(), "backup-dir") {
		t.Fatalf("validateApplyPolicy() error = %v, want backup-dir rejection", err)
	}
}

func TestValidateApplyPolicyRejectsUnexpectedUnit(t *testing.T) {
	withDockerAdminEnv(t, "")

	err := validateApplyPolicy(defaultDockerDaemonConfig, defaultDockerAdminBackup, "ssh.service")
	if err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("validateApplyPolicy() error = %v, want unit rejection", err)
	}
}

func TestValidateApplyPolicyUsesRootOwnedEnvFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "daemon.json")
	backupDir := filepath.Join(tempDir, "backups")
	withDockerAdminEnv(t, strings.Join([]string{
		"STACKLAB_DOCKER_DAEMON_CONFIG_PATH=" + configPath,
		"STACKLAB_DOCKER_ADMIN_BACKUP_DIR=" + backupDir,
		"STACKLAB_DOCKER_SYSTEMD_UNIT=custom-docker.service",
		"",
	}, "\n"))

	if err := validateApplyPolicy(configPath, backupDir, "custom-docker.service"); err != nil {
		t.Fatalf("validateApplyPolicy() error = %v", err)
	}
}

func TestValidateApplyPolicyIgnoresProcessEnvironment(t *testing.T) {
	withDockerAdminEnv(t, "")
	t.Setenv("STACKLAB_DOCKER_DAEMON_CONFIG_PATH", "/tmp/pwn.json")

	err := validateApplyPolicy("/tmp/pwn.json", defaultDockerAdminBackup, defaultDockerUnitName)
	if err == nil || !strings.Contains(err.Error(), "config-path") {
		t.Fatalf("validateApplyPolicy() error = %v, want config-path rejection", err)
	}
}

func TestValidateApplyPolicyDefaultsBackupDirFromConfiguredDataDir(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	withDockerAdminEnv(t, "STACKLAB_DATA_DIR="+dataDir+"\n")

	if err := validateApplyPolicy(defaultDockerDaemonConfig, filepath.Join(dataDir, "docker-admin"), defaultDockerUnitName); err != nil {
		t.Fatalf("validateApplyPolicy() error = %v", err)
	}
}

func TestRunProbeRejectsUnexpectedPositionalArguments(t *testing.T) {
	withDockerAdminEnv(t, "")

	err := runProbe([]string{"extra"})
	if err == nil || !strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("runProbe() error = %v, want unexpected positional rejection", err)
	}
}

func TestRunApplyRejectsUnexpectedPositionalArguments(t *testing.T) {
	withDockerAdminEnv(t, "")

	err := runApply([]string{
		"--config-path", defaultDockerDaemonConfig,
		"--backup-dir", defaultDockerAdminBackup,
		"--unit", defaultDockerUnitName,
		"--input", filepath.Join(t.TempDir(), "daemon.json"),
		"extra",
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected positional") {
		t.Fatalf("runApply() error = %v, want unexpected positional rejection", err)
	}
}

func TestWriteUniqueBackupNeverOverwritesSameTimestamp(t *testing.T) {
	t.Parallel()

	backupDir := t.TempDir()
	createdAt := time.Date(2026, time.July, 11, 12, 34, 56, 123, time.UTC)
	firstPath, err := writeUniqueBackup(backupDir, []byte("first\n"), createdAt)
	if err != nil {
		t.Fatalf("writeUniqueBackup(first) error = %v", err)
	}
	secondPath, err := writeUniqueBackup(backupDir, []byte("second\n"), createdAt)
	if err != nil {
		t.Fatalf("writeUniqueBackup(second) error = %v", err)
	}
	if firstPath == secondPath {
		t.Fatalf("backup paths are equal: %q", firstPath)
	}
	assertBackup(t, firstPath, []byte("first\n"))
	assertBackup(t, secondPath, []byte("second\n"))
}

func TestAtomicWriteReplacesContentAndCleansTemporaryFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "daemon.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile(daemon.json) error = %v", err)
	}
	if err := atomicWrite(path, []byte(`{"new":true}`), 0o640); err != nil {
		t.Fatalf("atomicWrite() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(daemon.json) error = %v", err)
	}
	if !bytes.Equal(content, []byte(`{"new":true}`)) {
		t.Fatalf("daemon.json content = %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(daemon.json) error = %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("daemon.json mode = %04o, want 0640", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".stacklab-daemon-*.tmp"))
	if err != nil {
		t.Fatalf("Glob(temp files) error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func assertBackup(t *testing.T, path string, want []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !bytes.Equal(content, want) {
		t.Fatalf("ReadFile(%s) = %q, want %q", path, content, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode(%s) = %04o, want 0600", path, info.Mode().Perm())
	}
}

func withDockerAdminEnv(t *testing.T, content string) {
	t.Helper()

	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "stacklab.env")
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(stacklab.env) error = %v", err)
	}

	original := stacklabEnvFilePath
	stacklabEnvFilePath = envPath
	t.Cleanup(func() {
		stacklabEnvFilePath = original
	})
}
