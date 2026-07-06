package maintenancejobs

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type fakeMaintenanceStackReader struct {
	details          map[string]stacks.StackDetailResponse
	stepCalls        []maintenanceStepCall
	baselineCalls    int
	invalidateCalls  int
	invalidatedImage []invalidatedImageCall
}

type maintenanceStepCall struct {
	StackID      string
	Action       string
	ServiceNames []string
}

type invalidatedImageCall struct {
	StackID      string
	ServiceNames []string
}

func (f *fakeMaintenanceStackReader) List(ctx context.Context, query stacks.ListQuery) (stacks.StackListResponse, error) {
	items := make([]stacks.StackListItem, 0, len(f.details))
	for stackID := range f.details {
		items = append(items, stacks.StackListItem{StackHeader: stacks.StackHeader{ID: stackID}})
	}
	return stacks.StackListResponse{Items: items}, nil
}

func (f *fakeMaintenanceStackReader) Get(ctx context.Context, stackID string) (stacks.StackDetailResponse, error) {
	detail, ok := f.details[stackID]
	if !ok {
		return stacks.StackDetailResponse{}, stacks.ErrNotFound
	}
	return detail, nil
}

func (f *fakeMaintenanceStackReader) MaintenanceNeedsBuild(ctx context.Context, stackID string) (bool, error) {
	detail, err := f.Get(ctx, stackID)
	if err != nil {
		return false, err
	}
	for _, service := range detail.Stack.Services {
		if service.Mode == stacks.ServiceModeBuild || service.Mode == stacks.ServiceModeHybrid || service.BuildContext != nil {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeMaintenanceStackReader) RunMaintenanceStep(ctx context.Context, stackID, action string, options stacks.MaintenanceStepOptions) (string, error) {
	return "", nil
}

func (f *fakeMaintenanceStackReader) RunMaintenanceStepStreaming(ctx context.Context, stackID, action string, options stacks.MaintenanceStepOptions, onProgress func(stacks.StepProgress)) (string, error) {
	f.stepCalls = append(f.stepCalls, maintenanceStepCall{
		StackID:      stackID,
		Action:       action,
		ServiceNames: append([]string(nil), options.ServiceNames...),
	})
	return "", nil
}

func (f *fakeMaintenanceStackReader) RecordDeployBaseline(ctx context.Context, stackID, jobID string, deployedAt time.Time) error {
	f.baselineCalls++
	return nil
}

func (f *fakeMaintenanceStackReader) InvalidateImageUpdateStatus(ctx context.Context, stackID string, serviceNames []string) error {
	f.invalidateCalls++
	f.invalidatedImage = append(f.invalidatedImage, invalidatedImageCall{
		StackID:      stackID,
		ServiceNames: append([]string(nil), serviceNames...),
	})
	return nil
}

type fakeMaintenancePruneRunner struct{}

func (f *fakeMaintenancePruneRunner) RunPruneStep(ctx context.Context, action string) (string, error) {
	return "", nil
}

func (f *fakeMaintenancePruneRunner) RunSystemPrune(ctx context.Context, includeVolumes bool) (string, error) {
	return "", nil
}

func TestResolveUpdateServiceTargetsExcludesKnownServices(t *testing.T) {
	t.Parallel()

	service := &Service{stackReader: fakeMaintenanceReader()}
	targets, err := service.resolveUpdateServiceTargets(context.Background(), []string{"demo"}, map[string][]string{
		"demo": {"db"},
	})
	if err != nil {
		t.Fatalf("resolveUpdateServiceTargets() error = %v", err)
	}
	want := map[string][]string{"demo": {"app"}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("resolveUpdateServiceTargets() = %#v, want %#v", targets, want)
	}
}

func TestResolveUpdateServiceTargetsRejectsUnknownService(t *testing.T) {
	t.Parallel()

	service := &Service{stackReader: fakeMaintenanceReader()}
	_, err := service.resolveUpdateServiceTargets(context.Background(), []string{"demo"}, map[string][]string{
		"demo": {"missing"},
	})
	if !errors.Is(err, stacks.ErrInvalidState) {
		t.Fatalf("resolveUpdateServiceTargets() error = %v, want ErrInvalidState", err)
	}
}

func TestBuildUpdateWorkflowUsesServiceTargets(t *testing.T) {
	t.Parallel()

	service := &Service{stackReader: fakeMaintenanceReader()}
	workflow, err := service.buildUpdateWorkflow(context.Background(), []string{"demo"}, map[string][]string{
		"demo": {"app"},
	}, UpdateOptions{PullImages: true, BuildImages: true})
	if err != nil {
		t.Fatalf("buildUpdateWorkflow() error = %v", err)
	}
	if len(workflow) != 3 {
		t.Fatalf("len(workflow) = %d, want 3: %#v", len(workflow), workflow)
	}
	for _, step := range workflow {
		if step.TargetStackID != "demo" {
			t.Fatalf("TargetStackID = %q, want demo", step.TargetStackID)
		}
		if !reflect.DeepEqual(step.TargetServiceNames, []string{"app"}) {
			t.Fatalf("TargetServiceNames = %#v, want [app]", step.TargetServiceNames)
		}
	}
}

func TestBuildUpdateWorkflowSkipsFullyExcludedStack(t *testing.T) {
	t.Parallel()

	service := &Service{stackReader: fakeMaintenanceReader()}
	workflow, err := service.buildUpdateWorkflow(context.Background(), []string{"demo"}, map[string][]string{
		"demo": {},
	}, UpdateOptions{PullImages: true, BuildImages: true})
	if err != nil {
		t.Fatalf("buildUpdateWorkflow() error = %v", err)
	}
	want := []store.JobWorkflowStep{{Action: "skip", State: "queued", TargetStackID: "demo"}}
	if !reflect.DeepEqual(workflow, want) {
		t.Fatalf("buildUpdateWorkflow() = %#v, want %#v", workflow, want)
	}
}

func TestRunUpdateRejectsRemoveOrphansWithServiceExclusions(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target: UpdateTarget{
			Mode:             "selected",
			StackIDs:         []string{"demo"},
			ExcludedServices: map[string][]string{"demo": {"db"}},
		},
		Options: UpdateOptions{RemoveOrphans: true},
	}, "test")
	if err == nil {
		t.Fatal("RunUpdate() error = nil, want validation error")
	}
}

func TestRunUpdateRecordsDeployBaselineOnlyForFullStackUp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fullReader := fakeMaintenanceReader()
	fullService := newMaintenanceTestService(t, fullReader)
	fullJob, err := fullService.RunUpdate(ctx, UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: false, BuildImages: false},
	}, "test")
	if err != nil {
		t.Fatalf("RunUpdate(full) error = %v", err)
	}
	if fullJob.State != "succeeded" {
		t.Fatalf("RunUpdate(full) state = %q, want succeeded", fullJob.State)
	}
	if fullReader.baselineCalls != 1 {
		t.Fatalf("full baselineCalls = %d, want 1", fullReader.baselineCalls)
	}
	if fullReader.invalidateCalls != 1 || len(fullReader.invalidatedImage[0].ServiceNames) != 0 {
		t.Fatalf("full image invalidations = %#v, want one full-stack invalidation", fullReader.invalidatedImage)
	}

	partialReader := fakeMaintenanceReader()
	partialService := newMaintenanceTestService(t, partialReader)
	partialJob, err := partialService.RunUpdate(ctx, UpdateRequest{
		Target: UpdateTarget{
			Mode:             "selected",
			StackIDs:         []string{"demo"},
			ExcludedServices: map[string][]string{"demo": {"db"}},
		},
		Options: UpdateOptions{PullImages: false, BuildImages: false},
	}, "test")
	if err != nil {
		t.Fatalf("RunUpdate(partial) error = %v", err)
	}
	if partialJob.State != "succeeded" {
		t.Fatalf("RunUpdate(partial) state = %q, want succeeded", partialJob.State)
	}
	if partialReader.baselineCalls != 0 {
		t.Fatalf("partial baselineCalls = %d, want 0", partialReader.baselineCalls)
	}
	if partialReader.invalidateCalls != 1 || !reflect.DeepEqual(partialReader.invalidatedImage[0].ServiceNames, []string{"app"}) {
		t.Fatalf("partial image invalidations = %#v, want one app-targeted invalidation", partialReader.invalidatedImage)
	}
	if len(partialReader.stepCalls) != 1 || !reflect.DeepEqual(partialReader.stepCalls[0].ServiceNames, []string{"app"}) {
		t.Fatalf("partial step calls = %#v, want one app-targeted up", partialReader.stepCalls)
	}
}

func newMaintenanceTestService(t *testing.T, reader *fakeMaintenanceStackReader) *Service {
	t.Helper()

	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })

	return NewService(nil, jobs.NewService(testStore), audit.NewService(testStore), reader, &fakeMaintenancePruneRunner{})
}

func fakeMaintenanceReader() *fakeMaintenanceStackReader {
	buildContext := "/tmp/demo"
	return &fakeMaintenanceStackReader{
		details: map[string]stacks.StackDetailResponse{
			"demo": {
				Stack: stacks.StackDetail{
					StackHeader:      stacks.StackHeader{ID: "demo"},
					AvailableActions: []string{"up"},
					Services: []stacks.Service{
						{Name: "app", Mode: stacks.ServiceModeBuild, BuildContext: &buildContext},
						{Name: "db", Mode: stacks.ServiceModeImage},
					},
				},
			},
		},
	}
}
