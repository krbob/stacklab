package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"stacklab/internal/limitedio"
	"stacklab/internal/servicemetrics"
	"stacklab/internal/stacks"
)

func (h *Handler) initializePrometheusMetrics() error {
	if h.cfg.MetricsTokenFile == "" {
		return nil
	}
	data, err := limitedio.ReadFile(h.cfg.MetricsTokenFile, 4096)
	if err != nil {
		return fmt.Errorf("read metrics token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || len(token) > 256 || strings.ContainsAny(token, " \t\r\n") {
		return fmt.Errorf("metrics token must contain 32–256 characters without whitespace")
	}
	expected := sha256.Sum256([]byte(token))
	registry := servicemetrics.NewPrometheusRegistry(h.serviceMetrics, stacks.AppVersion, stacks.AppCommit)
	expose := promhttp.HandlerFor(registry, promhttp.HandlerOpts{MaxRequestsInFlight: 2, Timeout: 5 * time.Second})
	h.metricsHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		provided, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		actual := sha256.Sum256([]byte(provided))
		if !bearer || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "A metrics bearer token is required.", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		h.evaluateReadiness(ctx)
		expose.ServeHTTP(w, r)
	})
	return nil
}

func (h *Handler) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if h.metricsHandler == nil {
		http.NotFound(w, r)
		return
	}
	h.metricsHandler.ServeHTTP(w, r)
}
