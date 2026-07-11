package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"stacklab/internal/store"
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

func TestStackResourcesAndAccessors(t *testing.T) {
	t.Parallel()

	resources := StackResources([]string{" alpha ", "", "beta"})
	if len(resources) != 2 || resources[0] != StackResource("alpha") || resources[1] != StackResource("beta") {
		t.Fatalf("StackResources() = %#v", resources)
	}
	if resources[0].Kind() != ResourceKindStack || resources[0].StackID() != "alpha" {
		t.Fatalf("stack resource accessors = kind %d stack %q", resources[0].Kind(), resources[0].StackID())
	}
	if GlobalResource().StackID() != "" {
		t.Fatalf("GlobalResource().StackID() = %q, want empty", GlobalResource().StackID())
	}
	if got := (Resource{}).String(); got != "invalid" {
		t.Fatalf("zero Resource.String() = %q, want invalid", got)
	}
}

func TestResourceValidationAndConflictErrors(t *testing.T) {
	t.Parallel()

	tests := []Resource{
		{kind: ResourceKindGlobal, id: "unexpected"},
		{kind: ResourceKindStack},
		{kind: ResourceKind(255)},
	}
	for _, resource := range tests {
		if err := resource.validate(); !errors.Is(err, ErrInvalidResource) {
			t.Errorf("Resource(%#v).validate() error = %v, want ErrInvalidResource", resource, err)
		}
	}

	var nilConflict *ResourceConflictError
	if got := nilConflict.Error(); got != ErrResourceConflict.Error() {
		t.Fatalf("nil ResourceConflictError.Error() = %q", got)
	}
	conflict := &ResourceConflictError{
		Reason: ConflictReasonResourceHeld, Requested: StackResource("alpha"),
		Conflicting: DockerRegistryResource(), ConflictingJobID: "job-holder",
	}
	if message := conflict.Error(); !strings.Contains(message, "resource_held") || !strings.Contains(message, "job-holder") {
		t.Fatalf("ResourceConflictError.Error() = %q", message)
	}
	if !errors.Is(conflict, ErrResourceConflict) {
		t.Fatalf("ResourceConflictError does not unwrap to ErrResourceConflict")
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

func TestStartDrainingRejectsActiveStackAndGlobalMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start func(*Service) (store.Job, error)
	}{
		{
			name: "stack",
			start: func(service *Service) (store.Job, error) {
				return service.Start(context.Background(), "demo", "up", "local")
			},
		},
		{
			name: "global",
			start: func(service *Service) (store.Job, error) {
				return service.StartWithResources(context.Background(), "", "prune", "local", GlobalResource())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(openJobsTestStore(t))
			holder, err := test.start(service)
			if err != nil {
				t.Fatalf("start holder error = %v", err)
			}
			_, err = service.StartDraining(context.Background(), "self_update_stacklab", "local", SelfUpdateResource())
			var conflict *ResourceConflictError
			if !errors.As(err, &conflict) || conflict.Reason != ConflictReasonDrainBlocked || conflict.ConflictingJobID != holder.ID {
				t.Fatalf("StartDraining() error = %#v, want drain_blocked for job %s", err, holder.ID)
			}
			if _, err := service.FinishSucceeded(context.Background(), holder); err != nil {
				t.Fatalf("FinishSucceeded(holder) error = %v", err)
			}
		})
	}
}

func TestDrainBlocksEveryNewMutationUntilFinalized(t *testing.T) {
	t.Parallel()

	service := NewService(openJobsTestStore(t))
	drain, err := service.StartDraining(context.Background(), "self_update_stacklab", "local", SelfUpdateResource())
	if err != nil {
		t.Fatalf("StartDraining() error = %v", err)
	}
	attempts := []struct {
		name  string
		start func() error
	}{
		{name: "stack", start: func() error { _, err := service.Start(context.Background(), "demo", "up", "local"); return err }},
		{name: "global", start: func() error {
			_, err := service.StartWithResources(context.Background(), "", "prune", "local", GlobalResource())
			return err
		}},
		{name: "daemon", start: func() error {
			_, err := service.StartWithResources(context.Background(), "", "daemon", "local", DockerDaemonResource())
			return err
		}},
		{name: "registry", start: func() error {
			_, err := service.StartWithResources(context.Background(), "", "registry", "local", DockerRegistryResource())
			return err
		}},
		{name: "image updates", start: func() error {
			_, err := service.StartWithResources(context.Background(), "", "images", "local", ImageUpdatesResource())
			return err
		}},
	}
	for _, attempt := range attempts {
		err := attempt.start()
		var conflict *ResourceConflictError
		if !errors.As(err, &conflict) || conflict.Reason != ConflictReasonDrainActive || conflict.ConflictingJobID != drain.ID {
			t.Errorf("%s start error = %#v, want active drain owned by %s", attempt.name, err, drain.ID)
		}
	}

	if _, err := service.FinishFailed(context.Background(), drain, "test_finished", "test finished"); err != nil {
		t.Fatalf("FinishFailed(drain) error = %v", err)
	}
	if _, err := service.Start(context.Background(), "demo", "up", "local"); err != nil {
		t.Fatalf("Start(stack after drain finalization) error = %v", err)
	}
}

func TestStartDrainingRaceWithStackAndGlobalMutationIsAtomic(t *testing.T) {
	tests := []struct {
		name     string
		mutation func(*Service, int) (store.Job, error)
	}{
		{
			name: "stack",
			mutation: func(service *Service, iteration int) (store.Job, error) {
				return service.Start(context.Background(), fmt.Sprintf("stack-%d", iteration), "up", "local")
			},
		},
		{
			name: "global",
			mutation: func(service *Service, iteration int) (store.Job, error) {
				return service.StartWithResources(context.Background(), "", fmt.Sprintf("prune-%d", iteration), "local", GlobalResource())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(openJobsTestStore(t))
			for iteration := 0; iteration < 50; iteration++ {
				gate := make(chan struct{})
				results := make(chan startResult, 2)
				go func() {
					<-gate
					job, err := service.StartDraining(context.Background(), "self_update_stacklab", "local", SelfUpdateResource())
					results <- startResult{kind: "drain", job: job, err: err}
				}()
				go func() {
					<-gate
					job, err := test.mutation(service, iteration)
					results <- startResult{kind: "mutation", job: job, err: err}
				}()
				close(gate)

				first := <-results
				second := <-results
				successes := 0
				var winner startResult
				var loser startResult
				for _, result := range []startResult{first, second} {
					if result.err == nil {
						successes++
						winner = result
					} else {
						loser = result
					}
				}
				if successes != 1 || !errors.Is(loser.err, ErrResourceConflict) {
					t.Fatalf("iteration %d results = %#v, %#v; want one success and one conflict", iteration, first, second)
				}
				var conflict *ResourceConflictError
				if !errors.As(loser.err, &conflict) {
					t.Fatalf("iteration %d loser error = %v, want typed conflict", iteration, loser.err)
				}
				if winner.kind == "drain" && conflict.Reason != ConflictReasonDrainActive {
					t.Fatalf("iteration %d mutation lost with reason %q, want drain_active", iteration, conflict.Reason)
				}
				if winner.kind == "mutation" && conflict.Reason != ConflictReasonDrainBlocked {
					t.Fatalf("iteration %d drain lost with reason %q, want drain_blocked", iteration, conflict.Reason)
				}
				if _, err := service.FinishSucceeded(context.Background(), winner.job); err != nil {
					t.Fatalf("iteration %d FinishSucceeded(winner) error = %v", iteration, err)
				}
			}
		})
	}
}

type startResult struct {
	kind string
	job  store.Job
	err  error
}
