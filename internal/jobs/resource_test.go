package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestResourceString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		resource Resource
		want     string
	}{
		{resource: GlobalResource(), want: "global"},
		{resource: DockerDaemonResource(), want: "docker-daemon"},
		{resource: DockerRegistryResource(), want: "docker-registry"},
		{resource: SelfUpdateResource(), want: "self-update"},
		{resource: ImageUpdatesResource(), want: "image-updates"},
		{resource: StackResource("demo"), want: "stack:demo"},
	}
	for _, test := range tests {
		if got := test.resource.String(); got != test.want {
			t.Errorf("Resource.String() = %q, want %q", got, test.want)
		}
	}
}

func TestResourcesConflictMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  Resource
		right Resource
		want  bool
	}{
		{name: "same stack", left: StackResource("demo"), right: StackResource("demo"), want: true},
		{name: "different stacks", left: StackResource("demo"), right: StackResource("other"), want: false},
		{name: "global and stack", left: GlobalResource(), right: StackResource("demo"), want: true},
		{name: "global and image updates", left: GlobalResource(), right: ImageUpdatesResource(), want: true},
		{name: "daemon and stack", left: DockerDaemonResource(), right: StackResource("demo"), want: true},
		{name: "daemon and registry", left: DockerDaemonResource(), right: DockerRegistryResource(), want: true},
		{name: "registry and stack", left: DockerRegistryResource(), right: StackResource("demo"), want: true},
		{name: "image updates and stack", left: ImageUpdatesResource(), right: StackResource("demo"), want: false},
		{name: "self update and stack before drain", left: SelfUpdateResource(), right: StackResource("demo"), want: false},
		{name: "same self update", left: SelfUpdateResource(), right: SelfUpdateResource(), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ResourcesConflict(test.left, test.right); got != test.want {
				t.Errorf("ResourcesConflict(%s, %s) = %t, want %t", test.left, test.right, got, test.want)
			}
			if got := ResourcesConflict(test.right, test.left); got != test.want {
				t.Errorf("ResourcesConflict(%s, %s) = %t, want %t", test.right, test.left, got, test.want)
			}
		})
	}
}

func TestUnscopedJobRequiresExplicitResource(t *testing.T) {
	t.Parallel()

	service := NewService(openJobsTestStore(t))
	if _, err := service.Start(context.Background(), "", "unscoped", "local"); !errors.Is(err, ErrResourcesRequired) {
		t.Fatalf("Start(unscoped) error = %v, want ErrResourcesRequired", err)
	}
	if _, err := service.StartWithResources(context.Background(), "", "unscoped", "local"); !errors.Is(err, ErrResourcesRequired) {
		t.Fatalf("StartWithResources(unscoped) error = %v, want ErrResourcesRequired", err)
	}
	if _, err := service.StartWithResources(context.Background(), "", "unscoped", "local", Resource{}); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("StartWithResources(invalid) error = %v, want ErrInvalidResource", err)
	}
}

func TestStartWithResourcesReturnsTypedConflict(t *testing.T) {
	t.Parallel()

	service := NewService(openJobsTestStore(t))
	holder, err := service.Start(context.Background(), "demo", "up", "local")
	if err != nil {
		t.Fatalf("Start(holder) error = %v", err)
	}

	_, err = service.StartWithResources(context.Background(), "", "docker_login", "local", DockerRegistryResource())
	if !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("StartWithResources(conflict) error = %v, want ErrResourceConflict", err)
	}
	var conflict *ResourceConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("StartWithResources(conflict) error type = %T, want *ResourceConflictError", err)
	}
	if conflict.Reason != ConflictReasonResourceHeld || conflict.Requested != DockerRegistryResource() || conflict.Conflicting != StackResource("demo") || conflict.ConflictingJobID != holder.ID {
		t.Fatalf("unexpected conflict details: %#v", conflict)
	}

	if _, err := service.FinishSucceeded(context.Background(), holder); err != nil {
		t.Fatalf("FinishSucceeded(holder) error = %v", err)
	}
	if _, err := service.StartWithResources(context.Background(), "", "docker_login", "local", DockerRegistryResource()); err != nil {
		t.Fatalf("StartWithResources(after release) error = %v", err)
	}
}
