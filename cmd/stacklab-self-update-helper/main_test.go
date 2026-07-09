package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAPTUpgradeArgsPinsRequestedVersion(t *testing.T) {
	t.Parallel()

	got := aptUpgradeArgs("stacklab", "2026.07.09~nightly")
	want := []string{
		"install",
		"-y",
		"--only-upgrade",
		"-o",
		"Dpkg::Options::=--force-confold",
		"--",
		"stacklab=2026.07.09~nightly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptUpgradeArgs() = %#v, want %#v", got, want)
	}
}

func TestAPTUpgradeArgsAllowsLatestWhenVersionMissing(t *testing.T) {
	t.Parallel()

	got := aptUpgradeArgs("stacklab", "")
	want := []string{
		"install",
		"-y",
		"--only-upgrade",
		"-o",
		"Dpkg::Options::=--force-confold",
		"--",
		"stacklab",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptUpgradeArgs() = %#v, want %#v", got, want)
	}
}

func TestValidateRunPolicyAllowsConfiguredSelfUpdateInputs(t *testing.T) {
	dataDir := t.TempDir()
	withStacklabEnv(t, strings.Join([]string{
		"STACKLAB_DATA_DIR=" + dataDir,
		"STACKLAB_SELF_UPDATE_PACKAGE_NAME=stacklab",
		"STACKLAB_SELF_UPDATE_HEALTH_URL=http://127.0.0.1:8080/api/health",
		"STACKLAB_SYSTEMD_UNIT=stacklab",
	}, "\n"))

	err := validateRunPolicy(
		filepath.Join(dataDir, "stacklab.db"),
		"stacklab",
		"http://127.0.0.1:8080/api/health",
		"stacklab",
		defaultRuntimeKey,
	)
	if err != nil {
		t.Fatalf("validateRunPolicy() error = %v", err)
	}
}

func TestValidateRunPolicyRejectsFlagOverrides(t *testing.T) {
	dataDir := t.TempDir()
	withStacklabEnv(t, "STACKLAB_DATA_DIR="+dataDir+"\n")
	dbPath := filepath.Join(dataDir, "stacklab.db")

	tests := []struct {
		name        string
		dbPath      string
		packageName string
		healthURL   string
		serviceUnit string
		runtimeKey  string
		want        string
	}{
		{
			name:        "db path",
			dbPath:      "/etc/shadow",
			packageName: "stacklab",
			healthURL:   defaultHealthURL,
			serviceUnit: defaultSystemdUnit,
			runtimeKey:  defaultRuntimeKey,
			want:        "db-path",
		},
		{
			name:        "package",
			dbPath:      dbPath,
			packageName: "bash",
			healthURL:   defaultHealthURL,
			serviceUnit: defaultSystemdUnit,
			runtimeKey:  defaultRuntimeKey,
			want:        "package-name",
		},
		{
			name:        "health URL",
			dbPath:      dbPath,
			packageName: "stacklab",
			healthURL:   "http://127.0.0.1:9/api/health",
			serviceUnit: defaultSystemdUnit,
			runtimeKey:  defaultRuntimeKey,
			want:        "health-url",
		},
		{
			name:        "runtime key",
			dbPath:      dbPath,
			packageName: "stacklab",
			healthURL:   defaultHealthURL,
			serviceUnit: defaultSystemdUnit,
			runtimeKey:  "other_key",
			want:        "runtime-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunPolicy(tt.dbPath, tt.packageName, tt.healthURL, tt.serviceUnit, tt.runtimeKey)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateRunPolicy() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateRequestedVersionRejectsUnsafeTokens(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"--force", "1.0 other", "1.0/other"} {
		if err := validateRequestedVersion(version); err == nil {
			t.Fatalf("validateRequestedVersion(%q) error = nil, want error", version)
		}
	}
	if err := validateRequestedVersion("2026.07.09~nightly+abc"); err != nil {
		t.Fatalf("validateRequestedVersion(valid) error = %v", err)
	}
}

func TestLoadSelfUpdatePolicyRejectsInvalidConfiguredHealthURL(t *testing.T) {
	withStacklabEnv(t, "STACKLAB_SELF_UPDATE_HEALTH_URL=https://user:pass@example.test/api/health\n")
	_, err := loadSelfUpdatePolicy()
	if err == nil || !strings.Contains(err.Error(), "health URL") {
		t.Fatalf("loadSelfUpdatePolicy() error = %v, want health URL error", err)
	}
}

func withStacklabEnv(t *testing.T, content string) {
	t.Helper()

	envPath := filepath.Join(t.TempDir(), "stacklab.env")
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(stacklab.env) error = %v", err)
	}

	previous := stacklabEnvFilePath
	stacklabEnvFilePath = envPath
	t.Cleanup(func() {
		stacklabEnvFilePath = previous
	})
}

func TestCleanAbsolutePathRejectsRelativePath(t *testing.T) {
	t.Parallel()

	_, err := cleanAbsolutePath("relative.db")
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("cleanAbsolutePath(relative) error = %v, want absolute path error", err)
	}
}
