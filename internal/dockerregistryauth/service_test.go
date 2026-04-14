package dockerregistryauth

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"stacklab/internal/config"
)

func TestStatusReturnsMissingConfigGracefully(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", filepath.Join(tempDir, "docker"))

	service := NewService(config.Config{DataDir: tempDir})
	response, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if response.Exists || !response.ValidJSON || len(response.Items) != 0 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestStatusParsesConfiguredRegistries(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "docker")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte("bob:secret"))
	content := `{"auths":{"ghcr.io":{"auth":"` + auth + `"},"registry.local:5000":{"identitytoken":"token"}}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(config.json) error = %v", err)
	}
	t.Setenv("DOCKER_CONFIG", configDir)

	service := NewService(config.Config{DataDir: tempDir})
	response, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	got := []RegistryEntry{
		{
			Registry:   response.Items[0].Registry,
			Configured: response.Items[0].Configured,
			Username:   response.Items[0].Username,
			Source:     response.Items[0].Source,
			LastError:  response.Items[0].LastError,
		},
		{
			Registry:   response.Items[1].Registry,
			Configured: response.Items[1].Configured,
			Username:   response.Items[1].Username,
			Source:     response.Items[1].Source,
			LastError:  response.Items[1].LastError,
		},
	}
	want := []RegistryEntry{
		{Registry: "ghcr.io", Configured: true, Username: "bob", Source: "docker_config", LastError: ""},
		{Registry: "registry.local:5000", Configured: true, Username: "", Source: "docker_config", LastError: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Status() items = %#v, want %#v", got, want)
	}
}

func TestStatusReportsInvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "docker")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{ invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile(config.json) error = %v", err)
	}
	t.Setenv("DOCKER_CONFIG", configDir)

	service := NewService(config.Config{DataDir: tempDir})
	response, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if response.ValidJSON || response.ParseError == nil {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestLoginUsesDockerConfigEnvAndPasswordStdin(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "docker")
	t.Setenv("DOCKER_CONFIG", configDir)
	t.Setenv("HOME", filepath.Join(tempDir, "home"))

	var gotStdin string
	var gotEnv []string
	var gotArgs []string

	service := NewService(config.Config{DataDir: tempDir})
	service.runCommand = func(ctx context.Context, stdin string, env []string, name string, args ...string) ([]byte, error) {
		gotStdin = stdin
		gotEnv = append([]string(nil), env...)
		gotArgs = append([]string{name}, args...)
		return []byte("Login Succeeded"), nil
	}

	output, err := service.Login(context.Background(), LoginRequest{
		Registry: "ghcr.io",
		Username: "bob",
		Password: "secret-token",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if output != "Login Succeeded" {
		t.Fatalf("Login() output = %q, want %q", output, "Login Succeeded")
	}
	if gotStdin != "secret-token\n" {
		t.Fatalf("stdin = %q, want password with newline", gotStdin)
	}
	if !reflect.DeepEqual(gotArgs, []string{"docker", "login", "ghcr.io", "--username", "bob", "--password-stdin"}) {
		t.Fatalf("args = %#v", gotArgs)
	}
	if !containsEnv(gotEnv, "DOCKER_CONFIG="+configDir) || !containsEnv(gotEnv, "HOME="+filepath.Join(tempDir, "home")) {
		t.Fatalf("env = %#v", gotEnv)
	}
}

func TestLogoutValidatesRegistry(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{DataDir: t.TempDir()})
	_, err := service.Logout(context.Background(), LogoutRequest{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Logout() error = %v, want ErrInvalidInput", err)
	}
}

func TestLoginRequiresUsernameAndPassword(t *testing.T) {
	t.Parallel()

	service := NewService(config.Config{DataDir: t.TempDir()})
	_, err := service.Login(context.Background(), LoginRequest{Registry: "ghcr.io"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Login() error = %v, want ErrInvalidInput", err)
	}
}

func containsEnv(values []string, needle string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}
