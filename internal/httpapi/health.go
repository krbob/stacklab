package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

type readinessCheck struct {
	name  string
	check func(context.Context) error
}

type healthCheckResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type readinessResponse struct {
	Status  string                         `json:"status"`
	Version string                         `json:"version"`
	Checks  map[string]healthCheckResponse `json:"checks"`
}

func defaultReadinessChecks(cfg config.Config, appStore *store.Store, runtimeContext context.Context) []readinessCheck {
	return []readinessCheck{
		{
			name: "database",
			check: func(ctx context.Context) error {
				if appStore == nil {
					return errors.New("database is not configured")
				}
				return appStore.Ping(ctx)
			},
		},
		{
			name: "frontend",
			check: func(context.Context) error {
				return checkFrontendAssets(cfg.FrontendDistDir)
			},
		},
		{
			name: "runtime",
			check: func(context.Context) error {
				if runtimeContext == nil {
					return errors.New("runtime context is not configured")
				}
				select {
				case <-runtimeContext.Done():
					return errors.New("runtime is shutting down")
				default:
					return nil
				}
			},
		},
	}
}

func checkFrontendAssets(frontendDistDir string) error {
	indexPath := filepath.Join(frontendDistDir, "index.html")
	file, err := os.Open(indexPath)
	if err != nil {
		return fmt.Errorf("open frontend index: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat frontend index: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("frontend index is missing or empty")
	}
	return nil
}

func (h *Handler) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": stacks.AppVersion,
	})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	response := h.evaluateReadiness(ctx)
	status := http.StatusOK
	if response.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, response)
}

func (h *Handler) evaluateReadiness(ctx context.Context) readinessResponse {
	response := readinessResponse{
		Status:  "ok",
		Version: stacks.AppVersion,
		Checks:  make(map[string]healthCheckResponse, len(h.readinessChecks)),
	}
	for _, probe := range h.readinessChecks {
		if err := probe.check(ctx); err != nil {
			response.Status = "unavailable"
			response.Checks[probe.name] = healthCheckResponse{Status: "error", Message: "unavailable"}
			if h.logger != nil {
				h.logger.Warn("readiness check failed", slog.String("component", probe.name), slog.String("err", err.Error()))
			}
			continue
		}
		response.Checks[probe.name] = healthCheckResponse{Status: "ok"}
	}

	checks := make(map[string]string, len(response.Checks))
	for name, check := range response.Checks {
		checks[name] = check.Status
	}
	h.serviceMetrics.ReadinessChecked(response.Status, checks, time.Now().UTC())
	return response
}
