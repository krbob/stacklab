package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
