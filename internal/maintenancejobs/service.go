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
	RunPruneStep(ctx context.Context, action string) (string, error)
	RunSystemPrune(ctx context.Context, includeVolumes bool) (string, error)
}

type Service struct {
	logger      *slog.Logger
	jobs        *jobs.Service
	audit       *audit.Service
	stackReader stackReader
	maintenance pruneRunner
}

func NewService(logger *slog.Logger, jobService *jobs.Service, auditService *audit.Service, stackService stackReader, maintenanceService pruneRunner) *Service {
	return &Service{
		logger:      logger,
		jobs:        jobService,
		audit:       auditService,
		stackReader: stackService,
		maintenance: maintenanceService,
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
	if request.Options.IncludeVolumes && !request.Options.PruneAfter {
		return store.Job{}, errors.New("include_volumes requires prune_after = true")
	}
	if request.Options.RemoveOrphans && hasServiceExclusions(request.Target.ExcludedServices) {
		return store.Job{}, errors.New("remove_orphans cannot be used with service exclusions")
	}

	targetStackIDs, err := s.ResolveTargetStacks(ctx, request.Target.Mode, request.Target.StackIDs)
	if err != nil {
		return store.Job{}, err
	}
	serviceTargets, err := s.resolveUpdateServiceTargets(ctx, targetStackIDs, request.Target.ExcludedServices)
	if err != nil {
		return store.Job{}, err
	}

	workflow, err := s.buildUpdateWorkflow(ctx, targetStackIDs, serviceTargets, request.Options)
	if err != nil {
		return store.Job{}, err
	}

	job, err := s.jobs.StartWithLocks(ctx, "", "update_stacks", requestedBy, targetStackIDs)
	if err != nil {
		return store.Job{}, err
	}

	if len(workflow) > 0 {
		workflow = markWorkflowRunning(workflow, 0)
		if job, err = s.jobs.UpdateWorkflow(ctx, job, workflow); err != nil {
			return store.Job{}, err
		}
		_ = s.jobs.PublishEvent(ctx, job, "job_step_started", updateStepMessage("Starting", workflow[0]), "", workflowStepRef(workflow, 0))
	}

	for index, step := range workflow {
		stepRef := workflowStepRef(workflow, index)
		onProgress := s.progressPublisher(ctx, job, step, stepRef)
		output, runErr := s.runUpdateWorkflowStep(ctx, step, request.Options, onProgress)
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			_ = s.jobs.PublishEvent(ctx, job, "job_log", updateStepMessage("Output", step), trimmed, workflowStepRef(workflow, index))
		}
		if runErr != nil {
			workflow = markWorkflowFailed(workflow, index)
			if updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
				job = updatedJob
			}
			// Mark the failing step before the terminal transition so live
			// consumers never see a finished job with a step still running.
			failingJob := job
			failingJob.State = "failed"
			_ = s.jobs.PublishEvent(ctx, failingJob, "job_step_finished", updateStepMessage("Failed", step), "", workflowStepRef(workflow, index))
			job, _ = s.jobs.FinishFailed(ctx, job, "update_stacks_failed", runErr.Error())
			if err := s.audit.RecordJob(ctx, job, updateAuditDetails(request, targetStackIDs)); err != nil && s.logger != nil {
				s.logger.Warn("record maintenance audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
			return job, nil
		}
		if step.TargetStackID != "" && updateStepInvalidatesImageUpdates(step.Action) {
			if err := s.stackReader.InvalidateImageUpdateStatus(ctx, step.TargetStackID, step.TargetServiceNames); err != nil && s.logger != nil {
				s.logger.Warn("invalidate image update status failed", slog.String("stack_id", step.TargetStackID), slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
		}
		if step.Action == "up" && step.TargetStackID != "" && step.TargetServiceNames == nil {
			if baselineErr := s.stackReader.RecordDeployBaseline(ctx, step.TargetStackID, job.ID, time.Now().UTC()); baselineErr != nil {
				if s.logger != nil {
					s.logger.Warn("record deploy baseline failed", slog.String("stack_id", step.TargetStackID), slog.String("job_id", job.ID), slog.String("err", baselineErr.Error()))
				}
			}
		}

		workflow = markWorkflowSucceeded(workflow, index)
		_ = s.jobs.PublishEvent(ctx, job, "job_step_finished", updateStepMessage("Finished", step), "", workflowStepRef(workflow, index))
		if index+1 < len(workflow) {
			workflow = markWorkflowRunning(workflow, index+1)
			_ = s.jobs.PublishEvent(ctx, job, "job_step_started", updateStepMessage("Starting", workflow[index+1]), "", workflowStepRef(workflow, index+1))
		}
		if job, err = s.jobs.UpdateWorkflow(ctx, job, workflow); err != nil {
			return store.Job{}, err
		}
	}

	job, err = s.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		return store.Job{}, err
	}
	if err := s.audit.RecordJob(ctx, job, updateAuditDetails(request, targetStackIDs)); err != nil && s.logger != nil {
		s.logger.Warn("record maintenance audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	return job, nil
}

func (s *Service) RunPrune(ctx context.Context, request PruneRequest, requestedBy string, lockStackIDs []string) (store.Job, error) {
	if !request.Scope.Images && !request.Scope.BuildCache && !request.Scope.StoppedContainers && !request.Scope.Volumes {
		return store.Job{}, errors.New("at least one prune scope must be enabled")
	}

	workflow := buildPruneWorkflow(request.Scope)
	job, err := s.jobs.StartWithLocks(ctx, "", "prune", requestedBy, lockStackIDs)
	if err != nil {
		return store.Job{}, err
	}

	if len(workflow) > 0 {
		workflow = markWorkflowRunning(workflow, 0)
		if job, err = s.jobs.UpdateWorkflow(ctx, job, workflow); err != nil {
			return store.Job{}, err
		}
		_ = s.jobs.PublishEvent(ctx, job, "job_step_started", pruneStepMessage("Starting", workflow[0]), "", workflowStepRef(workflow, 0))
	}

	for index, step := range workflow {
		output, runErr := s.maintenance.RunPruneStep(ctx, step.Action)
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			_ = s.jobs.PublishEvent(ctx, job, "job_log", pruneStepMessage("Output", step), trimmed, workflowStepRef(workflow, index))
		}
		if runErr != nil {
			workflow = markWorkflowFailed(workflow, index)
			if updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
				job = updatedJob
			}
			failingJob := job
			failingJob.State = "failed"
			_ = s.jobs.PublishEvent(ctx, failingJob, "job_step_finished", pruneStepMessage("Failed", step), "", workflowStepRef(workflow, index))
			job, _ = s.jobs.FinishFailed(ctx, job, "prune_failed", runErr.Error())
			if err := s.audit.RecordJob(ctx, job, pruneAuditDetails(request)); err != nil && s.logger != nil {
				s.logger.Warn("record prune audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
			return job, nil
		}

		workflow = markWorkflowSucceeded(workflow, index)
		_ = s.jobs.PublishEvent(ctx, job, "job_step_finished", pruneStepMessage("Finished", step), "", workflowStepRef(workflow, index))
		if index+1 < len(workflow) {
			workflow = markWorkflowRunning(workflow, index+1)
			_ = s.jobs.PublishEvent(ctx, job, "job_step_started", pruneStepMessage("Starting", workflow[index+1]), "", workflowStepRef(workflow, index+1))
		}
		if job, err = s.jobs.UpdateWorkflow(ctx, job, workflow); err != nil {
			return store.Job{}, err
		}
	}

	job, err = s.jobs.FinishSucceeded(ctx, job)
	if err != nil {
		return store.Job{}, err
	}
	if err := s.audit.RecordJob(ctx, job, pruneAuditDetails(request)); err != nil && s.logger != nil {
		s.logger.Warn("record prune audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
	}
	return job, nil
}

func (s *Service) buildUpdateWorkflow(ctx context.Context, stackIDs []string, serviceTargets map[string][]string, options UpdateOptions) ([]store.JobWorkflowStep, error) {
	steps := make([]store.JobWorkflowStep, 0, len(stackIDs)*3+1)
	for _, stackID := range stackIDs {
		serviceNames := serviceTargets[stackID]
		if serviceNames != nil && len(serviceNames) == 0 {
			steps = append(steps, store.JobWorkflowStep{Action: "skip", State: "queued", TargetStackID: stackID})
			continue
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
		steps = append(steps, store.JobWorkflowStep{Action: "up", State: "queued", TargetStackID: stackID, TargetServiceNames: serviceNames})
	}
	if options.PruneAfter {
		steps = append(steps, store.JobWorkflowStep{Action: "prune", State: "queued"})
	}
	return steps, nil
}

func (s *Service) runUpdateWorkflowStep(ctx context.Context, step store.JobWorkflowStep, options UpdateOptions, onProgress func(stacks.StepProgress)) (string, error) {
	if step.Action == "prune" {
		return s.maintenance.RunSystemPrune(ctx, options.IncludeVolumes)
	}
	if step.Action == "skip" {
		return "Skipped " + step.TargetStackID + " because all services are excluded.", nil
	}
	return s.stackReader.RunMaintenanceStepStreaming(ctx, step.TargetStackID, step.Action, stacks.MaintenanceStepOptions{
		RemoveOrphans: options.RemoveOrphans,
		ServiceNames:  step.TargetServiceNames,
	}, onProgress)
}

var progressUnits = map[string]string{
	"pull":  "layers",
	"build": "steps",
	"up":    "services",
	"skip":  "services",
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

func markWorkflowFailed(steps []store.JobWorkflowStep, index int) []store.JobWorkflowStep {
	if index >= 0 && index < len(steps) {
		steps[index].State = "failed"
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
