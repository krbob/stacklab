package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"stacklab/internal/auth"
	"stacklab/internal/gitworkspace"
)

func (h *workspaceController) handleRestoreGitWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	var request gitworkspace.RestoreFileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	response, err := h.gitStatus.RestoreFile(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrValidation), errors.Is(err, gitworkspace.ErrInvalidManagedPath):
			writeError(w, http.StatusBadRequest, "validation_failed", "A managed file path and expected_state from its diff are required.", nil)
		case errors.Is(err, gitworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the managed workspace or follows a symbolic link.", nil)
		case errors.Is(err, gitworkspace.ErrPathNotFile), errors.Is(err, gitworkspace.ErrRestoreUnsupported):
			writeError(w, http.StatusBadRequest, "restore_not_supported", "Only modified or deleted regular files present in the last commit can be restored.", nil)
		case errors.Is(err, gitworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Changed file was not found. Refresh Changes before restoring.", nil)
		case errors.Is(err, gitworkspace.ErrConflict):
			writeError(w, http.StatusConflict, "edit_conflict", "The file, staged changes, or latest commit changed. Reload the diff before restoring.", nil)
		case errors.Is(err, gitworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "File cannot be restored due to permissions. Check access to the file and its parent directory.", nil)
		case errors.Is(err, gitworkspace.ErrOperationInProgress):
			writeError(w, http.StatusConflict, "operation_in_progress", "Finish the current Git operation before restoring.", nil)
		default:
			h.logger.Error("restore workspace file failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to restore workspace file. Refresh Changes to check its current state.", nil)
		}
		return
	}
	details := map[string]any{"path": response.Path, "scope": response.Scope, "head_commit": response.HeadCommit}
	if err := h.audit.RecordGitFileRestore(r.Context(), response.Path, response.StackID, "local", details); err != nil {
		h.logger.Warn("record workspace file restoration audit failed", slog.String("err", err.Error()))
	}
	writeJSON(w, http.StatusOK, response)
}
