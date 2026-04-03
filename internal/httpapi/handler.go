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
	"stacklab/internal/stacks"
	"strings"
	"time"
)

type Handler struct {
	cfg         config.Config
	logger      *slog.Logger
	mux         *http.ServeMux
	auth        *auth.Service
	stackReader *stacks.ServiceReader
}

func NewHandler(cfg config.Config, logger *slog.Logger, authService *auth.Service) (http.Handler, error) {
	handler := &Handler{
		cfg:         cfg,
		logger:      logger,
		mux:         http.NewServeMux(),
		auth:        authService,
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
