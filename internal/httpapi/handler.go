package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"stacklab/internal/auth"
	"stacklab/internal/config"
	"stacklab/internal/jobs"
	"stacklab/internal/stacks"
	"strings"
	"time"
)

type Handler struct {
	cfg         config.Config
	logger      *slog.Logger
	mux         *http.ServeMux
	auth        *auth.Service
	jobs        *jobs.Service
	stackReader *stacks.ServiceReader
}

func NewHandler(cfg config.Config, logger *slog.Logger, authService *auth.Service, jobService *jobs.Service) (http.Handler, error) {
	handler := &Handler{
		cfg:         cfg,
		logger:      logger,
		mux:         http.NewServeMux(),
		auth:        authService,
		jobs:        jobService,
		stackReader: stacks.NewServiceReader(cfg, logger),
	}

	handler.registerRoutes()

	return handler.withLogging(handler.mux), nil
}

func (h *Handler) registerRoutes() {
	h.mux.HandleFunc("GET /api/health", h.handleHealth)
	h.mux.HandleFunc("GET /api/session", h.handleSession)
	h.mux.HandleFunc("POST /api/auth/login", h.handleLogin)
	h.mux.HandleFunc("POST /api/auth/logout", h.withAuth(h.handleLogout))
	h.mux.HandleFunc("GET /api/meta", h.withAuth(h.handleMeta))
	h.mux.HandleFunc("GET /api/stacks", h.withAuth(h.handleListStacks))
	h.mux.HandleFunc("GET /api/stacks/{stackId}", h.withAuth(h.handleGetStack))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/definition", h.withAuth(h.handleGetDefinition))
	h.mux.HandleFunc("PUT /api/stacks/{stackId}/definition", h.withAuth(h.handlePutDefinition))
	h.mux.HandleFunc("GET /api/stacks/{stackId}/resolved-config", h.withAuth(h.handleGetResolvedConfig))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/resolved-config", h.withAuth(h.handlePostResolvedConfig))
	h.mux.HandleFunc("POST /api/stacks/{stackId}/actions/{action}", h.withAuth(h.handleRunStackAction))
	h.mux.HandleFunc("POST /api/settings/password", h.withAuth(h.handleUpdatePassword))
	h.mux.HandleFunc("GET /api/jobs/{jobId}", h.withAuth(h.handleGetJob))
	h.mux.HandleFunc("/api/", h.withAuth(h.handleAPINotImplemented))
	h.mux.HandleFunc("/", h.handleFrontend)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": stacks.AppVersion,
	})
}

func (h *Handler) handleSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.auth.AuthenticateRequest(r.Context(), r)
	if err != nil {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user": map[string]any{
			"id":           session.UserID,
			"display_name": "Local Operator",
		},
		"features": map[string]any{
			"host_shell": false,
		},
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	session, err := h.auth.Login(r.Context(), request.Password, r.UserAgent(), auth.ClientIP(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid password.", nil)
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "auth_not_configured", "Authentication is not configured yet.", nil)
		default:
			h.logger.Error("login failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to authenticate.", nil)
		}
		return
	}

	http.SetCookie(w, h.auth.SessionCookie(session))
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	cookie, err := r.Cookie(h.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	if err := h.auth.Logout(r.Context(), cookie.Value); err != nil {
		http.SetCookie(w, h.auth.ClearSessionCookie())
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
		return
	}

	http.SetCookie(w, h.auth.ClearSessionCookie())
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
	})
}

func (h *Handler) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.stackReader.Meta(r.Context()))
}

func (h *Handler) handleListStacks(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.List(r.Context(), stacks.ListQuery{
		Search: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))),
		Sort:   strings.TrimSpace(r.URL.Query().Get("sort")),
	})
	if err != nil {
		h.logger.Error("list stacks failed", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stacks.", nil)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGetStack(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Get(r.Context(), r.PathValue("stackId"))
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		default:
			h.logger.Error("get stack failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGetDefinition(w http.ResponseWriter, r *http.Request) {
	response, err := h.stackReader.Definition(r.Context(), r.PathValue("stackId"))
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack definition is not available for this stack state.", nil)
		default:
			h.logger.Error("get definition failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load stack definition.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handlePutDefinition(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request stacks.UpdateDefinitionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	job, err := h.jobs.Start(r.Context(), r.PathValue("stackId"), "save_definition", "local")
	if err != nil {
		h.logger.Error("start save_definition job failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		return
	}

	preview, saveErr := h.stackReader.SaveDefinition(r.Context(), r.PathValue("stackId"), request)
	if saveErr != nil {
		_, _ = h.jobs.FinishFailed(r.Context(), job, "save_definition_failed", saveErr.Error())
		switch {
		case errors.Is(saveErr, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(saveErr, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Stack definition cannot be updated in this state.", nil)
		default:
			h.logger.Error("save definition failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", saveErr.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save stack definition.", nil)
		}
		return
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish save_definition job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
		return
	}

	if request.ValidateAfterSave && !preview.Valid {
		h.logger.Warn("saved invalid stack definition", slog.String("stack_id", r.PathValue("stackId")), slog.String("message", preview.Error.Message))
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleGetResolvedConfig(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	switch source {
	case "", "current":
		source = "current"
	case "last_valid":
		writeError(w, http.StatusNotImplemented, "not_implemented", "last_valid resolved config is not implemented yet.", nil)
		return
	default:
		writeError(w, http.StatusBadRequest, "validation_failed", "Unsupported resolved config source.", nil)
		return
	}

	response, err := h.stackReader.ResolvedConfigCurrent(r.Context(), r.PathValue("stackId"), source)
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Resolved config is not available for this stack state.", nil)
		default:
			h.logger.Error("get resolved config failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve config.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handlePostResolvedConfig(w http.ResponseWriter, r *http.Request) {
	var request stacks.ResolvedConfigRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	response, err := h.stackReader.ResolvedConfigDraft(r.Context(), r.PathValue("stackId"), request)
	if err != nil {
		switch {
		case errors.Is(err, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(err, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Resolved config is not available for this stack state.", nil)
		default:
			h.logger.Error("post resolved config failed", slog.String("stack_id", r.PathValue("stackId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve draft config.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("jobId"))
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Job was not found.", nil)
		default:
			h.logger.Error("get job failed", slog.String("job_id", r.PathValue("jobId")), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load job.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleRunStackAction(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct{}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	stackID := r.PathValue("stackId")
	action := r.PathValue("action")

	job, err := h.jobs.Start(r.Context(), stackID, action, "local")
	if err != nil {
		switch {
		case errors.Is(err, jobs.ErrStackLocked):
			writeError(w, http.StatusConflict, "stack_locked", "Another job is already mutating this stack.", nil)
		default:
			h.logger.Error("start stack action job failed", slog.String("stack_id", stackID), slog.String("action", action), slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job.", nil)
		}
		return
	}

	actionErr := h.stackReader.RunAction(r.Context(), stackID, action)
	if actionErr != nil {
		job, finishErr := h.jobs.FinishFailed(r.Context(), job, "stack_action_failed", actionErr.Error())
		if finishErr != nil {
			h.logger.Error("finish stack action job failed", slog.String("job_id", job.ID), slog.String("err", finishErr.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
			return
		}

		switch {
		case errors.Is(actionErr, stacks.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Stack was not found.", nil)
		case errors.Is(actionErr, stacks.ErrInvalidState):
			writeError(w, http.StatusConflict, "invalid_state", "Action is not allowed for this stack state.", nil)
		case errors.Is(actionErr, stacks.ErrUnsupportedAction):
			writeError(w, http.StatusBadRequest, "validation_failed", "Unsupported stack action.", nil)
		default:
			writeJSON(w, http.StatusOK, map[string]any{"job": job})
		}
		return
	}

	job, err = h.jobs.FinishSucceeded(r.Context(), job)
	if err != nil {
		h.logger.Error("finish stack action job failed", slog.String("job_id", job.ID), slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to finalize job.", nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *Handler) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected.", nil)
		return
	}

	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", "Invalid request body.", nil)
		return
	}

	if err := h.auth.UpdatePassword(r.Context(), request.CurrentPassword, request.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "unauthorized", "Current password is invalid.", nil)
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "auth_not_configured", "Authentication is not configured yet.", nil)
		default:
			h.logger.Error("update password failed", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update password.", nil)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updated": true,
	})
}

func (h *Handler) handleAPINotImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "This API endpoint is not implemented yet.", nil)
}

func (h *Handler) handleFrontend(w http.ResponseWriter, r *http.Request) {
	requestPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
	if requestPath != "" && requestPath != "." {
		candidate := filepath.Join(h.cfg.FrontendDistDir, requestPath)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			http.ServeFile(w, r, candidate)
			return
		}
	}

	indexPath := filepath.Join(h.cfg.FrontendDistDir, "index.html")
	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, indexPath)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Stacklab backend is running. Frontend assets have not been built yet.\n"))
}

func (h *Handler) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()

		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		h.logger.Info("http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Duration("duration", time.Since(startedAt)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"details": details,
		},
	})
}

func (h *Handler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := h.auth.AuthenticateRequest(r.Context(), r); err != nil {
			http.SetCookie(w, h.auth.ClearSessionCookie())
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
			return
		}

		next(w, r)
	}
}
