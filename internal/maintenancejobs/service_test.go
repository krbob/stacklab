package maintenancejobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/jobs"
	"stacklab/internal/maintenance"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type fakeMaintenanceStackReader struct {
	details             map[string]stacks.StackDetailResponse
	stepCalls           []maintenanceStepCall
	baselineCalls       int
	baselineErr         error
	invalidateCalls     int
	invalidatedImage    []invalidatedImageCall
	stepResults         map[string][]maintenanceStepResult
	unverifiedServices  []string
	rollbackCalls       []bool
	rollbackErr         error
	prepareErr          error
	prepareWarning      string
	released            int
	imageWarningJobs    []string
	recoveriesAtWarning []int
	registryAtWarning   []bool
	imageWarningChecks  []string
}

type maintenanceStepCall struct {
	StackID      string
	Action       string
	ServiceNames []string
}

type maintenanceStepResult struct {
	Output string
	Err    error
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
	return f.runStep(stackID, action, options.ServiceNames)
}

func (f *fakeMaintenanceStackReader) CaptureMaintenanceHealth(ctx context.Context, stackID string, serviceNames []string) (stacks.MaintenanceHealthBaseline, string, error) {
	var baseline stacks.MaintenanceHealthBaseline
	output, err := f.runStep(stackID, "verify_before", serviceNames)
	return baseline, output, err
}

func (f *fakeMaintenanceStackReader) VerifyMaintenanceHealth(ctx context.Context, stackID string, serviceNames []string, baseline stacks.MaintenanceHealthBaseline) (stacks.MaintenanceHealthReport, error) {
	output, err := f.runStep(stackID, "verify_after", serviceNames)
	return stacks.MaintenanceHealthReport{Summary: output, UnverifiedServices: f.unverifiedServices}, err
}

func (f *fakeMaintenanceStackReader) PrepareMaintenanceRollback(context.Context, string, stacks.MaintenanceHealthBaseline) (stacks.MaintenanceRollback, string, error) {
	return stacks.MaintenanceRollback{Warning: f.prepareWarning}, "Previous images", f.prepareErr
}

func (f *fakeMaintenanceStackReader) RecordMaintenanceImageFailure(_ context.Context, job store.Job, _ stacks.MaintenanceHealthBaseline, _ stacks.MaintenanceImageRecovery, pullPending bool) error {
	f.registryAtWarning = append(f.registryAtWarning, pullPending)
	f.imageWarningJobs = append(f.imageWarningJobs, job.ID)
	f.recoveriesAtWarning = append(f.recoveriesAtWarning, len(f.rollbackCalls))
	return nil
}
func (f *fakeMaintenanceStackReader) ClearMaintenanceImageWarnings(_ context.Context, stackID string, _ stacks.MaintenanceHealthReport) error {
	f.imageWarningChecks = append(f.imageWarningChecks, stackID)
	return nil
}
func (f *fakeMaintenanceStackReader) RollbackMaintenanceImages(_ context.Context, _ stacks.MaintenanceRollback, _ stacks.MaintenanceHealthReport, upFailed bool) (stacks.MaintenanceImageRecovery, error) {
	f.rollbackCalls = append(f.rollbackCalls, upFailed)
	return stacks.MaintenanceImageRecovery{Summary: "Image recovery result"}, f.rollbackErr
}

func (f *fakeMaintenanceStackReader) ReleaseMaintenanceRollback(context.Context, stacks.MaintenanceRollback) error {
	f.released++
	return nil
}

func (f *fakeMaintenanceStackReader) runStep(stackID, action string, serviceNames []string) (string, error) {
	f.stepCalls = append(f.stepCalls, maintenanceStepCall{
		StackID:      stackID,
		Action:       action,
		ServiceNames: append([]string(nil), serviceNames...),
	})
	key := stackID + "/" + action
	if results := f.stepResults[key]; len(results) > 0 {
		result := results[0]
		f.stepResults[key] = results[1:]
		return result.Output, result.Err
	}
	return "", nil
}

func (f *fakeMaintenanceStackReader) RecordDeployBaseline(ctx context.Context, stackID, jobID string, deployedAt time.Time) error {
	f.baselineCalls++
	return f.baselineErr
}

func (f *fakeMaintenanceStackReader) InvalidateImageUpdateStatus(ctx context.Context, stackID string, serviceNames []string) error {
	f.invalidateCalls++
	f.invalidatedImage = append(f.invalidatedImage, invalidatedImageCall{
		StackID:      stackID,
		ServiceNames: append([]string(nil), serviceNames...),
	})
	return nil
}

type fakeMaintenancePruneRunner struct {
	systemPruneCalls []systemPruneCall
	stepErr          error
	systemErr        error
}

type systemPruneCall struct {
	IncludeVolumes  bool
	ManagedStackIDs []string
}

func (f *fakeMaintenancePruneRunner) RunPruneStep(ctx context.Context, action string, managedStackIDs []string) (string, error) {
	return "", f.stepErr
}

func (f *fakeMaintenancePruneRunner) RunSystemPrune(ctx context.Context, includeVolumes bool, managedStackIDs []string) (string, error) {
	f.systemPruneCalls = append(f.systemPruneCalls, systemPruneCall{
		IncludeVolumes:  includeVolumes,
		ManagedStackIDs: append([]string(nil), managedStackIDs...),
	})
	return "", f.systemErr
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
	}, UpdateOptions{PullImages: true, BuildImages: true}, false)
	if err != nil {
		t.Fatalf("buildUpdateWorkflow() error = %v", err)
	}
	wantActions := []string{"verify_before", "pull", "build", "up", "verify_after"}
	if len(workflow) != len(wantActions) {
		t.Fatalf("workflow = %#v, want %v", workflow, wantActions)
	}
	for i, step := range workflow {
		if step.Action != wantActions[i] {
			t.Errorf("step %d action = %s, want %s", i, step.Action, wantActions[i])
		}
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
	}, UpdateOptions{PullImages: true, BuildImages: true}, false)
	if err != nil {
		t.Fatalf("buildUpdateWorkflow() error = %v", err)
	}
	want := []store.JobWorkflowStep{{Action: "skip", State: "queued", TargetStackID: "demo"}}
	if !reflect.DeepEqual(workflow, want) {
		t.Fatalf("buildUpdateWorkflow() = %#v, want %#v", workflow, want)
	}
}

func TestBuildUpdateWorkflowPreservesInactiveStackState(t *testing.T) {
	t.Parallel()

	for _, runtimeState := range []stacks.RuntimeState{stacks.RuntimeStateDefined, stacks.RuntimeStateStopped} {
		t.Run(string(runtimeState), func(t *testing.T) {
			t.Parallel()

			reader := fakeMaintenanceReader()
			detail := reader.details["demo"]
			detail.Stack.RuntimeState = runtimeState
			reader.details["demo"] = detail
			service := &Service{stackReader: reader}

			workflow, err := service.buildUpdateWorkflow(context.Background(), []string{"demo"}, nil, UpdateOptions{
				PullImages:  true,
				BuildImages: true,
			}, true)
			if err != nil {
				t.Fatalf("buildUpdateWorkflow() error = %v", err)
			}
			gotActions := make([]string, 0, len(workflow))
			for _, step := range workflow {
				gotActions = append(gotActions, step.Action)
			}
			wantActions := []string{"pull", "build", "preserve_inactive"}
			if !reflect.DeepEqual(gotActions, wantActions) {
				t.Fatalf("workflow actions = %#v, want %#v", gotActions, wantActions)
			}
		})
	}
}

func TestRunUpdatePreservesInactiveStacksForBulkPolicies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		target            UpdateTarget
		trigger           string
		preserveInactive  bool
		wantActions       []string
		wantStepCalls     int
		wantBaselineCalls int
	}{
		{
			name:              "manual all",
			target:            UpdateTarget{Mode: "all"},
			trigger:           "manual",
			wantActions:       []string{"preserve_inactive"},
			wantStepCalls:     0,
			wantBaselineCalls: 0,
		},
		{
			name:              "scheduled selected",
			target:            UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
			trigger:           "scheduled",
			wantActions:       []string{"preserve_inactive"},
			wantStepCalls:     0,
			wantBaselineCalls: 0,
		},
		{
			name:              "manual selected",
			target:            UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
			trigger:           "manual",
			wantActions:       []string{"verify_before", "up", "verify_after"},
			wantStepCalls:     3,
			wantBaselineCalls: 1,
		},
		{
			name:              "dashboard selected",
			target:            UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
			preserveInactive:  true,
			wantActions:       []string{"preserve_inactive"},
			wantStepCalls:     0,
			wantBaselineCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := fakeMaintenanceReader()
			detail := reader.details["demo"]
			detail.Stack.RuntimeState = stacks.RuntimeStateStopped
			reader.details["demo"] = detail
			service := newMaintenanceTestService(t, reader)

			job, err := service.RunUpdate(context.Background(), UpdateRequest{
				Target:  tt.target,
				Options: UpdateOptions{PreserveInactive: tt.preserveInactive},
				Trigger: tt.trigger,
			}, "test")
			if err != nil {
				t.Fatalf("RunUpdate() error = %v", err)
			}
			if job.Workflow == nil || len(job.Workflow.Steps) != len(tt.wantActions) {
				t.Fatalf("RunUpdate() workflow = %#v, want %v", job.Workflow, tt.wantActions)
			}
			for i, action := range tt.wantActions {
				if got := job.Workflow.Steps[i].Action; got != action {
					t.Fatalf("step %d action = %q, want %q", i, got, action)
				}
			}
			if len(reader.stepCalls) != tt.wantStepCalls {
				t.Fatalf("step calls = %#v, want %d", reader.stepCalls, tt.wantStepCalls)
			}
			if reader.baselineCalls != tt.wantBaselineCalls {
				t.Fatalf("baseline calls = %d, want %d", reader.baselineCalls, tt.wantBaselineCalls)
			}
		})
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
	if fullReader.invalidateCalls != 0 {
		t.Fatalf("full image invalidations = %#v, want none for up-only update", fullReader.invalidatedImage)
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
	if partialReader.invalidateCalls != 0 {
		t.Fatalf("partial image invalidations = %#v, want none for up-only update", partialReader.invalidatedImage)
	}
	wantCalls := []maintenanceStepCall{
		{StackID: "demo", Action: "verify_before", ServiceNames: []string{"app"}},
		{StackID: "demo", Action: "up", ServiceNames: []string{"app"}},
		{StackID: "demo", Action: "verify_after", ServiceNames: []string{"app"}},
	}
	if !reflect.DeepEqual(partialReader.stepCalls, wantCalls) {
		t.Fatalf("partial step calls = %#v, want %#v", partialReader.stepCalls, wantCalls)
	}
}

func TestRunUpdateTreatsDeployBaselineFailureAsWarning(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	reader.baselineErr = errors.New("baseline unavailable")
	service := newMaintenanceTestService(t, reader)

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: false, BuildImages: false},
	}, "test")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "succeeded" {
		t.Fatalf("RunUpdate() state = %q, want succeeded", job.State)
	}
	if reader.baselineCalls != 1 {
		t.Fatalf("baselineCalls = %d, want 1", reader.baselineCalls)
	}
}

func TestRunUpdateInvalidatesImageStatusAfterPullOrBuild(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	service := newMaintenanceTestService(t, reader)

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: true, BuildImages: true},
	}, "test")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "succeeded" {
		t.Fatalf("RunUpdate() state = %q, want succeeded", job.State)
	}
	if reader.invalidateCalls != 2 {
		t.Fatalf("invalidateCalls = %d, want 2 for pull+build", reader.invalidateCalls)
	}
	for _, call := range reader.invalidatedImage {
		if call.StackID != "demo" || len(call.ServiceNames) != 0 {
			t.Fatalf("unexpected image invalidation call: %#v", call)
		}
	}
}

func TestRunUpdatePruneAfterWithVolumesUsesAllManagedStacks(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	reader.details["stopped"] = stacks.StackDetailResponse{
		Stack: stacks.StackDetail{
			StackHeader:      stacks.StackHeader{ID: "stopped"},
			AvailableActions: []string{"up"},
			Services: []stacks.Service{
				{Name: "db", Mode: stacks.ServiceModeImage},
			},
		},
	}
	reader.details["invalid"] = stacks.StackDetailResponse{
		Stack: stacks.StackDetail{
			StackHeader:      stacks.StackHeader{ID: "invalid"},
			AvailableActions: []string{"validate"},
			Services: []stacks.Service{
				{Name: "db", Mode: stacks.ServiceModeImage},
			},
		},
	}
	pruner := &fakeMaintenancePruneRunner{}
	service := newMaintenanceTestServiceWithPruner(t, reader, pruner)

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target: UpdateTarget{
			Mode:     "selected",
			StackIDs: []string{"demo"},
		},
		Options: UpdateOptions{
			PullImages:     false,
			BuildImages:    false,
			PruneAfter:     true,
			IncludeVolumes: true,
		},
	}, "test")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "succeeded" {
		t.Fatalf("RunUpdate() state = %q, want succeeded", job.State)
	}
	if len(pruner.systemPruneCalls) != 1 {
		t.Fatalf("system prune calls = %#v, want one call", pruner.systemPruneCalls)
	}
	call := pruner.systemPruneCalls[0]
	if !call.IncludeVolumes {
		t.Fatalf("IncludeVolumes = false, want true")
	}
	wantIDs := []string{"demo", "invalid", "stopped"}
	if !reflect.DeepEqual(call.ManagedStackIDs, wantIDs) {
		t.Fatalf("ManagedStackIDs = %#v, want %#v", call.ManagedStackIDs, wantIDs)
	}
}

func TestRunUpdateRetriesTransientPullAndSucceedsWithWarnings(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	reader.stepResults["demo/pull"] = []maintenanceStepResult{
		{Output: `Get "https://lscr.io/v2/": net/http: TLS handshake timeout`, Err: errors.New("pull failed")},
		{Output: "toomanyrequests: retry-after: 1s", Err: errors.New("pull failed")},
		{},
	}
	service := newMaintenanceTestService(t, reader)
	var waits []time.Duration
	service.retryWait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: true, BuildImages: false},
		Trigger: "scheduled",
	}, "scheduler")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "succeeded" {
		t.Fatalf("RunUpdate() state = %q, want succeeded", job.State)
	}
	if !reflect.DeepEqual(waits, defaultUpdateStepRetryDelays) {
		t.Fatalf("retry waits = %v, want %v", waits, defaultUpdateStepRetryDelays)
	}
	pullCalls := 0
	for _, call := range reader.stepCalls {
		if call.StackID == "demo" && call.Action == "pull" {
			pullCalls++
		}
	}
	if pullCalls != 3 {
		t.Fatalf("pull calls = %d, want 3", pullCalls)
	}
	events, err := service.jobs.ReplayEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ReplayEvents() error = %v", err)
	}
	warnings := 0
	for _, event := range events {
		if event.Event == "job_warning" {
			warnings++
			if event.Step == nil || event.Step.State != "running" {
				t.Fatalf("retry warning step = %#v, want running", event.Step)
			}
		}
	}
	if warnings != 2 {
		t.Fatalf("job warnings = %d, want 2", warnings)
	}
}

func TestRunUpdatePersistsHealthObservationsAndSkipsPruneOnFailure(t *testing.T) {
	t.Parallel()

	const observations = "app unhealthy\nworker skipped: no healthcheck"
	for _, tt := range []struct {
		action       string
		result       maintenanceStepResult
		wantState    string
		unverified   []string
		wantWarnings int
		wantCalls    []string
	}{
		{
			action:    "verify_before",
			result:    maintenanceStepResult{Err: errors.New("Docker inspection unavailable")},
			wantState: "failed",
			wantCalls: []string{"verify_before"},
		},
		{
			action:     "verify_after",
			result:     maintenanceStepResult{Output: observations, Err: errors.New("app failed its health check")},
			unverified: []string{"worker"},
			wantState:  "failed",
			wantCalls:  []string{"verify_before", "pull", "up", "verify_after"},
		},
		{
			action:       "verify_after",
			result:       maintenanceStepResult{Output: "worker and scheduler running without healthchecks"},
			unverified:   []string{"scheduler", "worker"},
			wantWarnings: 1,
			wantState:    "succeeded",
			wantCalls:    []string{"verify_before", "pull", "up", "verify_after"},
		},
		{
			action:    "verify_after",
			result:    maintenanceStepResult{Output: "app healthy"},
			wantState: "succeeded",
			wantCalls: []string{"verify_before", "pull", "up", "verify_after"},
		},
	} {
		t.Run(tt.action+"/"+tt.wantState, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			reader := fakeMaintenanceReader()
			reader.unverifiedServices = tt.unverified
			reader.stepResults["demo/"+tt.action] = []maintenanceStepResult{tt.result}
			pruner := &fakeMaintenancePruneRunner{}
			service := newMaintenanceTestServiceWithPruner(t, reader, pruner)
			job, err := service.RunUpdate(ctx, UpdateRequest{
				Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
				Options: UpdateOptions{PullImages: true, PruneAfter: true},
			}, "test")
			if err != nil || job.State != tt.wantState {
				t.Fatalf("RunUpdate() state = %q, err = %v, want %s", job.State, err, tt.wantState)
			}
			var actions []string
			for _, call := range reader.stepCalls {
				actions = append(actions, call.Action)
			}
			if !reflect.DeepEqual(actions, tt.wantCalls) {
				t.Fatalf("executed %v, want %v", actions, tt.wantCalls)
			}
			if got := len(pruner.systemPruneCalls); (got > 0) != (tt.wantState == "succeeded") {
				t.Fatalf("prune ran %d times after %s", got, tt.wantState)
			}
			if tt.result.Err != nil && !strings.Contains(job.ErrorMessage, tt.result.Err.Error()) {
				t.Errorf("job error %q does not identify the failed check", job.ErrorMessage)
			}
			events, err := service.jobs.ReplayEvents(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			var warnings int
			var logged bool
			for _, event := range events {
				switch event.Event {
				case "job_log":
					if event.Step != nil && event.Step.Action == tt.action && event.Data == tt.result.Output {
						logged = true
					}
				case "job_warning":
					warnings++
					if event.Step == nil || event.Step.Action != "verify_after" || event.Step.TargetStackID != "demo" || event.Step.State != "running" {
						t.Errorf("unexpected warning step: %+v", event.Step)
					}
					for _, name := range tt.unverified {
						if !strings.Contains(event.Message, name) {
							t.Errorf("warning omits %q: %q", name, event.Message)
						}
					}
					if !strings.Contains(event.Message, "without a healthcheck") {
						t.Errorf("warning omits reason: %q", event.Message)
					}
				}
			}
			if tt.result.Output != "" && !logged {
				t.Errorf("missing health details for %s: %q", tt.action, tt.result.Output)
			}
			if warnings != tt.wantWarnings {
				t.Errorf("got %d warnings, want %d", warnings, tt.wantWarnings)
			}
		})
	}
}

func TestRunUpdateRetriesTransientBuildAndSucceedsWithWarnings(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	reader.stepResults["demo/build"] = []maintenanceStepResult{
		{Output: "failed to solve: context deadline exceeded", Err: errors.New("build failed")},
		{Err: fmt.Errorf("resolve source metadata: %w", context.DeadlineExceeded)},
		{},
	}
	service := newMaintenanceTestService(t, reader)
	var waits []time.Duration
	service.retryWait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: false, BuildImages: true},
		Trigger: "scheduled",
	}, "scheduler")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "succeeded" {
		t.Fatalf("RunUpdate() state = %q, want succeeded", job.State)
	}
	if !reflect.DeepEqual(waits, defaultUpdateStepRetryDelays) {
		t.Fatalf("retry waits = %v, want %v", waits, defaultUpdateStepRetryDelays)
	}
	buildCalls := 0
	upCalls := 0
	for _, call := range reader.stepCalls {
		switch call.Action {
		case "build":
			buildCalls++
		case "up":
			upCalls++
		}
	}
	if buildCalls != 3 || upCalls != 1 {
		t.Fatalf("step calls = %#v, want 3 build and 1 up", reader.stepCalls)
	}
	if reader.invalidateCalls != 1 {
		t.Fatalf("image invalidations = %d, want 1 after successful build", reader.invalidateCalls)
	}

	events, err := service.jobs.ReplayEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ReplayEvents() error = %v", err)
	}
	warnings := 0
	for _, event := range events {
		if event.Event != "job_warning" {
			continue
		}
		warnings++
		if !strings.HasPrefix(event.Message, "Build attempt ") {
			t.Fatalf("retry warning message = %q, want Build attempt", event.Message)
		}
		if event.Step == nil || event.Step.Action != "build" || event.Step.State != "running" {
			t.Fatalf("retry warning step = %#v, want running build", event.Step)
		}
	}
	if warnings != 2 {
		t.Fatalf("job warnings = %d, want 2", warnings)
	}
}

func TestRunUpdateExhaustsTransientBuildRetriesAndSkipsUp(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	reader.stepResults["demo/build"] = []maintenanceStepResult{
		{Err: context.DeadlineExceeded},
		{Err: context.DeadlineExceeded},
		{Err: context.DeadlineExceeded},
	}
	service := newMaintenanceTestService(t, reader)
	service.retryWait = func(context.Context, time.Duration) error { return nil }

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: false, BuildImages: true},
		Trigger: "scheduled",
	}, "scheduler")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "failed" {
		t.Fatalf("RunUpdate() state = %q, want failed", job.State)
	}
	if !strings.Contains(job.ErrorMessage, "demo/build after 3 attempt(s)") {
		t.Fatalf("RunUpdate() error message = %q", job.ErrorMessage)
	}
	if job.Workflow == nil || len(job.Workflow.Steps) != 4 || job.Workflow.Steps[1].State != "failed" || job.Workflow.Steps[2].State != "skipped" || job.Workflow.Steps[3].State != "skipped" {
		t.Fatalf("RunUpdate() workflow = %#v, want failed build and skipped up", job.Workflow)
	}
	buildCalls := 0
	for _, call := range reader.stepCalls {
		if call.Action == "build" {
			buildCalls++
		}
		if call.Action == "up" {
			t.Fatalf("unexpected up call after exhausted build retries: %#v", call)
		}
	}
	if buildCalls != 3 {
		t.Fatalf("build calls = %d, want 3", buildCalls)
	}
	if reader.invalidateCalls != 0 || reader.baselineCalls != 0 {
		t.Fatalf("effects after failed build: invalidations=%d baselines=%d, want 0/0", reader.invalidateCalls, reader.baselineCalls)
	}
}

func TestRunUpdateContinuesAfterExhaustedPullAndSkipsDependentSteps(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	zeta := reader.details["demo"]
	zeta.Stack.ID = "zeta"
	zeta.Stack.Name = "zeta"
	reader.details["zeta"] = zeta
	reader.stepResults["demo/pull"] = []maintenanceStepResult{
		{Output: "TLS handshake timeout", Err: errors.New("pull failed")},
		{Output: "connection reset by peer", Err: errors.New("pull failed")},
		{Output: "toomanyrequests", Err: errors.New("pull failed")},
	}
	pruner := &fakeMaintenancePruneRunner{}
	service := newMaintenanceTestServiceWithPruner(t, reader, pruner)
	service.retryWait = func(context.Context, time.Duration) error { return nil }
	var logs bytes.Buffer
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))

	job, err := service.RunUpdate(context.Background(), UpdateRequest{
		Target: UpdateTarget{Mode: "selected", StackIDs: []string{"demo", "zeta"}},
		Options: UpdateOptions{
			PullImages: true,
			PruneAfter: true,
		},
		Trigger:     "scheduled",
		ScheduleKey: "update",
	}, "scheduler")
	if err != nil {
		t.Fatalf("RunUpdate() error = %v", err)
	}
	if job.State != "failed" {
		t.Fatalf("RunUpdate() state = %q, want failed", job.State)
	}
	if !strings.Contains(job.ErrorMessage, "demo/pull after 3 attempt(s)") {
		t.Fatalf("RunUpdate() error message = %q", job.ErrorMessage)
	}
	wantStates := []string{"succeeded", "failed", "skipped", "skipped", "succeeded", "succeeded", "succeeded", "succeeded", "skipped"}
	if job.Workflow == nil || len(job.Workflow.Steps) != len(wantStates) {
		t.Fatalf("workflow = %#v, want %d steps", job.Workflow, len(wantStates))
	}
	for index, want := range wantStates {
		if got := job.Workflow.Steps[index].State; got != want {
			t.Fatalf("workflow step %d state = %q, want %q", index, got, want)
		}
	}
	for _, call := range reader.stepCalls {
		if call.StackID == "demo" && call.Action != "pull" && call.Action != "verify_before" {
			t.Fatalf("unexpected dependent demo call after pull failure: %#v", call)
		}
	}
	if reader.invalidateCalls != 1 || reader.baselineCalls != 1 {
		t.Fatalf("successful zeta effects: invalidations=%d baselines=%d, want 1/1", reader.invalidateCalls, reader.baselineCalls)
	}
	if len(pruner.systemPruneCalls) != 0 {
		t.Fatalf("prune calls = %#v, want none after update failure", pruner.systemPruneCalls)
	}
	events, err := service.jobs.ReplayEvents(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ReplayEvents() error = %v", err)
	}
	stepStates := map[string]string{}
	for _, event := range events {
		if event.Event == "job_step_finished" && event.Step != nil {
			stepStates[event.Step.TargetStackID+"/"+event.Step.Action] = event.Step.State
		}
	}
	if stepStates["demo/pull"] != "failed" || stepStates["demo/up"] != "skipped" || stepStates["zeta/up"] != "succeeded" || stepStates["/prune"] != "skipped" {
		t.Fatalf("finished step states = %#v", stepStates)
	}
	if output := logs.String(); !strings.Contains(output, "maintenance update completed with failures") || !strings.Contains(output, "job_id="+job.ID) || !strings.Contains(output, "failed_stacks=[demo]") {
		t.Fatalf("maintenance failure log = %q", output)
	}
	auditEntries, err := service.audit.List(context.Background(), audit.ListQuery{Limit: 10})
	if err != nil {
		t.Fatalf("audit.List() error = %v", err)
	}
	if len(auditEntries.Items) != 1 || auditEntries.Items[0].DetailJSON == nil || !strings.Contains(*auditEntries.Items[0].DetailJSON, `"failed_steps"`) || !strings.Contains(*auditEntries.Items[0].DetailJSON, `"attempts":3`) {
		t.Fatalf("maintenance failure audit = %#v", auditEntries.Items)
	}
}

func TestRetryableUpdateStepErrorClassification(t *testing.T) {
	t.Parallel()

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	tests := []struct {
		name   string
		ctx    context.Context
		output string
		err    error
		want   bool
	}{
		{name: "tls timeout", output: "net/http: TLS handshake timeout", err: errors.New("exit status 1"), want: true},
		{name: "context deadline output", output: "Get https://lscr.io/v2/: context deadline exceeded", err: errors.New("exit status 1"), want: true},
		{name: "context deadline error text", err: errors.New("context deadline exceeded"), want: true},
		{name: "context deadline sentinel", err: context.DeadlineExceeded, want: true},
		{name: "wrapped context deadline sentinel", err: fmt.Errorf("registry request: %w", context.DeadlineExceeded), want: true},
		{name: "rate limit", output: "toomanyrequests: retry-after: 829ms", err: errors.New("exit status 1"), want: true},
		{name: "connection reset", output: "read: connection reset by peer", err: errors.New("exit status 1"), want: true},
		{name: "dns", output: "temporary failure in name resolution", err: errors.New("exit status 1"), want: true},
		{name: "cancelled error", output: "TLS handshake timeout", err: context.Canceled, want: false},
		{name: "wrapped cancelled error", output: "TLS handshake timeout", err: fmt.Errorf("compose: %w", context.Canceled), want: false},
		{name: "cancelled parent", ctx: cancelledCtx, output: "TLS handshake timeout", err: errors.New("exit status 1"), want: false},
		{name: "expired parent", ctx: deadlineCtx, output: "context deadline exceeded", err: errors.New("exit status 1"), want: false},
		{name: "unauthorized", output: "unauthorized: authentication required", err: errors.New("exit status 1"), want: false},
		{name: "manifest missing", output: "manifest for example.test/app:missing not found", err: errors.New("exit status 1"), want: false},
		{name: "nil error", output: "TLS handshake timeout", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if got := isRetryableUpdateStepError(ctx, tt.output, tt.err); got != tt.want {
				t.Fatalf("isRetryableUpdateStepError(output=%q, err=%v) = %t, want %t", tt.output, tt.err, got, tt.want)
			}
		})
	}
}

func TestExecuteUpdateCancelsDuringRetryWait(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	reader.stepResults["demo/pull"] = []maintenanceStepResult{{Output: "TLS handshake timeout", Err: errors.New("pull failed")}}
	service := newMaintenanceTestService(t, reader)
	request := UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: true},
	}
	job, run, err := service.StartUpdate(context.Background(), request, "test")
	if err != nil {
		t.Fatalf("StartUpdate() error = %v", err)
	}
	service.retryWait = func(ctx context.Context, _ time.Duration) error {
		if _, cancelErr := service.jobs.Cancel(context.Background(), job.ID); cancelErr != nil {
			return cancelErr
		}
		return ctx.Err()
	}

	finishedJob, err := service.ExecuteUpdate(context.Background(), job, run)
	if err != nil {
		t.Fatalf("ExecuteUpdate() error = %v", err)
	}
	if finishedJob.State != "cancelled" {
		t.Fatalf("ExecuteUpdate() state = %q, want cancelled", finishedJob.State)
	}
	pullCalls := 0
	for _, call := range reader.stepCalls {
		if call.Action == "pull" {
			pullCalls++
		}
	}
	if pullCalls != 1 {
		t.Fatalf("pull calls = %d, want 1", pullCalls)
	}
}

func TestRunPruneLogsPersistedOperationalFailure(t *testing.T) {
	t.Parallel()

	pruner := &fakeMaintenancePruneRunner{stepErr: errors.New("prune registry unavailable")}
	service := newMaintenanceTestServiceWithPruner(t, fakeMaintenanceReader(), pruner)
	var logs bytes.Buffer
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))

	job, err := service.RunPrune(context.Background(), PruneRequest{
		Scope:       maintenance.PruneScope{Images: true},
		Trigger:     "scheduled",
		ScheduleKey: "prune",
	}, "scheduler", []string{"demo"})
	if err != nil {
		t.Fatalf("RunPrune() error = %v", err)
	}
	if job.State != "failed" {
		t.Fatalf("RunPrune() state = %q, want failed", job.State)
	}
	if output := logs.String(); !strings.Contains(output, "maintenance job failed") || !strings.Contains(output, "kind=prune") || !strings.Contains(output, "job_id="+job.ID) || !strings.Contains(output, "prune registry unavailable") {
		t.Fatalf("prune failure log = %q", output)
	}
}

func TestExecuteUpdateFinalizesAndUnlocksWhenWorkflowUpdateFails(t *testing.T) {
	t.Parallel()

	reader := fakeMaintenanceReader()
	service := newMaintenanceTestService(t, reader)
	request := UpdateRequest{
		Target:  UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
		Options: UpdateOptions{PullImages: false, BuildImages: false},
	}

	job, run, err := service.StartUpdate(context.Background(), request, "test")
	if err != nil {
		t.Fatalf("StartUpdate() error = %v", err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	finishedJob, err := service.ExecuteUpdate(cancelledCtx, job, run)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteUpdate() error = %v, want context.Canceled", err)
	}
	if finishedJob.State != "failed" {
		t.Fatalf("ExecuteUpdate() state = %q, want failed; err=%v; job=%#v", finishedJob.State, err, finishedJob)
	}

	if _, _, err := service.StartUpdate(context.Background(), request, "test"); err != nil {
		t.Fatalf("StartUpdate() after failed execution error = %v, want lock released", err)
	}
}

func TestExecutePruneFinalizesAndUnlocksWhenWorkflowUpdateFails(t *testing.T) {
	t.Parallel()

	service := newMaintenanceTestService(t, fakeMaintenanceReader())
	request := PruneRequest{
		Scope: maintenance.PruneScope{Images: true},
	}

	job, run, err := service.StartPrune(context.Background(), request, "test", []string{"demo"})
	if err != nil {
		t.Fatalf("StartPrune() error = %v", err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	finishedJob, err := service.ExecutePrune(cancelledCtx, job, run)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecutePrune() error = %v, want context.Canceled", err)
	}
	if finishedJob.State != "failed" {
		t.Fatalf("ExecutePrune() state = %q, want failed; err=%v; job=%#v", finishedJob.State, err, finishedJob)
	}

	if _, _, err := service.StartPrune(context.Background(), request, "test", []string{"demo"}); err != nil {
		t.Fatalf("StartPrune() after failed execution error = %v, want lock released", err)
	}
}

func newMaintenanceTestService(t *testing.T, reader *fakeMaintenanceStackReader) *Service {
	return newMaintenanceTestServiceWithPruner(t, reader, &fakeMaintenancePruneRunner{})
}

func newMaintenanceTestServiceWithPruner(t *testing.T, reader *fakeMaintenanceStackReader, pruner *fakeMaintenancePruneRunner) *Service {
	t.Helper()

	testStore, err := store.Open(filepath.Join(t.TempDir(), "stacklab.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = testStore.Close() })

	return NewService(nil, jobs.NewService(testStore), audit.NewService(testStore), reader, pruner)
}

func fakeMaintenanceReader() *fakeMaintenanceStackReader {
	buildContext := "/tmp/demo"
	return &fakeMaintenanceStackReader{
		stepResults: map[string][]maintenanceStepResult{},
		details: map[string]stacks.StackDetailResponse{
			"demo": {
				Stack: stacks.StackDetail{
					StackHeader:      stacks.StackHeader{ID: "demo", RuntimeState: stacks.RuntimeStateRunning},
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
