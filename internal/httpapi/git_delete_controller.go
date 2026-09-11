package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"stacklab/internal/auth"
	"stacklab/internal/gitworkspace"
)

func (h *workspaceController) handleDeleteGitWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}
	var request gitworkspace.DeleteFileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}
	response, err := h.gitStatus.DeleteFile(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrValidation), errors.Is(err, gitworkspace.ErrInvalidManagedPath):
			writeError(w, http.StatusBadRequest, "validation_failed", "A managed file path and expected_modified_at timestamp are required.", nil)
		case errors.Is(err, gitworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the managed workspace or follows a symbolic link.", nil)
		case errors.Is(err, gitworkspace.ErrReservedPath):
			writeError(w, http.StatusBadRequest, "reserved_path", "Use stack management for the active compose.yaml and .env files.", nil)
		case errors.Is(err, gitworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Only regular files can be deleted. Directories and symbolic links are not supported.", nil)
		case errors.Is(err, gitworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Changed file was not found. Refresh Changes before deleting.", nil)
		case errors.Is(err, gitworkspace.ErrConflict):
			writeError(w, http.StatusConflict, "edit_conflict", "File changed on disk. Reload its diff before deleting.", nil)
		case errors.Is(err, gitworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "File cannot be deleted due to permissions. Check access to its parent directory.", nil)
		case errors.Is(err, gitworkspace.ErrOperationInProgress):
			writeError(w, http.StatusConflict, "operation_in_progress", "Finish the current Git operation before deleting.", nil)
		default:
			h.logger.Error("delete changed workspace file failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete workspace file.", nil)
		}
		return
	}
	details := map[string]any{"path": response.Path, "scope": response.Scope}
	if err := h.audit.RecordConfigFileDelete(r.Context(), response.Path, response.StackID, "local", details); err != nil {
		h.logger.Warn("record workspace file deletion audit failed", slog.String("err", err.Error()))
	}
	writeJSON(w, http.StatusOK, response)
}
