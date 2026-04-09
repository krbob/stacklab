package dockeradmin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stacklab/internal/config"
)

func TestOverviewUsesSystemctlDockerAndDaemonConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	daemonPath := filepath.Join(tempDir, "daemon.json")
	if err := os.WriteFile(daemonPath, []byte("{\n  \"dns\": [\"192.168.1.2\"],\n  \"log-driver\": \"json-file\"\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(daemon.json) error = %v", err)
	}

	service := NewService(config.Config{
		DockerSystemdUnitName:  "docker.service",
		DockerDaemonConfigPath: daemonPath,
	})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "systemctl":
			return []byte(strings.Join([]string{
				"LoadState=loaded",
				"ActiveState=active",
				"SubState=running",
				"UnitFileState=enabled",
				"FragmentPath=/lib/systemd/system/docker.service",
				"ExecMainStartTimestampUSec=1712598000000000",
			}, "\n")), nil
		case name == "docker" && len(args) >= 2 && args[0] == "version":
			return []byte(`{"Version":"28.5.1","APIVersion":"1.51"}`), nil
		case name == "docker" && len(args) >= 2 && args[0] == "info":
			return []byte(`{"DockerRootDir":"/var/lib/docker","Driver":"overlay2","LoggingDriver":"json-file","CgroupDriver":"systemd"}`), nil
		case name == "docker" && len(args) >= 2 && args[0] == "compose":
			return []byte("2.39.2\n"), nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return nil, nil
		}
	}

	response, err := service.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}

	if !response.Service.Supported || response.Service.ActiveState != "active" {
		t.Fatalf("unexpected service status: %#v", response.Service)
	}
	if !response.Engine.Available || response.Engine.Version != "28.5.1" || response.Engine.ComposeVersion != "2.39.2" {
		t.Fatalf("unexpected engine status: %#v", response.Engine)
	}
	if !response.DaemonConfig.Exists || !response.DaemonConfig.ValidJSON {
		t.Fatalf("unexpected daemon config meta: %#v", response.DaemonConfig)
	}
	if len(response.DaemonConfig.Summary.DNS) != 1 || response.DaemonConfig.Summary.DNS[0] != "192.168.1.2" {
		t.Fatalf("unexpected daemon config summary: %#v", response.DaemonConfig.Summary)
	}
	if response.WriteCapability.Supported {
		t.Fatalf("expected write capability to be disabled in this slice, got %#v", response.WriteCapability)
	}
}

func TestDaemonConfigHandlesInvalidJSON(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	daemonPath := filepath.Join(tempDir, "daemon.json")
	if err := os.WriteFile(daemonPath, []byte("{ invalid json"), 0o644); err != nil {
		t.Fatalf("WriteFile(daemon.json) error = %v", err)
	}

	service := NewService(config.Config{DockerDaemonConfigPath: daemonPath})

	response, err := service.DaemonConfig(context.Background())
	if err != nil {
		t.Fatalf("DaemonConfig() error = %v", err)
	}
	if !response.Exists || response.ValidJSON || response.ParseError == nil {
		t.Fatalf("unexpected invalid daemon config response: %#v", response)
	}
	if response.Content == nil || !strings.Contains(*response.Content, "invalid json") {
		t.Fatalf("expected raw content in invalid daemon config response: %#v", response)
	}
}

func TestOverviewDegradesWhenSystemctlAndDockerUnavailable(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{})
	service.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	response, err := service.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if response.Service.Supported {
		t.Fatalf("expected unsupported service status, got %#v", response.Service)
	}
	if response.Engine.Available {
		t.Fatalf("expected unavailable engine status, got %#v", response.Engine)
	}
	if response.DaemonConfig.Path == "" {
		t.Fatalf("expected default daemon config path, got %#v", response.DaemonConfig)
	}
}

func TestValidateManagedConfigMergesSupportedKeysAndPreservesUnknownKeys(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	daemonPath := filepath.Join(tempDir, "daemon.json")
	if err := os.WriteFile(daemonPath, []byte("{\n  \"dns\": [\"192.168.1.2\"],\n  \"live-restore\": false,\n  \"log-driver\": \"json-file\"\n}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(daemon.json) error = %v", err)
	}

	service := NewService(config.Config{DockerDaemonConfigPath: daemonPath})
	response, err := service.ValidateManagedConfig(context.Background(), ValidateManagedConfigRequest{
		Settings: ManagedSettings{
			DNS:         &[]string{"1.1.1.1", "8.8.8.8"},
			LiveRestore: pointerToBool(true),
		},
	})
	if err != nil {
		t.Fatalf("ValidateManagedConfig() error = %v", err)
	}

	if !strings.Contains(response.Preview.Content, "\"log-driver\": \"json-file\"") {
		t.Fatalf("expected preview to preserve unknown keys, got %s", response.Preview.Content)
	}
	if !strings.Contains(response.Preview.Content, "\"live-restore\": true") {
		t.Fatalf("expected preview to update live-restore, got %s", response.Preview.Content)
	}
	if len(response.ChangedKeys) != 2 || response.ChangedKeys[0] != "dns" || response.ChangedKeys[1] != "live_restore" {
		t.Fatalf("unexpected changed keys: %#v", response.ChangedKeys)
	}
}

func TestValidateManagedConfigRejectsInvalidExistingJSON(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	daemonPath := filepath.Join(tempDir, "daemon.json")
	if err := os.WriteFile(daemonPath, []byte("{ invalid json"), 0o644); err != nil {
		t.Fatalf("WriteFile(daemon.json) error = %v", err)
	}

	service := NewService(config.Config{DockerDaemonConfigPath: daemonPath})
	_, err := service.ValidateManagedConfig(context.Background(), ValidateManagedConfigRequest{
		Settings: ManagedSettings{
			DNS: &[]string{"1.1.1.1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), ErrInvalidDaemonConfig.Error()) {
		t.Fatalf("ValidateManagedConfig() error = %v, want invalid daemon config", err)
	}
}

func pointerToBool(value bool) *bool {
	return &value
}
