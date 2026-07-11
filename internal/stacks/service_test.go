package stacks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/store"
)

func TestParseComposeServicesParsesImageBuildPortsVolumesAndDependsOn(t *testing.T) {
	t.Parallel()

	stackRoot := filepath.Join("/tmp", "stacklab-test", "demo")
	content := []byte(`
services:
  app:
    image: ghcr.io/example/app:1.2.3
    build:
      context: ./app
      dockerfile: Dockerfile.custom
    ports:
      - "8080:80"
      - target: 8443
        published: 9443
        protocol: tcp
    volumes:
      - ./config:/app/config
      - type: bind
        source: ./data
        target: /app/data
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_started
    healthcheck:
      test: ["CMD", "true"]
  worker:
    build: ./worker
    volumes:
      - worker-data:/var/lib/worker
`)

	services, _, err := parseComposeServices(stackRoot, content)
	if err != nil {
		t.Fatalf("parseComposeServices() error = %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("len(services) = %d, want 2", len(services))
	}

	app := services[0]
	if app.Name != "app" {
		t.Fatalf("app.Name = %q, want %q", app.Name, "app")
	}
	if app.Mode != ServiceModeHybrid {
		t.Fatalf("app.Mode = %q, want %q", app.Mode, ServiceModeHybrid)
	}
	if app.ImageRef == nil || *app.ImageRef != "ghcr.io/example/app:1.2.3" {
		t.Fatalf("app.ImageRef = %#v", app.ImageRef)
	}
	if app.BuildContext == nil || *app.BuildContext != filepath.Join(stackRoot, "app") {
		t.Fatalf("app.BuildContext = %#v", app.BuildContext)
	}
	if app.DockerfilePath == nil || *app.DockerfilePath != filepath.Join(stackRoot, "app", "Dockerfile.custom") {
		t.Fatalf("app.DockerfilePath = %#v", app.DockerfilePath)
	}
	if !app.HealthcheckPresent {
		t.Fatalf("expected app.HealthcheckPresent to be true")
	}
	expectedDependsOn := []string{"db", "redis"}
	if !reflect.DeepEqual(app.DependsOn, expectedDependsOn) {
		t.Fatalf("app.DependsOn = %#v, want %#v", app.DependsOn, expectedDependsOn)
	}
	expectedPorts := []PortMapping{
		{Published: 8080, Target: 80, Protocol: "tcp"},
		{Published: 9443, Target: 8443, Protocol: "tcp"},
	}
	if !reflect.DeepEqual(app.Ports, expectedPorts) {
		t.Fatalf("app.Ports = %#v, want %#v", app.Ports, expectedPorts)
	}
	expectedVolumes := []VolumeMount{
		{Source: filepath.Join(stackRoot, "config"), Target: "/app/config"},
		{Source: filepath.Join(stackRoot, "data"), Target: "/app/data"},
	}
	if !reflect.DeepEqual(app.Volumes, expectedVolumes) {
		t.Fatalf("app.Volumes = %#v, want %#v", app.Volumes, expectedVolumes)
	}

	worker := services[1]
	if worker.Name != "worker" {
		t.Fatalf("worker.Name = %q, want %q", worker.Name, "worker")
	}
	if worker.Mode != ServiceModeBuild {
		t.Fatalf("worker.Mode = %q, want %q", worker.Mode, ServiceModeBuild)
	}
	if worker.BuildContext == nil || *worker.BuildContext != filepath.Join(stackRoot, "worker") {
		t.Fatalf("worker.BuildContext = %#v", worker.BuildContext)
	}
	if worker.ImageRef != nil {
		t.Fatalf("worker.ImageRef = %#v, want nil", worker.ImageRef)
	}
	expectedWorkerVolumes := []VolumeMount{{Source: "worker-data", Target: "/var/lib/worker"}}
	if !reflect.DeepEqual(worker.Volumes, expectedWorkerVolumes) {
		t.Fatalf("worker.Volumes = %#v, want %#v", worker.Volumes, expectedWorkerVolumes)
	}
}

func TestNormalizePortMappingsDeduplicatesIPv4IPv6Bindings(t *testing.T) {
	t.Parallel()

	ports := []PortMapping{
		{Published: 8081, Target: 80, Protocol: "tcp"},
		{Published: 8081, Target: 80, Protocol: "TCP"},
		{Published: 9443, Target: 443, Protocol: "tcp"},
	}
	got := normalizePortMappings(ports)
	want := []PortMapping{
		{Published: 8081, Target: 80, Protocol: "tcp"},
		{Published: 9443, Target: 443, Protocol: "tcp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizePortMappings() = %#v, want %#v", got, want)
	}
}

func TestDefinitionWarningChangedSuppressesRepeatedSignature(t *testing.T) {
	t.Parallel()

	reader := &ServiceReader{}
	if !reader.definitionWarningChanged("invalid", "parse", "yaml error") {
		t.Fatal("first warning should be reported")
	}
	if reader.definitionWarningChanged("invalid", "parse", "yaml error") {
		t.Fatal("repeated warning should be suppressed")
	}
	if !reader.definitionWarningChanged("invalid", "parse", "different yaml error") {
		t.Fatal("changed warning should be reported")
	}
	reader.clearDefinitionWarning("invalid", "parse")
	if !reader.definitionWarningChanged("invalid", "parse", "different yaml error") {
		t.Fatal("warning should be reported after clear")
	}
}

func TestDeriveRuntimeState(t *testing.T) {
	t.Parallel()

	services := []Service{{Name: "app"}, {Name: "db"}}
	tests := []struct {
		name             string
		definitionExists bool
		services         []Service
		containers       []Container
		want             RuntimeState
	}{
		{
			name:             "orphaned runtime",
			definitionExists: false,
			containers:       []Container{{ServiceName: "app", Status: "running"}},
			want:             RuntimeStateOrphaned,
		},
		{
			name:             "defined without runtime",
			definitionExists: true,
			services:         services,
			want:             RuntimeStateDefined,
		},
		{
			name:             "stopped",
			definitionExists: true,
			services:         services,
			containers: []Container{
				{ServiceName: "app", Status: "exited"},
				{ServiceName: "db", Status: "created"},
			},
			want: RuntimeStateStopped,
		},
		{
			name:             "partial",
			definitionExists: true,
			services:         services,
			containers: []Container{
				{ServiceName: "app", Status: "running"},
				{ServiceName: "db", Status: "exited"},
			},
			want: RuntimeStatePartial,
		},
		{
			name:             "error",
			definitionExists: true,
			services:         services,
			containers: []Container{
				{ServiceName: "app", Status: "running"},
				{ServiceName: "db", Status: "restarting"},
			},
			want: RuntimeStateError,
		},
		{
			name:             "running",
			definitionExists: true,
			services:         services,
			containers: []Container{
				{ServiceName: "app", Status: "running"},
				{ServiceName: "db", Status: "running"},
			},
			want: RuntimeStateRunning,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deriveRuntimeState(tt.definitionExists, tt.services, tt.containers)
			if got != tt.want {
				t.Fatalf("deriveRuntimeState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAvailableActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stack discoveredStack
		want  []string
	}{
		{
			name: "orphaned",
			stack: discoveredStack{
				RuntimeState: RuntimeStateOrphaned,
			},
			want: []string{"down"},
		},
		{
			name: "invalid with containers",
			stack: discoveredStack{
				RuntimeState: RuntimeStateStopped,
				ConfigState:  ConfigStateInvalid,
				Containers:   []Container{{ID: "c1"}},
			},
			want: []string{"validate", "save_definition", "stop", "down"},
		},
		{
			name: "defined",
			stack: discoveredStack{
				RuntimeState: RuntimeStateDefined,
				ConfigState:  ConfigStateUnknown,
			},
			want: []string{"validate", "up", "pull", "build", "save_definition", "remove_stack_definition"},
		},
		{
			name: "running",
			stack: discoveredStack{
				RuntimeState: RuntimeStateRunning,
				ConfigState:  ConfigStateUnknown,
			},
			want: []string{"validate", "up", "restart", "stop", "down", "pull", "build", "recreate", "save_definition", "remove_stack_definition"},
		},
		{
			name: "stopped",
			stack: discoveredStack{
				RuntimeState: RuntimeStateStopped,
				ConfigState:  ConfigStateUnknown,
			},
			want: []string{"validate", "up", "down", "pull", "build", "save_definition", "remove_stack_definition"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.stack.availableActions()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("availableActions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCreateListGetSaveAndDeleteStackFilesystemFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()

	if err := reader.EnsureCreateStackAvailable(ctx, stackID); err != nil {
		t.Fatalf("EnsureCreateStackAvailable(before create) error = %v", err)
	}

	createRequest := CreateStackRequest{
		StackID:           stackID,
		ComposeYAML:       "services:\n  app:\n    image: nginx:alpine\n",
		Env:               "",
		CreateConfigDir:   true,
		CreateDataDir:     true,
		DeployAfterCreate: false,
	}
	if err := reader.CreateStack(ctx, createRequest); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	if err := reader.EnsureCreateStackAvailable(ctx, stackID); !errors.Is(err, ErrConflict) {
		t.Fatalf("EnsureCreateStackAvailable(after create) error = %v, want ErrConflict", err)
	}

	listResponse, err := reader.List(ctx, ListQuery{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var listItem *StackListItem
	for i := range listResponse.Items {
		if listResponse.Items[i].ID == stackID {
			listItem = &listResponse.Items[i]
			break
		}
	}
	if listItem == nil {
		t.Fatalf("List() missing stack %q in %#v", stackID, listResponse.Items)
	}
	if listItem.RuntimeState != RuntimeStateDefined {
		t.Fatalf("list runtime_state = %q, want %q", listItem.RuntimeState, RuntimeStateDefined)
	}
	if listItem.ServiceCount.Defined != 1 {
		t.Fatalf("list service_count.defined = %d, want 1", listItem.ServiceCount.Defined)
	}
	composePath := filepath.Join(reader.cfg.RootDir, "stacks", stackID, "compose.yaml")
	if info, err := os.Stat(composePath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("new compose.yaml mode = %v, %v; want 0644", infoMode(info), err)
	}

	detailResponse, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if detailResponse.Stack.ID != stackID {
		t.Fatalf("Get().Stack.ID = %q, want %q", detailResponse.Stack.ID, stackID)
	}
	if len(detailResponse.Stack.Services) != 1 || detailResponse.Stack.Services[0].Name != "app" {
		t.Fatalf("unexpected services: %#v", detailResponse.Stack.Services)
	}
	if err := os.Chmod(composePath, 0o600); err != nil {
		t.Fatalf("Chmod(compose.yaml) error = %v", err)
	}

	savePreview, _, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML:       "services:\n  app:\n    image: nginx:stable\n",
		Env:               "PORT=9090\n",
		ValidateAfterSave: false,
	})
	if err != nil {
		t.Fatalf("SaveDefinition() error = %v", err)
	}
	if !savePreview.Valid {
		t.Fatalf("SaveDefinition() valid = false, want true")
	}
	composeBytes, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(compose.yaml) error = %v", err)
	}
	if !strings.Contains(string(composeBytes), "nginx:stable") {
		t.Fatalf("compose.yaml does not contain updated image: %q", string(composeBytes))
	}
	if info, err := os.Stat(composePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("updated compose.yaml mode = %v, %v; want 0600", infoMode(info), err)
	}
	envPath := filepath.Join(reader.cfg.RootDir, "stacks", stackID, ".env")
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if string(envBytes) != "PORT=9090\n" {
		t.Fatalf(".env content = %q, want %q", string(envBytes), "PORT=9090\n")
	}
	if info, err := os.Stat(envPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new .env mode = %v, %v; want 0600", infoMode(info), err)
	}
	if err := os.Chmod(envPath, 0o644); err != nil {
		t.Fatalf("Chmod(.env) error = %v", err)
	}
	if _, _, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML:       "services:\n  app:\n    image: nginx:stable\n",
		Env:               "PORT=9091\n",
		ValidateAfterSave: false,
	}); err != nil {
		t.Fatalf("SaveDefinition(existing .env) error = %v", err)
	}
	if info, err := os.Stat(envPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("updated .env mode = %v, %v; want 0600", infoMode(info), err)
	}

	if err := reader.DeleteStack(ctx, stackID, DeleteStackRequest{
		RemoveRuntime:    false,
		RemoveDefinition: true,
		RemoveConfig:     true,
		RemoveData:       true,
	}); err != nil {
		t.Fatalf("DeleteStack() error = %v", err)
	}

	assertMissing(t, filepath.Join(reader.cfg.RootDir, "stacks", stackID))
	assertMissing(t, filepath.Join(reader.cfg.RootDir, "config", stackID))
	assertMissing(t, filepath.Join(reader.cfg.RootDir, "data", stackID))
}

func TestNewServiceReaderSecuresExistingStackEnvironmentFiles(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	stackDir := filepath.Join(rootDir, "stacks", "demo")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(stackDir) error = %v", err)
	}
	envPath := filepath.Join(stackDir, ".env")
	composePath := filepath.Join(stackDir, "compose.yaml")
	for _, path := range []string{envPath, composePath} {
		if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("Chmod(%s) error = %v", path, err)
		}
	}

	_ = NewServiceReader(config.Config{RootDir: rootDir}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if info, err := os.Stat(envPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("existing .env mode = %v, %v; want 0600", infoMode(info), err)
	}
	if info, err := os.Stat(composePath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("existing compose.yaml mode = %v, %v; want 0644", infoMode(info), err)
	}
}

func TestDeleteStackWithRuntimeStopsWhenDockerUnavailable(t *testing.T) {
	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()

	shimDir := t.TempDir()
	dockerPath := filepath.Join(shimDir, "docker")
	if err := os.WriteFile(dockerPath, []byte("#!/bin/sh\necho docker unavailable >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(docker shim) error = %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:         stackID,
		ComposeYAML:     "services:\n  app:\n    image: nginx:alpine\n",
		CreateConfigDir: true,
		CreateDataDir:   true,
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	err := reader.DeleteStack(ctx, stackID, DeleteStackRequest{
		RemoveRuntime:    true,
		RemoveDefinition: true,
		RemoveConfig:     true,
		RemoveData:       true,
	})
	if !errors.Is(err, ErrDockerUnavailable) {
		t.Fatalf("DeleteStack() error = %v, want ErrDockerUnavailable", err)
	}

	assertExists(t, filepath.Join(reader.cfg.RootDir, "stacks", stackID, "compose.yaml"))
	assertExists(t, filepath.Join(reader.cfg.RootDir, "config", stackID))
	assertExists(t, filepath.Join(reader.cfg.RootDir, "data", stackID))
}

func TestRunDownRemovesComposeOrphans(t *testing.T) {
	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()
	logPath := filepath.Join(t.TempDir(), "docker.log")

	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:     stackID,
		ComposeYAML: "services:\n  app:\n    image: nginx:alpine\n",
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	shimDir := t.TempDir()
	dockerPath := filepath.Join(shimDir, "docker")
	script := `#!/bin/sh
set -eu

append_log() {
  printf '%s\n' "$1" >> "$DOCKER_LOG"
}

if [ "$1" = "ps" ]; then
  echo "container-demo"
  exit 0
fi

if [ "$1" = "inspect" ]; then
  cat <<JSON
[{"Id":"container-demo","Name":"/demo-app-1","Image":"sha256:demo","Config":{"Image":"nginx:alpine","Labels":{"com.docker.compose.project":"` + stackID + `","com.docker.compose.service":"app"}},"State":{"Status":"running","StartedAt":"2026-07-09T10:00:00Z"},"NetworkSettings":{"Ports":{},"Networks":{"demo_default":{}}}}]
JSON
  exit 0
fi

if [ "$1" = "compose" ]; then
  shift
  if [ "$1" = "version" ]; then
    echo "2.39.2"
    exit 0
  fi
  sub=""
  args=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --project-directory|-f|--env-file)
        shift 2
        ;;
      down)
        sub="$1"
        shift
        args="$*"
        break
        ;;
      *)
        shift
        ;;
    esac
  done
  append_log "compose $sub $args"
  echo "OK"
  exit 0
fi

echo "unsupported docker invocation: $*" >&2
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(docker shim) error = %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", logPath)
	ResetComposeCLICacheForTests()
	t.Cleanup(ResetComposeCLICacheForTests)

	if _, err := reader.RunActionWithOutput(ctx, stackID, "down"); err != nil {
		t.Fatalf("RunActionWithOutput(down) error = %v", err)
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(docker log) error = %v", err)
	}
	if !strings.Contains(string(logContent), "compose down --remove-orphans") {
		t.Fatalf("docker log = %q, want compose down --remove-orphans", string(logContent))
	}
}

func TestDetectComposeCLIDoesNotCacheCancelledFallback(t *testing.T) {
	ResetComposeCLICacheForTests()
	t.Cleanup(ResetComposeCLICacheForTests)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := detectComposeCLI(ctx)
	if got.command != "docker" || !reflect.DeepEqual(got.prefix, []string{"compose"}) {
		t.Fatalf("detectComposeCLI(cancelled) = %#v, want docker compose fallback", got)
	}
	if composeCLICached != nil {
		t.Fatalf("composeCLICached = %#v, want nil after cancelled detection", composeCLICached)
	}

	shimDir := t.TempDir()
	dockerPath := filepath.Join(shimDir, "docker")
	script := "#!/bin/sh\nif [ \"$1\" = \"compose\" ] && [ \"$2\" = \"version\" ]; then exit 0; fi\necho unsupported >&2\nexit 1\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(docker shim) error = %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got = detectComposeCLI(context.Background())
	if got.command != "docker" || !reflect.DeepEqual(got.prefix, []string{"compose"}) {
		t.Fatalf("detectComposeCLI() = %#v, want docker compose", got)
	}
	if composeCLICached == nil {
		t.Fatalf("composeCLICached = nil, want successful detection cached")
	}
}

func TestDeployBaselineDrivesConfigState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })
	reader.AttachStore(testStore)

	stackID := uniqueTestStackID()
	compose := "services:\n  app:\n    image: nginx:alpine\n"
	if err := reader.CreateStack(ctx, CreateStackRequest{StackID: stackID, ComposeYAML: compose}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	before, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get(before baseline) error = %v", err)
	}
	if before.Stack.ConfigState != ConfigStateUnknown {
		t.Fatalf("ConfigState before baseline = %q, want %q", before.Stack.ConfigState, ConfigStateUnknown)
	}

	deployedAt := time.Date(2026, 7, 6, 3, 17, 0, 0, time.UTC)
	if err := reader.RecordDeployBaseline(ctx, stackID, "job_123", deployedAt); err != nil {
		t.Fatalf("RecordDeployBaseline() error = %v", err)
	}
	inSync, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get(after baseline) error = %v", err)
	}
	if inSync.Stack.ConfigState != ConfigStateInSync {
		t.Fatalf("ConfigState after baseline = %q, want %q", inSync.Stack.ConfigState, ConfigStateInSync)
	}
	if inSync.Stack.LastDeployedAt == nil || !inSync.Stack.LastDeployedAt.Equal(deployedAt) {
		t.Fatalf("LastDeployedAt = %#v, want %v", inSync.Stack.LastDeployedAt, deployedAt)
	}

	if _, _, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{ComposeYAML: compose + "    restart: unless-stopped\n"}); err != nil {
		t.Fatalf("SaveDefinition() error = %v", err)
	}
	drifted, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get(after edit) error = %v", err)
	}
	if drifted.Stack.ConfigState != ConfigStateDrifted {
		t.Fatalf("ConfigState after edit = %q, want %q", drifted.Stack.ConfigState, ConfigStateDrifted)
	}

	invalidCompose := "services:\n  app:\n    image: [\n"
	invalidPreview, invalidDefinition, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML:       invalidCompose,
		ValidateAfterSave: true,
	})
	if err != nil {
		t.Fatalf("SaveDefinition(invalid) error = %v", err)
	}
	if invalidPreview.Valid {
		t.Fatal("SaveDefinition(invalid) preview valid = true, want false")
	}
	if invalidDefinition.Files.ComposeYAML.Content != invalidCompose || invalidDefinition.ConfigState != ConfigStateInvalid {
		t.Fatalf("unexpected returned invalid definition: %#v", invalidDefinition)
	}
	invalid, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get(after invalid edit) error = %v", err)
	}
	if invalid.Stack.ConfigState != ConfigStateInvalid {
		t.Fatalf("ConfigState after invalid edit = %q, want %q", invalid.Stack.ConfigState, ConfigStateInvalid)
	}
	if invalid.Stack.LastDeployedAt == nil || !invalid.Stack.LastDeployedAt.Equal(deployedAt) {
		t.Fatalf("LastDeployedAt after invalid edit = %#v, want %v", invalid.Stack.LastDeployedAt, deployedAt)
	}
}

func TestRemoveDefinitionRejectsSymlinkedStackRoot(t *testing.T) {
	t.Parallel()

	rootDir := filepath.Join(t.TempDir(), "root")
	stacksRoot := filepath.Join(rootDir, "stacks")
	if err := os.MkdirAll(stacksRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(stacks root) error = %v", err)
	}
	externalRoot := t.TempDir()
	composePath := filepath.Join(externalRoot, "compose.yaml")
	envPath := filepath.Join(externalRoot, ".env")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(compose target) error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte("SECRET=outside\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(env target) error = %v", err)
	}
	if err := os.Symlink(externalRoot, filepath.Join(stacksRoot, "demo")); err != nil {
		t.Fatalf("Symlink(stack root) error = %v", err)
	}

	reader := NewServiceReader(config.Config{RootDir: rootDir}, nil)
	err := reader.RemoveDefinition(context.Background(), "demo")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("RemoveDefinition(symlinked stack root) error = %v, want %v", err, ErrInvalidState)
	}
	for _, target := range []string{composePath, envPath} {
		if _, statErr := os.Stat(target); statErr != nil {
			t.Fatalf("external target %q was changed: %v", target, statErr)
		}
	}
}

func TestGetIncludesImageUpdateRollup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()
	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:     stackID,
		ComposeYAML: "services:\n  app:\n    image: example/app:latest\n",
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}
	checkedAt := time.Date(2026, 7, 9, 3, 0, 0, 0, time.UTC)
	reader.AttachUpdateStatus(func() map[string]ImageUpdateState {
		return map[string]ImageUpdateState{
			"example/app:latest": {State: "available", CheckedAt: checkedAt},
		}
	})

	detail, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if detail.Stack.Updates == nil {
		t.Fatal("Stack.Updates = nil, want rollup")
	}
	if detail.Stack.Updates.State != "available" || detail.Stack.Updates.ServicesWithUpdates != 1 || !detail.Stack.Updates.CheckedAt.Equal(checkedAt) {
		t.Fatalf("Stack.Updates = %#v, want available rollup", detail.Stack.Updates)
	}
}

func TestDeployBaselineUsesFreshDefinitionHashes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })
	reader.AttachStore(testStore)

	stackID := uniqueTestStackID()
	compose := "services:\n  app:\n    image: nginx:alpine\n"
	if err := reader.CreateStack(ctx, CreateStackRequest{StackID: stackID, ComposeYAML: compose}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	deployedAt := time.Date(2026, 7, 6, 3, 17, 0, 0, time.UTC)
	if err := reader.RecordDeployBaseline(ctx, stackID, "job_123", deployedAt); err != nil {
		t.Fatalf("RecordDeployBaseline() error = %v", err)
	}

	reader.afterScanDefinitionsForTest = func() {
		reader.afterScanDefinitionsForTest = nil
		paths := stackPaths(reader.cfg.RootDir, stackID)
		if err := os.WriteFile(paths.ComposeFilePath, []byte(compose+"    restart: unless-stopped\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(compose) error = %v", err)
		}
	}

	drifted, err := reader.Get(ctx, stackID)
	if err != nil {
		t.Fatalf("Get(after interleaved edit) error = %v", err)
	}
	if drifted.Stack.ConfigState != ConfigStateDrifted {
		t.Fatalf("ConfigState after interleaved edit = %q, want %q", drifted.Stack.ConfigState, ConfigStateDrifted)
	}
}

func TestInvalidateImageUpdateStatusMarksTargetedImagesUnknown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })
	reader.AttachStore(testStore)
	var cachedUpdates []store.ImageUpdateStatus
	reader.AttachUpdateStatusCacheUpdater(func(statuses []store.ImageUpdateStatus) {
		cachedUpdates = append(cachedUpdates, statuses...)
	})

	stackID := uniqueTestStackID()
	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID: stackID,
		ComposeYAML: `services:
  app:
    image: example/app:latest
  db:
    image: postgres:17
`,
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	checkedAt := time.Date(2026, 7, 6, 3, 17, 0, 0, time.UTC)
	for _, imageRef := range []string{"example/app:latest", "postgres:17"} {
		if err := testStore.UpsertImageUpdateStatus(ctx, store.ImageUpdateStatus{
			ImageRef:     imageRef,
			LocalDigest:  "sha256:old",
			RemoteDigest: "sha256:new",
			State:        "available",
			CheckedAt:    checkedAt,
		}); err != nil {
			t.Fatalf("UpsertImageUpdateStatus(%s) error = %v", imageRef, err)
		}
	}

	if err := reader.InvalidateImageUpdateStatus(ctx, stackID, []string{"app"}); err != nil {
		t.Fatalf("InvalidateImageUpdateStatus() error = %v", err)
	}
	statusByImage := imageUpdateStatusByRef(t, testStore)
	if statusByImage["example/app:latest"].State != "unknown" {
		t.Fatalf("app image state = %q, want unknown", statusByImage["example/app:latest"].State)
	}
	if statusByImage["example/app:latest"].LocalDigest != "" || statusByImage["example/app:latest"].RemoteDigest != "" {
		t.Fatalf("app image digests = %#v, want cleared", statusByImage["example/app:latest"])
	}
	if statusByImage["postgres:17"].State != "available" {
		t.Fatalf("db image state = %q, want available", statusByImage["postgres:17"].State)
	}
	if len(cachedUpdates) != 1 || cachedUpdates[0].ImageRef != "example/app:latest" || cachedUpdates[0].State != "unknown" {
		t.Fatalf("cache updates after targeted invalidation = %#v, want app unknown", cachedUpdates)
	}

	if err := reader.InvalidateImageUpdateStatus(ctx, stackID, nil); err != nil {
		t.Fatalf("InvalidateImageUpdateStatus(full) error = %v", err)
	}
	statusByImage = imageUpdateStatusByRef(t, testStore)
	if statusByImage["postgres:17"].State != "unknown" {
		t.Fatalf("db image state after full invalidation = %q, want unknown", statusByImage["postgres:17"].State)
	}
	if len(cachedUpdates) != 3 {
		t.Fatalf("cache updates after full invalidation = %#v, want 3 total updates", cachedUpdates)
	}
}

func TestSaveDefinitionEmptyEnvDoesNotCreateFileWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()

	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:           stackID,
		ComposeYAML:       "services:\n  app:\n    image: nginx:alpine\n",
		Env:               "",
		CreateConfigDir:   false,
		CreateDataDir:     false,
		DeployAfterCreate: false,
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	envPath := filepath.Join(reader.cfg.RootDir, "stacks", stackID, ".env")
	assertMissing(t, envPath)

	if _, _, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML:       "services:\n  app:\n    image: nginx:stable\n",
		Env:               "",
		ValidateAfterSave: false,
	}); err != nil {
		t.Fatalf("SaveDefinition() error = %v", err)
	}

	assertMissing(t, envPath)
}

func TestSaveDefinitionRollsBackComposeWhenEnvCommitFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()
	oldCompose := "services:\n  app:\n    image: nginx:alpine\n"
	oldEnv := "PORT=8080\n"
	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:     stackID,
		ComposeYAML: oldCompose,
		Env:         oldEnv,
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	paths := stackPaths(reader.cfg.RootDir, stackID)
	injectedErr := errors.New("injected env rename failure")
	reader.renameDefinitionFileForTest = func(oldPath, newPath string) error {
		if newPath == paths.EnvFilePath {
			return injectedErr
		}
		return os.Rename(oldPath, newPath)
	}

	_, _, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML: "services:\n  app:\n    image: nginx:stable\n",
		Env:         "PORT=9090\n",
	})
	if !errors.Is(err, injectedErr) {
		t.Fatalf("SaveDefinition() error = %v, want injected error", err)
	}
	assertFileContent(t, paths.ComposeFilePath, oldCompose)
	assertFileContent(t, paths.EnvFilePath, oldEnv)
	if matches, globErr := filepath.Glob(filepath.Join(paths.RootPath, ".stacklab-*-*")); globErr != nil || len(matches) != 0 {
		t.Fatalf("staged definition files after rollback = %v, glob error = %v", matches, globErr)
	}
}

func TestCreateStackRemovesPartialStateWhenDefinitionCommitFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()
	paths := stackPaths(reader.cfg.RootDir, stackID)
	injectedErr := errors.New("injected env rename failure")
	reader.renameDefinitionFileForTest = func(oldPath, newPath string) error {
		if newPath == paths.EnvFilePath {
			return injectedErr
		}
		return os.Rename(oldPath, newPath)
	}

	err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:         stackID,
		ComposeYAML:     "services:\n  app:\n    image: nginx:alpine\n",
		Env:             "PORT=8080\n",
		CreateConfigDir: true,
		CreateDataDir:   true,
	})
	if !errors.Is(err, injectedErr) {
		t.Fatalf("CreateStack() error = %v, want injected error", err)
	}
	assertMissing(t, paths.RootPath)
	assertMissing(t, paths.ConfigPath)
	assertMissing(t, paths.DataPath)
	if err := reader.EnsureCreateStackAvailable(ctx, stackID); err != nil {
		t.Fatalf("EnsureCreateStackAvailable() after rollback error = %v", err)
	}
}

func TestSaveDefinitionRejectsStaleRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	stackID := uniqueTestStackID()

	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:           stackID,
		ComposeYAML:       "services:\n  app:\n    image: nginx:alpine\n",
		Env:               "",
		CreateConfigDir:   false,
		CreateDataDir:     false,
		DeployAfterCreate: false,
	}); err != nil {
		t.Fatalf("CreateStack() error = %v", err)
	}

	definition, err := reader.Definition(ctx, stackID)
	if err != nil {
		t.Fatalf("Definition() error = %v", err)
	}
	revision := DefinitionRevision{
		ComposeModifiedAt: definition.Files.ComposeYAML.ModifiedAt,
		EnvModifiedAt:     definition.Files.Env.ModifiedAt,
	}

	composePath := filepath.Join(reader.cfg.RootDir, "stacks", stackID, "compose.yaml")
	if err := os.WriteFile(composePath, []byte("services:\n  app:\n    image: caddy:2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}
	newTime := revision.ComposeModifiedAt.Add(2 * time.Second)
	if err := os.Chtimes(composePath, newTime, newTime); err != nil {
		t.Fatalf("Chtimes(compose.yaml) error = %v", err)
	}

	_, _, err = reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML:      "services:\n  app:\n    image: nginx:stable\n",
		Env:              "",
		ExpectedRevision: &revision,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("SaveDefinition(stale revision) error = %v, want %v", err, ErrConflict)
	}
	composeBytes, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(compose.yaml) error = %v", err)
	}
	if !strings.Contains(string(composeBytes), "caddy:2") {
		t.Fatalf("stale save overwrote compose.yaml: %q", string(composeBytes))
	}
}

func TestRenderTemplateAppliesVariables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	templateDir := filepath.Join(reader.cfg.RootDir, "templates", "demo")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(templateDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "template.yaml"), []byte(`name: Demo app
icon: globe
variables:
  - name: IMAGE
    label: Image
    default: nginx:alpine
    required: true
  - name: HOST_PORT
    label: Port
    default: "8080"
    required: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(template.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "compose.yaml"), []byte(`services:
  app:
    image: ${IMAGE}
    ports:
      - "${HOST_PORT}:80"
    environment:
      TZ: ${TZ}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}

	rendered, err := reader.RenderTemplate(ctx, "demo", map[string]string{"IMAGE": "caddy:2"})
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}
	if !strings.Contains(rendered, "image: caddy:2") || !strings.Contains(rendered, `"8080:80"`) || !strings.Contains(rendered, "TZ: ${TZ}") {
		t.Fatalf("RenderTemplate() = %q", rendered)
	}

	if _, err := reader.RenderTemplate(ctx, "demo", map[string]string{"IMAGE": " "}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("RenderTemplate(required empty) error = %v, want ErrInvalidState", err)
	}
	if _, err := reader.RenderTemplate(ctx, "demo", map[string]string{" IMAGE ": ""}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("RenderTemplate(spaced key) error = %v, want ErrInvalidState", err)
	}
	if _, err := reader.RenderTemplate(ctx, "demo", map[string]string{"IMAGE": "caddy:2", "PUID": "1000"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("RenderTemplate(unknown key) error = %v, want ErrInvalidState", err)
	}
	if _, err := reader.RenderTemplate(ctx, "demo", map[string]string{"IMAGE": "caddy:2\ninjected: true"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("RenderTemplate(control char) error = %v, want ErrInvalidState", err)
	}
	if _, err := reader.RenderTemplate(ctx, "demo", map[string]string{"IMAGE": "caddy:2", "HOST_PORT": `8081"`}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("RenderTemplate(broken yaml) error = %v, want ErrInvalidState", err)
	}

	stackID := uniqueTestStackID()
	if err := reader.CreateStack(ctx, CreateStackRequest{
		StackID:           stackID,
		TemplateID:        "demo",
		Variables:         map[string]string{"IMAGE": "caddy:2", "HOST_PORT": "8081"},
		Env:               "",
		CreateConfigDir:   false,
		CreateDataDir:     false,
		DeployAfterCreate: false,
	}); err != nil {
		t.Fatalf("CreateStack(template) error = %v", err)
	}
	composeBytes, err := os.ReadFile(filepath.Join(reader.cfg.RootDir, "stacks", stackID, "compose.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(template compose) error = %v", err)
	}
	if !strings.Contains(string(composeBytes), "image: caddy:2") || !strings.Contains(string(composeBytes), `"8081:80"`) {
		t.Fatalf("template create compose = %q", string(composeBytes))
	}
}

func TestBuiltInTemplatesRenderWithDefaults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := newTestServiceReader(t)
	response, err := reader.Templates(ctx)
	if err != nil {
		t.Fatalf("Templates() error = %v", err)
	}
	if len(response.Items) != 6 {
		t.Fatalf("built-in template count = %d, want 6", len(response.Items))
	}

	seen := map[string]bool{}
	for _, template := range response.Items {
		seen[template.ID] = true
		if template.Name == "" || template.ComposeYAML == "" {
			t.Fatalf("template %q is missing name or compose yaml", template.ID)
		}
		rendered, err := reader.RenderTemplate(ctx, template.ID, nil)
		if err != nil {
			t.Fatalf("RenderTemplate(%q) error = %v", template.ID, err)
		}
		if strings.Contains(rendered, "${") {
			t.Fatalf("RenderTemplate(%q) left an unresolved variable: %q", template.ID, rendered)
		}
		if !strings.Contains(rendered, "x-stacklab:") {
			t.Fatalf("RenderTemplate(%q) missing x-stacklab metadata", template.ID)
		}
	}

	for _, id := range []string{"web-service", "static-site", "postgres-service", "app-with-db", "worker-with-redis", "volume-backed-service"} {
		if !seen[id] {
			t.Fatalf("missing built-in template %q", id)
		}
	}
}

func uniqueTestStackID() string {
	return fmt.Sprintf("test-%d", time.Now().UTC().UnixNano())
}

func newTestServiceReader(t *testing.T) *ServiceReader {
	t.Helper()

	rootDir := filepath.Join(t.TempDir(), "root")
	cfg := config.Config{
		RootDir: rootDir,
		DataDir: filepath.Join(t.TempDir(), "var"),
	}

	return NewServiceReader(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func assertMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be missing, got err = %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %q to exist, got err = %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content of %q = %q, want %q", path, string(content), want)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}

func imageUpdateStatusByRef(t *testing.T, testStore *store.Store) map[string]store.ImageUpdateStatus {
	t.Helper()

	items, err := testStore.ListImageUpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("ListImageUpdateStatus() error = %v", err)
	}
	result := map[string]store.ImageUpdateStatus{}
	for _, item := range items {
		result[item.ImageRef] = item
	}
	return result
}
