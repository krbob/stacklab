package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"stacklab/internal/auth"
	"stacklab/internal/configworkspace"
	"stacklab/internal/gitworkspace"
	"stacklab/internal/workspacerepair"
)

type workspaceController struct {
	*Handler
}

func (c *workspaceController) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config/workspace/tree", c.withAuth(c.handleConfigWorkspaceTree))
	mux.HandleFunc("GET /api/config/workspace/file", c.withAuth(c.handleConfigWorkspaceFile))
	mux.HandleFunc("PUT /api/config/workspace/file", c.withAuth(c.handlePutConfigWorkspaceFile))
	mux.HandleFunc("POST /api/config/workspace/repair-permissions", c.withAuth(c.handleRepairConfigWorkspacePermissions))
	mux.HandleFunc("GET /api/git/workspace/status", c.withAuth(c.handleGitWorkspaceStatus))
	mux.HandleFunc("GET /api/git/workspace/diff", c.withAuth(c.handleGitWorkspaceDiff))
	mux.HandleFunc("POST /api/git/workspace/commit", c.withAuth(c.handleGitWorkspaceCommit))
	mux.HandleFunc("POST /api/git/workspace/push", c.withAuth(c.handleGitWorkspacePush))
}

func (h *workspaceController) handleConfigWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	response, err := h.configFiles.Tree(r.Context(), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace path was not found.", nil)
		case errors.Is(err, configworkspace.ErrPathNotDirectory):
			writeError(w, http.StatusBadRequest, "path_not_directory", "Path is not a directory.", nil)
		case errors.Is(err, configworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Config workspace path is not readable by the Stacklab service.", nil)
		default:
			h.logger.Error("config workspace tree failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load config workspace tree.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handleConfigWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	response, err := h.configFiles.File(r.Context(), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace file was not found.", nil)
		case errors.Is(err, configworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Path is not a file.", nil)
		case errors.Is(err, configworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Config workspace file is not readable by the Stacklab service.", nil)
		default:
			h.logger.Error("config workspace file failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load config workspace file.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handlePutConfigWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request configworkspace.SaveFileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.configFiles.SaveFile(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace path was not found.", nil)
		case errors.Is(err, configworkspace.ErrPathNotDirectory):
			writeError(w, http.StatusBadRequest, "path_not_directory", "Parent path is not a directory.", nil)
		case errors.Is(err, configworkspace.ErrPathNotFile):
			writeError(w, http.StatusBadRequest, "path_not_file", "Path is not a file.", nil)
		case errors.Is(err, configworkspace.ErrBinaryNotEditable):
			writeError(w, http.StatusConflict, "binary_not_editable", "Binary files cannot be edited in the browser.", nil)
		case errors.Is(err, configworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Config workspace file cannot be edited due to file permissions.", nil)
		case errors.Is(err, configworkspace.ErrConflict):
			writeError(w, http.StatusConflict, "edit_conflict", "File changed on disk. Reload it before saving again.", nil)
		default:
			h.logger.Error("save config workspace file failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save config workspace file.", nil)
		}
		return
	}

	details := map[string]any{
		"path": response.Path,
		"type": "text_file",
	}
	if stackID := deriveConfigWorkspaceStackID(response.Path); stackID != nil {
		details["stack_id"] = *stackID
	}
	if err := h.audit.RecordConfigFileSave(r.Context(), response.Path, deriveConfigWorkspaceStackID(response.Path), "local", details); err != nil {
		h.logger.Warn("record save_config_file audit failed", slog.String("path", response.Path), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handleRepairConfigWorkspacePermissions(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request configworkspace.RepairPermissionsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.configFiles.RepairPermissions(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, configworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the config workspace.", nil)
		case errors.Is(err, configworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Workspace path was not found.", nil)
		case errors.Is(err, workspacerepair.ErrUnsupported):
			writeError(w, http.StatusNotImplemented, "not_implemented", "Workspace permission repair is not configured yet.", nil)
		default:
			h.logger.Error("repair config workspace permissions failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to repair config workspace permissions.", nil)
		}
		return
	}

	details := map[string]any{
		"path":          response.Path,
		"recursive":     response.Recursive,
		"changed_items": response.ChangedItems,
	}
	if len(response.Warnings) > 0 {
		details["warnings"] = response.Warnings
	}
	if err := h.audit.RecordConfigPermissionRepair(r.Context(), response.Path, "local", details); err != nil {
		h.logger.Warn("record repair_config_workspace_permissions audit failed", slog.String("path", response.Path), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handleGitWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	response, err := h.gitStatus.Status(r.Context())
	if err != nil {
		if errors.Is(err, gitworkspace.ErrContentTooLarge) {
			writeContentTooLargeError(w, err)
			return
		}
		h.logger.Error("git workspace status failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Git workspace status.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handleGitWorkspaceDiff(w http.ResponseWriter, r *http.Request) {
	response, err := h.gitStatus.Diff(r.Context(), strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrContentTooLarge):
			writeContentTooLargeError(w, err)
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the Git workspace.", nil)
		case errors.Is(err, gitworkspace.ErrInvalidManagedPath):
			writeError(w, http.StatusBadRequest, "validation_failed", "Path must be under stacks/ or config/.", nil)
		case errors.Is(err, gitworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Changed file was not found.", nil)
		default:
			h.logger.Error("git workspace diff failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load Git diff.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handleGitWorkspaceCommit(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request gitworkspace.CommitRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeJSONError(w, err)
		return
	}

	response, err := h.gitStatus.Commit(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrPathOutsideWorkspace):
			writeError(w, http.StatusBadRequest, "path_outside_workspace", "Path escapes the Git workspace.", nil)
		case errors.Is(err, gitworkspace.ErrInvalidManagedPath), errors.Is(err, gitworkspace.ErrValidation):
			writeError(w, http.StatusBadRequest, "validation_failed", "Commit request is invalid.", nil)
		case errors.Is(err, gitworkspace.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Selected changed file was not found.", nil)
		case errors.Is(err, gitworkspace.ErrConflictedSelection):
			writeError(w, http.StatusConflict, "conflicted_files_selected", "Resolve conflicted files before committing.", nil)
		case errors.Is(err, gitworkspace.ErrOperationInProgress):
			writeError(w, http.StatusConflict, "operation_in_progress", "Finish the current Git operation before committing.", nil)
		case errors.Is(err, gitworkspace.ErrPermissionDenied):
			writeError(w, http.StatusConflict, "permission_denied", "Selected files could not be staged due to permissions.", nil)
		case errors.Is(err, gitworkspace.ErrNothingToCommit):
			writeError(w, http.StatusConflict, "nothing_to_commit", "Selected files have no commit-ready changes.", nil)
		default:
			h.logger.Error("git workspace commit failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create Git commit.", nil)
		}
		return
	}

	details := map[string]any{
		"paths":             response.Paths,
		"path_count":        len(response.Paths),
		"commit":            response.Commit,
		"summary":           response.Summary,
		"remaining_changes": response.RemainingChanges,
	}
	if err := h.audit.RecordGitCommit(r.Context(), "local", response.Commit, response.Summary, response.Paths, response.RemainingChanges, details); err != nil {
		h.logger.Warn("record git commit audit failed", slog.String("commit", response.Commit), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *workspaceController) handleGitWorkspacePush(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	response, err := h.gitStatus.Push(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, gitworkspace.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "git_unavailable", "Git workspace is unavailable.", nil)
		case errors.Is(err, gitworkspace.ErrUpstreamNotConfigured):
			writeError(w, http.StatusConflict, "upstream_not_configured", "Current branch has no configured upstream.", nil)
		case errors.Is(err, gitworkspace.ErrAuthFailed):
			writeError(w, http.StatusBadGateway, "git_auth_failed", "Push failed due to remote authentication.", nil)
		case errors.Is(err, gitworkspace.ErrPushRejected):
			writeError(w, http.StatusConflict, "push_rejected", "Remote rejected the push.", nil)
		default:
			h.logger.Error("git workspace push failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to push Git changes.", nil)
		}
		return
	}

	details := map[string]any{
		"remote":        response.Remote,
		"branch":        response.Branch,
		"upstream_name": response.UpstreamName,
		"head_commit":   response.HeadCommit,
		"pushed":        response.Pushed,
		"ahead_count":   response.AheadCount,
		"behind_count":  response.BehindCount,
	}
	if err := h.audit.RecordGitPush(r.Context(), "local", response.Remote, response.Branch, response.UpstreamName, response.HeadCommit, response.Pushed, response.AheadCount, response.BehindCount, details); err != nil {
		h.logger.Warn("record git push audit failed", slog.String("branch", response.Branch), slog.String("err", err.Error()))
	}

	writeJSON(w, http.StatusOK, response)
}
