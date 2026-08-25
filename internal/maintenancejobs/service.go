package maintenancejobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/jobs"
	"stacklab/internal/maintenance"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

const (
	maintenanceErrorSummaryLimit = 1024
	maintenanceErrorMessageLimit = 4096
)

var defaultUpdateStepRetryDelays = []time.Duration{5 * time.Second, 20 * time.Second}

type retryWaitFunc func(context.Context, time.Duration) error

type updateStepFailure struct {
	StackID  string
	Action   string
	Attempts int
	Message  string
}

type updateStepResult struct {
	Attempts int
	Output   string
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type stackReader interface {
	List(ctx context.Context, query stacks.ListQuery) (stacks.StackListResponse, error)
	Get(ctx context.Context, stackID string) (stacks.StackDetailResponse, error)
	MaintenanceNeedsBuild(ctx context.Context, stackID string) (bool, error)
	RunMaintenanceStep(ctx context.Context, stackID, action string, options stacks.MaintenanceStepOptions) (string, error)
	RunMaintenanceStepStreaming(ctx context.Context, stackID, action string, options stacks.MaintenanceStepOptions, onProgress func(stacks.StepProgress)) (string, error)
	RecordDeployBaseline(ctx context.Context, stackID, jobID string, deployedAt time.Time) error
	InvalidateImageUpdateStatus(ctx context.Context, stackID string, serviceNames []string) error
}

type pruneRunner interface {
	RunPruneStep(ctx context.Context, action string, managedStackIDs []string) (string, error)
	RunSystemPrune(ctx context.Context, includeVolumes bool, managedStackIDs []string) (string, error)
}

type Service struct {
	logger                *slog.Logger
	jobs                  *jobs.Service
	audit                 *audit.Service
	stackReader           stackReader
	maintenance           pruneRunner
	updateStepRetryDelays []time.Duration
	retryWait             retryWaitFunc
}

func NewService(logger *slog.Logger, jobService *jobs.Service, auditService *audit.Service, stackService stackReader, maintenanceService pruneRunner) *Service {
	return &Service{
		logger:                logger,
		jobs:                  jobService,
		audit:                 auditService,
		stackReader:           stackService,
		maintenance:           maintenanceService,
		updateStepRetryDelays: append([]time.Duration(nil), defaultUpdateStepRetryDelays...),
		retryWait:             waitForRetry,
	}
}

func (s *Service) ResolveTargetStacks(ctx context.Context, mode string, stackIDs []string) ([]string, error) {
	switch mode {
	case "selected":
		if len(stackIDs) == 0 {
			return nil, errors.New("target.stack_ids must be non-empty when mode = selected")
		}
		deduped := dedupeSortedStackIDs(stackIDs)
		for _, stackID := range deduped {
			detail, err := s.stackReader.Get(ctx, stackID)
			if err != nil {
				if errors.Is(err, stacks.ErrNotFound) {
					return nil, fmt.Errorf("%w: stack %q was not found", stacks.ErrNotFound, stackID)
				}
				return nil, err
			}
			if !containsString(detail.Stack.AvailableActions, "up") {
				return nil, fmt.Errorf("%w: stack %q cannot be updated in its current state", stacks.ErrInvalidState, stackID)
			}
		}
		return deduped, nil
	case "all":
		list, err := s.stackReader.List(ctx, stacks.ListQuery{})
		if err != nil {
			return nil, err
		}
		candidates := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			detail, err := s.stackReader.Get(ctx, item.ID)
			if err != nil {
				return nil, err
			}
			if containsString(detail.Stack.AvailableActions, "up") {
				candidates = append(candidates, item.ID)
			}
		}
		if len(candidates) == 0 {
			return nil, errors.New("no updatable stacks found")
		}
		sort.Strings(candidates)
		return candidates, nil
	default:
		return nil, errors.New("target.mode must be one of: selected, all")
	}
}

func (s *Service) listManagedStackIDs(ctx context.Context) ([]string, error) {
	list, err := s.stackReader.List(ctx, stacks.ListQuery{})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, item.ID)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Service) resolveUpdateServiceTargets(ctx context.Context, stackIDs []string, excluded map[string][]string) (map[string][]string, error) {
	result := map[string][]string{}
	if !hasServiceExclusions(excluded) {
		return result, nil
	}

	targeted := make(map[string]struct{}, len(stackIDs))
	for _, stackID := range stackIDs {
		targeted[stackID] = struct{}{}
	}
	for stackID, serviceNames := range excluded {
		if len(serviceNames) == 0 {
			continue
		}
		if _, ok := targeted[stackID]; !ok {
			return nil, fmt.Errorf("%w: excluded services include non-target stack %q", stacks.ErrInvalidState, stackID)
		}
	}

	for _, stackID := range stackIDs {
		excludedForStack := dedupeSortedStrings(excluded[stackID])
		if len(excludedForStack) == 0 {
			continue
		}
		detail, err := s.stackReader.Get(ctx, stackID)
		if err != nil {
			return nil, err
		}
		allServices := make([]string, 0, len(detail.Stack.Services))
		serviceSet := make(map[string]struct{}, len(detail.Stack.Services))
		for _, service := range detail.Stack.Services {
			allServices = append(allServices, service.Name)
			serviceSet[service.Name] = struct{}{}
		}
		for _, serviceName := range excludedForStack {
			if _, ok := serviceSet[serviceName]; !ok {
				return nil, fmt.Errorf("%w: service %q does not exist in stack %q", stacks.ErrInvalidState, serviceName, stackID)
			}
		}
		excludedSet := make(map[string]struct{}, len(excludedForStack))
		for _, serviceName := range excludedForStack {
			excludedSet[serviceName] = struct{}{}
		}
		included := make([]string, 0, len(allServices))
		for _, serviceName := range allServices {
			if _, excluded := excludedSet[serviceName]; !excluded {
				included = append(included, serviceName)
			}
		}
		sort.Strings(included)
		result[stackID] = included
	}
	return result, nil
}

func hasServiceExclusions(excluded map[string][]string) bool {
	for _, serviceNames := range excluded {
		if len(serviceNames) > 0 {
			return true
		}
	}
	return false
}

func (s *Service) maintenanceNeedsBuild(ctx context.Context, stackID string, serviceNames []string) (bool, error) {
	if serviceNames == nil {
		return s.stackReader.MaintenanceNeedsBuild(ctx, stackID)
	}
	detail, err := s.stackReader.Get(ctx, stackID)
	if err != nil {
		return false, err
	}
	targeted := make(map[string]struct{}, len(serviceNames))
	for _, serviceName := range serviceNames {
		targeted[serviceName] = struct{}{}
	}
	for _, service := range detail.Stack.Services {
		if _, ok := targeted[service.Name]; !ok {
			continue
		}
		if service.Mode == stacks.ServiceModeBuild || service.Mode == stacks.ServiceModeHybrid || service.BuildContext != nil {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) RunUpdate(ctx context.Context, request UpdateRequest, requestedBy string) (store.Job, error) {
	job, run, err := s.StartUpdate(ctx, request, requestedBy)
	if err != nil {
		return store.Job{}, err
	}
	return s.ExecuteUpdate(ctx, job, run)
}

func (s *Service) StartUpdate(ctx context.Context, request UpdateRequest, requestedBy string) (store.Job, UpdateRun, error) {
	if request.Options.IncludeVolumes && !request.Options.PruneAfter {
		return store.Job{}, UpdateRun{}, errors.New("include_volumes requires prune_after = true")
	}
	if request.Options.RemoveOrphans && hasServiceExclusions(request.Target.ExcludedServices) {
		return store.Job{}, UpdateRun{}, errors.New("remove_orphans cannot be used with service exclusions")
	}

	targetStackIDs, err := s.ResolveTargetStacks(ctx, request.Target.Mode, request.Target.StackIDs)
	if err != nil {
		return store.Job{}, UpdateRun{}, err
	}
	managedStackIDs := append([]string(nil), targetStackIDs...)
	resourceStackIDs := targetStackIDs
	if request.Options.PruneAfter && request.Options.IncludeVolumes {
		managedStackIDs, err = s.listManagedStackIDs(ctx)
		if err != nil {
			return store.Job{}, UpdateRun{}, err
		}
		resourceStackIDs = managedStackIDs
	}
	serviceTargets, err := s.resolveUpdateServiceTargets(ctx, targetStackIDs, request.Target.ExcludedServices)
	if err != nil {
		return store.Job{}, UpdateRun{}, err
	}

	preserveInactive := request.Target.Mode == "all" || request.Trigger == "scheduled"
	workflow, err := s.buildUpdateWorkflow(ctx, targetStackIDs, serviceTargets, request.Options, preserveInactive)
	if err != nil {
		return store.Job{}, UpdateRun{}, err
	}

	if len(workflow) > 0 {
		workflow = markWorkflowRunning(workflow, 0)
	}
	resources := jobs.StackResources(resourceStackIDs)
	if len(resources) == 0 {
		resources = []jobs.Resource{jobs.GlobalResource()}
	}
	job, err := s.jobs.StartWithResourcesAndWorkflow(ctx, "", "update_stacks", requestedBy, workflow, resources...)
	if err != nil {
		return store.Job{}, UpdateRun{}, err
	}

	if len(workflow) > 0 {
		_ = s.jobs.PublishEvent(ctx, job, "job_step_started", updateStepMessage("Starting", workflow[0]), "", workflowStepRef(workflow, 0))
	}

	return job, UpdateRun{
		Request:         request,
		TargetStackIDs:  append([]string(nil), targetStackIDs...),
		ManagedStackIDs: append([]string(nil), managedStackIDs...),
		Workflow:        append([]store.JobWorkflowStep(nil), workflow...),
	}, nil
}

func (s *Service) ExecuteUpdate(ctx context.Context, job store.Job, run UpdateRun) (store.Job, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	unregisterCancel := s.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()
	ctx = runCtx

	workflow := append([]store.JobWorkflowStep(nil), run.Workflow...)
	failures := make([]updateStepFailure, 0)
	blockedStacks := make(map[string]updateStepFailure)

	for index := range workflow {
		step := workflow[index]
		if reason := updateStepSkipReason(step, failures, blockedStacks); reason != "" {
			workflow = markWorkflowState(workflow, index, "skipped")
			updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow)
			if updateErr != nil {
				return s.finishUpdateExecutionError(ctx, job, workflow, index, updateErr, run)
			}
			job = updatedJob
			_ = s.jobs.PublishEvent(ctx, job, "job_step_finished", reason, "", workflowStepRef(workflow, index))
			continue
		}

		if workflow[index].State != "running" {
			workflow = markWorkflowRunning(workflow, index)
			updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow)
			if updateErr != nil {
				return s.finishUpdateExecutionError(ctx, job, workflow, index, updateErr, run)
			}
			job = updatedJob
			_ = s.jobs.PublishEvent(ctx, job, "job_step_started", updateStepMessage("Starting", workflow[index]), "", workflowStepRef(workflow, index))
		}

		step = workflow[index]
		result, runErr := s.runUpdateWorkflowStepWithRetry(ctx, job, workflow, index, run.Request.Options, run.ManagedStackIDs, run.Request.Trigger)
		if runErr != nil {
			if ctx.Err() != nil || errors.Is(runErr, context.Canceled) {
				finishCtx, finishCancel := jobFinalizationContext()
				terminalState, errorCode, errorMessage := maintenanceFailure(ctx, "update_stacks_failed", runErr, s.jobCancelRequested(job.ID))
				finishedJob, finishErr := s.finishUpdateFailure(finishCtx, job, workflow, index, terminalState, errorCode, errorMessage, run)
				finishCancel()
				return finishedJob, finishErr
			}

			failure := updateStepFailure{
				StackID:  step.TargetStackID,
				Action:   step.Action,
				Attempts: result.Attempts,
				Message:  summarizeMaintenanceError(result.Output, runErr),
			}
			failures = append(failures, failure)
			if step.TargetStackID != "" {
				blockedStacks[step.TargetStackID] = failure
			}

			workflow = markWorkflowState(workflow, index, "failed")
			updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow)
			if updateErr != nil {
				return s.finishUpdateExecutionError(ctx, job, workflow, index, updateErr, run)
			}
			job = updatedJob
			stepRef := workflowStepRef(workflow, index)
			_ = s.jobs.PublishEvent(ctx, job, "job_error", updateStepFailureMessage(failure), "", stepRef)
			_ = s.jobs.PublishEvent(ctx, job, "job_step_finished", updateStepMessage("Failed", step), "", stepRef)
			continue
		}

		if step.TargetStackID != "" && updateStepInvalidatesImageUpdates(step.Action) {
			if err := s.stackReader.InvalidateImageUpdateStatus(ctx, step.TargetStackID, step.TargetServiceNames); err != nil && s.logger != nil {
				s.logger.Warn("invalidate image update status failed", slog.String("stack_id", step.TargetStackID), slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
		}
		if step.Action == "up" && step.TargetStackID != "" && step.TargetServiceNames == nil {
			if baselineErr := s.stackReader.RecordDeployBaseline(ctx, step.TargetStackID, job.ID, time.Now().UTC()); baselineErr != nil && s.logger != nil {
				s.logger.Warn("record deploy baseline failed", slog.String("stack_id", step.TargetStackID), slog.String("job_id", job.ID), slog.String("err", baselineErr.Error()))
			}
		}

		workflow = markWorkflowSucceeded(workflow, index)
		updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow)
		if updateErr != nil {
			return s.finishUpdateExecutionError(ctx, job, workflow, index, updateErr, run)
		}
		job = updatedJob
		_ = s.jobs.PublishEvent(ctx, job, "job_step_finished", updateStepMessage("Finished", step), "", workflowStepRef(workflow, index))
	}

	finishCtx, finishCancel := jobFinalizationContext()
	defer finishCancel()
	if len(failures) > 0 {
		message := aggregateUpdateFailureMessage(failures)
		finishedJob, finishErr := s.jobs.FinishFailed(finishCtx, job, "update_stacks_failed", message)
		if finishErr != nil {
			return finishedJob, finishErr
		}
		details := updateAuditDetails(run.Request, run.TargetStackIDs)
		details["failed_steps"] = updateFailureAuditDetails(failures)
		if auditErr := s.audit.RecordJob(finishCtx, finishedJob, details); auditErr != nil && s.logger != nil {
			s.logger.Warn("record maintenance audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", auditErr.Error()))
		}
		s.logUpdateFailures(finishedJob, run.Request, failures)
		return finishedJob, nil
	}

	finishedJob, finishErr := s.jobs.FinishSucceeded(finishCtx, job)
	if finishErr != nil {
		return finishedJob, finishErr
	}
	if auditErr := s.audit.RecordJob(finishCtx, finishedJob, updateAuditDetails(run.Request, run.TargetStackIDs)); auditErr != nil && s.logger != nil {
		s.logger.Warn("record maintenance audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", auditErr.Error()))
	}
	return finishedJob, nil
}

func (s *Service) finishUpdateExecutionError(ctx context.Context, job store.Job, workflow []store.JobWorkflowStep, index int, runErr error, run UpdateRun) (store.Job, error) {
	finishCtx, finishCancel := jobFinalizationContext()
	defer finishCancel()
	terminalState, errorCode, errorMessage := maintenanceFailure(ctx, "update_stacks_failed", runErr, s.jobCancelRequested(job.ID))
	finishedJob, finishErr := s.finishUpdateFailure(finishCtx, job, workflow, index, terminalState, errorCode, errorMessage, run)
	return finishedJob, errors.Join(runErr, finishErr)
}

func (s *Service) RunPrune(ctx context.Context, request PruneRequest, requestedBy string, managedStackIDs []string) (store.Job, error) {
	job, run, err := s.StartPrune(ctx, request, requestedBy, managedStackIDs)
	if err != nil {
		return store.Job{}, err
	}
	return s.ExecutePrune(ctx, job, run)
}

func (s *Service) StartPrune(ctx context.Context, request PruneRequest, requestedBy string, managedStackIDs []string) (store.Job, PruneRun, error) {
	if !request.Scope.Images && !request.Scope.BuildCache && !request.Scope.StoppedContainers && !request.Scope.Volumes {
		return store.Job{}, PruneRun{}, errors.New("at least one prune scope must be enabled")
	}

	workflow := buildPruneWorkflow(request.Scope)
	if len(workflow) > 0 {
		workflow = markWorkflowRunning(workflow, 0)
	}
	job, err := s.jobs.StartWithResourcesAndWorkflow(ctx, "", "prune", requestedBy, workflow, jobs.GlobalResource())
	if err != nil {
		return store.Job{}, PruneRun{}, err
	}

	if len(workflow) > 0 {
		_ = s.jobs.PublishEvent(ctx, job, "job_step_started", pruneStepMessage("Starting", workflow[0]), "", workflowStepRef(workflow, 0))
	}

	return job, PruneRun{
		Request:        request,
		TargetStackIDs: append([]string(nil), managedStackIDs...),
		Workflow:       append([]store.JobWorkflowStep(nil), workflow...),
	}, nil
}

func (s *Service) ExecutePrune(ctx context.Context, job store.Job, run PruneRun) (store.Job, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	unregisterCancel := s.jobs.RegisterCancel(job.ID, cancel)
	defer unregisterCancel()
	ctx = runCtx

	workflow := append([]store.JobWorkflowStep(nil), run.Workflow...)
	var err error

	for index, step := range workflow {
		output, runErr := s.maintenance.RunPruneStep(ctx, step.Action, run.TargetStackIDs)
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			_ = s.jobs.PublishEvent(ctx, job, "job_log", pruneStepMessage("Output", step), trimmed, workflowStepRef(workflow, index))
		}
		if runErr != nil {
			finishCtx, cancel := jobFinalizationContext()
			terminalState, errorCode, errorMessage := maintenanceFailure(ctx, "prune_failed", runErr, s.jobCancelRequested(job.ID))
			finishedJob, finishErr := s.finishPruneFailure(finishCtx, job, workflow, index, terminalState, errorCode, errorMessage, run)
			cancel()
			if finishErr != nil {
				return finishedJob, finishErr
			}
			return finishedJob, nil
		}

		workflow = markWorkflowSucceeded(workflow, index)
		_ = s.jobs.PublishEvent(ctx, job, "job_step_finished", pruneStepMessage("Finished", step), "", workflowStepRef(workflow, index))
		if index+1 < len(workflow) {
			workflow = markWorkflowRunning(workflow, index+1)
			_ = s.jobs.PublishEvent(ctx, job, "job_step_started", pruneStepMessage("Starting", workflow[index+1]), "", workflowStepRef(workflow, index+1))
		}
		updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow)
		if updateErr != nil {
			finishCtx, cancel := jobFinalizationContext()
			terminalState, errorCode, errorMessage := maintenanceFailure(ctx, "prune_failed", updateErr, s.jobCancelRequested(job.ID))
			finishedJob, finishErr := s.finishPruneFailure(finishCtx, job, workflow, index, terminalState, errorCode, errorMessage, run)
			cancel()
			return finishedJob, errors.Join(updateErr, finishErr)
		}
		job = updatedJob
	}

	finishCtx, cancel := jobFinalizationContext()
	defer cancel()
	job, err = s.jobs.FinishSucceeded(finishCtx, job)
	if err != nil {
		return job, err
	}
	if err := s.audit.RecordJob(finishCtx, job, pruneAuditDetails(run.Request)); err != nil && s.logger != nil {
		s.logger.Warn("record prune audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	return job, nil
}

func (s *Service) finishUpdateFailure(ctx context.Context, job store.Job, workflow []store.JobWorkflowStep, index int, terminalState, errorCode, errorMessage string, run UpdateRun) (store.Job, error) {
	workflow = markWorkflowState(workflow, index, terminalState)
	if updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
		job = updatedJob
	} else if s.logger != nil {
		s.logger.Warn("update failed maintenance workflow failed", slog.String("job_id", job.ID), slog.String("err", updateErr.Error()))
	}
	// Mark the failing step before the terminal transition so live consumers
	// never see a finished job with a step still running.
	failingJob := job
	failingJob.State = terminalState
	_ = s.jobs.PublishEvent(ctx, failingJob, "job_step_finished", updateStepMessage(terminalStepVerb(terminalState), workflow[index]), "", workflowStepRef(workflow, index))
	finishedJob, err := finishMaintenanceJob(ctx, s.jobs, job, terminalState, errorCode, errorMessage)
	if err != nil {
		return finishedJob, err
	}
	if err := s.audit.RecordJob(ctx, finishedJob, updateAuditDetails(run.Request, run.TargetStackIDs)); err != nil && s.logger != nil {
		s.logger.Warn("record maintenance audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
	s.logTerminalMaintenanceFailure("update", finishedJob, run.Request.Trigger, run.Request.ScheduleKey)
	return finishedJob, nil
}

func (s *Service) finishPruneFailure(ctx context.Context, job store.Job, workflow []store.JobWorkflowStep, index int, terminalState, errorCode, errorMessage string, run PruneRun) (store.Job, error) {
	workflow = markWorkflowState(workflow, index, terminalState)
	if updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
		job = updatedJob
	} else if s.logger != nil {
		s.logger.Warn("update failed prune workflow failed", slog.String("job_id", job.ID), slog.String("err", updateErr.Error()))
	}
	failingJob := job
	failingJob.State = terminalState
	_ = s.jobs.PublishEvent(ctx, failingJob, "job_step_finished", pruneStepMessage(terminalStepVerb(terminalState), workflow[index]), "", workflowStepRef(workflow, index))
	finishedJob, err := finishMaintenanceJob(ctx, s.jobs, job, terminalState, errorCode, errorMessage)
	if err != nil {
		return finishedJob, err
	}
	if err := s.audit.RecordJob(ctx, finishedJob, pruneAuditDetails(run.Request)); err != nil && s.logger != nil {
		s.logger.Warn("record prune audit failed", slog.String("job_id", finishedJob.ID), slog.String("err", err.Error()))
	}
	s.logTerminalMaintenanceFailure("prune", finishedJob, run.Request.Trigger, run.Request.ScheduleKey)
	return finishedJob, nil
}

func (s *Service) logTerminalMaintenanceFailure(kind string, job store.Job, trigger, scheduleKey string) {
	if s.logger == nil || (job.State != "failed" && job.State != "timed_out") {
		return
	}
	s.logger.Error("maintenance job failed",
		slog.String("kind", kind),
		slog.String("job_id", job.ID),
		slog.String("trigger", fallbackTrigger(trigger)),
		slog.String("schedule_key", scheduleKey),
		slog.String("state", job.State),
		slog.String("error_code", job.ErrorCode),
		slog.String("err", truncateRunes(job.ErrorMessage, maintenanceErrorSummaryLimit)),
	)
}

func (s *Service) jobCancelRequested(jobID string) bool {
	ctx, cancel := jobFinalizationContext()
	defer cancel()
	job, err := s.jobs.Get(ctx, jobID)
	return err == nil && job.State == "cancel_requested"
}

func maintenanceFailure(ctx context.Context, defaultCode string, err error, cancelRequested bool) (terminalState, errorCode, errorMessage string) {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return "timed_out", strings.TrimSuffix(defaultCode, "_failed") + "_timed_out", "Maintenance job timed out."
	case cancelRequested && (errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled)):
		return "cancelled", strings.TrimSuffix(defaultCode, "_failed") + "_cancelled", "Maintenance job was cancelled."
	default:
		return "failed", defaultCode, err.Error()
	}
}

func finishMaintenanceJob(ctx context.Context, jobService *jobs.Service, job store.Job, terminalState, errorCode, errorMessage string) (store.Job, error) {
	switch terminalState {
	case "timed_out":
		return jobService.FinishTimedOut(ctx, job, errorCode, errorMessage)
	case "cancelled":
		return jobService.FinishCancelled(ctx, job, errorCode, errorMessage)
	default:
		return jobService.FinishFailed(ctx, job, errorCode, errorMessage)
	}
}

func terminalStepVerb(terminalState string) string {
	switch terminalState {
	case "cancelled":
		return "Cancelled"
	case "timed_out":
		return "Timed out"
	default:
		return "Failed"
	}
}

func jobFinalizationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (s *Service) buildUpdateWorkflow(ctx context.Context, stackIDs []string, serviceTargets map[string][]string, options UpdateOptions, preserveInactive bool) ([]store.JobWorkflowStep, error) {
	steps := make([]store.JobWorkflowStep, 0, len(stackIDs)*3+1)
	for _, stackID := range stackIDs {
		serviceNames := serviceTargets[stackID]
		if serviceNames != nil && len(serviceNames) == 0 {
			steps = append(steps, store.JobWorkflowStep{Action: "skip", State: "queued", TargetStackID: stackID})
			continue
		}
		keepInactive := false
		if preserveInactive {
			detail, err := s.stackReader.Get(ctx, stackID)
			if err != nil {
				return nil, err
			}
			keepInactive = detail.Stack.RuntimeState == stacks.RuntimeStateDefined || detail.Stack.RuntimeState == stacks.RuntimeStateStopped
		}
		if options.PullImages {
			steps = append(steps, store.JobWorkflowStep{Action: "pull", State: "queued", TargetStackID: stackID, TargetServiceNames: serviceNames})
		}
		if options.BuildImages {
			needsBuild, err := s.maintenanceNeedsBuild(ctx, stackID, serviceNames)
			if err != nil {
				return nil, err
			}
			if needsBuild {
				steps = append(steps, store.JobWorkflowStep{Action: "build", State: "queued", TargetStackID: stackID, TargetServiceNames: serviceNames})
			}
		}
		if keepInactive {
			steps = append(steps, store.JobWorkflowStep{Action: "preserve_inactive", State: "queued", TargetStackID: stackID})
			continue
		}
		steps = append(steps, store.JobWorkflowStep{Action: "up", State: "queued", TargetStackID: stackID, TargetServiceNames: serviceNames})
	}
	if options.PruneAfter {
		steps = append(steps, store.JobWorkflowStep{Action: "prune", State: "queued"})
	}
	return steps, nil
}

func (s *Service) runUpdateWorkflowStep(ctx context.Context, step store.JobWorkflowStep, options UpdateOptions, targetStackIDs []string, onProgress func(stacks.StepProgress)) (string, error) {
	if step.Action == "prune" {
		return s.maintenance.RunSystemPrune(ctx, options.IncludeVolumes, targetStackIDs)
	}
	if step.Action == "skip" {
		return "Skipped " + step.TargetStackID + " because all services are excluded.", nil
	}
	if step.Action == "preserve_inactive" {
		return "Skipped deployment for " + step.TargetStackID + " because it was inactive before maintenance.", nil
	}
	return s.stackReader.RunMaintenanceStepStreaming(ctx, step.TargetStackID, step.Action, stacks.MaintenanceStepOptions{
		RemoveOrphans: options.RemoveOrphans,
		ServiceNames:  step.TargetServiceNames,
	}, onProgress)
}

func (s *Service) runUpdateWorkflowStepWithRetry(ctx context.Context, job store.Job, workflow []store.JobWorkflowStep, index int, options UpdateOptions, targetStackIDs []string, trigger string) (updateStepResult, error) {
	step := workflow[index]
	delays := []time.Duration(nil)
	if step.Action == "pull" || step.Action == "build" {
		delays = s.updateStepRetryDelays
	}
	maxAttempts := len(delays) + 1
	result := updateStepResult{}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.Attempts = attempt
		onProgress := s.progressPublisher(ctx, job, step, workflowStepRef(workflow, index))
		output, runErr := s.runUpdateWorkflowStep(ctx, step, options, targetStackIDs, onProgress)
		result.Output = output
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			message := updateStepMessage("Output", step)
			if attempt > 1 {
				message = fmt.Sprintf("%s Attempt %d/%d.", strings.TrimSuffix(message, "."), attempt, maxAttempts)
			}
			_ = s.jobs.PublishEvent(ctx, job, "job_log", message, trimmed, workflowStepRef(workflow, index))
		}
		if runErr == nil {
			return result, nil
		}
		if attempt == maxAttempts || !isRetryableUpdateStepError(ctx, output, runErr) {
			return result, runErr
		}

		delay := delays[attempt-1]
		summary := summarizeMaintenanceError(output, runErr)
		message := fmt.Sprintf("%s attempt %d/%d failed for %s; retrying in %s.", maintenanceActionLabel(step.Action), attempt, maxAttempts, updateStepTarget(step), delay)
		_ = s.jobs.PublishEvent(ctx, job, "job_warning", message, summary, workflowStepRef(workflow, index))
		if s.logger != nil {
			s.logger.Warn("maintenance update step retry scheduled",
				slog.String("job_id", job.ID),
				slog.String("trigger", fallbackTrigger(trigger)),
				slog.String("stack_id", step.TargetStackID),
				slog.String("action", step.Action),
				slog.Int("attempt", attempt),
				slog.Int("max_attempts", maxAttempts),
				slog.Duration("retry_in", delay),
				slog.String("err", summary),
			)
		}
		wait := s.retryWait
		if wait == nil {
			wait = waitForRetry
		}
		if waitErr := wait(ctx, delay); waitErr != nil {
			return result, waitErr
		}
	}
	return result, errors.New("maintenance update step retry loop exhausted unexpectedly")
}

func updateStepSkipReason(step store.JobWorkflowStep, failures []updateStepFailure, blockedStacks map[string]updateStepFailure) string {
	if step.Action == "prune" && len(failures) > 0 {
		return "Skipped prune because one or more stack updates failed."
	}
	if failure, ok := blockedStacks[step.TargetStackID]; ok && step.TargetStackID != "" {
		return fmt.Sprintf("Skipped %s for %s because %s failed.", strings.ToLower(maintenanceActionLabel(step.Action)), step.TargetStackID, strings.ToLower(maintenanceActionLabel(failure.Action)))
	}
	return ""
}

func isRetryableUpdateStepError(ctx context.Context, output string, err error) bool {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// The parent is still active, so this deadline belongs to the Docker or
		// registry operation and is safe to retry as a transient step failure.
		return true
	}
	message := strings.ToLower(strings.TrimSpace(output + "\n" + err.Error()))
	markers := []string{
		"context deadline exceeded",
		"tls handshake timeout",
		"client.timeout exceeded",
		"i/o timeout",
		"connection reset by peer",
		"connection refused",
		"unexpected eof",
		"temporary failure in name resolution",
		"no such host",
		"server misbehaving",
		"toomanyrequests",
		"too many requests",
		"status code 429",
		"retry-after",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func summarizeMaintenanceError(output string, err error) string {
	message := strings.TrimSpace(output)
	if message == "" && err != nil {
		message = strings.TrimSpace(err.Error())
	}
	lines := strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return truncateRunes(line, maintenanceErrorSummaryLimit)
		}
	}
	return "Maintenance step failed."
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func updateStepTarget(step store.JobWorkflowStep) string {
	if step.TargetStackID != "" {
		return step.TargetStackID
	}
	return "workspace"
}

func updateStepFailureMessage(failure updateStepFailure) string {
	return fmt.Sprintf("%s failed for %s after %d attempt(s): %s", maintenanceActionLabel(failure.Action), updateFailureTarget(failure), failure.Attempts, failure.Message)
}

func updateFailureTarget(failure updateStepFailure) string {
	if failure.StackID != "" {
		return failure.StackID
	}
	return "workspace"
}

func aggregateUpdateFailureMessage(failures []updateStepFailure) string {
	label := "update step failed"
	if len(failures) != 1 {
		label = "update steps failed"
	}
	parts := make([]string, 0, len(failures))
	for _, failure := range failures {
		parts = append(parts, fmt.Sprintf("%s/%s after %d attempt(s): %s", updateFailureTarget(failure), failure.Action, failure.Attempts, failure.Message))
	}
	return truncateRunes(fmt.Sprintf("%d %s: %s", len(failures), label, strings.Join(parts, "; ")), maintenanceErrorMessageLimit)
}

func updateFailureAuditDetails(failures []updateStepFailure) []map[string]any {
	details := make([]map[string]any, 0, len(failures))
	for _, failure := range failures {
		details = append(details, map[string]any{
			"stack_id": failure.StackID,
			"action":   failure.Action,
			"attempts": failure.Attempts,
			"message":  failure.Message,
		})
	}
	return details
}

func (s *Service) logUpdateFailures(job store.Job, request UpdateRequest, failures []updateStepFailure) {
	if s.logger == nil {
		return
	}
	stackSet := make(map[string]struct{})
	for _, failure := range failures {
		if failure.StackID != "" {
			stackSet[failure.StackID] = struct{}{}
		}
	}
	failedStacks := make([]string, 0, len(stackSet))
	for stackID := range stackSet {
		failedStacks = append(failedStacks, stackID)
	}
	sort.Strings(failedStacks)
	s.logger.Error("maintenance update completed with failures",
		slog.String("job_id", job.ID),
		slog.String("trigger", fallbackTrigger(request.Trigger)),
		slog.String("schedule_key", request.ScheduleKey),
		slog.Int("failed_step_count", len(failures)),
		slog.Any("failed_stacks", failedStacks),
		slog.String("err", truncateRunes(job.ErrorMessage, maintenanceErrorSummaryLimit)),
	)
}

func fallbackTrigger(trigger string) string {
	if strings.TrimSpace(trigger) == "" {
		return "manual"
	}
	return trigger
}

var progressUnits = map[string]string{
	"pull":              "layers",
	"build":             "steps",
	"up":                "services",
	"skip":              "services",
	"preserve_inactive": "services",
}

// progressPublisher translates streaming compose progress into throttled
// job_progress events carrying the structured payload (Slice C).
func (s *Service) progressPublisher(ctx context.Context, job store.Job, step store.JobWorkflowStep, stepRef *store.JobEventStep) func(stacks.StepProgress) {
	unit := progressUnits[step.Action]
	if unit == "" {
		unit = "items"
	}
	return func(progress stacks.StepProgress) {
		payload := &store.JobProgress{
			Phase:     step.Action,
			Completed: progress.Completed,
			Total:     progress.Total,
			Unit:      unit,
			Detail:    progress.Detail,
		}
		_ = s.jobs.PublishEventWithProgress(ctx, job, "job_progress", updateStepMessage("Progress", step), "", stepRef, payload)
	}
}

func markWorkflowRunning(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = "running"
	}
	return steps
}

func markWorkflowSucceeded(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = "succeeded"
	}
	return steps
}

func markWorkflowState(steps []store.JobWorkflowStep, index int, state string) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = state
	}
	return steps
}

func workflowStepRef(steps []store.JobWorkflowStep, index int) *store.JobEventStep {
	if index < 0 || index >= len(steps) {
		return nil
	}
	return &store.JobEventStep{
		Index:              index + 1,
		Total:              len(steps),
		Action:             steps[index].Action,
		State:              steps[index].State,
		TargetStackID:      steps[index].TargetStackID,
		TargetServiceNames: steps[index].TargetServiceNames,
	}
}

func updateStepMessage(prefix string, step store.JobWorkflowStep) string {
	label := maintenanceActionLabel(step.Action)
	if step.TargetStackID == "" {
		return prefix + " " + strings.ToLower(label) + "."
	}
	if len(step.TargetServiceNames) > 0 {
		return prefix + " " + strings.ToLower(label) + " for " + step.TargetStackID + " services " + strings.Join(step.TargetServiceNames, ", ") + "."
	}
	return prefix + " " + strings.ToLower(label) + " for " + step.TargetStackID + "."
}

func pruneStepMessage(prefix string, step store.JobWorkflowStep) string {
	return prefix + " " + strings.ToLower(maintenanceActionLabel(step.Action)) + "."
}

func maintenanceActionLabel(action string) string {
	switch action {
	case "pull":
		return "Pull"
	case "build":
		return "Build"
	case "up":
		return "Up"
	case "prune":
		return "Prune"
	case "skip":
		return "Skip"
	case "preserve_inactive":
		return "Preserve inactive state"
	case "prune_images":
		return "Prune images"
	case "prune_build_cache":
		return "Prune build cache"
	case "prune_stopped_containers":
		return "Prune stopped containers"
	case "prune_volumes":
		return "Prune volumes"
	default:
		return action
	}
}

func updateStepInvalidatesImageUpdates(action string) bool {
	return action == "pull" || action == "build"
}

func buildPruneWorkflow(scope maintenance.PruneScope) []store.JobWorkflowStep {
	steps := []store.JobWorkflowStep{}
	if scope.Images {
		steps = append(steps, store.JobWorkflowStep{Action: "prune_images", State: "queued"})
	}
	if scope.BuildCache {
		steps = append(steps, store.JobWorkflowStep{Action: "prune_build_cache", State: "queued"})
	}
	if scope.StoppedContainers {
		steps = append(steps, store.JobWorkflowStep{Action: "prune_stopped_containers", State: "queued"})
	}
	if scope.Volumes {
		steps = append(steps, store.JobWorkflowStep{Action: "prune_volumes", State: "queued"})
	}
	return steps
}

func updateAuditDetails(request UpdateRequest, stackIDs []string) map[string]any {
	details := map[string]any{
		"target_mode": request.Target.Mode,
		"stack_ids":   stackIDs,
		"options": map[string]any{
			"pull_images":     request.Options.PullImages,
			"build_images":    request.Options.BuildImages,
			"remove_orphans":  request.Options.RemoveOrphans,
			"prune_after":     request.Options.PruneAfter,
			"include_volumes": request.Options.IncludeVolumes,
		},
	}
	if hasServiceExclusions(request.Target.ExcludedServices) {
		details["excluded_services"] = normalizeExcludedServices(request.Target.ExcludedServices)
	}
	if request.Trigger != "" {
		details["trigger"] = request.Trigger
	}
	if request.ScheduleKey != "" {
		details["schedule_key"] = request.ScheduleKey
	}
	return details
}

func normalizeExcludedServices(excluded map[string][]string) map[string][]string {
	result := map[string][]string{}
	for stackID, serviceNames := range excluded {
		normalized := dedupeSortedStrings(serviceNames)
		if len(normalized) > 0 {
			result[stackID] = normalized
		}
	}
	return result
}

func pruneAuditDetails(request PruneRequest) map[string]any {
	details := map[string]any{
		"scope": map[string]any{
			"images":             request.Scope.Images,
			"build_cache":        request.Scope.BuildCache,
			"stopped_containers": request.Scope.StoppedContainers,
			"volumes":            request.Scope.Volumes,
		},
	}
	if request.Trigger != "" {
		details["trigger"] = request.Trigger
	}
	if request.ScheduleKey != "" {
		details["schedule_key"] = request.ScheduleKey
	}
	return details
}

func dedupeSortedStackIDs(stackIDs []string) []string {
	return dedupeSortedStrings(stackIDs)
}

func dedupeSortedStrings(values []string) []string {
	unique := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
