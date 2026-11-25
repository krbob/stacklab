package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"stacklab/internal/auth"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

// Older releases saved the OS error in the message under a generic code.
func remainingRemoval(job store.Job) ([]store.JobWorkflowStep, []string) {
	if job.Action != "remove_stack_definition" || job.State != "failed" || job.Workflow == nil || !stacks.IsValidStackID(job.StackID) {
		return nil, nil
	}
	legacyPermissionError := job.ErrorCode == "remove_stack_failed" && (strings.HasSuffix(job.ErrorMessage, ": permission denied") || strings.HasSuffix(job.ErrorMessage, ": operation not permitted"))
	if job.ErrorCode != "remove_stack_permission_denied" && !legacyPermissionError {
		return nil, nil
	}
	steps := append([]store.JobWorkflowStep(nil), job.Workflow.Steps...)
	first := slices.IndexFunc(steps, func(step store.JobWorkflowStep) bool { return step.State != "succeeded" })
	if first < 0 || steps[first].State != "failed" || (steps[first].Action != "remove_config" && steps[first].Action != "remove_data") {
		return nil, nil
	}
	var targets []string
	for i := first; i < len(steps); i++ {
		step := steps[i]
		if i > first && step.State != "queued" && step.State != "skipped" {
			return nil, nil
		}
		switch step.Action {
		case "remove_config":
			targets = append(targets, "config/"+job.StackID)
		case "remove_data":
			targets = append(targets, "data/"+job.StackID)
		case "remove_stack_definition":
			targets = append(targets, "stacks/"+job.StackID+"/compose.yaml and .env")
		default:
			return nil, nil
		}
		steps[i].State = "queued"
	}
	return steps, targets
}

var errStaleRemoval = errors.New("A newer operation or stack definition exists. Review the current stack before removing it.")

func (h *Handler) validateRemovalRetry(ctx context.Context, job store.Job, currentRetryID string) error {
	latest, err := h.jobs.IsLatestStackJob(ctx, job, currentRetryID)
	if err != nil {
		return err
	}
	if !latest {
		return errStaleRemoval
	}
	var removedDefinition, removedRuntime bool
	for _, step := range job.Workflow.Steps {
		if step.State == "succeeded" {
			switch step.Action {
			case "down":
				removedRuntime = true
			case "remove_stack_definition":
				removedDefinition = true
			}
		}
	}
	if !removedDefinition && !removedRuntime {
		return nil
	}
	current, err := h.stackReader.Get(ctx, job.StackID)
	if errors.Is(err, stacks.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if (removedDefinition && current.Stack.RuntimeState != stacks.RuntimeStateOrphaned) ||
		(removedRuntime && len(current.Stack.Containers) > 0) {
		return errStaleRemoval
	}

	return nil
}

func (h *stackController) handleRetryRemoval(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	job, err := h.jobs.Get(r.Context(), r.PathValue("jobId"))
	if errors.Is(err, jobs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load removal job.", nil)
		return
	}
	steps, _ := remainingRemoval(job)
	if len(steps) == 0 {
		writeError(w, http.StatusConflict, "invalid_state", "This job has no removal to retry after a directory permission error.", nil)
		return
	}
	if err := h.validateRemovalRetry(r.Context(), job, ""); err != nil {
		if errors.Is(err, errStaleRemoval) {
			writeError(w, http.StatusConflict, "invalid_state", err.Error(), nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to verify the current removal.", nil)
		}
		return
	}
	if cap := h.removalRepair.Capability(r.Context()); !cap.Supported {
		reason := "Directory access repair is unavailable."
		if cap.Reason != nil {
			reason = *cap.Reason
		}
		writeError(w, http.StatusConflict, "repair_unavailable", reason, nil)
		return
	}
	h.startRemoval(w, r, job.StackID, steps, &job)
}
