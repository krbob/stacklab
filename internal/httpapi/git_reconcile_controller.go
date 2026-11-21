package httpapi

import (
	"context"
	"net/http"

	"stacklab/internal/auth"
	"stacklab/internal/gitworkspace"
)

func (h *workspaceController) handleGetReconcile(w http.ResponseWriter, r *http.Request) {
	result, err := h.gitStatus.Reconcile(r.Context())
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func reconcileRequest[T any](w http.ResponseWriter, r *http.Request, request *T) bool {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return false
	}
	if err := decodeJSON(w, r, request); err != nil {
		writeDecodeJSONError(w, err)
		return false
	}
	return true
}
func (h *workspaceController) handlePrepareReconcile(w http.ResponseWriter, r *http.Request) {
	var request gitworkspace.PullRequest
	if !reconcileRequest(w, r, &request) {
		return
	}
	result, err := h.gitStatus.PrepareReconcile(r.Context(), request)
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *workspaceController) handleReconcileFile(w http.ResponseWriter, r *http.Request) {
	var request gitworkspace.ReconcileFileRequest
	if !reconcileRequest(w, r, &request) {
		return
	}
	result, err := h.gitStatus.ReconcileFile(r.Context(), request)
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *workspaceController) handleResolveReconcile(w http.ResponseWriter, r *http.Request) {
	var request gitworkspace.ResolveRequest
	if !reconcileRequest(w, r, &request) {
		return
	}
	result, err := h.gitStatus.ResolveReconcile(r.Context(), request)
	if err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *workspaceController) handleDiscardReconcile(w http.ResponseWriter, r *http.Request) {
	var request gitworkspace.ReconcileRequest
	if !reconcileRequest(w, r, &request) {
		return
	}
	if err := h.gitStatus.DiscardReconcile(r.Context(), request); err != nil {
		writeGitSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"discarded": true})
}
func (h *workspaceController) handleApplyReconcile(w http.ResponseWriter, r *http.Request) {
	var request gitworkspace.ReconcileRequest
	if !reconcileRequest(w, r, &request) {
		return
	}
	h.runGitCheckout(w, r, "git_reconcile", func(ctx context.Context) (gitworkspace.PullResponse, error) {
		return h.gitStatus.ApplyReconcile(ctx, request)
	})
}
