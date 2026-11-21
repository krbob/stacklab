package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"stacklab/internal/auth"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/jobs"
)

func (h *workspaceController) handleGitIncoming(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	result, err := h.gitStatus.Incoming(r.Context())
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *workspaceController) handleGitIncomingDiff(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	var request gitworkspace.IncomingDiffRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	result, err := h.gitStatus.IncomingDiff(r.Context(), request)
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *workspaceController) handleGitPull(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	var request gitworkspace.PullRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	if len(request.ExpectedState) != 64 {
		writeGitSyncError(w, gitworkspace.ErrValidation)
		return
	}
	h.runGitCheckout(w, r, "git_pull", func(ctx context.Context) (gitworkspace.PullResponse, error) { return h.gitStatus.Pull(ctx, request) })
}

func (h *workspaceController) runGitCheckout(w http.ResponseWriter, r *http.Request, action string, checkout func(context.Context) (gitworkspace.PullResponse, error)) {
	if !h.workspaceWriteMu.TryLock() {
		writeError(w, http.StatusConflict, "operation_in_progress", "A workspace edit is in progress. Retry when it finishes.", nil)
		return
	}
	defer h.workspaceWriteMu.Unlock()
	job, err := h.jobs.StartWithResources(r.Context(), "", action, "local", jobs.GlobalResource())
	if err != nil {
		if errors.Is(err, jobs.ErrResourceConflict) {
			writeError(w, http.StatusConflict, "operation_in_progress", "Wait for active host or stack operations before downloading configuration.", nil)
		} else {
			writeGitSyncError(w, err)
		}
		return
	}
	result, pullErr := checkout(r.Context())
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer cancel()
	if pullErr != nil {
		finished, err := h.jobs.FinishFailed(finishCtx, job, action+"_failed", "Configuration synchronization failed. Refresh Remote updates and inspect the checkout before retrying.")
		if err == nil {
			_ = h.audit.RecordJob(finishCtx, finished, nil)
		}
		writeGitSyncError(w, pullErr)
		return
	}
	finished, err := h.jobs.FinishSucceeded(finishCtx, job)
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	if err := h.audit.RecordJob(finishCtx, finished, map[string]any{"base_commit": result.Receipt.BaseCommit, "head_commit": result.Receipt.HeadCommit, "branch": result.Receipt.Branch, "files": result.Receipt.Files}); err != nil {
		h.logger.Warn("record Git download audit failed")
	}
	writeJSON(w, http.StatusOK, result)
}

// File edits may run concurrently with one another, but never with a checkout
// replacement. Job reservations separately exclude deploy/capture/restore work.
func (h *Handler) withWorkspaceEdit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.workspaceWriteMu.TryRLock() {
			writeError(w, http.StatusConflict, "operation_in_progress", "Configuration is being downloaded. Reload the file after the download finishes before editing it.", nil)
			return
		}
		defer h.workspaceWriteMu.RUnlock()
		next(w, r)
	}
}

func writeGitSyncError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gitworkspace.ErrDraftExists):
		writeError(w, http.StatusConflict, "draft_exists", "A reconciliation draft already exists. Resume it, or explicitly discard it before preparing another.", nil)
	case errors.Is(err, gitworkspace.ErrDraftStale):
		writeError(w, http.StatusConflict, "draft_stale", "This draft changed, was already applied, or no longer matches the checkout. Reload the draft and check remote updates. Preserve local edits before preparing a new draft.", nil)
	case errors.Is(err, gitworkspace.ErrReconcileBlocked):
		writeError(w, http.StatusConflict, "reconcile_blocked", "Reconciliation requires diverged commits and a clean checkout without submodules or hidden index flags. Save local edits with Commit in Changes first.", nil)
	case errors.Is(err, gitworkspace.ErrResolutionInvalid):
		writeError(w, http.StatusBadRequest, "invalid_resolution", "Choose local, remote, delete, or edit an ordinary UTF-8 text file within config/ or stacks/.", nil)
	case errors.Is(err, gitworkspace.ErrValidation):
		writeError(w, http.StatusBadRequest, "validation_failed", "A reviewed expected_state and, for a diff, an incoming file path are required.", nil)
	case errors.Is(err, gitworkspace.ErrConflict):
		writeError(w, http.StatusConflict, "edit_conflict", "The branch or reviewed commits changed. Check remote updates and review again.", nil)
	case errors.Is(err, gitworkspace.ErrPullBlocked):
		writeError(w, http.StatusConflict, "pull_blocked", "Download is blocked. Check remote updates for local edits, divergent history or an unfinished Git operation.", nil)
	case errors.Is(err, gitworkspace.ErrOperationInProgress):
		writeError(w, http.StatusConflict, "operation_in_progress", "Another Git operation is in progress. Retry when it finishes.", nil)
	case errors.Is(err, gitworkspace.ErrAuthFailed):
		writeError(w, http.StatusBadGateway, "git_auth_failed", "Cannot access the remote. Check repository access and Git credentials for the Stacklab service account.", nil)
	case errors.Is(err, gitworkspace.ErrHostKeyFailed):
		writeError(w, http.StatusBadGateway, "git_host_key_failed", "Verify the Git server fingerprint and the service account's known_hosts file.", nil)
	case errors.Is(err, gitworkspace.ErrPermissionDenied):
		writeError(w, http.StatusConflict, "permission_denied", "Check the service account's access to the checkout and Git credentials.", nil)
	case errors.Is(err, gitworkspace.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "File is not part of this incoming update.", nil)
	case errors.Is(err, gitworkspace.ErrContentTooLarge):
		writeContentTooLargeError(w, err)
	default:
		writeError(w, http.StatusBadGateway, "git_sync_failed", "Git operation did not finish. Check remote connectivity, local file collisions and Git locks, then refresh Remote updates and reload any saved draft before retrying.", nil)
	}
}
