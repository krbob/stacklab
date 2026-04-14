package dockerregistryauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"stacklab/internal/config"
	"stacklab/internal/fsmeta"
)

var ErrInvalidInput = errors.New("invalid docker registry auth input")

type commandRunner func(ctx context.Context, stdin string, env []string, name string, args ...string) ([]byte, error)

type Service struct {
	fallbackDockerConfigDir string
	runCommand              commandRunner
}

func NewService(cfg config.Config) *Service {
	return &Service{
		fallbackDockerConfigDir: filepath.Join(cfg.DataDir, "docker"),
		runCommand:              defaultCommandRunner,
	}
}

func defaultCommandRunner(ctx context.Context, stdin string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func (s *Service) Status(ctx context.Context) (StatusResponse, error) {
	_ = ctx

	path := s.configPath()
	response := StatusResponse{
		DockerConfigPath: path,
		ValidJSON:        true,
		Items:            []RegistryEntry{},
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return response, nil
		}
		return response, err
	}

	response.Exists = true
	sizeBytes := info.Size()
	modifiedAt := info.ModTime().UTC()
	permissions := fsmeta.Inspect(path, info)
	response.SizeBytes = &sizeBytes
	response.ModifiedAt = &modifiedAt
	response.Permissions = &permissions

	if !permissions.Readable {
		return response, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return response, err
	}
	if strings.TrimSpace(string(content)) == "" {
		return response, nil
	}

	var dockerConfig dockerConfigFile
	if err := json.Unmarshal(content, &dockerConfig); err != nil {
		response.ValidJSON = false
		message := err.Error()
		response.ParseError = &message
		return response, nil
	}

	response.Items = make([]RegistryEntry, 0, len(dockerConfig.Auths))
	for registry, entry := range dockerConfig.Auths {
		response.Items = append(response.Items, RegistryEntry{
			Registry:   registry,
			Configured: true,
			Username:   discoverUsername(entry),
			Source:     "docker_config",
			LastError:  "",
		})
	}
	sort.Slice(response.Items, func(i, j int) bool {
		return response.Items[i].Registry < response.Items[j].Registry
	})

	return response, nil
}

func (s *Service) Login(ctx context.Context, request LoginRequest) (string, error) {
	if err := validateLoginRequest(request); err != nil {
		return "", err
	}

	output, err := s.runCommand(ctx, request.Password+"\n", s.commandEnv(), "docker", "login", request.Registry, "--username", request.Username, "--password-stdin")
	return strings.TrimSpace(string(output)), err
}

func (s *Service) Logout(ctx context.Context, request LogoutRequest) (string, error) {
	if err := validateLogoutRequest(request); err != nil {
		return "", err
	}

	output, err := s.runCommand(ctx, "", s.commandEnv(), "docker", "logout", request.Registry)
	return strings.TrimSpace(string(output)), err
}

func validateLoginRequest(request LoginRequest) error {
	if strings.TrimSpace(request.Registry) == "" {
		return fmt.Errorf("%w: registry is required", ErrInvalidInput)
	}
	if strings.TrimSpace(request.Username) == "" {
		return fmt.Errorf("%w: username is required", ErrInvalidInput)
	}
	if request.Password == "" {
		return fmt.Errorf("%w: password is required", ErrInvalidInput)
	}
	return nil
}

func validateLogoutRequest(request LogoutRequest) error {
	if strings.TrimSpace(request.Registry) == "" {
		return fmt.Errorf("%w: registry is required", ErrInvalidInput)
	}
	return nil
}

func (s *Service) configPath() string {
	return filepath.Join(s.effectiveDockerConfigDir(), "config.json")
}

func (s *Service) commandEnv() []string {
	configDir := s.effectiveDockerConfigDir()
	return []string{
		"DOCKER_CONFIG=" + configDir,
		"HOME=" + s.effectiveHomeDir(configDir),
	}
}

func (s *Service) effectiveDockerConfigDir() string {
	if path := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); path != "" {
		return path
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".docker")
	}
	return s.fallbackDockerConfigDir
}

func (s *Service) effectiveHomeDir(configDir string) string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home
	}
	return filepath.Dir(configDir)
}

type dockerConfigFile struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Auth          string `json:"auth,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
}

func discoverUsername(entry dockerAuthEntry) string {
	if entry.Auth == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		return ""
	}
	username, _, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return ""
	}
	return username
}
