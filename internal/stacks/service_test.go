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

	services, err := parseComposeServices(stackRoot, content)
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

	savePreview, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
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
	composeBytes, err := os.ReadFile(filepath.Join(reader.cfg.RootDir, "stacks", stackID, "compose.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(compose.yaml) error = %v", err)
	}
	if !strings.Contains(string(composeBytes), "nginx:stable") {
		t.Fatalf("compose.yaml does not contain updated image: %q", string(composeBytes))
	}
	envBytes, err := os.ReadFile(filepath.Join(reader.cfg.RootDir, "stacks", stackID, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if string(envBytes) != "PORT=9090\n" {
		t.Fatalf(".env content = %q, want %q", string(envBytes), "PORT=9090\n")
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

	if _, err := reader.SaveDefinition(ctx, stackID, UpdateDefinitionRequest{
		ComposeYAML:       "services:\n  app:\n    image: nginx:stable\n",
		Env:               "",
		ValidateAfterSave: false,
	}); err != nil {
		t.Fatalf("SaveDefinition() error = %v", err)
	}

	assertMissing(t, envPath)
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
