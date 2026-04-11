package maintenancejobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

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

func (s *Service) RunUpdate(ctx context.Context, request UpdateRequest, requestedBy string) (store.Job, error) {
	if request.Options.IncludeVolumes && !request.Options.PruneAfter {
		return store.Job{}, errors.New("include_volumes requires prune_after = true")
	}

	targetStackIDs, err := s.ResolveTargetStacks(ctx, request.Target.Mode, request.Target.StackIDs)
	if err != nil {
		return store.Job{}, err
	}

	workflow, err := s.buildUpdateWorkflow(ctx, targetStackIDs, request.Options)
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
		output, runErr := s.runUpdateWorkflowStep(ctx, step, request.Options)
		if trimmed := strings.TrimSpace(output); trimmed != "" {
			_ = s.jobs.PublishEvent(ctx, job, "job_log", updateStepMessage("Output", step), trimmed, workflowStepRef(workflow, index))
		}
		if runErr != nil {
			workflow = markWorkflowFailed(workflow, index)
			if updatedJob, updateErr := s.jobs.UpdateWorkflow(ctx, job, workflow); updateErr == nil {
				job = updatedJob
			}
			job, _ = s.jobs.FinishFailed(ctx, job, "update_stacks_failed", runErr.Error())
			if err := s.audit.RecordJob(ctx, job, updateAuditDetails(request, targetStackIDs)); err != nil && s.logger != nil {
				s.logger.Warn("record maintenance audit failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
			}
			return job, nil
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

func (s *Service) buildUpdateWorkflow(ctx context.Context, stackIDs []string, options UpdateOptions) ([]store.JobWorkflowStep, error) {
	steps := make([]store.JobWorkflowStep, 0, len(stackIDs)*3+1)
	for _, stackID := range stackIDs {
		if options.PullImages {
			steps = append(steps, store.JobWorkflowStep{Action: "pull", State: "queued", TargetStackID: stackID})
		}
		if options.BuildImages {
			needsBuild, err := s.stackReader.MaintenanceNeedsBuild(ctx, stackID)
			if err != nil {
				return nil, err
			}
			if needsBuild {
				steps = append(steps, store.JobWorkflowStep{Action: "build", State: "queued", TargetStackID: stackID})
			}
		}
		steps = append(steps, store.JobWorkflowStep{Action: "up", State: "queued", TargetStackID: stackID})
	}
	if options.PruneAfter {
		steps = append(steps, store.JobWorkflowStep{Action: "prune", State: "queued"})
	}
	return steps, nil
}

func (s *Service) runUpdateWorkflowStep(ctx context.Context, step store.JobWorkflowStep, options UpdateOptions) (string, error) {
	if step.Action == "prune" {
		return s.maintenance.RunSystemPrune(ctx, options.IncludeVolumes)
	}
	return s.stackReader.RunMaintenanceStep(ctx, step.TargetStackID, step.Action, stacks.MaintenanceStepOptions{
		RemoveOrphans: options.RemoveOrphans,
	})
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
		Index:         index + 1,
		Total:         len(steps),
		Action:        steps[index].Action,
		TargetStackID: steps[index].TargetStackID,
	}
}

func updateStepMessage(prefix string, step store.JobWorkflowStep) string {
	label := maintenanceActionLabel(step.Action)
	if step.TargetStackID == "" {
		return prefix + " " + strings.ToLower(label) + "."
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
	if request.Trigger != "" {
		details["trigger"] = request.Trigger
	}
	if request.ScheduleKey != "" {
		details["schedule_key"] = request.ScheduleKey
	}
	return details
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
	unique := map[string]struct{}{}
	for _, stackID := range stackIDs {
		stackID = strings.TrimSpace(stackID)
		if stackID == "" {
			continue
		}
		unique[stackID] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for stackID := range unique {
		result = append(result, stackID)
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
