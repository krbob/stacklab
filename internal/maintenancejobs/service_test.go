package maintenancejobs

import (
	"bytes"
	"context"
	"errors"
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
	details          map[string]stacks.StackDetailResponse
	stepCalls        []maintenanceStepCall
	baselineCalls    int
	baselineErr      error
	invalidateCalls  int
	invalidatedImage []invalidatedImageCall
	stepResults      map[string][]maintenanceStepResult
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
	f.stepCalls = append(f.stepCalls, maintenanceStepCall{
		StackID:      stackID,
		Action:       action,
		ServiceNames: append([]string(nil), options.ServiceNames...),
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
		wantAction        string
		wantStepCalls     int
		wantBaselineCalls int
	}{
		{
			name:              "manual all",
			target:            UpdateTarget{Mode: "all"},
			trigger:           "manual",
			wantAction:        "preserve_inactive",
			wantStepCalls:     0,
			wantBaselineCalls: 0,
		},
		{
			name:              "scheduled selected",
			target:            UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
			trigger:           "scheduled",
			wantAction:        "preserve_inactive",
			wantStepCalls:     0,
			wantBaselineCalls: 0,
		},
		{
			name:              "manual selected",
			target:            UpdateTarget{Mode: "selected", StackIDs: []string{"demo"}},
			trigger:           "manual",
			wantAction:        "up",
			wantStepCalls:     1,
			wantBaselineCalls: 1,
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
				Options: UpdateOptions{},
				Trigger: tt.trigger,
			}, "test")
			if err != nil {
				t.Fatalf("RunUpdate() error = %v", err)
			}
			if job.Workflow == nil || len(job.Workflow.Steps) != 1 || job.Workflow.Steps[0].Action != tt.wantAction {
				t.Fatalf("RunUpdate() workflow = %#v, want one %q step", job.Workflow, tt.wantAction)
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
	if len(partialReader.stepCalls) != 1 || !reflect.DeepEqual(partialReader.stepCalls[0].ServiceNames, []string{"app"}) {
		t.Fatalf("partial step calls = %#v, want one app-targeted up", partialReader.stepCalls)
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
	if !reflect.DeepEqual(waits, defaultPullRetryDelays) {
		t.Fatalf("retry waits = %v, want %v", waits, defaultPullRetryDelays)
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
	wantStates := []string{"failed", "skipped", "succeeded", "succeeded", "skipped"}
	if job.Workflow == nil || len(job.Workflow.Steps) != len(wantStates) {
		t.Fatalf("workflow = %#v, want %d steps", job.Workflow, len(wantStates))
	}
	for index, want := range wantStates {
		if got := job.Workflow.Steps[index].State; got != want {
			t.Fatalf("workflow step %d state = %q, want %q", index, got, want)
		}
	}
	for _, call := range reader.stepCalls {
		if call.StackID == "demo" && call.Action != "pull" {
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

func TestRetryablePullErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "tls timeout", message: "net/http: TLS handshake timeout", want: true},
		{name: "rate limit", message: "toomanyrequests: retry-after: 829ms", want: true},
		{name: "connection reset", message: "read: connection reset by peer", want: true},
		{name: "dns", message: "temporary failure in name resolution", want: true},
		{name: "unauthorized", message: "unauthorized: authentication required", want: false},
		{name: "manifest missing", message: "manifest for example.test/app:missing not found", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isRetryablePullError(context.Background(), tt.message, errors.New(tt.message)); got != tt.want {
				t.Fatalf("isRetryablePullError(%q) = %t, want %t", tt.message, got, tt.want)
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
